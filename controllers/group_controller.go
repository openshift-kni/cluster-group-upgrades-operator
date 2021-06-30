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
	"strconv"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	//	"sigs.k8s.io/controller-runtime/pkg/event"
	//	"sigs.k8s.io/controller-runtime/pkg/predicate"

	ranv1alpha1 "github.com/redhat-ztp/cluster-group-lcm/api/v1alpha1"
)

// GroupReconciler reconciles a Group object
type GroupReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=ran.openshift.io,resources=groups,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=ran.openshift.io,resources=groups/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=ran.openshift.io,resources=groups/finalizers,verbs=update
//+kubebuilder:rbac:groups=apps.open-cluster-management.io,resources=placementrules,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=policy.open-cluster-management.io,resources=placementbindings,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=policy.open-cluster-management.io,resources=policies,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=cluster.open-cluster-management.io,resources=managedclusters,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Group object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.8.3/pkg/reconcile
func (r *GroupReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = r.Log.WithValues("group", req.NamespacedName)

	group := &ranv1alpha1.Group{}
	err := r.Get(ctx, req.NamespacedName, group)
	if err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		r.Log.Error(err, "Failed to get Group")
		return ctrl.Result{}, err
	}

	// Create remediation plan
	var remediationPlan [][]string
	isCanary := make(map[string]bool)
	if group.Spec.RemediationStrategy.Canaries != nil && len(group.Spec.RemediationStrategy.Canaries) > 0 {
		for _, canary := range group.Spec.RemediationStrategy.Canaries {
			remediationPlan = append(remediationPlan, []string{canary})
			isCanary[canary] = true
		}

	}
	var sites []string
	for _, site := range group.Spec.Sites {
		if !isCanary[site] {
			sites = append(sites, site)
		}
	}
	for i := 0; i < len(sites); i += group.Spec.RemediationStrategy.MaxConcurrency {
		var batch []string
		for j := i; j < i+group.Spec.RemediationStrategy.MaxConcurrency && j != len(sites); j++ {
			site := sites[j]
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
	for i, remediateBatch := range remediationPlan {
		err = r.ensureBatchPlacementRules(ctx, group, remediateBatch, i+1)
		if err != nil {
			return ctrl.Result{}, err
		}
		err = r.ensureBatchPolicies(ctx, group, remediateBatch, i+1)
		if err != nil {
			return ctrl.Result{}, err
		}
		err = r.ensureBatchPlacementBindings(ctx, group, remediateBatch, i+1)
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	// Remediate policies depending on compliance state and upgrade plan.
	if group.Spec.RemediationAction == "enforce" {
		for i, remediateBatch := range remediationPlan {
			// Get policies with label cluster-group-lcm/batch=i
			// Set remediationAction to enforce on list
			// Get policies with label cluster-group-lcm/batch=i
			// Set batchCompliant to false if any of the policies is non-compliant
			r.Log.Info("Remediating sites", "remediateBatch", remediateBatch)
			r.Log.Info("Batch", "i", i+1)
			batchCompliant := true
			var labelsForBatch = map[string]string{"cluster-group-lcm/batch": strconv.Itoa(i + 1)}
			listOpts := []client.ListOption{
				client.InNamespace(group.Namespace),
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
				r.Log.Info("Upgrade batch not fully compliant yet")
				break
			} else {
				r.Log.Info("Upgrade batch fully compliant")
			}
		}
	}

	err = r.updateStatus(ctx, group)
	if err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *GroupReconciler) ensureBatchPlacementRules(ctx context.Context, group *ranv1alpha1.Group, batch []string, batchIndex int) error {
	for _, site := range batch {
		pr, err := r.newSitePlacementRule(ctx, group, batchIndex, site)
		if err != nil {
			return err
		}

		if err := controllerutil.SetControllerReference(group, pr, r.Scheme); err != nil {
			return err
		}

		foundPlacementRule := &unstructured.Unstructured{}
		foundPlacementRule.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "apps.open-cluster-management.io",
			Kind:    "PlacementRule",
			Version: "v1",
		})
		err = r.Client.Get(ctx, client.ObjectKey{
			Name:      pr.GetName(),
			Namespace: group.Namespace,
		}, foundPlacementRule)

		if err != nil && errors.IsNotFound((err)) {
			err = r.Client.Create(ctx, pr)
			if err != nil {
				return err
			}
			r.Log.Info("Created API PlacementRule object", "placementRule", pr)
		} else if err != nil {
			return err
		}
	}

	pr, err := r.newBatchPlacementRule(ctx, group, batch, batchIndex)
	if err != nil {
		return err
	}

	if err := controllerutil.SetControllerReference(group, pr, r.Scheme); err != nil {
		return err
	}

	foundPlacementRule := &unstructured.Unstructured{}
	foundPlacementRule.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "apps.open-cluster-management.io",
		Kind:    "PlacementRule",
		Version: "v1",
	})
	err = r.Client.Get(ctx, client.ObjectKey{
		Name:      pr.GetName(),
		Namespace: group.Namespace,
	}, foundPlacementRule)

	if err != nil && errors.IsNotFound((err)) {
		err = r.Client.Create(ctx, pr)
		if err != nil {
			return err
		}
		r.Log.Info("Created API PlacementRule object", "placementRule", pr)
	} else if err != nil {
		return err
	}

	return nil
}

func (r *GroupReconciler) newSitePlacementRule(ctx context.Context, group *ranv1alpha1.Group, batchIndex int, site string) (*unstructured.Unstructured, error) {
	u := &unstructured.Unstructured{}
	u.Object = map[string]interface{}{
		"metadata": map[string]interface{}{
			"name":      site + "-" + "placement-rule",
			"namespace": group.Namespace,
			"labels": map[string]interface{}{
				"app":                     "cluster-group-lcm",
				"cluster-group-lcm/group": group.Name,
				"cluster-group-lcm/batch": strconv.Itoa(batchIndex),
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
	s := &ranv1alpha1.Site{}
	nn := types.NamespacedName{Namespace: group.Namespace, Name: site}
	err := r.Get(ctx, nn, s)
	if err != nil {
		return nil, err
	}
	clusters = append(clusters, map[string]interface{}{"name": s.Spec.Cluster})
	specObject := u.Object["spec"].(map[string]interface{})
	specObject["clusters"] = clusters

	u.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "apps.open-cluster-management.io",
		Kind:    "PlacementRule",
		Version: "v1",
	})

	return u, nil
}

func (r *GroupReconciler) newBatchPlacementRule(ctx context.Context, group *ranv1alpha1.Group, batch []string, batchIndex int) (*unstructured.Unstructured, error) {
	u := &unstructured.Unstructured{}
	u.Object = map[string]interface{}{
		"metadata": map[string]interface{}{
			"name":      group.Name + "-" + "batch" + "-" + strconv.Itoa(batchIndex) + "-" + "placement-rule",
			"namespace": group.Namespace,
			"labels": map[string]interface{}{
				"app":                     "cluster-group-lcm",
				"cluster-group-lcm/group": group.Name,
				"cluster-group-lcm/batch": strconv.Itoa(batchIndex),
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
	for _, site := range batch {
		s := &ranv1alpha1.Site{}
		nn := types.NamespacedName{Namespace: group.Namespace, Name: site}
		err := r.Get(ctx, nn, s)
		if err != nil {
			return nil, err
		}
		clusters = append(clusters, map[string]interface{}{"name": s.Spec.Cluster})
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

func (r *GroupReconciler) ensureBatchPolicies(ctx context.Context, group *ranv1alpha1.Group, batch []string, batchIndex int) error {
	common := &ranv1alpha1.Common{}
	nn := types.NamespacedName{Namespace: group.Namespace, Name: "common"}
	err := r.Get(ctx, nn, common)
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	if common != nil {
		for _, commonPolicyTemplate := range common.Spec.CommonPolicyTemplates {
			policy := r.newCommonBatchPolicy(ctx, group, batchIndex, common.Name, commonPolicyTemplate.ObjectDefinition)

			if err := controllerutil.SetControllerReference(group, policy, r.Scheme); err != nil {
				return err
			}

			foundPolicy := &unstructured.Unstructured{}
			foundPolicy.SetGroupVersionKind(schema.GroupVersionKind{
				Group:   "policy.open-cluster-management.io",
				Kind:    "Policy",
				Version: "v1",
			})
			err := r.Client.Get(ctx, client.ObjectKey{
				Name:      policy.GetName(),
				Namespace: group.Namespace,
			}, foundPolicy)

			if err != nil && errors.IsNotFound((err)) {
				err = r.Client.Create(ctx, policy)
				if err != nil {
					return err
				}
				r.Log.Info("Created API Policy object", "policy", policy)
			} else if err != nil {
				return err
			}
		}
	}

	for _, site := range batch {
		s := &ranv1alpha1.Site{}
		nn := types.NamespacedName{Namespace: group.Namespace, Name: site}
		err := r.Get(ctx, nn, s)
		if err != nil {
			return err
		}

		for _, sitePolicyTemplate := range s.Spec.SitePolicyTemplates {
			policy := r.newSitePolicy(ctx, group, batchIndex, s.Name, sitePolicyTemplate.ObjectDefinition)

			if err := controllerutil.SetControllerReference(group, policy, r.Scheme); err != nil {
				return err
			}

			foundPolicy := &unstructured.Unstructured{}
			foundPolicy.SetGroupVersionKind(schema.GroupVersionKind{
				Group:   "policy.open-cluster-management.io",
				Kind:    "Policy",
				Version: "v1",
			})
			err := r.Client.Get(ctx, client.ObjectKey{
				Name:      policy.GetName(),
				Namespace: group.Namespace,
			}, foundPolicy)

			if err != nil && errors.IsNotFound((err)) {
				err = r.Client.Create(ctx, policy)
				if err != nil {
					return err
				}
				r.Log.Info("Created API Policy object", "policy", policy)
			} else if err != nil {
				return err
			}
		}
	}

	for _, groupPolicyTemplate := range group.Spec.GroupPolicyTemplates {
		policy := r.newGroupBatchPolicy(ctx, group, batchIndex, groupPolicyTemplate.ObjectDefinition)

		if err := controllerutil.SetControllerReference(group, policy, r.Scheme); err != nil {
			return err
		}

		foundPolicy := &unstructured.Unstructured{}
		foundPolicy.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "policy.open-cluster-management.io",
			Kind:    "Policy",
			Version: "v1",
		})
		err := r.Client.Get(ctx, client.ObjectKey{
			Name:      policy.GetName(),
			Namespace: group.Namespace,
		}, foundPolicy)

		if err != nil && errors.IsNotFound((err)) {
			err = r.Client.Create(ctx, policy)
			if err != nil {
				return err
			}
			r.Log.Info("Created API Policy object", "policy", policy)
		} else if err != nil {
			return err
		}

	}

	return nil
}

func (r *GroupReconciler) newCommonBatchPolicy(ctx context.Context, group *ranv1alpha1.Group, batchIndex int, common string, objectDefinition runtime.RawExtension) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	err := json.Unmarshal(objectDefinition.Raw, u)
	if err != nil {
		return nil
	}

	u.SetName(common + "-" + group.Name + "-" + "batch" + "-" + strconv.Itoa(batchIndex) + "-" + u.GetName() + "-" + "policy")
	u.SetNamespace(group.GetNamespace())
	labels := u.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	labels["app"] = "cluster-group-lcm"
	labels["cluster-group-lcm/group"] = group.Name
	labels["cluster-group-lcm/batch"] = strconv.Itoa(batchIndex)

	u.SetLabels(labels)

	specObject := u.Object["spec"].(map[string]interface{})
	specObject["remediationAction"] = "inform"

	return u
}

func (r *GroupReconciler) newSitePolicy(ctx context.Context, group *ranv1alpha1.Group, batchIndex int, site string, objectDefinition runtime.RawExtension) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	err := json.Unmarshal(objectDefinition.Raw, u)
	if err != nil {
		return nil
	}

	u.SetName(site + "-" + u.GetName() + "-" + "policy")
	u.SetNamespace(group.GetNamespace())
	labels := u.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	labels["app"] = "cluster-group-lcm"
	labels["cluster-group-lcm/group"] = group.Name
	labels["cluster-group-lcm/batch"] = strconv.Itoa(batchIndex)

	u.SetLabels(labels)

	specObject := u.Object["spec"].(map[string]interface{})
	specObject["remediationAction"] = "inform"

	return u
}

func (r *GroupReconciler) newGroupBatchPolicy(ctx context.Context, group *ranv1alpha1.Group, batchIndex int, objectDefinition runtime.RawExtension) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	err := json.Unmarshal(objectDefinition.Raw, u)
	if err != nil {
		return nil
	}

	u.SetName(group.Name + "-" + "batch" + "-" + strconv.Itoa(batchIndex) + "-" + u.GetName() + "-" + "policy")
	u.SetNamespace(group.GetNamespace())
	labels := u.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	labels["app"] = "cluster-group-lcm"
	labels["cluster-group-lcm/group"] = group.Name
	labels["cluster-group-lcm/batch"] = strconv.Itoa(batchIndex)
	u.SetLabels(labels)

	specObject := u.Object["spec"].(map[string]interface{})
	specObject["remediationAction"] = "inform"

	return u
}

func (r *GroupReconciler) ensureBatchPlacementBindings(ctx context.Context, group *ranv1alpha1.Group, batch []string, batchIndex int) error {
	// ensure sites placement bindings
	for _, site := range batch {
		pb, err := r.newSitePlacementBinding(ctx, group, batchIndex, site)
		if err != nil {
			return err
		}

		if err := controllerutil.SetControllerReference(group, pb, r.Scheme); err != nil {
			return err
		}

		foundPlacementBinding := &unstructured.Unstructured{}
		foundPlacementBinding.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "policy.open-cluster-management.io",
			Kind:    "PlacementBinding",
			Version: "v1",
		})
		err = r.Client.Get(ctx, client.ObjectKey{
			Name:      pb.GetName(),
			Namespace: group.Namespace,
		}, foundPlacementBinding)

		if err != nil && errors.IsNotFound((err)) {
			err = r.Client.Create(ctx, pb)
			if err != nil {
				return err
			}
			r.Log.Info("Created API PlacementBindingObject object", "placementBinding", pb)
		} else if err != nil {
			return err
		}
	}
	// ensure batch placement bindings
	pb, err := r.newBatchPlacementBinding(ctx, group, batchIndex)
	if err != nil {
		return err
	}

	if err := controllerutil.SetControllerReference(group, pb, r.Scheme); err != nil {
		return err
	}

	foundPlacementBinding := &unstructured.Unstructured{}
	foundPlacementBinding.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "policy.open-cluster-management.io",
		Kind:    "PlacementBinding",
		Version: "v1",
	})
	err = r.Client.Get(ctx, client.ObjectKey{
		Name:      pb.GetName(),
		Namespace: group.Namespace,
	}, foundPlacementBinding)

	if err != nil && errors.IsNotFound((err)) {
		err = r.Client.Create(ctx, pb)
		if err != nil {
			return err
		}
		r.Log.Info("Created API PlacementBindingObject object", "placementBinding", pb)
	} else if err != nil {
		return err
	}

	return nil
}

func (r *GroupReconciler) newSitePlacementBinding(ctx context.Context, group *ranv1alpha1.Group, batchIndex int, site string) (*unstructured.Unstructured, error) {
	var subjects []map[string]interface{}
	s := &ranv1alpha1.Site{}
	nn := types.NamespacedName{Namespace: group.Namespace, Name: site}
	err := r.Get(ctx, nn, s)
	if err != nil {
		return nil, err
	}

	for _, sitePolicyTemplate := range s.Spec.SitePolicyTemplates {
		u := &unstructured.Unstructured{}
		err := json.Unmarshal(sitePolicyTemplate.ObjectDefinition.Raw, u)
		if err != nil {
			return nil, err
		}

		subject := make(map[string]interface{})
		subject["name"] = site + "-" + u.GetName() + "-" + "policy"
		subject["kind"] = "Policy"
		subject["apiGroup"] = "policy.open-cluster-management.io"

		subjects = append(subjects, subject)
	}

	u := &unstructured.Unstructured{}
	u.Object = map[string]interface{}{
		"metadata": map[string]interface{}{
			"name":      site + "-" + "placement-binding",
			"namespace": group.Namespace,
			"labels": map[string]interface{}{
				"app":                     "cluster-group-lcm",
				"cluster-group-lcm/group": group.Name,
			},
		},
		"placementRef": map[string]interface{}{
			"name":     site + "-" + "placement-rule",
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

func (r *GroupReconciler) newBatchPlacementBinding(ctx context.Context, group *ranv1alpha1.Group, batchIndex int) (*unstructured.Unstructured, error) {
	var subjects []map[string]interface{}

	common := &ranv1alpha1.Common{}
	nn := types.NamespacedName{Namespace: group.Namespace, Name: "common"}
	err := r.Get(ctx, nn, common)
	if err != nil && !errors.IsNotFound(err) {
		return nil, err
	}

	for _, commonPolicyTemplate := range common.Spec.CommonPolicyTemplates {
		u := &unstructured.Unstructured{}
		err := json.Unmarshal(commonPolicyTemplate.ObjectDefinition.Raw, u)
		if err != nil {
			return nil, err
		}

		subject := make(map[string]interface{})
		subject["name"] = common.Name + "-" + group.Name + "-" + "batch" + "-" + strconv.Itoa(batchIndex) + "-" + u.GetName() + "-" + "policy"
		subject["kind"] = "Policy"
		subject["apiGroup"] = "policy.open-cluster-management.io"

		subjects = append(subjects, subject)
	}

	for _, groupPolicyTemplate := range group.Spec.GroupPolicyTemplates {
		u := &unstructured.Unstructured{}
		err := json.Unmarshal(groupPolicyTemplate.ObjectDefinition.Raw, u)
		if err != nil {
			return nil, err
		}

		subject := make(map[string]interface{})
		subject["name"] = group.Name + "-" + "batch" + "-" + strconv.Itoa(batchIndex) + "-" + u.GetName() + "-" + "policy"
		subject["kind"] = "Policy"
		subject["apiGroup"] = "policy.open-cluster-management.io"

		subjects = append(subjects, subject)
	}

	u := &unstructured.Unstructured{}
	u.Object = map[string]interface{}{
		"metadata": map[string]interface{}{
			"name":      group.Name + "-" + "batch" + "-" + strconv.Itoa(batchIndex) + "-" + "placement-binding",
			"namespace": group.Namespace,
			"labels": map[string]interface{}{
				"app":                     "cluster-group-lcm",
				"cluster-group-lcm/group": group.Name,
			},
		},
		"placementRef": map[string]interface{}{
			"name":     group.Name + "-" + "batch" + "-" + strconv.Itoa(batchIndex) + "-" + "placement-rule",
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

func (r *GroupReconciler) updateStatus(ctx context.Context, group *ranv1alpha1.Group) error {
	var labelsForGroup = map[string]string{"app": "cluster-group-lcm", "cluster-group-lcm/group": group.GetName()}
	listOpts := []client.ListOption{
		client.InNamespace(group.Namespace),
		client.MatchingLabels(labelsForGroup),
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
	placementRulesNames := getUnstructuredItemsNames(placementRulesList.Items)
	group.Status.PlacementRules = placementRulesNames

	placementBindingsList := &unstructured.UnstructuredList{}
	placementBindingsList.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "policy.open-cluster-management.io",
		Kind:    "PlacementBindingList",
		Version: "v1",
	})
	if err := r.List(ctx, placementBindingsList, listOpts...); err != nil {
		return err
	}
	placementBindingsNames := getUnstructuredItemsNames(placementBindingsList.Items)
	group.Status.PlacementBindings = placementBindingsNames

	policiesList := &unstructured.UnstructuredList{}
	policiesList.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "policy.open-cluster-management.io",
		Kind:    "PolicyList",
		Version: "v1",
	})
	if err := r.List(ctx, policiesList, listOpts...); err != nil {
		return err
	}
	var policiesStatus []ranv1alpha1.PolicyStatus
	for _, policy := range policiesList.Items {
		policyStatus := &ranv1alpha1.PolicyStatus{}
		policyStatus.Name = policy.GetName()
		if policy.Object["status"] == nil {
			policyStatus.ComplianceState = "NonCompliant"
		} else {
			statusObject := policy.Object["status"].(map[string]interface{})
			if statusObject["compliant"] != nil {
				policyStatus.ComplianceState = statusObject["compliant"].(string)
			}
		}
		policiesStatus = append(policiesStatus, *policyStatus)
	}
	group.Status.Policies = policiesStatus

	err := r.Status().Update(ctx, group)
	if err != nil {
		return err
	}
	r.Log.Info("Updated Group status")

	return nil
}

func getUnstructuredItemsNames(items []unstructured.Unstructured) []string {
	var unstructuredItemsNames []string
	for _, item := range items {
		unstructuredItemsNames = append(unstructuredItemsNames, item.GetName())
	}

	return unstructuredItemsNames
}

// SetupWithManager sets up the controller with the Manager.
func (r *GroupReconciler) SetupWithManager(mgr ctrl.Manager) error {
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
		For(&ranv1alpha1.Group{}).
		Owns(placementRuleUnstructured).
		Owns(placementBindingUnstructured).
		Owns(policyUnstructured).
		Complete(r)
}
