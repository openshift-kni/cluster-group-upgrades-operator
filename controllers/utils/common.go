package utils

import (
	"fmt"
	"regexp"
	"unicode/utf8"

	ranv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/api/v1alpha1"
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
	// nolint: gocritic
	if number1 <= number2 && number1 <= number3 {
		return number1
	} else if number2 <= number1 && number2 <= number3 {
		return number2
	} else {
		return number3
	}
}

// GetSafeResourceName returns the safename if already allocated in the map and creates a new one if not
func GetSafeResourceName(name, namespace string, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, maxLength int) string {
	if clusterGroupUpgrade.Status.SafeResourceNames == nil {
		clusterGroupUpgrade.Status.SafeResourceNames = make(map[string]string)
	}
	safeName, ok := clusterGroupUpgrade.Status.SafeResourceNames[PrefixNameWithNamespace(namespace, name)]

	if !ok {
		safeName = NewSafeResourceName(name, namespace, clusterGroupUpgrade.GetAnnotations()[NameSuffixAnnotation], maxLength)
		clusterGroupUpgrade.Status.SafeResourceNames[PrefixNameWithNamespace(namespace, name)] = safeName
	}
	return safeName
}

const (
	finalDashLength = 1
)

// NewSafeResourceName creates a safe name to use with random suffix and possible truncation based on limits passed in
func NewSafeResourceName(name, namespace, suffix string, maxLength int) (safename string) {
	if suffix == "" {
		suffix = rand.String(RandomNameSuffixLength)
	}
	suffixLength := utf8.RuneCountInString(suffix)
	maxGeneratedNameLength := maxLength - suffixLength - utf8.RuneCountInString(namespace) - finalDashLength
	var base string
	if len(name) > maxGeneratedNameLength {
		base = name[:maxGeneratedNameLength]
	} else {
		base = name
	}

	// Make sure base ends in '-' or an alphanumerical character.
	for !regexp.MustCompile(`^[a-zA-Z0-9-]*$`).MatchString(base[utf8.RuneCountInString(base)-1:]) {
		base = base[:utf8.RuneCountInString(base)-1]
	}

	// The newSafeResourceName should match regex
	// `[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*` as per
	// https://kubernetes.io/docs/concepts/overview/working-with-objects/names/
	return fmt.Sprintf("%s-%s", base, suffix)
}

// PrefixNameWithNamespace Prefixes the passed name with the passed namespace. Use '/' as a separator
func PrefixNameWithNamespace(namespace, name string) string {
	return namespace + "/" + name
}
