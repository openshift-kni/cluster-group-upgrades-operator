package utils

import (
	"context"
	"strings"

	policiesv1 "github.com/open-cluster-management/governance-policy-propagator/api/v1"
	ranv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

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

func GetResourceName(clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, initialString string) string {
	return strings.ToLower(clusterGroupUpgrade.Name + "-" + initialString)
}
