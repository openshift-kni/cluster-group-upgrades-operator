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
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	// Sync status from the owned CGUs first so plan-item eligibility in
	// ensureManifests is resolved from the latest CGU status and the
	// ManagedCluster labels it drives. This matters when the final batch of a plan
	// item completes in the same status write that sets the CGU's Succeeded
	// condition: syncing first makes that completion visible to the next plan item
	// in the same reconcile, instead of a reconcile later.
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

	err = r.ensureManifests(ctx, ibgu)
	if err != nil {
		r.Log.Error(err, "error ensure manifests")
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
			if errors.IsNotFound(err) {
				// The cluster was removed mid-rollout. It is reported separately as
				// dropped; skip it here so a vanished cluster does not fail the whole
				// reconcile and block later plan items.
				r.Log.Info("managed cluster no longer exists, skipping label update", "cluster", clusterState.Name)
				continue
			}
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
	// Capture which clusters were already reported as dropped so markDroppedClusters
	// can emit a Warning event only the first time each one is observed as dropped.
	prevDropped := droppedClusterNames(ibgu)

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
			switch cluster.State {
			case utils.ClusterRemediationComplete:
				m[cluster.Name].CompletedActions = append(m[cluster.Name].CompletedActions,
					utils.GetAllActionMessagesFromCGU(&cgu)...)
			case utils.ClusterRemediationTimedout:
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
	return r.markDroppedClusters(ctx, ibgu, cguList, prevDropped)
}

func getCGUNameForPlanItem(ibgu *ibguv1alpha1.ImageBasedGroupUpgrade, planItem *ibguv1alpha1.PlanItem, planItemIndex int) string {
	actions := strings.ToLower(strings.Join(planItem.Actions, "-"))
	return fmt.Sprintf("%s-%s-%d", ibgu.GetName(), actions, planItemIndex)
}

func createIBU(ibgu *ibguv1alpha1.ImageBasedGroupUpgrade) *lcav1.ImageBasedUpgrade {
	ibu := &lcav1.ImageBasedUpgrade{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "upgrade",
			Annotations: make(map[string]string),
		},
		Spec: ibgu.Spec.IBUSpec,
	}
	for n, v := range ibgu.Annotations {
		if strings.Contains(n, LcaAnnotationSuffix) {
			ibu.Annotations[n] = v
		}
	}
	return ibu
}

// droppedClusterReason is recorded on cluster states that were part of the
// frozen cluster set but no longer exist as ManagedClusters.
const droppedClusterReason = "ManagedCluster no longer exists; dropped from rollout"

// resolveBaseClusters returns the names of the ManagedClusters matching the
// IBGU's clusterLabelSelectors. This is the base set that gets frozen at plan
// item 0. Multiple selectors are unioned, mirroring CGU selector semantics.
func (r *IBGUReconciler) resolveBaseClusters(ctx context.Context, ibgu *ibguv1alpha1.ImageBasedGroupUpgrade) ([]string, error) {
	set := make(map[string]struct{})
	for i := range ibgu.Spec.ClusterLabelSelectors {
		selector, err := metav1.LabelSelectorAsSelector(&ibgu.Spec.ClusterLabelSelectors[i])
		if err != nil {
			return nil, fmt.Errorf("invalid cluster label selector: %w", err)
		}
		mcList := &clusterv1.ManagedClusterList{}
		if err := r.List(ctx, mcList, &client.ListOptions{LabelSelector: selector}); err != nil {
			return nil, fmt.Errorf("failed to list managed clusters: %w", err)
		}
		for _, mc := range mcList.Items {
			set[mc.GetName()] = struct{}{}
		}
	}
	names := make([]string, 0, len(set))
	for name := range set {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

// getFrozenClusterSet returns the cluster set frozen at plan item 0, read back
// from plan item 0's CGU spec.clusters. Every later plan item is narrowed from
// this record rather than re-resolving the selector.
func getFrozenClusterSet(ibgu *ibguv1alpha1.ImageBasedGroupUpgrade, cguList *ranv1alpha1.ClusterGroupUpgradeList) []string {
	cguName := getCGUNameForPlanItem(ibgu, &ibgu.Spec.Plan[0], 0)
	for i := range cguList.Items {
		if cguList.Items[i].GetName() == cguName {
			return cguList.Items[i].Spec.Clusters
		}
	}
	// Plan item 0 completed without a CGU because it had no eligible clusters,
	// so the frozen set is empty.
	return nil
}

// hasPlanItem0CGU reports whether plan item 0's CGU already exists in cguList.
func hasPlanItem0CGU(ibgu *ibguv1alpha1.ImageBasedGroupUpgrade, cguList *ranv1alpha1.ClusterGroupUpgradeList) bool {
	cguName := getCGUNameForPlanItem(ibgu, &ibgu.Spec.Plan[0], 0)
	for i := range cguList.Items {
		if cguList.Items[i].GetName() == cguName {
			return true
		}
	}
	return false
}

// ibguReportsCompletion reports whether the IBGU's Progressing condition records
// that all plan steps have completed.
func ibguReportsCompletion(ibgu *ibguv1alpha1.ImageBasedGroupUpgrade) bool {
	cond := meta.FindStatusCondition(ibgu.Status.Conditions, string(utils.ConditionTypes.Progressing))
	return cond != nil && cond.Reason == string(utils.ConditionReasons.Completed)
}

// effectiveActionLabels returns the ManagedCluster labels augmented with the IBGU
// action completed/failed labels implied by this IBGU's freshly synced status for
// the cluster. Plan-item eligibility for this IBGU's own progress is decided from
// both sources: the synced status reflects the final batch even when its
// completion landed in the same write that set the CGU's Succeeded condition
// (before ensureClusterLabels has written the matching labels), while the
// ManagedCluster labels remain the authoritative carrier for plans that span
// IBGUs (e.g. a separate Abort IBGU acting on another IBGU's completed clusters).
func effectiveActionLabels(clusterLabels map[string]string, state *ibguv1alpha1.ClusterState) map[string]string {
	merged := make(map[string]string, len(clusterLabels))
	for k, v := range clusterLabels {
		merged[k] = v
	}
	if state == nil {
		return merged
	}
	// A completed Abort/Finalize clears all IBGU action labels on the
	// ManagedCluster (see ensureClusterLabels). Mirror that clearing here so the
	// synced status does not resurrect action labels the ManagedCluster has had
	// removed: otherwise a cluster that already completed an Abort/Finalize would
	// still carry its earlier failed/completed actions in status and be re-selected
	// by a later plan item (e.g. a second AbortOnFailure re-aborting a cluster that
	// was already aborted for an earlier failure).
	for _, action := range state.CompletedActions {
		if slices.Contains([]string{ibguv1alpha1.AbortOnFailure, ibguv1alpha1.Abort,
			ibguv1alpha1.FinalizeUpgrade, ibguv1alpha1.FinalizeRollback}, action.Action) {
			return merged
		}
	}
	for _, action := range state.CompletedActions {
		merged[fmt.Sprintf(utils.IBGUActionCompletedLabelTemplate, strings.ToLower(action.Action))] = ""
	}
	for _, action := range state.FailedActions {
		merged[fmt.Sprintf(utils.IBGUActionFailedLabelTemplate, strings.ToLower(action.Action))] = ""
	}
	return merged
}

// resolveClustersForPlanItem determines the frozen base set for the plan item
// (resolved live from the selector at plan item 0, read back from plan item 0's
// CGU afterwards), intersects it with the ManagedClusters that still exist, and
// narrows it to those eligible for the plan item. It returns the sorted list of
// clusters the plan item's CGU should target; vanished clusters are reported
// separately each reconcile by markDroppedClusters.
func (r *IBGUReconciler) resolveClustersForPlanItem(
	ctx context.Context,
	ibgu *ibguv1alpha1.ImageBasedGroupUpgrade,
	planItem *ibguv1alpha1.PlanItem, planItemIndex int,
	cguList *ranv1alpha1.ClusterGroupUpgradeList,
) ([]string, error) {
	var frozen []string
	if planItemIndex == 0 {
		// Once the IBGU has reported completion while plan item 0 created no CGU,
		// plan item 0 resolved to zero eligible clusters and the rollout finished
		// empty. Treat that result as final: do not re-resolve the selector, so a
		// ManagedCluster that appears afterwards does not restart a completed, empty
		// rollout.
		if !hasPlanItem0CGU(ibgu, cguList) && ibguReportsCompletion(ibgu) {
			return nil, nil
		}
		var err error
		frozen, err = r.resolveBaseClusters(ctx, ibgu)
		if err != nil {
			return nil, err
		}
	} else {
		frozen = getFrozenClusterSet(ibgu, cguList)
	}

	// Index this IBGU's freshly synced per-cluster status so eligibility can be
	// decided from the completed/failed actions it records as well as the
	// ManagedCluster labels (see effectiveActionLabels).
	statusByName := make(map[string]*ibguv1alpha1.ClusterState, len(ibgu.Status.Clusters))
	for i := range ibgu.Status.Clusters {
		statusByName[ibgu.Status.Clusters[i].Name] = &ibgu.Status.Clusters[i]
	}

	selected := []string{}
	for _, name := range frozen {
		cluster := &clusterv1.ManagedCluster{}
		if getErr := r.Get(ctx, types.NamespacedName{Name: name}, cluster); getErr != nil {
			if errors.IsNotFound(getErr) {
				// Dropped from the rollout; excluded so the CGU is not created with a
				// missing cluster (which validateCR would hard-fail on).
				continue
			}
			return nil, fmt.Errorf("failed to get managed cluster %q: %w", name, getErr)
		}
		if utils.ClusterMatchesPlanItem(effectiveActionLabels(cluster.GetLabels(), statusByName[name]), planItem) {
			selected = append(selected, name)
		}
	}
	sort.Strings(selected)
	return selected, nil
}

// droppedClusterNames returns the set of cluster names currently marked as
// dropped in the IBGU status.
func droppedClusterNames(ibgu *ibguv1alpha1.ImageBasedGroupUpgrade) map[string]bool {
	dropped := make(map[string]bool)
	for _, cs := range ibgu.Status.Clusters {
		if cs.Dropped {
			dropped[cs.Name] = true
		}
	}
	return dropped
}

// markDroppedClusters re-derives, from the frozen set recorded in plan item 0's
// CGU, which clusters no longer exist as ManagedClusters and marks them dropped
// in the IBGU status. It runs every reconcile so the marker is durable (it is not
// lost when syncStatusWithCGUs rebuilds Status.Clusters), and emits a Warning
// event only for clusters not already reported as dropped (prevDropped).
func (r *IBGUReconciler) markDroppedClusters(
	ctx context.Context, ibgu *ibguv1alpha1.ImageBasedGroupUpgrade,
	cguList *ranv1alpha1.ClusterGroupUpgradeList, prevDropped map[string]bool,
) error {
	if len(ibgu.Spec.Plan) == 0 {
		return nil
	}
	cguName := getCGUNameForPlanItem(ibgu, &ibgu.Spec.Plan[0], 0)
	var frozen []string
	found := false
	for i := range cguList.Items {
		if cguList.Items[i].GetName() == cguName {
			frozen = cguList.Items[i].Spec.Clusters
			found = true
			break
		}
	}
	if !found {
		// Plan item 0's CGU does not exist yet, so nothing is frozen.
		return nil
	}

	dropped := []string{}
	for _, name := range frozen {
		cluster := &clusterv1.ManagedCluster{}
		err := r.Get(ctx, types.NamespacedName{Name: name}, cluster)
		if err == nil {
			continue
		}
		if !errors.IsNotFound(err) {
			return fmt.Errorf("failed to get managed cluster %q: %w", name, err)
		}
		dropped = append(dropped, name)
	}
	if len(dropped) == 0 {
		return nil
	}

	index := make(map[string]int, len(ibgu.Status.Clusters))
	for i := range ibgu.Status.Clusters {
		index[ibgu.Status.Clusters[i].Name] = i
	}
	for _, name := range dropped {
		if i, exist := index[name]; exist {
			ibgu.Status.Clusters[i].Dropped = true
			ibgu.Status.Clusters[i].DroppedReason = droppedClusterReason
		} else {
			// A cluster dropped at plan item 0 appears in no CGU status.
			ibgu.Status.Clusters = append(ibgu.Status.Clusters, ibguv1alpha1.ClusterState{
				Name:          name,
				Dropped:       true,
				DroppedReason: droppedClusterReason,
			})
		}
		if !prevDropped[name] {
			r.Log.Info("ManagedCluster dropped from rollout", "cluster", name, "ibgu", ibgu.GetName())
			if r.Recorder != nil {
				r.Recorder.Event(ibgu, corev1.EventTypeWarning, "ClusterDropped",
					fmt.Sprintf("ManagedCluster %q no longer exists and was dropped from the rollout", name))
			}
		}
	}
	sort.Slice(ibgu.Status.Clusters, func(i, j int) bool {
		return ibgu.Status.Clusters[i].Name < ibgu.Status.Clusters[j].Name
	})
	return nil
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
			return false, fmt.Errorf("error generating manifestworkreplicaset: %w", err)
		}
		manifestWorkReplicaSets = append(manifestWorkReplicaSets, mwrs)
		manifestWorkReplicaSetsNames = append(manifestWorkReplicaSetsNames, templateName)
	}

	// Resolve the frozen cluster set for this plan item: the base set is frozen at
	// plan item 0, every later item is narrowed from that record and intersected
	// with the ManagedClusters that still exist.
	selected, err := r.resolveClustersForPlanItem(ctx, ibgu, planItem, planItemIndex, cguList)
	if err != nil {
		return false, fmt.Errorf("error resolving clusters for plan item: %w", err)
	}
	if len(selected) == 0 {
		// No cluster is eligible for this plan item (e.g. an AbortOnFailure step
		// when nothing failed, or every frozen cluster has been deleted). There is
		// nothing to remediate, so skip creating a CGU — an empty spec.clusters
		// would be rejected by the CGU controller — and treat the item as completed.
		r.Log.Info("No eligible clusters for plan item, skipping CGU creation",
			"planItem actions", planItem.Actions, "planItemIndex", planItemIndex)
		return true, nil
	}

	for _, mwrs := range manifestWorkReplicaSets {
		foundMWRS := &mwv1alpha1.ManifestWorkReplicaSet{}
		err := r.Get(ctx, types.NamespacedName{Name: mwrs.Name, Namespace: mwrs.Namespace}, foundMWRS)

		// nolint: gocritic
		if err != nil && errors.IsNotFound(err) {
			r.Log.Info("Creating ManifestWorkReplicaSet", "ManifestWorkReplicaSet", mwrs.Name)
			err = ctrl.SetControllerReference(ibgu, mwrs, r.Scheme)
			if err != nil {
				return false, err
			}
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
	if suffix, exists := ibgu.Annotations[utils.NameSuffixAnnotation]; exists {
		annotations[utils.NameSuffixAnnotation] = suffix
	}
	cgu := utils.GenerateClusterGroupUpgradeForPlanItem(
		cguName, ibgu, planItem, manifestWorkReplicaSetsNames, selected, annotations, disableAutoImport)
	r.Log.Info("Creating CGU for plan item", "ClusterGroupUpgrade", cgu.GetName())
	err = ctrl.SetControllerReference(ibgu, cgu, r.Scheme)
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
	secret.ManagedFields = []metav1.ManagedFieldsEntry{}
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
		cm.ManagedFields = []metav1.ManagedFieldsEntry{}
		cm.CreationTimestamp = metav1.Time{}
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
	ibgu.Status.ObservedGeneration = ibgu.Generation
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
	r.Recorder = mgr.GetEventRecorderFor("IBGU Reconciler") //nolint: staticcheck

	return ctrl.NewControllerManagedBy(mgr).
		For(&ibguv1alpha1.ImageBasedGroupUpgrade{}, builder.WithPredicates(predicate.Funcs{
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
