/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"text/template"

	ranv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/api/v1alpha1"
	"github.com/openshift-kni/cluster-group-upgrades-operator/controllers/templates"
	utils "github.com/openshift-kni/cluster-group-upgrades-operator/controllers/utils"

	v1 "k8s.io/api/batch/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	"k8s.io/client-go/dynamic"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// templateData provides template rendering data
type templateData struct {
	Cluster               string
	ResourceName          string
	PlatformImage         string
	Operators             operatorsData
	WorkloadImage         string
	JobTimeout            uint64
	ViewUpdateIntervalSec int
}

// operatorsData provides operators data for template rendering
type operatorsData struct {
	Indexes             []string
	PackagesAndChannels []string
}

// resourceTemplate define a resource template structure
type resourceTemplate struct {
	// Must always correspond the Action or View resource name
	resourceName string
	// Template text
	template string
}

var precacheDependenciesCreateTemplates = []resourceTemplate{
	{"precache-ns-create", templates.MngClusterActCreatePrecachingNS},
	{"precache-spec-cm-create", templates.MngClusterActCreatePrecachingSpecCM},
	{"precache-sa-create", templates.MngClusterActCreateServiceAcct},
	{"precache-crb-create", templates.MngClusterActCreateClusterRoleBinding},
}

var precacheDependenciesViewTemplates = []resourceTemplate{
	{"view-precache-spec-configmap", templates.MngClusterViewConfigMap},
	{"view-precache-service-acct", templates.MngClusterViewServiceAcct},
	{"view-precache-cluster-role-binding", templates.MngClusterViewClusterRoleBinding},
}

var precacheCreateTemplates = []resourceTemplate{
	{"precache-job-create", templates.MngClusterActCreateJob},
	{"view-precache-job", templates.MngClusterViewJob},
}
var precacheJobView = []resourceTemplate{
	{"view-precache-job", templates.MngClusterViewJob},
}
var precacheDeleteTemplates = []resourceTemplate{
	{"precache-ns-delete", templates.MngClusterActDeletePrecachingNS},
}

var precacheNSViewTemplates = []resourceTemplate{
	{"view-precache-namespace", templates.MngClusterViewNamespace},
}

var precacheAllViews = []resourceTemplate{ // only used for deleting, hence empty templates
	{"view-precache-namespace", ""},
	{"view-precache-job", ""},
	{"view-precache-spec-configmap", ""},
	{"view-precache-service-acct", ""},
	{"view-precache-cluster-role-binding", ""},
}

var backupDependenciesCreateTemplates = []resourceTemplate{
	{"backup-ns-create", templates.MngClusterActCreateBackupNS},
	{"backup-sa-create", templates.MngClusterActCreateSA},
	{"backup-crb-create", templates.MngClusterActCreateRB},
	{"view-backup-namespace", templates.MngClusterViewBackupNS},
}

var backupCreateTemplates = []resourceTemplate{
	{"backup-job-create", templates.MngClusterActCreateBackupJob},
	{"view-backup-job", templates.MngClusterViewBackupJob},
}

var backupJobView = []resourceTemplate{
	{"view-backup-job", templates.MngClusterViewBackupJob},
}

var backupNSView = []resourceTemplate{
	{"view-backup-namespace", templates.MngClusterViewBackupNS},
}

var backupDeleteTemplates = []resourceTemplate{
	{"backup-ns-delete", templates.MngClusterActDeleteBackupNS},
}

var backupView = []resourceTemplate{ // only used for deleting, hence empty templates
	{"view-backup-job", ""},
	{"view-backup-namespace", ""},
}

var (
	jobsInitialStatus = []string{"status", "conditions"}
	jobsFinalStatus   = []string{"status", "result", "status"}
	precache          = "precache"
	backup            = "backup"
)

// createResourceFromTemplate creates managedclusteraction or managedclusterview
//      resources from templates using dynamic client
// returns:   error
func (r *ClusterGroupUpgradeReconciler) createResourcesFromTemplates(
	ctx context.Context, data *templateData, templates []resourceTemplate) error {

	for _, item := range templates {
		r.Log.Info("[createResourcesFromTemplates]", "cluster", data.Cluster, "template", item.resourceName)
		obj := &unstructured.Unstructured{}
		w, err := r.renderYamlTemplate(item.resourceName, item.template, *data)
		if err != nil && !errors.IsAlreadyExists(err) {
			return err
		}

		// decode YAML into unstructured.Unstructured
		dec := yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)
		_, _, err = dec.Decode(w.Bytes(), nil, obj)
		if err != nil {
			return err
		}
		err = r.Create(ctx, obj)
		if err != nil {
			if errors.IsAlreadyExists(err) {
				r.Log.Info("[createResourcesFromTemplates] Already exists",
					"cluster", data.Cluster, "template", item.resourceName)
				return nil
			}
			return err
		}
	}
	return nil
}

// deleteManagedClusterViewResource deletes view by name and namespace
// returns: error
func (r *ClusterGroupUpgradeReconciler) deleteManagedClusterViewResource(
	ctx context.Context, name string, namespace string) error {

	config := ctrl.GetConfigOrDie()
	dynamic := dynamic.NewForConfigOrDie(config)
	resourceID := schema.GroupVersionResource{
		Group:    "view.open-cluster-management.io",
		Version:  "v1beta1",
		Resource: "managedclusterviews",
	}
	err := dynamic.Resource(resourceID).Namespace(namespace).Delete(
		ctx, name, metav1.DeleteOptions{})
	if err != nil {
		return err
	}
	return nil
}

// getView gets view resource
// returns:     *unstructured.Unstructured (view data)
//              bool (available)
//              error
func (r *ClusterGroupUpgradeReconciler) getView(
	ctx context.Context, name string, namespace string) (
	*unstructured.Unstructured, bool, error) {

	view := &unstructured.Unstructured{}
	view.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "view.open-cluster-management.io",
		Kind:    "ManagedClusterView",
		Version: "v1beta1",
	})
	err := r.Client.Get(ctx, client.ObjectKey{
		Name:      name,
		Namespace: namespace,
	}, view)
	if err != nil {
		if errors.IsNotFound(err) {
			return view, false, nil
		}
		return view, false, err
	}
	return view, true, nil
}

// deleteAllViews deletes all managed cluster view resources
// returns: error
func (r *ClusterGroupUpgradeReconciler) deleteAllViews(ctx context.Context, cluster string, deleteViews []resourceTemplate) error {
	// Cleanup all existing view objects that might have been left behind
	// in case of a crash etc.
	for _, item := range deleteViews {
		err := r.deleteManagedClusterViewResource(ctx, item.resourceName, cluster)
		if err != nil {
			if !errors.IsNotFound(err) {
				return err
			}
		}
	}
	return nil
}

// checkViewProcessing checks whether managedclusterview is processing
// returns: 	processing bool
func (r *ClusterGroupUpgradeReconciler) checkViewProcessing(viewConditions []interface{}) bool {
	var status string
	for _, condition := range viewConditions {
		if condition.(map[string]interface{})["type"] == "Processing" {
			status = condition.(map[string]interface{})["status"].(string)
			message := condition.(map[string]interface{})["message"].(string)
			r.Log.Info("[checkViewProcessing]", "viewStatus", message)
			break
		}
	}
	return status == "True"
}

// renderYamlTemplate renders a single yaml template
//            resourceName - resource name
//            templateBody - template body
// returns:   bytes.Buffer rendered template
//            error
func (r *ClusterGroupUpgradeReconciler) renderYamlTemplate(
	resourceName string,
	templateBody string,
	data templateData) (*bytes.Buffer, error) {

	w := new(bytes.Buffer)
	template, err := template.New(resourceName).Parse(templates.CommonTemplates + templateBody)
	if err != nil {
		return w, fmt.Errorf("failed to parse template %s: %v", resourceName, err)
	}
	data.ResourceName = resourceName
	err = template.Execute(w, data)
	if err != nil {
		return w, fmt.Errorf("failed to render template %s: %v", resourceName, err)
	}
	return w, nil
}

// precachingCleanup deletes all precaching objects. Called when upgrade is done
// returns: 			error
func (r *ClusterGroupUpgradeReconciler) precachingCleanup(
	ctx context.Context,
	clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) error {

	if clusterGroupUpgrade.Spec.PreCaching {
		clusters, err := r.getAllClustersForUpgrade(ctx, clusterGroupUpgrade)
		if err != nil {
			return fmt.Errorf("[precachingCleanup]cannot obtain the CGU cluster list: %s", err)
		}

		for _, cluster := range clusters {
			err := r.deleteAllViews(ctx, cluster, precacheAllViews)

			if err != nil {
				return err
			}
			data := templateData{
				Cluster: cluster,
			}
			err = r.createResourcesFromTemplates(ctx, &data, precacheDeleteTemplates)
			if err != nil {
				return err
			}

		}
	}
	// No precaching required
	return nil
}

// getPreparingConditions gets the pre-caching preparing conditions
// returns: condition (string)
//			error
func (r *ClusterGroupUpgradeReconciler) getPreparingConditions(
	ctx context.Context, cluster, resourceName string) (
	string, error) {

	nsView, present, err := r.getView(ctx, resourceName, cluster)
	if err != nil {
		return UnforeseenCondition, err
	}
	if !present {
		return NoNsView, nil
	}
	viewConditions, exists, err := unstructured.NestedSlice(
		nsView.Object, jobsInitialStatus...)
	if !exists {
		return UnforeseenCondition, fmt.Errorf(
			"[getPreparingConditions] no ManagedClusterView conditions found")
	}
	if err != nil {
		return UnforeseenCondition, err
	}
	if r.checkViewProcessing(viewConditions) {
		return NsFoundOnSpoke, nil
	}
	return NoNsFoundOnSpoke, nil
}

// getJobStatus gets job status from its view
// returns: condition (string)
//			error
func (r *ClusterGroupUpgradeReconciler) getJobStatus(
	jobView *unstructured.Unstructured) (string, error) {
	usJobStatus, exists, err := unstructured.NestedFieldCopy(
		jobView.Object, jobsFinalStatus...)
	if !exists {
		return UnforeseenCondition, fmt.Errorf(
			"[getJobStatus] no job status found in ManagedClusterView")
	}
	if err != nil {
		return UnforeseenCondition, err
	}
	btJobStatus, err := json.Marshal(usJobStatus)
	if err != nil {
		return UnforeseenCondition, err
	}
	var jobStatus v1.JobStatus
	err = json.Unmarshal(btJobStatus, &jobStatus)
	if err != nil {
		return UnforeseenCondition, err
	}
	if jobStatus.Active > 0 {
		return JobActive, nil
	}
	if jobStatus.Succeeded > 0 {
		return JobSucceeded, nil
	}
	for _, condition := range jobStatus.Conditions {
		if condition.Type == "Failed" && condition.Status == "True" {
			r.Log.Info("[getJobStatus]", "condition",
				condition.String())
			if condition.Reason == "DeadlineExceeded" {
				r.Log.Info("[getJobStatus]", "DeadlineExceeded",
					"Partially done")
				return JobDeadline, nil
			} else if condition.Reason == "BackoffLimitExceeded" {
				r.Log.Info("[getJobStatus]", "BackoffLimitExceeded",
					"Job failed")
				return JobBackoffLimitExceeded, nil
			}
			break
		}
	}
	return UnforeseenCondition, fmt.Errorf(string(btJobStatus))
}

// getStartingConditions gets the pre-caching starting conditions
// returns: condition (string)
//			error
func (r *ClusterGroupUpgradeReconciler) getStartingConditions(
	ctx context.Context, cluster, resourceName string, jobType string) (
	string, error) {

	if jobType == precache {
		depsViewPresent, err := r.checkDependenciesViews(ctx, cluster)
		if err != nil {
			return UnforeseenCondition, err
		}
		if !depsViewPresent {
			return DependenciesViewNotPresent, nil
		}
	}

	depsPresent, err := r.checkDependencies(ctx, cluster, jobType)
	if err != nil {
		return UnforeseenCondition, err
	}
	if !depsPresent {
		return DependenciesNotPresent, nil
	}
	jobView, present, err := r.getView(ctx, resourceName, cluster)
	if err != nil {
		return UnforeseenCondition, err
	}
	if !present {
		return NoJobView, nil
	}
	viewConditions, exists, err := unstructured.NestedSlice(
		jobView.Object, jobsInitialStatus...)
	if !exists {
		return UnforeseenCondition, fmt.Errorf(
			"[getStartingConditions] no ManagedClusterView conditions found")
	}
	if err != nil {
		return UnforeseenCondition, err
	}
	if !r.checkViewProcessing(viewConditions) {
		return NoJobFoundOnSpoke, nil
	}
	return r.getJobStatus(jobView)
}

// getActiveCondition gets the pre-caching active state conditions
// returns: condition (string)
//			error
func (r *ClusterGroupUpgradeReconciler) getActiveConditions(
	ctx context.Context, cluster, resourceName string) (
	string, error) {

	jobView, present, err := r.getView(ctx, resourceName, cluster)
	if err != nil {
		return UnforeseenCondition, err
	}
	if !present {
		return NoJobView, nil
	}
	viewConditions, exists, err := unstructured.NestedSlice(
		jobView.Object, jobsInitialStatus...)
	if !exists {
		return UnforeseenCondition, fmt.Errorf(
			"[getActiveConditions] no ManagedClusterView conditions found")
	}
	if err != nil {
		return UnforeseenCondition, err
	}
	if !r.checkViewProcessing(viewConditions) {
		return NoJobFoundOnSpoke, nil
	}
	return r.getJobStatus(jobView)
}

// checkDependenciesViews check all precache job dependencies views
//		have been deployed
// returns: available (bool)
//			error
func (r *ClusterGroupUpgradeReconciler) checkDependenciesViews(
	ctx context.Context, cluster string) (bool, error) {

	for _, item := range precacheDependenciesViewTemplates {
		_, available, err := r.getView(
			ctx, item.resourceName, cluster)
		if err != nil {
			return false, err
		}
		if !available {
			return false, nil
		}
	}
	return true, nil
}

// checkDependencies check all precache job dependencies are available
// returns: 	available (bool)
//				error
func (r *ClusterGroupUpgradeReconciler) checkDependencies(
	ctx context.Context, cluster, jobType string) (bool, error) {

	//nolint:ineffassign
	var templates []resourceTemplate
	if jobType == precache {
		//nolint:ineffassign
		templates = precacheDependenciesViewTemplates
	} else {
		templates = backupNSView
	}

	for _, item := range templates {
		view, available, err := r.getView(
			ctx, item.resourceName, cluster)
		if err != nil {
			return false, err
		}
		if !available {
			return false, nil
		}
		viewConditions, exists, err := unstructured.NestedSlice(
			view.Object, jobsInitialStatus...)
		if !exists {
			return false, fmt.Errorf(
				"[getPreparingConditions] no ManagedClusterView conditions found")
		}
		if err != nil {
			return false, err
		}
		present := r.checkViewProcessing(viewConditions)
		if !present {
			return false, nil
		}
	}
	return true, nil
}

// getPrecacheJobTemplateData initializes template data for the job creation
// returns: 	*templateData
//				error
func (r *ClusterGroupUpgradeReconciler) getPrecacheJobTemplateData(
	ctx context.Context,
	clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, clusterName string) (
	*templateData, error) {

	rv := new(templateData)

	rv.Cluster = clusterName
	rv.JobTimeout = uint64(
		clusterGroupUpgrade.Spec.RemediationStrategy.Timeout) * 60
	image, err := r.getPrecacheimagePullSpec(ctx, clusterGroupUpgrade)
	if err != nil {
		return rv, err
	}
	rv.WorkloadImage = image
	return rv, nil
}

func (r *ClusterGroupUpgradeReconciler) getBackupJobTemplateData(clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, clusterName string) *templateData {

	rv := new(templateData)

	rv.Cluster = clusterName
	rv.JobTimeout = uint64(
		clusterGroupUpgrade.Spec.RemediationStrategy.Timeout)

	// TODO: currently using hard-coded recovery image, we must get
	// image from csv.
	//	image, err := r.getPrecacheimagePullSpec(ctx, clusterGroupUpgrade)
	//	if err != nil {
	//		return rv, err
	//	}
	//	rv.WorkloadImage = image

	return rv
}

// deployPrecachingWorkload deploys precaching workload on the spoke
//          using a set of templated manifests
// returns: error
func (r *ClusterGroupUpgradeReconciler) deployWorkload(
	ctx context.Context,
	clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade,
	cluster, workloadType, mcvTemplate string, template []resourceTemplate) error {

	// the idea is to reuse the deployworkload function for backup job, so using switch to differentiate between cases
	// for example case backup:
	var spec *templateData
	var err error
	switch workloadType {
	case precache:
		spec, err = r.getPrecacheJobTemplateData(ctx, clusterGroupUpgrade, cluster)
		if err != nil {
			return err
		}
		spec.ViewUpdateIntervalSec = utils.ViewUpdateSec * len(clusterGroupUpgrade.Status.Precaching.Clusters)
		r.Log.Info("[deployPrecachingWorkload]", "getPrecacheJobTemplateData",
			cluster, "status", "success")
	case backup:
		spec = r.getBackupJobTemplateData(clusterGroupUpgrade, cluster)
		if err != nil {
			return err
		}
		r.Log.Info("[deployBackupWorkload]", "getBackupJobTemplateData",
			cluster, "spec", spec, "status", "success")
	default:
		return fmt.Errorf("[deployWorkload] no workload found to deploy")
	}

	// Delete the job view so it is refreshed
	err = r.deleteManagedClusterViewResource(ctx, mcvTemplate, cluster)
	// if the job view is not present, we should continue
	if err != nil {
		if !errors.IsNotFound(err) {
			return err
		}
	}

	err = r.createResourcesFromTemplates(ctx, spec, template)
	if err != nil {
		return err
	}
	return nil
}

// deleteDependenciesViews deletes views of precaching dependencies
// returns: 	error
func (r *ClusterGroupUpgradeReconciler) deleteDependenciesViews(
	ctx context.Context, cluster string) error {
	for _, item := range precacheDependenciesViewTemplates {
		err := r.deleteManagedClusterViewResource(
			ctx, item.resourceName, cluster)
		if err != nil {
			if errors.IsNotFound(err) {
				continue
			}
			return err
		}
	}
	return nil
}
