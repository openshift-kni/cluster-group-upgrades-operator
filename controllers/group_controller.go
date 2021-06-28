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

	// Create upgrade plan
	var upgradePlan [][]string
	isCanary := make(map[string]bool)
	if group.Spec.UpgradeStrategy.Canaries != nil && len(group.Spec.UpgradeStrategy.Canaries) > 0 {
		for _, canary := range group.Spec.UpgradeStrategy.Canaries {
			upgradePlan = append(upgradePlan, []string{canary})
			isCanary[canary] = true
		}

	}
	for i := 0; i < len(group.Spec.Sites); i += group.Spec.UpgradeStrategy.MaxConcurrency {
		var batch []string
		for j := i; j < i+group.Spec.UpgradeStrategy.MaxConcurrency && j != len(group.Spec.Sites); j++ {
			site := group.Spec.Sites[j]
			if !isCanary[site] {
				batch = append(batch, site)
			}
		}
		if len(batch) > 0 {
			upgradePlan = append(upgradePlan, batch)
		}
	}
	r.Log.Info("Upgrade plan", "upgradePlan", upgradePlan)

	// Reconcile resources
	for i, upgradeBatch := range upgradePlan {
		err = r.ensurePlacementRule(ctx, group, i+1, upgradeBatch)
		if err != nil {
			return ctrl.Result{}, err
		}
		err = r.ensurePolicies(ctx, group, i+1)
		if err != nil {
			return ctrl.Result{}, err
		}
		err = r.ensurePlacementBinding(ctx, group, i+1)
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	// Remediate policies depending on compliance state and upgrade plan.
	if group.Spec.RemediationAction == "enforce" {
		for i, upgradeBatch := range upgradePlan {
			batchCompliant := true
			for _, site := range upgradeBatch {
				err := r.remediateSite(ctx, group, i+1, site)
				if err != nil {
					return ctrl.Result{}, err
				}
				siteCompliant := true
				var labelsForGroup = map[string]string{"app": "cluster-group-lcm"}
				listOpts := []client.ListOption{
					client.InNamespace(group.GetName()),
					client.MatchingLabels(labelsForGroup),
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
					statusObject := policy.Object["status"].(map[string]interface{})
					if statusObject["compliant"] != nil {
						siteCompliant = siteCompliant && (statusObject["compliant"].(string) == "Compliant")
					} else {
						siteCompliant = false
					}
				}
				batchCompliant = batchCompliant && siteCompliant
			}
			if !batchCompliant {
				r.Log.Info("Upgrade batch not fully compliant yet", "upgradeBatch", upgradeBatch)
				break
			} else {
				r.Log.Info("Upgrade batch fully compliant", "upgradeBatch", upgradeBatch)
			}
		}
	}

	err = r.updateStatus(ctx, group)
	if err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *GroupReconciler) ensurePlacementRule(ctx context.Context, group *ranv1alpha1.Group, batch int, sites []string) error {
	pr := r.newPlacementRule(ctx, group, batch, sites)

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
		r.Log.Info("Created API PlacementRule object", "placementRule", pr)
	} else if err != nil {
		return err
	}

	return nil
}

func (r *GroupReconciler) newPlacementRule(ctx context.Context, group *ranv1alpha1.Group, batch int, sites []string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.Object = map[string]interface{}{
		"metadata": map[string]interface{}{
			"name":      group.Name + "-" + "batch" + "-" + strconv.Itoa(batch) + "-" + "placement-rule",
			"namespace": group.Namespace,
			"labels": map[string]interface{}{
				"app":                           "cluster-group-lcm",
				"cluster-group-lcm/group-owner": group.Name,
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
	for _, site := range sites {
		s := &ranv1alpha1.Site{}
		nn := types.NamespacedName{Namespace: group.Namespace, Name: site}
		err := r.Get(ctx, nn, s)
		if err != nil {
			r.Log.Error(err, "Failed to get Site")
			return nil
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

	return u
}

func (r *GroupReconciler) ensurePlacementBinding(ctx context.Context, group *ranv1alpha1.Group, batch int) error {
	pb := r.newPlacementBinding(ctx, group, batch)

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
		r.Log.Info("Created API PlacementBindingObject object", "placementBinding", pb)
	} else if err != nil {
		return err
	}

	return nil
}

func (r *GroupReconciler) newPlacementBinding(ctx context.Context, group *ranv1alpha1.Group, batch int) *unstructured.Unstructured {
	var subjects []map[string]interface{}
	for _, groupPolicyTemplate := range group.Spec.GroupPolicyTemplates {
		u := &unstructured.Unstructured{}
		err := json.Unmarshal(groupPolicyTemplate.ObjectDefinition.Raw, u)
		if err != nil {
			r.Log.Info("Unable to unmarshal group policy template")
			return nil
		}

		subject := make(map[string]interface{})
		subject["name"] = group.Name + "-" + "batch" + "-" + strconv.Itoa(batch) + "-" + u.GetName() + "-" + "policy"
		subject["kind"] = "Policy"
		subject["apiGroup"] = "policy.open-cluster-management.io"

		subjects = append(subjects, subject)
	}

	u := &unstructured.Unstructured{}
	u.Object = map[string]interface{}{
		"metadata": map[string]interface{}{
			"name":      group.Name + "-" + "batch" + "-" + strconv.Itoa(batch) + "-" + "placement-binding",
			"namespace": group.Namespace,
			"labels": map[string]interface{}{
				"app":                           "cluster-group-lcm",
				"cluster-group-lcm/group-owner": group.Name,
			},
		},
		"placementRef": map[string]interface{}{
			"name":     group.Name + "-" + "batch" + "-" + strconv.Itoa(batch) + "-" + "placement-rule",
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

func (r *GroupReconciler) ensurePolicies(ctx context.Context, group *ranv1alpha1.Group, batch int) error {
	for _, groupPolicyTemplate := range group.Spec.GroupPolicyTemplates {
		policy := r.newPolicy(ctx, group, batch, groupPolicyTemplate.ObjectDefinition)

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

func (r *GroupReconciler) newPolicy(ctx context.Context, group *ranv1alpha1.Group, batch int, objectDefinition runtime.RawExtension) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	err := json.Unmarshal(objectDefinition.Raw, u)
	if err != nil {
		return nil
	}

	u.SetName(group.Name + "-" + "batch" + "-" + strconv.Itoa(batch) + "-" + u.GetName() + "-" + "policy")
	u.SetNamespace(group.GetNamespace())
	labels := u.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	labels["app"] = "cluster-group-lcm"
	labels["cluster-group-lcm/group-owner"] = group.Name
	u.SetLabels(labels)

	specObject := u.Object["spec"].(map[string]interface{})
	specObject["remediationAction"] = "inform"

	return u
}

func (r *GroupReconciler) remediateSite(ctx context.Context, group *ranv1alpha1.Group, batch int, site string) error {
	r.Log.Info("Remediating Site", "site", site)
	c := &ranv1alpha1.Common{}
	cnn := types.NamespacedName{Namespace: group.Namespace, Name: "common"}
	err := r.Get(ctx, cnn, c)
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	if c != nil {
		for _, commonPolicyTemplate := range c.Spec.CommonPolicyTemplates {
			u := &unstructured.Unstructured{}
			err := json.Unmarshal(commonPolicyTemplate.ObjectDefinition.Raw, u)
			if err != nil {
				return err
			}

			foundPolicy := &unstructured.Unstructured{}
			foundPolicy.SetGroupVersionKind(schema.GroupVersionKind{
				Group:   "policy.open-cluster-management.io",
				Kind:    "Policy",
				Version: "v1",
			})

			err = r.Client.Get(ctx, client.ObjectKey{
				Name:      "common" + "-" + u.GetName() + "-" + "policy",
				Namespace: group.Namespace,
			}, foundPolicy)
			if err != nil {
				return err
			}

			specObject := foundPolicy.Object["spec"].(map[string]interface{})
			if specObject["remediationAction"] == "inform" {
				specObject["remediationAction"] = "enforce"
				err = r.Client.Update(ctx, foundPolicy)
				if err != nil {
					return err
				}
				r.Log.Info("Set remediationAction to enforce on Common Policy object", "policy", foundPolicy)
			}
		}
	}

	s := &ranv1alpha1.Site{}
	nn := types.NamespacedName{Namespace: group.Namespace, Name: site}
	err = r.Get(ctx, nn, s)
	if err != nil {
		return err
	}

	for _, sitePolicyTemplate := range s.Spec.SitePolicyTemplates {
		u := &unstructured.Unstructured{}
		err := json.Unmarshal(sitePolicyTemplate.ObjectDefinition.Raw, u)
		if err != nil {
			return err
		}

		foundPolicy := &unstructured.Unstructured{}
		foundPolicy.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "policy.open-cluster-management.io",
			Kind:    "Policy",
			Version: "v1",
		})

		err = r.Client.Get(ctx, client.ObjectKey{
			Name:      site + "-" + u.GetName() + "-" + "policy",
			Namespace: group.Namespace,
		}, foundPolicy)
		if err != nil {
			return err
		}

		specObject := foundPolicy.Object["spec"].(map[string]interface{})
		if specObject["remediationAction"] == "inform" {
			specObject["remediationAction"] = "enforce"
			err = r.Client.Update(ctx, foundPolicy)
			if err != nil {
				return err
			}
			r.Log.Info("Set remediationAction to enforce on Site Policy object", "policy", foundPolicy)
		}
	}

	for _, groupPolicyTemplate := range group.Spec.GroupPolicyTemplates {
		u := &unstructured.Unstructured{}
		err := json.Unmarshal(groupPolicyTemplate.ObjectDefinition.Raw, u)
		if err != nil {
			return err
		}

		foundPolicy := &unstructured.Unstructured{}
		foundPolicy.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "policy.open-cluster-management.io",
			Kind:    "Policy",
			Version: "v1",
		})

		err = r.Client.Get(ctx, client.ObjectKey{
			Name:      group.Name + "-" + "batch" + "-" + strconv.Itoa(batch) + "-" + u.GetName() + "-" + "policy",
			Namespace: group.Namespace,
		}, foundPolicy)
		if err != nil {
			return err
		}

		specObject := foundPolicy.Object["spec"].(map[string]interface{})
		if specObject["remediationAction"] == "inform" {
			specObject["remediationAction"] = "enforce"
			err = r.Client.Update(ctx, foundPolicy)
			if err != nil {
				return err
			}
			r.Log.Info("Set remediationAction to enforce on Group Policy object", "policy", foundPolicy)
		}
	}

	return nil
}

func (r *GroupReconciler) updateStatus(ctx context.Context, group *ranv1alpha1.Group) error {
	var labelsForGroup = map[string]string{"app": "cluster-group-lcm", "cluster-group-lcm/group-owner": group.GetName()}
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
