package controllers

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strconv"

	"strings"

	"github.com/docker/go-units"

	"github.com/openshift-kni/cluster-group-upgrades-operator/controllers/utils"
	ranv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/clustergroupupgrades/v1alpha1"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// reconcilePrecaching provides the main precaching entry point
// returns: 			error
func (r *ClusterGroupUpgradeReconciler) reconcilePrecaching(
	ctx context.Context,
	clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, clusters []string, policies []*unstructured.Unstructured) error {

	if clusterGroupUpgrade.Spec.PreCaching && len(clusters) > 0 {
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
		} else if clusterGroupUpgrade.Status.Precaching.Status == nil {
			clusterGroupUpgrade.Status.Precaching.Status = make(map[string]string)
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

	insecureSkipVerify := os.Getenv("INSECURE_GRAPH_CALL") == "true"
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: insecureSkipVerify},
		Proxy:           http.ProxyFromEnvironment,
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

	body, err := io.ReadAll(res.Body)
	if err != nil && len(body) > 0 {
		return "", fmt.Errorf("unable to read body from response: %w", err)
	}

	var graph map[string]interface{}
	err = json.Unmarshal(body, &graph)
	if err != nil {
		return "", fmt.Errorf("unable to unmarshal body: %w", err)
	}

	if nodes, ok := graph["nodes"]; ok {
		for _, n := range nodes.([]interface{}) {
			node := n.(map[string]interface{})
			if node["version"] == version && node["payload"] != "" {
				return node["payload"].(string), nil
			}
		}
	}
	return "", fmt.Errorf("unable to find version %s on update graph on url %s", version, updateGraphURL)
}

// extractPrecachingSpecFromPolicies extracts the software spec to be pre-cached
//
//			from policies.
//			There are three object types to look at in the policies:
//	     - ClusterVersion: release image must be specified to be pre-cached
//	     - Subscription: provides the list of operator packages and channels
//	     - CatalogSource: must be explicitly configured to be precached.
//	       All the clusters in the CGU must have same catalog source(s)
//
// returns: precachingSpec, error
func (r *ClusterGroupUpgradeReconciler) extractPrecachingSpecFromPolicies(
	policies []*unstructured.Unstructured) (ranv1alpha1.PrecachingSpec, error) {

	var spec ranv1alpha1.PrecachingSpec
	for _, policy := range policies {
		objects, err := stripPolicy(policy.Object)
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
	image, err := r.extractOCPImageFromPolicies(policies)
	if err != nil {
		return ranv1alpha1.PrecachingSpec{}, err
	}
	spec.PlatformImage = image
	r.Log.Info("[extractPrecachingSpecFromPolicies]", "ClusterVersion image", spec.PlatformImage)

	return spec, nil
}

// stripPolicy strips policy information and returns the underlying objects
// filters objects with mustnothave compliance type
// returns: []interface{} - list of the underlying objects in the policy
//
//	error
func stripPolicy(
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

		plcTmplDefSpec := policyTemplate.(map[string]interface {
		})["objectDefinition"].(map[string]interface {
		})["spec"].(map[string]interface{})

		// One and only one of [object-templates, object-templates-raw] should be defined
		objectTemplatePresent := plcTmplDefSpec[utils.ObjectTemplates] != nil
		objectTemplateRawPresent := plcTmplDefSpec[utils.ObjectTemplatesRaw] != nil

		var objTemplates interface{}

		switch {
		case objectTemplatePresent && objectTemplateRawPresent:
			return nil, fmt.Errorf("[stripPolicy] found both %s and %s in policyTemplate", utils.ObjectTemplates, utils.ObjectTemplatesRaw)
		case !objectTemplatePresent && !objectTemplateRawPresent:
			return nil, fmt.Errorf("[stripPolicy] can't find %s or %s in policyTemplate", utils.ObjectTemplates, utils.ObjectTemplatesRaw)
		case objectTemplatePresent:
			objTemplates = plcTmplDefSpec[utils.ObjectTemplates]
		case objectTemplateRawPresent:
			stringTemplate := utils.StripObjectTemplatesRaw(plcTmplDefSpec[utils.ObjectTemplatesRaw].(string))

			var err error
			objTemplates, err = utils.StringToYaml(stringTemplate)
			if err != nil {
				return nil, fmt.Errorf("%s", utils.ConfigPlcFailRawMarshal)
			}
		default:
			return nil, fmt.Errorf("[stripPolicy] can't find %s or %s in policyTemplate", utils.ObjectTemplates, utils.ObjectTemplatesRaw)
		}

		for _, objTemplate := range objTemplates.([]interface{}) {
			complianceType := objTemplate.(map[string]interface{})["complianceType"]
			if complianceType == "mustnothave" {
				continue
			}
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
//
//	error
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
	spec.ViewUpdateIntervalSec = utils.GetMCVUpdateInterval(len(clusterGroupUpgrade.Status.Precaching.Status))
	err = r.createResourcesFromTemplates(ctx, spec, precacheDependenciesViewTemplates)
	if err != nil {
		return false, err
	}
	return true, nil
}

// getPrecacheImagePullSpec gets the precaching workload image pull spec.
// returns: image - pull spec string
//
//	error
func (r *ClusterGroupUpgradeReconciler) getPrecacheImagePullSpec(
	ctx context.Context,
	clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) (
	string, error) {

	preCachingConfigSpec, err := r.getPreCachingConfigSpec(ctx, clusterGroupUpgrade)
	if err != nil {
		r.Log.Error(err, "getPreCachingConfigSpec failed ")
		return "", err
	}

	preCacheImage := preCachingConfigSpec.Overrides.PreCacheImage
	if preCacheImage == "" {
		overrides, err := r.getOverrides(ctx, clusterGroupUpgrade)
		if err != nil {
			r.Log.Error(err, "getOverrides failed ")
			return "", err
		}
		preCacheImage = overrides["precache.image"]
		if preCacheImage == "" {
			preCacheImage = os.Getenv("PRECACHE_IMG")
			r.Log.Info("[getPrecacheImagePullSpec]", "workload image", preCacheImage)
			if preCacheImage == "" {
				return "", fmt.Errorf(
					"can't find pre-caching image pull spec in environment or overrides")
			}
		} else {
			r.Log.Info(getDeprecationMessage("precache.image"))
		}
	}
	return preCacheImage, nil
}

// parseSpaceRequired parses the spaceRequired string value (can be a floating-point value) and converts it to
// an integer string value representing the space required on the managed cluster in Gibibytes.
// Note that the parsed value is rounded up to the nearest integer via the math.Ceil function.
func parseSpaceRequired(spaceRequired string) (string, error) {
	var result int64
	var err error
	// Check if the spaceRequired is specified in base-2 (KiB, MiB, GiB, TiB, PiB) or base-10 (KB, MB, GB, TB, PB)
	if strings.Contains(spaceRequired, "i") {
		result, err = units.RAMInBytes(spaceRequired)
	} else {
		result, err = units.FromHumanSize(spaceRequired)
	}

	// Verify that no parsing errors occurred
	if err != nil {
		return "", err
	}
	if result < 0 {
		return "", fmt.Errorf("invalid value for spaceRequired, must be a number greater than 0")
	}

	// Convert to base-2 format in Gibibytes (result is rounded-up to the next integer value)
	resultGiB := int(math.Ceil(float64(result) / math.Pow(1024, 3)))
	return strconv.Itoa(resultGiB), nil
}

// getPrecacheSpecTemplateData: Converts precaching payload spec to template data
// returns: precacheTemplateData (softwareSpec)
//
//	error
func (r *ClusterGroupUpgradeReconciler) getPrecacheSpecTemplateData(
	clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) *templateData {

	rv := new(templateData)
	spec := clusterGroupUpgrade.Status.Precaching.Spec
	rv.PlatformImage = spec.PlatformImage
	rv.Operators.Indexes = spec.OperatorsIndexes
	rv.Operators.PackagesAndChannels = spec.OperatorsPackagesAndChannels
	rv.ExcludePrecachePatterns = spec.ExcludePrecachePatterns
	rv.AdditionalImages = spec.AdditionalImages
	rv.SpaceRequired = spec.SpaceRequired
	return rv
}

// includePreCachingConfigs retrieves the PreCachingConfigCR associated to the CGU
// returns: *ranv1alpha1.PrecachingSpec, error
func (r *ClusterGroupUpgradeReconciler) includePreCachingConfigs(
	ctx context.Context,
	clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, spec *ranv1alpha1.PrecachingSpec) (
	ranv1alpha1.PrecachingSpec, error) {

	rv := new(ranv1alpha1.PrecachingSpec)

	preCachingConfigSpec, err := r.getPreCachingConfigSpec(ctx, clusterGroupUpgrade)
	if err != nil {
		return *rv, err
	}

	// Support specifying overrides via ConfigMap (if PreCachingConfig fields are not specified)
	overrides, err := r.getOverrides(ctx, clusterGroupUpgrade)
	if err != nil {
		return *rv, err
	}

	// Check the OpenShift platform image
	platformImage := preCachingConfigSpec.Overrides.PlatformImage
	if platformImage == "" {
		overrideField := "platform.image"
		platformImage = overrides[overrideField]
		if platformImage == "" {
			platformImage = spec.PlatformImage
		} else {
			r.Log.Info(getDeprecationMessage(overrideField))
		}
	}
	rv.PlatformImage = platformImage

	// Define re-usable function to extract pre-caching config from the following sources (in order of precedence):
	// 1) PreCachingConfig CR,
	// 2) cluster-group-upgrade-overrides ConfigMap (log deprecation message if this source is used),
	// 3) spec.<field> (value derived by TALM)
	extractConfig := func(preCachingConfigCRValue []string, overrideField string, talmDerivedValue []string) []string {
		if len(preCachingConfigCRValue) == 0 {
			extractedOverrides := strings.Split(overrides[overrideField], "\n")
			if overrides[overrideField] == "" {
				return talmDerivedValue
			}
			r.Log.Info(getDeprecationMessage(overrideField))
			// Remove empty strings as a consequence of strings.Split
			var filteredResult []string
			for _, value := range extractedOverrides {
				if value != "" {
					filteredResult = append(filteredResult, strings.TrimSpace(value))
				}
			}
			return filteredResult
		}
		return preCachingConfigCRValue
	}

	// Extract the operator indexes
	rv.OperatorsIndexes = extractConfig(preCachingConfigSpec.Overrides.OperatorsIndexes,
		"operators.indexes", spec.OperatorsIndexes)

	// Extract the operator packages and channels
	rv.OperatorsPackagesAndChannels = extractConfig(preCachingConfigSpec.Overrides.OperatorsPackagesAndChannels,
		"operators.packagesAndChannels", spec.OperatorsPackagesAndChannels)

	// Extract the pre-cache exclusion patterns
	rv.ExcludePrecachePatterns = extractConfig(preCachingConfigSpec.ExcludePrecachePatterns,
		"excludePrecachePatterns", []string{})

	// Retrieve additional user images
	rv.AdditionalImages = preCachingConfigSpec.AdditionalImages

	// Extract the space required for pre-caching
	spaceRequired := preCachingConfigSpec.SpaceRequired
	if spaceRequired == "" {
		overrideField := "precache.spaceRequired"
		spaceRequired = overrides[overrideField]
		if spaceRequired == "" {
			spaceRequired = utils.SpaceRequiredForPrecache
		} else {
			r.Log.Info(getDeprecationMessage(overrideField))
		}
	}
	spaceRequired, err = parseSpaceRequired(spaceRequired)
	if err != nil {
		return *rv, err
	}
	rv.SpaceRequired = spaceRequired

	return *rv, nil
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
