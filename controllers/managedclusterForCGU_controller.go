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
	"sort"
	"strings"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	policiesv1 "github.com/open-cluster-management/governance-policy-propagator/api/v1"
	ranv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/api/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

const (
	clusterStatusCheckRetryDelay = time.Minute * 5
	ztpInstallNS                 = "ztp-install"
	ztpDeployWaveAnnotation      = "ran.openshift.io/ztp-deploy-wave"
)

// ManagedClusterForCguReconciler reconciles a ManagedCluster object to auto create the ClusterGroupUpgrade
type ManagedClusterForCguReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// Reconcile the managed cluster auto create ClusterGroupUpgrade
// - Controller watches for create event of managed cluster object. Reconcilation
//   is triggered when a new managed cluster is created
// - When a new managed cluster is created, create ClusterGroupUpgrade CR for the
//   cluster only when it's ready and its child policies are available
// - As created ClusterGroupUpgrade has ownReference set to its managed cluster,
//   when the managed cluster is deleted, the ClusterGroupUpgrade will be auto-deleted
//
// Note: The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ManagedClusterForCguReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	reqLogger := r.Log.WithValues("Request.Name", req.Name)
	reqLogger.Info("Reconciling managedCluster to create clusterGroupUpgrade")

	managedCluster := &clusterv1.ManagedCluster{}
	if err := r.Get(ctx, types.NamespacedName{Name: req.Name}, managedCluster); err != nil {
		if errors.IsNotFound(err) {
			// managed cluster could have been deleted
			return ctrl.Result{}, nil
		}
		// Error reading managed cluster, requeue the request
		return ctrl.Result{}, err
	}

	clusterGroupUpgrade := &ranv1alpha1.ClusterGroupUpgrade{}
	if err := r.Get(ctx, types.NamespacedName{Name: req.Name, Namespace: ztpInstallNS}, clusterGroupUpgrade); err != nil {
		if !errors.IsNotFound(err) {
			// Error reading clusterGroupUpgrade, requeue the request
			return ctrl.Result{}, err
		}
	} else {
		// clusterGroupUpgrade for this cluster already exists, stop reconcile
		r.Log.Info("clusterGroupUpgrade found", "Name", clusterGroupUpgrade.Name, "Namespace", clusterGroupUpgrade.Namespace)
		return ctrl.Result{}, nil
	}

	// clusterGroupUpgrade CR doesn't exist
	availableCondition := meta.FindStatusCondition(managedCluster.Status.Conditions, clusterv1.ManagedClusterConditionAvailable)
	if availableCondition == nil {
		r.Log.Info("cluster has no status yet", "Name", managedCluster.Name, "RequeueAfter:", clusterStatusCheckRetryDelay)
		return ctrl.Result{RequeueAfter: clusterStatusCheckRetryDelay}, nil
	} else if availableCondition.Status == metav1.ConditionTrue {
		// cluster is ready
		r.Log.Info("cluster is ready", "Name", managedCluster.Name)

		// Child policies get created as soon as the placementrule/placementbinding
		// gets created and matches the parent policies to the managedcluster.
		// It takes ~45 minutes for cluster to be installed and ready.
		// At this stage, all child policies should be created.
		policies, err := r.getPolicies(ctx, managedCluster.Name)
		if err != nil {
			return ctrl.Result{}, err
		}
		if len(policies.Items) == 0 {
			// likey that no policies created so child policies not found
			r.Log.Info("WARN: No child policies found for cluster", "Name", managedCluster.Name)
			return ctrl.Result{}, nil
		}

		// create clusterGroupUpgrade
		if err = r.newClusterGroupUpgrade(ctx, managedCluster, policies); err != nil {
			return ctrl.Result{}, err
		}
	} else {
		// availableCondition is false or unknown, cluster is not ready
		r.Log.Info("cluster is not ready", "RequeueAfter:", clusterStatusCheckRetryDelay)
		return ctrl.Result{RequeueAfter: clusterStatusCheckRetryDelay}, nil
	}

	return ctrl.Result{}, nil
}

func (r *ManagedClusterForCguReconciler) getPolicies(ctx context.Context, clusterName string) (*policiesv1.PolicyList, error) {
	policies := &policiesv1.PolicyList{}
	if err := r.List(ctx, policies, client.InNamespace(clusterName)); err != nil {
		return nil, err
	}
	return policies, nil
}

// Create a clusterGroupUpgrade
func (r *ManagedClusterForCguReconciler) newClusterGroupUpgrade(ctx context.Context, cluster *clusterv1.ManagedCluster, policies *policiesv1.PolicyList) (err error) {
	var managedPolicies []string
	var policyWaveMap = make(map[string]string)

	// Generate a list of ordered managed policies based on the deploy wave.
	// Deploywave is a way to order deployment of policies, it's defined
	// as a annotation in policy.
	// For example,
	//   metadata:
	//     annotations:
	//       "ran.openshift.io/ztp-deploy-wave": "1"
	// The list of policies is ordered from the lowest value to the highest.
	// Policy without a wave is not managed.
	for _, policy := range policies.Items {
		deployWave, found := policy.GetAnnotations()[ztpDeployWaveAnnotation]
		if found {
			policyName := strings.SplitAfter(policy.GetName(), ".")[1]
			policyWaveMap[policyName] = deployWave
			managedPolicies = append(managedPolicies, policyName)
		}
	}

	if len(managedPolicies) == 0 {
		r.Log.Info("No policies need to be managed by ClusterGroupUpgrade operator")
		return nil
	}

	sort.Slice(managedPolicies, func(i, j int) bool {
		return policyWaveMap[managedPolicies[i]] < policyWaveMap[managedPolicies[j]]
	})

	cguMeta := metav1.ObjectMeta{
		Name:      cluster.Name,
		Namespace: ztpInstallNS,
	}
	cguSpec := ranv1alpha1.ClusterGroupUpgradeSpec{
		Enable:          true, // default
		Clusters:        []string{cluster.Name},
		ManagedPolicies: managedPolicies,
		RemediationStrategy: &ranv1alpha1.RemediationStrategySpec{
			MaxConcurrency: 1,
		},
	}
	clusterGroupUpgrade := &ranv1alpha1.ClusterGroupUpgrade{
		ObjectMeta: cguMeta,
		Spec:       cguSpec,
	}

	// set managedcluster as the owner of its created ClusterGroupUpgrade CR, so when a cluster
	// is deleted, its dependent ClusterGroupUpgrade CR will be automatically cleaned up
	if err := controllerutil.SetControllerReference(cluster, clusterGroupUpgrade, r.Scheme); err != nil {
		return err
	}

	if err := r.Create(ctx, clusterGroupUpgrade); err != nil {
		r.Log.Error(err, "Fail to create clusterGroupUpgrade", "name", cluster.Name, "namespace", ztpInstallNS)
		return err
	}

	r.Log.Info("Created clusterGroupUpgrade", "name", cluster.Name, "namespace", ztpInstallNS)
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ManagedClusterForCguReconciler) SetupWithManager(mgr ctrl.Manager) error {
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: ztpInstallNS,
		},
	}
	if err := r.Create(context.TODO(), namespace); err != nil {
		// fail to create namespace
		if !errors.IsAlreadyExists(err) {
			return err
		}
	}

	return ctrl.NewControllerManagedBy(mgr).
		Named("managedclusterForCGU").
		For(&clusterv1.ManagedCluster{},
			// watch for create and delete events for managedcluster
			builder.WithPredicates(predicate.Funcs{
				GenericFunc: func(e event.GenericEvent) bool { return false },
				CreateFunc:  func(e event.CreateEvent) bool { return true },
				DeleteFunc:  func(e event.DeleteEvent) bool { return false },
				UpdateFunc:  func(e event.UpdateEvent) bool { return false },
			})).
		Complete(r)
}
