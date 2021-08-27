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
	"strconv"
	"text/template"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	ranv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/api/v1alpha1"
)

// ClusterGroupUpgradeReconciler reconciles a ClusterGroupUpgrade object
type ClusterGroupUpgradeReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
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

	ClusterGroupUpgrade := &ranv1alpha1.ClusterGroupUpgrade{}
	err := r.Get(ctx, req.NamespacedName, ClusterGroupUpgrade)
	if err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		r.Log.Error(err, "Failed to get ClusterGroupUpgrade")
		return ctrl.Result{}, err
	}

	// Create remediation plan
	var remediationPlan [][]string
	isCanary := make(map[string]bool)
	if ClusterGroupUpgrade.Spec.RemediationStrategy.Canaries != nil && len(ClusterGroupUpgrade.Spec.RemediationStrategy.Canaries) > 0 {
		for _, canary := range ClusterGroupUpgrade.Spec.RemediationStrategy.Canaries {
			remediationPlan = append(remediationPlan, []string{canary})
			isCanary[canary] = true
		}

	}
	var clusters []string
	for _, cluster := range ClusterGroupUpgrade.Spec.Clusters {
		if !isCanary[cluster] {
			clusters = append(clusters, cluster)
		}
	}
	for i := 0; i < len(clusters); i += ClusterGroupUpgrade.Spec.RemediationStrategy.MaxConcurrency {
		var batch []string
		for j := i; j < i+ClusterGroupUpgrade.Spec.RemediationStrategy.MaxConcurrency && j != len(clusters); j++ {
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

	// Reconcile resources
	var placementRules []string
	var placementBindings []string
	var policies []string
	for i, remediateBatch := range remediationPlan {
		placementRulesForBatch, err := r.ensureBatchPlacementRules(ctx, ClusterGroupUpgrade, remediateBatch, i+1)
		if err != nil {
			return ctrl.Result{}, err
		}
		placementRules = append(placementRules, placementRulesForBatch...)
		policiesForBatch, err := r.ensureBatchPolicies(ctx, ClusterGroupUpgrade, remediateBatch, i+1)
		if err != nil {
			return ctrl.Result{}, err
		}
		policies = append(policies, policiesForBatch...)
		placementBindingsForBatch, err := r.ensureBatchPlacementBindings(ctx, ClusterGroupUpgrade, remediateBatch, i+1)
		if err != nil {
			return ctrl.Result{}, err
		}
		placementBindings = append(placementBindings, placementBindingsForBatch...)
	}

	// Remediate policies depending on compliance state and upgrade plan.
	if ClusterGroupUpgrade.Spec.RemediationAction == "enforce" {
		for i, remediateBatch := range remediationPlan {
			r.Log.Info("Remediating clusters", "remediateBatch", remediateBatch)
			r.Log.Info("Batch", "i", i+1)
			batchCompliant := true
			var labelsForBatch = map[string]string{"openshift-cluster-group-upgrades/batch": strconv.Itoa(i + 1)}
			listOpts := []client.ListOption{
				client.InNamespace(ClusterGroupUpgrade.Namespace),
				client.MatchingLabels(labelsForBatch),
			}
			policiesList := &unstructured.UnstructuredList{}
			policiesList.SetGroupVersionKind(schema.GroupVersionKind{
				Group:   "policy.open-cluster-management.io",
				Kind:    "PolicyList",
				Version: "v1",
			})
			if err := r.List(ctx, policiesList, listOpts...); err != nil {
				return ctrl.Result{}, err
			}
			for _, policy := range policiesList.Items {
				specObject := policy.Object["spec"].(map[string]interface{})
				if specObject["remediationAction"] == "inform" {
					specObject["remediationAction"] = "enforce"
					err = r.Client.Update(ctx, &policy)
					if err != nil {
						return ctrl.Result{}, err
					}
					r.Log.Info("Set remediationAction to enforce on Policy object", "policy", policy)
				}

				statusObject := policy.Object["status"].(map[string]interface{})
				if statusObject["compliant"] == nil || statusObject["compliant"] != "Compliant" {
					batchCompliant = false
				}
			}

			if !batchCompliant {
				r.Log.Info("Remediate batch not fully compliant yet")
				break
			} else {
				r.Log.Info("Remediate batch fully compliant")
			}
		}
	}

	// Update status
	err = r.updateStatus(ctx, ClusterGroupUpgrade, placementRules, placementBindings, policies)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Delete old resources
	r.deleteOldResources(ctx, ClusterGroupUpgrade)

	return ctrl.Result{}, nil
}

func (r *ClusterGroupUpgradeReconciler) ensureBatchPlacementRules(ctx context.Context, ClusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, batch []string, batchIndex int) ([]string, error) {
	var placementRules []string

	pr, err := r.newBatchPlacementRule(ctx, ClusterGroupUpgrade, batch, batchIndex)
	if err != nil {
		return nil, err
	}
	placementRules = append(placementRules, pr.GetName())

	if err := controllerutil.SetControllerReference(ClusterGroupUpgrade, pr, r.Scheme); err != nil {
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
		Namespace: ClusterGroupUpgrade.Namespace,
	}, foundPlacementRule)

	if err != nil {
		if errors.IsNotFound(err) {
			err = r.Client.Create(ctx, pr)
			if err != nil {
				return nil, err
			}
			r.Log.Info("Created API PlacementRule object", "placementRule", pr.GetName())
		} else {
			return nil, err
		}
	} else {
		if !equality.Semantic.DeepEqual(foundPlacementRule.Object["spec"], pr.Object["spec"]) {
			err = r.Client.Update(ctx, foundPlacementRule)
			if err != nil {
				return nil, err
			}
			r.Log.Info("Updated API PlacementRule object", "placementRule", foundPlacementRule.GetName())
		}
	}

	return placementRules, nil
}

func (r *ClusterGroupUpgradeReconciler) newBatchPlacementRule(ctx context.Context, ClusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, batch []string, batchIndex int) (*unstructured.Unstructured, error) {
	u := &unstructured.Unstructured{}
	u.Object = map[string]interface{}{
		"metadata": map[string]interface{}{
			"name":      ClusterGroupUpgrade.Name + "-" + "batch" + "-" + strconv.Itoa(batchIndex),
			"namespace": ClusterGroupUpgrade.Namespace,
			"labels": map[string]interface{}{
				"app": "openshift-cluster-group-upgrades",
				"openshift-cluster-group-upgrades/clusterGroupUpgrade": ClusterGroupUpgrade.Name,
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

func (r *ClusterGroupUpgradeReconciler) ensureBatchPolicies(ctx context.Context, ClusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, batch []string, batchIndex int) ([]string, error) {
	var policies []string

	policy, err := r.newBatchPolicy(ctx, ClusterGroupUpgrade, batchIndex)
	if err != nil {
		return nil, err
	}
	policies = append(policies, policy.GetName())

	if err := controllerutil.SetControllerReference(ClusterGroupUpgrade, policy, r.Scheme); err != nil {
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
		Namespace: ClusterGroupUpgrade.Namespace,
	}, foundPolicy)

	if err != nil {
		if errors.IsNotFound(err) {
			err = r.Client.Create(ctx, policy)
			if err != nil {
				return nil, err
			}
			r.Log.Info("Created API Policy object", "policy", policy.GetName())
		} else {
			return nil, err
		}
	} else {
		if !equality.Semantic.DeepEqual(foundPolicy.Object["spec"], policy.Object["spec"]) {
			foundPolicy.Object["spec"] = policy.Object["spec"]
			unstructured.SetNestedField(foundPolicy.Object, ClusterGroupUpgrade.Spec.RemediationAction, "spec", "remediationAction")
			err = r.Client.Update(ctx, foundPolicy)
			if err != nil {
				return nil, err
			}
			r.Log.Info("Updated API Policy object", "policy", foundPolicy.GetName())
		}
	}

	return policies, nil
}

func (r *ClusterGroupUpgradeReconciler) newBatchPolicy(ctx context.Context, ClusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, batchIndex int) (*unstructured.Unstructured, error) {
	var buf bytes.Buffer
	tmpl := template.New("policy")
	tmpl.Parse(platformUpgradeTemplate)
	tmpl.Execute(&buf, map[string]string{"Channel": ClusterGroupUpgrade.Spec.PlatformUpgrade.Channel,
		"Version":  ClusterGroupUpgrade.Spec.PlatformUpgrade.DesiredUpdate.Version,
		"Image":    ClusterGroupUpgrade.Spec.PlatformUpgrade.DesiredUpdate.Image,
		"Force":    strconv.FormatBool(ClusterGroupUpgrade.Spec.PlatformUpgrade.DesiredUpdate.Force),
		"Upstream": ClusterGroupUpgrade.Spec.PlatformUpgrade.Upstream,
	})
	u := &unstructured.Unstructured{}
	dec := yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)
	_, _, err := dec.Decode(buf.Bytes(), nil, u)
	if err != nil {
		return nil, err
	}

	u.SetName(ClusterGroupUpgrade.Name + "-" + "batch" + "-" + strconv.Itoa(batchIndex))
	u.SetNamespace(ClusterGroupUpgrade.GetNamespace())
	labels := u.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	labels["app"] = "openshift-cluster-group-upgrades"
	labels["openshift-cluster-group-upgrades/clusterGroupUpgrade"] = ClusterGroupUpgrade.Name
	labels["openshift-cluster-group-upgrades/batch"] = strconv.Itoa(batchIndex)
	u.SetLabels(labels)

	specObject := u.Object["spec"].(map[string]interface{})
	specObject["remediationAction"] = "inform"

	return u, nil
}

func (r *ClusterGroupUpgradeReconciler) ensureBatchPlacementBindings(ctx context.Context, ClusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, batch []string, batchIndex int) ([]string, error) {
	var placementBindings []string

	// ensure batch placement bindings
	pb, err := r.newBatchPlacementBinding(ctx, ClusterGroupUpgrade, batchIndex)
	if err != nil {
		return nil, err
	}
	placementBindings = append(placementBindings, pb.GetName())

	if err := controllerutil.SetControllerReference(ClusterGroupUpgrade, pb, r.Scheme); err != nil {
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
		Namespace: ClusterGroupUpgrade.Namespace,
	}, foundPlacementBinding)

	if err != nil {
		if errors.IsNotFound(err) {
			err = r.Client.Create(ctx, pb)
			if err != nil {
				return nil, err
			}
			r.Log.Info("Created API PlacementBinding object", "placementBinding", pb.GetName())
		} else {
			return nil, err
		}
	} else {
		if !equality.Semantic.DeepEqual(foundPlacementBinding.Object["placementRef"], pb.Object["placementRef"]) || !equality.Semantic.DeepEqual(foundPlacementBinding.Object["subjects"], pb.Object["subjects"]) {
			foundPlacementBinding.Object["placementRef"] = pb.Object["placementRef"]
			foundPlacementBinding.Object["subjects"] = pb.Object["subjects"]
			err = r.Client.Update(ctx, foundPlacementBinding)
			if err != nil {
				return nil, err
			}
			r.Log.Info("Updated API PlacementBinding object", "placementBinding", foundPlacementBinding.GetName())
		}
	}

	return placementBindings, nil
}

func (r *ClusterGroupUpgradeReconciler) newBatchPlacementBinding(ctx context.Context, ClusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, batchIndex int) (*unstructured.Unstructured, error) {
	var subjects []map[string]interface{}

	subject := make(map[string]interface{})
	subject["name"] = ClusterGroupUpgrade.Name + "-" + "batch" + "-" + strconv.Itoa(batchIndex)
	subject["kind"] = "Policy"
	subject["apiGroup"] = "policy.open-cluster-management.io"

	subjects = append(subjects, subject)

	u := &unstructured.Unstructured{}
	u.Object = map[string]interface{}{
		"metadata": map[string]interface{}{
			"name":      ClusterGroupUpgrade.Name + "-" + "batch" + "-" + strconv.Itoa(batchIndex),
			"namespace": ClusterGroupUpgrade.Namespace,
			"labels": map[string]interface{}{
				"app": "openshift-cluster-group-upgrades",
				"openshift-cluster-group-upgrades/clusterGroupUpgrade": ClusterGroupUpgrade.Name,
			},
		},
		"placementRef": map[string]interface{}{
			"name":     ClusterGroupUpgrade.Name + "-" + "batch" + "-" + strconv.Itoa(batchIndex),
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

func (r *ClusterGroupUpgradeReconciler) updateStatus(ctx context.Context, ClusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, placementRules []string, placementBindings []string, policies []string) error {
	ClusterGroupUpgrade.Status.PlacementRules = placementRules
	ClusterGroupUpgrade.Status.PlacementBindings = placementBindings

	var policiesStatus []ranv1alpha1.PolicyStatus
	foundPolicy := &unstructured.Unstructured{}
	foundPolicy.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "policy.open-cluster-management.io",
		Kind:    "Policy",
		Version: "v1",
	})
	for _, policy := range policies {
		err := r.Client.Get(ctx, client.ObjectKey{
			Name:      policy,
			Namespace: ClusterGroupUpgrade.Namespace,
		}, foundPolicy)
		if err != nil {
			return err
		}

		policyStatus := &ranv1alpha1.PolicyStatus{}
		policyStatus.Name = foundPolicy.GetName()
		if foundPolicy.Object["status"] == nil {
			policyStatus.ComplianceState = "NonCompliant"
		} else {
			statusObject := foundPolicy.Object["status"].(map[string]interface{})
			if statusObject["compliant"] != nil {
				policyStatus.ComplianceState = statusObject["compliant"].(string)
			}
		}
		policiesStatus = append(policiesStatus, *policyStatus)
	}
	ClusterGroupUpgrade.Status.Policies = policiesStatus

	err := r.Status().Update(ctx, ClusterGroupUpgrade)
	if err != nil {
		return err
	}
	r.Log.Info("Updated ClusterGroupUpgrade status")

	return nil
}

func (r *ClusterGroupUpgradeReconciler) deleteOldResources(ctx context.Context, ClusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) error {
	var labelsForClusterGroupUpgrade = map[string]string{"app": "openshift-cluster-group-upgrades", "openshift-cluster-group-upgrades/clusterGroupUpgrade": ClusterGroupUpgrade.GetName()}
	listOpts := []client.ListOption{
		client.InNamespace(ClusterGroupUpgrade.Namespace),
		client.MatchingLabels(labelsForClusterGroupUpgrade),
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
	for _, foundPlacementRule := range placementRulesList.Items {
		foundInStatus := false
		for _, statusPlacementRule := range ClusterGroupUpgrade.Status.PlacementRules {
			if foundPlacementRule.GetName() == statusPlacementRule {
				foundInStatus = true
				break
			}
		}
		if !foundInStatus {
			err := r.Delete(ctx, &foundPlacementRule)
			if err != nil {
				return err
			}
			r.Log.Info("Deleted API PlacementRule object", "foundPlacementRule", foundPlacementRule)
		}
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
	for _, foundPlacementBinding := range placementBindingsList.Items {
		foundInStatus := false
		for _, statusPlacementBinding := range ClusterGroupUpgrade.Status.PlacementBindings {
			if foundPlacementBinding.GetName() == statusPlacementBinding {
				foundInStatus = true
				break
			}
		}
		if !foundInStatus {
			err := r.Delete(ctx, &foundPlacementBinding)
			if err != nil {
				return err
			}
			r.Log.Info("Deleted API PlacementBinding object", "foundPlacementBinding", foundPlacementBinding)
		}
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
	for _, foundPolicy := range policiesList.Items {
		foundInStatus := false
		for _, statusPolicy := range ClusterGroupUpgrade.Status.Policies {
			if foundPolicy.GetName() == statusPolicy.Name {
				foundInStatus = true
				break
			}
		}
		if !foundInStatus {
			err := r.Delete(ctx, &foundPolicy)
			if err != nil {
				return err
			}
			r.Log.Info("Deleted API Policy object", "foundPolicy", foundPolicy)
		}
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterGroupUpgradeReconciler) SetupWithManager(mgr ctrl.Manager) error {
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
