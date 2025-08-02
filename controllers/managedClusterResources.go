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
	"os"
	"strings"
	"text/template"

	"github.com/openshift-kni/cluster-group-upgrades-operator/controllers/templates"
	utils "github.com/openshift-kni/cluster-group-upgrades-operator/controllers/utils"
	ranv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/clustergroupupgrades/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"

	viewv1beta1 "github.com/stolostron/cluster-lifecycle-api/view/v1beta1"
	v1 "k8s.io/api/batch/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// templateData provides template rendering data
type templateData struct {
	Cluster                 string
	ResourceName            string
	PlatformImage           string
	Operators               operatorsData
	WorkloadImage           string
	SpaceRequired           string
	JobTimeout              uint64
	ViewUpdateIntervalSec   int
	ExcludePrecachePatterns []string
	AdditionalImages        []string
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
	{"precache-crb-delete", templates.MngClusterActDeletePrecachingCRB},
}

var precacheNSViewTemplates = []resourceTemplate{
	{"view-precache-namespace", templates.MngClusterViewNamespace},
}

var precacheAllViews = []resourceTemplate{
	{"view-precache-namespace", utils.ManagedClusterViewPrefix},
	{"view-precache-job", utils.ManagedClusterViewPrefix},
	{"view-precache-spec-configmap", utils.ManagedClusterViewPrefix},
	{"view-precache-service-acct", utils.ManagedClusterViewPrefix},
	{"view-precache-cluster-role-binding", utils.ManagedClusterViewPrefix},
}

var precacheMCAs = []resourceTemplate{
	{"precache-ns-create", utils.ManagedClusterActionPrefix},
	{"precache-spec-cm-create", utils.ManagedClusterActionPrefix},
	{"precache-sa-create", utils.ManagedClusterActionPrefix},
	{"precache-crb-create", utils.ManagedClusterActionPrefix},
	{"precache-job-create", utils.ManagedClusterActionPrefix},
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
	{"backup-crb-delete", templates.MngClusterActDeleteBackupCRB},
}

var backupViews = []resourceTemplate{
	{"view-backup-job", utils.ManagedClusterViewPrefix},
	{"view-backup-namespace", utils.ManagedClusterViewPrefix},
}

var backupMCAs = []resourceTemplate{
	{"backup-ns-create", utils.ManagedClusterActionPrefix},
	{"backup-sa-create", utils.ManagedClusterActionPrefix},
	{"backup-crb-create", utils.ManagedClusterActionPrefix},
	{"backup-job-create", utils.ManagedClusterActionPrefix},
}

var (
	jobsInitialStatus = []string{"status", "conditions"}
	jobsFinalStatus   = []string{"status", "result", "status"}
	precache          = "precache"
	backup            = "backup"
)

func viewGroupVersionKind() schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group:   "view.open-cluster-management.io",
		Kind:    "ManagedClusterView",
		Version: "v1beta1",
	}
}

func actionGroupVersionKind() schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group:   "action.open-cluster-management.io",
		Kind:    "ManagedClusterAction",
		Version: "v1beta1",
	}
}

// createResourceFromTemplate creates managedclusteraction or managedclusterview
//
//	resources from templates
//
// returns:   error
func (r *ClusterGroupUpgradeReconciler) createResourcesFromTemplates(
	ctx context.Context, data *templateData, templates []resourceTemplate) error {

	for _, item := range templates {
		r.Log.Info("[createResourcesFromTemplates]", "cluster", data.Cluster, "template", item.resourceName)
		obj := &unstructured.Unstructured{}
		w, err := r.renderYamlTemplate(item.resourceName, item.template, *data)
		if err != nil {
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
				continue
			}
			return err
		}
	}
	return nil
}

// deleteManagedClusterResource deletes resource by name and namespace
// returns: error
func (r *ClusterGroupUpgradeReconciler) deleteManagedClusterResource(
	ctx context.Context, name string, namespace string, gvk schema.GroupVersionKind) error {

	var obj = &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gvk)
	obj.SetName(name)
	obj.SetNamespace(namespace)
	if err := r.Client.Delete(ctx, obj); client.IgnoreNotFound(err) != nil {
		return err
	}
	return nil
}

// getView gets view resource
// returns:     *unstructured.Unstructured (view data)
//
//	bool (available)
//	error
func (r *ClusterGroupUpgradeReconciler) getView(
	ctx context.Context, name string, namespace string) (
	*unstructured.Unstructured, bool, error) {

	view := &unstructured.Unstructured{}
	view.SetGroupVersionKind(viewGroupVersionKind())
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

// deleteManagedClusterResources deletes all managed cluster resources in the templates
// returns: error
func (r *ClusterGroupUpgradeReconciler) deleteManagedClusterResources(ctx context.Context, cluster string, resources []resourceTemplate) error {
	// Cleanup all existing view objects that might have been left behind
	// in case of a crash etc.
	for _, item := range resources {
		var err error
		switch item.template {
		case utils.ManagedClusterViewPrefix:
			err = r.deleteManagedClusterResource(ctx, item.resourceName, cluster, viewGroupVersionKind())
		case utils.ManagedClusterActionPrefix:
			err = r.deleteManagedClusterResource(ctx, item.resourceName, cluster, actionGroupVersionKind())
		}
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
func checkViewProcessing(viewConditions []interface{}) (bool, error) {
	var status, message string
	for _, condition := range viewConditions {
		if condition.(map[string]interface{})["type"] == viewv1beta1.ConditionViewProcessing {
			status = condition.(map[string]interface{})["status"].(string)
			message = condition.(map[string]interface{})["message"].(string)
		}
	}
	if status == "True" {
		return true, nil
	}
	if strings.HasSuffix(message, "not found") {
		return false, nil
	}
	if message == "" {
		message = "Processing condition not found in MCV status"
	}
	return false, fmt.Errorf("MCV processing error: %s", message)
}

// renderYamlTemplate renders a single yaml template
//
//	resourceName - resource name
//	templateBody - template body
//
// returns:   bytes.Buffer rendered template
//
//	error
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

// jobAndViewCleanup deletes the precaching/backup resources on hub and spoke
func (r *ClusterGroupUpgradeReconciler) jobAndViewCleanup(ctx context.Context,
	cluster string, hubResourceTemplates []resourceTemplate, spokeResourceTemplates []resourceTemplate) error {

	err := r.deleteManagedClusterResources(ctx, cluster, hubResourceTemplates)
	if err != nil {
		return err
	}
	data := templateData{
		Cluster: cluster,
	}
	return r.createResourcesFromTemplates(ctx, &data, spokeResourceTemplates)
}

// jobAndViewFinalCleanup deletes all remaining precaching/backup objects. Called when upgrade is done or CR is deleted
// returns: 			error
func (r *ClusterGroupUpgradeReconciler) jobAndViewFinalCleanup(
	ctx context.Context,
	clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) error {

	if clusterGroupUpgrade.Status.Precaching != nil {
		for cluster, status := range clusterGroupUpgrade.Status.Precaching.Status {
			if status != PrecacheStateSucceeded {
				err := r.jobAndViewCleanup(ctx, cluster, append(precacheAllViews, precacheMCAs...), precacheDeleteTemplates)
				if err != nil {
					return err
				}
			}
		}
	}

	if clusterGroupUpgrade.Status.Backup != nil {
		for cluster, status := range clusterGroupUpgrade.Status.Backup.Status {
			if status != BackupStateSucceeded {
				err := r.jobAndViewCleanup(ctx, cluster, append(backupViews, backupMCAs...), backupDeleteTemplates)
				if err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// getPreparingConditions gets the pre-caching preparing conditions
// returns: condition (string)
//
//	error
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
	processing, err := checkViewProcessing(viewConditions)
	if err != nil {
		return UnforeseenCondition, err
	}
	if processing {
		return NsFoundOnSpoke, nil
	}
	return NoNsFoundOnSpoke, nil
}

// getJobStatus gets job status from its view
// returns: condition (string)
//
//	error
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
	return UnforeseenCondition, fmt.Errorf("%s", string(btJobStatus))
}

// getStartingConditions gets the pre-caching starting conditions
// returns: condition (string)
//
//	error
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
	processing, err := checkViewProcessing(viewConditions)
	if err != nil {
		return UnforeseenCondition, err
	}
	if !processing {
		return NoJobFoundOnSpoke, nil
	}
	return r.getJobStatus(jobView)
}

// getActiveCondition gets the pre-caching active state conditions
// returns: condition (string)
//
//	error
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
	processing, err := checkViewProcessing(viewConditions)
	if err != nil {
		return UnforeseenCondition, err
	}
	if !processing {
		return NoJobFoundOnSpoke, nil
	}
	jobStatus, err := r.getJobStatus(jobView)

	if jobStatus == JobActive {
		// In this state, we need to check the managed cluster availability first before
		// proceeding based on the job status in the view. The view can contain stale
		// results if the cluster stops updating it.
		managedCluster := &clusterv1.ManagedCluster{}
		if err := r.Get(ctx, types.NamespacedName{Name: cluster}, managedCluster); err != nil {
			// Error reading managed cluster
			return UnforeseenCondition, err
		}

		availableCondition := meta.FindStatusCondition(managedCluster.Status.Conditions, clusterv1.ManagedClusterConditionAvailable)
		if availableCondition == nil || availableCondition.Status != metav1.ConditionTrue {
			return UnforeseenCondition, fmt.Errorf("[getActiveConditions] Cluster %s is unavailable", cluster)
		}
	}
	return jobStatus, err
}

// checkDependenciesViews check all precache job dependencies views
//
//	have been deployed
//
// returns: available (bool)
//
//	error
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
//
//	error
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
		present, err := checkViewProcessing(viewConditions)
		if err != nil {
			return false, err
		}
		if !present {
			return false, nil
		}
	}
	return true, nil
}

// getPrecacheJobTemplateData initializes template data for the job creation
// returns: 	*templateData
//
//	error
func (r *ClusterGroupUpgradeReconciler) getPrecacheJobTemplateData(
	ctx context.Context,
	clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, clusterName string) (
	*templateData, error) {

	rv := new(templateData)

	rv.Cluster = clusterName
	rv.JobTimeout = uint64(
		clusterGroupUpgrade.Spec.RemediationStrategy.Timeout) * 60
	image, err := r.getPrecacheImagePullSpec(ctx, clusterGroupUpgrade)
	if err != nil {
		return rv, err
	}
	rv.WorkloadImage = image
	return rv, nil
}

// getBackupJobTemplateData initializes template data for the backup job creation
// returns: 	*templateData
//
//	error
func (r *ClusterGroupUpgradeReconciler) getBackupJobTemplateData(clusterName string) (*templateData, error) {

	rv := new(templateData)
	rv.Cluster = clusterName
	rv.JobTimeout = uint64(backupJobTimeout)

	rv.WorkloadImage = os.Getenv("RECOVERY_IMG")
	r.Log.Info("[getBackupJobTemplateData]", "workload image", rv.WorkloadImage)
	// if RECOVERY_IMG is not set or empty
	if rv.WorkloadImage == "" {
		return rv, fmt.Errorf(
			"can't find recovery image pull spec in environment")
	}
	return rv, nil
}

// deployPrecachingWorkload deploys precaching workload on the spoke
//
//	using a set of templated manifests
//
// returns: error
func (r *ClusterGroupUpgradeReconciler) deployWorkload(
	ctx context.Context,
	clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade,
	cluster, workloadType, mcvTemplate string, template []resourceTemplate) error {

	var spec *templateData
	var err error
	switch workloadType {
	case precache:
		spec, err = r.getPrecacheJobTemplateData(ctx, clusterGroupUpgrade, cluster)
		if err != nil {
			return err
		}
		spec.ViewUpdateIntervalSec = utils.GetMCVUpdateInterval(len(clusterGroupUpgrade.Status.Precaching.Clusters))
		r.Log.Info("[deployPrecachingWorkload]", "getPrecacheJobTemplateData",
			cluster, "status", "success")

	case backup:
		spec, err = r.getBackupJobTemplateData(cluster)
		if err != nil {
			return err
		}
		r.Log.Info("[deployBackupWorkload]", "getBackupJobTemplateData",
			cluster, "spec", spec, "status", "success")

	default:
		return fmt.Errorf("[deployWorkload] no workload found to deploy")
	}

	// Delete the job view so it is refreshed
	err = r.deleteManagedClusterResource(ctx, mcvTemplate, cluster, viewGroupVersionKind())
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
		err := r.deleteManagedClusterResource(
			ctx, item.resourceName, cluster, viewGroupVersionKind())
		if err != nil {
			if errors.IsNotFound(err) {
				continue
			}
			return err
		}
	}
	return nil
}
