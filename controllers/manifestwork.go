package controllers

import (
	"context"

	ranv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/clustergroupupgrades/v1alpha1"
	mwv1 "open-cluster-management.io/api/work/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (r *ClusterGroupUpgradeReconciler) validateManifestWorkTemplates(
	ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) (bool, []mwv1.Manifest, error) {
	return false, nil, nil
}

func (r *ClusterGroupUpgradeReconciler) isBatchCompleteForManifestWork(
	ctx context.Context, client client.Client, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) (bool, bool, error) {
	return false, false, nil
}

func (r *ClusterGroupUpgradeReconciler) createManifestWorkForCurrentBatch(
	ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, nextReconcile *ctrl.Result) error {
	return nil
}
