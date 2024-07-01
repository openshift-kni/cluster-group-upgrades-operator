/*
Copyright 2024.

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
	"strings"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/openshift-kni/cluster-group-upgrades-operator/controllers/utils"
	ranv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/clustergroupupgrades/v1alpha1"
	"github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/imagebasedgroupupgrades/v1alpha1"
	ibguv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/imagebasedgroupupgrades/v1alpha1"
	lcav1 "github.com/openshift-kni/lifecycle-agent/api/imagebasedupgrade/v1"
	mwv1alpha1 "open-cluster-management.io/api/work/v1alpha1"
)

// IBGUReconciler reconciles a ImageBasedGroupUpgrade object
type IBGUReconciler struct {
	client.Client
	Log      logr.Logger
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

//+kubebuilder:rbac:groups=lcm.openshift.io,resources=imagebasedgroupupgrades,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=lcm.openshift.io,resources=imagebasedgroupupgrades/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=lcm.openshift.io,resources=imagebasedgroupupgrades/finalizers,verbs=update
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.8.3/pkg/reconcile
func (r *IBGUReconciler) Reconcile(ctx context.Context, req ctrl.Request) (nextReconcile ctrl.Result, err error) {
	r.Log.Info("Start reconciling IBGU", "name", req.NamespacedName)
	defer func() {
		if nextReconcile.RequeueAfter > 0 {
			r.Log.Info("Finish reconciling IBGU", "name", req.NamespacedName, "requeueAfter", nextReconcile.RequeueAfter.Seconds())
		} else {
			r.Log.Info("Finish reconciling IBGU", "name", req.NamespacedName, "requeueRightAway", nextReconcile.Requeue)
		}
	}()

	nextReconcile = doNotRequeue()

	ibgu := &ibguv1alpha1.ImageBasedGroupUpgrade{}
	err = r.Get(ctx, req.NamespacedName, ibgu)
	if err != nil {
		if errors.IsNotFound(err) {
			err = nil
			return
		}
		r.Log.Error(err, "Failed to get IBGU")
		return
	}
	err = r.ensureManifests(ctx, ibgu)
	if err != nil {
		r.Log.Error(err, "error ensure manifests")
	}
	utils.SetStatusCondition(&ibgu.Status.Conditions,
		utils.ConditionTypes.ManifestsCreated,
		utils.ConditionReasons.Completed,
		metav1.ConditionTrue,
		"All manifests are created")

	err = r.syncStatusWithCGUs(ctx, ibgu)
	if err != nil {
		r.Log.Error(err, "error syncing status with CGUs")
	}

	// Update status
	err = r.updateStatus(ctx, ibgu)
	return
}

func (r *IBGUReconciler) syncStatusWithCGUs(ctx context.Context, ibgu *ibguv1alpha1.ImageBasedGroupUpgrade) error {
	cguList := &ranv1alpha1.ClusterGroupUpgradeList{}
	err := r.List(ctx, cguList, &client.ListOptions{
		Namespace:     ibgu.Namespace,
		LabelSelector: labels.SelectorFromSet(labels.Set{utils.CGUOwnerIBGULabel: ibgu.Name}),
	})
	if err != nil {
		return fmt.Errorf("failed to get CGUs for the IBGU: %w", err)
	}
	if len(cguList.Items) == 0 {
		return nil
	}
	m := make(map[string]ibguv1alpha1.ClusterState)
	utils.SortCGUListByIBUAction(cguList)
	for _, cgu := range cguList.Items {
		for _, cluster := range cgu.Status.Clusters {
			m[cluster.Name] = ibguv1alpha1.ClusterState{Name: cluster.Name, State: cluster.State}
		}
	}
	for _, cgu := range cguList.Items {
		for cluster, progress := range cgu.Status.Status.CurrentBatchRemediationProgress {
			var currentAction *string
			if progress.ManifestWorkIndex != nil {
				currentAction = &cgu.Spec.ManifestWorkTemplates[*progress.ManifestWorkIndex]
			}
			m[cluster] = ibguv1alpha1.ClusterState{Name: cluster, State: progress.State, CurrentAction: currentAction}
		}
	}
	clusters := make([]ibguv1alpha1.ClusterState, 0, len(m))
	for _, value := range m {
		clusters = append(clusters, value)
	}
	ibgu.Status.Clusters = clusters
	return nil
}

func (r *IBGUReconciler) ensureManifests(ctx context.Context, ibgu *ibguv1alpha1.ImageBasedGroupUpgrade) error {
	manifestWorkReplicaSets := []*mwv1alpha1.ManifestWorkReplicaSet{}
	manifestWorkReplicaSetsNames := []string{}

	permissionsName := "ibu-permissions"
	mwrs, err := utils.GeneratePermissionsManifestWorkReplicaset(permissionsName, ibgu.GetNamespace())
	if err != nil {
		return fmt.Errorf("Error generating manifestworkreplicaset: %w", err)
	}
	manifestWorkReplicaSets = append(manifestWorkReplicaSets, mwrs)
	manifestWorkReplicaSetsNames = append(manifestWorkReplicaSetsNames, permissionsName)

	ibu := &lcav1.ImageBasedUpgrade{
		ObjectMeta: v1.ObjectMeta{
			Name: "upgrade",
		},
		Spec: ibgu.Spec.IBUSpec,
	}
	for _, action := range ibgu.Spec.Actions {
		templateName := ""
		var mwrs *mwv1alpha1.ManifestWorkReplicaSet
		var err error
		switch action {
		case ibguv1alpha1.Prep:
			templateName = strings.ToLower(fmt.Sprintf("%s-%s", ibgu.Name, ibguv1alpha1.Prep))
			mwrs, err = utils.GeneratePrepManifestWorkReplicaset(templateName, ibgu.GetNamespace(), ibu)
		case ibguv1alpha1.Upgrade:
			templateName = strings.ToLower(fmt.Sprintf("%s-%s", ibgu.Name, ibguv1alpha1.Upgrade))
			mwrs, err = utils.GenerateUpgradeManifestWorkReplicaset(templateName, ibgu.GetNamespace(), ibu)
		case ibguv1alpha1.Abort:
			templateName = strings.ToLower(fmt.Sprintf("%s-%s", ibgu.Name, ibguv1alpha1.Abort))
			mwrs, err = utils.GenerateAbortManifestWorkReplicaset(templateName, ibgu.GetNamespace(), ibu)
		case ibguv1alpha1.Finalize:
			templateName = strings.ToLower(fmt.Sprintf("%s-%s", ibgu.Name, ibguv1alpha1.Finalize))
			mwrs, err = utils.GenerateFinalizeManifestWorkReplicaset(templateName, ibgu.GetNamespace(), ibu)
		case ibguv1alpha1.Rollback:
			templateName = strings.ToLower(fmt.Sprintf("%s-%s", ibgu.Name, ibguv1alpha1.Rollback))
			mwrs, err = utils.GenerateRollbackManifestWorkReplicaset(templateName, ibgu.GetNamespace(), ibu)
		}
		if err != nil {
			return fmt.Errorf("Error generating manifestworkreplicaset: %w", err)
		}
		manifestWorkReplicaSets = append(manifestWorkReplicaSets, mwrs)
		manifestWorkReplicaSetsNames = append(manifestWorkReplicaSetsNames, templateName)
	}
	for _, mwrs := range manifestWorkReplicaSets {
		foundMWRS := &mwv1alpha1.ManifestWorkReplicaSet{}
		err := r.Get(ctx, types.NamespacedName{Name: mwrs.Name, Namespace: mwrs.Namespace}, foundMWRS)
		if err != nil && errors.IsNotFound(err) {
			r.Log.Info("Creating ManifestWorkReplicaSet", "ManifestWorkReplicaSet", mwrs.Name)
			ctrl.SetControllerReference(ibgu, mwrs, r.Scheme)
			err = r.Create(ctx, mwrs)
			if err != nil {
				return fmt.Errorf("error creating ManifestWorkReplicaSet: %w", err)
			}
		} else if err != nil {
			return fmt.Errorf("error getting ManifestWorkReplicaSet: %w", err)
		} else {
			// TODO: is it necessary to check if the two mwrs are different
			r.Log.Info("ManifestWorkReplicaSet already exist", "ManifestWorkReplicaSet", mwrs.Name)
		}
	}
	cguList := &ranv1alpha1.ClusterGroupUpgradeList{}
	err = r.List(ctx, cguList, &client.ListOptions{
		Namespace:     ibgu.Namespace,
		LabelSelector: labels.SelectorFromSet(labels.Set{utils.CGUOwnerIBGULabel: ibgu.Name}),
	})
	if err != nil {
		return fmt.Errorf("error listing cgus for ibgu")
	}
	templatesInCGUs := []string{}
	blockingCGUs := []string{}
	for _, cgu := range cguList.Items {
		for _, template := range cgu.Spec.ManifestWorkTemplates {
			templatesInCGUs = append(templatesInCGUs, template)
		}
		blockingCGUs = append(blockingCGUs, cgu.GetName())
	}
	templatesNotInCGUs := utils.Difference(manifestWorkReplicaSetsNames, templatesInCGUs)
	if len(templatesNotInCGUs) == 0 {
		return nil
	}
	cgu := utils.GenerateClusterGroupUpgradeForIBGU(ibgu, templatesNotInCGUs, blockingCGUs)
	r.Log.Info("Creating CGU for IBGU", "ClusterGroupUpgrade", cgu.GetName())
	err = ctrl.SetControllerReference(ibgu, cgu, r.Scheme)
	if err != nil {
		return fmt.Errorf("error setting owner reference for cgu: %w", err)
	}
	err = r.Create(ctx, cgu)
	if err != nil {
		return fmt.Errorf("error creating CGU: %w", err)
	}
	return nil
}

func (r *IBGUReconciler) updateStatus(ctx context.Context, ibgu *ibguv1alpha1.ImageBasedGroupUpgrade) error {
	ibgu.Status.ObservedGeneration = ibgu.ObjectMeta.Generation
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		err := r.Status().Update(ctx, ibgu)
		return err
	})

	if err != nil {
		return err
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *IBGUReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.Recorder = mgr.GetEventRecorderFor("IBGU Reconciler")

	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.ImageBasedGroupUpgrade{}, builder.WithPredicates(predicate.Funcs{
			UpdateFunc: func(e event.UpdateEvent) bool {
				// Generation is only updated on spec changes (also on deletion),
				// not metadata or status
				oldGeneration := e.ObjectOld.GetGeneration()
				newGeneration := e.ObjectNew.GetGeneration()
				// spec update only for SeedGen
				return oldGeneration != newGeneration
			},
			CreateFunc:  func(ce event.CreateEvent) bool { return true },
			GenericFunc: func(ge event.GenericEvent) bool { return false },
			DeleteFunc:  func(de event.DeleteEvent) bool { return false },
		})).
		Owns(&ranv1alpha1.ClusterGroupUpgrade{}).
		WithOptions(controller.Options{MaxConcurrentReconciles: 1}).
		Complete(r)
}
