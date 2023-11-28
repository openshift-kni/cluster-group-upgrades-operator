package utils

import (
	"fmt"
	"regexp"

	ranv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/clustergroupupgrades/v1alpha1"
	"k8s.io/apimachinery/pkg/util/rand"
)

// GetManagedPolicyForUpgradeByIndex return the policy from the list of managedPoliciesForUpgrade
// by the index.
func GetManagedPolicyForUpgradeByIndex(
	policyIndex int, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) *ranv1alpha1.ManagedPolicyForUpgrade {
	for index, crtPolicy := range clusterGroupUpgrade.Status.ManagedPoliciesForUpgrade {
		if index == policyIndex {
			return &crtPolicy
		}
	}
	return nil
}

// GetMinOf3 return the minimum of 3 numbers.
func GetMinOf3(number1, number2, number3 int) int {
	if number1 <= number2 && number1 <= number3 {
		return number1
	} else if number2 <= number1 && number2 <= number3 {
		return number2
	} else {
		return number3
	}
}

// GetSafeResourceName returns the safename if already allocated in the map and creates a new one if not
func GetSafeResourceName(name string, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, maxLength,
	spareLength int) (string, error) {

	var err error
	if clusterGroupUpgrade.Status.SafeResourceNames == nil {
		clusterGroupUpgrade.Status.SafeResourceNames = make(map[string]string)
	}
	safeName, ok := clusterGroupUpgrade.Status.SafeResourceNames[name]
	if !ok {
		safeName, err =
			NewSafeResourceName(name, clusterGroupUpgrade.GetAnnotations()[NameSuffixAnnotation],
				maxLength, spareLength)
		if err != nil {
			return "", err
		}
		clusterGroupUpgrade.Status.SafeResourceNames[name] = safeName
	}
	return safeName, nil
}

// NewSafeResourceName creates a safe name to use with random suffix and possible truncation based on limits passed in
func NewSafeResourceName(name, suffix string, maxLength, spareLength int) (string, error) {
	if suffix == "" {
		suffix = rand.String(RandomNameSuffixLength)
	}
	suffixLength := len(suffix)
	maxGeneratedNameLength := maxLength - suffixLength - spareLength - 1
	var base string
	if len(name) > maxGeneratedNameLength {
		base = name[:maxGeneratedNameLength]
	} else {
		base = name
	}

	// Make sure base ends in '-' or an alphanumerical character.
	for !regexp.MustCompile(`^[a-zA-Z0-9-]*$`).MatchString(base[len(base)-1:]) {
		base = base[:len(base)-1]
	}

	newSafeResourceName := fmt.Sprintf("%s-%s", base, suffix)
	// Validate the newSafeResourceName matches the required regex as per
	// https://kubernetes.io/docs/concepts/overview/working-with-objects/names/
	match, err := regexp.MatchString(RFC1123MetadataRegex, newSafeResourceName)
	if err != nil {
		return "", err
	}
	if !match {
		return "", fmt.Errorf("Safe resource name \"%s\" for \"%s\" does not match regex '%s'",
			newSafeResourceName, name, RFC1123MetadataRegex)
	}

	return newSafeResourceName, nil
}
