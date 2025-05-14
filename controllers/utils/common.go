package utils

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/clustergroupupgrades/v1alpha1"
	ranv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/clustergroupupgrades/v1alpha1"
	lcav1 "github.com/openshift-kni/lifecycle-agent/api/imagebasedupgrade/v1"
	corev1 "k8s.io/api/core/v1"
	rbac "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/rand"
	mwv1alpha1 "open-cluster-management.io/api/work/v1alpha1"
)

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

// Difference returns the elements in `a` that aren't in `b`.
func Difference(a, b []string) []string {
	mb := make(map[string]struct{}, len(b))
	for _, x := range b {
		mb[x] = struct{}{}
	}
	var diff []string
	for _, x := range a {
		if _, found := mb[x]; !found {
			diff = append(diff, x)
		}
	}
	return diff
}

func ObjectToJSON(obj runtime.Object) (string, error) {
	scheme := runtime.NewScheme()
	mwv1alpha1.AddToScheme(scheme)
	v1alpha1.AddToScheme(scheme)
	corev1.AddToScheme(scheme)
	rbac.AddToScheme(scheme)
	lcav1.AddToScheme(scheme)
	outUnstructured := &unstructured.Unstructured{}
	scheme.Convert(obj, outUnstructured, nil)
	json, err := outUnstructured.MarshalJSON()
	return string(json), err
}

func ObjectToByteArray(obj runtime.Object) ([]byte, error) {
	json, err := ObjectToJSON(obj)
	return []byte(json), err
}

// Contains check if str is in slice
// can be replaced by slices.Contains when golang is updated to 1.21
func Contains(s []string, str string) bool {
	for _, v := range s {
		if v == str {
			return true
		}
	}

	return false
}

// SortCGUListByPlanIndex orders the CGUs by the last action in the name of cgu
// with following order prep-upgrade-rollback-finalize-abort
func SortCGUListByPlanIndex(cguList *ranv1alpha1.ClusterGroupUpgradeList) {
	sort.Slice(cguList.Items, func(i, j int) bool {
		iSplitted := strings.Split(cguList.Items[i].GetName(), "-")
		jSplitted := strings.Split(cguList.Items[j].GetName(), "-")
		iLast := iSplitted[len(iSplitted)-1]
		jLast := jSplitted[len(jSplitted)-1]
		return iLast < jLast
	})
}
