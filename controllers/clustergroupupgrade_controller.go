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
	"fmt"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ranv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/api/v1alpha1"
	workv1 "open-cluster-management.io/api/work/v1"
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
//+kubebuilder:rbac:groups=work.open-cluster-management.io,resources=manifestworks,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=cluster.open-cluster-management.io,resources=managedclusters,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=action.open-cluster-management.io,resources=managedclusteractions,verbs=create;update;delete;get;list;watch;patch
//+kubebuilder:rbac:groups=view.open-cluster-management.io,resources=managedclusterviews,verbs=create;update;delete;get;list;watch;patch
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch
//+kubebuilder:rbac:groups=operators.coreos.com,resources=clusterserviceversions,verbs=get;list;watch

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

	r.reconcileResources(ctx, clusterGroupUpgrade)

	return ctrl.Result{}, nil
}

func (r *ClusterGroupUpgradeReconciler) getPolicyByName(ctx context.Context, policyName string, namespace string) (*unstructured.Unstructured, error) {
	foundPolicy := &unstructured.Unstructured{}
	foundPolicy.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "policy.open-cluster-management.io",
		Kind:    "Policy",
		Version: "v1",
	})

	err := r.Get(ctx, types.NamespacedName{Name: policyName, Namespace: namespace}, foundPolicy)

	return foundPolicy, err
}

func (r *ClusterGroupUpgradeReconciler) ensureManifestWork(
	ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade, cluster, managedPolicy string) error {

	// Get payload from managed policy
	policy, err := r.getPolicyByName(ctx, managedPolicy, clusterGroupUpgrade.GetNamespace())
	if err != nil {
		return fmt.Errorf("policy was not found")
	}
	spec := policy.Object["spec"].(map[string]interface{})
	policyTemplate := spec["policy-templates"].([]interface{})[0].(map[string]interface{})
	configurationPolicySpec := policyTemplate["objectDefinition"].(map[string]interface{})["spec"].(map[string]interface{})
	objectTemplate := configurationPolicySpec["object-templates"].([]interface{})[0].(map[string]interface{})
	payload := objectTemplate["objectDefinition"].(map[string]interface{})
	r.Log.Info("DEBUG", "payload", payload)

	// Create ManifestWork on namespace cluster with workload from previous step
	manifestWork := &workv1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterGroupUpgrade.Name + "-" + managedPolicy,
			Namespace: cluster,
		},
		Spec: workv1.ManifestWorkSpec{
			Workload: workv1.ManifestsTemplate{
				Manifests: []workv1.Manifest{},
			},
		},
	}

	manifest := workv1.Manifest{}
	manifest.Raw, _ = json.Marshal(payload)
	manifestWork.Spec.Workload.Manifests = append(manifestWork.Spec.Workload.Manifests, manifest)

	r.Log.Info("DEBUG", "manifestWork", manifestWork)
	return nil
}

func (r *ClusterGroupUpgradeReconciler) reconcileResources(ctx context.Context, clusterGroupUpgrade *ranv1alpha1.ClusterGroupUpgrade) error {
	// Reconcile resources
	for _, cluster := range clusterGroupUpgrade.Spec.Clusters {
		for _, managedPolicy := range clusterGroupUpgrade.Spec.ManagedPolicies {
			err := r.ensureManifestWork(ctx, clusterGroupUpgrade, cluster, managedPolicy)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterGroupUpgradeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.Recorder = mgr.GetEventRecorderFor("ClusterGroupUpgrade")

	return ctrl.NewControllerManagedBy(mgr).
		For(&ranv1alpha1.ClusterGroupUpgrade{}).
		Owns(&workv1.ManifestWork{}).
		Complete(r)
}
