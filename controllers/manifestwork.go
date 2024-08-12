package controllers

import (
	"context"
	"fmt"

	utils "github.com/openshift-kni/cluster-group-upgrades-operator/controllers/utils"
	ranv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/clustergroupupgrades/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	mwv1 "open-cluster-management.io/api/work/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (r *ClusterGroupUpgradeReconciler) validateManifestWorkTemplates(
	ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) (manifests []mwv1.Manifest, missingTemplates []string, err error) {
	for _, name := range clusterGroupUpgrade.Spec.ManifestWorkTemplates {
		var ml []mwv1.Manifest
		ml, err = utils.GetManifestsFromTemplate(ctx, r.Client, types.NamespacedName{Name: name, Namespace: clusterGroupUpgrade.Namespace})
		if err != nil {
			if errors.IsNotFound(err) {
				err = nil
				missingTemplates = append(missingTemplates, name)
			} else {
				return
			}
		} else {
			manifests = append(manifests, ml...)
		}
	}
	return
}

func (r *ClusterGroupUpgradeReconciler) getNextManifestWorkForCluster(
	ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, clusterName string, startIndex int) (int, bool, error) {
	currentManifestWork, err := utils.GetManifestWorkForCluster(ctx, r.Client, clusterGroupUpgrade, startIndex, clusterName)
	if err == nil {
		completed, err := utils.IsManifestWorkCompleted(currentManifestWork)
		if completed {
			return startIndex + 1, false, nil
		}
		return startIndex, false, err
	} else if errors.IsNotFound(err) {
		// current mw is not created yet
		if startIndex > 0 {
			// clean up previous mw if exists
			previousManifestWork, err := utils.GetManifestWorkForCluster(ctx, r.Client, clusterGroupUpgrade, startIndex-1, clusterName)
			if err == nil {
				// Need to cleanup previous mw for this cluster
				err = r.Client.Delete(ctx, previousManifestWork)
				if client.IgnoreNotFound(err) != nil {
					return startIndex, false, err
				}
			}
			return startIndex, false, client.IgnoreNotFound(err)
		}
	}
	return startIndex, false, client.IgnoreNotFound(err)
}

func (r *ClusterGroupUpgradeReconciler) updateManifestWorkForCurrentBatch(
	ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) error {
	for clusterName, clusterProgress := range clusterGroupUpgrade.Status.Status.CurrentBatchRemediationProgress {
		if clusterProgress.State == ranv1alpha1.Completed {
			continue
		}
		currentIndex := *clusterProgress.ManifestWorkIndex
		if currentIndex > 0 {
			_, err := utils.GetManifestWorkForCluster(ctx, r.Client, clusterGroupUpgrade, currentIndex-1, clusterName)
			if err == nil {
				// Previous mw still there, can't create the new one yet
				continue
			} else if client.IgnoreNotFound(err) != nil {
				return err
			}
		}

		_, err := utils.GetManifestWorkForCluster(ctx, r.Client, clusterGroupUpgrade, currentIndex, clusterName)
		if errors.IsNotFound(err) {
			err = utils.CreateManifestWorkForCluster(ctx, r.Client, clusterGroupUpgrade, currentIndex, clusterName)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *ClusterGroupUpgradeReconciler) handleManifestWorkTimeoutForCluster(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade,
	clusterName string, clusterState *ranv1alpha1.ClusterState) error {
	clusterProgress := clusterGroupUpgrade.Status.Status.CurrentBatchRemediationProgress[clusterName]
	if clusterProgress.ManifestWorkIndex == nil {
		r.Log.Info("[handleManifestWorkTimeoutForCluster] Missing index for cluster", "clusterName", clusterName, "clusterProgress", clusterProgress)
		return nil
	}

	index := *clusterProgress.ManifestWorkIndex
	// Avoid panics because of index out of bound in edge cases
	if index < len(clusterGroupUpgrade.Spec.ManifestWorkTemplates) {
		clusterState.CurrentManifestWork = &ranv1alpha1.ManifestWorkStatus{Name: clusterGroupUpgrade.Spec.ManifestWorkTemplates[index]}
		currentManifestWork, err := utils.GetManifestWorkForCluster(ctx, r.Client, clusterGroupUpgrade, index, clusterName)
		if errors.IsNotFound(err) {
			r.Log.Error(err, "[handleManifestWorkTimeoutForCluster] Missing manifestwork", "cluster", clusterName)
			return nil
		}
		if err != nil {
			return err
		}
		clusterState.CurrentManifestWork.Status = currentManifestWork.Status.ResourceStatus
		// Trim manifest status conditionally as it's too verbose
		for i := range clusterState.CurrentManifestWork.Status.Manifests {
			mc := &clusterState.CurrentManifestWork.Status.Manifests[i]
			if utils.IsManifestConditionReady(mc) {
				mc.Conditions = nil
			} else {
				mc.StatusFeedbacks.Values = nil
			}
		}
	}
	return nil
}

func (r *ClusterGroupUpgradeReconciler) cleanupManifestWorkForCurrentBatch(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) error {
	if clusterGroupUpgrade.Status.Status.CurrentBatch < 1 {
		return nil
	}
	// CurrentBatch starts at 1, hence -1 for the array index of the current batch
	batchIndex := clusterGroupUpgrade.Status.Status.CurrentBatch - 1
	return utils.CleanupManifestWorkForBatch(ctx, r.Client, clusterGroupUpgrade, batchIndex)
}

func (r *ClusterGroupUpgradeReconciler) cleanupManifestWorkForPreviousBatch(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) error {
	if clusterGroupUpgrade.Status.Status.CurrentBatch < 2 {
		return nil
	}
	// CurrentBatch starts at 1, hence -2 for the array index of the previous batch
	batchIndex := clusterGroupUpgrade.Status.Status.CurrentBatch - 2
	return utils.CleanupManifestWorkForBatch(ctx, r.Client, clusterGroupUpgrade, batchIndex)
}

// finalCleanupManifestWork cleans up all previous batches
func (r *ClusterGroupUpgradeReconciler) finalCleanupManifestWork(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) error {
	var lastBatch int
	if clusterGroupUpgrade.Status.Status.CurrentBatch < 1 {
		// No current batch , clean up all batches just in case
		lastBatch = len(clusterGroupUpgrade.Status.RemediationPlan)
	} else {
		lastBatch = clusterGroupUpgrade.Status.Status.CurrentBatch - 1
	}
	for i := 0; i < lastBatch; i++ {
		r.Log.Info("Final cleanup for manifestworks from previous batches", "batchIndex", i)
		if err := utils.CleanupManifestWorkForBatch(ctx, r.Client, clusterGroupUpgrade, i); err != nil {
			return fmt.Errorf("failed to cleanup manifestworks for batch %d due to err: %v", i, err)
		}
	}
	return nil
}
