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

// SiteReconciler reconciles a Site object
type SiteReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=ran.openshift.io,resources=sites,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=ran.openshift.io,resources=sites/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=ran.openshift.io,resources=sites/finalizers,verbs=update
//+kubebuilder:rbac:groups=apps.open-cluster-management.io,resources=placementrules,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=policy.open-cluster-management.io,resources=placementbindings,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=policy.open-cluster-management.io,resources=policies,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=cluster.open-cluster-management.io,resources=managedclusters,verbs=get;list;watch;create;update;patch;delete

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

	err = r.ensurePolicies(ctx, site)
	if err != nil {
		return ctrl.Result{}, err
	}
	err = r.ensurePlacementBinding(ctx, site)
	if err != nil {
		return ctrl.Result{}, err
	}

	err = r.updateStatus(ctx, site)
	if err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *SiteReconciler) ensurePlacementRule(ctx context.Context, site *ranv1alpha1.Site) error {
	pr := r.newPlacementRule(ctx, site)

	if err := controllerutil.SetControllerReference(site, pr, r.Scheme); err != nil {
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
		Namespace: site.Namespace,
	}, foundPlacementRule)
	if err != nil && errors.IsNotFound((err)) {
		err = r.Client.Create(ctx, pr)
		if err != nil {
			return err
		}
		r.Log.Info("Created API PlacementRule object", "placementRule", pr)
		site.Status.PlacementRules = append(site.Status.PlacementRules, pr.GetName())
		r.Status().Update(ctx, site)
		if err != nil {
			return err
		}
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
				"app":                          "cluster-group-lcm",
				"cluster-group-lcm/site-owner": site.Name,
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

func (r *SiteReconciler) ensurePlacementBinding(ctx context.Context, site *ranv1alpha1.Site) error {
	pb := r.newPlacementBinding(ctx, site)

	if err := controllerutil.SetControllerReference(site, pb, r.Scheme); err != nil {
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
		Namespace: site.Namespace,
	}, foundPlacementBinding)
	if err != nil && errors.IsNotFound((err)) {
		err = r.Client.Create(ctx, pb)
		if err != nil {
			return err
		}
		r.Log.Info("Created API PlacementBinding object", "placementBinding", pb)
		site.Status.PlacementBindings = append(site.Status.PlacementBindings, pb.GetName())
		r.Status().Update(ctx, site)
		if err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	return nil
}

func (r *SiteReconciler) newPlacementBinding(ctx context.Context, site *ranv1alpha1.Site) *unstructured.Unstructured {
	var subjects []map[string]interface{}
	for _, sitePolicyTemplate := range site.Spec.SitePolicyTemplates {
		u := &unstructured.Unstructured{}
		err := json.Unmarshal(sitePolicyTemplate.ObjectDefinition.Raw, u)
		if err != nil {
			return nil
		}

		subject := make(map[string]interface{})
		subject["name"] = site.Name + "-" + u.GetName() + "-" + "policy"
		subject["kind"] = "Policy"
		subject["apiGroup"] = "policy.open-cluster-management.io"

		subjects = append(subjects, subject)
	}

	u := &unstructured.Unstructured{}
	u.Object = map[string]interface{}{
		"metadata": map[string]interface{}{
			"name":      site.Name + "-" + "placement-binding",
			"namespace": site.Namespace,
			"labels": map[string]interface{}{
				"app":                          "cluster-group-lcm",
				"cluster-group-lcm/site-owner": site.Name,
			},
		},
		"placementRef": map[string]interface{}{
			"name":     site.Name + "-" + "placement-rule",
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

func (r *SiteReconciler) ensurePolicies(ctx context.Context, site *ranv1alpha1.Site) error {
	for _, sitePolicyTemplate := range site.Spec.SitePolicyTemplates {
		policy := r.newPolicy(ctx, site, sitePolicyTemplate.ObjectDefinition)

		if err := controllerutil.SetControllerReference(site, policy, r.Scheme); err != nil {
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
			Namespace: site.Namespace,
		}, foundPolicy)
		if err != nil && errors.IsNotFound((err)) {
			err = r.Client.Create(ctx, policy)
			if err != nil {
				return err
			}
			r.Log.Info("Created API Policy object for", "policy", policy)
		} else if err != nil {
			return err
		}
	}

	return nil
}

func (r *SiteReconciler) newPolicy(ctx context.Context, site *ranv1alpha1.Site, objectDefinition runtime.RawExtension) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	err := json.Unmarshal(objectDefinition.Raw, u)
	if err != nil {
		return nil
	}
	// TODO: Validate the unmarshaled object
	u.SetName(site.Name + "-" + u.GetName() + "-" + "policy")
	u.SetNamespace(site.GetNamespace())
	labels := u.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	labels["app"] = "cluster-group-lcm"
	labels["cluster-group-lcm/site-owner"] = site.Name
	u.SetLabels(labels)

	specObject := u.Object["spec"].(map[string]interface{})
	specObject["remediationAction"] = "inform"

	return u
}

func (r *SiteReconciler) updateStatus(ctx context.Context, site *ranv1alpha1.Site) error {
	var labelsForSite = map[string]string{"app": "cluster-group-lcm", "cluster-group-lcm/site-owner": site.GetName()}
	listOpts := []client.ListOption{
		client.InNamespace(site.Namespace),
		client.MatchingLabels(labelsForSite),
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
	site.Status.PlacementRules = placementRulesNames

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
	site.Status.PlacementBindings = placementBindingsNames

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
		statusObject := policy.Object["status"].(map[string]interface{})
		if statusObject["compliant"] != nil {
			policyStatus.ComplianceState = statusObject["compliant"].(string)
		} else {
			policyStatus.ComplianceState = "NonCompliant"
		}
		policiesStatus = append(policiesStatus, *policyStatus)
	}
	site.Status.Policies = policiesStatus

	err := r.Status().Update(ctx, site)
	if err != nil {
		return err
	}
	r.Log.Info("Updated Site status")

	return nil
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
