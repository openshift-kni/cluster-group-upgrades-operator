package utils

import (
	"context"
	"fmt"
	"strings"

	policiesv1 "github.com/open-cluster-management/governance-policy-propagator/api/v1"
	ranv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// PolicyErr type
type PolicyErr struct {
	ObjName string
	ErrMsg  string
}

func (e *PolicyErr) Error() string {
	return fmt.Sprintf("%s: %s", e.ObjName, e.ErrMsg)
}

// GetChildPolicies gets the child policies for a list of clusters
func GetChildPolicies(ctx context.Context, c client.Client, clusters []string) ([]policiesv1.Policy, error) {
	var childPolicies []policiesv1.Policy

	for _, clusterName := range clusters {
		policies := &policiesv1.PolicyList{}
		if err := c.List(ctx, policies, client.InNamespace(clusterName)); err != nil {
			return nil, err
		}

		for _, policy := range policies.Items {
			labels := policy.GetLabels()
			if labels == nil {
				continue
			}
			// Skip if it's the child policy of a copied policy.
			if _, ok := labels["openshift-cluster-group-upgrades/clusterGroupUpgrade"]; ok {
				continue
			}
			// If we can find the child policy specific label, add the child policy name to the list.
			if _, ok := labels[ChildPolicyLabel]; ok {
				childPolicies = append(childPolicies, policy)
			}
		}
	}

	return childPolicies, nil
}

// DeletePolicies deletes Policies
func DeletePolicies(ctx context.Context, c client.Client, ns string, labels map[string]string) error {
	listOpts := []client.ListOption{
		client.InNamespace(ns),
		client.MatchingLabels(labels),
	}
	policiesList := &policiesv1.PolicyList{}
	if err := c.List(ctx, policiesList, listOpts...); err != nil {
		return err
	}

	for _, policy := range policiesList.Items {
		if err := c.Delete(ctx, &policy); err != nil {
			return err
		}
	}
	return nil
}

// DeletePlacementBindings deletes PlacementBindings
func DeletePlacementBindings(ctx context.Context, c client.Client, ns string, labels map[string]string) error {
	listOpts := []client.ListOption{
		client.InNamespace(ns),
		client.MatchingLabels(labels),
	}
	placementBindingsList := &policiesv1.PlacementBindingList{}
	if err := c.List(ctx, placementBindingsList, listOpts...); err != nil {
		return err
	}

	for _, placementBinding := range placementBindingsList.Items {
		if err := c.Delete(ctx, &placementBinding); err != nil {
			return err
		}
	}
	return nil
}

// DeletePlacementRules deletes PlacementRules
func DeletePlacementRules(ctx context.Context, c client.Client, ns string, labels map[string]string) error {
	listOpts := []client.ListOption{
		client.InNamespace(ns),
		client.MatchingLabels(labels),
	}
	placementRulesList := &unstructured.UnstructuredList{}
	placementRulesList.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "apps.open-cluster-management.io",
		Kind:    "PlacementRuleList",
		Version: "v1",
	})
	if err := c.List(ctx, placementRulesList, listOpts...); err != nil {
		return err
	}

	for _, policy := range placementRulesList.Items {
		if err := c.Delete(ctx, &policy); err != nil {
			return err
		}
	}
	return nil
}

// GetResourceName constructs composite names for policy objects
func GetResourceName(clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, initialString string) string {
	return strings.ToLower(clusterGroupUpgrade.Name + "-" + initialString)
}

// GetParentPolicyNameAndNamespace gets the parent policy name and namespace from a given child policy
// returns: []string       a two-element slice which the first element is policy namespace and the second one is policy name
func GetParentPolicyNameAndNamespace(childPolicyName string) []string {
	// The format of a child policy name is parent_policy_namespace.parent_policy_name.
	// Extract the parent policy name and namespace by splitting the child policy name into two substrings separated by "."
	// and we are safe to split with the separator "." as the namespace is disallowed to contain "."
	return strings.SplitN(childPolicyName, ".", 2)
}

// VerifyPolicyObjects validates the policy objects and return error if the policy is invalid
func VerifyPolicyObjects(policy *unstructured.Unstructured) error {
	policyName := policy.GetName()
	policySpec := policy.Object["spec"].(map[string]interface{})

	// Get the policy templates.
	policyTemplates := policySpec["policy-templates"].([]interface{})

	// Go through the policy policy-templates.
	for _, plcTmpl := range policyTemplates {
		// Make sure the objectDefinition of the policy template exists.
		if plcTmpl.(map[string]interface{})["objectDefinition"] == nil {
			return &PolicyErr{policyName, PlcMissTmplDef}
		}
		plcTmplDef := plcTmpl.(map[string]interface{})["objectDefinition"].(map[string]interface{})

		// Make sure the ConfigurationPolicy metadata exists.
		if plcTmplDef["metadata"] == nil {
			return &PolicyErr{policyName, PlcMissTmplDefMeta}
		}

		// Make sure the ConfigurationPolicy spec exists.
		if plcTmplDef["spec"] == nil {
			return &PolicyErr{policyName, PlcMissTmplDefSpec}
		}
		plcTmplDefSpec := plcTmplDef["spec"].(map[string]interface{})

		// Make sure the ConfigurationPolicy object-templates exists.
		if plcTmplDefSpec["object-templates"] == nil {
			return &PolicyErr{policyName, ConfigPlcMissObjTmpl}
		}
		configPlcTmpls := plcTmplDefSpec["object-templates"].([]interface{})

		// Go through the ConfigurationPolicy object-templates.
		for _, configPlcTmpl := range configPlcTmpls {
			// Make sure the objectDefinition of the ConfigurationPolicy object template exists.
			if configPlcTmpl.(map[string]interface{})["objectDefinition"] == nil {
				return &PolicyErr{policyName, ConfigPlcMissObjTmplDef}
			}
		}
	}
	return nil
}
