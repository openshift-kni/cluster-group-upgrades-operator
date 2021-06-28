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

// CommonReconciler reconciles a Common object
type CommonReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=ran.openshift.io,resources=commons,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=ran.openshift.io,resources=commons/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=ran.openshift.io,resources=commons/finalizers,verbs=update
//+kubebuilder:rbac:groups=apps.open-cluster-management.io,resources=placementrules,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=policy.open-cluster-management.io,resources=placementbindings,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=policy.open-cluster-management.io,resources=policies,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=cluster.open-cluster-management.io,resources=managedclusters,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Common object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.8.3/pkg/reconcile
func (r *CommonReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = r.Log.WithValues("common", req.NamespacedName)

	common := &ranv1alpha1.Common{}
	err := r.Get(ctx, req.NamespacedName, common)
	if err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		r.Log.Error(err, "Failed to get Site")
		return ctrl.Result{}, err
	}

	// TODO: Remove this and put a predicate to ignore CRs that don't have 'common' as name
	if common.Name != "common" {
		r.Log.Info("Ignoring Common CR, only one Common CR with name set to 'common' is allowed")
		return ctrl.Result{}, err
	}

	sitesList := &ranv1alpha1.SiteList{}
	if err := r.List(ctx, sitesList); err != nil {
		return ctrl.Result{}, err
	}

	for _, site := range sitesList.Items {
		err = r.ensurePlacementRule(ctx, common, &site)
		if err != nil {
			return ctrl.Result{}, err
		}

		err = r.ensurePolicies(ctx, common, &site)
		if err != nil {
			return ctrl.Result{}, err
		}

		err = r.ensurePlacementBinding(ctx, common, &site)
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	err = r.updateStatus(ctx, common)
	if err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *CommonReconciler) ensurePlacementRule(ctx context.Context, common *ranv1alpha1.Common, site *ranv1alpha1.Site) error {
	pr := r.newPlacementRule(ctx, common, site)

	if err := controllerutil.SetControllerReference(common, pr, r.Scheme); err != nil {
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
		Namespace: common.Namespace,
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

func (r *CommonReconciler) newPlacementRule(ctx context.Context, common *ranv1alpha1.Common, site *ranv1alpha1.Site) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.Object = map[string]interface{}{
		"metadata": map[string]interface{}{
			"name":      common.Name + "-" + site.Name + "-" + "placement-rule",
			"namespace": common.Namespace,
			"labels": map[string]interface{}{
				"app":                            "cluster-group-lcm",
				"cluster-group-lcm/common-owner": common.Name,
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

func (r *CommonReconciler) ensurePlacementBinding(ctx context.Context, common *ranv1alpha1.Common, site *ranv1alpha1.Site) error {
	pb := r.newPlacementBinding(ctx, common, site)

	if err := controllerutil.SetControllerReference(common, pb, r.Scheme); err != nil {
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
		Namespace: common.Namespace,
	}, foundPlacementBinding)
	if err != nil && errors.IsNotFound((err)) {
		err = r.Client.Create(ctx, pb)
		if err != nil {
			return err
		}
		r.Log.Info("Created API PlacementBinding object", "placementBinding", pb)
	} else if err != nil {
		return err
	}

	return nil
}

func (r *CommonReconciler) newPlacementBinding(ctx context.Context, common *ranv1alpha1.Common, site *ranv1alpha1.Site) *unstructured.Unstructured {
	var subjects []map[string]interface{}
	for _, commonPolicyTemplate := range common.Spec.CommonPolicyTemplates {
		u := &unstructured.Unstructured{}
		err := json.Unmarshal(commonPolicyTemplate.ObjectDefinition.Raw, u)
		if err != nil {
			r.Log.Info("Unable to unmarshall common policy template")
			return nil
		}

		subject := make(map[string]interface{})
		subject["name"] = common.Name + "-" + site.Name + "-" + u.GetName() + "-" + "policy"
		subject["kind"] = "Policy"
		subject["apiGroup"] = "policy.open-cluster-management.io"

		subjects = append(subjects, subject)
	}

	u := &unstructured.Unstructured{}
	u.Object = map[string]interface{}{
		"metadata": map[string]interface{}{
			"name":      common.Name + "-" + site.Name + "-" + "placement-binding",
			"namespace": common.Namespace,
			"labels": map[string]interface{}{
				"app":                            "cluster-group-lcm",
				"cluster-group-lcm/common-owner": common.Name,
			},
		},
		"placementRef": map[string]interface{}{
			"name":     common.Name + "-" + site.Name + "-" + "placement-rule",
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

func (r *CommonReconciler) ensurePolicies(ctx context.Context, common *ranv1alpha1.Common, site *ranv1alpha1.Site) error {
	for _, commonPolicyTemplate := range common.Spec.CommonPolicyTemplates {
		policy := r.newPolicy(ctx, common, site, commonPolicyTemplate.ObjectDefinition)

		if err := controllerutil.SetControllerReference(common, policy, r.Scheme); err != nil {
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
			Namespace: common.Namespace,
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

func (r *CommonReconciler) newPolicy(ctx context.Context, common *ranv1alpha1.Common, site *ranv1alpha1.Site, objectDefinition runtime.RawExtension) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	err := json.Unmarshal(objectDefinition.Raw, u)
	if err != nil {
		return nil
	}
	// TODO: Validate the unmarshaled object
	u.SetName(common.Name + "-" + site.Name + "-" + u.GetName() + "-" + "policy")
	u.SetNamespace(common.GetNamespace())
	labels := u.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	labels["app"] = "cluster-group-lcm"
	labels["cluster-group-lcm/common-owner"] = common.Name
	u.SetLabels(labels)

	specObject := u.Object["spec"].(map[string]interface{})
	specObject["remediationAction"] = "inform"

	return u
}

func (r *CommonReconciler) updateStatus(ctx context.Context, common *ranv1alpha1.Common) error {
	var labelsForCommon = map[string]string{"app": "cluster-group-lcm", "cluster-group-lcm/common-owner": common.GetName()}
	listOpts := []client.ListOption{
		client.InNamespace(common.Namespace),
		client.MatchingLabels(labelsForCommon),
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
	if placementRulesNames == nil {
		placementRulesNames = []string{}
	}
	common.Status.PlacementRules = placementRulesNames

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
	if placementBindingsNames == nil {
		placementBindingsNames = []string{}
	}
	common.Status.PlacementBindings = placementBindingsNames

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
	if policiesStatus == nil {
		policiesStatus = []ranv1alpha1.PolicyStatus{}
	}
	common.Status.Policies = policiesStatus

	err := r.Status().Update(ctx, common)
	if err != nil {
		return err
	}
	r.Log.Info("Updated Common status")

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *CommonReconciler) SetupWithManager(mgr ctrl.Manager) error {
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
		For(&ranv1alpha1.Common{}).
		Owns(placementRuleUnstructured).
		Owns(placementBindingUnstructured).
		Owns(policyUnstructured).
		Complete(r)
}
