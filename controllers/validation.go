package controllers

import (
	"errors"
	"fmt"

	"github.com/openshift-kni/cluster-group-upgrades-operator/controllers/utils"
	ranv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/clustergroupupgrades/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type ocpVersionInfo struct {
	upstream              string
	channel               string
	version               string
	image                 string
	clusterVersionCRFound bool
}

func extractOCPVersionInfoFromPolicies(policies []*unstructured.Unstructured) (ocpVersionInfo, error) {

	result := ocpVersionInfo{}

	// validate ClusterVersionGroupVersionKind and keep track to upstream, channel, version, image
	for _, policy := range policies {
		objects, err := stripPolicy(policy.Object)
		if err != nil {
			return result, err
		}
		for _, object := range objects {
			kind := object["kind"]
			switch kind {
			case utils.ClusterVersionGroupVersionKind().Kind:
				_, foundSpec := object["spec"]
				if !foundSpec || object["spec"] == nil {
					continue
				}

				if object["spec"].(map[string]interface{})["upstream"] != nil {
					nextUpstream := object["spec"].(map[string]interface{})["upstream"].(string)

					if nextUpstream == utils.Placeholder {
						return result, errors.New("templating cluster version fields not supported")
					}

					if result.upstream == "" {
						result.upstream = nextUpstream
					} else if result.upstream != nextUpstream {
						return result, errors.New("platform image defined more then once with conflicting upstream values")
					}
				}

				if object["spec"].(map[string]interface{})["channel"] != nil {
					nextChannel := object["spec"].(map[string]interface{})["channel"].(string)

					if nextChannel == utils.Placeholder {
						return result, errors.New("templating cluster version fields not supported")
					}

					if result.channel == "" {
						result.channel = nextChannel
					} else if result.channel != nextChannel {
						return result, errors.New("platform image defined more then once with conflicting channel values")
					}
				}

				if object["spec"].(map[string]interface{})["desiredUpdate"] != nil {
					if object["spec"].(map[string]interface{})["desiredUpdate"].(map[string]interface{})["version"] != nil {
						nextVersion := object["spec"].(map[string]interface{})["desiredUpdate"].(map[string]interface{})["version"].(string)

						if nextVersion == utils.Placeholder {
							return result, errors.New("templating cluster version fields not supported")
						}

						if result.version == "" {
							result.version = nextVersion
						} else if result.version != nextVersion {
							return result, errors.New("platform image defined more then once with conflicting version values")
						}
					}
					if object["spec"].(map[string]interface{})["desiredUpdate"].(map[string]interface{})["image"] != nil {
						nextImage := object["spec"].(map[string]interface{})["desiredUpdate"].(map[string]interface{})["image"].(string)

						if nextImage == utils.Placeholder {
							return result, errors.New("templating cluster version fields not supported")
						}

						if result.image == "" {
							result.image = nextImage
						} else if result.image != nextImage {
							return result, errors.New("platform image defined more then once with conflicting image values")
						}
					}
				}

				result.clusterVersionCRFound = true
			default:
				continue
			}
		}
	}
	return result, nil
}

// extractOCPImageFromPolicies validates that there's ClusterVersion policy, validates the content of ClusterVersion and extracts Image if needed
func (r *ClusterGroupUpgradeReconciler) extractOCPImageFromPolicies(
	policies []*unstructured.Unstructured) (string, error) {

	versionInfo, err := extractOCPVersionInfoFromPolicies(policies)

	if err != nil {
		return "", err
	}

	if !versionInfo.clusterVersionCRFound {
		return "", nil
	}

	// return early if policy is valid and user provided .Spec.DesiredUpdate.image in ClusterVersion
	if versionInfo.image != "" {
		return versionInfo.image, nil
	}

	image, err := r.getImageForVersionFromUpdateGraph(versionInfo.upstream, versionInfo.channel, versionInfo.version)
	if err != nil {
		return "", err
	}
	if image == "" {
		return "", errors.New("unable to find platform image for specified upstream, channel, and version")
	}

	return image, nil
}

func (r *ClusterGroupUpgradeReconciler) validateOpenshiftUpgradeVersion(
	clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, policies []*unstructured.Unstructured) error {

	versionInfo, err := extractOCPVersionInfoFromPolicies(policies)

	if err == nil {
		if !versionInfo.clusterVersionCRFound || versionInfo.image != "" {
			return nil
		}

		// Using a few temporary variables here makes the if below much more readable
		versionInfoContainsEmptyString := versionInfo.upstream == "" || versionInfo.channel == "" || versionInfo.version == ""
		versionInfoContainsTemplate := utils.ContainsTemplates(versionInfo.upstream) || utils.ContainsTemplates(versionInfo.channel) || utils.ContainsTemplates(versionInfo.version)
		versionInfoContainsPlaceholder := versionInfo.upstream == utils.Placeholder || versionInfo.channel == utils.Placeholder || versionInfo.version == utils.Placeholder

		// Check for all the required parameters needed to make the update graph HTTP call and retrieve the image
		// nolint: gocritic
		if versionInfoContainsEmptyString {
			err = errors.New("policy with ClusterVersion must have upstream, channel, and version when image is not provided")
		} else if versionInfoContainsTemplate || versionInfoContainsPlaceholder {
			if clusterGroupUpgrade.Spec.PreCaching {
				// return error if the fields contain templates
				err = errors.New("templatized ClusterVersion fields not supported with precaching")
			}
		} else {
			_, err = r.getImageForVersionFromUpdateGraph(versionInfo.upstream, versionInfo.channel, versionInfo.version)
		}
	}

	if err != nil {
		utils.SetStatusCondition(
			&clusterGroupUpgrade.Status.Conditions,
			utils.ConditionTypes.Validated,
			utils.ConditionReasons.InvalidPlatformImage,
			metav1.ConditionFalse,
			err.Error(),
		)
	}

	return err
}

func indexOf(element ranv1alpha1.ManagedPolicyForUpgrade, data []ranv1alpha1.ManagedPolicyForUpgrade) (int, error) {
	for k, v := range data {
		if element.Name == v.Name && element.Namespace == v.Namespace {
			return k, nil
		}
	}
	return -1, errors.New("element not found in data")
}

func (r *ClusterGroupUpgradeReconciler) validatePoliciesDependenciesOrder(
	clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, managedPoliciesForUpgrade []*unstructured.Unstructured) error {
	for _, managedPolicy := range managedPoliciesForUpgrade {
		managedPolicyIndex, _ := indexOf(
			ranv1alpha1.ManagedPolicyForUpgrade{Name: managedPolicy.GetName(), Namespace: managedPolicy.GetNamespace()},
			clusterGroupUpgrade.Status.ManagedPoliciesForUpgrade)
		specObject := managedPolicy.Object["spec"].(map[string]interface{})
		dependencies := specObject["dependencies"]
		if dependencies == nil {
			continue
		}
		dependenciesArr := dependencies.([]interface{})

		for _, d := range dependenciesArr {
			name := d.(map[string]interface{})["name"]
			namespace := d.(map[string]interface{})["namespace"]
			dependecyIndex, err := indexOf(
				ranv1alpha1.ManagedPolicyForUpgrade{Name: name.(string), Namespace: namespace.(string)},
				clusterGroupUpgrade.Status.ManagedPoliciesForUpgrade)
			if err == nil && dependecyIndex > managedPolicyIndex {
				utils.SetStatusCondition(
					&clusterGroupUpgrade.Status.Conditions,
					utils.ConditionTypes.Validated,
					utils.ConditionReasons.UnresolvableDenpendency,
					metav1.ConditionFalse,
					fmt.Sprintf("Managed Policy %s depends on %s, which is to be remediated later", managedPolicy.GetName(), name),
				)
				return errors.New("invalid dependency order")
			}
		}
	}
	return nil
}
