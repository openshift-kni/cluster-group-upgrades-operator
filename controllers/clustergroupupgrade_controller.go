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
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	viewv1beta1 "github.com/open-cluster-management/multicloud-operators-foundation/pkg/apis/view/v1beta1"
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

//+kubebuilder:rbac:groups=ran.openshift.io,resources=clustergroupupgrades,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=ran.openshift.io,resources=clustergroupupgrades/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=ran.openshift.io,resources=clustergroupupgrades/finalizers,verbs=update
//+kubebuilder:rbac:groups=apps.open-cluster-management.io,resources=placementrules,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=policy.open-cluster-management.io,resources=placementbindings,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=policy.open-cluster-management.io,resources=policies,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=cluster.open-cluster-management.io,resources=managedclusters,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=action.open-cluster-management.io,resources=managedclusteractions,verbs=create;update;delete;get;list;watch;patch
//+kubebuilder:rbac:groups=view.open-cluster-management.io,resources=managedclusterviews,verbs=create;update;delete;get;list;watch;patch
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch
//+kubebuilder:rbac:groups=operators.coreos.com,resources=clusterserviceversions,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the ClusterGroupUpgrade object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.8.3/pkg/reconcile
//nolint:gocyclo // TODO: simplify this function
func (r *ClusterGroupUpgradeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = r.Log.WithValues("ClusterGroupUpgrade", req.NamespacedName)

	clusterGroupUpgrade := &ranv1alpha1.ClusterGroupUpgrade{}
	err := r.Get(ctx, req.NamespacedName, clusterGroupUpgrade)
	if err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		r.Log.Error(err, "Failed to get ClusterGroupUpgrade")
		return ctrl.Result{}, err
	}

	reconcileTime, err := r.handleCguFinalizer(ctx, clusterGroupUpgrade)
	if err != nil {
		return ctrl.Result{}, err
	}
	if reconcileTime == utils.ReconcileNow {
		return ctrl.Result{Requeue: true}, nil
	} else if reconcileTime == utils.StopReconciling {
		return ctrl.Result{}, nil
	}

	reconcile, err := r.validateCR(ctx, clusterGroupUpgrade)
	if err != nil {
		return ctrl.Result{}, err
	}
	if reconcile {
		return ctrl.Result{Requeue: true}, nil
	}

	nextReconcile := ctrl.Result{}

	err = r.reconcileBackup(ctx, clusterGroupUpgrade)
	if err != nil {
		r.Log.Error(err, "reconcileBackup error")
		return ctrl.Result{}, err
	}

	if clusterGroupUpgrade.Status.Backup != nil {
		for _, v := range clusterGroupUpgrade.Status.Backup.Status {
			//nolint
			if v == BackupStatePreparingToStart || v == BackupStateStarting || v == BackupStateActive {
				requeueAfter := 30 * time.Second
				nextReconcile = ctrl.Result{RequeueAfter: requeueAfter}
				err = r.updateStatus(ctx, clusterGroupUpgrade)
				if err != nil {
					return ctrl.Result{}, err
				}
				return nextReconcile, nil
			}
		}
	}

	if clusterGroupUpgrade.Status.Backup == nil || meta.IsStatusConditionTrue(clusterGroupUpgrade.Status.Conditions, BackupStateDone) {
		err = r.reconcilePrecaching(ctx, clusterGroupUpgrade)
		if err != nil {
			r.Log.Error(err, "reconcilePrecaching error")
			return ctrl.Result{}, err
		}
		for _, v := range clusterGroupUpgrade.Status.Precaching.Status {
			//nolint
			if v == PrecacheStatePreparingToStart || v == PrecacheStateStarting {
				requeueAfter := 30 * time.Second
				nextReconcile = ctrl.Result{RequeueAfter: requeueAfter}
				err = r.updateStatus(ctx, clusterGroupUpgrade)
				if err != nil {
					return ctrl.Result{}, err
				}
				return nextReconcile, nil
			}
		}

		readyCondition := meta.FindStatusCondition(clusterGroupUpgrade.Status.Conditions, "Ready")

		if readyCondition == nil {
			meta.SetStatusCondition(&clusterGroupUpgrade.Status.Conditions, metav1.Condition{
				Type:    "Ready",
				Status:  metav1.ConditionFalse,
				Reason:  "UpgradeNotStarted",
				Message: "The ClusterGroupUpgrade CR is not enabled",
			})
		} else if readyCondition.Status == metav1.ConditionFalse {
			if readyCondition.Reason == "PrecachingRequired" {
				requeueAfter := 5 * time.Minute
				nextReconcile = ctrl.Result{RequeueAfter: requeueAfter}
			} else if readyCondition.Reason == "UpgradeNotStarted" || readyCondition.Reason == utils.CannotStart {
				// Before starting the upgrade check that all the managed policies exist.
				allManagedPoliciesExist, managedPoliciesMissing, managedPoliciesPresent, err :=
					r.doManagedPoliciesExist(ctx, clusterGroupUpgrade, true)
				if err != nil {
					return ctrl.Result{}, err
				}

				if allManagedPoliciesExist {
					// Build the upgrade batches.
					err = r.buildRemediationPlan(ctx, clusterGroupUpgrade, managedPoliciesPresent)
					if err != nil {
						return ctrl.Result{}, err
					}

					// Set default values for status reason and message.
					var statusReason, statusMessage string
					statusReason = "UpgradeNotStarted"
					statusMessage = "The ClusterGroupUpgrade CR is not enabled"
					requeueAfter := 5 * time.Minute

					// If the remediation plan is empty, update the status.
					if clusterGroupUpgrade.Status.RemediationPlan == nil {
						statusReason = "UpgradeCompleted"
						statusMessage = "The ClusterGroupUpgrade CR has all clusters already compliant with the specified managed policies"
						nextReconcile = ctrl.Result{RequeueAfter: requeueAfter}
					} else {
						// Create the needed resources for starting the upgrade.
						err = r.reconcileResources(ctx, clusterGroupUpgrade, managedPoliciesPresent)
						if err != nil {
							return ctrl.Result{}, err
						}
						err = r.processManagedPolicyForUpgradeContent(ctx, clusterGroupUpgrade, managedPoliciesPresent)
						if err != nil {
							return ctrl.Result{}, err
						}

						if *clusterGroupUpgrade.Spec.Enable {
							// Check if there are any CRs that are blocking the start of the current one and are not yet completed.
							blockingCRsNotCompleted, blockingCRsMissing, err := r.blockingCRsNotCompleted(ctx, clusterGroupUpgrade)
							if err != nil {
								return ctrl.Result{}, err
							}

							if len(blockingCRsMissing) > 0 {
								// If there are blocking CRs missing, update the message to show which those are.
								statusReason = utils.CannotStart
								statusMessage = fmt.Sprintf("The ClusterGroupUpgrade CR has blocking CRs that are missing: %s", blockingCRsMissing)
								requeueAfter := 1 * time.Minute
								nextReconcile = ctrl.Result{RequeueAfter: requeueAfter}
							} else if len(blockingCRsNotCompleted) > 0 {
								// If there are blocking CRs that are not completed, then the upgrade can't start.
								statusReason = utils.CannotStart
								statusMessage = fmt.Sprintf("The ClusterGroupUpgrade CR is blocked by other CRs that have not yet completed: %s", blockingCRsNotCompleted)
								requeueAfter := 1 * time.Minute
								nextReconcile = ctrl.Result{RequeueAfter: requeueAfter}
							} else {
								// There are no blocking CRs, continue with the upgrade process.
								// Take actions before starting upgrade.
								err := r.takeActionsBeforeEnable(ctx, clusterGroupUpgrade)
								if err != nil {
									return ctrl.Result{}, err
								}
								// Start the upgrade.
								statusReason = "UpgradeNotCompleted"
								statusMessage = "The ClusterGroupUpgrade CR has upgrade policies that are still non compliant"
								clusterGroupUpgrade.Status.Status.StartedAt = metav1.Now()
								nextReconcile = ctrl.Result{Requeue: true}
							}
						} else {
							nextReconcile = ctrl.Result{RequeueAfter: requeueAfter}
						}
					}

					meta.SetStatusCondition(&clusterGroupUpgrade.Status.Conditions, metav1.Condition{
						Type:    "Ready",
						Status:  metav1.ConditionFalse,
						Reason:  statusReason,
						Message: statusMessage,
					})
					r.Log.Info("[Reconcile]", "RequeueAfter:", requeueAfter)
				} else {
					// If not all managedPolicies exist, update the Status accordingly.
					statusMessage := fmt.Sprintf("The ClusterGroupUpgrade CR has managed policies that are missing: %s", managedPoliciesMissing)
					meta.SetStatusCondition(&clusterGroupUpgrade.Status.Conditions, metav1.Condition{
						Type:    "Ready",
						Status:  metav1.ConditionFalse,
						Reason:  utils.CannotStart,
						Message: statusMessage,
					})
					requeueAfter := 1 * time.Minute
					nextReconcile = ctrl.Result{RequeueAfter: requeueAfter}
				}
			} else if readyCondition.Reason == "UpgradeNotCompleted" {
				r.Log.Info("[Reconcile]", "Status.CurrentBatch", clusterGroupUpgrade.Status.Status.CurrentBatch)

				// If the upgrade is just starting, set the batch to be shown in the Status as 1.
				if clusterGroupUpgrade.Status.Status.CurrentBatch == 0 {
					clusterGroupUpgrade.Status.Status.CurrentBatch = 1
				}

				if clusterGroupUpgrade.Status.Status.CurrentBatchStartedAt.IsZero() {
					nextReconcile = ctrl.Result{Requeue: true}
				} else {
					//nolint
					requeueAfter := clusterGroupUpgrade.Status.Status.CurrentBatchStartedAt.Add(5 * time.Minute).Sub(time.Now())
					if requeueAfter < 0 {
						requeueAfter = 5 * time.Minute
					}
					r.Log.Info("[Reconcile] Requeuing after", "requeueAfter", requeueAfter)
					nextReconcile = ctrl.Result{RequeueAfter: requeueAfter}
				}

				var isBatchComplete bool

				// At first, assume all clusters in the batch start applying policies starting with the first one.
				// Also set the start time of the current batch to the current timestamp.
				if clusterGroupUpgrade.Status.Status.CurrentBatchStartedAt.IsZero() {
					r.initializeRemediationPolicyForBatch(clusterGroupUpgrade)
					// Set the time for when the batch started updating.
					clusterGroupUpgrade.Status.Status.CurrentBatchStartedAt = metav1.Now()
				}

				// Check if current policies have become compliant and if new policies have to be applied.
				isBatchComplete, err := r.getNextRemediationPoliciesForBatch(ctx, clusterGroupUpgrade)
				if err != nil {
					return ctrl.Result{}, err
				}

				isUpgradeComplete := false
				if isBatchComplete {
					// If the upgrade is completed for the current batch, cleanup and move to the next.
					r.Log.Info("[Reconcile] Upgrade completed for batch", "batchIndex", clusterGroupUpgrade.Status.Status.CurrentBatch)
					r.cleanupPlacementRules(ctx, clusterGroupUpgrade)
					clusterGroupUpgrade.Status.Status.CurrentBatchStartedAt = metav1.Time{}

					// If the batch is complete and it was the last batch in the remediationPlan, then the whole upgrade is complete.
					// If we haven't reached the last batch yet, move to the next batch.
					if clusterGroupUpgrade.Status.Status.CurrentBatch == len(clusterGroupUpgrade.Status.RemediationPlan) {
						isUpgradeComplete = true
						r.Log.Info("Upgrade is complete")
					} else {
						clusterGroupUpgrade.Status.Status.CurrentBatch++
					}
				} else {
					// Add the needed cluster names to upgrade to the appropriate placement rule.
					reconcileSooner, err := r.remediateCurrentBatch(ctx, clusterGroupUpgrade)
					if err != nil {
						return ctrl.Result{}, err
					}
					if reconcileSooner {
						requeueAfter := 30 * time.Second
						r.Log.Info("[Reconcile] Requeuing after", "requeueAfter", requeueAfter)
						nextReconcile = ctrl.Result{RequeueAfter: requeueAfter}
					}

					// Check for batch timeout.
					batchTimeout := time.Duration(clusterGroupUpgrade.Spec.RemediationStrategy.Timeout/len(clusterGroupUpgrade.Status.RemediationPlan)) * time.Minute
					if !clusterGroupUpgrade.Status.Status.CurrentBatchStartedAt.IsZero() && time.Since(clusterGroupUpgrade.Status.Status.CurrentBatchStartedAt.Time) > batchTimeout {
						if len(clusterGroupUpgrade.Spec.RemediationStrategy.Canaries) != 0 &&
							clusterGroupUpgrade.Status.Status.CurrentBatch <= len(clusterGroupUpgrade.Spec.RemediationStrategy.Canaries) {
							r.Log.Info("Canaries batch timed out")
							meta.SetStatusCondition(&clusterGroupUpgrade.Status.Conditions, metav1.Condition{
								Type:    "Ready",
								Status:  metav1.ConditionFalse,
								Reason:  "UpgradeTimedOut",
								Message: "The ClusterGroupUpgrade CR policies are taking too long to complete",
							})
						} else {
							r.Log.Info("Batch upgrade timed out")
							if clusterGroupUpgrade.Status.Status.CurrentBatch < len(clusterGroupUpgrade.Status.RemediationPlan) {
								clusterGroupUpgrade.Status.Status.CurrentBatch++
							}
						}
					}
				}

				if isUpgradeComplete {
					err = r.precachingCleanup(ctx, clusterGroupUpgrade)
					if err != nil {
						msg := fmt.Sprint("Precaching cleanup failed with error:", err)
						r.Recorder.Event(clusterGroupUpgrade, corev1.EventTypeWarning, "PrecachingCleanupFailed", msg)
					}
					meta.SetStatusCondition(&clusterGroupUpgrade.Status.Conditions, metav1.Condition{
						Type:    "Ready",
						Status:  metav1.ConditionTrue,
						Reason:  "UpgradeCompleted",
						Message: "The ClusterGroupUpgrade CR has all clusters compliant with all the managed policies",
					})
				} else if !clusterGroupUpgrade.Status.Status.StartedAt.IsZero() && time.Since(clusterGroupUpgrade.Status.Status.StartedAt.Time) > time.Duration(clusterGroupUpgrade.Spec.RemediationStrategy.Timeout)*time.Minute {
					meta.SetStatusCondition(&clusterGroupUpgrade.Status.Conditions, metav1.Condition{
						Type:    "Ready",
						Status:  metav1.ConditionFalse,
						Reason:  "UpgradeTimedOut",
						Message: "The ClusterGroupUpgrade CR policies are taking too long to complete",
					})

				}
			} else if readyCondition.Reason == "UpgradeTimedOut" {
				r.Recorder.Event(clusterGroupUpgrade, corev1.EventTypeWarning, "UpgradeTimedOut", "The ClusterGroupUpgrade CR policies are taking too long to complete")
				nextReconcile = ctrl.Result{RequeueAfter: 60 * time.Minute}

				// If the upgrade timeout out,check if the upgrade has finished or not meanwhile.
				isUpgradeComplete, err := r.isUpgradeComplete(ctx, clusterGroupUpgrade)
				if err != nil {
					return ctrl.Result{}, err
				}

				if isUpgradeComplete {
					meta.SetStatusCondition(&clusterGroupUpgrade.Status.Conditions, metav1.Condition{
						Type:    "Ready",
						Status:  metav1.ConditionTrue,
						Reason:  "UpgradeCompleted",
						Message: "The ClusterGroupUpgrade CR has all upgrade policies compliant",
					})
				}
			}
		} else {
			r.Log.Info("Upgrade is completed")
			clusterGroupUpgrade.Status.Status.CurrentBatch = 0
			clusterGroupUpgrade.Status.Status.CurrentBatchStartedAt = metav1.Time{}
			clusterGroupUpgrade.Status.Status.CompletedAt = metav1.Now()

			// Take actions after upgrade is completed
			if err := r.takeActionsAfterCompletion(ctx, clusterGroupUpgrade); err != nil {
				return ctrl.Result{}, err
			}
		}

	}

	// Update status
	err = r.updateStatus(ctx, clusterGroupUpgrade)
	if err != nil {
		return ctrl.Result{}, err
	}

	return nextReconcile, nil
}

func (r *ClusterGroupUpgradeReconciler) initializeRemediationPolicyForBatch(
	clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) {

	clusterGroupUpgrade.Status.Status.CurrentRemediationPolicyIndex = make(map[string]int)
	batchIndex := clusterGroupUpgrade.Status.Status.CurrentBatch - 1

	// By default, don't set any policy index for any of the clusters in the batch.
	for _, batchClusterName := range clusterGroupUpgrade.Status.RemediationPlan[batchIndex] {
		clusterGroupUpgrade.Status.Status.CurrentRemediationPolicyIndex[batchClusterName] = utils.NoPolicyIndex
	}

	r.Log.Info("[initializeRemediationPolicyForBatch]",
		"CurrentRemediationPolicyIndex", clusterGroupUpgrade.Status.Status.CurrentRemediationPolicyIndex)
}

/*
  getNextRemediationPoliciesForBatch: Each cluster is checked against each policy in order. If the cluster is not bound
  to the policy, or if the cluster is already compliant with the policy, the indexing advances until a NonCompliant
  policy is found for the cluster or the end of the list is reached.

  The policy currently applied for each cluster has its index held in
  clusterGroupUpgrade.Status.Status.CurrentRemediationPolicyIndex[clusterName] (the index is used to range through the
  policies present in clusterGroupUpgrade.Status.ManagedPoliciesForUpgrade).

  returns: bool     : true if the batch is done upgrading; false if not
           error/nil: in case any error happens
*/
func (r *ClusterGroupUpgradeReconciler) getNextRemediationPoliciesForBatch(
	ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) (bool, error) {
	batchIndex := clusterGroupUpgrade.Status.Status.CurrentBatch - 1
	isBatchComplete := true

	for _, batchClusterName := range clusterGroupUpgrade.Status.RemediationPlan[batchIndex] {
		currentPolicyIndex := clusterGroupUpgrade.Status.Status.CurrentRemediationPolicyIndex[batchClusterName]
		if currentPolicyIndex == utils.AllPoliciesValidated {
			continue
		}

		// If it's the first time calling the function for the batch, move to policy index 0.
		if currentPolicyIndex == utils.NoPolicyIndex {
			currentPolicyIndex = 0
		}

		// Get the index of the next policy for which the cluster is NonCompliant.
		currentPolicyIndex, err := r.getNextNonCompliantPolicyForCluster(ctx, clusterGroupUpgrade, batchClusterName, currentPolicyIndex)
		if err != nil {
			return false, err
		}

		if currentPolicyIndex != utils.AllPoliciesValidated {
			isBatchComplete = false
		}

		// Update the policy index for the cluster.
		clusterGroupUpgrade.Status.Status.CurrentRemediationPolicyIndex[batchClusterName] = currentPolicyIndex
	}

	r.Log.Info("[getNextRemediationPoliciesForBatch]", "isBatchComplete", isBatchComplete)
	r.Log.Info("[getNextRemediationPoliciesForBatch]", "plan", clusterGroupUpgrade.Status.Status.CurrentRemediationPolicyIndex)
	return isBatchComplete, nil
}

/*
  remediateCurrentBatch:
  - steps through the remediationPolicyIndex and add the clusterNames to the corresponding
  placement rules in order so that at the end of a batch upgrade, all the copied policies are Compliant.
  - approves the needed InstallPlans for the Subscription type policies

  returns: error/nil
*/
func (r *ClusterGroupUpgradeReconciler) remediateCurrentBatch(
	ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) (bool, error) {

	reconcileSooner := false
	for clusterName, managedPolicyIndex := range clusterGroupUpgrade.Status.Status.CurrentRemediationPolicyIndex {
		if managedPolicyIndex == utils.AllPoliciesValidated || managedPolicyIndex == utils.NoPolicyIndex {
			continue
		}

		policyName := clusterGroupUpgrade.Name + "-" + clusterGroupUpgrade.Status.ManagedPoliciesForUpgrade[managedPolicyIndex].Name
		var clusterNameArr []string
		clusterNameArr = append(clusterNameArr, clusterName)
		err := r.updatePlacementRuleWithClusters(ctx, clusterGroupUpgrade, clusterNameArr, policyName)
		if err != nil {
			return reconcileSooner, err
		}

		// Approve needed InstallPlans.
		reconcileSooner, err = r.approveInstallPlan(ctx, clusterGroupUpgrade, managedPolicyIndex, clusterName)
		if err != nil {
			return reconcileSooner, err
		}
	}
	return reconcileSooner, nil
}

func (r *ClusterGroupUpgradeReconciler) approveInstallPlan(
	ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade,
	managedPolicyIndex int, clusterName string) (bool, error) {
	managedPolicyName := clusterGroupUpgrade.Status.ManagedPoliciesForUpgrade[managedPolicyIndex].Name

	// If there is no content saved for the current managed policy, return.
	_, ok := clusterGroupUpgrade.Status.ManagedPoliciesContent[managedPolicyName]
	if !ok {
		r.Log.Info("[approveInstallPlan] No content for policy", "managedPolicyName", managedPolicyName)
		return false, nil
	}

	// If there is content saved for the current managed policy, retrieve it.
	policyContentArr := []ranv1alpha1.PolicyContent{}
	json.Unmarshal([]byte(clusterGroupUpgrade.Status.ManagedPoliciesContent[managedPolicyName]), &policyContentArr)

	multiCloudPendingStatus := false
	for _, policyContent := range policyContentArr {
		if policyContent.Kind != utils.PolicyTypeSubscription {
			continue
		}

		r.Log.Info("[approveInstallPlan] Attempt to approve install plan for subscription",
			"name", policyContent.Name, "in namespace", policyContent.Namespace)
		// Get the managedClusterView for the subscription contained in the current managedPolicy.
		// If missing, then return error.
		mcvName := utils.GetMultiCloudObjectName(clusterGroupUpgrade, policyContent.Kind, policyContent.Name)
		mcv := &viewv1beta1.ManagedClusterView{}
		if err := r.Get(ctx, types.NamespacedName{Name: mcvName, Namespace: clusterName}, mcv); err != nil {
			if errors.IsNotFound(err) {
				r.Log.Info("ManagedClusterView should have been present, but it was not found", "error", err.Error())
				continue
			} else {
				return false, err
			}
		}

		// If the specific managedClusterView was found, check that it's condition Reason is "GetResourceProcessing"
		installPlanStatus, err := utils.ProcessSubscriptionManagedClusterView(
			ctx, r.Client, clusterGroupUpgrade, clusterName, mcv)
		// If there is an error in trying to approve the install plan, just print the error and continue.
		if err != nil {
			r.Log.Info("An error occurred trying to approve install plan", "error", err.Error())
			continue
		}
		if installPlanStatus == utils.InstallPlanCannotBeApproved {
			r.Log.Info("InstallPlan for subscription could not be approved", "subscription name", policyContent.Name)
		} else if installPlanStatus == utils.InstallPlanWasApproved {
			r.Log.Info("InstallPlan for subscription was approved", "subscription name", policyContent.Name)
		} else if installPlanStatus == utils.MultiCloudPendingStatus {
			r.Log.Info("InstallPlan for subscription could not be approved due to a MultiCloud object pending status, "+
				"retry again later", "subscription name", policyContent.Name)
			multiCloudPendingStatus = true
		}
	}

	return multiCloudPendingStatus, nil
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

/* doManagedPoliciesExist checks that all the managedPolicies specified in the CR exist.
   returns: true/false                   if all the policies exist or not
            []string                     with the missing managed policy names
			[]*unstructured.Unstructured a list of the managedPolicies present on the system
			error
*/
func (r *ClusterGroupUpgradeReconciler) doManagedPoliciesExist(
	ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade,
	filterNonCompliantPolicies bool) (bool, []string, []*unstructured.Unstructured, error) {

	clusters, err := r.getAllClustersForUpgrade(ctx, clusterGroupUpgrade)
	if err != nil {
		return false, nil, nil, err
	}
	childPoliciesList, err := utils.GetChildPolicies(ctx, r.Client, clusters)
	if err != nil {
		return false, nil, nil, err
	}

	// Go through all the child policies and split the namespace from the policy name.
	// A child policy name has the name format parent_policy_namespace.parent_policy_name
	// The policy map we are creating will be of format {"policy_name": "policy_namespace"}
	policyMap := make(map[string]string)
	policyEnforce := make(map[string]bool)
	for _, childPolicy := range childPoliciesList {
		policyNameArr := strings.SplitN(childPolicy.Name, ".", 2)

		// Identify policies with remediationAction enforce to ignore
		if strings.EqualFold(string(childPolicy.Spec.RemediationAction), "enforce") {
			policyEnforce[policyNameArr[1]] = true
			continue
		}

		policyMap[policyNameArr[1]] = policyNameArr[0]
	}
	r.Log.Info("[doManagedPoliciesExist]", "policyMap", policyMap)

	// Go through the managedPolicies in the CR, make sure they exist and save them to the upgrade's status together with
	// their namespace.
	var managedPoliciesMissing []string
	var managedPoliciesPresent []*unstructured.Unstructured
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
					managedPoliciesMissing = append(managedPoliciesMissing, managedPolicyName)
					continue
				} else {
					// If another error happened, return it.
					return false, nil, nil, err
				}
			}

			if filterNonCompliantPolicies {
				// Check the policy has at least one of the clusters from the CR in NonCompliant state.
				clustersNonCompliantWithPolicy, err := r.getClustersNonCompliantWithPolicy(
					ctx, clusterGroupUpgrade, foundPolicy, true)
				if err != nil {
					return false, nil, nil, err
				}

				if len(clustersNonCompliantWithPolicy) == 0 {
					managedPoliciesCompliantBeforeUpgrade = append(managedPoliciesCompliantBeforeUpgrade, foundPolicy.GetName())
					continue
				}

				// Update the info on the policies used in the upgrade.
				newPolicyInfo := ranv1alpha1.ManagedPolicyForUpgrade{Name: managedPolicyName, Namespace: managedPolicyNamespace}
				managedPoliciesForUpgrade = append(managedPoliciesForUpgrade, newPolicyInfo)
			}
			// Add the policy to the list of present policies and update the status with the policy's namespace.
			managedPoliciesPresent = append(managedPoliciesPresent, foundPolicy)
			clusterGroupUpgrade.Status.ManagedPoliciesNs[managedPolicyName] = managedPolicyNamespace
		} else {
			managedPoliciesMissing = append(managedPoliciesMissing, managedPolicyName)
		}
	}

	if len(managedPoliciesForUpgrade) > 0 {
		clusterGroupUpgrade.Status.ManagedPoliciesForUpgrade = managedPoliciesForUpgrade
	}
	if len(managedPoliciesCompliantBeforeUpgrade) > 0 {
		clusterGroupUpgrade.Status.ManagedPoliciesCompliantBeforeUpgrade = managedPoliciesCompliantBeforeUpgrade
	}

	// If there are missing managed policies, return.
	if len(managedPoliciesMissing) != 0 {
		return false, managedPoliciesMissing, managedPoliciesPresent, nil
	}

	return true, nil, managedPoliciesPresent, nil
}

func (r *ClusterGroupUpgradeReconciler) processManagedPolicyForUpgradeContent(
	ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, managedPoliciesForUpgrade []*unstructured.Unstructured) error {
	for _, managedPolicy := range managedPoliciesForUpgrade {
		// Get the policy content and create any needed ManagedClusterViews for subscription type policies.
		policyTypes, err := r.getPolicyContent(clusterGroupUpgrade, managedPolicy)
		if err != nil {
			return err
		}

		p, err := json.Marshal(policyTypes)
		if err != nil {
			return err
		}
		clusterGroupUpgrade.Status.ManagedPoliciesContent[managedPolicy.GetName()] = string(p)
		r.createSubscriptionManagedClusterView(ctx, clusterGroupUpgrade, managedPolicy, policyTypes)
	}

	return nil
}

//nolint:unparam
func (r *ClusterGroupUpgradeReconciler) createSubscriptionManagedClusterView(
	ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, policy *unstructured.Unstructured, policyContent []ranv1alpha1.PolicyContent) error {

	nonCompliantClusters, err := r.getClustersNonCompliantWithPolicy(ctx, clusterGroupUpgrade, policy, true)
	if err != nil {
		return err
	}

	policyContentArr := []ranv1alpha1.PolicyContent{}
	json.Unmarshal(
		[]byte(clusterGroupUpgrade.Status.ManagedPoliciesContent[policy.GetName()]),
		&policyContentArr)

	// Check if the current policy is also a subscription policy.
	for _, policyContent := range policyContentArr {
		if policyContent.Kind == utils.PolicyTypeSubscription {

			// Compute the name of the managedClusterView
			managedClusterViewName := utils.GetMultiCloudObjectName(clusterGroupUpgrade, policyContent.Kind, policyContent.Name)

			// Create managedClusterView in each of the NonCompliant managed clusters' namespaces to access information for the policy.
			for _, nonCompliantCluster := range nonCompliantClusters {
				utils.EnsureManagedClusterView(
					ctx, r.Client, managedClusterViewName, nonCompliantCluster, "subscriptions.operators.coreos.com",
					policyContent.Name, *policyContent.Namespace, clusterGroupUpgrade.Namespace+"-"+clusterGroupUpgrade.Name)
			}
		}
	}
	return nil
}

func (r *ClusterGroupUpgradeReconciler) copyManagedInformPolicy(
	ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, managedPolicy *unstructured.Unstructured, newPolicyName string) error {

	// Create a new unstructured variable to keep all the information for the new policy.
	newPolicy := &unstructured.Unstructured{}

	// Set new policy name, namespace, group, kind and version.
	newPolicy.SetName(newPolicyName)
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
	newPolicy.SetLabels(labels)

	// Set new policy annotations - copy them from the managed policy.
	newPolicy.SetAnnotations(managedPolicy.GetAnnotations())

	// Set new policy remediationAction.
	newPolicy.Object["spec"] = managedPolicy.Object["spec"]
	err := r.updateConfigurationPolicyNameForCopiedPolicy(clusterGroupUpgrade, newPolicy.Object["spec"], managedPolicy.GetName())
	if err != nil {
		return err
	}

	specObject := newPolicy.Object["spec"].(map[string]interface{})
	specObject["remediationAction"] = utils.RemediationActionEnforce

	// Create the new policy in the desired namespace.
	err = r.createNewPolicyFromStructure(ctx, clusterGroupUpgrade, newPolicy)
	if err != nil {
		r.Log.Info("Error creating policy", "err", err)
		return err
	}
	return nil
}

func (r *ClusterGroupUpgradeReconciler) updateConfigurationPolicyNameForCopiedPolicy(
	clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, spec interface{}, managedPolicyName string) error {
	specObject := spec.(map[string]interface{})

	// Get the policy templates.
	policyTemplates := specObject["policy-templates"]
	policyTemplatesArr := policyTemplates.([]interface{})

	// Go through the template array.
	for _, template := range policyTemplatesArr {
		// Get to the metadata name of the ConfigurationPolicy.
		objectDefinition := template.(map[string]interface{})["objectDefinition"]
		if objectDefinition == nil {
			return fmt.Errorf("policy %s is missing its spec.policy-templates.objectDefinition", managedPolicyName)
		}
		objectDefinitionContent := objectDefinition.(map[string]interface{})
		metadata := objectDefinitionContent["metadata"]
		if metadata == nil {
			return fmt.Errorf("policy %s is missing its spec.policy-templates.objectDefinition.metadata", managedPolicyName)
		}
		// Update the metadata name
		metadataContent := metadata.(map[string]interface{})
		metadataContent["name"] = getResourceName(clusterGroupUpgrade, metadataContent["name"].(string))
	}

	return nil
}

func (r *ClusterGroupUpgradeReconciler) createNewPolicyFromStructure(
	ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, policy *unstructured.Unstructured) error {

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
		Name:      policy.GetName(),
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

//nolint:unparam
func (r *ClusterGroupUpgradeReconciler) getPolicyContent(
	clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, managedPolicy *unstructured.Unstructured) ([]ranv1alpha1.PolicyContent, error) {
	managedPolicyName := managedPolicy.GetName()
	specObject := managedPolicy.Object["spec"].(map[string]interface{})

	// Get the policy templates.
	policyTemplates := specObject["policy-templates"]
	policyTemplatesArr := policyTemplates.([]interface{})
	var policyContent []ranv1alpha1.PolicyContent

	// Go through the template array.
	for _, template := range policyTemplatesArr {
		// Get to the metadata name of the ConfigurationPolicy.
		objectDefinition := template.(map[string]interface{})["objectDefinition"]
		if objectDefinition == nil {
			return nil, fmt.Errorf("policy %s is missing its spec.policy-templates.objectDefinition", managedPolicyName)
		}
		objectDefinitionContent := objectDefinition.(map[string]interface{})

		// Get the spec.
		spec := objectDefinitionContent["spec"]
		if spec == nil {
			return nil, fmt.Errorf("policy %s is missing its spec.policy-templates.objectDefinition.spec", managedPolicyName)
		}

		// Get the object-templates from the spec.
		specContent := spec.(map[string]interface{})
		objectTemplates := specContent["object-templates"]
		if objectTemplates == nil {
			return nil, fmt.Errorf("policy %s is missing its spec.policy-templates.objectDefinition.spec.object-templates", managedPolicyName)
		}

		objectTemplatesContent := objectTemplates.([]interface{})
		for _, objectTemplate := range objectTemplatesContent {
			objectTemplateContent := objectTemplate.(map[string]interface{})
			innerObjectDefinition := objectTemplateContent["objectDefinition"]
			if innerObjectDefinition == nil {
				return nil, fmt.Errorf("policy %s is missing its spec.policy-templates.objectDefinition.spec.object-templates.objectDefinition", managedPolicyName)
			}

			innerObjectDefinitionContent := innerObjectDefinition.(map[string]interface{})
			// Get the object's metadata.
			objectDefinitionMetadata := innerObjectDefinitionContent["metadata"]
			if objectDefinitionMetadata == nil {
				r.Log.Info(
					"[getPolicyContent] Policy is missing its spec.policy-templates.objectDefinition.spec.object-templates.metadata",
					"policyName", managedPolicyName)
				continue
			}

			objectDefinitionMetadataContent := innerObjectDefinitionContent["metadata"].(map[string]interface{})
			// Save the kind, name and namespace if they exist and if kind is of Subscription type.
			// If kind is missing, log and skip.
			kind, ok := innerObjectDefinitionContent["kind"]
			if !ok {
				r.Log.Info(
					"[getPolicyContent] Policy is missing its spec.policy-templates.objectDefinition.spec.object-templates.kind",
					"policyName", managedPolicyName)
				continue
			}

			// Filter only Subscription templates.
			if kind != utils.PolicyTypeSubscription {
				r.Log.Info(
					"[getPolicyContent] Policy spec.policy-templates.objectDefinition.spec.object-templates.kind is not of Subscription kind",
					"policyName", managedPolicyName)
				continue
			}

			// If name is missing, log and skip. We need Subscription name in order to have a valid content for
			// Subscription InstallPlan approval.
			_, ok = objectDefinitionMetadataContent["name"]
			if !ok {
				r.Log.Info(
					"[getPolicyContent] Policy is missing its spec.policy-templates.objectDefinition.spec.object-templates.metadata.name",
					"policyName", managedPolicyName)
				continue
			}

			// If namespace is missing, log and skip. We need Subscription namespace in order to have a valid content for
			// Subscription InstallPlan approval.
			_, ok = objectDefinitionMetadataContent["namespace"]
			if !ok {
				r.Log.Info(
					"[getPolicyContent] Policy is missing its spec.policy-templates.objectDefinition.spec.object-templates.metadata.namespace",
					"policyName", managedPolicyName)
				continue
			}

			// Save the info into the policy content status.
			var policyContentCrt ranv1alpha1.PolicyContent
			policyContentCrt.Kind = innerObjectDefinitionContent["kind"].(string)
			policyContentCrt.Name = objectDefinitionMetadataContent["name"].(string)

			namespace := objectDefinitionMetadataContent["namespace"].(string)
			policyContentCrt.Namespace = &namespace

			policyContent = append(policyContent, policyContentCrt)
		}

	}

	return policyContent, nil
}

func (r *ClusterGroupUpgradeReconciler) ensureBatchPlacementRule(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, prName string) error {

	pr := r.newBatchPlacementRule(clusterGroupUpgrade, prName)

	if err := controllerutil.SetControllerReference(clusterGroupUpgrade, pr, r.Scheme); err != nil {
		return err
	}

	foundPlacementRule := &unstructured.Unstructured{}
	foundPlacementRule.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "apps.open-cluster-management.io",
		Kind:    "PlacementRule",
		Version: "v1",
	})

	err := r.Client.Get(ctx, client.ObjectKey{
		Name:      prName,
		Namespace: clusterGroupUpgrade.Namespace,
	}, foundPlacementRule)

	if err != nil {
		if errors.IsNotFound(err) {
			err = r.Client.Create(ctx, pr)
			if err != nil {
				return err
			}
		} else {
			return err
		}
	} else {
		pr.SetResourceVersion(foundPlacementRule.GetResourceVersion())
		err = r.Client.Update(ctx, pr)
		if err != nil {
			return err
		}
	}

	return nil
}

func (r *ClusterGroupUpgradeReconciler) newBatchPlacementRule(clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, prName string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.Object = map[string]interface{}{
		"metadata": map[string]interface{}{
			"name":      prName,
			"namespace": clusterGroupUpgrade.Namespace,
			"labels": map[string]interface{}{
				"app": "openshift-cluster-group-upgrades",
				"openshift-cluster-group-upgrades/clusterGroupUpgrade": clusterGroupUpgrade.Name,
				"openshift-cluster-group-upgrades/forPolicy":           prName,
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

/* getNextNonCompliantPolicyForCluster goes through all the policies in the managedPolicies list, starting with the
   policy index for the requested cluster and returns the index of the first policy that has the cluster as NonCompliant.

   returns: policyIndex the index of the next policy for which the cluster is NonCompliant or -1 if no policy found
            error/nil
*/
func (r *ClusterGroupUpgradeReconciler) getNextNonCompliantPolicyForCluster(
	ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, clusterName string, startIndex int) (int, error) {
	currentPolicyIndex := startIndex

	for {
		// Get the name of the managed policy matching the current index.
		currentManagedPolicyInfo := utils.GetManagedPolicyForUpgradeByIndex(currentPolicyIndex, clusterGroupUpgrade)
		currentManagedPolicy, err := r.getPolicyByName(ctx, currentManagedPolicyInfo.Name, currentManagedPolicyInfo.Namespace)
		if err != nil {
			return utils.NoPolicyIndex, err
		}

		// Check if current cluster is compliant or not for its current managed policy.
		clusterStatus, err := r.getClusterComplianceWithPolicy(clusterName, currentManagedPolicy)
		if err != nil {
			return utils.NoPolicyIndex, err
		}

		// If the cluster is compliant for the policy or if the cluster is not matched with the policy,
		// move to the next policy index.
		if clusterStatus == utils.ClusterStatusCompliant || clusterStatus == utils.ClusterNotMatchedWithPolicy {
			if currentPolicyIndex < len(clusterGroupUpgrade.Status.ManagedPoliciesForUpgrade)-1 {
				currentPolicyIndex++
				continue
			} else {
				currentPolicyIndex = utils.AllPoliciesValidated
				break
			}
		} else if clusterStatus == utils.ClusterStatusNonCompliant {
			break
		}
	}

	return currentPolicyIndex, nil
}

/* isUpgradeComplete checks if there is at least one managed policy left for which at least one cluster in the
   batch is NonCompliant.

   returns: true/false if the upgrade is complete
            error/nil
*/
func (r *ClusterGroupUpgradeReconciler) isUpgradeComplete(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) (bool, error) {
	// If we are not at the last batch, the upgrade clearly didn't complete.
	if clusterGroupUpgrade.Status.Status.CurrentBatch < len(clusterGroupUpgrade.Status.CopiedPolicies) {
		return false, nil
	}

	// Go through all the clusters in the current last batch (which is also the last batch) and make sure
	// they are either compliant with all the managedPolicies or they don't match them.
	for _, batchClusterName := range clusterGroupUpgrade.Status.RemediationPlan[clusterGroupUpgrade.Status.Status.CurrentBatch-1] {
		currentPolicyIndex := clusterGroupUpgrade.Status.Status.CurrentRemediationPolicyIndex[batchClusterName]
		if currentPolicyIndex == utils.AllPoliciesValidated || currentPolicyIndex == utils.NoPolicyIndex {
			continue
		}

		nextNonCompliantPolicyIndex, err := r.getNextNonCompliantPolicyForCluster(ctx, clusterGroupUpgrade, batchClusterName, currentPolicyIndex)
		if err != nil {
			return false, err
		}

		// If we can find a managed policy for which the cluster is NonCompliant, it means the upgrade has
		// not finished.
		if nextNonCompliantPolicyIndex != utils.NoPolicyIndex {
			return false, nil
		}
	}

	return true, nil
}

func (r *ClusterGroupUpgradeReconciler) ensureBatchPlacementBinding(
	ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, commonResourceName string) error {

	// Ensure batch placement bindings.
	pb := r.newBatchPlacementBinding(clusterGroupUpgrade, commonResourceName, commonResourceName)

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
		Name:      commonResourceName,
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
	placementRuleName string, placementBindingName string) *unstructured.Unstructured {

	var subjects []map[string]interface{}

	subject := make(map[string]interface{})
	subject["name"] = placementBindingName
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

func (r *ClusterGroupUpgradeReconciler) reconcileResources(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, managedPoliciesPresent []*unstructured.Unstructured) error {
	// Reconcile resources
	for _, managedPolicy := range managedPoliciesPresent {
		commonName := getResourceName(clusterGroupUpgrade, managedPolicy.GetName())

		err := r.ensureBatchPlacementRule(ctx, clusterGroupUpgrade, commonName)
		if err != nil {
			return err
		}

		err = r.copyManagedInformPolicy(ctx, clusterGroupUpgrade, managedPolicy, commonName)
		if err != nil {
			return err
		}

		err = r.ensureBatchPlacementBinding(ctx, clusterGroupUpgrade, commonName)
		if err != nil {
			return err
		}
	}

	return nil
}

func (r *ClusterGroupUpgradeReconciler) getPolicyClusterStatus(policy *unstructured.Unstructured) ([]interface{}, error) {
	policyName := policy.GetName()

	// Get the compliant status part of the policy.
	if policy.Object["status"] == nil {
		return nil, fmt.Errorf("policy %s is missing its status", policyName)
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
		return nil, fmt.Errorf("policy %s has it's list of cluster statuses pending", policyName)
	}

	subStatus := statusCompliance.([]interface{})
	if subStatus == nil {
		return nil, fmt.Errorf("policy %s is missing it's compliance status", policyName)
	}

	return subStatus, nil
}

func (r *ClusterGroupUpgradeReconciler) getClustersNonCompliantWithPolicy(
	ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade,
	policy *unstructured.Unstructured, filterClustersForUpgrade bool) ([]string, error) {

	// Get the status of the clusters matching the policy.
	subStatus, err := r.getPolicyClusterStatus(policy)
	if err != nil {
		return nil, err
	}

	var nonCompliantClusters []string
	// Loop through all the clusters in the policy's compliance status.
	for _, crtSubStatusCrt := range subStatus {
		crtSubStatusMap := crtSubStatusCrt.(map[string]interface{})
		// If the cluster is NonCompliant, add it to the list. A cluster without a status is also considered NonCompliant.
		if crtSubStatusMap["compliant"] == utils.ClusterStatusNonCompliant || crtSubStatusMap["compliant"] == nil {
			nonCompliantClusters = append(nonCompliantClusters, crtSubStatusMap["clustername"].(string))
		}
	}

	// Filter only the clusters present in the current upgrade.
	if filterClustersForUpgrade {
		var nonCompliantClustersInUpgrade []string
		allClustersForUpgrade, err := r.getAllClustersForUpgrade(ctx, clusterGroupUpgrade)
		if err != nil {
			return nil, fmt.Errorf("cannot obtain all the details about the clusters in the CR: %s", err)
		}
		for _, nonCompliantCluster := range nonCompliantClusters {
			for _, cluster := range allClustersForUpgrade {
				if cluster == nonCompliantCluster {
					nonCompliantClustersInUpgrade = append(nonCompliantClustersInUpgrade, cluster)
					break
				}
			}
		}
		r.Log.Info("[getClustersNonCompliantWithPolicy]", "policy: ", policy.GetName(), "clusters: ", nonCompliantClusters)
		return nonCompliantClustersInUpgrade, nil
	}

	r.Log.Info("[getClustersNonCompliantWithPolicy]", "policy: ", policy.GetName(), "clusters: ", nonCompliantClusters)
	return nonCompliantClusters, nil
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
	clusterName string, policy *unstructured.Unstructured) (string, error) {
	// Get the status of the clusters matching the policy.
	subStatus, err := r.getPolicyClusterStatus(policy)
	if err != nil {
		return utils.PolicyStatusUnknown, err
	}

	// Loop through all the clusters in the policy's compliance status.
	for _, crtSubStatusCrt := range subStatus {
		crtSubStatusMap := crtSubStatusCrt.(map[string]interface{})
		// If the cluster is Compliant, return true.
		if clusterName == crtSubStatusMap["clustername"].(string) {
			if crtSubStatusMap["compliant"] == utils.ClusterStatusCompliant {
				return utils.ClusterStatusCompliant, nil
			} else if crtSubStatusMap["compliant"] == utils.ClusterStatusNonCompliant {
				return utils.ClusterStatusNonCompliant, nil
			} else if crtSubStatusMap["compliant"] == nil {
				r.Log.Info(
					"[getClusterComplianceWithPolicy] Cluster is missing its compliance status, treat as NonCompliant",
					"clusterName", clusterName, "policyName", policy.GetName())
				return utils.ClusterStatusNonCompliant, nil
			}
		}
	}
	return utils.ClusterNotMatchedWithPolicy, nil
}

func (r *ClusterGroupUpgradeReconciler) getClustersNonCompliantWithManagedPolicies(ctx context.Context,
	clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, managedPolicies []*unstructured.Unstructured) (map[string]bool, error) {
	clustersNonCompliantMap := make(map[string]bool)

	// clustersNonCompliantMap will be a map of the clusters present in the CR and wether they are NonCompliant with at
	// least one managed policy.
	allClustersForUpgrade, err := r.getAllClustersForUpgrade(ctx, clusterGroupUpgrade)
	if err != nil {
		return nil, err
	}
	for _, clusterName := range allClustersForUpgrade {
		for _, managedPolicy := range managedPolicies {
			clusterCompliance, err := r.getClusterComplianceWithPolicy(clusterName, managedPolicy)
			if err != nil {
				return nil, err
			}

			if clusterCompliance == utils.ClusterStatusNonCompliant {
				// If the cluster is NonCompliant in this current policy mark it as such and move to the next cluster.
				clustersNonCompliantMap[clusterName] = true
				break
			}
		}
	}

	return clustersNonCompliantMap, nil
}

func (r *ClusterGroupUpgradeReconciler) buildRemediationPlan(
	ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, managedPolicies []*unstructured.Unstructured) error {
	// Get all clusters from the CR that are non compliant with at least one of the managedPolicies.
	clusterNonCompliantWithManagedPoliciesMap, err := r.getClustersNonCompliantWithManagedPolicies(ctx, clusterGroupUpgrade, managedPolicies)
	if err != nil {
		return err
	}

	// Create remediation plan
	var remediationPlan [][]string
	isCanary := make(map[string]bool)
	if clusterGroupUpgrade.Spec.RemediationStrategy.Canaries != nil && len(clusterGroupUpgrade.Spec.RemediationStrategy.Canaries) > 0 {
		for _, canary := range clusterGroupUpgrade.Spec.RemediationStrategy.Canaries {
			// TODO: make sure the canary clusters are in the list of clusters.
			if clusterNonCompliantWithManagedPoliciesMap[canary] {
				remediationPlan = append(remediationPlan, []string{canary})
				isCanary[canary] = true
			}
		}
	}

	allClustersForUpgrade, err := r.getAllClustersForUpgrade(ctx, clusterGroupUpgrade)
	if err != nil {
		return err
	}

	var batch []string
	clusterCount := 0
	for i := 0; i < len(allClustersForUpgrade); i++ {
		site := allClustersForUpgrade[i]
		if !isCanary[site] && clusterNonCompliantWithManagedPoliciesMap[site] {
			batch = append(batch, site)
			clusterCount++
		}

		if clusterCount == clusterGroupUpgrade.Status.ComputedMaxConcurrency || i == len(allClustersForUpgrade)-1 {
			if len(batch) > 0 {
				remediationPlan = append(remediationPlan, batch)
				clusterCount = 0
				batch = nil
			}
		}
	}
	r.Log.Info("Remediation plan", "remediatePlan", remediationPlan)
	clusterGroupUpgrade.Status.RemediationPlan = remediationPlan

	return nil
}

func getResourceName(clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, initialString string) string {
	return clusterGroupUpgrade.Name + "-" + initialString
}

func (r *ClusterGroupUpgradeReconciler) getAllClustersForUpgrade(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) ([]string, error) {
	var clusterNames []string
	keys := make(map[string]bool)
	// The cluster selector field holds a label common to multiple clusters that will be updated.
	// All the clusters matching the labels specified in clusterSelector will be included in the update plan.
	// The expected format for Spec.ClusterSelector is as follows:
	// clusterSelector:
	//   - label1Name=label1Value
	//   - label2Name=label2Value
	// If the value is empty, then the expected format is:
	// clusterSelector:
	//   - label1Name
	for _, clusterSelector := range clusterGroupUpgrade.Spec.ClusterSelector {
		selectorList := strings.Split(clusterSelector, "=")
		//nolint:ineffassign
		var clusterLabels = make(map[string]string)
		if len(selectorList) == 2 {
			clusterLabels = map[string]string{selectorList[0]: selectorList[1]}
		} else if len(selectorList) == 1 {
			clusterLabels = map[string]string{selectorList[0]: ""}
		} else {
			r.Log.Info("Cluster selector has wrong format: %s", clusterSelector)
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
				clusterNames = append(clusterNames, cluster.GetName())
			}
		}
	}

	r.Log.Info("[getClusterBySelectors]", "clustersBySelector", clusterNames)

	for _, clusterName := range clusterGroupUpgrade.Spec.Clusters {
		// Make sure a cluster name doesn't appear twice.
		if _, value := keys[clusterName]; !value {
			keys[clusterName] = true
			clusterNames = append(clusterNames, clusterName)
		}
	}

	r.Log.Info("[getClustersBySelectors]", "clusterNames", clusterNames)
	return clusterNames, nil
}

func (r *ClusterGroupUpgradeReconciler) updateStatus(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) error {
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		placementRules, err := r.getPlacementRules(ctx, clusterGroupUpgrade, nil)
		if err != nil {
			return err
		}
		placementRulesStatus := make([]string, 0)
		for _, placementRule := range placementRules.Items {
			placementRulesStatus = append(placementRulesStatus, placementRule.GetName())
		}
		clusterGroupUpgrade.Status.PlacementRules = placementRulesStatus

		placementBindings, err := r.getPlacementBindings(ctx, clusterGroupUpgrade)
		if err != nil {
			return err
		}
		placementBindingsStatus := make([]string, 0)
		for _, placementBinding := range placementBindings.Items {
			placementBindingsStatus = append(placementBindingsStatus, placementBinding.GetName())
		}
		clusterGroupUpgrade.Status.PlacementBindings = placementBindingsStatus

		copiedPolicies, err := r.getCopiedPolicies(ctx, clusterGroupUpgrade)
		if err != nil {
			return err
		}
		policiesStatus := make([]string, 0)
		for _, policy := range copiedPolicies.Items {
			policiesStatus = append(policiesStatus, policy.GetName())
		}
		clusterGroupUpgrade.Status.CopiedPolicies = policiesStatus

		err = r.Status().Update(ctx, clusterGroupUpgrade)

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

		// If we find a blocking CR with a status different than "UpgradeCompleted", then we add it to the list.
		for i := range cgu.Status.Conditions {
			if cgu.Status.Conditions[i].Reason != "UpgradeCompleted" {
				blockingCRsNotCompleted = append(blockingCRsNotCompleted, cgu.Name)
			}
		}
	}

	r.Log.Info("[blockingCRsNotCompleted]", "blockingCRs", blockingCRsNotCompleted)
	return blockingCRsNotCompleted, blockingCRsMissing, nil
}

func (r *ClusterGroupUpgradeReconciler) validateCR(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) (bool, error) {
	reconcile := false
	// Validate clusters in spec are ManagedCluster objects
	clusters, err := r.getAllClustersForUpgrade(ctx, clusterGroupUpgrade)
	if err != nil {
		return reconcile, fmt.Errorf("cannot obtain all the details about the clusters in the CR: %s", err)
	}

	for _, cluster := range clusters {
		managedCluster := &clusterv1.ManagedCluster{}
		err := r.Client.Get(ctx, types.NamespacedName{Name: cluster}, managedCluster)
		if err != nil {
			return reconcile, fmt.Errorf("cluster %s is not a ManagedCluster", cluster)
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
				return reconcile, fmt.Errorf("canary cluster %s is not in the list of clusters", canary)
			}
		}
	}

	var newMaxConcurrency int
	// Automatically adjust maxConcurrency to the min of maxConcurrency, number of clusters, 100 or
	// to the min of number of clusters, 100 if maxConcurrency is set to 0.
	if clusterGroupUpgrade.Spec.RemediationStrategy.MaxConcurrency > 0 {
		newMaxConcurrency = utils.GetMinOf3(
			clusterGroupUpgrade.Spec.RemediationStrategy.MaxConcurrency,
			len(clusters),
			utils.MaxNumberOfClustersForUpgrade)
	} else {
		newMaxConcurrency = len(clusters)
		if utils.MaxNumberOfClustersForUpgrade < len(clusters) {
			newMaxConcurrency = utils.MaxNumberOfClustersForUpgrade
		}
	}

	if newMaxConcurrency != clusterGroupUpgrade.Status.ComputedMaxConcurrency {
		clusterGroupUpgrade.Status.ComputedMaxConcurrency = newMaxConcurrency
		err = r.Status().Update(ctx, clusterGroupUpgrade)
		if err != nil {
			r.Log.Info("Error updating Cluster Group Upgrade")
			return reconcile, err
		}
		reconcile = true
	}

	return reconcile, nil
}

func (r *ClusterGroupUpgradeReconciler) handleCguFinalizer(
	ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) (int, error) {

	isCguMarkedToBeDeleted := clusterGroupUpgrade.GetDeletionTimestamp() != nil
	if isCguMarkedToBeDeleted {
		if controllerutil.ContainsFinalizer(clusterGroupUpgrade, utils.CleanupFinalizer) {
			// Run finalization logic for cguFinalizer. If the finalization logic fails, don't remove the finalizer so
			// that we can retry during the next reconciliation.
			clusters, err := r.getAllClustersForUpgrade(ctx, clusterGroupUpgrade)
			if err != nil {
				return utils.StopReconciling, fmt.Errorf("cannot obtain all the details about the clusters in the CR: %s", err)
			}
			err = utils.DeleteMultiCloudObjects(ctx, r.Client, clusterGroupUpgrade, clusters)
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

	return ctrl.NewControllerManagedBy(mgr).
		For(&ranv1alpha1.ClusterGroupUpgrade{}).
		Owns(placementRuleUnstructured).
		Owns(placementBindingUnstructured).
		Owns(policyUnstructured).
		Complete(r)
}
