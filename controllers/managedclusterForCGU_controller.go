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
	"fmt"
	"sort"
	"strconv"
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

	ranv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/api/v1alpha1"
	utils "github.com/openshift-kni/cluster-group-upgrades-operator/controllers/utils"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	policiesv1 "open-cluster-management.io/governance-policy-propagator/api/v1"
)

const (
	clusterStatusCheckRetryDelay = time.Minute * 1
	ztpInstallNS                 = "ztp-install"
	ztpDeployWaveAnnotation      = "ran.openshift.io/ztp-deploy-wave"
	ztpRunningLabel              = "ztp-running"
	ztpDoneLabel                 = "ztp-done"
)

// ManagedClusterForCguReconciler reconciles a ManagedCluster object to auto create the ClusterGroupUpgrade
type ManagedClusterForCguReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=cluster.open-cluster-management.io,resources=managedclusters,verbs=get;list;watch
//+kubebuilder:rbac:groups=cluster.open-cluster-management.io,resources=managedclusters/finalizers,verbs=update
//+kubebuilder:rbac:groups=ran.openshift.io,resources=clustergroupupgrades,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch;create;update;patch;delete

// Reconcile the managed cluster auto create ClusterGroupUpgrade
//   - Controller watches for create event of managed cluster object. Reconciliation
//     is triggered when a new managed cluster is created
//   - When a new managed cluster is created, create ClusterGroupUpgrade CR for the
//     cluster only when it's ready and its child policies are available
//   - As created ClusterGroupUpgrade has ownReference set to its managed cluster,
//     when the managed cluster is deleted, the ClusterGroupUpgrade will be auto-deleted
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

	// Stop creating UOCR if ztp of this cluster is done already
	if _, found := managedCluster.Labels[ztpDoneLabel]; found {
		r.Log.Info("ZTP for the cluster has completed. "+ztpDoneLabel+" label found.", "Name", managedCluster.Name)
		return ctrl.Result{}, nil
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
		policies, err := utils.GetChildPolicies(ctx, r.Client, []string{managedCluster.Name})
		if err != nil {
			return ctrl.Result{}, err
		}
		if len(policies) == 0 {
			// likely no policies were created, so no child policies found
			r.Log.Info("WARN: No child policies found for cluster", "Name", managedCluster.Name)
		}

		// create clusterGroupUpgrade
		if err := r.newClusterGroupUpgrade(ctx, managedCluster, policies); err != nil {
			return ctrl.Result{}, err
		}
	} else {
		// availableCondition is false or unknown, cluster is not ready
		r.Log.Info("cluster is not ready", "RequeueAfter:", clusterStatusCheckRetryDelay)
		return ctrl.Result{RequeueAfter: clusterStatusCheckRetryDelay}, nil
	}

	return ctrl.Result{}, nil
}

// sort map[string]int by value in ascending order, return sorted keys
func sortMapByValue(sortMap map[string]int) []string {
	var keys []string
	for key := range sortMap {
		keys = append(keys, key)
	}

	sort.Slice(keys, func(i, j int) bool {
		// for equal elements, sort string alphabetically
		if sortMap[keys[i]] == sortMap[keys[j]] {
			return keys[i] < keys[j]
		}
		return sortMap[keys[i]] < sortMap[keys[j]]
	})
	return keys
}

// Create a clusterGroupUpgrade
func (r *ManagedClusterForCguReconciler) newClusterGroupUpgrade(
	ctx context.Context, cluster *clusterv1.ManagedCluster, childPolicies []policiesv1.Policy) (err error) {

	var policyWaveMap = make(map[string]int)

	// Generate a list of ordered managed policies based on the deploy wave.
	// Deploywave is a way to order deployment of policies, it's defined
	// as a annotation in policy.
	// For example,
	//   metadata:
	//     annotations:
	//       "ran.openshift.io/ztp-deploy-wave": "1"
	// The list of policies is ordered from the lowest value to the highest.
	// Policy without a wave is not managed.
	for _, cPolicy := range childPolicies {
		// Ignore policies with remediationAction enforce
		if strings.EqualFold(string(cPolicy.Spec.RemediationAction), "enforce") {
			r.Log.Info("Ignoring policy " + cPolicy.Name + " with remediationAction enforce")
			continue
		}

		deployWave, found := cPolicy.GetAnnotations()[ztpDeployWaveAnnotation]
		if found {
			deployWaveInt, err := strconv.Atoi(deployWave)
			if err != nil {
				// err convert from string to int
				return fmt.Errorf("%s in policy %s is not an interger: %s", ztpDeployWaveAnnotation, cPolicy.GetName(), err)
			}
			policyName, err := utils.GetParentPolicyNameAndNamespace(cPolicy.GetName())
			if err != nil {
				r.Log.Info("Ignoring policy " + cPolicy.Name + " with invalid name")
				continue
			}
			policyWaveMap[policyName[1]] = deployWaveInt
		}
	}

	sortedManagedPolicies := sortMapByValue(policyWaveMap)
	cguMeta := metav1.ObjectMeta{
		Name:      cluster.Name,
		Namespace: ztpInstallNS,
	}
	enable := true // default
	cguSpec := ranv1alpha1.ClusterGroupUpgradeSpec{
		Enable:          &enable,
		Clusters:        []string{cluster.Name},
		ManagedPolicies: sortedManagedPolicies,
		RemediationStrategy: &ranv1alpha1.RemediationStrategySpec{
			MaxConcurrency: 1,
		},
		Actions: ranv1alpha1.Actions{
			BeforeEnable: ranv1alpha1.BeforeEnable{
				AddClusterLabels: map[string]string{
					ztpRunningLabel: "",
				},
			},
			AfterCompletion: ranv1alpha1.AfterCompletion{
				AddClusterLabels: map[string]string{
					ztpDoneLabel: "",
				},
				DeleteClusterLabels: map[string]string{
					ztpRunningLabel: "",
				},
			},
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
		if errors.IsNotFound(err) && strings.Contains(err.Error(), "namespace") {
			namespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: ztpInstallNS,
				},
			}
			if err := r.Create(ctx, namespace); err != nil {
				r.Log.Error(err, "Fail to create namespace", "name", ztpInstallNS)
				return err
			}
			// retry
			if err := r.Create(ctx, clusterGroupUpgrade); err != nil {
				r.Log.Error(err, "Fail to create clusterGroupUpgrade", "name", cluster.Name, "namespace", ztpInstallNS)
				return err
			}
		}

		r.Log.Error(err, "Fail to create clusterGroupUpgrade", "name", cluster.Name, "namespace", ztpInstallNS)
		return err
	}

	r.Log.Info("Found ManagedCluster "+cluster.Name+" without "+ztpDoneLabel+" label. Created clusterGroupUpgrade.",
		"name", cluster.Name, "namespace", ztpInstallNS)
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
			// watch for create event for managedcluster
			builder.WithPredicates(predicate.Funcs{
				GenericFunc: func(e event.GenericEvent) bool { return false },
				CreateFunc:  func(e event.CreateEvent) bool { return true },
				DeleteFunc:  func(e event.DeleteEvent) bool { return false },
				UpdateFunc:  func(e event.UpdateEvent) bool { return false },
			})).
		Owns(&ranv1alpha1.ClusterGroupUpgrade{},
			// watch for delete event for owned ClusterGroupUpgrade
			builder.WithPredicates(predicate.Funcs{
				GenericFunc: func(e event.GenericEvent) bool { return false },
				CreateFunc:  func(e event.CreateEvent) bool { return false },
				DeleteFunc:  func(e event.DeleteEvent) bool { return true },
				UpdateFunc:  func(e event.UpdateEvent) bool { return false },
			})).
		Complete(r)
}
