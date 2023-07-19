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
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	ranv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/api/v1alpha1"
	utils "github.com/openshift-kni/cluster-group-upgrades-operator/controllers/utils"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
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

//+kubebuilder:rbac:groups=ran.openshift.io,resources=clustergroupupgrades,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=ran.openshift.io,resources=clustergroupupgrades/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=ran.openshift.io,resources=clustergroupupgrades/finalizers,verbs=update
//+kubebuilder:rbac:groups=apps.open-cluster-management.io,resources=placementrules,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=policy.open-cluster-management.io,resources=placementbindings,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=policy.open-cluster-management.io,resources=policies,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=cluster.open-cluster-management.io,resources=managedclusters,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=action.open-cluster-management.io,resources=managedclusteractions,verbs=create;update;delete;get;list;watch;patch
//+kubebuilder:rbac:groups=view.open-cluster-management.io,resources=managedclusterviews,verbs=create;update;delete;get;list;watch;patch
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

	if suceededCondition != nil {
		if clusterGroupUpgrade.Status.Status.CompletedAt.IsZero() {
			deleteObjects := clusterGroupUpgrade.Spec.Actions.AfterCompletion.DeleteObjects
			if deleteObjects == nil || *deleteObjects {
				err = r.deleteResources(ctx, clusterGroupUpgrade)
				if err != nil {
					return
				}
			}

			if suceededCondition.Status == metav1.ConditionTrue {
				r.Recorder.Event(clusterGroupUpgrade, corev1.EventTypeNormal, suceededCondition.Reason, suceededCondition.Message)
			} else {
				r.Recorder.Event(clusterGroupUpgrade, corev1.EventTypeWarning, suceededCondition.Reason, suceededCondition.Message)
				r.handleBatchTimeout(ctx, clusterGroupUpgrade)
			}
			// Set completion time only after post actions are executed with no errors
			clusterGroupUpgrade.Status.Status.CompletedAt = metav1.Now()
			clusterGroupUpgrade.Status.Status.CurrentBatch = 0
			clusterGroupUpgrade.Status.Status.CurrentBatchStartedAt = metav1.Time{}
			clusterGroupUpgrade.Status.Status.CurrentBatchRemediationProgress = nil
		}
	} else if progressingCondition == nil || progressingCondition.Status == metav1.ConditionFalse {

		var allManagedPoliciesExist bool
		var managedPoliciesInfo policiesInfo
		var clusters []string
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

		allManagedPoliciesExist, managedPoliciesInfo, err =
			r.doManagedPoliciesExist(ctx, clusterGroupUpgrade, clusters)
		if err != nil {
			return
		}
		if allManagedPoliciesExist {

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
			r.buildRemediationPlan(ctx, clusterGroupUpgrade, clusters, managedPoliciesInfo.presentPolicies)

			// Recheck clusters list for any changes to the plan
			clusters = utils.GetClustersListFromRemediationPlan(clusterGroupUpgrade)

			// Create the needed resources for starting the upgrade.
			var isPolicyErr bool
			isPolicyErr, err = r.reconcileResources(ctx, clusterGroupUpgrade, managedPoliciesInfo.presentPolicies)
			if err != nil {
				return
			} else if isPolicyErr {
				nextReconcile = requeueWithMediumInterval()
				return nextReconcile, nil
			}
			err = r.processManagedPolicyForMonitoredObjects(clusterGroupUpgrade, managedPoliciesInfo.presentPolicies)
			if err != nil {
				return
			}
		} else {

			// If not all managedPolicies exist or invalid, update the Status accordingly.
			var statusMessage string
			var conditionReason utils.ConditionReason

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
			// There are no blocking CRs, continue with the upgrade process.
			// create backup

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

			// Rebuild remediation plan since we are about to start the upgrade and want to make sure the non-successful clusters were filtered out
			r.buildRemediationPlan(ctx, clusterGroupUpgrade, clusters, managedPoliciesInfo.presentPolicies)

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
					"Remediating non-compliant policies",
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
			r.initializeRemediationPolicyForBatch(clusterGroupUpgrade)
			// Set the time for when the batch started updating.
			clusterGroupUpgrade.Status.Status.CurrentBatchStartedAt = metav1.Now()
		}

		// Check whether we have time left on the cgu timeout
		if time.Since(clusterGroupUpgrade.Status.Status.StartedAt.Time) > time.Duration(clusterGroupUpgrade.Spec.RemediationStrategy.Timeout)*time.Minute {
			// We are completely out of time
			utils.SetStatusCondition(
				&clusterGroupUpgrade.Status.Conditions,
				utils.ConditionTypes.Progressing,
				utils.ConditionReasons.TimedOut,
				metav1.ConditionFalse,
				"Policy remediation took too long",
			)
			utils.SetStatusCondition(
				&clusterGroupUpgrade.Status.Conditions,
				utils.ConditionTypes.Succeeded,
				utils.ConditionReasons.TimedOut,
				metav1.ConditionFalse,
				"Policy remediation took too long",
			)
			nextReconcile = requeueImmediately()
		} else if clusterGroupUpgrade.Status.Status.CurrentBatch < len(clusterGroupUpgrade.Status.RemediationPlan) {
			// Check if current policies have become compliant and if new policies have to be applied.
			var isBatchComplete, isSoaking bool
			isBatchComplete, isSoaking, err = r.getNextRemediationPoliciesForBatch(ctx, r.Client, clusterGroupUpgrade)
			if err != nil {
				return
			}

			if isBatchComplete {
				// If the upgrade is completed for the current batch, cleanup and move to the next.
				r.Log.Info("[Reconcile] Upgrade completed for batch", "batchIndex", clusterGroupUpgrade.Status.Status.CurrentBatch)
				r.cleanupPlacementRules(ctx, clusterGroupUpgrade)
				clusterGroupUpgrade.Status.Status.CurrentBatchStartedAt = metav1.Time{}
				clusterGroupUpgrade.Status.Status.CurrentBatch++
				nextReconcile = requeueImmediately()
			} else {
				// Add the needed cluster names to upgrade to the appropriate placement rule.
				err = r.remediateCurrentBatch(ctx, clusterGroupUpgrade, &nextReconcile, isSoaking)
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
								"Policy remediation took too long on canary clusters",
							)
							utils.SetStatusCondition(
								&clusterGroupUpgrade.Status.Conditions,
								utils.ConditionTypes.Succeeded,
								utils.ConditionReasons.TimedOut,
								metav1.ConditionFalse,
								"Policy remediation took too long on canary clusters",
							)
						} else {
							r.Log.Info("Batch upgrade timed out")
							r.handleBatchTimeout(ctx, clusterGroupUpgrade)
							switch clusterGroupUpgrade.Spec.BatchTimeoutAction {
							case ranv1alpha1.BatchTimeoutAction.Abort:
								// If the value was abort then we need to fail out
								utils.SetStatusCondition(
									&clusterGroupUpgrade.Status.Conditions,
									utils.ConditionTypes.Progressing,
									utils.ConditionReasons.TimedOut,
									metav1.ConditionFalse,
									"Policy remediation took too long on some clusters",
								)
								utils.SetStatusCondition(
									&clusterGroupUpgrade.Status.Conditions,
									utils.ConditionTypes.Succeeded,
									utils.ConditionReasons.TimedOut,
									metav1.ConditionFalse,
									"Policy remediation took too long on some clusters",
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
			var isUpgradeComplete, isSoaking bool
			isUpgradeComplete, isSoaking, err = r.isUpgradeComplete(ctx, clusterGroupUpgrade)
			if err != nil {
				return
			}
			if isUpgradeComplete {
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
					"All clusters are compliant with all the managed policies",
				)
				nextReconcile = requeueImmediately()
			} else {
				err = r.remediateCurrentBatch(ctx, clusterGroupUpgrade, &nextReconcile, isSoaking)
				if err != nil {
					return
				}
			}
		}
	}

	// Update status
	err = r.updateStatus(ctx, clusterGroupUpgrade)
	return
}

func (r *ClusterGroupUpgradeReconciler) handleBatchTimeout(ctx context.Context,
	clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) {

	// check if batch is initialized in case of timeout happened before the batch starting
	if len(clusterGroupUpgrade.Status.Status.CurrentBatchRemediationProgress) == 0 {
		return
	}

	// check if there was a remediation plan at all
	if len(clusterGroupUpgrade.Status.RemediationPlan) == 0 {
		return
	}

	batchIndex := clusterGroupUpgrade.Status.Status.CurrentBatch - 1

	// If the index is longer then the remediation plan that would cause a nil access below
	if batchIndex >= len(clusterGroupUpgrade.Status.RemediationPlan) {
		r.Log.Info("Batch index out of range")
		r.Log.Info("[addClustersStatusOnTimeout]", "RemediationPlan", clusterGroupUpgrade.Status.RemediationPlan)
		return
	}

	for _, batchClusterName := range clusterGroupUpgrade.Status.RemediationPlan[batchIndex] {
		clusterState := ranv1alpha1.ClusterState{
			Name: batchClusterName, State: utils.ClusterRemediationComplete}
		// In certain edge cases we need to be careful to avoid a nil pointer on this access
		clusterStatus := clusterGroupUpgrade.Status.Status.CurrentBatchRemediationProgress[batchClusterName]
		if clusterStatus == nil {
			// Assume the cluster timed out if the status was not defined when it should have been
			// This implies that this batch did not even get a chance to start
			clusterState.State = utils.ClusterRemediationTimedout
			utils.DeleteMultiCloudObjects(ctx, r.Client, clusterGroupUpgrade, batchClusterName)
			clusterGroupUpgrade.Status.Clusters = append(clusterGroupUpgrade.Status.Clusters, clusterState)
		} else if clusterStatus.State == ranv1alpha1.InProgress {
			clusterState.State = utils.ClusterRemediationTimedout

			if clusterStatus.PolicyIndex == nil {
				r.Log.Info("[addClustsersStatusOnTimeout] Undefined policy index for cluster")
				r.Log.Info("[addClustersStatusOnTimeout]", "batchClusterName", batchClusterName)
				r.Log.Info("[addClustersStatusOnTimeout]", "batchIndex", batchIndex)
				r.Log.Info("[addClustersStatusOnTimeout]", "RemediationPlan", clusterGroupUpgrade.Status.RemediationPlan)
				continue
			}

			policyIndex := *clusterStatus.PolicyIndex
			// Avoid panics because of index out of bound in edge cases
			if policyIndex < len(clusterGroupUpgrade.Status.ManagedPoliciesForUpgrade) {
				clusterState.CurrentPolicy = &ranv1alpha1.PolicyStatus{
					Name:   clusterGroupUpgrade.Status.ManagedPoliciesForUpgrade[policyIndex].Name,
					Status: utils.ClusterStatusNonCompliant}
			}
			utils.DeleteMultiCloudObjects(ctx, r.Client, clusterGroupUpgrade, batchClusterName)
			clusterGroupUpgrade.Status.Clusters = append(clusterGroupUpgrade.Status.Clusters, clusterState)
		}
	}
}

func (r *ClusterGroupUpgradeReconciler) initializeRemediationPolicyForBatch(
	clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) {

	clusterGroupUpgrade.Status.Status.CurrentBatchRemediationProgress = make(map[string]*ranv1alpha1.ClusterRemediationProgress)
	batchIndex := clusterGroupUpgrade.Status.Status.CurrentBatch - 1

	// By default, don't set any policy index for any of the clusters in the batch.
	for _, batchClusterName := range clusterGroupUpgrade.Status.RemediationPlan[batchIndex] {
		clusterGroupUpgrade.Status.Status.CurrentBatchRemediationProgress[batchClusterName] = new(ranv1alpha1.ClusterRemediationProgress)
		clusterGroupUpgrade.Status.Status.CurrentBatchRemediationProgress[batchClusterName].State = ranv1alpha1.NotStarted

	}

	r.Log.Info("[initializeRemediationPolicyForBatch]",
		"CurrentBatchRemediationProgress", clusterGroupUpgrade.Status.Status.CurrentBatchRemediationProgress)
}

/*
getNextRemediationPoliciesForBatch: Each cluster is checked against each policy in order. If the cluster is not bound
to the policy, or if the cluster is already compliant with the policy, the indexing advances until a NonCompliant
policy is found for the cluster or the end of the list is reached.

The policy currently applied for each cluster has its index held in
clusterGroupUpgrade.Status.Status.CurrentBatchRemediationProgress[clusterName].PolicyIndex (the index is used to range through the
policies present in clusterGroupUpgrade.Status.ManagedPoliciesForUpgrade).

returns: bool     : true if the batch is done upgrading; false if not

	error/nil: in case any error happens
*/
func (r *ClusterGroupUpgradeReconciler) getNextRemediationPoliciesForBatch(
	ctx context.Context, client client.Client, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) (bool, bool, error) {
	batchIndex := clusterGroupUpgrade.Status.Status.CurrentBatch - 1
	numberOfPolicies := len(clusterGroupUpgrade.Status.ManagedPoliciesForUpgrade)
	isBatchComplete := true
	isSoaking := false

	for _, clusterName := range clusterGroupUpgrade.Status.RemediationPlan[batchIndex] {
		// nil check to avoid panic in edge cases
		if clusterGroupUpgrade.Status.Status.CurrentBatchRemediationProgress == nil {
			clusterGroupUpgrade.Status.Status.CurrentBatchRemediationProgress = make(map[string]*ranv1alpha1.ClusterRemediationProgress)
		}
		if clusterGroupUpgrade.Status.Status.CurrentBatchRemediationProgress[clusterName] == nil {
			clusterGroupUpgrade.Status.Status.CurrentBatchRemediationProgress[clusterName] = new(ranv1alpha1.ClusterRemediationProgress)
			clusterGroupUpgrade.Status.Status.CurrentBatchRemediationProgress[clusterName].State = ranv1alpha1.NotStarted
		}
		clusterProgressState := clusterGroupUpgrade.Status.Status.CurrentBatchRemediationProgress[clusterName].State
		if clusterProgressState == ranv1alpha1.NotStarted {
			clusterGroupUpgrade.Status.Status.CurrentBatchRemediationProgress[clusterName].PolicyIndex = new(int)
			*clusterGroupUpgrade.Status.Status.CurrentBatchRemediationProgress[clusterName].PolicyIndex = 0
			clusterGroupUpgrade.Status.Status.CurrentBatchRemediationProgress[clusterName].State = ranv1alpha1.InProgress
		} else if clusterProgressState == ranv1alpha1.Completed {
			continue
		}
		currentPolicyIndex := *clusterGroupUpgrade.Status.Status.CurrentBatchRemediationProgress[clusterName].PolicyIndex

		// Get the index of the next policy for which the cluster is NonCompliant.
		currentPolicyIndex, soak, err := r.getNextNonCompliantPolicyForCluster(ctx, clusterGroupUpgrade, clusterName, currentPolicyIndex)
		if soak {
			isSoaking = true
		}
		if err != nil {
			return false, isSoaking, err
		}

		if currentPolicyIndex >= numberOfPolicies {
			clusterGroupUpgrade.Status.Status.CurrentBatchRemediationProgress[clusterName].PolicyIndex = nil
			clusterGroupUpgrade.Status.Status.CurrentBatchRemediationProgress[clusterName].State = ranv1alpha1.Completed
			err := r.takeActionsAfterCompletion(ctx, clusterGroupUpgrade, clusterName)
			if err != nil {
				return false, isSoaking, err
			}
			// Clean up ManagedClusterView only as ManagedClusterActions get deleted automatically when executed successfully.
			err = utils.DeleteManagedClusterViews(ctx, client, clusterGroupUpgrade, clusterName)
			if err != nil {
				return false, isSoaking, err
			}
		} else {
			isBatchComplete = false
			*clusterGroupUpgrade.Status.Status.CurrentBatchRemediationProgress[clusterName].PolicyIndex = currentPolicyIndex
		}
	}

	r.Log.Info("[getNextRemediationPoliciesForBatch]", "plan", clusterGroupUpgrade.Status.Status.CurrentBatchRemediationProgress, "isBatchComplete", isBatchComplete)
	return isBatchComplete, isSoaking, nil
}

/*
remediateCurrentBatch:
- steps through the remediationPolicyIndex and add the clusterNames to the corresponding
placement rules in order so that at the end of a batch upgrade, all the copied policies are Compliant.
- approves the needed InstallPlans for the Subscription type policies

returns: error/nil
*/
func (r *ClusterGroupUpgradeReconciler) remediateCurrentBatch(
	ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, nextReconcile *ctrl.Result, isSoaking bool) error {

	err := r.updatePlacementRules(ctx, clusterGroupUpgrade)
	if err != nil {
		return err
	}
	// Approve needed InstallPlans.
	reconcileSooner, err := r.processMonitoredObjects(ctx, clusterGroupUpgrade)
	if reconcileSooner || isSoaking {
		*nextReconcile = requeueWithShortInterval()
	}
	return err
}

func (r *ClusterGroupUpgradeReconciler) updatePlacementRules(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) error {

	policiesToUpdate := make(map[int][]string)
	for clusterName, clusterProgress := range clusterGroupUpgrade.Status.Status.CurrentBatchRemediationProgress {
		if clusterProgress.State != ranv1alpha1.InProgress {
			continue
		}
		clusterNames := policiesToUpdate[*clusterProgress.PolicyIndex]
		clusterNames = append(clusterNames, clusterName)
		policiesToUpdate[*clusterProgress.PolicyIndex] = clusterNames
	}

	for index, clusterNames := range policiesToUpdate {
		placementRuleName := utils.GetResourceName(clusterGroupUpgrade, clusterGroupUpgrade.Status.ManagedPoliciesForUpgrade[index].Name+"-placement")
		if safeName, ok := clusterGroupUpgrade.Status.SafeResourceNames[placementRuleName]; ok {
			err := r.updatePlacementRuleWithClusters(ctx, clusterGroupUpgrade, clusterNames, safeName)
			if err != nil {
				return err
			}
		} else {
			return fmt.Errorf("placement object name %s not found in CGU %s", placementRuleName, clusterGroupUpgrade.Name)
		}
	}
	return nil
}

func (r *ClusterGroupUpgradeReconciler) updatePlacementRuleWithClusters(
	ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, clusterNames []string, prName string) error {

	placementRule := &unstructured.Unstructured{}
	placementRule.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "apps.open-cluster-management.io",
		Kind:    "PlacementRule",
		Version: "v1",
	})
	err := r.Client.Get(ctx, client.ObjectKey{
		Name:      prName,
		Namespace: clusterGroupUpgrade.Namespace,
	}, placementRule)

	if err != nil {
		return err
	}

	placementRuleSpecClusters := placementRule.Object["spec"].(map[string]interface{})

	var prClusterNames []string
	var updatedClusters []map[string]interface{}
	currentClusters := placementRuleSpecClusters["clusters"]

	if currentClusters != nil {
		// Check clusterName is not already present in currentClusters
		for _, clusterEntry := range currentClusters.([]interface{}) {
			clusterMap := clusterEntry.(map[string]interface{})
			updatedClusters = append(updatedClusters, clusterMap)
			prClusterNames = append(prClusterNames, clusterMap["name"].(string))
		}
	}

	for _, clusterName := range clusterNames {
		isCurrentClusterAlreadyPresent := false
		for _, prClusterName := range prClusterNames {
			if prClusterName == clusterName {
				isCurrentClusterAlreadyPresent = true
				break
			}
		}
		if !isCurrentClusterAlreadyPresent {
			updatedClusters = append(updatedClusters, map[string]interface{}{"name": clusterName})
		}
	}

	placementRuleSpecClusters["clusters"] = updatedClusters
	placementRuleSpecClusters["clusterReplicas"] = nil

	err = r.Client.Update(ctx, placementRule)
	if err != nil {
		return err
	}

	return nil
}

func (r *ClusterGroupUpgradeReconciler) cleanupPlacementRules(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) error {
	// Get all the placementRules associated to this upgrades CR.
	placementRules, err := r.getPlacementRules(ctx, clusterGroupUpgrade, nil)

	if err != nil {
		return err
	}

	errorMap := make(map[string]string)
	for _, plr := range placementRules.Items {
		placementRuleSpecClusters := plr.Object["spec"].(map[string]interface{})
		placementRuleSpecClusters["clusters"] = nil
		placementRuleSpecClusters["clusterReplicas"] = 0

		err = r.Client.Update(ctx, &plr)
		if err != nil {
			errorMap[plr.GetName()] = err.Error()
			return err
		}
	}

	if len(errorMap) != 0 {
		return fmt.Errorf("errors cleaning up placement rules: %s", errorMap)
	}
	return nil
}

func (r *ClusterGroupUpgradeReconciler) getPolicyByName(ctx context.Context, policyName, namespace string) (*unstructured.Unstructured, error) {
	foundPolicy := &unstructured.Unstructured{}
	foundPolicy.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "policy.open-cluster-management.io",
		Kind:    "Policy",
		Version: "v1",
	})

	// Look for policy.
	return foundPolicy, r.Client.Get(ctx, types.NamespacedName{Name: policyName, Namespace: namespace}, foundPolicy)
}

func updateDuplicatedManagedPoliciesInfo(managedPoliciesInfo *policiesInfo, policiesNs map[string][]string) {
	managedPoliciesInfo.duplicatedPoliciesNs = make(map[string][]string)
	for crtPolicy, crtNs := range policiesNs {
		if len(crtNs) > 1 {
			managedPoliciesInfo.duplicatedPoliciesNs[crtPolicy] = crtNs
			sort.Strings(managedPoliciesInfo.duplicatedPoliciesNs[crtPolicy])
		}
	}
}

/*
	 doManagedPoliciesExist checks that all the managedPolicies specified in the CR exist.
	   returns: true/false                   if all the policies exist or not
				policiesInfo                 managed policies info including the missing policy names,
				                             the invalid policy names and the policies present on the system
				error
*/
func (r *ClusterGroupUpgradeReconciler) doManagedPoliciesExist(
	ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade,
	clusters []string) (bool, policiesInfo, error) {

	childPoliciesList, err := utils.GetChildPolicies(ctx, r.Client, clusters)
	if err != nil {
		return false, policiesInfo{}, err
	}

	var managedPoliciesInfo policiesInfo
	// Go through all the child policies and split the namespace from the policy name.
	// A child policy name has the name format parent_policy_namespace.parent_policy_name
	// The policy map we are creating will be of format {"policy_name": "policy_namespace"}
	policyMap := make(map[string]string)
	// Keep inventory of all the namespaces a managed policy appears in with policyNs which
	// is of format {"policy_name": []string of policy namespaces}
	policiesNs := make(map[string][]string)
	policyEnforce := make(map[string]bool)
	policyInvalidHubTmpl := make(map[string]bool)
	for _, childPolicy := range childPoliciesList {
		policyNameArr, err := utils.GetParentPolicyNameAndNamespace(childPolicy.Name)
		if err != nil {
			r.Log.Info("[doManagedPoliciesExist] Ignoring child policy " + childPolicy.Name + "with invalid name")
			continue
		}

		// Identify policies with remediationAction enforce to ignore
		if strings.EqualFold(string(childPolicy.Spec.RemediationAction), "enforce") {
			policyEnforce[policyNameArr[1]] = true
			continue
		}

		for _, policyT := range childPolicy.Spec.PolicyTemplates {
			// Identify policies have invalid hub templates.
			// If the child configuration policy contains a string pattern "{{hub",
			// it means the hub template is invalid and fails to be processed on the hub cluster.
			if strings.Contains(string(policyT.ObjectDefinition.Raw), "{{hub") {
				policyInvalidHubTmpl[policyNameArr[1]] = true
			}
		}

		policyMap[policyNameArr[1]] = policyNameArr[0]
		utils.UpdateManagedPolicyNamespaceList(policiesNs, policyNameArr)
	}

	// If a managed policy is present in more than one namespace, raise an error and advice user to
	// fix the duplicated name.
	updateDuplicatedManagedPoliciesInfo(&managedPoliciesInfo, policiesNs)
	if len(managedPoliciesInfo.duplicatedPoliciesNs) != 0 {
		return false, managedPoliciesInfo, nil
	}

	// Go through the managedPolicies in the CR, make sure they exist and save them to the upgrade's status together with
	// their namespace.
	var managedPoliciesForUpgrade []ranv1alpha1.ManagedPolicyForUpgrade
	var managedPoliciesCompliantBeforeUpgrade []string
	clusterGroupUpgrade.Status.ManagedPoliciesNs = make(map[string]string)
	clusterGroupUpgrade.Status.ManagedPoliciesContent = make(map[string]string)

	for _, managedPolicyName := range clusterGroupUpgrade.Spec.ManagedPolicies {
		if policyEnforce[managedPolicyName] {
			r.Log.Info("Ignoring policy " + managedPolicyName + " with remediationAction enforce")
			continue
		}

		if managedPolicyNamespace, ok := policyMap[managedPolicyName]; ok {
			// Make sure the parent policy exists and nothing happened between querying the child policies above and now.
			foundPolicy, err := r.getPolicyByName(ctx, managedPolicyName, managedPolicyNamespace)

			if err != nil {
				// If the parent policy was not found, add its name to the list of missing policies.
				if errors.IsNotFound(err) {
					managedPoliciesInfo.missingPolicies = append(managedPoliciesInfo.missingPolicies, managedPolicyName)
					continue
				} else {
					// If another error happened, return it.
					return false, managedPoliciesInfo, err
				}
			}

			// If the parent policy has invalid hub template, add its name to the list of invalid policies.
			if policyInvalidHubTmpl[managedPolicyName] {
				r.Log.Error(&utils.PolicyErr{ObjName: managedPolicyName, ErrMsg: utils.PlcHasHubTmplErr}, "Policy is invalid")
				managedPoliciesInfo.invalidPolicies = append(managedPoliciesInfo.invalidPolicies, managedPolicyName)
				continue
			}

			// If the parent policy is not valid due to missing field, add its name to the list of invalid policies.
			containsStatus, policyErr := utils.InspectPolicyObjects(foundPolicy)
			if policyErr != nil {
				r.Log.Error(policyErr, "Policy is invalid")
				managedPoliciesInfo.invalidPolicies = append(managedPoliciesInfo.invalidPolicies, managedPolicyName)
				continue
			}

			if !containsStatus {
				// Check the policy has at least one of the clusters from the CR in NonCompliant state.
				clustersNonCompliantWithPolicy := r.getClustersNonCompliantWithPolicy(clusters, foundPolicy)

				if len(clustersNonCompliantWithPolicy) == 0 {
					managedPoliciesCompliantBeforeUpgrade = append(managedPoliciesCompliantBeforeUpgrade, foundPolicy.GetName())
					managedPoliciesInfo.compliantPolicies = append(managedPoliciesInfo.compliantPolicies, foundPolicy)
					continue
				}
			}
			// Update the info on the policies used in the upgrade.
			newPolicyInfo := ranv1alpha1.ManagedPolicyForUpgrade{Name: managedPolicyName, Namespace: managedPolicyNamespace}
			managedPoliciesForUpgrade = append(managedPoliciesForUpgrade, newPolicyInfo)

			// Add the policy to the list of present policies and update the status with the policy's namespace.
			managedPoliciesInfo.presentPolicies = append(managedPoliciesInfo.presentPolicies, foundPolicy)
			clusterGroupUpgrade.Status.ManagedPoliciesNs[managedPolicyName] = managedPolicyNamespace
		} else {
			managedPoliciesInfo.missingPolicies = append(managedPoliciesInfo.missingPolicies, managedPolicyName)
		}
	}

	if len(managedPoliciesForUpgrade) > 0 {
		clusterGroupUpgrade.Status.ManagedPoliciesForUpgrade = managedPoliciesForUpgrade
	}
	if len(managedPoliciesCompliantBeforeUpgrade) > 0 {
		clusterGroupUpgrade.Status.ManagedPoliciesCompliantBeforeUpgrade = managedPoliciesCompliantBeforeUpgrade
	}

	// If there are missing managed policies, return.
	if len(managedPoliciesInfo.missingPolicies) != 0 || len(managedPoliciesInfo.invalidPolicies) != 0 {
		return false, managedPoliciesInfo, nil
	}

	return true, managedPoliciesInfo, nil
}

func (r *ClusterGroupUpgradeReconciler) copyManagedInformPolicy(
	ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, managedPolicy *unstructured.Unstructured) (string, error) {

	// Create a new unstructured variable to keep all the information for the new policy.
	newPolicy := &unstructured.Unstructured{}

	// Set new policy name, namespace, group, kind and version.
	name := utils.GetResourceName(clusterGroupUpgrade, managedPolicy.GetName())
	newPolicy.SetName(name)
	newPolicy.SetNamespace(clusterGroupUpgrade.GetNamespace())
	newPolicy.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "policy.open-cluster-management.io",
		Kind:    "Policy",
		Version: "v1",
	})

	// Set new policy labels.
	labels := managedPolicy.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	labels["app"] = "openshift-cluster-group-upgrades"
	labels["openshift-cluster-group-upgrades/clusterGroupUpgrade"] = clusterGroupUpgrade.Name
	labels["openshift-cluster-group-upgrades/parentPolicyName"] = managedPolicy.GetName()
	labels[utils.ExcludeFromClusterBackup] = "true"
	newPolicy.SetLabels(labels)

	// Set new policy annotations - copy them from the managed policy.
	annotations := managedPolicy.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}
	annotations[utils.DesiredResourceName] = name
	newPolicy.SetAnnotations(annotations)

	// Set new policy remediationAction.
	newPolicy.Object["spec"] = managedPolicy.Object["spec"]
	specObject := newPolicy.Object["spec"].(map[string]interface{})
	specObject["remediationAction"] = utils.RemediationActionEnforce

	// Update the ConfigurationPolicy of the new policy.
	err := r.updateConfigurationPolicyForCopiedPolicy(ctx, clusterGroupUpgrade, newPolicy, managedPolicy.GetName(), managedPolicy.GetNamespace())
	if err != nil {
		return "", err
	}

	// Create the new policy in the desired namespace.
	err = r.createNewPolicyFromStructure(ctx, clusterGroupUpgrade, newPolicy)
	if err != nil {
		r.Log.Info("Error creating policy", "err", err)
		return "", err
	}
	return newPolicy.GetName(), nil
}

func (r *ClusterGroupUpgradeReconciler) updateConfigurationPolicyForCopiedPolicy(
	ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, policy *unstructured.Unstructured, managedPolicyName, managedPolicyNamespace string) error {

	// Go through the policy policy-templates.
	policySpec := policy.Object["spec"].(map[string]interface{})
	policyTemplates := policySpec["policy-templates"].([]interface{})
	for _, plcTmpl := range policyTemplates {
		// Update the metadata name of the ConfigurationPolicy.
		plcTmplDef := plcTmpl.(map[string]interface{})["objectDefinition"].(map[string]interface{})
		metadata := plcTmplDef["metadata"]
		r.updateConfigurationPolicyName(clusterGroupUpgrade, metadata)

		// Ensure the resources referenced in the hub template policy exist if applicable
		plcTmplDefSpec := plcTmplDef["spec"].(map[string]interface{})
		configPlcTmpls := plcTmplDefSpec["object-templates"]
		resolvedconfigPlcTmpls, err := r.updateConfigurationPolicyHubTemplate(
			ctx, configPlcTmpls, clusterGroupUpgrade.GetNamespace(), managedPolicyName, managedPolicyNamespace)
		if err != nil {
			return err
		}
		plcTmplDefSpec["object-templates"] = resolvedconfigPlcTmpls
	}

	return nil
}

func (r *ClusterGroupUpgradeReconciler) updateConfigurationPolicyHubTemplate(
	ctx context.Context, objectTmpl interface{}, cguNamespace, managedPolicyName, managedPolicyNamespace string) (interface{}, error) {
	// Process only if the managed policy is not created in the CGU namespace
	if managedPolicyNamespace == cguNamespace {
		return objectTmpl, nil
	}

	tmplResolver := &utils.TemplateResolver{
		Client:          r.Client,
		Ctx:             ctx,
		TargetNamespace: cguNamespace,
		PolicyName:      managedPolicyName,
		PolicyNamespace: managedPolicyNamespace,
	}

	resolvedObjectTmpl, err := tmplResolver.ProcessHubTemplateFunctions(objectTmpl)
	if err != nil {
		return resolvedObjectTmpl, err
	}

	return resolvedObjectTmpl, nil
}

func (r *ClusterGroupUpgradeReconciler) updateConfigurationPolicyName(
	clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, metadata interface{}) {

	metadataContent := metadata.(map[string]interface{})
	name := utils.GetResourceName(clusterGroupUpgrade, metadataContent["name"].(string))
	safeName := utils.GetSafeResourceName(name, clusterGroupUpgrade, utils.MaxPolicyNameLength, 0)
	metadataContent["name"] = safeName
}

func (r *ClusterGroupUpgradeReconciler) createNewPolicyFromStructure(
	ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, policy *unstructured.Unstructured) error {

	name := policy.GetName()
	safeName := utils.GetSafeResourceName(name, clusterGroupUpgrade, utils.MaxPolicyNameLength, len(policy.GetNamespace())+1)
	policy.SetName(safeName)
	if err := controllerutil.SetControllerReference(clusterGroupUpgrade, policy, r.Scheme); err != nil {
		return err
	}
	existingPolicy := &unstructured.Unstructured{}
	existingPolicy.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "policy.open-cluster-management.io",
		Kind:    "Policy",
		Version: "v1",
	})
	err := r.Client.Get(ctx, client.ObjectKey{
		Name:      safeName,
		Namespace: clusterGroupUpgrade.Namespace,
	}, existingPolicy)

	if err != nil {
		if errors.IsNotFound(err) {
			err = r.Client.Create(ctx, policy)
			if err != nil {
				return err
			}
		} else {
			return err
		}
	} else {
		policy.SetResourceVersion(existingPolicy.GetResourceVersion())
		err = r.Client.Update(ctx, policy)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *ClusterGroupUpgradeReconciler) ensureBatchPlacementRule(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, policyName string, managedPolicy *unstructured.Unstructured) (string, error) {

	name := utils.GetResourceName(clusterGroupUpgrade, managedPolicy.GetName()+"-placement")
	safeName := utils.GetSafeResourceName(name, clusterGroupUpgrade, utils.MaxObjectNameLength, 0)
	pr := r.newBatchPlacementRule(clusterGroupUpgrade, policyName, safeName, name)

	if err := controllerutil.SetControllerReference(clusterGroupUpgrade, pr, r.Scheme); err != nil {
		return "", err
	}

	foundPlacementRule := &unstructured.Unstructured{}
	foundPlacementRule.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "apps.open-cluster-management.io",
		Kind:    "PlacementRule",
		Version: "v1",
	})

	err := r.Client.Get(ctx, client.ObjectKey{
		Name:      safeName,
		Namespace: clusterGroupUpgrade.Namespace,
	}, foundPlacementRule)

	if err != nil {
		if errors.IsNotFound(err) {
			err = r.Client.Create(ctx, pr)
			if err != nil {
				return "", err
			}
		} else {
			return "", err
		}
	} else {
		pr.SetResourceVersion(foundPlacementRule.GetResourceVersion())
		err = r.Client.Update(ctx, pr)
		if err != nil {
			return "", err
		}
	}
	return safeName, nil
}

func (r *ClusterGroupUpgradeReconciler) newBatchPlacementRule(clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, policyName, placementRuleName, desiredName string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.Object = map[string]interface{}{
		"metadata": map[string]interface{}{
			"name":      placementRuleName,
			"namespace": clusterGroupUpgrade.Namespace,
			"labels": map[string]interface{}{
				"app": "openshift-cluster-group-upgrades",
				"openshift-cluster-group-upgrades/clusterGroupUpgrade": clusterGroupUpgrade.Name,
				"openshift-cluster-group-upgrades/forPolicy":           policyName,
				utils.ExcludeFromClusterBackup:                         "true",
			},
			"annotations": map[string]interface{}{
				utils.DesiredResourceName: desiredName,
			},
		},
		"spec": map[string]interface{}{
			"clusterConditions": []map[string]interface{}{
				{
					"type":   "ManagedClusterConditionAvailable",
					"status": "True",
				},
			},
			"clusterReplicas": 0,
		},
	}

	u.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "apps.open-cluster-management.io",
		Kind:    "PlacementRule",
		Version: "v1",
	})

	return u
}

/*
getNextNonCompliantPolicyForCluster goes through all the policies in the managedPolicies list, starting with the

	policy index for the requested cluster and returns the index of the first policy that has the cluster as NonCompliant.

	returns: policyIndex the index of the next policy for which the cluster is NonCompliant or -1 if no policy found
	         error/nil
*/
func (r *ClusterGroupUpgradeReconciler) getNextNonCompliantPolicyForCluster(
	ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, clusterName string, startIndex int) (int, bool, error) {
	isSoaking := false
	numberOfPolicies := len(clusterGroupUpgrade.Status.ManagedPoliciesForUpgrade)
	currentPolicyIndex := startIndex
	for ; currentPolicyIndex < numberOfPolicies; currentPolicyIndex++ {
		// Get the name of the managed policy matching the current index.
		currentManagedPolicyInfo := utils.GetManagedPolicyForUpgradeByIndex(currentPolicyIndex, clusterGroupUpgrade)
		currentManagedPolicy, err := r.getPolicyByName(ctx, currentManagedPolicyInfo.Name, currentManagedPolicyInfo.Namespace)
		if err != nil {
			return currentPolicyIndex, isSoaking, err
		}

		// Check if current cluster is compliant or not for its current managed policy.
		clusterStatus := r.getClusterComplianceWithPolicy(clusterName, currentManagedPolicy)

		// If the cluster is compliant for the policy or if the cluster is not matched with the policy,
		// move to the next policy index.
		if clusterStatus == utils.ClusterNotMatchedWithPolicy {
			continue
		}

		if clusterStatus == utils.ClusterStatusCompliant {
			_, ok := clusterGroupUpgrade.Status.Status.CurrentBatchRemediationProgress[clusterName]
			if !ok {
				continue
			}
			shouldSoak, err := utils.ShouldSoak(currentManagedPolicy, clusterGroupUpgrade.Status.Status.CurrentBatchRemediationProgress[clusterName].FirstCompliantAt)
			if err != nil {
				r.Log.Info(err.Error())
				continue
			}
			if !shouldSoak {
				continue
			}

			if clusterGroupUpgrade.Status.Status.CurrentBatchRemediationProgress[clusterName].FirstCompliantAt.IsZero() {
				clusterGroupUpgrade.Status.Status.CurrentBatchRemediationProgress[clusterName].FirstCompliantAt = metav1.Now()
			}
			isSoaking = true
			r.Log.Info("Policy is compliant but should be soaked", "cluster name", clusterName, "policyName", currentManagedPolicy.GetName())
			break
		}

		if clusterStatus == utils.ClusterStatusNonCompliant {
			break
		}
	}

	return currentPolicyIndex, isSoaking, nil
}

/*
isUpgradeComplete checks if there is at least one managed policy left for which at least one cluster in the

	batch is NonCompliant.

	returns: true/false if the upgrade is complete
	         error/nil
*/
func (r *ClusterGroupUpgradeReconciler) isUpgradeComplete(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) (bool, bool, error) {
	isBatchComplete, isSoaking, err := r.getNextRemediationPoliciesForBatch(ctx, r.Client, clusterGroupUpgrade)
	if err != nil {
		return false, false, err
	}

	if isBatchComplete {
		// Check previous batches
		for i := 0; i < len(clusterGroupUpgrade.Status.RemediationPlan)-1; i++ {
			for _, batchClusterName := range clusterGroupUpgrade.Status.RemediationPlan[i] {
				// Start with policy index 0 as we don't keep progress info from previous batches
				nextNonCompliantPolicyIndex, isSoaking, err := r.getNextNonCompliantPolicyForCluster(ctx, clusterGroupUpgrade, batchClusterName, 0)
				if err != nil || nextNonCompliantPolicyIndex < len(clusterGroupUpgrade.Status.ManagedPoliciesForUpgrade) {
					return false, isSoaking, err
				}
			}
		}
	} else {
		return false, isSoaking, nil
	}
	return true, isSoaking, nil
}

func (r *ClusterGroupUpgradeReconciler) ensureBatchPlacementBinding(
	ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, policyName, placementRuleName string, managedPolicy *unstructured.Unstructured) error {

	name := utils.GetResourceName(clusterGroupUpgrade, managedPolicy.GetName()+"-placement")
	safeName := utils.GetSafeResourceName(name, clusterGroupUpgrade, utils.MaxObjectNameLength, 0)
	// Ensure batch placement bindings.
	pb := r.newBatchPlacementBinding(clusterGroupUpgrade, policyName, placementRuleName, safeName, name)

	if err := controllerutil.SetControllerReference(clusterGroupUpgrade, pb, r.Scheme); err != nil {
		return err
	}

	foundPlacementBinding := &unstructured.Unstructured{}
	foundPlacementBinding.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "policy.open-cluster-management.io",
		Kind:    "PlacementBinding",
		Version: "v1",
	})
	err := r.Client.Get(ctx, client.ObjectKey{
		Name:      safeName,
		Namespace: clusterGroupUpgrade.Namespace,
	}, foundPlacementBinding)

	if err != nil {
		if errors.IsNotFound(err) {
			err = r.Client.Create(ctx, pb)
			if err != nil {
				return err
			}
		} else {
			return err
		}
	} else {
		pb.SetResourceVersion(foundPlacementBinding.GetResourceVersion())
		err = r.Client.Update(ctx, pb)
		if err != nil {
			return err
		}
	}

	return nil
}

func (r *ClusterGroupUpgradeReconciler) newBatchPlacementBinding(clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade,
	policyName, placementRuleName, placementBindingName, desiredName string) *unstructured.Unstructured {

	var subjects []map[string]interface{}

	subject := make(map[string]interface{})
	subject["name"] = policyName
	subject["kind"] = "Policy"
	subject["apiGroup"] = "policy.open-cluster-management.io"
	subjects = append(subjects, subject)

	u := &unstructured.Unstructured{}
	u.Object = map[string]interface{}{
		"metadata": map[string]interface{}{
			"name":      placementBindingName,
			"namespace": clusterGroupUpgrade.Namespace,
			"labels": map[string]interface{}{
				"app": "openshift-cluster-group-upgrades",
				"openshift-cluster-group-upgrades/clusterGroupUpgrade": clusterGroupUpgrade.Name,
				utils.ExcludeFromClusterBackup:                         "true",
			},
			"annotations": map[string]interface{}{
				utils.DesiredResourceName: desiredName,
			},
		},
		"placementRef": map[string]interface{}{
			"name":     placementRuleName,
			"kind":     "PlacementRule",
			"apiGroup": "apps.open-cluster-management.io",
		},
		"subjects": subjects,
	}
	u.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "policy.open-cluster-management.io",
		Kind:    "PlacementBinding",
		Version: "v1",
	})

	return u
}

func (r *ClusterGroupUpgradeReconciler) getPlacementRules(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, policyName *string) (*unstructured.UnstructuredList, error) {
	var placementRuleLabels = map[string]string{"openshift-cluster-group-upgrades/clusterGroupUpgrade": clusterGroupUpgrade.Name}
	if policyName != nil {
		placementRuleLabels["openshift-cluster-group-upgrades/forPolicy"] = *policyName
	}

	listOpts := []client.ListOption{
		client.InNamespace(clusterGroupUpgrade.Namespace),
		client.MatchingLabels(placementRuleLabels),
	}
	placementRulesList := &unstructured.UnstructuredList{}
	placementRulesList.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "apps.open-cluster-management.io",
		Kind:    "PlacementRuleList",
		Version: "v1",
	})
	if err := r.List(ctx, placementRulesList, listOpts...); err != nil {
		return nil, err
	}

	return placementRulesList, nil
}

func (r *ClusterGroupUpgradeReconciler) getPlacementBindings(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) (*unstructured.UnstructuredList, error) {
	var placementBindingLabels = map[string]string{"openshift-cluster-group-upgrades/clusterGroupUpgrade": clusterGroupUpgrade.Name}
	listOpts := []client.ListOption{
		client.InNamespace(clusterGroupUpgrade.Namespace),
		client.MatchingLabels(placementBindingLabels),
	}
	placementBindingsList := &unstructured.UnstructuredList{}
	placementBindingsList.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "policy.open-cluster-management.io",
		Kind:    "PlacementBindingList",
		Version: "v1",
	})
	if err := r.List(ctx, placementBindingsList, listOpts...); err != nil {
		return nil, err
	}

	return placementBindingsList, nil
}

func (r *ClusterGroupUpgradeReconciler) getCopiedPolicies(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) (*unstructured.UnstructuredList, error) {
	var policyLabels = map[string]string{"openshift-cluster-group-upgrades/clusterGroupUpgrade": clusterGroupUpgrade.Name}
	listOpts := []client.ListOption{
		client.InNamespace(clusterGroupUpgrade.Namespace),
		client.MatchingLabels(policyLabels),
	}
	policiesList := &unstructured.UnstructuredList{}
	policiesList.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "policy.open-cluster-management.io",
		Kind:    "PolicyList",
		Version: "v1",
	})
	if err := r.List(ctx, policiesList, listOpts...); err != nil {
		return nil, err
	}

	return policiesList, nil
}

func (r *ClusterGroupUpgradeReconciler) reconcileResources(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, managedPoliciesPresent []*unstructured.Unstructured) (bool, error) {
	// Reconcile resources
	isPolicyErr := false
	for _, managedPolicy := range managedPoliciesPresent {

		policyName, err := r.copyManagedInformPolicy(ctx, clusterGroupUpgrade, managedPolicy)
		if err != nil {
			if _, ok := err.(*utils.PolicyErr); ok {
				// If it's a policy error(i.e. unsupported hub template),
				// break the loop to execute updateChildResourceNamesInStatus
				// to update the CGU status with already created policies
				isPolicyErr = true
				break
			}
			return false, err
		}

		placementRuleName, err := r.ensureBatchPlacementRule(ctx, clusterGroupUpgrade, policyName, managedPolicy)
		if err != nil {
			return false, err
		}

		err = r.ensureBatchPlacementBinding(ctx, clusterGroupUpgrade, policyName, placementRuleName, managedPolicy)
		if err != nil {
			return false, err
		}
	}
	err := r.updateChildResourceNamesInStatus(ctx, clusterGroupUpgrade)
	return isPolicyErr, err
}

func (r *ClusterGroupUpgradeReconciler) getPolicyClusterStatus(policy *unstructured.Unstructured) []interface{} {
	policyName := policy.GetName()

	// Get the compliant status part of the policy.
	if policy.Object["status"] == nil {
		r.Log.Info("[getPolicyClusterStatus] Policy has its status missing", "policyName", policyName)
		return nil
	}

	statusObject := policy.Object["status"].(map[string]interface{})
	// If there is just one cluster in the policy's status that's missing it's compliance status, then the overall
	// policy compliance status will be missing. Log if the overall compliance status is missing, but continue.
	if statusObject["compliant"] == nil {
		r.Log.Info("[getPolicyClusterStatus] Policy has it's compliant status pending", "policyName", policyName)
	}

	// Get the policy's list of cluster compliance.
	statusCompliance := statusObject["status"]
	if statusCompliance == nil {
		r.Log.Info("[getPolicyClusterStatus] Policy has it's list of cluster statuses pending", "policyName", policyName)
		return nil
	}

	subStatus := statusCompliance.([]interface{})
	if subStatus == nil {
		r.Log.Info("[getPolicyClusterStatus] Policy is missing it's compliance status", "policyName", policyName)
		return nil
	}

	return subStatus
}

func (r *ClusterGroupUpgradeReconciler) getClustersNonCompliantWithPolicy(
	clusters []string,
	policy *unstructured.Unstructured) []string {

	var nonCompliantClusters []string

	for _, cluster := range clusters {
		compliance := r.getClusterComplianceWithPolicy(cluster, policy)
		if compliance != utils.ClusterStatusCompliant {
			nonCompliantClusters = append(nonCompliantClusters, cluster)
		}
	}
	r.Log.Info("[getClustersNonCompliantWithPolicy]", "policy: ", policy.GetName(), "clusters: ", nonCompliantClusters)
	return nonCompliantClusters
}

/*
	  getClusterComplianceWithPolicy returns the compliance of a certain cluster with a certain policy
	  based on a policy's status structure which is below. If a policy is bound to a placementRule, then
	  all the clusters bound to the policy will appear in status.status as either Compliant or NonCompliant.

	  status:
	    compliant: NonCompliant
	    placement:
	    - placementBinding: binding-policy1-common-cluster-version-policy
	      placementRule: placement-policy1-common-cluster-version-policy
	    status:
	    - clustername: spoke1
	      clusternamespace: spoke1
	      compliant: NonCompliant
	    - clustername: spoke4
	      clusternamespace: spoke4
	      compliant: NonCompliant

		returns: *string pointer to a string holding either Compliant/NonCompliant/NotMatchedWithPolicy
		         error
*/
func (r *ClusterGroupUpgradeReconciler) getClusterComplianceWithPolicy(
	clusterName string, policy *unstructured.Unstructured) string {
	// Get the status of the clusters matching the policy.
	subStatus := r.getPolicyClusterStatus(policy)
	if subStatus == nil {
		r.Log.Info(
			"[getClusterComplianceWithPolicy] Policy is missing its status, treat as NonCompliant")
		return utils.ClusterStatusNonCompliant
	}

	// Loop through all the clusters in the policy's compliance status.
	for _, crtSubStatusCrt := range subStatus {
		crtSubStatusMap := crtSubStatusCrt.(map[string]interface{})
		// If the cluster is Compliant, return true.
		if clusterName == crtSubStatusMap["clustername"].(string) {
			if crtSubStatusMap["compliant"] == utils.ClusterStatusCompliant {
				return utils.ClusterStatusCompliant
			} else if crtSubStatusMap["compliant"] == utils.ClusterStatusNonCompliant ||
				crtSubStatusMap["compliant"] == utils.ClusterStatusPending {
				// Treat pending as non-compliant
				return utils.ClusterStatusNonCompliant
			} else if crtSubStatusMap["compliant"] == nil {
				r.Log.Info(
					"[getClusterComplianceWithPolicy] Cluster is missing its compliance status, treat as NonCompliant",
					"clusterName", clusterName, "policyName", policy.GetName())
				return utils.ClusterStatusNonCompliant
			}
		}
	}
	return utils.ClusterNotMatchedWithPolicy
}

func (r *ClusterGroupUpgradeReconciler) getClustersNonCompliantWithManagedPolicies(clusters []string, managedPolicies []*unstructured.Unstructured) map[string]bool {
	clustersNonCompliantMap := make(map[string]bool)

	// clustersNonCompliantMap will be a map of the clusters present in the CR and wether they are NonCompliant with at
	// least one managed policy.
	for _, clusterName := range clusters {
		for _, managedPolicy := range managedPolicies {
			clusterCompliance := r.getClusterComplianceWithPolicy(clusterName, managedPolicy)

			if clusterCompliance == utils.ClusterStatusNonCompliant {
				// If the cluster is NonCompliant in this current policy mark it as such and move to the next cluster.
				clustersNonCompliantMap[clusterName] = true
				break
			}
		}
	}

	return clustersNonCompliantMap
}

func (r *ClusterGroupUpgradeReconciler) buildRemediationPlan(ctx context.Context,
	clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, clusters []string, managedPolicies []*unstructured.Unstructured) {
	// Get all clusters from the CR that are non compliant with at least one of the managedPolicies.
	clusterNonCompliantWithManagedPoliciesMap := r.getClustersNonCompliantWithManagedPolicies(clusters, managedPolicies)

	// Create remediation plan
	var remediationPlan [][]string
	isCanary := make(map[string]bool)
	if clusterGroupUpgrade.Spec.RemediationStrategy.Canaries != nil && len(clusterGroupUpgrade.Spec.RemediationStrategy.Canaries) > 0 {
		for _, canary := range clusterGroupUpgrade.Spec.RemediationStrategy.Canaries {
			if clusterNonCompliantWithManagedPoliciesMap[canary] {
				remediationPlan = append(remediationPlan, []string{canary})
				isCanary[canary] = true
			} else if *clusterGroupUpgrade.Spec.Enable {
				r.takeActionsAfterCompletion(ctx, clusterGroupUpgrade, canary)
			}
		}
	}

	var batch []string
	clusterCount := 0
	for i := 0; i < len(clusters); i++ {
		cluster := clusters[i]
		if !isCanary[cluster] {
			if clusterNonCompliantWithManagedPoliciesMap[cluster] {
				batch = append(batch, cluster)
				clusterCount++
			} else if *clusterGroupUpgrade.Spec.Enable {
				r.takeActionsAfterCompletion(ctx, clusterGroupUpgrade, cluster)
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

/*
checkDuplicateChildResources looks up the name and desired name of the new resource in the list of resource names and the safe name map, before

	adding the names to them. If duplicate (with same desired name annotation value) resource is found, it gets deleted, i.e. the new one takes precedence.

	returns: the updated childResourceNameList
*/
func (r *ClusterGroupUpgradeReconciler) checkDuplicateChildResources(ctx context.Context, safeNameMap map[string]string, childResourceNames []string, newResource *unstructured.Unstructured) ([]string, error) {
	if desiredName, ok := newResource.GetAnnotations()[utils.DesiredResourceName]; ok {
		if safeName, ok := safeNameMap[desiredName]; ok {
			if newResource.GetName() != safeName {
				// Found an object with the same object name in annotation but different from our records in the names map
				// This could happen when reconcile calls work on a stale version of CGU right after a status update from a previous reconcile
				// Or the controller pod fails to update the status after creating objects, e.g. node failure
				// Remove it as we have created a new one and updated the map
				r.Log.Info("[checkDuplicateChildResources] clean up stale child resource", "name", newResource.GetName(), "kind", newResource.GetKind())
				err := r.Client.Delete(ctx, newResource)
				if err != nil {
					return childResourceNames, err
				}
				return childResourceNames, nil
			}
		} else {
			safeNameMap[desiredName] = newResource.GetName()
		}
	}
	childResourceNames = append(childResourceNames, newResource.GetName())
	return childResourceNames, nil
}

func (r *ClusterGroupUpgradeReconciler) updateChildResourceNamesInStatus(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) error {
	placementRules, err := r.getPlacementRules(ctx, clusterGroupUpgrade, nil)
	if err != nil {
		return err
	}

	placementRuleNames := make([]string, 0)
	for _, placementRule := range placementRules.Items {
		placementRuleNames, err = r.checkDuplicateChildResources(ctx, clusterGroupUpgrade.Status.SafeResourceNames, placementRuleNames, &placementRule)
		if err != nil {
			return err
		}
	}
	clusterGroupUpgrade.Status.PlacementRules = placementRuleNames

	placementBindings, err := r.getPlacementBindings(ctx, clusterGroupUpgrade)
	if err != nil {
		return err
	}
	placementBindingNames := make([]string, 0)
	for _, placementBinding := range placementBindings.Items {
		placementBindingNames, err = r.checkDuplicateChildResources(ctx, clusterGroupUpgrade.Status.SafeResourceNames, placementBindingNames, &placementBinding)
		if err != nil {
			return err
		}
	}
	clusterGroupUpgrade.Status.PlacementBindings = placementBindingNames

	copiedPolicies, err := r.getCopiedPolicies(ctx, clusterGroupUpgrade)
	if err != nil {
		return err
	}
	copiedPolicyNames := make([]string, 0)
	for _, policy := range copiedPolicies.Items {
		copiedPolicyNames, err = r.checkDuplicateChildResources(ctx, clusterGroupUpgrade.Status.SafeResourceNames, copiedPolicyNames, &policy)
		if err != nil {
			return err
		}
	}
	clusterGroupUpgrade.Status.CopiedPolicies = copiedPolicyNames
	return err
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

		// If we find a blocking CR that does not contain a true succeeded condition then we add it to the list.
		if !meta.IsStatusConditionTrue(cgu.Status.Conditions, string(utils.ConditionTypes.Succeeded)) {
			blockingCRsNotCompleted = append(blockingCRsNotCompleted, cgu.Name)
		}
	}

	r.Log.Info("[blockingCRsNotCompleted]", "blockingCRs", blockingCRsNotCompleted)
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
			err := utils.FinalMultiCloudObjectCleanup(ctx, r.Client, clusterGroupUpgrade)
			if err != nil {
				return utils.StopReconciling, err
			}

			err = r.jobAndViewFinalCleanup(ctx, clusterGroupUpgrade)
			if err != nil {
				return utils.StopReconciling, err
			}

			// Remove cguFinalizer. Once all finalizers have been removed, the object will be deleted.
			controllerutil.RemoveFinalizer(clusterGroupUpgrade, utils.CleanupFinalizer)
			err = r.Update(ctx, clusterGroupUpgrade)
			if err != nil {
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

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterGroupUpgradeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.Recorder = mgr.GetEventRecorderFor("ClusterGroupUpgrade")

	placementRuleUnstructured := &unstructured.Unstructured{}
	placementRuleUnstructured.SetGroupVersionKind(schema.GroupVersionKind{
		Kind:    "PlacementRule",
		Group:   "apps.open-cluster-management.io",
		Version: "v1",
	})

	placementBindingUnstructured := &unstructured.Unstructured{}
	placementBindingUnstructured.SetGroupVersionKind(schema.GroupVersionKind{
		Kind:    "PlacementBinding",
		Group:   "policy.open-cluster-management.io",
		Version: "v1",
	})

	policyUnstructured := &unstructured.Unstructured{}
	policyUnstructured.SetGroupVersionKind(schema.GroupVersionKind{
		Kind:    "Policy",
		Group:   "policy.open-cluster-management.io",
		Version: "v1",
	})

	maxConcurrency, set := os.LookupEnv(utils.CGUControllerWorkerCountEnv)
	var maxConcurrentReconciles int
	var err error
	if set {
		maxConcurrentReconciles, err = strconv.Atoi(maxConcurrency)
		if err != nil {
			r.Log.Info("Invalid value %s for %s, using the default: %i", maxConcurrency, utils.CGUControllerWorkerCountEnv, utils.DefaultCGUControllerWorkerCount)
			maxConcurrentReconciles = utils.DefaultCGUControllerWorkerCount
		}
	} else {
		maxConcurrentReconciles = utils.DefaultCGUControllerWorkerCount
	}

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
		Owns(policyUnstructured, builder.WithPredicates(predicate.Funcs{
			UpdateFunc: func(e event.UpdateEvent) bool {
				// Generation is only updated on spec changes (also on deletion),
				// not metadata or status
				oldGeneration := e.ObjectOld.GetGeneration()
				newGeneration := e.ObjectNew.GetGeneration()
				// status update only for parent policies
				return oldGeneration == newGeneration
			},
			CreateFunc:  func(ce event.CreateEvent) bool { return false },
			GenericFunc: func(ge event.GenericEvent) bool { return false },
			DeleteFunc:  func(de event.DeleteEvent) bool { return false },
		})).
		WithOptions(controller.Options{MaxConcurrentReconciles: maxConcurrentReconciles}).
		Complete(r)
}
