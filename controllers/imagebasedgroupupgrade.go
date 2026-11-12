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
	"time"

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
	lcabcv1 "github.com/openshift-kni/lifecycle-agent/api/backupcontent/v1"
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
	// SeedImageReader reads the seed format version label from a seed image for the
	// seed-format compatibility pre-flight. It is an interface so tests can inject a
	// fake and avoid real registry egress; when nil the default registry-backed
	// reader is used.
	SeedImageReader utils.SeedImageReader
}

const LcaAnnotationSuffix = "lca.openshift.io"

//+kubebuilder:rbac:groups=lcm.openshift.io,resources=imagebasedgroupupgrades,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=lcm.openshift.io,resources=backupcontents,verbs=get;list;watch
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

	blocked, err := r.ensureManifests(ctx, ibgu)
	if err != nil {
		r.Log.Error(err, "error ensure manifests")
		return requeueWithError(err)
	}

	err = r.updateStatus(ctx, ibgu)
	if err != nil {
		r.Log.Error(err, "error updating ibgu status")
		return requeueWithError(err)
	}

	if blocked {
		// A transient pre-flight failure (an unresolvable reference, an empty cluster
		// set, or an unreadable seed image) is still within its grace window and no CGU
		// exists yet, so nothing the controller watches (the IBGU generation or its
		// owned CGUs) will change if the cause clears. Requeue so the gate is
		// re-evaluated: the IBGU proceeds if the cause clears within the window, or is
		// blocked terminally once it expires. A terminal block sets the Succeeded
		// condition and returns false, so Reconcile does not requeue it.
		nextReconcile = requeueWithMediumInterval()
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

// appendFailedActionOnce records a failed action on the cluster state unless an
// entry for the same action already exists. It guards against a duplicate entry
// when a cluster is seen by both the report-only detection and the timeout path
// within a single reconcile.
func appendFailedActionOnce(state *ibguv1alpha1.ClusterState, failed ibguv1alpha1.ActionMessage) {
	for _, existing := range state.FailedActions {
		if existing.Action == failed.Action {
			return
		}
	}
	state.FailedActions = append(state.FailedActions, failed)
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

				// Report-only: when LCA reports a terminal <Stage>Completed=False on the
				// live ManifestWork, surface it in the IBGU status within the reconcile it
				// is first seen. The CGU state machine only distinguishes complete vs
				// not-complete, so the cluster stays InProgress until its batch timeout;
				// reading the ManifestWork directly is what makes the failure visible early.
				if progress.State == ranv1alpha1.InProgress {
					mw, err := utils.GetManifestWorkForCluster(ctx, r.Client, &cgu, *progress.ManifestWorkIndex, clusterName)
					if err == nil {
						if failed, msg := utils.GetStageFailureFromManifestWork(mw); failed {
							action := utils.GetActionFromMWRSName(cgu.Spec.ManifestWorkTemplates[*progress.ManifestWorkIndex])
							appendFailedActionOnce(m[clusterName], ibguv1alpha1.ActionMessage{Action: action, Message: msg})
						}
					} else if client.IgnoreNotFound(err) != nil {
						// Report-only detection must not block the authoritative status
						// sync: log and continue so a transient ManifestWork read error
						// does not skip the Status.Clusters write and dropped-cluster
						// bookkeeping below. A missing ManifestWork is expected (the stage
						// is simply still being applied) and is ignored silently.
						r.Log.Error(err, "failed to read manifestwork for report-only failure detection",
							"cluster", clusterName)
					}
				}
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
		// A finished IBGU (one carrying the terminal Succeeded condition, including a
		// migrated empty-completed one) is short-circuited in ensureManifests before
		// this runs, so a late-matching ManagedCluster cannot restart a completed,
		// empty rollout. At plan item 0 the base set is therefore resolved live from
		// the selector; the empty-set pre-flight gate has already guaranteed at least
		// one eligible cluster before the plan loop is reached.
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

// planItemResult is the outcome of reconciling a single plan item into its CGU.
type planItemResult int

const (
	// planItemWaiting means the plan item's CGU exists but has not finished, or was
	// just created this reconcile. The plan loop stops and waits.
	planItemWaiting planItemResult = iota
	// planItemCompleted means the plan item is done (its CGU carries the Succeeded
	// condition, or a later plan item legitimately narrowed to no eligible cluster).
	// The plan loop proceeds to the next item.
	planItemCompleted
	// planItemEmptyAtZero means plan item 0 resolved to no eligible cluster at
	// first-CGU creation. This is the empty-set condition, not a completion: the
	// empty-set pre-flight gate passed (at least one cluster was eligible), but the
	// last one was deleted before its CGU could be created (a TOCTOU race). The plan
	// loop must apply the transient-then-terminal empty-set block instead of reporting
	// success, so a one-item plan never records Succeeded=True with no CGU.
	planItemEmptyAtZero
)

func (r *IBGUReconciler) ensureCGUForPlanItem(
	ctx context.Context,
	ibgu *ibguv1alpha1.ImageBasedGroupUpgrade,
	planItem *ibguv1alpha1.PlanItem, planItemIndex int,
	cguList *ranv1alpha1.ClusterGroupUpgradeList,
) (planItemResult, error) {
	cguName := getCGUNameForPlanItem(ibgu, planItem, planItemIndex)

	for _, cgu := range cguList.Items {
		if cgu.GetName() == cguName {
			completed := utils.IsStatusConditionPresent(cgu.Status.Conditions, string(utils.ConditionTypes.Succeeded))
			r.Log.Info("CGU already exist for plan item", "cgu name", cguName, "completed", completed)
			if completed {
				return planItemCompleted, nil
			}
			return planItemWaiting, nil
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
			var manifests []mwv1.Manifest
			manifests, err = r.getConfigMapManifests(ctx, ibgu)
			if err != nil {
				return planItemWaiting, fmt.Errorf("error getting configmap manifests: %w", err)
			}
			var bcManifests []mwv1.Manifest
			bcManifests, err = r.getBackupContentManifests(ctx, ibgu)
			if err != nil {
				return planItemWaiting, fmt.Errorf("error getting backupcontent manifests: %w", err)
			}
			manifests = append(manifests, bcManifests...)
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
			return planItemWaiting, fmt.Errorf("error generating manifestworkreplicaset: %w", err)
		}
		manifestWorkReplicaSets = append(manifestWorkReplicaSets, mwrs)
		manifestWorkReplicaSetsNames = append(manifestWorkReplicaSetsNames, templateName)
	}

	// Resolve the frozen cluster set for this plan item: the base set is frozen at
	// plan item 0, every later item is narrowed from that record and intersected
	// with the ManagedClusters that still exist.
	selected, err := r.resolveClustersForPlanItem(ctx, ibgu, planItem, planItemIndex, cguList)
	if err != nil {
		return planItemWaiting, fmt.Errorf("error resolving clusters for plan item: %w", err)
	}
	if len(selected) == 0 {
		if planItemIndex == 0 {
			// Plan item 0 resolved to no eligible cluster at first-CGU creation. The
			// empty-set pre-flight gate already passed (at least one cluster was
			// eligible), so the last one was deleted in the race between the gate and
			// here. Reporting completion would record Succeeded=True with no CGU for a
			// rollout that never ran; signal the empty-set condition so the plan loop
			// applies the transient-then-terminal empty-set block instead.
			r.Log.Info("Plan item 0 resolved to no eligible cluster at CGU creation",
				"planItem actions", planItem.Actions)
			return planItemEmptyAtZero, nil
		}
		// A later plan item legitimately narrowing to zero (e.g. an AbortOnFailure
		// step when nothing failed, or every frozen cluster has been deleted). There
		// is nothing to remediate, so skip creating a CGU — an empty spec.clusters
		// would be rejected by the CGU controller — and treat the item as completed.
		r.Log.Info("No eligible clusters for plan item, skipping CGU creation",
			"planItem actions", planItem.Actions, "planItemIndex", planItemIndex)
		return planItemCompleted, nil
	}

	for _, mwrs := range manifestWorkReplicaSets {
		foundMWRS := &mwv1alpha1.ManifestWorkReplicaSet{}
		err := r.Get(ctx, types.NamespacedName{Name: mwrs.Name, Namespace: mwrs.Namespace}, foundMWRS)

		// nolint: gocritic
		if err != nil && errors.IsNotFound(err) {
			r.Log.Info("Creating ManifestWorkReplicaSet", "ManifestWorkReplicaSet", mwrs.Name)
			err = ctrl.SetControllerReference(ibgu, mwrs, r.Scheme)
			if err != nil {
				return planItemWaiting, err
			}
			err = r.Create(ctx, mwrs)
			if err != nil {
				return planItemWaiting, fmt.Errorf("error creating ManifestWorkReplicaSet: %w", err)
			}
		} else if err != nil {
			return planItemWaiting, fmt.Errorf("error getting ManifestWorkReplicaSet: %w", err)
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
		return planItemWaiting, fmt.Errorf("error setting owner reference for cgu: %w", err)
	}
	err = r.Create(ctx, cgu)
	if err != nil {
		return planItemWaiting, fmt.Errorf("error creating CGU: %w", err)
	}
	return planItemWaiting, nil
}

// countInProgressStageFailures returns the number of clusters that are still
// progressing an action but have already had an LCA stage failure surfaced via
// the report-only detection for that same action (a FailedActions entry whose
// action matches the cluster's CurrentAction). It drives the augmented
// Progressing message so an observed failure is reflected in the IBGU conditions
// without adding a new condition type. Matching the action avoids counting a
// stale cross-step timeout (e.g. an earlier Prep/Upgrade failure carried into a
// later AbortOnFailure step) as an in-progress failure.
func countInProgressStageFailures(ibgu *ibguv1alpha1.ImageBasedGroupUpgrade) int {
	count := 0
	for _, cs := range ibgu.Status.Clusters {
		if cs.CurrentAction == nil {
			continue
		}
		for _, failed := range cs.FailedActions {
			if failed.Action == cs.CurrentAction.Action {
				count++
				break
			}
		}
	}
	return count
}

// planHasPrep reports whether any plan item contains a Prep action. Plans without
// a Prep item (follow-up Upgrade-only IBGUs) copy nothing to the spoke, so the
// pre-flight reference-resolution gate does not apply to them.
func planHasPrep(ibgu *ibguv1alpha1.ImageBasedGroupUpgrade) bool {
	for i := range ibgu.Spec.Plan {
		if slices.Contains(ibgu.Spec.Plan[i].Actions, ibguv1alpha1.Prep) {
			return true
		}
	}
	return false
}

// resolvePrepReferences resolves every hub-side reference a Prep step copies to
// the spoke: each ibuSpec.backupContent entry against a hub BackupContent in the
// IBGU's namespace, and each ibuSpec.oadpContent entry against a ConfigMap in the
// ref's own namespace. It returns the sorted, human-readable names of the
// references that do not exist. Any error other than NotFound is returned so the
// reconcile requeues rather than wrongly reporting a reference as missing.
func (r *IBGUReconciler) resolvePrepReferences(ctx context.Context, ibgu *ibguv1alpha1.ImageBasedGroupUpgrade) ([]string, error) {
	var missing []string
	for _, bcRef := range ibgu.Spec.IBUSpec.BackupContent {
		bc := &ibguv1alpha1.BackupContent{}
		if err := r.Get(ctx, types.NamespacedName{
			Name:      bcRef.Name,
			Namespace: ibgu.GetNamespace(),
		}, bc); err != nil {
			if errors.IsNotFound(err) {
				missing = append(missing, fmt.Sprintf("BackupContent %q", bcRef.Name))
				continue
			}
			return nil, fmt.Errorf("failed to get BackupContent %q: %w", bcRef.Name, err)
		}
	}
	for _, cmRef := range ibgu.Spec.IBUSpec.OADPContent { //nolint:staticcheck // SA1019: OADPContent is honoured for backward compatibility.
		cm := &corev1.ConfigMap{}
		if err := r.Get(ctx, types.NamespacedName{
			Name:      cmRef.Name,
			Namespace: cmRef.Namespace,
		}, cm); err != nil {
			if errors.IsNotFound(err) {
				missing = append(missing, fmt.Sprintf("ConfigMap %q", cmRef.Namespace+"/"+cmRef.Name))
				continue
			}
			return nil, fmt.Errorf("failed to get ConfigMap %s/%s: %w", cmRef.Namespace, cmRef.Name, err)
		}
	}
	sort.Strings(missing)
	return missing, nil
}

// ensureManifests reconciles the plan into CGUs. Its boolean return means
// "transient — requeue to re-evaluate within the grace window"; a terminal outcome
// records the Succeeded condition and returns false so Reconcile does not requeue.
// It returns an error for any genuine fault.
func (r *IBGUReconciler) ensureManifests(ctx context.Context, ibgu *ibguv1alpha1.ImageBasedGroupUpgrade) (bool, error) {
	cguList := &ranv1alpha1.ClusterGroupUpgradeList{}
	err := r.List(ctx, cguList, &client.ListOptions{
		Namespace:     ibgu.Namespace,
		LabelSelector: labels.SelectorFromSet(labels.Set{utils.CGUOwnerIBGULabel: ibgu.Name}),
	})
	if err != nil {
		return false, fmt.Errorf("error listing cgus for ibgu: %w", err)
	}

	// The Succeeded condition records one of two terminal states, distinguished by
	// reason (design D4):
	//
	//   - A pre-flight block (Succeeded=False with a block reason) is permanent: it is
	//     never cleared by a restart or an appended plan item. The IBGU short-circuits
	//     for good; recovery is delete-and-recreate.
	//   - A plan completion (Succeeded=True/Completed or False/CompletedWithFailures) is
	//     final only for the spec generation it was recorded against, stamped on the
	//     condition's ObservedGeneration. A completion whose ObservedGeneration matches
	//     the current generation short-circuits (a restart or a late-matching
	//     ManagedCluster cannot revive it). A completion recorded against an older
	//     generation means plan items were appended (the plan is the only mutable spec
	//     field), so the IBGU resumes through the plan loop below.
	if succeeded := meta.FindStatusCondition(ibgu.Status.Conditions, string(utils.ConditionTypes.Succeeded)); succeeded != nil {
		if !isPlanCompletion(succeeded.Reason) {
			return false, nil
		}
		if succeeded.ObservedGeneration >= ibgu.Generation {
			return false, nil
		}
		// Plan items were appended after completion: drop Succeeded and resume. Earlier
		// plan items are already finished (their CGUs exist), the pre-flight gates do
		// not run again (CGUs exist), and appended items act on the frozen set recorded
		// in plan item 0's CGU — never a re-resolved selector. setPlanCompleted re-writes
		// Succeeded stamped with the new generation once the appended items finish.
		r.Log.Info("Plan items appended after completion, resuming IBGU", "ibgu", ibgu.GetName(),
			"completionGeneration", succeeded.ObservedGeneration, "currentGeneration", ibgu.Generation)
		r.resumeForAppend(ibgu)
	} else if ibguReportsCompletion(ibgu) {
		// An IBGU that finished under older code carries Progressing reason Completed but
		// no Succeeded condition. Migrate it so it obeys the presence rules above. Run
		// this only when there is no Succeeded condition (the resume path above handles
		// an already-migrated IBGU) so a resumed IBGU is not migrated straight back to a
		// completion.
		//
		//   - No plan-item-0 CGU: it completed empty (no cluster was ever eligible).
		//     Migrate to a permanent empty-set block so a later-matching cluster cannot
		//     revive it after a TALM upgrade (design D2).
		//   - Has CGUs: fall through to the plan loop, which waits on any still-active
		//     owned CGU and re-records the completion through setPlanCompleted stamped
		//     with the current generation (so it stays appendable).
		if !hasPlanItem0CGU(ibgu, cguList) {
			r.Log.Info("Migrating an empty-completed IBGU (no CGU) to a terminal block", "ibgu", ibgu.GetName())
			r.setTerminal(ibgu, utils.ConditionReasons.NoEligibleClusters,
				"this IBGU completed with no CGU (no cluster was eligible for plan item 0); "+
					"it no longer reports success")
			return false, nil
		}
	}

	// Pre-flight gates run once, before the first CGU is created, so an IBGU already
	// in progress (an owned CGU exists) — including one resumed for an appended item —
	// is unaffected. On failure they create no CGU and, per their classification,
	// either retry within the grace window (transient) or block the IBGU terminally.
	if len(cguList.Items) == 0 {
		outcome, err := r.runPreflightGates(ctx, ibgu)
		if err != nil {
			return false, err
		}
		switch outcome.class {
		case preflightTerminal:
			r.setTerminal(ibgu, outcome.reason, outcome.message)
			return false, nil
		case preflightTransient:
			return r.handleTransientPreflight(ibgu, outcome), nil
		case preflightPass:
			// Honour the grace window on the passing path too: even when every gate
			// passes, the first CGU must be created within the window measured from
			// creationTimestamp. If TALM was down past the window and recovered, a
			// now-passing pre-flight must not start a rollout hours late (outside the
			// chosen maintenance window); block it terminally instead. This applies only
			// before the first CGU — appended items (CGUs already exist) are never gated
			// on the window.
			if r.preflightWindowExpired(ibgu) {
				grace := time.Duration(r.preflightRetrySeconds(ibgu)) * time.Second
				r.setTerminal(ibgu, utils.ConditionReasons.PreflightWindowExpired,
					fmt.Sprintf("pre-flight checks passed but the %s grace window since creation has "+
						"expired before the first CGU could be created; the IBGU started too late", grace))
				return false, nil
			}
		}
	}

	for i := range ibgu.Spec.Plan {
		planItem := ibgu.Spec.Plan[i]
		result, err := r.ensureCGUForPlanItem(ctx, ibgu, &planItem, i, cguList)
		if err != nil {
			return false, fmt.Errorf("error ensuring cgu for plan item: %w", err)
		}
		switch result {
		case planItemEmptyAtZero:
			// Plan item 0 resolved to no eligible cluster at CGU creation (the empty-set
			// gate passed, then the last cluster was deleted). Apply the empty-set block:
			// transient within the grace window, terminal after.
			outcome, err := r.ensureNonEmptyClusterSet(ctx, ibgu)
			if err != nil {
				return false, fmt.Errorf("error re-evaluating empty-set gate: %w", err)
			}
			if outcome.class == preflightPass {
				// A cluster re-appeared between the resolution above and here; requeue to
				// create the CGU on the next reconcile.
				return true, nil
			}
			return r.handleTransientPreflight(ibgu, outcome), nil
		case planItemWaiting:
			r.Log.Info("CGU for plan item is not completed, delay creating next CGUs",
				"planItem actions", planItem.Actions)
			msg := fmt.Sprintf("Waiting for plan step %d to be completed", i)
			// Report-only: surface that LCA has already reported a stage failure on
			// in-progress clusters, without introducing a new condition type. The step
			// stays Progressing; the cluster detail lives in Status.Clusters[].failedActions.
			if n := countInProgressStageFailures(ibgu); n > 0 {
				msg = fmt.Sprintf("%s; %d cluster(s) reported a stage failure", msg, n)
			}
			utils.SetStatusCondition(&ibgu.Status.Conditions,
				utils.ConditionTypes.Progressing,
				utils.ConditionReasons.InProgress,
				metav1.ConditionTrue,
				msg)
			return false, nil
		case planItemCompleted:
			// Proceed to the next plan item.
		}
	}

	r.setPlanCompleted(ibgu, cguList)
	return false, nil
}

// isPlanCompletion reports whether a Succeeded condition reason records a plan
// completion (Completed or CompletedWithFailures) rather than a pre-flight block.
// A completion is final only for its generation (appendable); a block is permanent.
func isPlanCompletion(reason string) bool {
	return reason == string(utils.ConditionReasons.Completed) ||
		reason == string(utils.ConditionReasons.CompletedWithFailures)
}

// resumeForAppend drops the completion Succeeded condition and marks the IBGU
// in progress so the plan loop runs the appended plan items. Earlier items keep
// their CGUs, so only the appended items act (on the frozen cluster set).
func (r *IBGUReconciler) resumeForAppend(ibgu *ibguv1alpha1.ImageBasedGroupUpgrade) {
	meta.RemoveStatusCondition(&ibgu.Status.Conditions, string(utils.ConditionTypes.Succeeded))
	utils.SetStatusCondition(&ibgu.Status.Conditions,
		utils.ConditionTypes.Progressing,
		utils.ConditionReasons.InProgress,
		metav1.ConditionTrue,
		"Resuming: plan items were appended after completion")
}

// preflightWindowExpired reports whether the transient pre-flight grace window,
// measured from metadata.creationTimestamp, has elapsed for this IBGU.
func (r *IBGUReconciler) preflightWindowExpired(ibgu *ibguv1alpha1.ImageBasedGroupUpgrade) bool {
	grace := time.Duration(r.preflightRetrySeconds(ibgu)) * time.Second
	return metav1.Now().Sub(ibgu.GetCreationTimestamp().Time) >= grace
}

// runPreflightGates runs the hub-side pre-flight gates in order — empty cluster
// set, then references, then the seed-format (registry) read — and returns the
// first non-pass outcome. Running the hub-only, deterministic checks before the
// network read means a selector typo or a missing BackupContent is reported with
// its own reason and does not trigger a registry read or wait out the grace window
// reporting the wrong reason (review item 4).
func (r *IBGUReconciler) runPreflightGates(ctx context.Context, ibgu *ibguv1alpha1.ImageBasedGroupUpgrade) (preflightOutcome, error) {
	// 1. Empty-set gate (D2): plan item 0 must resolve to at least one eligible cluster.
	if outcome, err := r.ensureNonEmptyClusterSet(ctx, ibgu); err != nil || outcome.class != preflightPass {
		return outcome, err
	}

	// 2. Reference-resolution gate: for a plan that contains a Prep item, every
	// backupContent and oadpContent reference must resolve on the hub. An Upgrade-only
	// follow-up plan (no Prep item) is exempt because it copies nothing — LCA fails
	// such a plan terminally at pre-pivot if a spoke copy is missing. In every plan the
	// CRD admits a Prep item is the first item (see the plan XValidation rule), so this
	// validates the Prep references before any CGU that copies them is created.
	if outcome, err := r.ensurePrepReferencesResolve(ctx, ibgu); err != nil || outcome.class != preflightPass {
		return outcome, err
	}

	// 3. Seed-format compatibility gate (CNF-29242): the network read, run last.
	outcome, err := r.ensureSeedFormatCompatible(ctx, ibgu)
	if err != nil {
		return preflightOutcome{}, fmt.Errorf("error checking seed format compatibility: %w", err)
	}
	return outcome, nil
}

// ensureNonEmptyClusterSet is the empty-set pre-flight gate (design D2): plan item
// 0 must resolve to at least one eligible cluster. It distinguishes two reasons,
// both transient (the cached manager client can return an empty list under
// single-sync GitOps lag, so a short grace absorbs that before the block becomes
// terminal): no ManagedCluster matches the selector (NoClustersMatchSelector), or
// the base set is non-empty but none is eligible for plan item 0's action
// (NoEligibleClusters).
func (r *IBGUReconciler) ensureNonEmptyClusterSet(ctx context.Context, ibgu *ibguv1alpha1.ImageBasedGroupUpgrade) (preflightOutcome, error) {
	base, err := r.resolveBaseClusters(ctx, ibgu)
	if err != nil {
		return preflightOutcome{}, fmt.Errorf("error resolving base clusters: %w", err)
	}
	if len(base) == 0 {
		return transientOutcome(utils.ConditionReasons.NoClustersMatchSelector,
			"no ManagedCluster matches the IBGU clusterLabelSelectors"), nil
	}

	planItem := &ibgu.Spec.Plan[0]
	for _, name := range base {
		cluster := &clusterv1.ManagedCluster{}
		if getErr := r.Get(ctx, types.NamespacedName{Name: name}, cluster); getErr != nil {
			if errors.IsNotFound(getErr) {
				continue
			}
			return preflightOutcome{}, fmt.Errorf("failed to get managed cluster %q: %w", name, getErr)
		}
		if utils.ClusterMatchesPlanItem(cluster.GetLabels(), planItem) {
			return passOutcome(), nil
		}
	}
	return transientOutcome(utils.ConditionReasons.NoEligibleClusters,
		fmt.Sprintf("no cluster is eligible for plan item 0 (actions %v)", planItem.Actions)), nil
}

// ensurePrepReferencesResolve is the reference-resolution pre-flight gate. A
// missing BackupContent/ConfigMap is transient: the cached manager client can
// return NotFound for a reference a single GitOps sync applied alongside the IBGU,
// so a short grace absorbs that lag before the block becomes terminal.
func (r *IBGUReconciler) ensurePrepReferencesResolve(ctx context.Context, ibgu *ibguv1alpha1.ImageBasedGroupUpgrade) (preflightOutcome, error) {
	if !planHasPrep(ibgu) {
		return passOutcome(), nil
	}
	missing, err := r.resolvePrepReferences(ctx, ibgu)
	if err != nil {
		return preflightOutcome{}, fmt.Errorf("error resolving Prep references: %w", err)
	}
	if len(missing) > 0 {
		return transientOutcome(utils.ConditionReasons.UnresolvableDenpendency,
			fmt.Sprintf("unresolvable backup references: %s", strings.Join(missing, ", "))), nil
	}
	return passOutcome(), nil
}

// ibguHasFailedActions reports whether any cluster recorded a failed action in the
// IBGU status, which distinguishes a clean completion (Succeeded=True, Completed)
// from one where at least one cluster failed (Succeeded=False, CompletedWithFailures).
func ibguHasFailedActions(ibgu *ibguv1alpha1.ImageBasedGroupUpgrade) bool {
	for i := range ibgu.Status.Clusters {
		if len(ibgu.Status.Clusters[i].FailedActions) > 0 {
			return true
		}
	}
	return false
}

// anyPlanItemCGUFailed reports whether any owned plan-item CGU ended with a
// terminal Succeeded=False condition. A CGU can fail (e.g. precaching with no
// available cluster, or a batch that times out before any cluster starts) without
// recording a per-cluster failed action, so this is checked alongside
// ibguHasFailedActions when deciding plan completion.
func anyPlanItemCGUFailed(cguList *ranv1alpha1.ClusterGroupUpgradeList) bool {
	for i := range cguList.Items {
		cond := meta.FindStatusCondition(cguList.Items[i].Status.Conditions, string(utils.ConditionTypes.Succeeded))
		if cond != nil && cond.Status == metav1.ConditionFalse {
			return true
		}
	}
	return false
}

// setPlanCompleted records the end of a plan that ran to completion on the
// Succeeded condition:
//
//   - Any cluster recorded a failed action, or any owned plan-item CGU ended
//     Succeeded=False: Succeeded=False reason CompletedWithFailures.
//   - Otherwise: Succeeded=True reason Completed.
//
// The completion is stamped with the current spec generation on the condition's
// ObservedGeneration, so ensureManifests can tell a completion that is final for
// this generation from one that predates an appended plan item (which resumes the
// IBGU). Progressing is mirrored with the same reason so existing consumers of
// Progressing (ACM UI, ZTP, scripts) see the IBGU stopped between steps.
func (r *IBGUReconciler) setPlanCompleted(ibgu *ibguv1alpha1.ImageBasedGroupUpgrade, cguList *ranv1alpha1.ClusterGroupUpgradeList) {
	reason := utils.ConditionReasons.Completed
	status := metav1.ConditionTrue
	msg := "All plan steps are completed"
	if ibguHasFailedActions(ibgu) || anyPlanItemCGUFailed(cguList) {
		reason = utils.ConditionReasons.CompletedWithFailures
		status = metav1.ConditionFalse
		msg = "All plan steps completed; at least one cluster or plan-item CGU reported a failure"
	}
	utils.SetStatusCondition(&ibgu.Status.Conditions, utils.ConditionTypes.Succeeded,
		reason, status, msg)
	// utils.SetStatusCondition does not set the condition's ObservedGeneration; stamp
	// it so the completion is final only for this generation (an appended plan item
	// bumps the generation and resumes the IBGU — see ensureManifests).
	setConditionObservedGeneration(&ibgu.Status.Conditions, utils.ConditionTypes.Succeeded, ibgu.Generation)
	utils.SetStatusCondition(&ibgu.Status.Conditions, utils.ConditionTypes.Progressing,
		reason, metav1.ConditionFalse, msg)
}

// setConditionObservedGeneration stamps the given generation onto an existing
// condition's ObservedGeneration field (utils.SetStatusCondition does not set it).
func setConditionObservedGeneration(conditions *[]metav1.Condition, conditionType utils.ConditionType, generation int64) {
	if cond := meta.FindStatusCondition(*conditions, string(conditionType)); cond != nil {
		cond.ObservedGeneration = generation
	}
}

// setTerminal records a terminal pre-flight block on the Succeeded condition
// (Succeeded=False with the block reason), mirrors it onto Progressing so existing
// consumers see the IBGU stopped, and emits a Warning event. The presence of
// Succeeded makes ensureManifests short-circuit on the next reconcile, so the block
// is durable across a TALM restart or an appended plan item. Recovery is
// delete-and-recreate.
func (r *IBGUReconciler) setTerminal(ibgu *ibguv1alpha1.ImageBasedGroupUpgrade, reason utils.ConditionReason, message string) {
	msg := fmt.Sprintf("Blocked terminally: %s. Recover by deleting and recreating the IBGU.", message)
	r.Log.Info("IBGU blocked terminally, no CGU created", "reason", reason, "message", message)
	utils.SetStatusCondition(&ibgu.Status.Conditions, utils.ConditionTypes.Succeeded,
		reason, metav1.ConditionFalse, msg)
	utils.SetStatusCondition(&ibgu.Status.Conditions, utils.ConditionTypes.Progressing,
		reason, metav1.ConditionFalse, msg)
	if r.Recorder != nil {
		r.Recorder.Event(ibgu, corev1.EventTypeWarning, string(reason), msg)
	}
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

// getConfigMapManifests builds the manifests for the IBGU's oadpContent ConfigMaps.
// It returns an error on any Get or convert failure rather than skipping silently,
// since the pre-flight gate in ensureManifests has already validated every reference
// before this Prep CGU is created (so a failure here is a genuine fault).
func (r *IBGUReconciler) getConfigMapManifests(ctx context.Context, ibgu *ibguv1alpha1.ImageBasedGroupUpgrade) ([]mwv1.Manifest, error) {
	manifests := []mwv1.Manifest{}
	// OADPContent is deprecated in favour of BackupContent (name-only references
	// that LCA resolves on the spoke), but it remains honoured for backward
	// compatibility until LCA removes it. Hub-side BackupContent handling is
	// tracked separately (CNF-29240).
	//
	// In every plan the CRD admits a Prep item is the first plan item, so the
	// pre-flight gate in ensureManifests has already validated every oadpContent
	// reference before this Prep CGU is created. A Get/convert failure here is
	// therefore a genuine fault (e.g. an API error) rather than the expected
	// missing-backup case: return the error instead of silently skipping it.
	for _, cmRef := range ibgu.Spec.IBUSpec.OADPContent { //nolint:staticcheck // SA1019: OADPContent is honoured for backward compatibility.
		cm := &corev1.ConfigMap{}
		if err := r.Get(ctx, types.NamespacedName{
			Name:      cmRef.Name,
			Namespace: cmRef.Namespace,
		}, cm); err != nil {
			return nil, fmt.Errorf("failed to get configmap %s/%s: %w", cmRef.Namespace, cmRef.Name, err)
		}
		cm.ResourceVersion = ""
		cm.ManagedFields = []metav1.ManagedFieldsEntry{}
		cm.CreationTimestamp = metav1.Time{}
		cm.UID = ""
		cmBytes, err := utils.ObjectToByteArray(cm)
		if err != nil {
			return nil, fmt.Errorf("failed to convert configmap %s/%s to []bytes: %w", cmRef.Namespace, cmRef.Name, err)
		}
		manifests = append(manifests, mwv1.Manifest{RawExtension: runtime.RawExtension{Raw: cmBytes}})
	}
	return manifests, nil
}

// getBackupContentManifests builds the spoke BackupContent manifests for the IBGU's
// backupContent references. For each ibuSpec.backupContent entry it resolves the hub
// BackupContent (lcm.openshift.io) of that name in the IBGU's namespace and emits a
// fresh spoke BackupContent (lca.openshift.io) carrying only Name + Namespace and the
// hub entry's spec verbatim. Building a fresh object (rather than mutating the fetched
// one) inherently strips resourceVersion, managedFields, creationTimestamp, uid,
// ownerReferences, generation and any hub labels. The name is the entry name verbatim
// and the namespace is LCA's default install namespace (lcav1.DefaultNamespace); TALM
// does not create that namespace, so a spoke whose LCA is installed elsewhere fails
// loudly at apply time.
//
// As with getConfigMapManifests, a Get/convert failure is returned rather than skipped:
// the pre-flight gate in ensureManifests has already validated every backupContent
// reference before this Prep CGU is created, so a failure here is a genuine fault.
func (r *IBGUReconciler) getBackupContentManifests(ctx context.Context, ibgu *ibguv1alpha1.ImageBasedGroupUpgrade) ([]mwv1.Manifest, error) {
	manifests := []mwv1.Manifest{}
	for _, bcRef := range ibgu.Spec.IBUSpec.BackupContent {
		hubBC := &ibguv1alpha1.BackupContent{}
		if err := r.Get(ctx, types.NamespacedName{
			Name:      bcRef.Name,
			Namespace: ibgu.GetNamespace(),
		}, hubBC); err != nil {
			return nil, fmt.Errorf("failed to get BackupContent %q: %w", bcRef.Name, err)
		}
		spokeBC := &lcabcv1.BackupContent{
			TypeMeta: metav1.TypeMeta{
				APIVersion: lcabcv1.GroupVersion.String(),
				Kind:       "BackupContent",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      bcRef.Name,
				Namespace: lcav1.DefaultNamespace,
			},
			Spec: hubBC.Spec,
		}
		bcBytes, err := utils.ObjectToByteArray(spokeBC)
		if err != nil {
			return nil, fmt.Errorf("failed to convert BackupContent %q to []bytes: %w", bcRef.Name, err)
		}
		manifests = append(manifests, mwv1.Manifest{RawExtension: runtime.RawExtension{Raw: bcBytes}})
	}
	return manifests, nil
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
