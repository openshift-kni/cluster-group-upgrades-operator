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
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
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
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	mwv1 "open-cluster-management.io/api/work/v1"
	mwv1alpha1 "open-cluster-management.io/api/work/v1alpha1"
)

// IBGUReconciler reconciles a ImageBasedGroupUpgrade object
type IBGUReconciler struct {
	client.Client
	Log      logr.Logger
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

const LcaAnnotationSuffix = "lca.openshift.io"

//+kubebuilder:rbac:groups=lcm.openshift.io,resources=imagebasedgroupupgrades,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=lcm.openshift.io,resources=imagebasedgroupupgrades/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=lcm.openshift.io,resources=imagebasedgroupupgrades/finalizers,verbs=update
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
//+kubebuilder:rbac:groups=cluster.open-cluster-management.io,resources=managedclusters,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=work.open-cluster-management.io,resources=manifestworkreplicasets,verbs=create;get;list;watch;delete
//+kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.8.3/pkg/reconcile
func (r *IBGUReconciler) Reconcile(ctx context.Context, req ctrl.Request) (nextReconcile ctrl.Result, err error) {
	r.Log.Info("Start reconciling IBGU", "name", req.NamespacedName)
	defer func() {
		r.Log.Info("Finish reconciling IBGU", "name", req.NamespacedName, "requeueAfter", nextReconcile.RequeueAfter.Seconds())
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
		return requeueWithError(err)
	}
	err = r.ensureClusterLabels(ctx, ibgu)
	if err != nil {
		r.Log.Error(err, "error ensuring cluster labels")
		return requeueWithError(err)
	}

	err = r.ensureManifests(ctx, ibgu)
	if err != nil {
		r.Log.Error(err, "error ensure manifests")
		return requeueWithError(err)
	}

	err = r.syncStatusWithCGUs(ctx, ibgu)
	if err != nil {
		r.Log.Error(err, "error syncing status with CGUs")
		return requeueWithError(err)
	}

	err = r.ensureClusterLabels(ctx, ibgu)
	if err != nil {
		r.Log.Error(err, "error ensuring cluster labels")
		return requeueWithError(err)
	}

	err = r.updateStatus(ctx, ibgu)
	if err != nil {
		r.Log.Error(err, "error updating ibgu status")
		return requeueWithError(err)
	}

	return
}

func (r *IBGUReconciler) ensureClusterLabels(ctx context.Context, ibgu *ibguv1alpha1.ImageBasedGroupUpgrade) error {
	failedClusters := []string{}
	for _, clusterState := range ibgu.Status.Clusters {
		labelsToAdd := make(map[string]string)
		for _, action := range clusterState.FailedActions {
			labelsToAdd[fmt.Sprintf(utils.IBGUActionFailedLabelTemplate, strings.ToLower(action.Action))] = ""
		}
		removeLabels := false
		for _, action := range clusterState.CompletedActions {
			if slices.Contains([]string{ibguv1alpha1.AbortOnFailure, ibguv1alpha1.Abort,
				ibguv1alpha1.FinalizeUpgrade, ibguv1alpha1.FinalizeRollback}, action.Action) {
				removeLabels = true
				break
			}
			labelsToAdd[fmt.Sprintf(utils.IBGUActionCompletedLabelTemplate, strings.ToLower(action.Action))] = ""
		}
		if !removeLabels && len(labelsToAdd) == 0 {
			continue
		}
		cluster := &clusterv1.ManagedCluster{}
		if err := r.Get(ctx, types.NamespacedName{Name: clusterState.Name}, cluster); err != nil {
			r.Log.Error(err, "failed to get managed cluster")
			failedClusters = append(failedClusters, clusterState.Name)
			continue
		}
		currentLabels := cluster.GetLabels()
		if currentLabels == nil {
			currentLabels = make(map[string]string)
		}

		needToUpdate := false
		if removeLabels {
			pattern := `lcm\.openshift\.io/ibgu-[a-zA-Z]+-(completed|failed)`
			re := regexp.MustCompile(pattern)
			for key := range currentLabels {
				if re.MatchString(key) {
					needToUpdate = true
					delete(currentLabels, key)
				}
			}
		} else {
			for key, value := range labelsToAdd {
				if _, exist := currentLabels[key]; exist {
					continue
				}
				currentLabels[key] = value
				needToUpdate = true
			}
		}

		if needToUpdate {
			cluster.SetLabels(currentLabels)
			if err := r.Update(ctx, cluster); err != nil {
				r.Log.Error(err, "failed to update labels for cluster")
				failedClusters = append(failedClusters, cluster.Name)
				continue
			}
		}
	}
	if len(failedClusters) == 0 {
		return nil
	}
	return fmt.Errorf("failed to ensure cluster labels for %v", failedClusters)
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
	m := make(map[string]*ibguv1alpha1.ClusterState)
	utils.SortCGUListByPlanIndex(cguList)
	for _, cgu := range cguList.Items {
		for _, cluster := range cgu.Status.Clusters {
			if _, exist := m[cluster.Name]; !exist {
				m[cluster.Name] = &ibguv1alpha1.ClusterState{Name: cluster.Name}
			}
			if cluster.State == utils.ClusterRemediationComplete {
				m[cluster.Name].CompletedActions = append(m[cluster.Name].CompletedActions,
					utils.GetAllActionMessagesFromCGU(&cgu)...)
			} else if cluster.State == utils.ClusterRemediationTimedout {
				if cluster.CurrentManifestWork != nil {
					action := utils.GetActionFromMWRSName(cluster.CurrentManifestWork.Name)
					msg := utils.GetConditionMessageFromManifestWorkStatus(cluster.CurrentManifestWork)
					if msg == "" {
						msg = "Action did not successfully complete before the timeout specified in the rolloutStrategy"
					}
					m[cluster.Name].FailedActions = append(m[cluster.Name].FailedActions,
						ibguv1alpha1.ActionMessage{Action: action, Message: msg})
				}
			}
		}
	}
	for _, cgu := range cguList.Items {
		for clusterName, progress := range cgu.Status.Status.CurrentBatchRemediationProgress {
			if _, exist := m[clusterName]; !exist {
				m[clusterName] = &ibguv1alpha1.ClusterState{Name: clusterName}
			}
			if progress.ManifestWorkIndex != nil {
				m[clusterName].CurrentAction = &ibguv1alpha1.ActionMessage{
					Action: utils.GetActionFromMWRSName(cgu.Spec.ManifestWorkTemplates[*progress.ManifestWorkIndex]),
				}
				m[clusterName].CompletedActions = append(m[clusterName].CompletedActions,
					utils.GetFirstNActionMessagesFromCGU(&cgu, *progress.ManifestWorkIndex)...)
			}
		}
	}
	clusters := make([]ibguv1alpha1.ClusterState, 0, len(m))
	for _, value := range m {
		clusters = append(clusters, *value)
	}
	sort.Slice(clusters, func(i, j int) bool {
		return clusters[i].Name < clusters[j].Name
	})
	ibgu.Status.Clusters = clusters
	return nil
}

func getCGUNameForPlanItem(ibgu *ibguv1alpha1.ImageBasedGroupUpgrade, planItem *ibguv1alpha1.PlanItem, planItemIndex int) string {
	actions := strings.ToLower(strings.Join(planItem.Actions, "-"))
	return fmt.Sprintf("%s-%s-%d", ibgu.GetName(), actions, planItemIndex)
}

func createIBU(ibgu *ibguv1alpha1.ImageBasedGroupUpgrade) *lcav1.ImageBasedUpgrade {
	ibu := &lcav1.ImageBasedUpgrade{
		ObjectMeta: v1.ObjectMeta{
			Name:        "upgrade",
			Annotations: make(map[string]string),
		},
		Spec: ibgu.Spec.IBUSpec,
	}
	for n, v := range ibgu.ObjectMeta.Annotations {
		if strings.Contains(n, LcaAnnotationSuffix) {
			ibu.ObjectMeta.Annotations[n] = v
		}
	}
	return ibu
}

func (r *IBGUReconciler) ensureCGUForPlanItem(
	ctx context.Context,
	ibgu *ibguv1alpha1.ImageBasedGroupUpgrade,
	planItem *ibguv1alpha1.PlanItem, planItemIndex int,
	cguList *ranv1alpha1.ClusterGroupUpgradeList,
) (bool, error) {
	cguName := getCGUNameForPlanItem(ibgu, planItem, planItemIndex)

	for _, cgu := range cguList.Items {
		if cgu.GetName() == cguName {
			completed := utils.IsStatusConditionPresent(cgu.Status.Conditions, string(utils.ConditionTypes.Succeeded))
			r.Log.Info("CGU already exist for plan item", "cgu name", cguName, "completed", completed)
			return completed, nil
		}
	}

	manifestWorkReplicaSets := []*mwv1alpha1.ManifestWorkReplicaSet{}
	manifestWorkReplicaSetsNames := []string{}
	ibu := createIBU(ibgu)
	disableAutoImport := false
	for _, action := range planItem.Actions {
		templateName := ""
		var mwrs *mwv1alpha1.ManifestWorkReplicaSet
		var err error
		switch action {
		case ibguv1alpha1.Prep:
			templateName = strings.ToLower(fmt.Sprintf("%s-%s", ibgu.Name, ibguv1alpha1.Prep))
			manifests := r.getConfigMapManifests(ctx, ibgu)
			secretManifest := r.getSecretManifest(ctx, ibgu)
			if secretManifest != nil {
				manifests = append(manifests, *secretManifest)
			}
			mwrs, err = utils.GeneratePrepManifestWorkReplicaset(templateName, ibgu.GetNamespace(), ibu, manifests)
		case ibguv1alpha1.Upgrade:
			templateName = strings.ToLower(fmt.Sprintf("%s-%s", ibgu.Name, ibguv1alpha1.Upgrade))
			mwrs, err = utils.GenerateUpgradeManifestWorkReplicaset(templateName, ibgu.GetNamespace(), ibu)
			disableAutoImport = true
		case ibguv1alpha1.Abort:
			templateName = strings.ToLower(fmt.Sprintf("%s-%s", ibgu.Name, ibguv1alpha1.Abort))
			mwrs, err = utils.GenerateAbortManifestWorkReplicaset(templateName, ibgu.GetNamespace(), ibu)
		case ibguv1alpha1.AbortOnFailure:
			templateName = strings.ToLower(fmt.Sprintf("%s-%s", ibgu.Name, ibguv1alpha1.AbortOnFailure))
			mwrs, err = utils.GenerateAbortManifestWorkReplicaset(templateName, ibgu.GetNamespace(), ibu)
		case ibguv1alpha1.FinalizeRollback:
			templateName = strings.ToLower(fmt.Sprintf("%s-%s", ibgu.Name, ibguv1alpha1.FinalizeRollback))
			mwrs, err = utils.GenerateFinalizeManifestWorkReplicaset(templateName, ibgu.GetNamespace(), ibu)
		case ibguv1alpha1.FinalizeUpgrade:
			templateName = strings.ToLower(fmt.Sprintf("%s-%s", ibgu.Name, ibguv1alpha1.FinalizeUpgrade))
			mwrs, err = utils.GenerateFinalizeManifestWorkReplicaset(templateName, ibgu.GetNamespace(), ibu)
		case ibguv1alpha1.Rollback:
			templateName = strings.ToLower(fmt.Sprintf("%s-%s", ibgu.Name, ibguv1alpha1.Rollback))
			mwrs, err = utils.GenerateRollbackManifestWorkReplicaset(templateName, ibgu.GetNamespace(), ibu)
		}
		if err != nil {
			return false, fmt.Errorf("Error generating manifestworkreplicaset: %w", err)
		}
		manifestWorkReplicaSets = append(manifestWorkReplicaSets, mwrs)
		manifestWorkReplicaSetsNames = append(manifestWorkReplicaSetsNames, templateName)
	}
	for _, mwrs := range manifestWorkReplicaSets {
		foundMWRS := &mwv1alpha1.ManifestWorkReplicaSet{}
		err := r.Get(ctx, types.NamespacedName{Name: mwrs.Name, Namespace: mwrs.Namespace}, foundMWRS)

		// nolint: gocritic
		if err != nil && errors.IsNotFound(err) {
			r.Log.Info("Creating ManifestWorkReplicaSet", "ManifestWorkReplicaSet", mwrs.Name)
			ctrl.SetControllerReference(ibgu, mwrs, r.Scheme)
			err = r.Create(ctx, mwrs)
			if err != nil {
				return false, fmt.Errorf("error creating ManifestWorkReplicaSet: %w", err)
			}
		} else if err != nil {
			return false, fmt.Errorf("error getting ManifestWorkReplicaSet: %w", err)
		} else {
			// TODO: is it necessary to check if the two mwrs are different
			r.Log.Info("ManifestWorkReplicaSet already exist", "ManifestWorkReplicaSet", mwrs.Name)
		}
	}
	annotations := make(map[string]string)
	if suffix, exists := ibgu.ObjectMeta.Annotations[utils.NameSuffixAnnotation]; exists {
		annotations[utils.NameSuffixAnnotation] = suffix
	}
	cgu := utils.GenerateClusterGroupUpgradeForPlanItem(
		cguName, ibgu, planItem, manifestWorkReplicaSetsNames, annotations, disableAutoImport)
	r.Log.Info("Creating CGU for plan item", "ClusterGroupUpgrade", cgu.GetName())
	err := ctrl.SetControllerReference(ibgu, cgu, r.Scheme)
	if err != nil {
		return false, fmt.Errorf("error setting owner reference for cgu: %w", err)
	}
	err = r.Create(ctx, cgu)
	if err != nil {
		return false, fmt.Errorf("error creating CGU: %w", err)
	}
	return false, nil
}

func (r *IBGUReconciler) ensureManifests(ctx context.Context, ibgu *ibguv1alpha1.ImageBasedGroupUpgrade) error {
	cguList := &ranv1alpha1.ClusterGroupUpgradeList{}
	err := r.List(ctx, cguList, &client.ListOptions{
		Namespace:     ibgu.Namespace,
		LabelSelector: labels.SelectorFromSet(labels.Set{utils.CGUOwnerIBGULabel: ibgu.Name}),
	})
	if err != nil {
		return fmt.Errorf("error listing cgus for ibgu")
	}
	for i, planItem := range ibgu.Spec.Plan {
		completed, err := r.ensureCGUForPlanItem(ctx, ibgu, &planItem, i, cguList)
		if err != nil {
			return fmt.Errorf("error ensuring cgu for plan item: %w", err)
		}
		if !completed {
			r.Log.Info("CGU for plan item is not completed, delay creating next CGUs",
				"planItem actions", planItem.Actions)
			utils.SetStatusCondition(&ibgu.Status.Conditions,
				utils.ConditionTypes.Progressing,
				utils.ConditionReasons.InProgress,
				metav1.ConditionTrue,
				fmt.Sprintf("Waiting for plan step %d to be completed", i))
			return nil
		}
	}

	utils.SetStatusCondition(&ibgu.Status.Conditions,
		utils.ConditionTypes.Progressing,
		utils.ConditionReasons.Completed,
		metav1.ConditionFalse,
		"All plan steps are completed")

	return nil
}

func (r *IBGUReconciler) getSecretManifest(
	ctx context.Context, ibgu *ibguv1alpha1.ImageBasedGroupUpgrade,
) *mwv1.Manifest {
	if ibgu.Spec.IBUSpec.SeedImageRef.PullSecretRef == nil {
		r.Log.Info("No PullSecretRef in IBGU. Skip adding secret to IBGU manifests")
		return nil
	}
	secret := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      ibgu.Spec.IBUSpec.SeedImageRef.PullSecretRef.Name,
		Namespace: ibgu.GetNamespace(),
	}, secret); err != nil {
		r.Log.Info("[WARN] pullsecret does not exist on hub. Skip adding it to manifests, secret name ",
			ibgu.Spec.IBUSpec.SeedImageRef.PullSecretRef.Name, " namespace ", ibgu.GetNamespace())
		return nil
	}
	secret.ResourceVersion = ""
	secret.ObjectMeta.ManagedFields = []metav1.ManagedFieldsEntry{}
	secret.CreationTimestamp = metav1.Time{}
	secret.UID = ""
	secret.Namespace = "openshift-lifecycle-agent"
	secretBytes, err := utils.ObjectToByteArray(secret)
	if err != nil {
		r.Log.Info("[WARN] failed to convert secret to []bytes")
		return nil
	}
	return &mwv1.Manifest{RawExtension: runtime.RawExtension{Raw: secretBytes}}
}

func (r *IBGUReconciler) getConfigMapManifests(ctx context.Context, ibgu *ibguv1alpha1.ImageBasedGroupUpgrade) []mwv1.Manifest {
	manifests := []mwv1.Manifest{}
	for _, cmRef := range ibgu.Spec.IBUSpec.OADPContent {
		cm := &corev1.ConfigMap{}
		if err := r.Get(ctx, types.NamespacedName{
			Name:      cmRef.Name,
			Namespace: cmRef.Namespace,
		}, cm); err != nil {
			r.Log.Info("[WARN] configmap does not exist on hub. Skip adding it to manifests",
				"configmap name", cmRef.Name, "configmap namespace", cmRef.Namespace)
			continue
		}
		cm.ResourceVersion = ""
		cm.ObjectMeta.ManagedFields = []metav1.ManagedFieldsEntry{}
		cm.ObjectMeta.CreationTimestamp = metav1.Time{}
		cm.UID = ""
		cmBytes, err := utils.ObjectToByteArray(cm)
		if err != nil {
			r.Log.Info("[WARN] failed to convert configmap to []bytes",
				"configmap name", cmRef.Name, "configmap namespace", cmRef.Namespace)
			continue
		}
		manifests = append(manifests, mwv1.Manifest{RawExtension: runtime.RawExtension{Raw: cmBytes}})
	}
	return manifests
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
