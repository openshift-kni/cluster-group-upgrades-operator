package controllers

import (
	"context"
	"fmt"

	ranv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/api/v1alpha1"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Backup states
const (
	BackupStatePreparingToStart = "PreparingToStart"
	BackupStateStarting         = "Starting"
	BackupStateActive           = "Active"
	BackupStateSucceeded        = "Succeeded"
	BackupStateDone             = "BackupDone"
	BackupStateTimeout          = "BackupTimeout"
	BackupStateError            = "UnrecoverableError"
)

func (r *ClusterGroupUpgradeReconciler) reconcileBackup(
	ctx context.Context,
	clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) error {

	if clusterGroupUpgrade.Spec.Backup {
		// Backup is required

		clusters, err := r.getAllClustersForUpgrade(ctx, clusterGroupUpgrade)
		if err != nil {
			return fmt.Errorf("cannot obtain the CGU cluster list: %s", err)
		}
		if clusterGroupUpgrade.Status.Backup == nil {
			clusterGroupUpgrade.Status.Backup = &ranv1alpha1.BackupStatus{
				Status:   make(map[string]string),
				Clusters: clusters,
			}
		}

		doneCondition := meta.FindStatusCondition(clusterGroupUpgrade.Status.Conditions, BackupStateDone)
		r.Log.Info("[reconcileBackup]", "FindStatusCondition  BackupDone", doneCondition)
		if doneCondition != nil && doneCondition.Status == metav1.ConditionTrue {
			// Backup is done
			return nil
		}
		// Backup is required and not marked as done
		return r.triggerBackup(ctx, clusterGroupUpgrade.Status.Backup.Clusters, clusterGroupUpgrade)
	}
	// No backup required
	return nil
}

func (r *ClusterGroupUpgradeReconciler) triggerBackup(ctx context.Context, clusters []string, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) error {

	r.setBackupRequired(clusterGroupUpgrade)
	r.Log.Info("[triggerBackup]", "triggerbackup function", clusters)

	clusterStates := make(map[string]string)
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

	spec := r.getBackupJobTemplateData(clusterGroupUpgrade, cluster)
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
	spec := r.getBackupJobTemplateData(clusterGroupUpgrade, cluster)
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
		nextState = PrecacheStateActive

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

// setBackupRequired sets conditions of backup required
func (r *ClusterGroupUpgradeReconciler) setBackupRequired(clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) {

	meta.SetStatusCondition(
		&clusterGroupUpgrade.Status.Conditions, metav1.Condition{
			Type:    BackupStateDone,
			Status:  metav1.ConditionFalse,
			Reason:  "BackupNotDone",
			Message: "Backup is required and not done"})
}

// checkAllBackupDone handles alleviation of BackupDone==False condition
func (r *ClusterGroupUpgradeReconciler) checkAllBackupDone(
	clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) {
	// Handle completion
	if func() bool {
		for _, state := range clusterGroupUpgrade.Status.Backup.Status {
			if state != BackupStateSucceeded {
				return false
			}
		}
		return true
	}() {
		meta.SetStatusCondition(
			&clusterGroupUpgrade.Status.Conditions, metav1.Condition{
				Type:    BackupStateDone,
				Status:  metav1.ConditionTrue,
				Reason:  "BackupCompleted",
				Message: "Backup is completed"})
	}
}
