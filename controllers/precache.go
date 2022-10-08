package controllers

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"

	"strings"

	ranv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/api/v1alpha1"
	"github.com/openshift-kni/cluster-group-upgrades-operator/controllers/utils"

	v1 "k8s.io/api/batch/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// reconcilePrecaching provides the main precaching entry point
// returns: 			error
func (r *ClusterGroupUpgradeReconciler) reconcilePrecaching(
	ctx context.Context,
	clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) error {

	if clusterGroupUpgrade.Spec.PreCaching {
		// Pre-caching is required
		doneCondition := meta.FindStatusCondition(
			clusterGroupUpgrade.Status.Conditions, "PrecachingDone")
		r.Log.Info("[reconcilePrecaching]",
			"FindStatusCondition  PrecachingDone", doneCondition)
		if doneCondition != nil && doneCondition.Status == metav1.ConditionTrue {
			// Precaching is done
			return nil
		}
		// Precaching is required and not marked as done
		return r.precachingFsm(ctx, clusterGroupUpgrade)
	}
	// No precaching required
	return nil
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
			err := r.deleteAllViews(ctx, cluster)

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
	ctx context.Context, cluster string) (
	string, error) {

	nsView, present, err := r.getView(ctx, "view-precache-namespace", cluster)
	if err != nil {
		return PrecacheUnforeseenCondition, err
	}
	if !present {
		return NoNsView, nil
	}
	viewConditions, exists, err := unstructured.NestedSlice(
		nsView.Object, "status", "conditions")
	if !exists {
		return PrecacheUnforeseenCondition, fmt.Errorf(
			"[getPreparingConditions] no ManagedClusterView conditions found")
	}
	if err != nil {
		return PrecacheUnforeseenCondition, err
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
		jobView.Object, "status", "result", "status")
	if !exists {
		return PrecacheUnforeseenCondition, fmt.Errorf(
			"[getJobStatus] no job status found in ManagedClusterView")
	}
	if err != nil {
		return PrecacheUnforeseenCondition, err
	}
	btJobStatus, err := json.Marshal(usJobStatus)
	if err != nil {
		return PrecacheUnforeseenCondition, err
	}
	var jobStatus v1.JobStatus
	err = json.Unmarshal(btJobStatus, &jobStatus)
	if err != nil {
		return PrecacheUnforeseenCondition, err
	}
	if jobStatus.Active > 0 {
		return PrecacheJobActive, nil
	}
	if jobStatus.Succeeded > 0 {
		return PrecacheJobSucceeded, nil
	}
	for _, condition := range jobStatus.Conditions {
		if condition.Type == "Failed" && condition.Status == "True" {
			r.Log.Info("[getJobStatus]", "condition",
				condition.String())
			if condition.Reason == "DeadlineExceeded" {
				r.Log.Info("[getJobStatus]", "DeadlineExceeded",
					"Partially done")
				return PrecacheJobDeadline, nil
			} else if condition.Reason == "BackoffLimitExceeded" {
				return PrecacheJobBackoffLimitExceeded, nil
			}
			break
		}
	}
	return PrecacheUnforeseenCondition, fmt.Errorf(string(btJobStatus))
}

// getStartingConditions gets the pre-caching starting conditions
// returns: condition (string)
//			error
func (r *ClusterGroupUpgradeReconciler) getStartingConditions(
	ctx context.Context, cluster string) (
	string, error) {

	depsViewPresent, err := r.checkDependenciesViews(ctx, cluster)
	if err != nil {
		return PrecacheUnforeseenCondition, err
	}
	if !depsViewPresent {
		return DependenciesViewNotPresent, nil
	}
	depsPresent, err := r.checkDependencies(ctx, cluster)
	if err != nil {
		return PrecacheUnforeseenCondition, err
	}
	if !depsPresent {
		return DependenciesNotPresent, nil
	}
	jobView, present, err := r.getView(ctx, "view-precache-job", cluster)
	if err != nil {
		return PrecacheUnforeseenCondition, err
	}
	if !present {
		return NoJobView, nil
	}
	viewConditions, exists, err := unstructured.NestedSlice(
		jobView.Object, "status", "conditions")
	if !exists {
		return PrecacheUnforeseenCondition, fmt.Errorf(
			"[getStartingConditions] no ManagedClusterView conditions found")
	}
	if err != nil {
		return PrecacheUnforeseenCondition, err
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
	ctx context.Context, cluster string) (
	string, error) {

	jobView, present, err := r.getView(ctx, "view-precache-job", cluster)
	if err != nil {
		return PrecacheUnforeseenCondition, err
	}
	if !present {
		return NoJobView, nil
	}
	viewConditions, exists, err := unstructured.NestedSlice(
		jobView.Object, "status", "conditions")
	if !exists {
		return PrecacheUnforeseenCondition, fmt.Errorf(
			"[getActiveConditions] no ManagedClusterView conditions found")
	}
	if err != nil {
		return PrecacheUnforeseenCondition, err
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
	ctx context.Context, cluster string) (bool, error) {

	for _, item := range precacheDependenciesViewTemplates {
		view, available, err := r.getView(
			ctx, item.resourceName, cluster)
		if err != nil {
			return false, err
		}
		if !available {
			return false, nil
		}
		viewConditions, exists, err := unstructured.NestedSlice(
			view.Object, "status", "conditions")
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

// getImageForVersionFromUpdateGraph gets the image for the given version
// by traversing the update graph.
// Connecting to the upstream URL with the channel passed as a parameter
// the update graph is returned as JSON. This function then traverses
// the nodes list from that JSON to find the version and if found
// then returns the image
func (r *ClusterGroupUpgradeReconciler) getImageForVersionFromUpdateGraph(
	upstream string, channel string, version string) (string, error) {
	updateGraphURL := upstream + "?channel=" + channel

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: tr}
	req, _ := http.NewRequest("GET", updateGraphURL, nil)
	req.Header.Add("Accept", "application/json")
	res, err := client.Do(req)

	if err != nil {
		return "", fmt.Errorf("unable to request update graph on url %s: %w", updateGraphURL, err)
	}

	defer res.Body.Close()
	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return "", fmt.Errorf("unable to read body from response: %w", err)
	}

	var graph map[string]interface{}
	err = json.Unmarshal(body, &graph)
	if err != nil {
		return "", fmt.Errorf("unable to unmarshal body: %w", err)
	}

	nodes := graph["nodes"].([]interface{})
	for _, n := range nodes {
		node := n.(map[string]interface{})
		if node["version"] == version {
			return node["payload"].(string), nil
		}
	}

	return "", fmt.Errorf("unable to find version %s on update graph on url %s", version, updateGraphURL)
}

// extractPrecachingSpecFromPolicies extracts the software spec to be pre-cached
// 		from policies.
//		There are three object types to look at in the policies:
//      - ClusterVersion: release image must be specified to be pre-cached
//      - Subscription: provides the list of operator packages and channels
//      - CatalogSource: must be explicitly configured to be precached.
//        All the clusters in the CGU must have same catalog source(s)
// returns: precachingSpec, error
func (r *ClusterGroupUpgradeReconciler) extractPrecachingSpecFromPolicies(
	ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade,
	policies []*unstructured.Unstructured) (ranv1alpha1.PrecachingSpec, error) {

	var spec ranv1alpha1.PrecachingSpec
	for _, policy := range policies {
		objects, err := r.stripPolicy(policy.Object)
		if err != nil {
			return spec, err
		}
		for _, object := range objects {
			kind := object["kind"]
			switch kind {
			case utils.PolicyTypeClusterVersion:
				cvSpec := object["spec"].(map[string]interface{})
				desiredUpdate, found := cvSpec["desiredUpdate"]
				if !found {
					continue
				}
				image, found := desiredUpdate.(map[string]interface{})["image"]
				if found && image != "" {
					if len(spec.PlatformImage) > 0 && spec.PlatformImage != image {
						msg := fmt.Sprintf("Platform image must be set once, but %s and %s were given",
							spec.PlatformImage, image)
						meta.SetStatusCondition(&clusterGroupUpgrade.Status.Conditions, metav1.Condition{
							Type:    utils.PrecacheSpecValidCondition,
							Status:  metav1.ConditionFalse,
							Reason:  "PlatformImageConflict",
							Message: msg})
						return *new(ranv1alpha1.PrecachingSpec), nil
					}
					spec.PlatformImage = fmt.Sprintf("%s", image)
				} else {
					upstream := object["spec"].(map[string]interface {
					})["upstream"].(string)
					channel := object["spec"].(map[string]interface {
					})["channel"].(string)
					version := object["spec"].(map[string]interface {
					})["desiredUpdate"].(map[string]interface{})["version"].(string)

					image, err = r.getImageForVersionFromUpdateGraph(upstream, channel, version)

					if err != nil {
						meta.SetStatusCondition(&clusterGroupUpgrade.Status.Conditions, metav1.Condition{
							Type:    utils.PrecacheSpecValidCondition,
							Status:  metav1.ConditionFalse,
							Reason:  "PlatformImageInvalid",
							Message: err.Error()})
						return *new(ranv1alpha1.PrecachingSpec), nil
					}

					spec.PlatformImage = image.(string)
				}
				r.Log.Info("[extractPrecachingSpecFromPolicies]", "ClusterVersion image", spec.PlatformImage)
			case utils.PolicyTypeSubscription:
				packChan := fmt.Sprintf("%s:%s", object["spec"].(map[string]interface{})["name"],
					object["spec"].(map[string]interface{})["channel"])
				spec.OperatorsPackagesAndChannels = append(spec.OperatorsPackagesAndChannels, packChan)
				r.Log.Info("[extractPrecachingSpecFromPolicies]", "Operator package:channel", packChan)
				continue
			case utils.PolicyTypeCatalogSource:
				index := fmt.Sprintf("%s", object["spec"].(map[string]interface{})["image"])
				spec.OperatorsIndexes = append(spec.OperatorsIndexes, index)
				r.Log.Info("[extractPrecachingSpecFromPolicies]", "CatalogSource", index)
				continue
			default:
				continue
			}
		}
	}
	return spec, nil
}

// stripPolicy strips policy information and returns the underlying objects
// returns: []interface{} - list of the underlying objects in the policy
//			error
func (r *ClusterGroupUpgradeReconciler) stripPolicy(
	policyObject map[string]interface{}) ([]map[string]interface{}, error) {

	var objects []map[string]interface{}
	policyTemplates, exists, err := unstructured.NestedFieldCopy(
		policyObject, "spec", "policy-templates")
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("[stripPolicy] spec -> policy-templates not found")
	}

	for _, policyTemplate := range policyTemplates.([]interface{}) {
		objTemplates := policyTemplate.(map[string]interface {
		})["objectDefinition"].(map[string]interface {
		})["spec"].(map[string]interface{})["object-templates"]
		if objTemplates == nil {
			return nil, fmt.Errorf("[stripPolicy] can't find object-templates in policyTemplate")
		}
		for _, objTemplate := range objTemplates.([]interface{}) {
			spec := objTemplate.(map[string]interface{})["objectDefinition"]
			if spec == nil {
				return nil, fmt.Errorf("[stripPolicy] can't find any objectDefinition")
			}
			objects = append(objects, spec.(map[string]interface{}))
		}
	}
	return objects, nil
}

// deployPrecachingWorkload deploys precaching workload on the spoke
//          using a set of templated manifests
// returns: error
func (r *ClusterGroupUpgradeReconciler) deployPrecachingWorkload(
	ctx context.Context,
	clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade,
	cluster string) error {

	spec, err := r.getPrecacheJobTemplateData(ctx, clusterGroupUpgrade, cluster)
	if err != nil {
		return err
	}
	spec.ViewUpdateIntervalSec = utils.GetMCVUpdateInterval(len(clusterGroupUpgrade.Status.Precaching.Clusters))
	r.Log.Info("[deployPrecachingWorkload]", "getPrecacheJobTemplateData",
		cluster, "status", "success")
	// Delete the job view so it is refreshed
	err = r.deleteManagedClusterViewResource(ctx, "view-precache-job", cluster)
	if err != nil {
		return err
	}

	err = r.createResourcesFromTemplates(ctx, spec, precacheCreateTemplates)
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

// deployDependencies deploys precaching workload dependencies
// returns: ok (bool)
//			error
func (r *ClusterGroupUpgradeReconciler) deployDependencies(
	ctx context.Context,
	clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade,
	cluster string) (bool, error) {

	spec := r.getPrecacheSpecTemplateData(ctx, clusterGroupUpgrade)
	spec.Cluster = cluster
	msg := fmt.Sprintf("%v", spec)
	r.Log.Info("[deployDependencies]", "getPrecacheSpecTemplateData",
		cluster, "status", "success", "content", msg)

	err := r.createResourcesFromTemplates(ctx, spec, precacheDependenciesCreateTemplates)
	if err != nil {
		return false, err
	}
	spec.ViewUpdateIntervalSec = utils.GetMCVUpdateInterval(len(clusterGroupUpgrade.Status.Precaching.Clusters))
	err = r.createResourcesFromTemplates(ctx, spec, precacheDependenciesViewTemplates)
	if err != nil {
		return false, err
	}
	return true, nil
}

// getPrecacheimagePullSpec gets the precaching workload image pull spec.
// returns: image - pull spec string
//          error
func (r *ClusterGroupUpgradeReconciler) getPrecacheimagePullSpec(
	ctx context.Context,
	clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) (
	string, error) {

	overrides, err := r.getOverrides(ctx, clusterGroupUpgrade)
	if err != nil {
		r.Log.Error(err, "getOverrides failed ")
		return "", err
	}
	image := overrides["precache.image"]
	if image == "" {
		image = os.Getenv("PRECACHE_IMG")
		r.Log.Info("[getPrecacheimagePullSpec]", "workload image", image)
		if image == "" {
			return "", fmt.Errorf(
				"can't find pre-caching image pull spec in environment or overrides")
		}
	}
	return image, nil
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
	rv.PrecachingJobTimeout = uint64(
		clusterGroupUpgrade.Spec.RemediationStrategy.Timeout) * 60
	image, err := r.getPrecacheimagePullSpec(ctx, clusterGroupUpgrade)
	if err != nil {
		return rv, err
	}
	rv.PrecachingWorkloadImage = image
	return rv, nil
}

// getPrecacheSpecTemplateData: Converts precaching payload spec to template data
// returns: templateData (softwareSpec)
//          error
func (r *ClusterGroupUpgradeReconciler) getPrecacheSpecTemplateData(
	ctx context.Context,
	clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) *templateData {

	rv := new(templateData)
	spec := clusterGroupUpgrade.Status.Precaching.Spec
	rv.PlatformImage = spec.PlatformImage
	rv.Operators.Indexes = spec.OperatorsIndexes
	rv.Operators.PackagesAndChannels = spec.OperatorsPackagesAndChannels
	return rv
}

// includeSoftwareSpecOverrides includes software spec overrides if present
// Overrides can be used to force a specific pre-cache workload or payload
//		irrespective of the configured policies or the operator csv. This can be done
//		by creating a Configmap object named "cluster-group-upgrade-overrides"
//		in the CGU namespace with zero or more of the following "data" entries:
//		1. "precache.image" - pre-caching workload image pull spec. Normally derived
//			from the operator ClusterServiceVersion object.
//		2. "platform.image" - OCP release image pull URI
//		3. "operators.indexes" - OLM index images (list of index image URIs)
//		4. "operators.packagesAndChannels" - operator packages and channels
//			(list of  <package:channel> string entries)
//		If overrides are used, the configmap must be created before the CGU
// returns: *ranv1alpha1.PrecachingSpec, error
func (r *ClusterGroupUpgradeReconciler) includeSoftwareSpecOverrides(
	ctx context.Context,
	clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, spec *ranv1alpha1.PrecachingSpec) (
	ranv1alpha1.PrecachingSpec, error) {

	rv := new(ranv1alpha1.PrecachingSpec)

	overrides, err := r.getOverrides(ctx, clusterGroupUpgrade)
	if err != nil {
		return *rv, err
	}

	platformImage := overrides["platform.image"]
	operatorsIndexes := strings.Split(overrides["operators.indexes"], "\n")
	operatorsPackagesAndChannels := strings.Split(overrides["operators.packagesAndChannels"], "\n")
	if platformImage == "" {
		platformImage = spec.PlatformImage
	}
	rv.PlatformImage = platformImage

	if overrides["operators.indexes"] == "" {
		operatorsIndexes = spec.OperatorsIndexes
	}

	rv.OperatorsIndexes = operatorsIndexes

	if overrides["operators.packagesAndChannels"] == "" {
		operatorsPackagesAndChannels = spec.OperatorsPackagesAndChannels
	}
	rv.OperatorsPackagesAndChannels = operatorsPackagesAndChannels

	if err != nil {
		return *rv, err
	}
	return *rv, err
}

// checkPreCacheSpecConsistency checks software spec can be precached
// returns: consistent (bool), message (string)
func (r *ClusterGroupUpgradeReconciler) checkPreCacheSpecConsistency(
	spec ranv1alpha1.PrecachingSpec) (consistent bool, message string) {

	var operatorsRequested, platformRequested bool = true, true
	if len(spec.OperatorsIndexes) == 0 {
		operatorsRequested = false
	}
	if spec.PlatformImage == "" {
		platformRequested = false
	}
	if operatorsRequested && len(spec.OperatorsPackagesAndChannels) == 0 {
		return false, "inconsistent precaching configuration: olm index provided, but no packages"
	}
	if !operatorsRequested && !platformRequested {
		return false, "inconsistent precaching configuration: no software spec provided"
	}
	return true, ""
}
