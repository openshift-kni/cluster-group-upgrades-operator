package utils

import (
	"fmt"
	"regexp"
	"unicode/utf8"

	ranv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/clustergroupupgrades/v1alpha1"
	"k8s.io/apimachinery/pkg/util/rand"
)

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

// FindStringInSlice checks if a given string is in the slice, and returns true along with its index if it's found
func FindStringInSlice(a []string, s string) (int, bool) {
	for i, e := range a {
		if e == s {
			return i, true
		}
	}
	return -1, false
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

// ContainsTemplates checks if the string contains some templatized parts
func ContainsTemplates(s string) bool {
	// This expression matches all template types
	regexpAllTemplates := regexp.MustCompile(`{{.*}}`)

	return regexpAllTemplates.MatchString(s)
}
