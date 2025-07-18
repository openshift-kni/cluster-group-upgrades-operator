/*
Copyright 2021.

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
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	utils "github.com/openshift-kni/cluster-group-upgrades-operator/controllers/utils"
	ranv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/clustergroupupgrades/v1alpha1"
	viewv1beta1 "github.com/stolostron/cluster-lifecycle-api/view/v1beta1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	mwv1 "open-cluster-management.io/api/work/v1"
	policiesv1 "open-cluster-management.io/governance-policy-propagator/api/v1"
)

// ClusterGroupUpgradeReconciler reconciles a ClusterGroupUpgrade object
type ClusterGroupUpgradeReconciler struct {
	client.Client
	Log      logr.Logger
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

type policiesInfo struct {
	invalidPolicies      []string
	missingPolicies      []string
	presentPolicies      []*unstructured.Unstructured
	compliantPolicies    []*unstructured.Unstructured
	duplicatedPoliciesNs map[string][]string
}

const statusUpdateWaitInMilliSeconds = 100

func doNotRequeue() ctrl.Result {
	return ctrl.Result{}
}

func requeueImmediately() ctrl.Result {
	return ctrl.Result{Requeue: true}
}

func requeueWithShortInterval() ctrl.Result {
	return requeueWithCustomInterval(30 * time.Second)
}

func requeueWithMediumInterval() ctrl.Result {
	return requeueWithCustomInterval(1 * time.Minute)
}

func requeueWithLongInterval() ctrl.Result {
	return requeueWithCustomInterval(5 * time.Minute)
}

func requeueWithCustomInterval(interval time.Duration) ctrl.Result {
	return ctrl.Result{RequeueAfter: interval}
}

func requeueWithError(err error) (ctrl.Result, error) {
	// can not be fixed by user during reconcile
	return ctrl.Result{}, err
}

//+kubebuilder:rbac:groups=ran.openshift.io,resources=clustergroupupgrades,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=ran.openshift.io,resources=clustergroupupgrades/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=ran.openshift.io,resources=clustergroupupgrades/finalizers,verbs=update
//+kubebuilder:rbac:groups=ran.openshift.io,resources=precachingconfigs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=ran.openshift.io,resources=precachingconfigs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=ran.openshift.io,resources=precachingconfigs/finalizers,verbs=update
//+kubebuilder:rbac:groups=apps.open-cluster-management.io,resources=placementrules,verbs=get;list;watch;create;update;patch;delete;deletecollection
//+kubebuilder:rbac:groups=policy.open-cluster-management.io,resources=placementbindings,verbs=get;list;watch;create;update;patch;delete;deletecollection
//+kubebuilder:rbac:groups=policy.open-cluster-management.io,resources=policies,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=cluster.open-cluster-management.io,resources=managedclusters,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=action.open-cluster-management.io,resources=managedclusteractions,verbs=create;update;delete;get;list;watch;patch;deletecollection
//+kubebuilder:rbac:groups=view.open-cluster-management.io,resources=managedclusterviews,verbs=create;update;delete;get;list;watch;patch;deletecollection
//+kubebuilder:rbac:groups=work.open-cluster-management.io,resources=manifestworks,verbs=create;update;delete;get;list;watch;patch;deletecollection
//+kubebuilder:rbac:groups=work.open-cluster-management.io,resources=manifestworkreplicasets,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=events,verbs=create;patch
//+kubebuilder:rbac:groups=monitoring.coreos.com,resources=servicemonitors,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=monitoring.coreos.com,resources=prometheusrules,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the ClusterGroupUpgrade object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.8.3/pkg/reconcile
//
//nolint:gocyclo // TODO: simplify this function
func (r *ClusterGroupUpgradeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (nextReconcile ctrl.Result, err error) {
	r.Log.Info("Start reconciling CGU", "name", req.NamespacedName)
	defer func() {
		if nextReconcile.RequeueAfter > 0 {
			r.Log.Info("Finish reconciling CGU", "name", req.NamespacedName, "requeueAfter", nextReconcile.RequeueAfter.Seconds())
		} else {
			r.Log.Info("Finish reconciling CGU", "name", req.NamespacedName, "requeueRightAway", nextReconcile.Requeue)
		}
	}()

	nextReconcile = doNotRequeue()
	// Wait a bit so that API server/etcd syncs up and this reconcile has a better chance of getting the updated CGU and policies
	time.Sleep(statusUpdateWaitInMilliSeconds * time.Millisecond)
	clusterGroupUpgrade := &ranv1alpha1.ClusterGroupUpgrade{}
	err = r.Get(ctx, req.NamespacedName, clusterGroupUpgrade)
	if err != nil {
		if errors.IsNotFound(err) {
			err = nil
			return
		}
		r.Log.Error(err, "Failed to get ClusterGroupUpgrade")
		return
	}

	r.Log.Info("Loaded CGU", "name", req.NamespacedName, "version", clusterGroupUpgrade.GetResourceVersion())
	var reconcileTime int
	reconcileTime, err = r.handleCguFinalizer(ctx, clusterGroupUpgrade)
	if err != nil {
		return
	}
	if reconcileTime == utils.ReconcileNow {
		nextReconcile = requeueImmediately()
		return
	} else if reconcileTime == utils.StopReconciling {
		return
	}

	suceededCondition := meta.FindStatusCondition(clusterGroupUpgrade.Status.Conditions, string(utils.ConditionTypes.Succeeded))
	progressingCondition := meta.FindStatusCondition(clusterGroupUpgrade.Status.Conditions, string(utils.ConditionTypes.Progressing))

	// nolint: gocritic
	if suceededCondition != nil {
		if clusterGroupUpgrade.Status.Status.CompletedAt.IsZero() {
			if shouldDeleteObjects(clusterGroupUpgrade) {
				err = r.deleteResources(ctx, clusterGroupUpgrade)
				if err != nil {
					return
				}
			}

			if suceededCondition.Status == metav1.ConditionTrue {
				r.Recorder.Event(clusterGroupUpgrade, corev1.EventTypeNormal, suceededCondition.Reason, suceededCondition.Message)
			} else {
				r.Recorder.Event(clusterGroupUpgrade, corev1.EventTypeWarning, suceededCondition.Reason, suceededCondition.Message)
			}
			// Set completion time only after post actions are executed with no errors
			clusterGroupUpgrade.Status.Status.CompletedAt = metav1.Now()
			clusterGroupUpgrade.Status.Status.CurrentBatch = 0
			clusterGroupUpgrade.Status.Status.CurrentBatchStartedAt = metav1.Time{}
			clusterGroupUpgrade.Status.Status.CurrentBatchRemediationProgress = nil
		}
	} else if progressingCondition == nil || progressingCondition.Status == metav1.ConditionFalse {

		var allManagedPoliciesExist, allManifestWorkTemplatesExist bool
		var managedPoliciesInfo policiesInfo
		var clusters, missingTemplates []string
		var compliantClusters []string
		var reconcile bool
		clusters, reconcile, err = r.validateCR(ctx, clusterGroupUpgrade)
		if err != nil {
			utils.SetStatusCondition(
				&clusterGroupUpgrade.Status.Conditions,
				utils.ConditionTypes.ClustersSelected,
				utils.ConditionReasons.ClusterNotFound,
				metav1.ConditionFalse,
				fmt.Sprintf("Unable to select clusters: %s", err),
			)
			nextReconcile = requeueWithLongInterval()
			err = r.updateStatus(ctx, clusterGroupUpgrade)
			return
		}
		utils.SetStatusCondition(
			&clusterGroupUpgrade.Status.Conditions,
			utils.ConditionTypes.ClustersSelected,
			utils.ConditionReasons.ClusterSelectionCompleted,
			metav1.ConditionTrue,
			"All selected clusters are valid",
		)
		if reconcile {
			nextReconcile = requeueImmediately()
			return
		}

		if clusterGroupUpgrade.RolloutType() == ranv1alpha1.RolloutTypes.Policy {
			allManagedPoliciesExist, managedPoliciesInfo, err =
				r.doManagedPoliciesExist(ctx, clusterGroupUpgrade, clusters)
		} else {
			_, missingTemplates, err = r.validateManifestWorkTemplates(ctx, clusterGroupUpgrade)
			allManifestWorkTemplatesExist = len(missingTemplates) == 0
		}
		if err != nil {
			return
		}

		if allManagedPoliciesExist || allManifestWorkTemplatesExist {
			// TODO validate CV in manifest work templates
			err = r.validateOpenshiftUpgradeVersion(clusterGroupUpgrade, managedPoliciesInfo.presentPolicies)
			if err != nil {
				nextReconcile = requeueWithLongInterval()
				err = r.updateStatus(ctx, clusterGroupUpgrade)
				return
			}

			err = r.validatePoliciesDependenciesOrder(clusterGroupUpgrade, managedPoliciesInfo.presentPolicies)
			if err != nil {
				nextReconcile = requeueWithLongInterval()
				err = r.updateStatus(ctx, clusterGroupUpgrade)
				return
			}

			utils.SetStatusCondition(
				&clusterGroupUpgrade.Status.Conditions,
				utils.ConditionTypes.Validated,
				utils.ConditionReasons.ValidationCompleted,
				metav1.ConditionTrue,
				"Completed validation",
			)

			// Build the upgrade batches.
			compliantClusters = r.buildRemediationPlan(clusterGroupUpgrade, clusters, managedPoliciesInfo.presentPolicies)

			// Recheck clusters list for any changes to the plan
			clusters = utils.GetClustersListFromRemediationPlan(clusterGroupUpgrade)

			// Create the needed resources for starting the upgrade.
			err = r.reconcileResources(ctx, clusterGroupUpgrade, managedPoliciesInfo.presentPolicies)
			if err != nil {
				return
			}
			err = r.processManagedPolicyForMonitoredObjects(clusterGroupUpgrade, managedPoliciesInfo.presentPolicies)
			if err != nil {
				return
			}
		} else {

			// If not all managedPolicies exist or invalid, update the Status accordingly.
			var statusMessage string
			var conditionReason utils.ConditionReason

			if clusterGroupUpgrade.RolloutType() == ranv1alpha1.RolloutTypes.Policy {
				conditionReason = utils.ConditionReasons.NotAllManagedPoliciesExist
				if len(managedPoliciesInfo.missingPolicies) != 0 {
					statusMessage = fmt.Sprintf("Missing managed policies: %s ", managedPoliciesInfo.missingPolicies)
				}

				if len(managedPoliciesInfo.invalidPolicies) != 0 {
					statusMessage = fmt.Sprintf("Invalid managed policies: %s ", managedPoliciesInfo.invalidPolicies)
				}

				if len(managedPoliciesInfo.duplicatedPoliciesNs) != 0 {
					jsonData, _ := json.Marshal(managedPoliciesInfo.duplicatedPoliciesNs)
					statusMessage = fmt.Sprintf(
						"Managed policy name should be unique, but was found in multiple namespaces: %s ", jsonData)
					conditionReason = utils.ConditionReasons.AmbiguousManagedPoliciesNames
				}
			} else {
				conditionReason = utils.ConditionReasons.NotAllManifestTemplatesExist
				statusMessage = fmt.Sprintf("Missing manifest templates: %s", missingTemplates)
			}
			// If there are errors regarding the managedPolicies, update the Status accordingly.
			utils.SetStatusCondition(
				&clusterGroupUpgrade.Status.Conditions,
				utils.ConditionTypes.Validated,
				conditionReason,
				metav1.ConditionFalse,
				statusMessage,
			)
			nextReconcile = requeueWithMediumInterval()
			// Update status
			err = r.updateStatus(ctx, clusterGroupUpgrade)
			return
		}
		// TODO precaching support for CRs in manifest work template?
		// Pass in already compliant policies as the catalog source info is needed by precaching
		err = r.reconcilePrecaching(ctx, clusterGroupUpgrade, clusters, append(managedPoliciesInfo.presentPolicies, managedPoliciesInfo.compliantPolicies...))
		if err != nil {
			r.Log.Error(err, "reconcilePrecaching error")
			return
		}
		if clusterGroupUpgrade.Status.Precaching != nil {
			precachingSpecCondition := meta.FindStatusCondition(clusterGroupUpgrade.Status.Conditions, string(utils.ConditionTypes.PrecacheSpecValid))
			if precachingSpecCondition.Status == metav1.ConditionTrue {
				precachingSucceededCondition := meta.FindStatusCondition(clusterGroupUpgrade.Status.Conditions, string(utils.ConditionTypes.PrecachingSuceeded))
				if precachingSucceededCondition == nil || precachingSucceededCondition.Status == metav1.ConditionFalse {
					err = r.updateStatus(ctx, clusterGroupUpgrade)
					nextReconcile = requeueWithShortInterval()
					return
				}
				// Update the clusters list based on the precaching results
				clusters = r.filterFailedPrecachingClusters(clusterGroupUpgrade, clusters)

				// Check if there were any issues with the precaching
				if len(clusters) == 0 && len(clusterGroupUpgrade.Status.RemediationPlan) != 0 {
					// We expected to remediate some clusters but currently have none
					// There should already be a condition present describing the issue we just need to set succeeded and requeue once
					utils.SetStatusCondition(
						&clusterGroupUpgrade.Status.Conditions,
						utils.ConditionTypes.Progressing,
						utils.ConditionReasons.Completed,
						metav1.ConditionFalse,
						"No clusters available for remediation (Precaching failed)",
					)
					utils.SetStatusCondition(
						&clusterGroupUpgrade.Status.Conditions,
						utils.ConditionTypes.Succeeded,
						utils.ConditionReasons.Failed,
						metav1.ConditionFalse,
						"No clusters available for remediation (Precaching failed)",
					)
					// Requeue is required here since we need to come back and check the succeeded condition for final cleanup
					nextReconcile = requeueImmediately()
					r.updateStatus(ctx, clusterGroupUpgrade)
					return
				}
				// fallthrough to the enable check
			} else {
				// wait for cgu update with valid policies for precaching spec
				nextReconcile = requeueWithLongInterval()
				r.updateStatus(ctx, clusterGroupUpgrade)
				return
			}
		}

		if !*clusterGroupUpgrade.Spec.Enable {
			utils.SetStatusCondition(
				&clusterGroupUpgrade.Status.Conditions,
				utils.ConditionTypes.Progressing,
				utils.ConditionReasons.NotEnabled,
				metav1.ConditionFalse,
				"Not enabled",
			)
			nextReconcile = requeueWithLongInterval()
			r.updateStatus(ctx, clusterGroupUpgrade)
			return
		}

		if clusterGroupUpgrade.Status.Status.StartedAt.IsZero() {
			clusterGroupUpgrade.Status.Status.StartedAt = metav1.Now()
		}
		// Check if there are any CRs that are blocking the start of the current one and are not yet completed.
		var blockingCRsNotCompleted, blockingCRsMissing []string
		blockingCRsNotCompleted, blockingCRsMissing, err = r.blockingCRsNotCompleted(ctx, clusterGroupUpgrade)
		if err != nil {
			return
		}

		// nolint: gocritic
		if len(blockingCRsMissing) > 0 {
			// If there are blocking CRs missing, update the message to show which those are.
			utils.SetStatusCondition(
				&clusterGroupUpgrade.Status.Conditions,
				utils.ConditionTypes.Progressing,
				utils.ConditionReasons.MissingBlockingCR,
				metav1.ConditionFalse,
				fmt.Sprintf("Missing blocking CRs: %s", blockingCRsMissing),
			)
			nextReconcile = requeueWithMediumInterval()
		} else if len(blockingCRsNotCompleted) > 0 {
			// If there are blocking CRs that are not completed, then the upgrade can't start.
			utils.SetStatusCondition(
				&clusterGroupUpgrade.Status.Conditions,
				utils.ConditionTypes.Progressing,
				utils.ConditionReasons.IncompleteBlockingCR,
				metav1.ConditionFalse,
				fmt.Sprintf("Blocking CRs that are not completed: %s", blockingCRsNotCompleted),
			)
			nextReconcile = requeueWithMediumInterval()
		} else {
			err = r.reconcileBackup(ctx, clusterGroupUpgrade, clusters)
			if err != nil {
				r.Log.Error(err, "reconcileBackup error")
				return
			}

			if clusterGroupUpgrade.Status.Backup != nil {
				backupCondition := meta.FindStatusCondition(clusterGroupUpgrade.Status.Conditions, string(utils.ConditionTypes.BackupSuceeded))
				if backupCondition == nil || backupCondition.Status == metav1.ConditionFalse {
					utils.SetStatusCondition(
						&clusterGroupUpgrade.Status.Conditions,
						utils.ConditionTypes.Progressing,
						utils.ConditionReasons.NotStarted,
						metav1.ConditionFalse,
						"Cluster backup is in progress",
					)
					err = r.updateStatus(ctx, clusterGroupUpgrade)
					nextReconcile = requeueWithShortInterval()
					return
				}
			}
			// Update the clusters list based on the backup results
			clusters = r.filterFailedBackupClusters(clusterGroupUpgrade, clusters)

			// Check if there were any issues with the backup
			if len(clusters) == 0 && len(clusterGroupUpgrade.Status.RemediationPlan) != 0 {
				// We expected to remediate some clusters but currently have none
				// There should already be a condition present describing the issue we just need to set succeeded and requeue once
				utils.SetStatusCondition(
					&clusterGroupUpgrade.Status.Conditions,
					utils.ConditionTypes.Progressing,
					utils.ConditionReasons.Completed,
					metav1.ConditionFalse,
					"No clusters available for remediation (Backup failed)",
				)
				utils.SetStatusCondition(
					&clusterGroupUpgrade.Status.Conditions,
					utils.ConditionTypes.Succeeded,
					utils.ConditionReasons.Failed,
					metav1.ConditionFalse,
					"No clusters available for remediation (Backup failed)",
				)
				// Requeue is required here since we need to come back and check the succeeded condition for final cleanup
				nextReconcile = requeueImmediately()
				r.updateStatus(ctx, clusterGroupUpgrade)
				return
			}

			if clusterGroupUpgrade.GetAnnotations()[utils.BlockingCGUCompletionModeAnn] == utils.PartialBlockingCGUCompletion && clusterGroupUpgrade.GetAnnotations()[utils.BlockingCGUClusterFiltering] == "true" {
				clusters, err = r.filterNonCompletedClustersInBlockingCRs(ctx, clusterGroupUpgrade, clusters)
				if err != nil {
					r.Log.Error(err, "filterNonCompletedClustersInBlockingCRs")
					return
				}
				if len(clusters) == 0 {
					utils.SetStatusCondition(
						&clusterGroupUpgrade.Status.Conditions,
						utils.ConditionTypes.Progressing,
						utils.ConditionReasons.Completed,
						metav1.ConditionFalse,
						"No clusters available for remediation after filtering out non-completed clusters from blocking CGUs",
					)
					utils.SetStatusCondition(
						&clusterGroupUpgrade.Status.Conditions,
						utils.ConditionTypes.Succeeded,
						utils.ConditionReasons.Failed,
						metav1.ConditionFalse,
						"No clusters available for remediation after filtering out non-completed clusters from blocking CGUs",
					)
					nextReconcile = requeueWithShortInterval()
					r.updateStatus(ctx, clusterGroupUpgrade)
					return
				}
			}

			// Rebuild remediation plan since we are about to start the upgrade and want to make sure the non-successful clusters were filtered out
			newCompliantClustesrs := r.buildRemediationPlan(clusterGroupUpgrade, clusters, managedPoliciesInfo.presentPolicies)
			compliantClusters = append(compliantClusters, newCompliantClustesrs...)
			r.performAfterCompletionActions(
				ctx, clusterGroupUpgrade, compliantClusters)

			// Take actions before starting upgrade.
			err = r.takeActionsBeforeEnable(ctx, clusterGroupUpgrade)
			if err != nil {
				return
			}

			// If the remediation plan is empty, update the status.
			if clusterGroupUpgrade.Status.RemediationPlan == nil {
				utils.SetStatusCondition(
					&clusterGroupUpgrade.Status.Conditions,
					utils.ConditionTypes.Progressing,
					utils.ConditionReasons.Completed,
					metav1.ConditionFalse,
					"All clusters are compliant with all the managed policies",
				)
				utils.SetStatusCondition(
					&clusterGroupUpgrade.Status.Conditions,
					utils.ConditionTypes.Succeeded,
					utils.ConditionReasons.Completed,
					metav1.ConditionTrue,
					"All clusters already compliant with the specified managed policies",
				)
				nextReconcile = requeueImmediately()
			} else {

				// Start the upgrade.
				utils.SetStatusCondition(
					&clusterGroupUpgrade.Status.Conditions,
					utils.ConditionTypes.Progressing,
					utils.ConditionReasons.InProgress,
					metav1.ConditionTrue,
					utils.InProgressMessages[clusterGroupUpgrade.RolloutType()],
				)
				nextReconcile = requeueImmediately()
			}

		}
		// If the condition is defined and the status isn't false, then it has to be true here
		// so we can skip an explicit check for condition true
	} else {
		r.Log.Info("[Reconcile]", "Status.CurrentBatch", clusterGroupUpgrade.Status.Status.CurrentBatch)

		// If the upgrade is just starting, set the batch to be shown in the Status as 1.
		if clusterGroupUpgrade.Status.Status.CurrentBatch == 0 {
			clusterGroupUpgrade.Status.Status.CurrentBatch = 1
		}

		//nolint
		requeueAfter := time.Until(clusterGroupUpgrade.Status.Status.CurrentBatchStartedAt.Add(5 * time.Minute))
		if requeueAfter < 0 {
			requeueAfter = 5 * time.Minute
		}
		nextReconcile = requeueWithCustomInterval(requeueAfter)

		// At first, assume all clusters in the batch start applying policies starting with the first one.
		// Also set the start time of the current batch to the current timestamp.
		if clusterGroupUpgrade.Status.Status.CurrentBatchStartedAt.IsZero() {
			r.initializeBatchProgress(clusterGroupUpgrade)
			if shouldDeleteObjects(clusterGroupUpgrade) {
				err = r.cleanupManifestWorkForPreviousBatch(ctx, clusterGroupUpgrade)
				if err != nil {
					return
				}
			}
			// Set the time for when the batch started updating.
			clusterGroupUpgrade.Status.Status.CurrentBatchStartedAt = metav1.Now()
		}

		// Check whether we have time left on the cgu timeout
		// nolint: gocritic
		if time.Since(clusterGroupUpgrade.Status.Status.StartedAt.Time) > time.Duration(clusterGroupUpgrade.Spec.RemediationStrategy.Timeout)*time.Minute {
			// We are completely out of time
			utils.SetStatusCondition(
				&clusterGroupUpgrade.Status.Conditions,
				utils.ConditionTypes.Progressing,
				utils.ConditionReasons.TimedOut,
				metav1.ConditionFalse,
				utils.TimeoutMessages[clusterGroupUpgrade.RolloutType()],
			)
			utils.SetStatusCondition(
				&clusterGroupUpgrade.Status.Conditions,
				utils.ConditionTypes.Succeeded,
				utils.ConditionReasons.TimedOut,
				metav1.ConditionFalse,
				utils.TimeoutMessages[clusterGroupUpgrade.RolloutType()],
			)
			err = r.handleBatchTimeout(ctx, clusterGroupUpgrade)
			nextReconcile = requeueImmediately()
		} else if clusterGroupUpgrade.Status.Status.CurrentBatch < len(clusterGroupUpgrade.Status.RemediationPlan) {
			// Check if current policies have become compliant and if new policies have to be applied.
			var isBatchComplete, isSoaking, isProgressing bool
			isBatchComplete, isSoaking, isProgressing, err = r.updateCurrentBatchProgress(ctx, clusterGroupUpgrade)
			if err != nil {
				return
			}

			if isBatchComplete {
				// If the upgrade is completed for the current batch, cleanup and move to the next.
				r.Log.Info("[Reconcile] Upgrade completed for batch", "batchIndex", clusterGroupUpgrade.Status.Status.CurrentBatch)
				if clusterGroupUpgrade.RolloutType() == ranv1alpha1.RolloutTypes.Policy {
					r.cleanupPlacementRules(ctx, clusterGroupUpgrade)
				}
				clusterGroupUpgrade.Status.Status.CurrentBatchStartedAt = metav1.Time{}
				clusterGroupUpgrade.Status.Status.CurrentBatch++
				nextReconcile = requeueImmediately()
			} else {
				if isSoaking {
					nextReconcile = requeueWithShortInterval()
				}
				// Manifestwork rollout requires an additional reqconcile when progressing, first one for updating index and the second one
				// for deleting/creating the manifestwork
				if isProgressing && clusterGroupUpgrade.RolloutType() == ranv1alpha1.RolloutTypes.ManifestWork {
					nextReconcile = requeueImmediately()
				}
				// Add the needed cluster names to upgrade to the appropriate placement rule.
				err = r.remediateCurrentBatch(ctx, clusterGroupUpgrade)
				if err != nil {
					return
				}

				// Check if this batch has timed out
				if !clusterGroupUpgrade.Status.Status.CurrentBatchStartedAt.IsZero() {

					currentBatchTimeout := utils.CalculateBatchTimeout(
						clusterGroupUpgrade.Spec.RemediationStrategy.Timeout,
						len(clusterGroupUpgrade.Status.RemediationPlan),
						clusterGroupUpgrade.Status.Status.CurrentBatch,
						clusterGroupUpgrade.Status.Status.CurrentBatchStartedAt.Time,
						clusterGroupUpgrade.Status.Status.StartedAt.Time)

					r.Log.Info("[Reconcile] Calculating batch timeout (minutes)", "currentBatchTimeout", fmt.Sprintf("%f", currentBatchTimeout.Minutes()))

					if time.Since(clusterGroupUpgrade.Status.Status.CurrentBatchStartedAt.Time) > currentBatchTimeout {
						// We want to immediately continue to the next reconcile regardless of the timeout action
						nextReconcile = requeueImmediately()

						// Check if this was a canary or not
						if len(clusterGroupUpgrade.Spec.RemediationStrategy.Canaries) != 0 &&
							clusterGroupUpgrade.Status.Status.CurrentBatch <= len(clusterGroupUpgrade.Spec.RemediationStrategy.Canaries) {
							r.Log.Info("Canaries batch timed out")
							utils.SetStatusCondition(
								&clusterGroupUpgrade.Status.Conditions,
								utils.ConditionTypes.Progressing,
								utils.ConditionReasons.TimedOut,
								metav1.ConditionFalse,
								utils.TimeoutMessages[clusterGroupUpgrade.RolloutType()]+" on canary clusters",
							)
							utils.SetStatusCondition(
								&clusterGroupUpgrade.Status.Conditions,
								utils.ConditionTypes.Succeeded,
								utils.ConditionReasons.TimedOut,
								metav1.ConditionFalse,
								utils.TimeoutMessages[clusterGroupUpgrade.RolloutType()]+" on canary clusters",
							)
						} else {
							r.Log.Info("Batch upgrade timed out")
							err = r.handleBatchTimeout(ctx, clusterGroupUpgrade)
							if err != nil {
								return
							}
							switch clusterGroupUpgrade.Spec.BatchTimeoutAction {
							case ranv1alpha1.BatchTimeoutAction.Abort:
								// If the value was abort then we need to fail out
								utils.SetStatusCondition(
									&clusterGroupUpgrade.Status.Conditions,
									utils.ConditionTypes.Progressing,
									utils.ConditionReasons.TimedOut,
									metav1.ConditionFalse,
									utils.TimeoutMessages[clusterGroupUpgrade.RolloutType()]+" on some clusters",
								)
								utils.SetStatusCondition(
									&clusterGroupUpgrade.Status.Conditions,
									utils.ConditionTypes.Succeeded,
									utils.ConditionReasons.TimedOut,
									metav1.ConditionFalse,
									utils.TimeoutMessages[clusterGroupUpgrade.RolloutType()]+" on some clusters",
								)
							default:
								// If the value was continue or not defined then continue
								clusterGroupUpgrade.Status.Status.CurrentBatchStartedAt = metav1.Time{}
								if clusterGroupUpgrade.Status.Status.CurrentBatch < len(clusterGroupUpgrade.Status.RemediationPlan) {
									clusterGroupUpgrade.Status.Status.CurrentBatch++
								}
							}
						}
					}
				}
			}
		} else {
			// On last batch, check all batches
			var isUpgradeComplete bool
			isUpgradeComplete, err = r.remediateLastBatch(ctx, clusterGroupUpgrade, &nextReconcile)
			if err != nil {
				return
			}
			if isUpgradeComplete {
				utils.SetStatusCondition(
					&clusterGroupUpgrade.Status.Conditions,
					utils.ConditionTypes.Progressing,
					utils.ConditionReasons.Completed,
					metav1.ConditionFalse,
					utils.CompletedMessages[clusterGroupUpgrade.RolloutType()],
				)
				utils.SetStatusCondition(
					&clusterGroupUpgrade.Status.Conditions,
					utils.ConditionTypes.Succeeded,
					utils.ConditionReasons.Completed,
					metav1.ConditionTrue,
					utils.CompletedMessages[clusterGroupUpgrade.RolloutType()],
				)
				nextReconcile = requeueImmediately()
			}
		}
	}

	// Update status
	err = r.updateStatus(ctx, clusterGroupUpgrade)
	return
}

func shouldDeleteObjects(clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) bool {
	afterCompletion := clusterGroupUpgrade.Spec.Actions.AfterCompletion
	if afterCompletion == nil || afterCompletion.DeleteObjects == nil || *afterCompletion.DeleteObjects {
		return true
	}
	return false
}

func (r *ClusterGroupUpgradeReconciler) handleBatchTimeout(ctx context.Context,
	clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) error {

	// check if batch is initialized in case of timeout happened before the batch starting
	if len(clusterGroupUpgrade.Status.Status.CurrentBatchRemediationProgress) == 0 {
		return nil
	}

	// check if there was a remediation plan at all
	if len(clusterGroupUpgrade.Status.RemediationPlan) == 0 {
		return nil
	}

	batchIndex := clusterGroupUpgrade.Status.Status.CurrentBatch - 1

	// If the index is longer then the remediation plan that would cause a nil access below
	if batchIndex >= len(clusterGroupUpgrade.Status.RemediationPlan) {
		r.Log.Info("Batch index out of range")
		r.Log.Info("[addClustersStatusOnTimeout]", "RemediationPlan", clusterGroupUpgrade.Status.RemediationPlan)
		return nil
	}

	for _, batchClusterName := range clusterGroupUpgrade.Status.RemediationPlan[batchIndex] {
		clusterFinalState := ranv1alpha1.ClusterState{
			Name: batchClusterName, State: utils.ClusterRemediationComplete}
		// In certain edge cases we need to be careful to avoid a nil pointer on this access
		clusterStatus := clusterGroupUpgrade.Status.Status.CurrentBatchRemediationProgress[batchClusterName]
		if clusterStatus == nil {
			// Assume the cluster timed out if the status was not defined when it should have been
			// This implies that this batch did not even get a chance to start
			clusterFinalState.State = utils.ClusterRemediationTimedout
			utils.DeleteMultiCloudObjects(ctx, r.Client, clusterGroupUpgrade, batchClusterName)
			clusterGroupUpgrade.Status.Clusters = append(clusterGroupUpgrade.Status.Clusters, clusterFinalState)
		} else if clusterStatus.State == ranv1alpha1.InProgress {
			clusterFinalState.State = utils.ClusterRemediationTimedout
			switch clusterGroupUpgrade.RolloutType() {
			case ranv1alpha1.RolloutTypes.Policy:
				r.handlePolicyTimeoutForCluster(clusterGroupUpgrade, batchClusterName, &clusterFinalState)
			default:
				err := r.handleManifestWorkTimeoutForCluster(ctx, clusterGroupUpgrade, batchClusterName, &clusterFinalState)
				if err != nil {
					return err
				}
			}
			utils.DeleteMultiCloudObjects(ctx, r.Client, clusterGroupUpgrade, batchClusterName)
			clusterGroupUpgrade.Status.Clusters = append(clusterGroupUpgrade.Status.Clusters, clusterFinalState)
		}
	}
	return nil
}

func (r *ClusterGroupUpgradeReconciler) initializeBatchProgress(
	clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) {

	clusterGroupUpgrade.Status.Status.CurrentBatchRemediationProgress = make(map[string]*ranv1alpha1.ClusterRemediationProgress)
	batchIndex := clusterGroupUpgrade.Status.Status.CurrentBatch - 1

	// By default, don't set any policy index for any of the clusters in the batch.
	for _, batchClusterName := range clusterGroupUpgrade.Status.RemediationPlan[batchIndex] {
		clusterGroupUpgrade.Status.Status.CurrentBatchRemediationProgress[batchClusterName] = new(ranv1alpha1.ClusterRemediationProgress)
		clusterGroupUpgrade.Status.Status.CurrentBatchRemediationProgress[batchClusterName].State = ranv1alpha1.NotStarted

	}

	r.Log.Info("[initializeBatchProgress]",
		"CurrentBatchRemediationProgress", clusterGroupUpgrade.Status.Status.CurrentBatchRemediationProgress)
}

func (r *ClusterGroupUpgradeReconciler) updateCurrentBatchProgress(
	ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) (bool, bool, bool, error) {
	batchIndex := clusterGroupUpgrade.Status.Status.CurrentBatch - 1
	isBatchComplete := true
	isSoaking := false
	isProgressing := false

	for _, clusterName := range clusterGroupUpgrade.Status.RemediationPlan[batchIndex] {

		isClusterCompleted, soak, progressing, err := r.updateClusterProgress(ctx, clusterGroupUpgrade, clusterName)
		if soak {
			isSoaking = true
		}
		if progressing {
			isProgressing = true
		}
		if err != nil {
			return false, false, false, err
		}
		if !isClusterCompleted {
			isBatchComplete = false
		}
	}

	r.Log.Info("[updateCurrentBatchProgress]", "plan", clusterGroupUpgrade.Status.Status.CurrentBatchRemediationProgress, "isBatchComplete", isBatchComplete)
	return isBatchComplete, isSoaking, isProgressing, nil
}

func (r *ClusterGroupUpgradeReconciler) updateClusterProgress(
	ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, clusterName string) (bool, bool, bool, error) {
	// nil check to avoid panic in edge cases
	if clusterGroupUpgrade.Status.Status.CurrentBatchRemediationProgress == nil {
		clusterGroupUpgrade.Status.Status.CurrentBatchRemediationProgress = make(map[string]*ranv1alpha1.ClusterRemediationProgress)
	}
	if clusterGroupUpgrade.Status.Status.CurrentBatchRemediationProgress[clusterName] == nil {
		clusterGroupUpgrade.Status.Status.CurrentBatchRemediationProgress[clusterName] = new(ranv1alpha1.ClusterRemediationProgress)
		clusterGroupUpgrade.Status.Status.CurrentBatchRemediationProgress[clusterName].State = ranv1alpha1.NotStarted
	}
	clusterProgressState := &clusterGroupUpgrade.Status.Status.CurrentBatchRemediationProgress[clusterName].State

	var index **int
	var size int
	switch clusterGroupUpgrade.RolloutType() {
	case ranv1alpha1.RolloutTypes.Policy:
		index = &clusterGroupUpgrade.Status.Status.CurrentBatchRemediationProgress[clusterName].PolicyIndex
		size = len(clusterGroupUpgrade.Status.ManagedPoliciesForUpgrade)
	default:
		index = &clusterGroupUpgrade.Status.Status.CurrentBatchRemediationProgress[clusterName].ManifestWorkIndex
		size = len(clusterGroupUpgrade.Spec.ManifestWorkTemplates)
	}

	if *clusterProgressState == ranv1alpha1.NotStarted {
		*index = new(int)
		**index = 0
		*clusterProgressState = ranv1alpha1.InProgress
	} else if *clusterProgressState == ranv1alpha1.Completed {
		return true, false, false, nil
	}

	currentIndex, isSoaking, err := r.getClusterProgress(ctx, clusterGroupUpgrade, clusterName, **index)
	if err != nil {
		return false, false, false, err
	}

	isProgressing := currentIndex > **index
	if currentIndex >= size {
		clusterGroupUpgrade.Status.Status.CurrentBatchRemediationProgress[clusterName].PolicyIndex = nil
		clusterGroupUpgrade.Status.Status.CurrentBatchRemediationProgress[clusterName].ManifestWorkIndex = nil
		*clusterProgressState = ranv1alpha1.Completed
		err := r.takeActionsAfterCompletion(ctx, clusterGroupUpgrade, clusterName)
		if err != nil {
			return false, false, false, err
		}
		// Clean up ManagedClusterView only as ManagedClusterActions get deleted automatically when executed successfully.
		err = utils.DeleteManagedClusterViews(ctx, r.Client, clusterGroupUpgrade, clusterName)
		if err != nil {
			return false, false, false, err
		}
		return true, isSoaking, isProgressing, nil
	}
	**index = currentIndex
	return false, isSoaking, isProgressing, nil
}

func (r *ClusterGroupUpgradeReconciler) getClusterProgress(
	ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, clusterName string, startIndex int) (int, bool, error) {
	switch clusterGroupUpgrade.RolloutType() {
	case ranv1alpha1.RolloutTypes.Policy:
		return r.getNextNonCompliantPolicyForCluster(ctx, clusterGroupUpgrade, clusterName, startIndex)
	default:
		return r.getNextManifestWorkForCluster(ctx, clusterGroupUpgrade, clusterName, startIndex)
	}
}

func (r *ClusterGroupUpgradeReconciler) remediateLastBatch(
	ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, nextReconcile *ctrl.Result) (bool, error) {
	// On last batch, check all batches
	isUpgradeComplete, isSoaking, isProgressing, err := r.isUpgradeComplete(ctx, clusterGroupUpgrade)
	if err != nil {
		return false, err
	}
	if isUpgradeComplete {
		return true, nil
	}
	if isSoaking {
		*nextReconcile = requeueWithShortInterval()
	}
	// Manifestwork rollout requires an additional reqconcile when progressing, first one for updating index and the second one
	// for deleting/creating the manifestwork
	if isProgressing && clusterGroupUpgrade.RolloutType() == ranv1alpha1.RolloutTypes.ManifestWork {
		*nextReconcile = requeueImmediately()
	}
	err = r.remediateCurrentBatch(ctx, clusterGroupUpgrade)
	return false, err
}

/*
remediateCurrentBatch:
- steps through the remediationPolicyIndex and add the clusterNames to the corresponding
placement rules in order so that at the end of a batch upgrade, all the copied policies are Compliant.
- approves the needed InstallPlans for the Subscription type policies

returns: error/nil
*/
func (r *ClusterGroupUpgradeReconciler) remediateCurrentBatch(
	ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) error {
	switch clusterGroupUpgrade.RolloutType() {
	case ranv1alpha1.RolloutTypes.Policy:
		err := r.updatePlacementRules(ctx, clusterGroupUpgrade)
		if err != nil {
			return err
		}
		// Approve needed InstallPlans.
		err = r.processMonitoredObjects(ctx, clusterGroupUpgrade)
		return err

	default:
		return r.updateManifestWorkForCurrentBatch(ctx, clusterGroupUpgrade)
	}
}

/*
isUpgradeComplete checks if there is at least one managed policy left for which at least one cluster in the

	batch is NonCompliant.

	returns: true/false if the upgrade is complete
	         error/nil
*/
func (r *ClusterGroupUpgradeReconciler) isUpgradeComplete(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) (bool, bool, bool, error) {
	isComplete, isSoaking, isProgressing, err := r.updateCurrentBatchProgress(ctx, clusterGroupUpgrade)
	if err != nil {
		return false, false, false, err
	}

	if isComplete && clusterGroupUpgrade.RolloutType() == ranv1alpha1.RolloutTypes.Policy {
		isComplete, isSoaking, err = r.arePreviousBatchesCompleteForPolicies(ctx, clusterGroupUpgrade)
	}

	return isComplete, isSoaking, isProgressing, err
}

func (r *ClusterGroupUpgradeReconciler) performAfterCompletionActions(
	ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, clusters []string,
) {
	r.Log.Info("[performAfterCompletionActionsForCompliantClusters]",
		"cgu", clusterGroupUpgrade.Name, "clusters", clusters)
	// Make clusters unique
	clusterSet := make(map[string]struct{}, len(clusters))
	uniqueClusters := make([]string, 0, len(clusters))
	for _, c := range clusters {
		if _, exists := clusterSet[c]; !exists {
			clusterSet[c] = struct{}{}
			uniqueClusters = append(uniqueClusters, c)
		}
	}
	clusters = uniqueClusters

	if !*clusterGroupUpgrade.Spec.Enable {
		return
	}
	for i := 0; i < len(clusters); i++ {
		cluster := clusters[i]
		r.takeActionsAfterCompletion(ctx, clusterGroupUpgrade, cluster)
	}
}

func (r *ClusterGroupUpgradeReconciler) buildRemediationPlan(
	clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, clusters []string, managedPolicies []*unstructured.Unstructured) []string {
	var clusterMap map[string]bool
	compliantClusters := []string{}
	if len(managedPolicies) > 0 {
		// Get all clusters from the CR that are non compliant with at least one of the managedPolicies.
		clusterMap = r.getClustersNonCompliantWithManagedPolicies(clusters, managedPolicies)
	} else if len(clusterGroupUpgrade.Spec.ManifestWorkTemplates) > 0 {
		clusterMap = make(map[string]bool, len(clusters))
		// Assume all clusters need manifest work rollout
		for _, cluster := range clusters {
			clusterMap[cluster] = true
		}
	}

	// Create remediation plan
	var remediationPlan [][]string
	isCanary := make(map[string]bool)
	if clusterGroupUpgrade.Spec.RemediationStrategy.Canaries != nil && len(clusterGroupUpgrade.Spec.RemediationStrategy.Canaries) > 0 {
		for _, canary := range clusterGroupUpgrade.Spec.RemediationStrategy.Canaries {
			if clusterMap[canary] {
				remediationPlan = append(remediationPlan, []string{canary})
				isCanary[canary] = true
			} else {
				compliantClusters = append(compliantClusters, canary)
			}
		}
	}

	var batch []string
	clusterCount := 0
	for i := 0; i < len(clusters); i++ {
		cluster := clusters[i]
		if !isCanary[cluster] {
			if clusterMap[cluster] {
				batch = append(batch, cluster)
				clusterCount++
			} else {
				compliantClusters = append(compliantClusters, cluster)
			}
		}

		if clusterCount == clusterGroupUpgrade.Status.ComputedMaxConcurrency || i == len(clusters)-1 {
			if len(batch) > 0 {
				remediationPlan = append(remediationPlan, batch)
				clusterCount = 0
				batch = nil
			}
		}
	}
	r.Log.Info("Remediation plan", "remediatePlan", remediationPlan)
	clusterGroupUpgrade.Status.RemediationPlan = remediationPlan
	return compliantClusters
}

func (r *ClusterGroupUpgradeReconciler) getAllClustersForUpgrade(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) ([]string, error) {

	// These will be used later
	clusterNames := []string{}
	selectorClusters := []string{}
	keys := make(map[string]bool)

	// Check to make sure at least one cluster selection method is defined
	if clusterGroupUpgrade.Spec.Clusters == nil &&
		clusterGroupUpgrade.Spec.ClusterSelector == nil &&
		clusterGroupUpgrade.Spec.ClusterLabelSelectors == nil {
		return clusterNames, errors.NewBadRequest("no cluster specified for remediation")
	}

	// First add all the clusters explicitly specified in the spec
	for _, clusterName := range clusterGroupUpgrade.Spec.Clusters {
		// Make sure a cluster name doesn't appear twice.
		if _, value := keys[clusterName]; !value {
			keys[clusterName] = true
			clusterNames = append(clusterNames, clusterName)
		}
	}

	// Next get a list of all the clusters that match using the deprecated clusterSelector
	// The expected format for ClusterSelector can be found in codedoc for its type definition
	for _, clusterSelector := range clusterGroupUpgrade.Spec.ClusterSelector {
		selectorList := strings.Split(clusterSelector, "=")
		var clusterLabels map[string]string
		// nolint: gocritic
		if len(selectorList) == 2 {
			clusterLabels = map[string]string{selectorList[0]: selectorList[1]}
		} else if len(selectorList) == 1 {
			clusterLabels = map[string]string{selectorList[0]: ""}
		} else {
			r.Log.Info("Ignoring malformed cluster selector: '%s'", clusterSelector)
			continue
		}

		listOpts := []client.ListOption{
			client.MatchingLabels(clusterLabels),
		}

		clusterList := &clusterv1.ManagedClusterList{}
		if err := r.List(ctx, clusterList, listOpts...); err != nil {
			return nil, err
		}

		for _, cluster := range clusterList.Items {
			// Make sure a cluster name doesn't appear twice.
			if _, value := keys[cluster.GetName()]; !value {
				keys[cluster.GetName()] = true
				selectorClusters = append(selectorClusters, cluster.GetName())
			}
		}
	}

	// Next get a list of all the clusters that matching using the clusterLabelSelector
	// The expected format for ClusterLabelSelector can be found in codedoc for its type definition
	for _, clusterLabelSelector := range clusterGroupUpgrade.Spec.ClusterLabelSelectors {

		// The selector object has to be converted into this selector type to be used in the list options
		selector, err := metav1.LabelSelectorAsSelector(&clusterLabelSelector)
		if err != nil {
			return nil, err
		}

		listOpts := []client.ListOption{
			client.MatchingLabelsSelector{Selector: selector},
		}

		clusterList := &clusterv1.ManagedClusterList{}
		if err := r.List(ctx, clusterList, listOpts...); err != nil {
			return nil, err
		}

		for _, cluster := range clusterList.Items {
			// Make sure a cluster name doesn't appear twice.
			if _, value := keys[cluster.GetName()]; !value {
				keys[cluster.GetName()] = true
				selectorClusters = append(selectorClusters, cluster.GetName())
			}
		}
	}

	// The kubernetes api does not return consistent results for selectors
	// Due to this behaviour we have to sort that portion of the list so that the result is consistent
	sort.Strings(selectorClusters)

	// Add the selector clusters to the full list of clusters
	clusterNames = append(clusterNames, selectorClusters...)

	// Return the full list of clusters
	return clusterNames, nil
}

// filterFailedPrecachingClusters filters the input cluster list by removing any clusters which failed to perform their backup.
func (r *ClusterGroupUpgradeReconciler) filterFailedPrecachingClusters(clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, clusters []string) []string {
	var clustersList []string
	if clusterGroupUpgrade.Status.Precaching != nil {
		for _, name := range clusters {
			if clusterGroupUpgrade.Status.Precaching.Status[name] == PrecacheStateSucceeded {
				clustersList = append(clustersList, name)
			}
		}
	} else {
		clustersList = clusters
	}
	r.Log.Info("filterFailedPrecachingClusters: ", "clustersList", clustersList)
	return clustersList
}

// filterFailedBackupClusters filters the input cluster list by removing any clusters which failed to perform their backup.
func (r *ClusterGroupUpgradeReconciler) filterFailedBackupClusters(clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, clusters []string) []string {
	var clustersList []string
	if clusterGroupUpgrade.Status.Backup != nil {
		for _, name := range clusters {
			if clusterGroupUpgrade.Status.Backup.Status[name] == BackupStateSucceeded {
				clustersList = append(clustersList, name)
			}
		}
	} else {
		clustersList = clusters
	}
	r.Log.Info("filterFailedBackupClusters:", "clustersList", clustersList)
	return clustersList
}

func (r *ClusterGroupUpgradeReconciler) updateStatus(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) error {
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		err := r.Status().Update(ctx, clusterGroupUpgrade)
		return err
	})

	if err != nil {
		return err
	}

	return nil
}

func (r *ClusterGroupUpgradeReconciler) filterNonCompletedClustersInBlockingCRs(ctx context.Context, cgu *ranv1alpha1.ClusterGroupUpgrade, clusters []string) ([]string, error) {
	completedCGUs := make(map[string]int)
	for _, blockingCR := range cgu.Spec.BlockingCRs {
		blockingCGU := &ranv1alpha1.ClusterGroupUpgrade{}
		err := r.Get(ctx, types.NamespacedName{Name: blockingCR.Name, Namespace: blockingCR.Namespace}, blockingCGU)
		if err != nil {
			return []string{}, fmt.Errorf("failed to get blocking CR: %w", err)
		}
		for _, clusterState := range blockingCGU.Status.Clusters {
			if clusterState.State == utils.ClusterRemediationComplete {
				completedCGUs[clusterState.Name]++
			}
		}
	}
	filteredClusters := make([]string, 0)
	for clusterName, numberCompleted := range completedCGUs {
		if numberCompleted == len(cgu.Spec.BlockingCRs) && utils.Contains(clusters, clusterName) {
			filteredClusters = append(filteredClusters, clusterName)
		}
	}
	return filteredClusters, nil
}

func (r *ClusterGroupUpgradeReconciler) blockingCRsNotCompleted(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) ([]string, []string, error) {

	var blockingCRsNotCompleted []string
	var blockingCRsMissing []string

	// Range through all the blocking CRs.
	for _, blockingCR := range clusterGroupUpgrade.Spec.BlockingCRs {
		cgu := &ranv1alpha1.ClusterGroupUpgrade{}
		err := r.Get(ctx, types.NamespacedName{Name: blockingCR.Name, Namespace: blockingCR.Namespace}, cgu)

		if err != nil {
			r.Log.Info("[blockingCRsNotCompleted] CR not found", "name", blockingCR.Name, "error: ", err)
			if errors.IsNotFound(err) {
				blockingCRsMissing = append(blockingCRsMissing, blockingCR.Name)
				continue
			} else {
				return nil, nil, err
			}
		}

		// If a blocking CR doesn't have status conditions, it means something has gone wrong with processing
		// it, so we should assume it's not completed.
		if cgu.Status.Conditions == nil {
			blockingCRsNotCompleted = append(blockingCRsNotCompleted, cgu.Name)
			continue
		}

		if clusterGroupUpgrade.GetAnnotations()[utils.BlockingCGUCompletionModeAnn] == utils.PartialBlockingCGUCompletion {
			if !utils.IsStatusConditionPresent(cgu.Status.Conditions, string(utils.ConditionTypes.Succeeded)) {
				blockingCRsNotCompleted = append(blockingCRsNotCompleted, cgu.Name)
			}
		} else if !meta.IsStatusConditionTrue(cgu.Status.Conditions, string(utils.ConditionTypes.Succeeded)) {
			blockingCRsNotCompleted = append(blockingCRsNotCompleted, cgu.Name)
		}
	}

	r.Log.Info("[blockingCRsNotCompleted]", "blockingCRsNotCompleted", blockingCRsNotCompleted)
	return blockingCRsNotCompleted, blockingCRsMissing, nil
}

func (r *ClusterGroupUpgradeReconciler) validateCR(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) ([]string, bool, error) {
	reconcile := false
	// Validate clusters in spec are ManagedCluster objects
	clusters, err := r.getAllClustersForUpgrade(ctx, clusterGroupUpgrade)
	if err != nil {
		return nil, reconcile, fmt.Errorf("cannot obtain all the details about the clusters in the CR: %s", err)
	}

	for _, cluster := range clusters {
		managedCluster := &clusterv1.ManagedCluster{}
		err := r.Client.Get(ctx, types.NamespacedName{Name: cluster}, managedCluster)
		if err != nil {
			return nil, reconcile, fmt.Errorf("cluster %s is not a ManagedCluster", cluster)
		}
	}

	// Validate the canaries are in the list of clusters.
	if clusterGroupUpgrade.Spec.RemediationStrategy.Canaries != nil && len(clusterGroupUpgrade.Spec.RemediationStrategy.Canaries) > 0 {
		for _, canary := range clusterGroupUpgrade.Spec.RemediationStrategy.Canaries {
			foundCanary := false
			for _, cluster := range clusters {
				if canary == cluster {
					foundCanary = true
					break
				}
			}
			if !foundCanary {
				return nil, reconcile, fmt.Errorf("canary cluster %s is not in the list of clusters", canary)
			}
		}
	}

	var newMaxConcurrency int
	// Automatically adjust maxConcurrency to the min of maxConcurrency and the number of clusters.
	if clusterGroupUpgrade.Spec.RemediationStrategy.MaxConcurrency > 0 &&
		clusterGroupUpgrade.Spec.RemediationStrategy.MaxConcurrency < len(clusters) {
		newMaxConcurrency = clusterGroupUpgrade.Spec.RemediationStrategy.MaxConcurrency
	} else {
		newMaxConcurrency = len(clusters)
	}

	if newMaxConcurrency != clusterGroupUpgrade.Status.ComputedMaxConcurrency {
		clusterGroupUpgrade.Status.ComputedMaxConcurrency = newMaxConcurrency
		err = r.updateStatus(ctx, clusterGroupUpgrade)
		if err != nil {
			r.Log.Info("Error updating Cluster Group Upgrade")
			return nil, reconcile, err
		}
		reconcile = true
	}

	return clusters, reconcile, nil
}

func (r *ClusterGroupUpgradeReconciler) handleCguFinalizer(
	ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) (int, error) {

	isCguMarkedToBeDeleted := clusterGroupUpgrade.GetDeletionTimestamp() != nil
	if isCguMarkedToBeDeleted {
		if controllerutil.ContainsFinalizer(clusterGroupUpgrade, utils.CleanupFinalizer) {
			// Run finalization logic for cguFinalizer. If the finalization logic fails, don't remove the finalizer so
			// that we can retry during the next reconciliation.
			if err := utils.FinalMultiCloudObjectCleanup(ctx, r.Client, clusterGroupUpgrade); err != nil {
				return utils.StopReconciling, err
			}
			// additional cleanup if CGU is not completed yet or deleteObject is set to false
			if clusterGroupUpgrade.Status.Status.CompletedAt.IsZero() || !shouldDeleteObjects(clusterGroupUpgrade) {
				r.Log.Info("Final cleanup for in prgress CGU or deleteObject set to false")
				// Include placementRules, placementBindings, precaching/backup job/manageClusterView/Action and ManifestWork for current batch
				if err := r.deleteResources(ctx, clusterGroupUpgrade); err != nil {
					return utils.StopReconciling, err
				}

				if err := r.finalCleanupManifestWork(ctx, clusterGroupUpgrade); err != nil {
					return utils.StopReconciling, err
				}
			}

			// Remove cguFinalizer. Once all finalizers have been removed, the object will be deleted.
			controllerutil.RemoveFinalizer(clusterGroupUpgrade, utils.CleanupFinalizer)
			if err := r.Update(ctx, clusterGroupUpgrade); err != nil {
				return utils.StopReconciling, err
			}
		}
		return utils.StopReconciling, nil
	}

	// Add finalizer for this CR.
	if !controllerutil.ContainsFinalizer(clusterGroupUpgrade, utils.CleanupFinalizer) {
		controllerutil.AddFinalizer(clusterGroupUpgrade, utils.CleanupFinalizer)
		err := r.Update(ctx, clusterGroupUpgrade)
		if err != nil {
			return utils.StopReconciling, err
		}
		return utils.ReconcileNow, nil
	}

	return utils.DontReconcile, nil
}

func (r *ClusterGroupUpgradeReconciler) getCGUControllerWorkerCount() (count int) {
	maxConcurrency, isSet := os.LookupEnv(utils.CGUControllerWorkerCountEnv)
	var err error
	if isSet {
		count, err = strconv.Atoi(maxConcurrency)
		if err != nil || count < 1 {
			r.Log.Info("Invalid value '%s' for %s, using the default: %i", maxConcurrency, utils.CGUControllerWorkerCountEnv, utils.DefaultCGUControllerWorkerCount)
			count = utils.DefaultCGUControllerWorkerCount
		}
	} else {
		count = utils.DefaultCGUControllerWorkerCount
	}
	return
}

func (r *ClusterGroupUpgradeReconciler) managedClusterResourceMapper(ctx context.Context, resource client.Object) []reconcile.Request {
	reqs := make([]reconcile.Request, 0)
	cguName, nameExists := resource.GetLabels()["openshift-cluster-group-upgrades/clusterGroupUpgrade"]
	cguNamespace, namespaceExists := resource.GetLabels()["openshift-cluster-group-upgrades/clusterGroupUpgradeNamespace"]
	if nameExists && namespaceExists {
		reqs = append(reqs, reconcile.Request{NamespacedName: types.NamespacedName{Namespace: cguNamespace, Name: cguName}})
	}
	return reqs
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterGroupUpgradeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.Recorder = mgr.GetEventRecorderFor("ClusterGroupUpgrade")

	return ctrl.NewControllerManagedBy(mgr).
		For(&ranv1alpha1.ClusterGroupUpgrade{}, builder.WithPredicates(predicate.Funcs{
			UpdateFunc: func(e event.UpdateEvent) bool {
				// Generation is only updated on spec changes (also on deletion),
				// not metadata or status
				oldGeneration := e.ObjectOld.GetGeneration()
				newGeneration := e.ObjectNew.GetGeneration()
				// spec update only for CGU
				return oldGeneration != newGeneration
			},
			CreateFunc:  func(ce event.CreateEvent) bool { return true },
			GenericFunc: func(ge event.GenericEvent) bool { return false },
			DeleteFunc:  func(de event.DeleteEvent) bool { return false },
		})).
		Watches(
			&mwv1.ManifestWork{},
			handler.EnqueueRequestsFromMapFunc(r.managedClusterResourceMapper),
			builder.WithPredicates(predicate.Funcs{
				UpdateFunc: func(e event.UpdateEvent) bool {
					// Generation is only updated on spec changes (also on deletion),
					// not metadata or status
					oldGeneration := e.ObjectOld.GetGeneration()
					newGeneration := e.ObjectNew.GetGeneration()
					// status update only for manifestwork
					return oldGeneration == newGeneration
				},
				CreateFunc:  func(ce event.CreateEvent) bool { return false },
				GenericFunc: func(ge event.GenericEvent) bool { return false },
				DeleteFunc:  func(de event.DeleteEvent) bool { return true },
			})).
		Watches(
			&policiesv1.Policy{},
			handler.Funcs{UpdateFunc: r.rootPolicyHandlerOnUpdate},
			builder.WithPredicates(predicate.Funcs{
				UpdateFunc: func(e event.UpdateEvent) bool {
					// Filter out updates to child policies
					if _, ok := e.ObjectNew.GetLabels()[utils.ChildPolicyLabel]; ok {
						return false
					}

					// Process pure status updates to root policies
					return e.ObjectOld.GetGeneration() == e.ObjectNew.GetGeneration()
				},
				CreateFunc:  func(ce event.CreateEvent) bool { return false },
				GenericFunc: func(ge event.GenericEvent) bool { return false },
				DeleteFunc:  func(de event.DeleteEvent) bool { return false },
			})).
		Watches(
			&viewv1beta1.ManagedClusterView{},
			handler.EnqueueRequestsFromMapFunc(r.managedClusterResourceMapper),
			builder.WithPredicates(predicate.Funcs{
				UpdateFunc: func(e event.UpdateEvent) bool {
					return e.ObjectOld.GetGeneration() == e.ObjectNew.GetGeneration()
				},
				CreateFunc:  func(ce event.CreateEvent) bool { return false },
				GenericFunc: func(ge event.GenericEvent) bool { return false },
				DeleteFunc:  func(de event.DeleteEvent) bool { return false },
			})).
		WithOptions(controller.Options{MaxConcurrentReconciles: r.getCGUControllerWorkerCount()}).
		Complete(r)
}
