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

	// your logic here
	group := &ranv1alpha1.Group{}
	err := r.Get(ctx, req.NamespacedName, group)
	if err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		r.Log.Error(err, "Failed to get Group")
		return ctrl.Result{}, err
	}

	if group.Spec.UpgradeStrategy.Type == "Serial" {
		for _, v := range group.Spec.Sites {
			err = r.ensurePlacementRule(ctx, group, v)
			if err != nil {
				return ctrl.Result{}, err
			}
			err = r.ensurePlacementBinding(ctx, group, v)
		}

	}

	return ctrl.Result{}, nil
}

func (r *GroupReconciler) ensurePlacementRule(ctx context.Context, group *ranv1alpha1.Group, cluster string) error {
	r.Log.Info("Creating PlacementRule object", "cluster", cluster)
	pr := r.newPlacementRule(ctx, group, cluster)
	r.Log.Info("Created PlacementRule object", "placementRule", pr)

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
		Name:      group.Name + "-" + cluster + "-" + "placement-rule",
		Namespace: group.Namespace,
	}, foundPlacementRule)
	if err != nil && errors.IsNotFound((err)) {
		err = r.Client.Create(ctx, pr)
		if err != nil {
			return err
		}
		r.Log.Info("Created API PlacementRule object for", "placementRule", pr)
	} else if err != nil {
		return err
	}

	return nil
}

func (r *GroupReconciler) newPlacementRule(ctx context.Context, group *ranv1alpha1.Group, cluster string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.Object = map[string]interface{}{
		"metadata": map[string]interface{}{
			"name":      group.Name + "-" + cluster + "-" + "placement-rule",
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
					"name": cluster,
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

func (r *GroupReconciler) ensurePlacementBinding(ctx context.Context, group *ranv1alpha1.Group, cluster string) error {
	r.Log.Info("Creating PlacementBinding object", "cluster", cluster)
	pb := r.newPlacementBinding(ctx, group, cluster)
	r.Log.Info("Created PlacementBinding object", "placementBinding", pb)

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
		Name:      group.Name + "-" + cluster + "-" + "placement-binding",
		Namespace: group.Namespace,
	}, foundPlacementBinding)
	if err != nil && errors.IsNotFound((err)) {
		err = r.Client.Create(ctx, pb)
		if err != nil {
			return err
		}
		r.Log.Info("Created API PlacementBinding object for", "placementBinding", pb)
	} else if err != nil {
		return err
	}

	return nil
}

func (r *GroupReconciler) newPlacementBinding(ctx context.Context, group *ranv1alpha1.Group, cluster string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.Object = map[string]interface{}{
		"metadata": map[string]interface{}{
			"name":      group.Name + "-" + cluster + "-" + "placement-binding",
			"namespace": group.Namespace,
			"labels": map[string]interface{}{
				"app": "cluster-group-lcm",
			},
		},
		"spec": map[string]interface{}{
			"placementRef": map[string]interface{}{
				"name":     group.Name + "-" + cluster + "-" + "placement-rule",
				"kind":     "PlacementRule",
				"apiGroup": "apps.open-cluster-management.io",
			},
			"subjects": []map[string]interface{}{
				{
					"name":     "policy-test",
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

	return ctrl.NewControllerManagedBy(mgr).
		For(&ranv1alpha1.Group{}).
		Owns(placementRuleUnstructured).
		Owns(placementBindingUnstructured).
		Complete(r)
}
