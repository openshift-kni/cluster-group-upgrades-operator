package controllers

import (
	"errors"

	ranv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/api/v1alpha1"
	utils "github.com/openshift-kni/cluster-group-upgrades-operator/controllers/utils"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func (r *ClusterGroupUpgradeReconciler) extractOpenshiftImagePlatformFromPolicies(
	policies []*unstructured.Unstructured) (string, error) {

	var upstream string
	var channel string
	var version string
	var image string

	for _, policy := range policies {
		objects, err := r.stripPolicy(policy.Object)
		if err != nil {
			return "", err
		}
		for _, object := range objects {
			kind := object["kind"]
			switch kind {
			case utils.ClusterVersionGroupVersionKind().Kind:
				cvSpec := object["spec"].(map[string]interface{})
				desiredUpdate, found := cvSpec["desiredUpdate"]
				if !found {
					continue
				}
				desiredUpdateImage, found := desiredUpdate.(map[string]interface{})["image"]
				if found && desiredUpdateImage == "" {
					return "", errors.New("platform image defined but value is missing")
				}

				upgradeDefinedMultipleTimes := false

				if object["spec"] != "" {

					if object["spec"].(map[string]interface{})["upstream"] != nil {
						nextUpstream := object["spec"].(map[string]interface{})["upstream"].(string)
						if upstream == "" {
							upstream = nextUpstream
						} else if upstream != nextUpstream {
							upgradeDefinedMultipleTimes = true
						}
					}

					if object["spec"].(map[string]interface{})["channel"] != nil {
						nextChannel := object["spec"].(map[string]interface{})["channel"].(string)
						if channel == "" {
							channel = nextChannel
						} else if channel != nextChannel {
							upgradeDefinedMultipleTimes = true
						}
					}

					if object["spec"].(map[string]interface{})["desiredUpdate"] != nil {
						if object["spec"].(map[string]interface{})["desiredUpdate"].(map[string]interface{})["version"] != nil {
							nextVersion := object["spec"].(map[string]interface{})["desiredUpdate"].(map[string]interface{})["version"].(string)
							if version == "" {
								version = nextVersion
							} else if version != nextVersion {
								upgradeDefinedMultipleTimes = true
							}
						}
					}
				}

				if upgradeDefinedMultipleTimes {
					return "", errors.New("platform image defined more then once with conflicting values")
				}

				image, err = r.getImageForVersionFromUpdateGraph(upstream, channel, version)
				if err != nil {
					return "", err
				}
				if image == "" {
					return "", errors.New("unable to find platform image for specified upstream, channel, and version")
				}
			default:
				continue
			}
		}
	}
	return image, nil
}

func (r *ClusterGroupUpgradeReconciler) validateOpenshiftUpgradeVersion(
	clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, policies []*unstructured.Unstructured) error {

	_, err := r.extractOpenshiftImagePlatformFromPolicies(policies)
	if err != nil {
		utils.SetStatusCondition(
			&clusterGroupUpgrade.Status.Conditions,
			utils.ConditionTypes.Validated,
			utils.ConditionReasons.InvalidPlatformImage,
			metav1.ConditionFalse,
			err.Error(),
		)
		return err
	}

	return nil
}
