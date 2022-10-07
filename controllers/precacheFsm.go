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

// Jobresources conditions
const (
	NoNsView                   = "NoNsView"
	NoNsFoundOnSpoke           = "NoNsFoundOnSpoke"
	NsFoundOnSpoke             = "NsFoundOnSpoke"
	NoJobView                  = "NoJobView"
	NoJobFoundOnSpoke          = "NoJobFoundOnSpoke"
	JobViewExists              = "JobViewExists"
	DependenciesViewNotPresent = "DependenciesViewNotPresent"
	DependenciesNotPresent     = "DependenciesNotPresent"
	JobDeadline                = "JobDeadline"
	JobSucceeded               = "JobSucceeded"
	JobActive                  = "JobActive"
	JobBackoffLimitExceeded    = "JobBackoffLimitExceeded"
	UnforeseenCondition        = "UnforeseenCondition"
)

// precachingFsm implements the precaching state machine
// returns: error
func (r *ClusterGroupUpgradeReconciler) precachingFsm(ctx context.Context,
	clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, clusters []string) error {

	r.setPrecachingStartedCondition(clusterGroupUpgrade)
	specCondition := meta.FindStatusCondition(clusterGroupUpgrade.Status.Conditions, utils.PrecacheSpecValidCondition)
	if specCondition == nil || specCondition.Status == metav1.ConditionFalse {
		allManagedPoliciesExist, managedPoliciesInfo, err := r.doManagedPoliciesExist(
			ctx, clusterGroupUpgrade, clusters, false)
		if err != nil {
			return err
		}
		if !allManagedPoliciesExist {
			statusMessage := fmt.Sprintf(
				"The ClusterGroupUpgrade CR has managed policies that are missing: %s", managedPoliciesInfo.missingPolicies)
			utils.SetStatusCondition(
				&clusterGroupUpgrade.Status.Conditions,
				utils.ConditionTypes.PrecacheSpecValid,
				utils.ConditionReasons.NotAllManagedPoliciesExist,
				metav1.ConditionFalse,
				statusMessage,
			)
			return nil
		}

		spec, err := r.extractPrecachingSpecFromPolicies(managedPoliciesInfo.presentPolicies)
		if err != nil {
			return err
		}
		r.Log.Info("[precachingFsm]", "PrecacheSpecFromPolicies", spec)
		spec, err = r.includeSoftwareSpecOverrides(ctx, clusterGroupUpgrade, &spec)
		if err != nil {
			return err
		}
		ok, msg := r.checkPreCacheSpecConsistency(spec)
		if !ok {
			utils.SetStatusCondition(
				&clusterGroupUpgrade.Status.Conditions,
				utils.ConditionTypes.PrecacheSpecValid,
				utils.ConditionReasons.InvalidPlatformImage,
				metav1.ConditionFalse,
				fmt.Sprintf("Precaching spec is incomplete: %s", msg),
			)
			return nil
		}
		utils.SetStatusCondition(
			&clusterGroupUpgrade.Status.Conditions,
			utils.ConditionTypes.PrecacheSpecValid,
			utils.ConditionReasons.PrecacheSpecIsWellFormed,
			metav1.ConditionTrue,
			"Precaching spec is valid and consistent",
		)

		clusterGroupUpgrade.Status.Precaching.Spec = &spec
	}

	clusterGroupUpgrade.Status.Precaching.Clusters = clusters

	clusterStates := make(map[string]string)
	for _, cluster := range clusters {
		var currentState string
		if len(clusterGroupUpgrade.Status.Precaching.Status) == 0 {
			currentState = PrecacheStateNotStarted
		} else {
			currentState = clusterGroupUpgrade.Status.Precaching.Status[cluster]
		}
		var (
			nextState string
			err       error
		)
		r.Log.Info("[precachingFsm]", "currentState", currentState, "cluster", cluster)
		switch currentState {
		// Initial State
		case PrecacheStateNotStarted:
			nextState, err = r.handleNotStarted(ctx, cluster)
			if err != nil {
				return err
			}
		case PrecacheStatePreparingToStart:
			nextState, err = r.handlePreparing(ctx, cluster)
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
	clusterGroupUpgrade.Status.Precaching.Status = clusterStates
	r.checkAllPrecachingDone(clusterGroupUpgrade)
	return nil
}

// handleNotStarted handles conditions in PrecacheStateNotStarted
// returns: error
func (r *ClusterGroupUpgradeReconciler) handleNotStarted(ctx context.Context,
	cluster string) (string, error) {

	currentState, nextState := PrecacheStateNotStarted, PrecacheStatePreparingToStart
	r.Log.Info("[precachingFsm]", "currentState", currentState, "condition", "entry",
		"cluster", cluster, "nextState", nextState)

	err := r.deleteAllViews(ctx, cluster, precacheAllViews)
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
	cluster string) (string, error) {

	currentState := PrecacheStatePreparingToStart
	var nextState string
	var condition string
	condition, err := r.getPreparingConditions(ctx, cluster, precacheNSViewTemplates[0].resourceName)
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

	condition, err := r.getStartingConditions(ctx, cluster, precacheJobView[0].resourceName, precache)
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
		err = r.deployWorkload(ctx, clusterGroupUpgrade, cluster, precache, precacheJobView[0].resourceName, precacheCreateTemplates)
		if err != nil {
			return currentState, err
		}
	case JobActive:
		err = r.deleteDependenciesViews(ctx, cluster)
		if err != nil {
			return currentState, err
		}
		err = r.deleteManagedClusterViewResource(ctx, "view-precache-namespace", cluster)
		if err != nil {
			return currentState, err
		}
		nextState = PrecacheStateActive
	case JobSucceeded:
		err = r.deleteDependenciesViews(ctx, cluster)
		if err != nil {
			return nextState, err
		}
		nextState = PrecacheStateSucceeded
	case JobDeadline:
		err = r.deleteDependenciesViews(ctx, cluster)
		if err != nil {
			return nextState, err
		}
		nextState = PrecacheStateTimeout
	case JobBackoffLimitExceeded:
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
	condition, err := r.getActiveConditions(ctx, cluster, precacheJobView[0].resourceName)
	if err != nil {
		return nextState, err
	}
	switch condition {
	case JobDeadline:
		err = r.deleteDependenciesViews(ctx, cluster)
		if err != nil {
			return nextState, err
		}
		nextState = PrecacheStateTimeout
	case JobSucceeded:
		err = r.deleteDependenciesViews(ctx, cluster)
		if err != nil {
			return currentState, err
		}
		nextState = PrecacheStateSucceeded
	case JobBackoffLimitExceeded:
		err = r.deleteDependenciesViews(ctx, cluster)
		if err != nil {
			return nextState, err
		}
		nextState = PrecacheStateError
	case JobActive:
		nextState = PrecacheStateActive
	default:
		return currentState, fmt.Errorf("[precachingFsm] unknown condition %s in %s state",
			condition, currentState)
	}
	return nextState, nil
}

// setPrecachingStartedCondition sets conditions of precaching required
func (r *ClusterGroupUpgradeReconciler) setPrecachingStartedCondition(
	clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) {
	utils.SetStatusCondition(
		&clusterGroupUpgrade.Status.Conditions,
		utils.ConditionTypes.PrecachingSuceeded,
		utils.ConditionReasons.InProgress,
		metav1.ConditionFalse,
		"Precaching is required and not done",
	)
}

// checkAllPrecachingDone handles alleviation of PrecachingDone==False condition
func (r *ClusterGroupUpgradeReconciler) checkAllPrecachingDone(
	clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) {

	// Counts for the various cluster states
	var failedPrecacheCount int = 0
	var progressingPrecacheCount int = 0
	var successfulPrecacheCount int = 0

	// Loop over all the clusters and take count of all their states
	for _, state := range clusterGroupUpgrade.Status.Precaching.Status {
		switch state {
		case PrecacheStateSucceeded:
			successfulPrecacheCount++
		case PrecacheStateActive, PrecacheStateStarting, PrecacheStatePreparingToStart:
			progressingPrecacheCount++
		default:
			failedPrecacheCount++
		}
	}

	// Compare the total number of clusters to their status
	switch len(clusterGroupUpgrade.Status.Precaching.Status) {
	// All clusters were successful
	case successfulPrecacheCount:
		utils.SetStatusCondition(
			&clusterGroupUpgrade.Status.Conditions,
			utils.ConditionTypes.PrecachingSuceeded,
			utils.ConditionReasons.Completed,
			metav1.ConditionTrue,
			"Precaching is completed for all clusters",
		)
	// All clusters failed
	case failedPrecacheCount:
		utils.SetStatusCondition(
			&clusterGroupUpgrade.Status.Conditions,
			utils.ConditionTypes.PrecachingSuceeded,
			utils.ConditionReasons.Failed,
			metav1.ConditionFalse,
			"Precaching failed for all clusters",
		)
	// All clusters are completed but some failed
	case (failedPrecacheCount + successfulPrecacheCount):
		utils.SetStatusCondition(
			&clusterGroupUpgrade.Status.Conditions,
			utils.ConditionTypes.PrecachingSuceeded,
			utils.ConditionReasons.PartiallyDone,
			metav1.ConditionTrue,
			fmt.Sprintf("Precaching failed for %d clusters", failedPrecacheCount),
		)
	// Clusters are still in progress
	default:
		utils.SetStatusCondition(
			&clusterGroupUpgrade.Status.Conditions,
			utils.ConditionTypes.PrecachingSuceeded,
			utils.ConditionReasons.InProgress,
			metav1.ConditionFalse,
			fmt.Sprintf("Precaching in progress for %d clusters", progressingPrecacheCount),
		)
	}
}
