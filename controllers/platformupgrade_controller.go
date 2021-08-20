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
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	ranv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/api/v1alpha1"
)

const policyTemplate = `
apiVersion: policy.open-cluster-management.io/v1
kind: Policy
metadata:
  name: upgrade-cluster
  annotations:
    policy.open-cluster-management.io/standards: NIST SP 800-53
    policy.open-cluster-management.io/categories: CM Configuration Management
    policy.open-cluster-management.io/controls: CM-2 Baseline Configuration
spec:
  disabled: false
  policy-templates:
    - objectDefinition:
        apiVersion: policy.open-cluster-management.io/v1
        kind: ConfigurationPolicy
        metadata:
          name: upgrade-cluster
        spec:
          remediationAction: inform
          severity: high
          object-templates:
            - complianceType: musthave
              objectDefinition:
                apiVersion: config.openshift.io/v1
                kind: ClusterVersion
                metadata:
                  name: version
                spec:
                  channel: {{ .Channel }}
                  desiredUpdate:
                    version: "{{ .Version }}"
                    image: {{ .Image }}
                    force: True
                  upstream: {{ .Upstream }}
    - objectDefinition:
        apiVersion: policy.open-cluster-management.io/v1
        kind: ConfigurationPolicy
        metadata:
          name: policy-upgrade-check-co-available
        spec:
          remediationAction: inform
          severity: low
          namespaceSelector:
            exclude:
              - kube-*
            include:
              - default
          object-templates:
            - complianceType: mustnothave
              objectDefinition:
                apiVersion: config.openshift.io/v1
                kind: ClusterOperator
                status:
                  conditions:
                    - status: 'False'
                      type: Available
    - objectDefinition:
        apiVersion: policy.open-cluster-management.io/v1
        kind: ConfigurationPolicy
        metadata:
          name: policy-upgrade-check-co-not-degraded
        spec:
          remediationAction: inform
          severity: low
          namespaceSelector:
            exclude:
              - kube-*
            include:
              - default
          object-templates:
            - complianceType: mustnothave
              objectDefinition:
                apiVersion: config.openshift.io/v1
                kind: ClusterOperator
                status:
                  conditions:
                    - status: 'True'
                      type: Degraded           
    - objectDefinition:
        apiVersion: policy.open-cluster-management.io/v1
        kind: ConfigurationPolicy
        metadata:
          name: check-installed-version-status
        spec:
          remediationAction: inform # will be overridden by remediationAction in parent policy
          severity: high
          object-templates:
            - complianceType: musthave
              objectDefinition:
                apiVersion: config.openshift.io/v1
                kind: ClusterVersion
                metadata:
                  name: version
                status:
                  history:
                    - state: Completed 
                      version: "{{ .Version }}"    
                      image: {{ .Image }}
  remediationAction: inform     
`

// PlatformUpgradeReconciler reconciles a PlatformUpgrade object
type PlatformUpgradeReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=ran.openshift.io,resources=platformupgrades,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=ran.openshift.io,resources=platformupgrades/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=ran.openshift.io,resources=platformupgrades/finalizers,verbs=update
//+kubebuilder:rbac:groups=apps.open-cluster-management.io,resources=placementrules,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=policy.open-cluster-management.io,resources=placementbindings,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=policy.open-cluster-management.io,resources=policies,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=cluster.open-cluster-management.io,resources=managedclusters,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the PlatformUpgrade object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.8.3/pkg/reconcile
func (r *PlatformUpgradeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = r.Log.WithValues("platformUpgrade", req.NamespacedName)

	platformUpgrade := &ranv1alpha1.PlatformUpgrade{}
	err := r.Get(ctx, req.NamespacedName, platformUpgrade)
	if err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		r.Log.Error(err, "Failed to get PlatformUpgrade")
		return ctrl.Result{}, err
	}

	// Create remediation plan
	var remediationPlan [][]string
	isCanary := make(map[string]bool)
	if platformUpgrade.Spec.RemediationStrategy.Canaries != nil && len(platformUpgrade.Spec.RemediationStrategy.Canaries) > 0 {
		for _, canary := range platformUpgrade.Spec.RemediationStrategy.Canaries {
			remediationPlan = append(remediationPlan, []string{canary})
			isCanary[canary] = true
		}

	}
	var clusters []string
	for _, cluster := range platformUpgrade.Spec.Clusters {
		if !isCanary[cluster] {
			clusters = append(clusters, cluster)
		}
	}
	for i := 0; i < len(clusters); i += platformUpgrade.Spec.RemediationStrategy.MaxConcurrency {
		var batch []string
		for j := i; j < i+platformUpgrade.Spec.RemediationStrategy.MaxConcurrency && j != len(clusters); j++ {
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
		placementRulesForBatch, err := r.ensureBatchPlacementRules(ctx, platformUpgrade, remediateBatch, i+1)
		if err != nil {
			return ctrl.Result{}, err
		}
		placementRules = append(placementRules, placementRulesForBatch...)
		policiesForBatch, err := r.ensureBatchPolicies(ctx, platformUpgrade, remediateBatch, i+1)
		if err != nil {
			return ctrl.Result{}, err
		}
		policies = append(policies, policiesForBatch...)
		placementBindingsForBatch, err := r.ensureBatchPlacementBindings(ctx, platformUpgrade, remediateBatch, i+1)
		if err != nil {
			return ctrl.Result{}, err
		}
		placementBindings = append(placementBindings, placementBindingsForBatch...)
	}

	// Remediate policies depending on compliance state and upgrade plan.
	if platformUpgrade.Spec.RemediationAction == "enforce" {
		for i, remediateBatch := range remediationPlan {
			r.Log.Info("Remediating clusters", "remediateBatch", remediateBatch)
			r.Log.Info("Batch", "i", i+1)
			batchCompliant := true
			var labelsForBatch = map[string]string{"openshift-cluster-group-upgrades/batch": strconv.Itoa(i + 1)}
			listOpts := []client.ListOption{
				client.InNamespace(platformUpgrade.Namespace),
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
	err = r.updateStatus(ctx, platformUpgrade, placementRules, placementBindings, policies)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Delete old resources
	r.deleteOldResources(ctx, platformUpgrade)

	return ctrl.Result{}, nil
}

func (r *PlatformUpgradeReconciler) ensureBatchPlacementRules(ctx context.Context, platformUpgrade *ranv1alpha1.PlatformUpgrade, batch []string, batchIndex int) ([]string, error) {
	var placementRules []string

	pr, err := r.newBatchPlacementRule(ctx, platformUpgrade, batch, batchIndex)
	if err != nil {
		return nil, err
	}
	placementRules = append(placementRules, pr.GetName())

	if err := controllerutil.SetControllerReference(platformUpgrade, pr, r.Scheme); err != nil {
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
		Namespace: platformUpgrade.Namespace,
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

func (r *PlatformUpgradeReconciler) newBatchPlacementRule(ctx context.Context, platformUpgrade *ranv1alpha1.PlatformUpgrade, batch []string, batchIndex int) (*unstructured.Unstructured, error) {
	u := &unstructured.Unstructured{}
	u.Object = map[string]interface{}{
		"metadata": map[string]interface{}{
			"name":      platformUpgrade.Name + "-" + "batch" + "-" + strconv.Itoa(batchIndex),
			"namespace": platformUpgrade.Namespace,
			"labels": map[string]interface{}{
				"app": "openshift-cluster-group-upgrades",
				"openshift-cluster-group-upgrades/platformUpgrade": platformUpgrade.Name,
				"openshift-cluster-group-upgrades/batch":           strconv.Itoa(batchIndex),
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

func (r *PlatformUpgradeReconciler) ensureBatchPolicies(ctx context.Context, platformUpgrade *ranv1alpha1.PlatformUpgrade, batch []string, batchIndex int) ([]string, error) {
	var policies []string

	policy, err := r.newBatchPolicy(ctx, platformUpgrade, batchIndex)
	if err != nil {
		return nil, err
	}
	policies = append(policies, policy.GetName())

	if err := controllerutil.SetControllerReference(platformUpgrade, policy, r.Scheme); err != nil {
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
		Namespace: platformUpgrade.Namespace,
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
			unstructured.SetNestedField(foundPolicy.Object, platformUpgrade.Spec.RemediationAction, "spec", "remediationAction")
			err = r.Client.Update(ctx, foundPolicy)
			if err != nil {
				return nil, err
			}
			r.Log.Info("Updated API Policy object", "policy", foundPolicy.GetName())
		}
	}

	return policies, nil
}

func (r *PlatformUpgradeReconciler) newBatchPolicy(ctx context.Context, platformUpgrade *ranv1alpha1.PlatformUpgrade, batchIndex int) (*unstructured.Unstructured, error) {
	var buf bytes.Buffer
	tmpl := template.New("policy")
	tmpl.Parse(policyTemplate)
	tmpl.Execute(&buf, map[string]string{"Channel": platformUpgrade.Spec.Channel,
		"Version":  platformUpgrade.Spec.Version,
		"Image":    platformUpgrade.Spec.Image,
		"Upstream": platformUpgrade.Spec.Upstream,
	})
	u := &unstructured.Unstructured{}
	dec := yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)
	_, _, err := dec.Decode(buf.Bytes(), nil, u)
	if err != nil {
		return nil, err
	}

	u.SetName(platformUpgrade.Name + "-" + "batch" + "-" + strconv.Itoa(batchIndex))
	u.SetNamespace(platformUpgrade.GetNamespace())
	labels := u.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	labels["app"] = "openshift-cluster-group-upgrades"
	labels["openshift-cluster-group-upgrades/platformUpgrade"] = platformUpgrade.Name
	labels["openshift-cluster-group-upgrades/batch"] = strconv.Itoa(batchIndex)
	u.SetLabels(labels)

	specObject := u.Object["spec"].(map[string]interface{})
	specObject["remediationAction"] = "inform"

	return u, nil
}

func (r *PlatformUpgradeReconciler) ensureBatchPlacementBindings(ctx context.Context, platformUpgrade *ranv1alpha1.PlatformUpgrade, batch []string, batchIndex int) ([]string, error) {
	var placementBindings []string

	// ensure batch placement bindings
	pb, err := r.newBatchPlacementBinding(ctx, platformUpgrade, batchIndex)
	if err != nil {
		return nil, err
	}
	placementBindings = append(placementBindings, pb.GetName())

	if err := controllerutil.SetControllerReference(platformUpgrade, pb, r.Scheme); err != nil {
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
		Namespace: platformUpgrade.Namespace,
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

func (r *PlatformUpgradeReconciler) newBatchPlacementBinding(ctx context.Context, platformUpgrade *ranv1alpha1.PlatformUpgrade, batchIndex int) (*unstructured.Unstructured, error) {
	var subjects []map[string]interface{}

	subject := make(map[string]interface{})
	subject["name"] = platformUpgrade.Name + "-" + "batch" + "-" + strconv.Itoa(batchIndex)
	subject["kind"] = "Policy"
	subject["apiGroup"] = "policy.open-cluster-management.io"

	subjects = append(subjects, subject)

	u := &unstructured.Unstructured{}
	u.Object = map[string]interface{}{
		"metadata": map[string]interface{}{
			"name":      platformUpgrade.Name + "-" + "batch" + "-" + strconv.Itoa(batchIndex),
			"namespace": platformUpgrade.Namespace,
			"labels": map[string]interface{}{
				"app": "openshift-cluster-group-upgrades",
				"openshift-cluster-group-upgrades/platformUpgrade": platformUpgrade.Name,
			},
		},
		"placementRef": map[string]interface{}{
			"name":     platformUpgrade.Name + "-" + "batch" + "-" + strconv.Itoa(batchIndex),
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

func (r *PlatformUpgradeReconciler) updateStatus(ctx context.Context, platformUpgrade *ranv1alpha1.PlatformUpgrade, placementRules []string, placementBindings []string, policies []string) error {
	platformUpgrade.Status.PlacementRules = placementRules
	platformUpgrade.Status.PlacementBindings = placementBindings

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
			Namespace: platformUpgrade.Namespace,
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
	platformUpgrade.Status.Policies = policiesStatus

	err := r.Status().Update(ctx, platformUpgrade)
	if err != nil {
		return err
	}
	r.Log.Info("Updated PlatformUpgrade status")

	return nil
}

func (r *PlatformUpgradeReconciler) deleteOldResources(ctx context.Context, platformUpgrade *ranv1alpha1.PlatformUpgrade) error {
	var labelsForPlatformUpgrade = map[string]string{"app": "openshift-cluster-group-upgrades", "openshift-cluster-group-upgrades/platformUpgrade": platformUpgrade.GetName()}
	listOpts := []client.ListOption{
		client.InNamespace(platformUpgrade.Namespace),
		client.MatchingLabels(labelsForPlatformUpgrade),
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
		for _, statusPlacementRule := range platformUpgrade.Status.PlacementRules {
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
		for _, statusPlacementBinding := range platformUpgrade.Status.PlacementBindings {
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
		for _, statusPolicy := range platformUpgrade.Status.Policies {
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
func (r *PlatformUpgradeReconciler) SetupWithManager(mgr ctrl.Manager) error {
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
		For(&ranv1alpha1.PlatformUpgrade{}).
		Owns(placementRuleUnstructured).
		Owns(placementBindingUnstructured).
		Owns(policyUnstructured).
		Watches(&source.Kind{Type: &ranv1alpha1.Site{}}, handler.EnqueueRequestsFromMapFunc(func(a client.Object) []reconcile.Request {
			groupsList := &ranv1alpha1.GroupList{}
			client := mgr.GetClient()

			err := client.List(context.TODO(), groupsList)
			if err != nil {
				return []reconcile.Request{}
			}

			reconcileRequests := make([]reconcile.Request, len(groupsList.Items))
			for _, group := range groupsList.Items {
				reconcileRequests = append(reconcileRequests, reconcile.Request{
					NamespacedName: types.NamespacedName{
						Namespace: group.Namespace,
						Name:      group.Name,
					},
				})
			}
			return reconcileRequests
		})).
		Watches(&source.Kind{Type: &ranv1alpha1.Common{}}, handler.EnqueueRequestsFromMapFunc(func(a client.Object) []reconcile.Request {
			groupsList := &ranv1alpha1.GroupList{}
			client := mgr.GetClient()

			err := client.List(context.TODO(), groupsList)
			if err != nil {
				return []reconcile.Request{}
			}

			reconcileRequests := make([]reconcile.Request, len(groupsList.Items))
			for _, group := range groupsList.Items {
				reconcileRequests = append(reconcileRequests, reconcile.Request{
					NamespacedName: types.NamespacedName{
						Namespace: group.Namespace,
						Name:      group.Name,
					},
				})
			}
			return reconcileRequests
		})).
		Complete(r)
}
