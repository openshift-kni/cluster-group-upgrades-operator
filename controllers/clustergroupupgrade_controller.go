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
	"bytes"
	"context"
	"fmt"
	"strconv"
	"text/template"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	ranv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/api/v1alpha1"
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
				r.buildRemediationPlan(ctx, clusterGroupUpgrade)
				err := r.reconcileResources(ctx, clusterGroupUpgrade, managedPoliciesPresent)
				if err != nil {
					return ctrl.Result{}, err
				}

				var statusReason, statusMessage string
				statusReason = "UpgradeNotStarted"
				statusMessage = "The ClusterGroupUpgrade CR has remediationAction set to inform"
				if clusterGroupUpgrade.Spec.RemediationAction == "enforce" {
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
			if clusterGroupUpgrade.Status.Status.CurrentBatch == 0 {
				clusterGroupUpgrade.Status.Status.CurrentBatch = 1
			}
			if clusterGroupUpgrade.Status.Status.CurrentBatchStartedAt.IsZero() {
				nextReconcile = ctrl.Result{Requeue: true}
			} else {
				requeueAfter := clusterGroupUpgrade.Status.Status.CurrentBatchStartedAt.Add(60 * time.Minute).Sub(time.Now())
				if requeueAfter < 0 {
					requeueAfter = 60 * time.Minute
				}
				r.Log.Info("Requeuing after", "requeueAfter", requeueAfter)
				nextReconcile = ctrl.Result{RequeueAfter: requeueAfter}
			}

			var isBatchComplete, isBatchPlatformUpgradeComplete, isBatchOperatorUpgradeComplete bool

			platformUpgradePolicy, err := r.getBatchPlatformUpgradePolicy(ctx, clusterGroupUpgrade)
			if err != nil {
				return ctrl.Result{}, err
			}
			if platformUpgradePolicy != nil {
				err = r.ensureBatchPlatformUpgradePolicyIsEnforced(ctx, clusterGroupUpgrade, platformUpgradePolicy)
				if err != nil {
					return ctrl.Result{}, err
				}

				isBatchPlatformUpgradeComplete, err = r.isBatchPlatformUpgradePolicyCompliant(ctx, clusterGroupUpgrade)
				if err != nil {
					return ctrl.Result{}, err
				}
			}

			operatorUpgradePolicy, err := r.getBatchOperatorUpgradePolicy(ctx, clusterGroupUpgrade)
			if err != nil {
				return ctrl.Result{}, err
			}
			if isBatchPlatformUpgradeComplete || platformUpgradePolicy == nil {
				if operatorUpgradePolicy != nil {
					err = r.ensureBatchOperatorUpgradePolicyIsEnforced(ctx, clusterGroupUpgrade, operatorUpgradePolicy)
					if err != nil {
						return ctrl.Result{}, err
					}
				}
				isBatchOperatorUpgradeComplete, err = r.isBatchOperatorUpgradePolicyCompliant(ctx, clusterGroupUpgrade)
				if err != nil {
					return ctrl.Result{}, err
				}
			}

			if platformUpgradePolicy != nil && operatorUpgradePolicy != nil {
				isBatchComplete = isBatchPlatformUpgradeComplete && isBatchOperatorUpgradeComplete

			} else if platformUpgradePolicy != nil && operatorUpgradePolicy == nil {
				isBatchComplete = isBatchPlatformUpgradeComplete

			} else if platformUpgradePolicy == nil && operatorUpgradePolicy != nil {
				isBatchComplete = isBatchOperatorUpgradeComplete

			}

			if isBatchComplete {
				r.Log.Info("Batch upgrade completed")
				if clusterGroupUpgrade.Status.Status.CurrentBatch < len(clusterGroupUpgrade.Status.Policies) {
					clusterGroupUpgrade.Status.Status.CurrentBatch++
				}
			} else {
				batchTimeout := time.Duration(clusterGroupUpgrade.Spec.RemediationStrategy.Timeout/len(clusterGroupUpgrade.Status.RemediationPlan)) * time.Minute
				if !clusterGroupUpgrade.Status.Status.CurrentBatchStartedAt.IsZero() && time.Since(clusterGroupUpgrade.Status.Status.CurrentBatchStartedAt.Time) > batchTimeout {
					if len(clusterGroupUpgrade.Spec.RemediationStrategy.Canaries) != 0 && clusterGroupUpgrade.Status.Status.CurrentBatch == 1 {
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

			isUpgradeComplete, err := r.areAllPoliciesCompliant(ctx, clusterGroupUpgrade)
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

			isUpgradeComplete, err := r.areAllPoliciesCompliant(ctx, clusterGroupUpgrade)
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

func (r *ClusterGroupUpgradeReconciler) doManagedPoliciesExist(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) (bool, []string, []*unstructured.Unstructured) {
	var managedPoliciesMissing []string
	var managedPoliciesPresent []*unstructured.Unstructured

	// Go through the managedPolicies in the CR and make sure they exist.
	for _, managedPolicy := range clusterGroupUpgrade.Spec.ManagedPolicies {
		foundPolicy := &unstructured.Unstructured{}
		foundPolicy.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "policy.open-cluster-management.io",
			Kind:    "Policy",
			Version: "v1",
		})

		// Look for policy.
		err := r.Client.Get(ctx, client.ObjectKey{
			Name:      managedPolicy,
			Namespace: clusterGroupUpgrade.Namespace,
		}, foundPolicy)

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

func (r *ClusterGroupUpgradeReconciler) copyManagedInformPolicyForBatch(
	ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, policies []*unstructured.Unstructured, batchIndex int) ([]string, error) {
	var newPolicies []string

	// Go through the managedPolicies in the CR and make sure they exist.
	for managedPolicyIndex, managedPolicy := range policies {
		// Create a new unstructured variable to keep all the information for the new policy.
		newPolicy := &unstructured.Unstructured{}

		// Set new policy name, namespace, group, kind and version.
		newPolicy.SetName(clusterGroupUpgrade.Name + "-batch" + strconv.Itoa(batchIndex) + "-policy" + strconv.Itoa(managedPolicyIndex+1))
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
		labels["openshift-cluster-group-upgrades/batch"] = strconv.Itoa(batchIndex)
		labels["openshift-cluster-group-upgrades/parentPolicyName"] = managedPolicy.GetName()
		newPolicy.SetLabels(labels)

		// Set new policy annotations - copy them from the managed policy.
		newPolicy.SetAnnotations(managedPolicy.GetAnnotations())

		// Set new policy remediationAction.
		newPolicy.Object["spec"] = managedPolicy.Object["spec"]
		specObject := newPolicy.Object["spec"].(map[string]interface{})
		specObject["remediationAction"] = "inform"

		// Create the new policy in the desired namespace.
		err := r.createNewPolicyFromStructure(ctx, clusterGroupUpgrade, newPolicy)
		if err != nil {
			r.Log.Info("Error creating policy", "err", err)
			return nil, err
		}

		// Update the list of new policy names.
		newPolicies = append(newPolicies, newPolicy.GetName())
	}

	return newPolicies, nil
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

func (r *ClusterGroupUpgradeReconciler) ensureBatchPlacementRules(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, batch []string, batchIndex int) ([]string, error) {
	var placementRules []string

	pr, err := r.newBatchPlacementRule(ctx, clusterGroupUpgrade, batch, batchIndex)
	if err != nil {
		return nil, err
	}
	placementRules = append(placementRules, pr.GetName())

	if err := controllerutil.SetControllerReference(clusterGroupUpgrade, pr, r.Scheme); err != nil {
		return nil, err
	}

	foundPlacementRule := &unstructured.Unstructured{}
	foundPlacementRule.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "apps.open-cluster-management.io",
		Kind:    "PlacementRule",
		Version: "v1",
	})
	err = r.Client.Get(ctx, client.ObjectKey{
		Name:      pr.GetName(),
		Namespace: clusterGroupUpgrade.Namespace,
	}, foundPlacementRule)

	if err != nil {
		if errors.IsNotFound(err) {
			err = r.Client.Create(ctx, pr)
			if err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
	} else {
		err = r.Client.Update(ctx, foundPlacementRule)
		if err != nil {
			return nil, err
		}
	}

	return placementRules, nil
}

func (r *ClusterGroupUpgradeReconciler) newBatchPlacementRule(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, batch []string, batchIndex int) (*unstructured.Unstructured, error) {
	u := &unstructured.Unstructured{}
	u.Object = map[string]interface{}{
		"metadata": map[string]interface{}{
			"name":      clusterGroupUpgrade.Name + "-batch" + strconv.Itoa(batchIndex),
			"namespace": clusterGroupUpgrade.Namespace,
			"labels": map[string]interface{}{
				"app": "openshift-cluster-group-upgrades",
				"openshift-cluster-group-upgrades/clusterGroupUpgrade": clusterGroupUpgrade.Name,
				"openshift-cluster-group-upgrades/batch":               strconv.Itoa(batchIndex),
			},
		},
		"spec": map[string]interface{}{
			"clusterConditions": []map[string]interface{}{
				{
					"type":   "ManagedClusterConditionAvailable",
					"status": "True",
				},
			},
		},
	}

	var clusters []map[string]interface{}
	for _, cluster := range batch {
		clusters = append(clusters, map[string]interface{}{"name": cluster})
	}
	specObject := u.Object["spec"].(map[string]interface{})
	specObject["clusters"] = clusters

	u.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "apps.open-cluster-management.io",
		Kind:    "PlacementRule",
		Version: "v1",
	})

	return u, nil
}

// TODO: this function is obsolete, delete as part of cleanup
func (r *ClusterGroupUpgradeReconciler) ensureBatchPolicies(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, batch []string, batchIndex int) ([]string, error) {
	var policies []string

	if clusterGroupUpgrade.Spec.PlatformUpgrade != nil {
		policy, err := r.newBatchPlatformUpgradePolicy(ctx, clusterGroupUpgrade, batchIndex)
		if err != nil {
			return nil, err
		}
		policies = append(policies, policy.GetName())

		if err := controllerutil.SetControllerReference(clusterGroupUpgrade, policy, r.Scheme); err != nil {
			return nil, err
		}

		foundPolicy := &unstructured.Unstructured{}
		foundPolicy.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "policy.open-cluster-management.io",
			Kind:    "Policy",
			Version: "v1",
		})
		err = r.Client.Get(ctx, client.ObjectKey{
			Name:      policy.GetName(),
			Namespace: clusterGroupUpgrade.Namespace,
		}, foundPolicy)

		if err != nil {
			if errors.IsNotFound(err) {
				err = r.Client.Create(ctx, policy)
				if err != nil {
					return nil, err
				}
			} else {
				return nil, err
			}
		} else {
			err = r.Client.Update(ctx, foundPolicy)
			if err != nil {
				return nil, err
			}
		}
	}
	if clusterGroupUpgrade.Spec.OperatorUpgrades != nil {
		policy, err := r.newBatchOperatorUpgradePolicy(ctx, clusterGroupUpgrade, batchIndex)
		if err != nil {
			return nil, err
		}
		policies = append(policies, policy.GetName())

		if err := controllerutil.SetControllerReference(clusterGroupUpgrade, policy, r.Scheme); err != nil {
			return nil, err
		}

		foundPolicy := &unstructured.Unstructured{}
		foundPolicy.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "policy.open-cluster-management.io",
			Kind:    "Policy",
			Version: "v1",
		})
		err = r.Client.Get(ctx, client.ObjectKey{
			Name:      policy.GetName(),
			Namespace: clusterGroupUpgrade.Namespace,
		}, foundPolicy)

		if err != nil {
			if errors.IsNotFound(err) {
				err = r.Client.Create(ctx, policy)
				if err != nil {
					return nil, err
				}
			} else {
				return nil, err
			}
		} else {
			err = r.Client.Update(ctx, foundPolicy)
			if err != nil {
				return nil, err
			}
		}
	}

	return policies, nil
}

func (r *ClusterGroupUpgradeReconciler) ensureBatchPlatformUpgradePolicyIsEnforced(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, policy *unstructured.Unstructured) error {
	if policy != nil {
		specObject := policy.Object["spec"].(map[string]interface{})
		if specObject["remediationAction"] == "inform" {
			specObject["remediationAction"] = "enforce"
			err := r.Client.Update(ctx, policy)
			if err != nil {
				return err
			}
			clusterGroupUpgrade.Status.Status.CurrentBatchStartedAt = metav1.Now()
			r.Log.Info("Set remediationAction to enforce on Policy", "policy", policy.GetName())
		}
	}

	return nil
}

func (r *ClusterGroupUpgradeReconciler) ensureBatchOperatorUpgradePolicyIsEnforced(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, policy *unstructured.Unstructured) error {
	if policy != nil {
		specObject := policy.Object["spec"].(map[string]interface{})
		if specObject["remediationAction"] == "inform" {
			specObject["remediationAction"] = "enforce"
			err := r.Client.Update(ctx, policy)
			if err != nil {
				return err
			}
			if clusterGroupUpgrade.Status.Status.CurrentBatchStartedAt.IsZero() {
				clusterGroupUpgrade.Status.Status.CurrentBatchStartedAt = metav1.Now()
			}
			r.Log.Info("Set remediationAction to enforce on Policy", "policy", policy.GetName())
		}
	}

	return nil
}

// TODO: this function is obsolete, delete as part of cleanup
func (r *ClusterGroupUpgradeReconciler) newBatchPlatformUpgradePolicy(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, batchIndex int) (*unstructured.Unstructured, error) {
	var buf bytes.Buffer
	tmpl := template.New("cluster-upgrade-policy")
	tmpl.Parse(platformUpgradeTemplate)
	tmpl.Execute(&buf, clusterGroupUpgrade.Spec.PlatformUpgrade)
	u := &unstructured.Unstructured{}
	dec := yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)
	_, _, err := dec.Decode(buf.Bytes(), nil, u)
	if err != nil {
		return nil, err
	}

	u.SetName(clusterGroupUpgrade.Name + "-" + "platform" + "-" + strconv.Itoa(batchIndex))
	u.SetNamespace(clusterGroupUpgrade.GetNamespace())
	labels := u.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	labels["app"] = "openshift-cluster-group-upgrades"
	labels["openshift-cluster-group-upgrades/clusterGroupUpgrade"] = clusterGroupUpgrade.Name
	labels["openshift-cluster-group-upgrades/batch"] = strconv.Itoa(batchIndex)
	labels["openshift-cluster-group-upgrades/policyType"] = "platformUpgrade"
	u.SetLabels(labels)

	specObject := u.Object["spec"].(map[string]interface{})
	specObject["remediationAction"] = "inform"

	return u, nil
}

// TODO: this function is obsolete, delete as part of cleanup
func (r *ClusterGroupUpgradeReconciler) newBatchOperatorUpgradePolicy(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, batchIndex int) (*unstructured.Unstructured, error) {
	var buf bytes.Buffer
	tmpl := template.New("operator-upgrade-policy")
	tmpl.Parse(operatorUpgradeTemplate)
	tmpl.Execute(&buf, clusterGroupUpgrade.Spec.OperatorUpgrades)
	u := &unstructured.Unstructured{}
	dec := yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)
	_, _, err := dec.Decode(buf.Bytes(), nil, u)
	if err != nil {
		return nil, err
	}

	u.SetName(clusterGroupUpgrade.Name + "-" + "operator" + "-" + strconv.Itoa(batchIndex))
	u.SetNamespace(clusterGroupUpgrade.GetNamespace())
	labels := u.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	labels["app"] = "openshift-cluster-group-upgrades"
	labels["openshift-cluster-group-upgrades/clusterGroupUpgrade"] = clusterGroupUpgrade.Name
	labels["openshift-cluster-group-upgrades/batch"] = strconv.Itoa(batchIndex)
	labels["openshift-cluster-group-upgrades/policyType"] = "operatorUpgrade"
	u.SetLabels(labels)

	specObject := u.Object["spec"].(map[string]interface{})
	specObject["remediationAction"] = "inform"

	return u, nil
}

func (r *ClusterGroupUpgradeReconciler) isBatchPlatformUpgradePolicyCompliant(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) (bool, error) {
	var result bool
	policy, err := r.getBatchPlatformUpgradePolicy(ctx, clusterGroupUpgrade)
	if err != nil {
		return false, err
	}

	if policy != nil {
		statusObject := policy.Object["status"].(map[string]interface{})
		if statusObject["compliant"] == nil || statusObject["compliant"] == "NonCompliant" {
			result = false
		} else {
			result = true
		}
	}

	return result, nil
}

func (r *ClusterGroupUpgradeReconciler) isBatchOperatorUpgradePolicyCompliant(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) (bool, error) {
	var result bool
	policy, err := r.getBatchOperatorUpgradePolicy(ctx, clusterGroupUpgrade)
	if err != nil {
		return false, err
	}

	if policy != nil {
		statusObject := policy.Object["status"].(map[string]interface{})
		if statusObject["compliant"] == nil || statusObject["compliant"] == "NonCompliant" {
			result = false
		} else {
			result = true
		}
	}

	return result, nil
}

func (r *ClusterGroupUpgradeReconciler) areAllPoliciesCompliant(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) (bool, error) {
	policies, err := r.getPolicies(ctx, clusterGroupUpgrade)
	if err != nil {
		return false, err
	}

	areAllPoliciesCompliant := true
	for _, policy := range policies.Items {
		statusObject := policy.Object["status"].(map[string]interface{})
		if statusObject["compliant"] != "Compliant" {
			areAllPoliciesCompliant = false
		}
	}

	return areAllPoliciesCompliant, nil
}

func (r *ClusterGroupUpgradeReconciler) ensureBatchPlacementBindings(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, batchPlacementRules []string, batchPolicies []string, batchIndex int) ([]string, error) {
	var placementBindings []string

	// ensure batch placement bindings
	pb, err := r.newBatchPlacementBinding(ctx, clusterGroupUpgrade, batchPlacementRules, batchPolicies, batchIndex)
	if err != nil {
		return nil, err
	}
	placementBindings = append(placementBindings, pb.GetName())

	if err := controllerutil.SetControllerReference(clusterGroupUpgrade, pb, r.Scheme); err != nil {
		return nil, err
	}

	foundPlacementBinding := &unstructured.Unstructured{}
	foundPlacementBinding.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "policy.open-cluster-management.io",
		Kind:    "PlacementBinding",
		Version: "v1",
	})
	err = r.Client.Get(ctx, client.ObjectKey{
		Name:      pb.GetName(),
		Namespace: clusterGroupUpgrade.Namespace,
	}, foundPlacementBinding)

	if err != nil {
		if errors.IsNotFound(err) {
			err = r.Client.Create(ctx, pb)
			if err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
	} else {
		err = r.Client.Update(ctx, foundPlacementBinding)
		if err != nil {
			return nil, err
		}
	}

	return placementBindings, nil
}

func (r *ClusterGroupUpgradeReconciler) newBatchPlacementBinding(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, batchPlacementRules []string, batchPolicies []string, batchIndex int) (*unstructured.Unstructured, error) {
	var subjects []map[string]interface{}

	for _, batchPolicy := range batchPolicies {
		subject := make(map[string]interface{})
		subject["name"] = batchPolicy
		subject["kind"] = "Policy"
		subject["apiGroup"] = "policy.open-cluster-management.io"
		subjects = append(subjects, subject)
	}

	u := &unstructured.Unstructured{}
	u.Object = map[string]interface{}{
		"metadata": map[string]interface{}{
			"name":      clusterGroupUpgrade.Name + "-batch" + strconv.Itoa(batchIndex),
			"namespace": clusterGroupUpgrade.Namespace,
			"labels": map[string]interface{}{
				"app": "openshift-cluster-group-upgrades",
				"openshift-cluster-group-upgrades/clusterGroupUpgrade": clusterGroupUpgrade.Name,
			},
		},
		"placementRef": map[string]interface{}{
			"name":     batchPlacementRules[0],
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

func (r *ClusterGroupUpgradeReconciler) getPlacementRules(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) (*unstructured.UnstructuredList, error) {
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

func (r *ClusterGroupUpgradeReconciler) getPlatformUpgradePolicies(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) (*unstructured.UnstructuredList, error) {
	var policyLabels = map[string]string{
		"openshift-cluster-group-upgrades/clusterGroupUpgrade": clusterGroupUpgrade.Name,
		"openshift-cluster-group-upgrades/policyType":          "platformUpgrade",
	}
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

func (r *ClusterGroupUpgradeReconciler) getOperatorUpgradePolicies(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) (*unstructured.UnstructuredList, error) {
	var policyLabels = map[string]string{
		"openshift-cluster-group-upgrades/clusterGroupUpgrade": clusterGroupUpgrade.Name,
		"openshift-cluster-group-upgrades/policyType":          "operatorUpgrade",
	}
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

func (r *ClusterGroupUpgradeReconciler) getBatchPlatformUpgradePolicy(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) (*unstructured.Unstructured, error) {
	var platformUpgradeBatchPolicyLabels = map[string]string{
		"openshift-cluster-group-upgrades/clusterGroupUpgrade": clusterGroupUpgrade.Name,
		"openshift-cluster-group-upgrades/batch":               strconv.Itoa(clusterGroupUpgrade.Status.Status.CurrentBatch),
		"openshift-cluster-group-upgrades/policyType":          "platformUpgrade",
	}
	listOpts := []client.ListOption{
		client.InNamespace(clusterGroupUpgrade.Namespace),
		client.MatchingLabels(platformUpgradeBatchPolicyLabels),
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

	if len(policiesList.Items) == 0 {
		return nil, nil
	}
	return &policiesList.Items[0], nil
}

func (r *ClusterGroupUpgradeReconciler) getBatchOperatorUpgradePolicy(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) (*unstructured.Unstructured, error) {
	var platformUpgradeBatchPolicyLabels = map[string]string{
		"openshift-cluster-group-upgrades/clusterGroupUpgrade": clusterGroupUpgrade.Name,
		"openshift-cluster-group-upgrades/batch":               strconv.Itoa(clusterGroupUpgrade.Status.Status.CurrentBatch),
		"openshift-cluster-group-upgrades/policyType":          "operatorUpgrade",
	}
	listOpts := []client.ListOption{
		client.InNamespace(clusterGroupUpgrade.Namespace),
		client.MatchingLabels(platformUpgradeBatchPolicyLabels),
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

	if len(policiesList.Items) == 0 {
		return nil, nil
	}
	return &policiesList.Items[0], nil
}

func (r *ClusterGroupUpgradeReconciler) reconcileResources(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, managedPoliciesPresent []*unstructured.Unstructured) error {
	// Reconcile resources
	for i, remediateBatch := range clusterGroupUpgrade.Status.RemediationPlan {
		batchPlacementRules, err := r.ensureBatchPlacementRules(ctx, clusterGroupUpgrade, remediateBatch, i+1)
		if err != nil {
			return err
		}

		batchPolicies, err := r.copyManagedInformPolicyForBatch(ctx, clusterGroupUpgrade, managedPoliciesPresent, i+1)
		if err != nil {
			return err
		}

		_, err = r.ensureBatchPlacementBindings(ctx, clusterGroupUpgrade, batchPlacementRules, batchPolicies, i+1)
		if err != nil {
			return err
		}
	}

	return nil
}

func (r *ClusterGroupUpgradeReconciler) buildRemediationPlan(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) {
	// Create remediation plan
	var remediationPlan [][]string
	isCanary := make(map[string]bool)
	if clusterGroupUpgrade.Spec.RemediationStrategy.Canaries != nil && len(clusterGroupUpgrade.Spec.RemediationStrategy.Canaries) > 0 {
		for _, canary := range clusterGroupUpgrade.Spec.RemediationStrategy.Canaries {
			remediationPlan = append(remediationPlan, []string{canary})
			isCanary[canary] = true
		}

	}
	var clusters []string
	for _, cluster := range clusterGroupUpgrade.Spec.Clusters {
		if !isCanary[cluster] {
			clusters = append(clusters, cluster)
		}
	}
	for i := 0; i < len(clusters); i += clusterGroupUpgrade.Spec.RemediationStrategy.MaxConcurrency {
		var batch []string
		for j := i; j < i+clusterGroupUpgrade.Spec.RemediationStrategy.MaxConcurrency && j != len(clusters); j++ {
			site := clusters[j]
			if !isCanary[site] {
				batch = append(batch, site)
			}
		}
		if len(batch) > 0 {
			remediationPlan = append(remediationPlan, batch)
		}
	}
	r.Log.Info("Remediation plan", "remediatePlan", remediationPlan)
	clusterGroupUpgrade.Status.RemediationPlan = remediationPlan
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
		placementRules, err := r.getPlacementRules(ctx, clusterGroupUpgrade)
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

		platformUpgradePolicyStatus := make([]ranv1alpha1.PolicyStatus, 0)
		platformUpgradePolicies, err := r.getPlatformUpgradePolicies(ctx, clusterGroupUpgrade)
		if err != nil {
			return err
		}
		for _, policy := range platformUpgradePolicies.Items {
			policyStatus := ranv1alpha1.PolicyStatus{}
			policyStatus.Name = policy.GetName()
			statusObject := policy.Object["status"].(map[string]interface{})
			if statusObject == nil || statusObject["compliant"] == nil {
				policyStatus.ComplianceState = "NonCompliant"
			} else {
				policyStatus.ComplianceState = statusObject["compliant"].(string)
			}
			platformUpgradePolicyStatus = append(platformUpgradePolicyStatus, policyStatus)
		}
		clusterGroupUpgrade.Status.Status.PlatformUpgradePolicies = platformUpgradePolicyStatus

		operatorUpgradePolicyStatus := make([]ranv1alpha1.PolicyStatus, 0)
		operatorUpgradePolicies, err := r.getOperatorUpgradePolicies(ctx, clusterGroupUpgrade)
		if err != nil {
			return err
		}
		for _, policy := range operatorUpgradePolicies.Items {
			policyStatus := ranv1alpha1.PolicyStatus{}
			policyStatus.Name = policy.GetName()
			statusObject := policy.Object["status"].(map[string]interface{})
			if statusObject == nil || statusObject["compliant"] == nil {
				policyStatus.ComplianceState = "NonCompliant"
			} else {
				policyStatus.ComplianceState = statusObject["compliant"].(string)
			}
			operatorUpgradePolicyStatus = append(operatorUpgradePolicyStatus, policyStatus)
		}
		clusterGroupUpgrade.Status.Status.OperatorUpgradePolicies = operatorUpgradePolicyStatus

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
