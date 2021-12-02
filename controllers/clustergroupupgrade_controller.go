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
	"fmt"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	ranv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/api/v1alpha1"
	utils "github.com/openshift-kni/cluster-group-upgrades-operator/controllers/utils"
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
//+kubebuilder:rbac:groups=cluster.open-cluster-management.io,resources=managedclusters,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the ClusterGroupUpgrade object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.8.3/pkg/reconcile
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

	r.Log.Info(">>>>>>> clusterGroupUpgrade", "clusterGroupUpgrade", clusterGroupUpgrade.Name)

	err = r.validateCR(ctx, clusterGroupUpgrade)
	if err != nil {
		return ctrl.Result{}, err
	}

	nextReconcile := ctrl.Result{}
	readyCondition := meta.FindStatusCondition(clusterGroupUpgrade.Status.Conditions, "Ready")

	if readyCondition == nil {
		// TODO: Validate CR
		meta.SetStatusCondition(&clusterGroupUpgrade.Status.Conditions, metav1.Condition{
			Type:    "Ready",
			Status:  metav1.ConditionFalse,
			Reason:  "UpgradeNotStarted",
			Message: "The ClusterGroupUpgrade CR has remediationAction set to inform",
		})
	} else if readyCondition.Status == metav1.ConditionFalse {
		if readyCondition.Reason == "UpgradeNotStarted" || readyCondition.Reason == "UpgradeCannotStart" {
			// Before starting the upgrade check that all the managed policies exist.
			allManagedPoliciesExist, managedPoliciesMissing, managedPoliciesPresent := r.doManagedPoliciesExist(ctx, clusterGroupUpgrade)
			if allManagedPoliciesExist == true {
				// If all the managedPolicies exist, continue with building the upgrade batches.
				r.buildRemediationPlan(ctx, clusterGroupUpgrade, managedPoliciesPresent)
				// Create the needed resources for starting the upgrade.
				err := r.reconcileResources(ctx, clusterGroupUpgrade, managedPoliciesPresent)
				if err != nil {
					return ctrl.Result{}, err
				}

				var statusReason, statusMessage string
				statusReason = "UpgradeNotStarted"
				statusMessage = "The ClusterGroupUpgrade CR has remediationAction set to inform"
				if clusterGroupUpgrade.Spec.Enable == true {
					statusReason = "UpgradeNotCompleted"
					statusMessage = "The ClusterGroupUpgrade CR has upgrade policies that are still non compliant"
					clusterGroupUpgrade.Status.Status.StartedAt = metav1.Now()
				}
				meta.SetStatusCondition(&clusterGroupUpgrade.Status.Conditions, metav1.Condition{
					Type:    "Ready",
					Status:  metav1.ConditionFalse,
					Reason:  statusReason,
					Message: statusMessage,
				})
			} else {
				// If not all managedPolicies exist, update the Status accordingly.
				statusMessage := fmt.Sprintf("The ClusterGroupUpgrade CR has managed policies that are missing: %s", managedPoliciesMissing)
				meta.SetStatusCondition(&clusterGroupUpgrade.Status.Conditions, metav1.Condition{
					Type:    "Ready",
					Status:  metav1.ConditionFalse,
					Reason:  "UpgradeCannotStart",
					Message: statusMessage,
				})
				requeueAfter := 1 * time.Minute
				nextReconcile = ctrl.Result{RequeueAfter: requeueAfter}
			}
		} else if readyCondition.Reason == "UpgradeNotCompleted" {
			r.Log.Info("======== CurrentBatch: ", "Status.CurrentBatch", clusterGroupUpgrade.Status.Status.CurrentBatch)

			// If the upgrade is just starting, set the batch to be shown in the Status as 1.
			if clusterGroupUpgrade.Status.Status.CurrentBatch == 0 {
				clusterGroupUpgrade.Status.Status.CurrentBatch = 1
			}

			if clusterGroupUpgrade.Status.Status.CurrentBatchStartedAt.IsZero() {
				nextReconcile = ctrl.Result{Requeue: true}
			} else {
				requeueAfter := clusterGroupUpgrade.Status.Status.CurrentBatchStartedAt.Add(5 * time.Minute).Sub(time.Now())
				if requeueAfter < 0 {
					requeueAfter = 5 * time.Minute
				}
				r.Log.Info("Requeuing after", "requeueAfter", requeueAfter)
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

			r.Log.Info("\n\n Checking batch for new remediation policies ", "batch index", clusterGroupUpgrade.Status.Status.CurrentBatch)
			// Check if current policies have become compliant and if new policies have to be applied.
			err, isBatchComplete := r.getNextRemediationPoliciesForBatch(ctx, clusterGroupUpgrade)
			if err != nil {
				return ctrl.Result{}, err
			}

			// Add the needed cluster names to upgrade to the appropriate placement rule.
			err = r.addClustersToPlacementRule(ctx, clusterGroupUpgrade)
			if err != nil {
				return ctrl.Result{}, err
			}

			if isBatchComplete {
				r.Log.Info("Upgrade completed for batch", "batchIndex", clusterGroupUpgrade.Status.Status.CurrentBatch)
				r.cleanupPlacementRules(ctx, clusterGroupUpgrade)
				clusterGroupUpgrade.Status.Status.CurrentBatchStartedAt = metav1.Time{}

				// If we haven't reached the last batch yet, move to the next batch.
				if clusterGroupUpgrade.Status.Status.CurrentBatch < len(clusterGroupUpgrade.Status.Policies) {
					clusterGroupUpgrade.Status.Status.CurrentBatch++
				}
			} else {
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
						if clusterGroupUpgrade.Status.Status.CurrentBatch < len(clusterGroupUpgrade.Status.Policies) {
							clusterGroupUpgrade.Status.Status.CurrentBatch++
						}
					}
				}
			}

			isUpgradeComplete := false
			// If the batch is complete and it was the last batch in the remediationPlan, then the whole upgrade is complete.
			if isBatchComplete == true && clusterGroupUpgrade.Status.Status.CurrentBatch == len(clusterGroupUpgrade.Status.Policies) {
				isUpgradeComplete = true
			}
			if err != nil {
				return ctrl.Result{}, err
			}

			if isUpgradeComplete {
				meta.SetStatusCondition(&clusterGroupUpgrade.Status.Conditions, metav1.Condition{
					Type:    "Ready",
					Status:  metav1.ConditionTrue,
					Reason:  "UpgradeCompleted",
					Message: "The ClusterGroupUpgrade CR has all the managed policies compliant",
				})
			} else {
				if !clusterGroupUpgrade.Status.Status.StartedAt.IsZero() && time.Since(clusterGroupUpgrade.Status.Status.StartedAt.Time) > time.Duration(clusterGroupUpgrade.Spec.RemediationStrategy.Timeout)*time.Minute {
					meta.SetStatusCondition(&clusterGroupUpgrade.Status.Conditions, metav1.Condition{
						Type:    "Ready",
						Status:  metav1.ConditionFalse,
						Reason:  "UpgradeTimedOut",
						Message: "The ClusterGroupUpgrade CR policies are taking too long to complete",
					})
				}
			}
		} else if readyCondition.Reason == "UpgradeTimedOut" {
			r.Recorder.Event(clusterGroupUpgrade, corev1.EventTypeWarning, "UpgradeTimedOut", "The ClusterGroupUpgrade CR policies are taking too long to complete")
			nextReconcile = ctrl.Result{RequeueAfter: 60 * time.Minute}

			isUpgradeComplete, err := r.areAllManagedPoliciesCompliant(ctx, clusterGroupUpgrade)
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

		if clusterGroupUpgrade.Spec.DeleteObjectsOnCompletion {
			err := r.deleteResources(ctx, clusterGroupUpgrade)
			if err != nil {
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

	// By default, assume all clusters start applying all policies from the first policy.
	for _, batchClusterName := range clusterGroupUpgrade.Status.RemediationPlan[batchIndex] {
		clusterGroupUpgrade.Status.Status.CurrentRemediationPolicyIndex[batchClusterName] = 0
	}

	r.Log.Info(">>>> InitializeRemediationPolicyForBatch ", "CurrentRemediationPolicyIndex", clusterGroupUpgrade.Status.Status.CurrentRemediationPolicyIndex)
	r.Log.Info("NEW CurrentBatchStartedAt", "clusterGroupUpgrade.Status.Status.CurrentBatchStartedAt", clusterGroupUpgrade.Status.Status.CurrentBatchStartedAt)

}

/*
  getNextRemediationPoliciesForBatch steps through all the clusters in the current batch and checks if each cluster is
  compliant or not with the policy currently applied for it which has its index held in
  clusterGroupUpgrade.Status.Status.CurrentRemediationPolicyIndex[clusterName] (the index is used to range through the
  policies present in clusterGroupUpgrade.Spec.managedPolicies).

  If the cluster is compliant with the policy described by the corresponding index and if we haven't yet reached the
  last policy, we increment the policy index until we find the next policy for which the cluster is NonCompliant.

  If we don't find a policy for which the cluster is NonCompliant then we decide the batch is completed.
  If we find a policy for which the cluster is NonCompliant, we decide the batch is not completed.

  returns: error/nil: in case any error happens
           bool     : true if the batch is done upgrading or not; false if not
*/
func (r *ClusterGroupUpgradeReconciler) getNextRemediationPoliciesForBatch(
	ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) (error, bool) {
	r.Log.Info("\n===== In getCurrentRemediationPolicyIndex ===", "batchIndex", clusterGroupUpgrade.Status.Status.CurrentBatch)
	r.Log.Info("CURRENT CurrentRemediationPolicyIndex", "plan", clusterGroupUpgrade.Status.Status.CurrentRemediationPolicyIndex)

	var isBatchComplete bool = false
	batchIndex := clusterGroupUpgrade.Status.Status.CurrentBatch - 1

	for _, batchClusterName := range clusterGroupUpgrade.Status.RemediationPlan[batchIndex] {
		for {
			// Get the index and name of the current policy being applied for the current cluster in the batch.
			currentPolicyIndex := clusterGroupUpgrade.Status.Status.CurrentRemediationPolicyIndex[batchClusterName]
			currentManagedPolicyName := clusterGroupUpgrade.Spec.ManagedPolicies[currentPolicyIndex]
			r.Log.Info("Current policy name for cluster ", "cluster name", batchClusterName, "policyName", currentManagedPolicyName)

			currentManagedPolicy, err := r.getPolicyByName(ctx, currentManagedPolicyName, clusterGroupUpgrade.Namespace)
			if err != nil {
				return err, false
			}
			// Check if current cluster is compliant or not for its current policy.
			isBatchClusterCompliantForPolicy := r.isClusterCompliantWithPolicy(clusterGroupUpgrade, batchClusterName, currentManagedPolicy)
			r.Log.Info("isBatchClusterCompliantForPolicy", "isBatchClusterCompliantForPolicy", isBatchClusterCompliantForPolicy)

			// If the cluster is compliant for the policy, move to the next policy index.
			if isBatchClusterCompliantForPolicy == true {
				if currentPolicyIndex < len(clusterGroupUpgrade.Spec.ManagedPolicies) {
					r.Log.Info("Increase policyIndex for cluster\n\n")
					clusterGroupUpgrade.Status.Status.CurrentRemediationPolicyIndex[batchClusterName]++
				} else {
					break
				}
			} else {
				isBatchComplete = false
				break
			}
		}
	}

	r.Log.Info("isBatchComplete", "isBatchComplete", isBatchComplete)
	r.Log.Info("NEW CurrentRemediationPolicyIndex", "plan", clusterGroupUpgrade.Status.Status.CurrentRemediationPolicyIndex)
	return nil, isBatchComplete
}

/*
  addClustersToPlacementRule steps through all the managedPolicies and adds those for which it's not compliant with
  to the placement rules in order so that at the end of a batch upgrade, all the copied policies are Compliant.

  returns: error: if it exists
           nil  : when no error is reported
*/
func (r *ClusterGroupUpgradeReconciler) addClustersToPlacementRule(
	ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) error {

	for clusterName, managedPolicyIndex := range clusterGroupUpgrade.Status.Status.CurrentRemediationPolicyIndex {
		policyName := clusterGroupUpgrade.Name + "-" + clusterGroupUpgrade.Spec.ManagedPolicies[managedPolicyIndex]
		r.Log.Info("Add cluster to placementRule for policy ", "clusterName", clusterName, "policyName", policyName)
		err := r.updatePlacementRuleWithCluster(ctx, clusterGroupUpgrade, clusterName, policyName)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *ClusterGroupUpgradeReconciler) updatePlacementRuleWithCluster(
	ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, clusterName string, prName string) error {
	r.Log.Info("\n\n\n==== In function updatePlacementRuleWithCluster ====!!!")

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
		r.Log.Info("Error", "error", err)
		if errors.IsNotFound(err) {
			return err
		}
	} else {
		r.Log.Info("Got placementRule", "placementRuleName", placementRule.GetName())

		placementRuleSpecClusters := placementRule.Object["spec"].(map[string]interface{})
		r.Log.Info("placementRuleSpecClusters", "placementRuleSpecClusters", placementRuleSpecClusters)

		newClusterForPlacementRule := map[string]interface{}{"name": clusterName}
		var updatedClusters []map[string]interface{}
		currentClusters := placementRuleSpecClusters["clusters"]
		isCurrentClusterAlreadyPresent := false
		if currentClusters != nil {
			r.Log.Info("currentClusters", "currentClusters", currentClusters)
			// Check clusterName is not already present in currentClusters
			for _, clusterEntry := range currentClusters.([]interface{}) {
				clusterMap := clusterEntry.(map[string]interface{})
				r.Log.Info("currentClusters", "clusterMap[name]", clusterMap["name"])
				updatedClusters = append(updatedClusters, clusterMap)
				if clusterName == clusterMap["name"] {
					isCurrentClusterAlreadyPresent = true
				}
			}
		}
		if isCurrentClusterAlreadyPresent == false {
			updatedClusters = append(updatedClusters, newClusterForPlacementRule)
		}

		r.Log.Info("Updated clusters for placementRule", "updatedClusters", updatedClusters)
		placementRuleSpecClusters["clusters"] = updatedClusters
		placementRuleSpecClusters["clusterReplicas"] = nil

		err = r.Client.Update(ctx, placementRule)
		if err != nil {
			r.Log.Info("Error updating placementRule with updatedClusters", "err", err)
			return err
		}
	}

	return nil
}

func (r *ClusterGroupUpgradeReconciler) cleanupPlacementRules(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) error {
	// Get all the placementRules associated to this upgrades CR.
	err, placementRules := r.getPlacementRules(ctx, clusterGroupUpgrade, nil)

	if err != nil {
		r.Log.Info("Error getting PLRs in cleanupPlacementRule: ", "err", err)
		return err
	}

	errorMap := make(map[string]string)
	for _, plr := range placementRules.Items {
		r.Log.Info("Cleanup placementRule", "plrName", plr.GetName())
		placementRuleSpecClusters := plr.Object["spec"].(map[string]interface{})
		placementRuleSpecClusters["clusters"] = nil
		placementRuleSpecClusters["clusterReplicas"] = 0

		err = r.Client.Update(ctx, &plr)
		if err != nil {
			r.Log.Info("Error cleaning up placementRule", "err", err)
			errorMap[plr.GetName()] = string(err.Error())
			return err
		}
	}

	if errorMap != nil {
		return fmt.Errorf("Errors cleaning up placement rules: %s", errorMap)
	}
	return nil
}

func (r *ClusterGroupUpgradeReconciler) getPolicyByName(ctx context.Context, policyName string, namespace string) (*unstructured.Unstructured, error) {
	foundPolicy := &unstructured.Unstructured{}
	foundPolicy.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "policy.open-cluster-management.io",
		Kind:    "Policy",
		Version: "v1",
	})

	// Look for policy.
	err := r.Client.Get(ctx, client.ObjectKey{
		Name:      policyName,
		Namespace: namespace,
	}, foundPolicy)

	return foundPolicy, err
}

func (r *ClusterGroupUpgradeReconciler) doManagedPoliciesExist(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) (bool, []string, []*unstructured.Unstructured) {
	var managedPoliciesMissing []string
	var managedPoliciesPresent []*unstructured.Unstructured

	// Go through the managedPolicies in the CR and make sure they exist.
	for _, managedPolicy := range clusterGroupUpgrade.Spec.ManagedPolicies {
		foundPolicy, err := r.getPolicyByName(ctx, managedPolicy, clusterGroupUpgrade.Namespace)

		if err != nil {
			if errors.IsNotFound(err) {
				managedPoliciesMissing = append(managedPoliciesMissing, managedPolicy)
			}
		} else {
			managedPoliciesPresent = append(managedPoliciesPresent, foundPolicy)
		}
	}

	// If there are missing managed policies, return.
	if len(managedPoliciesMissing) != 0 {
		r.Log.Info("managedPoliciesMissing", "managedPoliciesMisisng", managedPoliciesMissing)
		return false, managedPoliciesMissing, managedPoliciesPresent
	}

	return true, nil, managedPoliciesPresent
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
	specObject := newPolicy.Object["spec"].(map[string]interface{})
	specObject["remediationAction"] = utils.RemediationActionEnforce

	// Create the new policy in the desired namespace.
	err := r.createNewPolicyFromStructure(ctx, clusterGroupUpgrade, newPolicy)
	if err != nil {
		r.Log.Info("Error creating policy", "err", err)
		return err
	}
	newPolicyName = newPolicy.GetName()

	return nil
}

func (r *ClusterGroupUpgradeReconciler) createNewPolicyFromStructure(
	ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, policy *unstructured.Unstructured) error {
	err := r.Client.Get(ctx, client.ObjectKey{
		Name:      policy.GetName(),
		Namespace: clusterGroupUpgrade.Namespace,
	}, policy)

	if err := controllerutil.SetControllerReference(clusterGroupUpgrade, policy, r.Scheme); err != nil {
		return err
	}

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
		err = r.Client.Update(ctx, policy)
		if err != nil {
			return err
		}
	}

	return nil
}

func (r *ClusterGroupUpgradeReconciler) ensureBatchPlacementRule(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, prName string) error {

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
			pr, err := r.newBatchPlacementRule(ctx, clusterGroupUpgrade, prName)
			if err != nil {
				return err
			}

			if err := controllerutil.SetControllerReference(clusterGroupUpgrade, pr, r.Scheme); err != nil {
				return err
			}

			err = r.Client.Create(ctx, pr)
			if err != nil {
				return err
			}
		} else {
			return err
		}
	} else {
		err = r.Client.Update(ctx, foundPlacementRule)
		if err != nil {
			return err
		}
	}

	return nil
}

func (r *ClusterGroupUpgradeReconciler) newBatchPlacementRule(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, prName string) (*unstructured.Unstructured, error) {
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

	return u, nil
}

func (r *ClusterGroupUpgradeReconciler) isPolicyCompliant(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, policyName string) (bool, error) {
	var result bool
	policy, err := r.getPolicyByName(ctx, policyName, clusterGroupUpgrade.Namespace)
	if err != nil {
		return false, err
	}

	if policy != nil {
		statusObject := policy.Object["status"].(map[string]interface{})
		if statusObject["compliant"] == nil || statusObject["compliant"] == utils.StatusNonCompliant {
			result = false
		} else {
			result = true
		}
	}

	return result, nil
}

func (r *ClusterGroupUpgradeReconciler) areAllManagedPoliciesCompliant(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) (bool, error) {
	policies, err := r.getManagedPolicies(ctx, clusterGroupUpgrade)
	if err != nil {
		return false, err
	}

	areAllPoliciesCompliant := true
	for _, policy := range policies {
		statusObject := policy.Object["status"].(map[string]interface{})
		if statusObject["compliant"] != utils.StatusCompliant {
			areAllPoliciesCompliant = false
		}
	}

	return areAllPoliciesCompliant, nil
}

func (r *ClusterGroupUpgradeReconciler) ensureBatchPlacementBinding(
	ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, commonResourceName string) error {

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
			// Ensure batch placement bindings.
			pb, err := r.newBatchPlacementBinding(ctx, clusterGroupUpgrade, commonResourceName, commonResourceName)
			if err != nil {
				return err
			}

			if err = controllerutil.SetControllerReference(clusterGroupUpgrade, pb, r.Scheme); err != nil {
				return err
			}

			err = r.Client.Create(ctx, pb)
			if err != nil {
				return err
			}
		} else {
			return err
		}
	} else {
		err = r.Client.Update(ctx, foundPlacementBinding)
		if err != nil {
			return err
		}
	}

	return nil
}

func (r *ClusterGroupUpgradeReconciler) newBatchPlacementBinding(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade,
	placementRuleName string, placementBindingName string) (*unstructured.Unstructured, error) {

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

	return u, nil
}

func (r *ClusterGroupUpgradeReconciler) getPlacementRules(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, policyName *string) (error, *unstructured.UnstructuredList) {
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
		return err, nil
	}

	return nil, placementRulesList
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

func (r *ClusterGroupUpgradeReconciler) getPolicies(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) (*unstructured.UnstructuredList, error) {
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

func (r *ClusterGroupUpgradeReconciler) getManagedPolicies(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) ([]unstructured.Unstructured, error) {
	var policies []unstructured.Unstructured
	for _, policyName := range clusterGroupUpgrade.Spec.ManagedPolicies {
		policy, err := r.getPolicyByName(ctx, policyName, clusterGroupUpgrade.Namespace)
		if err != nil {
			return nil, err
		}
		policies = append(policies, *policy)

	}

	return policies, nil
}

func (r *ClusterGroupUpgradeReconciler) reconcileResources(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, managedPoliciesPresent []*unstructured.Unstructured) error {

	r.Log.Info("Function reconcileResources", "managedPoliciesPresentLen", len(managedPoliciesPresent))

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

func (r *ClusterGroupUpgradeReconciler) isClusterCompliantWithPolicy(clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, clusterName string, policy *unstructured.Unstructured) bool {
	// Get the status part of the policy.
	statusObject := policy.Object["status"].(map[string]interface{})
	// Get the part of the status that holds the clusters' compliance.
	statusCompliance := statusObject["status"]

	// If the status doesn't contain anything, we don't have enough information to decide, so we assume the
	// cluster is not compliant with the policy.
	if statusCompliance == nil {
		return false
	}

	subStatus := statusCompliance.([]interface{})

	// Loop through all the NonCompliant clusters in the policy's status.
	for _, crtSubStatusCrt := range subStatus {
		crtSubStatusMap := crtSubStatusCrt.(map[string]interface{})
		// If the cluster is NonCompliant, return false.
		if crtSubStatusMap["compliant"] == utils.StatusNonCompliant && clusterName == crtSubStatusMap["clustername"].(string) {
			return false
		}
	}

	return true
}

/* A policy's status has the format below. In this function we are creating a list with the clusternames
   which are NonCompliant.

status: (map[string]interface{})
  compliant: NonCompliant
  placement:
  - placementBinding: operator-placementbinding
    placementRule: operator-placementrules
  status: ([]interface{})
  - clustername: spoke3
    clusternamespace: spoke3
    compliant: NonCompliant
  - clustername: spoke4
    clusternamespace: spoke4
    compliant: NonCompliant
*/
func (r *ClusterGroupUpgradeReconciler) getClustersNonCompliantWithPolicy(policy *unstructured.Unstructured) []string {
	var clustersNonCompliant []string
	// Get the status part of the policy.
	statusObject := policy.Object["status"].(map[string]interface{})
	// Get the part of the status that holds the clusters' compliance.
	statusCompliance := statusObject["status"]

	// If all the clusters are compliant, return nil.
	if statusCompliance == nil {
		return nil
	}

	subStatus := statusCompliance.([]interface{})

	// Loop through all the NonCompliant clusters in the policy's status.status.
	for _, crtSubStatusCrt := range subStatus {
		crtSubStatusMap := crtSubStatusCrt.(map[string]interface{})
		// If the cluster is NonCompliant, add it to the list of NonCompliant clusters.
		if crtSubStatusMap["compliant"] == utils.StatusNonCompliant {
			clustersNonCompliant = append(clustersNonCompliant, crtSubStatusMap["clustername"].(string))
		}
	}
	r.Log.Info("Clusters nonCompliant with ", "policy", policy.GetName(), "clustersNonCompliant", clustersNonCompliant)
	return clustersNonCompliant
}

func (r *ClusterGroupUpgradeReconciler) getClustersNonCompliantWithManagedPolicies(
	clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, managedPolicies []*unstructured.Unstructured) map[string]bool {
	clustersNonCompliantMap := make(map[string]bool)

	// clustersNonCompliant will be a map of the clusters present in the CR and within how many managedPolicies they
	// appear as being NonCompliant.
	clustersNonCompliant := make(map[string]int)
	for _, clusterName := range clusterGroupUpgrade.Spec.Clusters {
		clustersNonCompliant[clusterName] = 0
	}

	for _, managedPolicy := range managedPolicies {
		clustersNonCompliantWithManagedPolicy := r.getClustersNonCompliantWithPolicy(managedPolicy)

		if clustersNonCompliantWithManagedPolicy == nil {
			continue
		}

		for _, nonCompliantClusterName := range clustersNonCompliantWithManagedPolicy {
			// If the cluster is NonCompliant in this current policy and it's also present in the list of clusters from the CR
			// increment the number of policies for which it's NonCompliant.
			if _, ok := clustersNonCompliant[nonCompliantClusterName]; ok {
				clustersNonCompliantMap[nonCompliantClusterName] = true
				clustersNonCompliant[nonCompliantClusterName]++
			}
		}
	}

	r.Log.Info("Cluster (non) compliant with managedPolicies map  : ", "clustersNotCompliantMap", clustersNonCompliantMap)
	return clustersNonCompliantMap
}

func (r *ClusterGroupUpgradeReconciler) buildRemediationPlan(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, managedPolicies []*unstructured.Unstructured) {
	// Get all clusters from the CR that are non compliant with at least one of the managedPolicies.
	clusterNonCompliantWithManagedPoliciesMap := r.getClustersNonCompliantWithManagedPolicies(clusterGroupUpgrade, managedPolicies)

	/*  TODO: remove this at cleanup, keep it just for testing locally with kind.
	clusterNonCompliantWithManagedPoliciesMap := make(map[string]bool)
	clusterNonCompliantWithManagedPoliciesMap["spoke1"] = true
	clusterNonCompliantWithManagedPoliciesMap["spoke2"] = true
	clusterNonCompliantWithManagedPoliciesMap["spoke3"] = true
	clusterNonCompliantWithManagedPoliciesMap["spoke4"] = true
	clusterNonCompliantWithManagedPoliciesMap["spoke5"] = true
	clusterNonCompliantWithManagedPoliciesMap["spoke6"] = true
	*/

	// Create remediation plan
	var remediationPlan [][]string
	isCanary := make(map[string]bool)
	if clusterGroupUpgrade.Spec.RemediationStrategy.Canaries != nil && len(clusterGroupUpgrade.Spec.RemediationStrategy.Canaries) > 0 {
		for _, canary := range clusterGroupUpgrade.Spec.RemediationStrategy.Canaries {
			// TODO: make sure the canary clusters are in the list of clusters.
			if clusterNonCompliantWithManagedPoliciesMap[canary] == true {
				remediationPlan = append(remediationPlan, []string{canary})
				isCanary[canary] = true
			}
		}
	}

	var batch []string
	clusterCount := 0
	for i := 0; i < len(clusterGroupUpgrade.Spec.Clusters); i++ {
		site := clusterGroupUpgrade.Spec.Clusters[i]
		if !isCanary[site] && clusterNonCompliantWithManagedPoliciesMap[site] == true {
			batch = append(batch, site)
			clusterCount++
		}

		if clusterCount == clusterGroupUpgrade.Spec.RemediationStrategy.MaxConcurrency || i == len(clusterGroupUpgrade.Spec.Clusters)-1 {
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

func getResourceName(clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, policyName string) string {
	return clusterGroupUpgrade.Name + "-" + policyName
}

func (r *ClusterGroupUpgradeReconciler) deletePlacementRules(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) error {
	var placementRuleLabels = map[string]string{"openshift-cluster-group-upgrades/clusterGroupUpgrade": clusterGroupUpgrade.Name}
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
		return err
	}

	for _, policy := range placementRulesList.Items {
		if err := r.Delete(ctx, &policy); err != nil {
			return err
		}
	}
	return nil
}

func (r *ClusterGroupUpgradeReconciler) deletePlacementBindings(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) error {
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
		return err
	}

	for _, placementBinding := range placementBindingsList.Items {
		if err := r.Delete(ctx, &placementBinding); err != nil {

		}
	}
	return nil
}

func (r *ClusterGroupUpgradeReconciler) deletePolicies(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) error {
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
		return err
	}

	for _, policy := range policiesList.Items {
		if err := r.Delete(ctx, &policy); err != nil {

		}
	}
	return nil
}

func (r *ClusterGroupUpgradeReconciler) deleteResources(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) error {
	err := r.deletePlacementRules(ctx, clusterGroupUpgrade)
	if err != nil {
		return err
	}
	err = r.deletePlacementBindings(ctx, clusterGroupUpgrade)
	if err != nil {
		return err
	}
	err = r.deletePolicies(ctx, clusterGroupUpgrade)
	if err != nil {
		return err
	}

	return nil
}

func (r *ClusterGroupUpgradeReconciler) updateStatus(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) error {
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		err, placementRules := r.getPlacementRules(ctx, clusterGroupUpgrade, nil)
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

		policies, err := r.getPolicies(ctx, clusterGroupUpgrade)
		if err != nil {
			return err
		}
		policiesStatus := make([]string, 0)
		for _, policy := range policies.Items {
			policiesStatus = append(policiesStatus, policy.GetName())
		}
		clusterGroupUpgrade.Status.Policies = policiesStatus

		err = r.Status().Update(ctx, clusterGroupUpgrade)

		return err
	})

	if err != nil {
		return err
	}

	return nil
}

func (r *ClusterGroupUpgradeReconciler) validateCR(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) error {
	// Validate clusters in spec are ManagedCluster objects
	clusters := clusterGroupUpgrade.Spec.Clusters
	for _, cluster := range clusters {
		foundManagedCluster := &unstructured.Unstructured{}
		foundManagedCluster.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "cluster.open-cluster-management.io",
			Kind:    "ManagedCluster",
			Version: "v1",
		})
		err := r.Client.Get(ctx, client.ObjectKey{
			Name: cluster,
		}, foundManagedCluster)

		if err != nil {
			return fmt.Errorf("Cluster %s is not a ManagedCluster", cluster)
		}

	}

	// Check maxConcurrency is not greater than the number of clusters
	if clusterGroupUpgrade.Spec.RemediationStrategy.MaxConcurrency > len(clusterGroupUpgrade.Spec.Clusters) {
		return fmt.Errorf("maxConcurrency value cannot be greater than the number of clusters")
	}
	return nil
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
