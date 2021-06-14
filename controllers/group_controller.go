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

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

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

	// Reconcile resources
	for _, site := range group.Spec.Sites {
		err = r.ensurePlacementRule(ctx, group, site)
		if err != nil {
			return ctrl.Result{}, err
		}
		for _, groupPolicyTemplate := range group.Spec.GroupPolicyTemplates {
			err = r.ensurePolicy(ctx, group, site, groupPolicyTemplate)
			if err != nil {
				return ctrl.Result{}, err
			}
		}
		err = r.ensurePlacementBinding(ctx, group, site, "group1-site1-upgrade-cluster-policy")
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	// Create upgrade plan

	// Remediate policies depending on compliance state and upgrade plan

	return ctrl.Result{}, nil
}

func (r *GroupReconciler) ensurePlacementRule(ctx context.Context, group *ranv1alpha1.Group, site string) error {
	pr := r.newPlacementRule(ctx, group, site)

	if err := controllerutil.SetControllerReference(group, pr, r.Scheme); err != nil {
		return err
	}

	foundPlacementRule := &unstructured.Unstructured{}
	foundPlacementRule.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "apps.open-cluster-management.io",
		Kind:    "PlacementRule",
		Version: "v1",
	})
	err := r.Client.Get(ctx, client.ObjectKey{
		Name:      pr.GetName(),
		Namespace: group.Namespace,
	}, foundPlacementRule)
	if err != nil && errors.IsNotFound((err)) {
		err = r.Client.Create(ctx, pr)
		if err != nil {
			return err
		}
		r.Log.Info("Created API object", "placementRule", pr)
	} else if err != nil {
		return err
	}

	return nil
}

func (r *GroupReconciler) newPlacementRule(ctx context.Context, group *ranv1alpha1.Group, site string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.Object = map[string]interface{}{
		"metadata": map[string]interface{}{
			"name":      group.Name + "-" + site + "-" + "placement-rule",
			"namespace": group.Namespace,
			"labels": map[string]interface{}{
				"app": "cluster-group-lcm",
			},
		},
		"spec": map[string]interface{}{
			"clusterConditions": []map[string]interface{}{
				{
					"type": "OK",
				},
			},
			"clusters": []map[string]interface{}{
				{
					"name": site,
				},
			},
		},
	}
	u.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "apps.open-cluster-management.io",
		Kind:    "PlacementRule",
		Version: "v1",
	})

	return u
}

func (r *GroupReconciler) ensurePlacementBinding(ctx context.Context, group *ranv1alpha1.Group, site string, policy string) error {
	pb := r.newPlacementBinding(ctx, group, site, policy)

	if err := controllerutil.SetControllerReference(group, pb, r.Scheme); err != nil {
		return err
	}

	foundPlacementBinding := &unstructured.Unstructured{}
	foundPlacementBinding.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "policy.open-cluster-management.io",
		Kind:    "PlacementBinding",
		Version: "v1",
	})
	err := r.Client.Get(ctx, client.ObjectKey{
		Name:      pb.GetName(),
		Namespace: group.Namespace,
	}, foundPlacementBinding)
	if err != nil && errors.IsNotFound((err)) {
		err = r.Client.Create(ctx, pb)
		if err != nil {
			return err
		}
		r.Log.Info("Created API object", "placementBinding", pb)
	} else if err != nil {
		return err
	}

	return nil
}

func (r *GroupReconciler) newPlacementBinding(ctx context.Context, group *ranv1alpha1.Group, site string, policyName string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.Object = map[string]interface{}{
		"metadata": map[string]interface{}{
			"name":      group.Name + "-" + site + "-" + "placement-binding",
			"namespace": group.Namespace,
			"labels": map[string]interface{}{
				"app": "cluster-group-lcm",
			},
		},
		"spec": map[string]interface{}{
			"placementRef": map[string]interface{}{
				"name":     group.Name + "-" + site + "-" + "placement-rule",
				"kind":     "PlacementRule",
				"apiGroup": "apps.open-cluster-management.io",
			},
			"subjects": []map[string]interface{}{
				{
					"name":     policyName,
					"kind":     "Policy",
					"apiGroup": "policy.open-cluster-management.io",
				},
			},
		},
	}
	u.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "policy.open-cluster-management.io",
		Kind:    "PlacementBinding",
		Version: "v1",
	})

	return u
}

func (r *GroupReconciler) ensurePolicy(ctx context.Context, group *ranv1alpha1.Group, site string, groupPolicyTemplate ranv1alpha1.GroupPolicyTemplate) error {
	pol := r.newPolicy(ctx, group, site, groupPolicyTemplate)

	if err := controllerutil.SetControllerReference(group, pol, r.Scheme); err != nil {
		return err
	}

	foundPolicy := &unstructured.Unstructured{}
	foundPolicy.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "policy.open-cluster-management.io",
		Kind:    "Policy",
		Version: "v1",
	})
	err := r.Client.Get(ctx, client.ObjectKey{
		Name:      pol.GetName(),
		Namespace: group.Namespace,
	}, foundPolicy)
	if err != nil && errors.IsNotFound((err)) {
		err = r.Client.Create(ctx, pol)
		if err != nil {
			return err
		}
		r.Log.Info("Created API object for", "policy", pol)
	} else if err != nil {
		return err
	}

	return nil
}

func (r *GroupReconciler) newPolicy(ctx context.Context, group *ranv1alpha1.Group, site string, groupPolicyTemplate ranv1alpha1.GroupPolicyTemplate) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	err := json.Unmarshal(groupPolicyTemplate.ObjectDefinition.Raw, u)
	if err != nil {
		return nil
	}

	u.SetName(group.Name + "-" + site + "-" + u.GetName() + "-" + "policy")
	u.SetNamespace(group.GetNamespace())

	specObject := u.Object["spec"].(map[string]interface{})
	specObject["remediationAction"] = "inform"

	return u
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
