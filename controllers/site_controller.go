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

	ranv1alpha1 "github.com/redhat-ztp/cluster-group-lcm/api/v1alpha1"
)

// SiteReconciler reconciles a Site object
type SiteReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=ran.openshift.io,resources=sites,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=ran.openshift.io,resources=sites/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=ran.openshift.io,resources=sites/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Site object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.8.3/pkg/reconcile
func (r *SiteReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = r.Log.WithValues("site", req.NamespacedName)

	site := &ranv1alpha1.Site{}
	err := r.Get(ctx, req.NamespacedName, site)
	if err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		r.Log.Error(err, "Failed to get Site")
		return ctrl.Result{}, err
	}

	err = r.ensurePlacementRule(ctx, site)
	if err != nil {
		return ctrl.Result{}, err
	}
	for _, sitePolicyTemplate := range site.Spec.SitePolicyTemplates {
		err = r.ensurePolicy(ctx, site, sitePolicyTemplate)
		if err != nil {
			return ctrl.Result{}, err
		}
		err = r.ensurePlacementBinding(ctx, site, sitePolicyTemplate)
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func (r *SiteReconciler) ensurePlacementRule(ctx context.Context, site *ranv1alpha1.Site) error {
	pr := r.newPlacementRule(ctx, site)

	/*	if err := controllerutil.SetControllerReference(group, pr, r.Scheme); err != nil {
		return err
	}*/

	foundPlacementRule := &unstructured.Unstructured{}
	foundPlacementRule.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "apps.open-cluster-management.io",
		Kind:    "PlacementRule",
		Version: "v1",
	})
	err := r.Client.Get(ctx, client.ObjectKey{
		Name:      pr.GetName(),
		Namespace: site.Namespace,
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

func (r *SiteReconciler) newPlacementRule(ctx context.Context, site *ranv1alpha1.Site) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.Object = map[string]interface{}{
		"metadata": map[string]interface{}{
			"name":      site.Name + "-" + "placement-rule",
			"namespace": site.Namespace,
			"labels": map[string]interface{}{
				"app": "cluster-group-lcm",
			},
		},
		"spec": map[string]interface{}{
			"clusterConditions": []map[string]interface{}{
				{
					"type":   "ManagedClusterConditionAvailable",
					"status": "True",
				},
			},
			"clusters": []map[string]interface{}{
				{
					"name": site.Spec.Cluster,
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

func (r *SiteReconciler) ensurePlacementBinding(ctx context.Context, site *ranv1alpha1.Site, sitePolicyTemplate ranv1alpha1.SitePolicyTemplate) error {
	pb := r.newPlacementBinding(ctx, site, sitePolicyTemplate)
	/*
		if err := controllerutil.SetControllerReference(group, pb, r.Scheme); err != nil {
			return err
		}
	*/
	foundPlacementBinding := &unstructured.Unstructured{}
	foundPlacementBinding.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "policy.open-cluster-management.io",
		Kind:    "PlacementBinding",
		Version: "v1",
	})
	err := r.Client.Get(ctx, client.ObjectKey{
		Name:      pb.GetName(),
		Namespace: site.Namespace,
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

func (r *SiteReconciler) newPlacementBinding(ctx context.Context, site *ranv1alpha1.Site, sitePolicyTemplate ranv1alpha1.SitePolicyTemplate) *unstructured.Unstructured {
	policyUnstructured := &unstructured.Unstructured{}
	err := json.Unmarshal(sitePolicyTemplate.ObjectDefinition.Raw, policyUnstructured)
	if err != nil {
		return nil
	}

	u := &unstructured.Unstructured{}
	u.Object = map[string]interface{}{
		"metadata": map[string]interface{}{
			"name":      site.Name + "-" + "placement-binding",
			"namespace": site.Namespace,
			"labels": map[string]interface{}{
				"app": "cluster-group-lcm",
			},
		},
		"placementRef": map[string]interface{}{
			"name":     site.Name + "-" + "placement-rule",
			"kind":     "PlacementRule",
			"apiGroup": "apps.open-cluster-management.io",
		},
		"subjects": []map[string]interface{}{
			{
				"name":     site.Name + "-" + policyUnstructured.GetName() + "-" + "policy",
				"kind":     "Policy",
				"apiGroup": "policy.open-cluster-management.io",
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

func (r *SiteReconciler) ensurePolicy(ctx context.Context, site *ranv1alpha1.Site, sitePolicyTemplate ranv1alpha1.SitePolicyTemplate) error {
	pol := r.newPolicy(ctx, site, sitePolicyTemplate)

	/*	if err := controllerutil.SetControllerReference(group, pol, r.Scheme); err != nil {
		return err
	}*/

	foundPolicy := &unstructured.Unstructured{}
	foundPolicy.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "policy.open-cluster-management.io",
		Kind:    "Policy",
		Version: "v1",
	})
	err := r.Client.Get(ctx, client.ObjectKey{
		Name:      pol.GetName(),
		Namespace: site.Namespace,
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

func (r *SiteReconciler) newPolicy(ctx context.Context, site *ranv1alpha1.Site, sitePolicyTemplate ranv1alpha1.SitePolicyTemplate) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	err := json.Unmarshal(sitePolicyTemplate.ObjectDefinition.Raw, u)
	if err != nil {
		return nil
	}

	u.SetName(site.Name + "-" + u.GetName() + "-" + "policy")
	u.SetNamespace(site.GetNamespace())

	specObject := u.Object["spec"].(map[string]interface{})
	specObject["remediationAction"] = "inform"

	return u
}

// SetupWithManager sets up the controller with the Manager.
func (r *SiteReconciler) SetupWithManager(mgr ctrl.Manager) error {
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
		For(&ranv1alpha1.Site{}).
		Owns(placementRuleUnstructured).
		Owns(placementBindingUnstructured).
		Owns(policyUnstructured).
		Complete(r)
}
