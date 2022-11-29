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

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// reconcilePrecaching provides the main precaching entry point
// returns: 			error
func (r *ClusterGroupUpgradeReconciler) reconcilePrecaching(
	ctx context.Context,
	clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, clusters []string, policies []*unstructured.Unstructured) error {

	if clusterGroupUpgrade.Spec.PreCaching {
		// Pre-caching is required
		if clusterGroupUpgrade.Status.Precaching == nil {
			clusterGroupUpgrade.Status.Precaching = &ranv1alpha1.PrecachingStatus{
				Spec: &ranv1alpha1.PrecachingSpec{
					PlatformImage:                "",
					OperatorsIndexes:             []string{},
					OperatorsPackagesAndChannels: []string{},
				},
				Status:   make(map[string]string),
				Clusters: []string{},
			}
		}

		precachingCondition := meta.FindStatusCondition(
			clusterGroupUpgrade.Status.Conditions, string(utils.ConditionTypes.PrecachingSuceeded))
		r.Log.Info("[reconcilePrecaching]",
			"FindStatusCondition", precachingCondition)
		if precachingCondition != nil && precachingCondition.Status == metav1.ConditionTrue {
			// Precaching is done
			return nil
		}
		// Precaching is required and not marked as done
		return r.precachingFsm(ctx, clusterGroupUpgrade, clusters, policies)
	}
	// No precaching required
	return nil
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
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("error response from update graph url %s: %d", updateGraphURL, res.StatusCode)
	}

	defer res.Body.Close()

	body, err := ioutil.ReadAll(res.Body)
	if err != nil && len(body) > 0 {
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
			case utils.SubscriptionGroupVersionKind().Kind:
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

	// Get the platform image spec from the policies
	image, err := r.extractOpenshiftImagePlatformFromPolicies(policies)
	if err != nil {
		return *new(ranv1alpha1.PrecachingSpec), err
	}
	spec.PlatformImage = image
	r.Log.Info("[extractPrecachingSpecFromPolicies]", "ClusterVersion image", spec.PlatformImage)

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

// deployDependencies deploys precaching workload dependencies
// returns: ok (bool)
//			error
func (r *ClusterGroupUpgradeReconciler) deployDependencies(
	ctx context.Context,
	clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade,
	cluster string) (bool, error) {

	spec := r.getPrecacheSpecTemplateData(clusterGroupUpgrade)
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

// getPrecacheSpecTemplateData: Converts precaching payload spec to template data
// returns: precacheTemplateData (softwareSpec)
//          error
func (r *ClusterGroupUpgradeReconciler) getPrecacheSpecTemplateData(
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
