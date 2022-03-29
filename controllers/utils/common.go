package utils

import (
	"fmt"

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
	if number1 <= number2 && number1 <= number3 {
		return number1
	} else if number2 <= number1 && number2 <= number3 {
		return number2
	} else {
		return number3
	}
}

func GetSafeResourceName(name string, maxLength, spareLength int) string {

	// if len(name) <= maxLength-spareLength {
	// 	return name
	// }

	// Borrowed from k8s.io/kubernetes/pkg/util/hash.DeepHashObject()
	// // TODO import the module properly
	// hasher := fnv.New32a()
	// hasher.Reset()
	// printer := spew.ConfigState{
	// 	Indent:         " ",
	// 	SortKeys:       true,
	// 	DisableMethods: true,
	// 	SpewKeys:       true,
	// }
	// printer.Fprintf(hasher, "%#v", object)
	// hash := rand.SafeEncodeString(fmt.Sprint(hasher.Sum32()))
	// var limit int
	// if len(name) > maxLength-len(hash)-spareLength {
	// 	limit = maxLength - len(hash) - spareLength
	// } else {
	// 	limit = len(name)
	// }
	// name := name[:limit-2] + "-" + hash

	const randomLength = 5
	maxGeneratedNameLength := maxLength - randomLength - 1
	var base string
	if len(name) > maxGeneratedNameLength {
		base = name[:maxGeneratedNameLength]
	} else {
		base = name
	}

	safeName := fmt.Sprintf("%s-%s", base, rand.String(randomLength))
	return safeName
}
