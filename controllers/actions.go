package controllers

import (
	"context"
	"fmt"

	utils "github.com/openshift-kni/cluster-group-upgrades-operator/controllers/utils"
	ranv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/clustergroupupgrades/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

// takeActionsBeforeEnable takes the required actions before starting upgrade
// returns: error/nil
func (r *ClusterGroupUpgradeReconciler) takeActionsBeforeEnable(
	ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) error {

	beforeEnable := clusterGroupUpgrade.Spec.Actions.BeforeEnable
	if beforeEnable != nil {
		clusters := utils.GetClustersListFromRemediationPlan(clusterGroupUpgrade)
		r.Log.Info("[actions]", "clusterList", clusters)

		var labels map[string]any      // nil
		var annotations map[string]any // nil
		if len(beforeEnable.AddClusterLabels) != 0 || len(beforeEnable.DeleteClusterLabels) != 0 || len(beforeEnable.RemoveClusterLabels) != 0 {
			labels = map[string]any{
				"add":    beforeEnable.AddClusterLabels,
				"delete": beforeEnable.DeleteClusterLabels, // deleteClusterLabels is deprecated
				"remove": beforeEnable.RemoveClusterLabels,
			}
		}
		if len(beforeEnable.AddClusterAnnotations) != 0 || len(beforeEnable.RemoveClusterAnnotations) != 0 {
			annotations = map[string]any{
				"add":    beforeEnable.AddClusterAnnotations,
				"remove": beforeEnable.RemoveClusterAnnotations,
			}
		}

		if labels != nil || annotations != nil {
			for _, c := range clusters {
				if err := r.manageClusterLabelsAndAnnotations(ctx, c, labels, annotations); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func (r *ClusterGroupUpgradeReconciler) takeActionsAfterCompletion(
	ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, cluster string) error {

	r.Log.Info("[takeActionsAfterCompletion]", "cluster", cluster, "cgu", clusterGroupUpgrade.Name)
	clusterState := ranv1alpha1.ClusterState{
		Name: cluster, State: utils.ClusterRemediationComplete}
	clusterGroupUpgrade.Status.Clusters = append(clusterGroupUpgrade.Status.Clusters, clusterState)

	afterCompletion := clusterGroupUpgrade.Spec.Actions.AfterCompletion
	if afterCompletion != nil {
		var labels map[string]any      // nil
		var annotations map[string]any // nil
		if len(afterCompletion.AddClusterLabels) != 0 || len(afterCompletion.DeleteClusterLabels) != 0 || len(afterCompletion.RemoveClusterLabels) != 0 {
			labels = map[string]interface{}{
				"add":    afterCompletion.AddClusterLabels,
				"delete": afterCompletion.DeleteClusterLabels, // deleteClusterLabels is deprecated
				"remove": afterCompletion.RemoveClusterLabels,
			}
		}
		if len(afterCompletion.AddClusterAnnotations) != 0 || len(afterCompletion.RemoveClusterAnnotations) != 0 {
			annotations = map[string]any{
				"add":    afterCompletion.AddClusterAnnotations,
				"remove": afterCompletion.RemoveClusterAnnotations,
			}
		}
		if labels != nil || annotations != nil {
			if err := r.manageClusterLabelsAndAnnotations(ctx, cluster, labels, annotations); err != nil {
				return err
			}
		}
	}

	return nil
}

func setClusterLabels(cluster *clusterv1.ManagedCluster, labels map[string]any) {
	currentLabels := cluster.GetLabels()
	if currentLabels == nil {
		currentLabels = make(map[string]string)
	}

	for name, value := range labels["add"].(map[string]string) {
		currentLabels[name] = value
	}
	// Deprecated
	for name := range labels["delete"].(map[string]string) {
		delete(currentLabels, name)
	}
	for _, name := range labels["remove"].([]string) {
		delete(currentLabels, name)
	}
	cluster.SetLabels(currentLabels)
}

func setClusterAnnotations(cluster *clusterv1.ManagedCluster, annotations map[string]any) {
	currentAnnotations := cluster.GetAnnotations()
	if currentAnnotations == nil {
		currentAnnotations = make(map[string]string)
	}

	for name, value := range annotations["add"].(map[string]string) {
		currentAnnotations[name] = value
	}
	for _, name := range annotations["remove"].([]string) {
		delete(currentAnnotations, name)
	}
	cluster.SetAnnotations(currentAnnotations)
}

func (r *ClusterGroupUpgradeReconciler) manageClusterLabelsAndAnnotations(ctx context.Context, cluster string, labels, annotations map[string]any) error {
	managedCluster := &clusterv1.ManagedCluster{}
	if err := r.Get(ctx, types.NamespacedName{Name: cluster}, managedCluster); err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}

	if labels != nil {
		setClusterLabels(managedCluster, labels)
	}
	if annotations != nil {
		setClusterAnnotations(managedCluster, annotations)
	}

	if err := r.Update(ctx, managedCluster); err != nil {
		return fmt.Errorf("failed to update labels/annotations for cluster: %s, err: %v", managedCluster.Name, err)
	}
	return nil
}

func (r *ClusterGroupUpgradeReconciler) deleteResources(
	ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) error {
	var targetNamespaces []string
	for _, policy := range clusterGroupUpgrade.Status.ManagedPoliciesForUpgrade {
		if _, ok := utils.FindStringInSlice(targetNamespaces, policy.Namespace); !ok {
			targetNamespaces = append(targetNamespaces, policy.Namespace)
		}
	}

	labels := map[string]string{
		"openshift-cluster-group-upgrades/clusterGroupUpgrade":          clusterGroupUpgrade.Name,
		"openshift-cluster-group-upgrades/clusterGroupUpgradeNamespace": clusterGroupUpgrade.Namespace,
	}
	for _, ns := range targetNamespaces {
		if err := utils.DeletePlacementRules(ctx, r.Client, ns, labels); err != nil {
			return fmt.Errorf("failed to delete PlacementRules for CGU %s: %v", clusterGroupUpgrade.Name, err)
		}
		clusterGroupUpgrade.Status.PlacementRules = nil

		if err := utils.DeletePlacementBindings(ctx, r.Client, ns, labels); err != nil {
			return fmt.Errorf("failed to delete PlacementBindings for CGU %s: %v", clusterGroupUpgrade.Name, err)
		}
		clusterGroupUpgrade.Status.PlacementBindings = nil
	}

	if err := r.cleanupManifestWorkForCurrentBatch(ctx, clusterGroupUpgrade); err != nil {
		return fmt.Errorf("failed to delete ManifestWork for CGU %s: %v", clusterGroupUpgrade.Name, err)
	}

	if err := r.jobAndViewFinalCleanup(ctx, clusterGroupUpgrade); err != nil {
		return fmt.Errorf("failed to delete precaching objects for CGU %s: %v", clusterGroupUpgrade.Name, err)
	}
	clusterGroupUpgrade.Status.SafeResourceNames = nil
	return nil
}
