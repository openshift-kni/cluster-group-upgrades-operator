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

	actionsBeforeEnable := clusterGroupUpgrade.Spec.Actions.BeforeEnable
	// Add/delete cluster labels
	if actionsBeforeEnable.AddClusterLabels != nil || actionsBeforeEnable.DeleteClusterLabels != nil {
		clusters := utils.GetClustersListFromRemediationPlan(clusterGroupUpgrade)
		r.Log.Info("[actions]", "clusterList", clusters)
		labels := map[string]map[string]string{
			"add":    actionsBeforeEnable.AddClusterLabels,
			"delete": actionsBeforeEnable.DeleteClusterLabels,
		}

		for _, c := range clusters {
			err := r.manageClusterLabels(ctx, c, labels)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (r *ClusterGroupUpgradeReconciler) takeActionsAfterCompletion(
	ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, cluster string) error {

	clusterState := ranv1alpha1.ClusterState{
		Name: cluster, State: utils.ClusterRemediationComplete}
	clusterGroupUpgrade.Status.Clusters = append(clusterGroupUpgrade.Status.Clusters, clusterState)

	actionsAfterCompletion := clusterGroupUpgrade.Spec.Actions.AfterCompletion
	// Add/delete cluster labels
	if actionsAfterCompletion.AddClusterLabels != nil || actionsAfterCompletion.DeleteClusterLabels != nil {
		labels := map[string]map[string]string{
			"add":    actionsAfterCompletion.AddClusterLabels,
			"delete": actionsAfterCompletion.DeleteClusterLabels,
		}
		err := r.manageClusterLabels(ctx, cluster, labels)
		if err != nil {
			return err
		}
	}

	return nil
}

// manageClusterLabels adds/deletes the cluster labels for selected clusters
func (r *ClusterGroupUpgradeReconciler) manageClusterLabels(
	ctx context.Context, cluster string, labels map[string]map[string]string) error {

	managedCluster := &clusterv1.ManagedCluster{}
	if err := r.Get(ctx, types.NamespacedName{Name: cluster}, managedCluster); err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if err := r.addClusterLabels(ctx, managedCluster, labels["add"]); err != nil {
		return fmt.Errorf("fail to add labels for cluster %s: %v", managedCluster.Name, err)
	}
	if err := r.deleteClusterLabels(ctx, managedCluster, labels["delete"]); err != nil {
		return fmt.Errorf("fail to delete labels for cluster %s: %v", managedCluster.Name, err)
	}
	return nil
}

// Add cluster labels
func (r *ClusterGroupUpgradeReconciler) addClusterLabels(
	ctx context.Context, cluster *clusterv1.ManagedCluster, labels map[string]string) error {

	if len(labels) == 0 {
		return nil
	}

	currentLabels := cluster.GetLabels()
	if currentLabels == nil {
		currentLabels = make(map[string]string)
	}
	for key, value := range labels {
		currentLabels[key] = value
	}
	cluster.SetLabels(currentLabels)

	//nolint:revive
	if err := r.Update(ctx, cluster); err != nil {
		return err
	}
	return nil
}

// Delete cluster labels
func (r *ClusterGroupUpgradeReconciler) deleteClusterLabels(
	ctx context.Context, cluster *clusterv1.ManagedCluster, labels map[string]string) error {

	if len(labels) == 0 {
		return nil
	}

	currentLabels := cluster.GetLabels()
	if currentLabels == nil {
		return nil
	}
	for key, value := range labels {
		currentLabelValue, found := currentLabels[key]
		if found && currentLabelValue == value {
			delete(currentLabels, key)
		}
	}
	cluster.SetLabels(currentLabels)

	//nolint:revive
	if err := r.Update(ctx, cluster); err != nil {
		return err
	}
	return nil
}

func (r *ClusterGroupUpgradeReconciler) deleteResources(
	ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) error {

	labels := map[string]string{"openshift-cluster-group-upgrades/clusterGroupUpgrade": clusterGroupUpgrade.Name}
	err := utils.DeletePlacementRules(ctx, r.Client, clusterGroupUpgrade.Namespace, labels)
	if err != nil {
		return fmt.Errorf("failed to delete PlacementRules for CGU %s: %v", clusterGroupUpgrade.Name, err)
	}
	clusterGroupUpgrade.Status.PlacementRules = nil

	err = utils.DeletePlacementBindings(ctx, r.Client, clusterGroupUpgrade.Namespace, labels)
	if err != nil {
		return fmt.Errorf("failed to delete PlacementBindings for CGU %s: %v", clusterGroupUpgrade.Name, err)
	}
	clusterGroupUpgrade.Status.PlacementBindings = nil

	err = utils.DeletePolicies(ctx, r.Client, clusterGroupUpgrade.Namespace, labels)
	if err != nil {
		return fmt.Errorf("failed to delete Policies for CGU %s: %v", clusterGroupUpgrade.Name, err)
	}
	clusterGroupUpgrade.Status.CopiedPolicies = nil

	err = r.jobAndViewFinalCleanup(ctx, clusterGroupUpgrade)
	if err != nil {
		return fmt.Errorf("failed to delete precaching objects for CGU %s: %v", clusterGroupUpgrade.Name, err)
	}
	clusterGroupUpgrade.Status.SafeResourceNames = nil
	return nil
}
