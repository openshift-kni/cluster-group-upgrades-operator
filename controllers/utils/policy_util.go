package utils

import (
	"context"

	policiesv1 "github.com/open-cluster-management/governance-policy-propagator/api/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

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
