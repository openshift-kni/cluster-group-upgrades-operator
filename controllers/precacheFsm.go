/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"fmt"

	ranv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/api/v1alpha1"
	"github.com/openshift-kni/cluster-group-upgrades-operator/controllers/utils"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Pre-cache states
const (
	PrecacheStateNotStarted       = "NotStarted"
	PrecacheStatePreparingToStart = "PreparingToStart"
	PrecacheStateStarting         = "Starting"
	PrecacheStateActive           = "Active"
	PrecacheStateSucceeded        = "Succeeded"
	PrecacheStateTimeout          = "PrecacheTimeout"
	PrecacheStateError            = "UnrecoverableError"
)

// Pre-cache resources conditions
const (
	NoNsView                        = "NoNsView"
	NoNsFoundOnSpoke                = "NoNsFoundOnSpoke"
	NsFoundOnSpoke                  = "NsFoundOnSpoke"
	NoJobView                       = "NoJobView"
	NoJobFoundOnSpoke               = "NoJobFoundOnSpoke"
	JobViewExists                   = "JobViewExists"
	DependenciesViewNotPresent      = "DependenciesViewNotPresent"
	DependenciesNotPresent          = "DependenciesNotPresent"
	PrecacheJobDeadline             = "PrecacheJobDeadline"
	PrecacheJobSucceeded            = "PrecacheJobSucceeded"
	PrecacheJobActive               = "PrecacheJobActive"
	PrecacheJobBackoffLimitExceeded = "PrecacheJobBackoffLimitExceeded"
	PrecacheUnforeseenCondition     = "UnforeseenCondition"
)

// precachingFsm implements the precaching state machine
// returns: error
func (r *ClusterGroupUpgradeReconciler) precachingFsm(ctx context.Context,
	clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) error {

	r.setPrecachingRequired(clusterGroupUpgrade)
	var clusters []string
	var err error
	if len(clusterGroupUpgrade.Status.Precaching.Clusters) != 0 {
		clusters = clusterGroupUpgrade.Status.Precaching.Clusters
	} else {
		clusters, err = r.getAllClustersForUpgrade(ctx, clusterGroupUpgrade)
		if err != nil {
			return fmt.Errorf("cannot obtain the CGU cluster list: %s", err)
		}
		clusterGroupUpgrade.Status.Precaching.Clusters = clusters
	}

	clusterStates := make(map[string]string)
	for _, cluster := range clusters {
		var currentState string
		if len(clusterGroupUpgrade.Status.Precaching.Status) == 0 {
			currentState = PrecacheStateNotStarted
		} else {
			currentState = clusterGroupUpgrade.Status.Precaching.Status[cluster]
		}
		var nextState string
		r.Log.Info("[precachingFsm]", "currentState", currentState, "cluster", cluster)
		switch currentState {
		// Initial State
		case PrecacheStateNotStarted:
			nextState, err = r.handleNotStarted(ctx, clusterGroupUpgrade, cluster)
			if err != nil {
				return err
			}
		case PrecacheStatePreparingToStart:
			nextState, err = r.handlePreparing(ctx, clusterGroupUpgrade, cluster)
			if err != nil {
				return err
			}
		case PrecacheStateStarting:
			nextState, err = r.handleStarting(ctx, clusterGroupUpgrade, cluster)
			if err != nil {
				return err
			}
		// Final states that don't change for the life of the CR
		case PrecacheStateSucceeded, PrecacheStateTimeout, PrecacheStateError:
			nextState = currentState
			r.Log.Info("[precachingFsm]", "cluster", cluster, "final state", currentState)

		case PrecacheStateActive:
			nextState, err = r.handleActive(ctx, cluster)
			if err != nil {
				return err
			}

		default:
			return fmt.Errorf("[precachingFsm] unknown state %s", currentState)

		}

		clusterStates[cluster] = nextState
		r.Log.Info("[precachingFsm]", "previousState", currentState, "nextState", nextState, "cluster", cluster)

	}
	clusterGroupUpgrade.Status.Precaching.Status = make(map[string]string)
	clusterGroupUpgrade.Status.Precaching.Status = clusterStates
	r.checkAllPrecachingDone(clusterGroupUpgrade)
	return nil
}

// handleNotStarted handles conditions in PrecacheStateNotStarted
// returns: error
func (r *ClusterGroupUpgradeReconciler) handleNotStarted(ctx context.Context,
	clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade,
	cluster string) (string, error) {

	currentState, nextState := PrecacheStateNotStarted, PrecacheStatePreparingToStart
	r.Log.Info("[precachingFsm]", "currentState", currentState, "condition", "entry",
		"cluster", cluster, "nextState", nextState)

	err := r.deleteAllViews(ctx, cluster)
	if err != nil {
		return currentState, err
	}
	data := templateData{
		Cluster: cluster,
	}
	err = r.createResourcesFromTemplates(ctx, &data, precacheDeleteTemplates)
	if err != nil {
		return currentState, err
	}
	err = r.createResourcesFromTemplates(ctx, &data, precacheNSViewTemplates)
	if err != nil {
		return currentState, err
	}
	return nextState, nil
}

// handlePreparing handles conditions in PrecacheStatePreparingToStart
// returns: error
func (r *ClusterGroupUpgradeReconciler) handlePreparing(ctx context.Context,
	clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade,
	cluster string) (string, error) {

	currentState, nextState := PrecacheStatePreparingToStart, PrecacheStatePreparingToStart
	var condition string
	condition, err := r.getPreparingConditions(ctx, cluster)
	if err != nil {
		return currentState, err
	}
	switch condition {
	case NoNsView:
		nextState = currentState
	case NsFoundOnSpoke:
		nextState = currentState
	case NoNsFoundOnSpoke:
		nextState = PrecacheStateStarting
	default:
		return currentState, fmt.Errorf(
			"[handlePreparing] unknown condition %v in %s state", condition, currentState)
	}

	r.Log.Info("[precachingFsm]", "currentState", currentState, "condition", condition,
		"cluster", cluster, "nextState", nextState)
	return nextState, nil
}

// handleStarting handles conditions in PrecacheStateStarting
// returns: error
func (r *ClusterGroupUpgradeReconciler) handleStarting(ctx context.Context,
	clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade,
	cluster string) (string, error) {

	nextState, currentState := PrecacheStateStarting, PrecacheStateStarting
	var condition string

	condition, err := r.getStartingConditions(ctx, cluster)
	if err != nil {
		return currentState, err
	}
	switch condition {
	case DependenciesViewNotPresent, DependenciesNotPresent:
		_, err := r.deployDependencies(ctx, clusterGroupUpgrade, cluster)
		if err != nil {
			return currentState, err
		}
	case NoJobView:
		data := templateData{
			Cluster:               cluster,
			ViewUpdateIntervalSec: utils.ViewUpdateSec * len(clusterGroupUpgrade.Status.Precaching.Clusters),
		}
		err = r.createResourcesFromTemplates(ctx, &data, precacheJobView)
		if err != nil {
			return currentState, err
		}
	case NoJobFoundOnSpoke:
		r.Log.Info("[precachingFsm]", "currentState", currentState, "condition", NoJobFoundOnSpoke,
			"cluster", cluster, "nextState", PrecacheStateStarting)
		err = r.deployPrecachingWorkload(ctx, clusterGroupUpgrade, cluster)
		if err != nil {
			return currentState, err
		}
	case PrecacheJobActive:
		err = r.deleteDependenciesViews(ctx, cluster)
		if err != nil {
			return currentState, err
		}
		err = r.deleteManagedClusterViewResource(ctx, "view-precache-namespace", cluster)
		if err != nil {
			return currentState, err
		}
		nextState = PrecacheStateActive
	case PrecacheJobSucceeded:
		err = r.deleteDependenciesViews(ctx, cluster)
		if err != nil {
			return nextState, err
		}
		nextState = PrecacheStateSucceeded
	case PrecacheJobDeadline:
		err = r.deleteDependenciesViews(ctx, cluster)
		if err != nil {
			return nextState, err
		}
		nextState = PrecacheStateTimeout
	case PrecacheJobBackoffLimitExceeded:
		err = r.deleteDependenciesViews(ctx, cluster)
		if err != nil {
			return nextState, err
		}
		nextState = PrecacheStateError

	default:
		return currentState, fmt.Errorf(
			"[handleStarting] unknown condition %v in %s state", condition, currentState)
	}

	r.Log.Info("[precachingFsm]", "cluster", cluster, "currentState", currentState,
		"condition", condition, "nextState", nextState)

	return nextState, nil
}

// handleActive handles conditions in PrecacheStateActive
// returns: error
func (r *ClusterGroupUpgradeReconciler) handleActive(ctx context.Context,
	cluster string) (string, error) {

	nextState, currentState := PrecacheStateActive, PrecacheStateActive
	condition, err := r.getActiveConditions(ctx, cluster)
	if err != nil {
		return nextState, err
	}
	switch condition {
	case PrecacheJobDeadline:
		err = r.deleteDependenciesViews(ctx, cluster)
		if err != nil {
			return nextState, err
		}
		nextState = PrecacheStateTimeout
	case PrecacheJobSucceeded:
		err = r.deleteDependenciesViews(ctx, cluster)
		if err != nil {
			return currentState, err
		}
		nextState = PrecacheStateSucceeded
	case PrecacheJobBackoffLimitExceeded:
		err = r.deleteDependenciesViews(ctx, cluster)
		if err != nil {
			return nextState, err
		}
		nextState = PrecacheStateError
	case PrecacheJobActive:
		nextState = PrecacheStateActive
	default:
		return currentState, fmt.Errorf("[precachingFsm] unknown condition %s in %s state",
			condition, currentState)
	}
	return nextState, nil
}

// setPrecachingRequired sets conditions of precaching required
func (r *ClusterGroupUpgradeReconciler) setPrecachingRequired(
	clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) {
	meta.SetStatusCondition(
		&clusterGroupUpgrade.Status.Conditions, metav1.Condition{
			Type:    "Ready",
			Status:  metav1.ConditionFalse,
			Reason:  "PrecachingRequired",
			Message: "Precaching is not completed (required)"})

	meta.SetStatusCondition(
		&clusterGroupUpgrade.Status.Conditions, metav1.Condition{
			Type:    "PrecachingDone",
			Status:  metav1.ConditionFalse,
			Reason:  "PrecachingNotDone",
			Message: "Precaching is required and not done"})
}

// checkAllPrecachingDone handles alleviation of PrecachingDone==False condition
func (r *ClusterGroupUpgradeReconciler) checkAllPrecachingDone(
	clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) {
	// Handle completion
	if func() bool {
		for _, state := range clusterGroupUpgrade.Status.Precaching.Status {
			if state != PrecacheStateSucceeded {
				return false
			}
		}
		return true
	}() {
		meta.SetStatusCondition(
			&clusterGroupUpgrade.Status.Conditions, metav1.Condition{
				Type:    "Ready",
				Status:  metav1.ConditionFalse,
				Reason:  "UpgradeNotStarted",
				Message: "Precaching is completed"})
		meta.SetStatusCondition(
			&clusterGroupUpgrade.Status.Conditions, metav1.Condition{
				Type:    "PrecachingDone",
				Status:  metav1.ConditionTrue,
				Reason:  "PrecachingCompleted",
				Message: "Precaching is completed"})
		meta.RemoveStatusCondition(&clusterGroupUpgrade.Status.Conditions, "PrecacheSpecValid")
	}
}
