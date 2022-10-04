package controllers

import (
	"context"
	"fmt"

	ranv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/api/v1alpha1"
	utils "github.com/openshift-kni/cluster-group-upgrades-operator/controllers/utils"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Backup states
const (
	BackupStatePreparingToStart = "PreparingToStart"
	BackupStateStarting         = "Starting"
	BackupStateActive           = "Active"
	BackupStateSucceeded        = "Succeeded"
	BackupStateTimeout          = "BackupTimeout"
	BackupStateError            = "UnrecoverableError"
)

func (r *ClusterGroupUpgradeReconciler) reconcileBackup(
	ctx context.Context,
	clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) error {

	if clusterGroupUpgrade.Spec.Backup {
		// Backup is required
		if clusterGroupUpgrade.Status.Backup == nil {
			clusterGroupUpgrade.Status.Backup = &ranv1alpha1.BackupStatus{
				Status:   make(map[string]string),
				Clusters: []string{},
			}
			r.setBackupStartedCondition(clusterGroupUpgrade)
		}

		backupCondition := meta.FindStatusCondition(clusterGroupUpgrade.Status.Conditions, string(utils.ConditionTypes.BackupSuceeded))
		r.Log.Info("[reconcileBackup]", "FindStatusCondition", backupCondition)
		if backupCondition != nil && backupCondition.Status == metav1.ConditionTrue {
			// Backup is done
			return nil
		}

		// Backup is required and not marked as done
		return r.triggerBackup(ctx, clusterGroupUpgrade)

	}
	// No backup required
	return nil
}

func (r *ClusterGroupUpgradeReconciler) triggerBackup(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) error {

	clusterStates := make(map[string]string)
	var (
		clusters []string
		err      error
	)
	if len(clusterGroupUpgrade.Status.Backup.Clusters) != 0 {
		clusters = clusterGroupUpgrade.Status.Backup.Clusters
	} else {
		clusters, err = r.getSuccessfulClustersList(ctx, clusterGroupUpgrade, "")
		if err != nil {
			return fmt.Errorf("cannot obtain the CGU cluster list: %s", err)
		}
		clusterGroupUpgrade.Status.Backup.Clusters = clusters
	}
	r.Log.Info("[triggerBackup]", "Backup Clusters", clusters)

	for _, cluster := range clusters {
		var (
			currentState, nextState string
			err                     error
		)
		if len(clusterGroupUpgrade.Status.Backup.Status) == 0 {
			currentState = BackupStatePreparingToStart
		} else {
			currentState = clusterGroupUpgrade.Status.Backup.Status[cluster]
		}

		r.Log.Info("[triggerBackup]", "currentState", currentState, "cluster", cluster)
		switch currentState {
		// Initial State
		case BackupStatePreparingToStart:
			nextState, err = r.backupPreparing(ctx, clusterGroupUpgrade, cluster)
			if err != nil {
				return err
			}
		case BackupStateStarting:
			nextState, err = r.backupStarting(ctx, clusterGroupUpgrade, cluster)
			if err != nil {
				return err
			}
		// Final states that don't change for the life of the CR
		case BackupStateSucceeded, BackupStateTimeout, BackupStateError:
			nextState = currentState
			r.Log.Info("[triggerBackup]", "cluster", cluster, "final state", currentState)

		case BackupStateActive:
			nextState, err = r.backupActive(ctx, cluster)
			if err != nil {
				return err
			}

		default:
			return fmt.Errorf("[triggerBackup] unknown state %s", currentState)

		}
		clusterStates[cluster] = nextState
	}
	clusterGroupUpgrade.Status.Backup.Status = clusterStates
	r.checkAllBackupDone(clusterGroupUpgrade)
	return nil
}

// backupPreparing handles conditions in BackupStatePreparingToStart
// returns: error
func (r *ClusterGroupUpgradeReconciler) backupPreparing(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade,
	cluster string) (string, error) {

	currentState, nextState := BackupStatePreparingToStart, BackupStateStarting
	r.Log.Info("[triggerBackup]", "currentState", currentState, "condition", "entry",
		"cluster", cluster, "nextState", nextState)

	// delete managedclusterview objects if present
	err := r.deleteAllViews(ctx, cluster, backupView)
	if err != nil {
		return currentState, err
	}

	spec, err := r.getBackupJobTemplateData(clusterGroupUpgrade, cluster)
	if err != nil {
		return currentState, err
	}

	// delete namespace in the spoke with managedclusteraction
	err = r.createResourcesFromTemplates(ctx, spec, backupDeleteTemplates)
	if err != nil {
		return currentState, err
	}
	// log nextState, to be deleted
	r.Log.Info("[preparing]", "nextState returned", nextState)
	return nextState, nil
}

// backupStarting handles conditions in BackupStateStarting
// returns: error
func (r *ClusterGroupUpgradeReconciler) backupStarting(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade,
	cluster string) (string, error) {

	nextState, currentState := BackupStateStarting, BackupStateStarting
	var condition string

	condition, err := r.getStartingConditions(ctx, cluster, backupJobView[0].resourceName, backup)
	if err != nil {
		return currentState, err
	}
	r.Log.Info("[starting]", "starting started condition: ", condition)
	spec, err := r.getBackupJobTemplateData(clusterGroupUpgrade, cluster)
	if err != nil {
		return currentState, err
	}

	r.Log.Info("[starting]", "conditions: ", condition)
	switch condition {
	case DependenciesNotPresent:
		err := r.createResourcesFromTemplates(ctx, spec, backupDependenciesCreateTemplates)
		if err != nil {
			return currentState, err
		}

	case NoJobView, NoJobFoundOnSpoke:
		r.Log.Info("[triggerbackup]", "currentState", currentState, "condition", NoJobFoundOnSpoke,
			"cluster", cluster, "nextState", BackupStateStarting)
		err = r.deployWorkload(ctx, clusterGroupUpgrade, cluster, backup, backupJobView[0].resourceName, backupCreateTemplates)
		if err != nil {
			return currentState, err
		}

	case JobActive:
		nextState = BackupStateActive

	case JobSucceeded:
		nextState = BackupStateSucceeded

	case JobDeadline:
		nextState = BackupStateTimeout

	case JobBackoffLimitExceeded:
		nextState = BackupStateError

	default:
		return currentState, fmt.Errorf(
			"[starting] unknown condition %v in %s state", condition, currentState)
	}
	r.Log.Info("[starting]", "nextState returned", nextState)
	return nextState, nil
}

// backupActive handles conditions in BackupStateActive
// returns: error
func (r *ClusterGroupUpgradeReconciler) backupActive(ctx context.Context, cluster string) (string, error) {

	nextState, currentState := BackupStateActive, BackupStateActive
	// log nextState, to be deleted
	r.Log.Info("[active]", "active started", currentState)

	condition, err := r.getActiveConditions(ctx, cluster, backupJobView[0].resourceName)
	if err != nil {
		return nextState, err
	}

	switch condition {
	case JobActive:
		nextState = BackupStateActive

	case JobSucceeded:
		nextState = BackupStateSucceeded

	case JobDeadline:
		nextState = BackupStateTimeout

	case JobBackoffLimitExceeded:
		nextState = BackupStateError

	default:
		return currentState, fmt.Errorf("[triggerbackup] unknown condition %s in %s state",
			condition, currentState)
	}

	r.Log.Info("[active]", "nextState returned", nextState)
	return nextState, nil

}

// setBackupStartedCondition sets conditions of backup required
func (r *ClusterGroupUpgradeReconciler) setBackupStartedCondition(clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) {
	utils.SetStatusCondition(
		&clusterGroupUpgrade.Status.Conditions,
		utils.ConditionTypes.BackupSuceeded,
		utils.ConditionReasons.NotEnabled,
		metav1.ConditionFalse,
		"Backup will start when enable is true",
	)
}

// checkAllBackupDone handles alleviation of BackupDone==False condition
func (r *ClusterGroupUpgradeReconciler) checkAllBackupDone(
	clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) {

	// Counts for the various cluster states
	var failedBackupCount int = 0
	var progressingBackupCount int = 0
	var successfulBackupCount int = 0

	// Loop over all the clusters and take count of all their states
	for _, state := range clusterGroupUpgrade.Status.Backup.Status {
		switch state {
		case BackupStateSucceeded:
			successfulBackupCount++
		case BackupStateActive, BackupStateStarting, BackupStatePreparingToStart:
			progressingBackupCount++
		default:
			failedBackupCount++
		}
	}

	// Compare the total number of clusters to their status
	switch len(clusterGroupUpgrade.Status.Backup.Status) {
	// All clusters were successful
	case successfulBackupCount:
		utils.SetStatusCondition(
			&clusterGroupUpgrade.Status.Conditions,
			utils.ConditionTypes.BackupSuceeded,
			utils.ConditionReasons.Completed,
			metav1.ConditionTrue,
			"Backup is completed for all clusters",
		)
	// All clusters failed
	case failedBackupCount:
		utils.SetStatusCondition(
			&clusterGroupUpgrade.Status.Conditions,
			utils.ConditionTypes.BackupSuceeded,
			utils.ConditionReasons.Failed,
			metav1.ConditionFalse,
			"Backup failed for all clusters",
		)
	// All clusters are completed but some failed
	case (failedBackupCount + successfulBackupCount):
		utils.SetStatusCondition(
			&clusterGroupUpgrade.Status.Conditions,
			utils.ConditionTypes.BackupSuceeded,
			utils.ConditionReasons.PartiallyDone,
			metav1.ConditionTrue,
			fmt.Sprintf("Backup failed for %d clusters", failedBackupCount),
		)
	// Clusters are still in progress
	default:
		utils.SetStatusCondition(
			&clusterGroupUpgrade.Status.Conditions,
			utils.ConditionTypes.BackupSuceeded,
			utils.ConditionReasons.InProgress,
			metav1.ConditionFalse,
			fmt.Sprintf("Backup in progress for %d clusters", progressingBackupCount),
		)
	}

}
