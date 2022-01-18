package controllers

import (
	"context"
	"fmt"

	ranv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/api/v1alpha1"
	utils "github.com/openshift-kni/cluster-group-upgrades-operator/controllers/utils"
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
		clusters, err := r.getAllClustersForUpgrade(ctx, clusterGroupUpgrade)
		if err != nil {
			return err
		}
		labels := map[string]map[string]string{
			"add":    actionsBeforeEnable.AddClusterLabels,
			"delete": actionsBeforeEnable.DeleteClusterLabels,
		}

		if err := r.manageClusterLabels(ctx, clusters, labels); err != nil {
			return err
		}
	}

	return nil
}

// takeActionsAfterCompletion takes the required actions after upgrade is completed
// returns: error/nil
func (r *ClusterGroupUpgradeReconciler) takeActionsAfterCompletion(
	ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) error {

	actionsAfterCompletion := clusterGroupUpgrade.Spec.Actions.AfterCompletion
	// Add/delete cluster labels
	if actionsAfterCompletion.AddClusterLabels != nil || actionsAfterCompletion.DeleteClusterLabels != nil {
		clusters, err := r.getAllClustersForUpgrade(ctx, clusterGroupUpgrade)
		if err != nil {
			return err
		}
		labels := map[string]map[string]string{
			"add":    actionsAfterCompletion.AddClusterLabels,
			"delete": actionsAfterCompletion.DeleteClusterLabels,
		}

		if err := r.manageClusterLabels(ctx, clusters, labels); err != nil {
			return err
		}
	}

	// Cleanup resources
	if *actionsAfterCompletion.DeleteObjects {
		err := r.deleteResources(ctx, clusterGroupUpgrade)
		if err != nil {
			return err
		}
	}

	return nil
}

// manageClusterLabels adds/deletes the cluster labels for selected clusters
func (r *ClusterGroupUpgradeReconciler) manageClusterLabels(
	ctx context.Context, clusters []string, labels map[string]map[string]string) error {

	for _, c := range clusters {
		managedCluster := &clusterv1.ManagedCluster{}
		if err := r.Get(ctx, types.NamespacedName{Name: c}, managedCluster); err != nil {
			if errors.IsNotFound(err) {
				continue
			}
			return err
		}
		if err := r.addClusterLabels(ctx, managedCluster, labels["add"]); err != nil {
			return fmt.Errorf("Fail to add labels for cluster %s: %v", managedCluster.Name, err)
		}
		if err := r.deleteClusterLabels(ctx, managedCluster, labels["delete"]); err != nil {
			return fmt.Errorf("Fail to delete labels for cluster %s: %v", managedCluster.Name, err)
		}
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
		return fmt.Errorf("Fail to delete PlacementRules for cgu %s: %v", clusterGroupUpgrade.Name, err)
	}
	err = utils.DeletePlacementBindings(ctx, r.Client, clusterGroupUpgrade.Namespace, labels)
	if err != nil {
		return fmt.Errorf("Fail to delete PlacementBindings for cgu %s: %v", clusterGroupUpgrade.Name, err)
	}
	err = utils.DeletePolicies(ctx, r.Client, clusterGroupUpgrade.Namespace, labels)
	if err != nil {
		return fmt.Errorf("Fail to delete Policies for cgu %s: %v", clusterGroupUpgrade.Name, err)
	}

	return nil
}
