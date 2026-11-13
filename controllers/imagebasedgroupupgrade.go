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
	// APIReader is the manager's uncached client (mgr.GetAPIReader()). The pre-flight
	// reads — ManagedClusters, BackupContent/ConfigMap references, and the seed pull
	// secret — go through it so informer-cache lag cannot produce a false terminal
	// block (every pre-flight failure is now terminal at once; CNF-30176). When nil
	// the cached Client is used, so tests that wire only Client keep working.
	APIReader client.Reader
	// SeedImageReader reads the seed format version label from a seed image for the
	// seed-format compatibility pre-flight. It is an interface so tests can inject a
	// fake and avoid real registry egress; when nil the default registry-backed
	// reader is used.
	SeedImageReader utils.SeedImageReader
}

// preflightReader returns the client used for pre-flight reads: the manager's
// uncached API reader when set, so a resource applied alongside the IBGU (for
// example in the same GitOps sync) is not read as missing before the informer cache
// catches up and wrongly blocked. It falls back to the cached Client when APIReader
// is nil (tests that wire only Client).
func (r *IBGUReconciler) preflightReader() client.Reader {
	if r.APIReader != nil {
		return r.APIReader
	}
	return r.Client
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

	// The pre-flight never asks for a requeue: plan item 0's eligible cluster set is
	// resolved once (by the empty-set gate) and the first CGU is created from that same
	// set, so the gate and the CGU can never disagree and there is no empty-set race to
	// re-check. Every pre-flight failure is terminal (sets Succeeded, no requeue); the
	// plan loop otherwise waits on owned-CGU watches. A genuine API fault returns an
	// error above and requeues with backoff, still bounded by the start deadline.
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
	cguList := &ranv1alpha1.ClusterGroupUpgradeList{}
	err := r.List(ctx, cguList, &client.ListOptions{
		Namespace:     ibgu.Namespace,
		LabelSelector: labels.SelectorFromSet(labels.Set{utils.CGUOwnerIBGULabel: ibgu.Name}),
	})
	if err != nil {
		return fmt.Errorf("failed to get CGUs for the IBGU: %w", err)
	}
	return r.syncStatusFromCGUList(ctx, ibgu, cguList)
}

// syncStatusFromCGUList rebuilds Status.Clusters from the supplied CGU snapshot
// rather than listing the CGUs itself. ensureManifests calls it with the same CGU
// list it uses to advance the plan, so plan-item eligibility and the completion
// record are decided from one snapshot: without this, a one-cluster plan item that
// completes between Reconcile's status sync and ensureManifests' own CGU list would
// let the plan loop see that item finished (its snapshot) while the next item is
// evaluated against stale status — the next item resolves empty, is skipped, and
// generation-final completion records a false success it can never undo.
func (r *IBGUReconciler) syncStatusFromCGUList(
	ctx context.Context, ibgu *ibguv1alpha1.ImageBasedGroupUpgrade,
	cguList *ranv1alpha1.ClusterGroupUpgradeList,
) error {
	// Capture which clusters were already reported as dropped so markDroppedClusters
	// can emit a Warning event only the first time each one is observed as dropped.
	prevDropped := droppedClusterNames(ibgu)

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
// item 0. Multiple selectors are unioned, mirroring CGU selector semantics. The
// reader lets the caller choose the cached client (for appended plan items, once a
// CGU exists) or the uncached pre-flight reader (before the first CGU, so cache lag
// cannot drop a matching cluster and cause a false empty-set block).
func (r *IBGUReconciler) resolveBaseClusters(ctx context.Context, reader client.Reader, ibgu *ibguv1alpha1.ImageBasedGroupUpgrade) ([]string, error) {
	set := make(map[string]struct{})
	for i := range ibgu.Spec.ClusterLabelSelectors {
		selector, err := metav1.LabelSelectorAsSelector(&ibgu.Spec.ClusterLabelSelectors[i])
		if err != nil {
			return nil, fmt.Errorf("invalid cluster label selector: %w", err)
		}
		mcList := &clusterv1.ManagedClusterList{}
		if err := reader.List(ctx, mcList, &client.ListOptions{LabelSelector: selector}); err != nil {
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

// missingPlanItemCGUs reports the plan items that have no owned CGU but precede the
// highest plan index that does. When any owned CGU exists, every plan item up to the
// highest one that has a CGU must also have a CGU — the controller creates them in
// order and never deletes an owned CGU, so a gap means that item's CGU was deleted out
// of band. Plan items beyond the highest existing CGU have simply not been created yet
// and are not reported. Each missing item is named "N (actions)" for the block message.
func missingPlanItemCGUs(ibgu *ibguv1alpha1.ImageBasedGroupUpgrade, cguList *ranv1alpha1.ClusterGroupUpgradeList) []string {
	present := make(map[string]bool, len(cguList.Items))
	for i := range cguList.Items {
		present[cguList.Items[i].GetName()] = true
	}
	has := make([]bool, len(ibgu.Spec.Plan))
	highest := -1
	for i := range ibgu.Spec.Plan {
		if present[getCGUNameForPlanItem(ibgu, &ibgu.Spec.Plan[i], i)] {
			has[i] = true
			highest = i
		}
	}
	var missing []string
	for i := 0; i < highest; i++ {
		if !has[i] {
			missing = append(missing, fmt.Sprintf("%d (%s)", i, strings.Join(ibgu.Spec.Plan[i].Actions, ", ")))
		}
	}
	return missing
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

// resolveClustersForPlanItem determines the cluster set for an appended plan item
// (index > 0). The base set was frozen at plan item 0 and is read back from plan
// item 0's CGU on the cached client; this intersects it with the ManagedClusters
// that still exist and narrows it to those eligible for the plan item. It returns
// the sorted list of clusters the plan item's CGU should target; vanished clusters
// are reported separately each reconcile by markDroppedClusters.
//
// Plan item 0 does not go through here: its eligible set is resolved once by the
// empty-set pre-flight gate (through the uncached reader) and passed straight to
// ensureCGUForPlanItem, so the gate and the first CGU can never disagree (CNF-30176).
func (r *IBGUReconciler) resolveClustersForPlanItem(
	ctx context.Context,
	ibgu *ibguv1alpha1.ImageBasedGroupUpgrade,
	planItem *ibguv1alpha1.PlanItem,
	cguList *ranv1alpha1.ClusterGroupUpgradeList,
) ([]string, error) {
	frozen := getFrozenClusterSet(ibgu, cguList)

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
)

// ensureCGUForPlanItem reconciles a single plan item into its CGU. For plan item 0
// it targets item0Clusters — the eligible set the empty-set pre-flight gate resolved
// once (guaranteed non-empty) — rather than re-resolving the selector, so the gate
// and the first CGU can never disagree (CNF-30176). item0Clusters is consulted only
// when plan item 0's CGU does not yet exist; appended items (index > 0) resolve their
// set from the frozen record in plan item 0's CGU.
func (r *IBGUReconciler) ensureCGUForPlanItem(
	ctx context.Context,
	ibgu *ibguv1alpha1.ImageBasedGroupUpgrade,
	planItem *ibguv1alpha1.PlanItem, planItemIndex int,
	cguList *ranv1alpha1.ClusterGroupUpgradeList,
	item0Clusters []string,
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
			var secretManifest *mwv1.Manifest
			secretManifest, err = r.getSecretManifest(ctx, ibgu)
			if err != nil {
				return planItemWaiting, fmt.Errorf("error getting secret manifest: %w", err)
			}
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

	// Resolve the cluster set for this plan item. Plan item 0 reuses the eligible set
	// the empty-set gate already resolved (item0Clusters), so the gate and the first
	// CGU target exactly the same clusters. Appended items narrow the set frozen at
	// plan item 0, intersected with the ManagedClusters that still exist.
	var selected []string
	if planItemIndex == 0 {
		selected = item0Clusters
	} else {
		var err error
		selected, err = r.resolveClustersForPlanItem(ctx, ibgu, planItem, cguList)
		if err != nil {
			return planItemWaiting, fmt.Errorf("error resolving clusters for plan item: %w", err)
		}
	}
	if len(selected) == 0 {
		// Plan item 0 cannot reach here: the empty-set gate guaranteed item0Clusters is
		// non-empty. A later plan item legitimately narrowing to zero (e.g. an
		// AbortOnFailure step when nothing failed, or every frozen cluster has been
		// deleted) has nothing to remediate, so skip creating a CGU — an empty
		// spec.clusters would be rejected by the CGU controller — and treat the item as
		// completed.
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
	err := ctrl.SetControllerReference(ibgu, cgu, r.Scheme)
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
// IBGU's namespace, each ibuSpec.oadpContent entry against a ConfigMap in the ref's
// own namespace, and — when seedImageRef.pullSecretRef is set — the pull secret in
// the IBGU's namespace. It returns the sorted, human-readable names of the references
// that do not exist. A pullSecretRef that is set but names a missing secret is a
// misconfiguration reported here (terminally) so it is caught for every Prep plan,
// not only backupContent ones the seed gate happens to read; a plan with no
// pullSecretRef copies no secret and is exempt. Any error other than NotFound is
// returned so the reconcile requeues rather than wrongly reporting a reference as
// missing. The reader is the uncached pre-flight reader so a reference applied
// alongside the IBGU is not read as NotFound under informer-cache lag.
func (r *IBGUReconciler) resolvePrepReferences(ctx context.Context, reader client.Reader, ibgu *ibguv1alpha1.ImageBasedGroupUpgrade) ([]string, error) {
	var missing []string
	for _, bcRef := range ibgu.Spec.IBUSpec.BackupContent {
		bc := &ibguv1alpha1.BackupContent{}
		if err := reader.Get(ctx, types.NamespacedName{
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
		if err := reader.Get(ctx, types.NamespacedName{
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
	if pullSecretRef := ibgu.Spec.IBUSpec.SeedImageRef.PullSecretRef; pullSecretRef != nil {
		secret := &corev1.Secret{}
		if err := reader.Get(ctx, types.NamespacedName{
			Name:      pullSecretRef.Name,
			Namespace: ibgu.GetNamespace(),
		}, secret); err != nil {
			if errors.IsNotFound(err) {
				missing = append(missing, fmt.Sprintf("Secret %q", pullSecretRef.Name))
			} else {
				return nil, fmt.Errorf("failed to get pull secret %q: %w", pullSecretRef.Name, err)
			}
		}
	}
	sort.Strings(missing)
	return missing, nil
}

// ensureManifests reconciles the plan into CGUs. Every pre-flight failure is terminal
// at once (records the Succeeded condition, creates no CGU); it never requeues. It
// returns an error only for a genuine fault, which Reconcile requeues with backoff
// (still bounded by the start deadline).
func (r *IBGUReconciler) ensureManifests(ctx context.Context, ibgu *ibguv1alpha1.ImageBasedGroupUpgrade) error {
	cguList := &ranv1alpha1.ClusterGroupUpgradeList{}
	err := r.List(ctx, cguList, &client.ListOptions{
		Namespace:     ibgu.Namespace,
		LabelSelector: labels.SelectorFromSet(labels.Set{utils.CGUOwnerIBGULabel: ibgu.Name}),
	})
	if err != nil {
		return fmt.Errorf("error listing cgus for ibgu: %w", err)
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
			return nil
		}
		if succeeded.ObservedGeneration >= ibgu.Generation {
			return nil
		}
		// Plan items were appended after completion: drop Succeeded and resume. Earlier
		// plan items are already finished (their CGUs exist), the pre-flight gates do
		// not run again (CGUs exist), and appended items act on the frozen set recorded
		// in plan item 0's CGU — never a re-resolved selector. setPlanCompleted re-writes
		// Succeeded stamped with the new generation once the appended items finish.
		r.Log.Info("Plan items appended after completion, resuming IBGU", "ibgu", ibgu.GetName(),
			"completionGeneration", succeeded.ObservedGeneration, "currentGeneration", ibgu.Generation)
		r.resumeForAppend(ibgu)
	}

	// item0Clusters is plan item 0's eligible cluster set, resolved once by the
	// empty-set gate (through the uncached reader) and reused verbatim when plan item
	// 0's CGU is created, so the gate and the first CGU can never disagree about which
	// clusters are eligible. It is populated only on the first-CGU path below; for an
	// IBGU already in progress (an owned CGU exists) plan item 0's CGU already exists,
	// so this set is never consulted.
	var item0Clusters []string

	// Pre-flight gates run once, before the first CGU is created, so an IBGU already
	// in progress (an owned CGU exists) — including one resumed for an appended item —
	// is unaffected. On failure they create no CGU and block the IBGU terminally at
	// once: every pre-flight failure is terminal (CNF-30176), with no retry.
	//
	// The cached list above can lag behind an owned CGU this controller just created.
	// Before any terminal check, re-read owned CGUs through the uncached reader when the
	// cached list would trip one — it is empty, or it shows a gap in the plan's CGU
	// sequence (see the gap gate below). A lag-free, gap-free list needs no confirmation,
	// so an in-progress IBGU reconciles without the extra API read. Confirming uncached
	// means an empty list really is empty (the first CGU has not raced the informer) and
	// a sequence gap really is an out-of-band deletion, not cache lag, so neither records
	// a false permanent block on an IBGU whose CGUs already exist (CNF-30176).
	if len(cguList.Items) == 0 || len(missingPlanItemCGUs(ibgu, cguList)) > 0 {
		uncachedCGUList := &ranv1alpha1.ClusterGroupUpgradeList{}
		if listErr := r.preflightReader().List(ctx, uncachedCGUList, &client.ListOptions{
			Namespace:     ibgu.Namespace,
			LabelSelector: labels.SelectorFromSet(labels.Set{utils.CGUOwnerIBGULabel: ibgu.Name}),
		}); listErr != nil {
			return fmt.Errorf("error listing cgus for ibgu through the uncached reader: %w", listErr)
		}
		cguList = uncachedCGUList
	}

	// Gap gate (confirmed uncached above): an owned CGU exists but the CGU for an earlier
	// plan item is missing. The controller creates plan-item CGUs in order and never
	// deletes an owned one, so this is an out-of-band deletion. Block terminally and
	// create nothing — never report completion: plan item 0's CGU carries the frozen
	// cluster set, which is unrecoverable once gone, and a missing later item's CGU would
	// otherwise be silently recreated and re-run against current labels rather than the
	// frozen set. Recovery is delete-and-recreate.
	if len(cguList.Items) > 0 {
		if missing := missingPlanItemCGUs(ibgu, cguList); len(missing) > 0 {
			r.setTerminal(ibgu, utils.ConditionReasons.PlanCGUMissing,
				fmt.Sprintf("owned CGUs exist but the CGU(s) for earlier plan item(s) %s are missing "+
					"(deleted out of band); the IBGU cannot safely resume because plan item 0's frozen "+
					"cluster set may be unrecoverable", strings.Join(missing, "; ")))
			return nil
		}
	}

	if len(cguList.Items) == 0 {
		var outcome preflightOutcome
		item0Clusters, outcome, err = r.runPreflightGates(ctx, ibgu)
		if err != nil {
			return err
		}
		switch outcome.class {
		case preflightTerminal:
			r.setTerminal(ibgu, outcome.reason, outcome.message)
			return nil
		case preflightPass:
			// Honour the start deadline on the passing path too: even when every gate
			// passes, the first CGU must be created within preflightStartDeadline of
			// creationTimestamp. If TALM was down past that window and recovered, a
			// now-passing pre-flight must not start a rollout hours late (outside the
			// chosen maintenance window); block it terminally instead. This applies only
			// before the first CGU — appended items (CGUs already exist) are never gated
			// on the deadline.
			if metav1.Now().Sub(ibgu.GetCreationTimestamp().Time) >= preflightStartDeadline {
				r.setTerminal(ibgu, utils.ConditionReasons.PreflightWindowExpired,
					fmt.Sprintf("pre-flight checks passed but the %s start deadline since creation has "+
						"expired before the first CGU could be created; the IBGU started too late", preflightStartDeadline))
				return nil
			}
		}
	}

	// Re-sync status from this same CGU snapshot before evaluating plan-item
	// eligibility, so eligibility and the completion recorded at the end of the loop
	// come from one snapshot. Reconcile already synced from an earlier list; re-syncing
	// here closes the window where a one-cluster plan item completes between the two
	// reads, which would otherwise make the next plan item resolve empty against stale
	// status, be skipped, and record a generation-final false success.
	if err := r.syncStatusFromCGUList(ctx, ibgu, cguList); err != nil {
		return fmt.Errorf("error syncing status from cgu list: %w", err)
	}

	for i := range ibgu.Spec.Plan {
		planItem := ibgu.Spec.Plan[i]
		result, err := r.ensureCGUForPlanItem(ctx, ibgu, &planItem, i, cguList, item0Clusters)
		if err != nil {
			return fmt.Errorf("error ensuring cgu for plan item: %w", err)
		}
		switch result {
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
			return nil
		case planItemCompleted:
			// Proceed to the next plan item.
		}
	}

	r.setPlanCompleted(ibgu, cguList)
	return nil
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

// runPreflightGates runs the hub-side pre-flight gates in order — empty cluster
// set, then references, then the seed-format (registry) read — and returns the
// first non-pass outcome. Running the hub-only, deterministic checks before the
// network read means a selector typo or a missing BackupContent is reported with
// its own reason and does not trigger a registry read or block with the wrong
// reason (review item 4). On the passing path it also returns plan item 0's
// eligible cluster set, resolved once by the empty-set gate, so the first CGU is
// created from that same set.
func (r *IBGUReconciler) runPreflightGates(ctx context.Context, ibgu *ibguv1alpha1.ImageBasedGroupUpgrade) ([]string, preflightOutcome, error) {
	// 1. Empty-set gate (D2): plan item 0 must resolve to at least one eligible
	// cluster. Its resolved eligible set is returned for reuse as the first CGU's
	// cluster set.
	item0Clusters, outcome, err := r.ensureNonEmptyClusterSet(ctx, ibgu)
	if err != nil || outcome.class != preflightPass {
		return nil, outcome, err
	}

	// 2. Reference-resolution gate: for a plan that contains a Prep item, every
	// backupContent and oadpContent reference must resolve on the hub. An Upgrade-only
	// follow-up plan (no Prep item) is exempt because it copies nothing — LCA fails
	// such a plan terminally at pre-pivot if a spoke copy is missing. In every plan the
	// CRD admits a Prep item is the first item (see the plan XValidation rule), so this
	// validates the Prep references before any CGU that copies them is created.
	if outcome, err := r.ensurePrepReferencesResolve(ctx, ibgu); err != nil || outcome.class != preflightPass {
		return nil, outcome, err
	}

	// 3. Seed-format compatibility gate (CNF-29242): the network read, run last.
	outcome, err = r.ensureSeedFormatCompatible(ctx, ibgu)
	if err != nil {
		return nil, preflightOutcome{}, fmt.Errorf("error checking seed format compatibility: %w", err)
	}
	return item0Clusters, outcome, nil
}

// ensureNonEmptyClusterSet is the empty-set pre-flight gate (design D2): it resolves
// plan item 0's eligible cluster set once, through the uncached pre-flight reader, and
// returns that set so the first CGU is created from the very set the gate evaluated —
// the gate and the CGU can never disagree, so there is no empty-set race to re-check
// (CNF-30176). When the set is empty it distinguishes two terminal reasons: no
// ManagedCluster matches the selector (NoClustersMatchSelector), or the base set is
// non-empty but none is eligible for plan item 0's action (NoEligibleClusters).
// Reading through the uncached reader means a ManagedCluster or label applied
// alongside the IBGU is not missed under informer-cache lag and wrongly reported as
// empty. The returned set is meaningful only on a passing outcome.
func (r *IBGUReconciler) ensureNonEmptyClusterSet(ctx context.Context, ibgu *ibguv1alpha1.ImageBasedGroupUpgrade) ([]string, preflightOutcome, error) {
	reader := r.preflightReader()
	base, err := r.resolveBaseClusters(ctx, reader, ibgu)
	if err != nil {
		return nil, preflightOutcome{}, fmt.Errorf("error resolving base clusters: %w", err)
	}
	if len(base) == 0 {
		return nil, terminalOutcome(utils.ConditionReasons.NoClustersMatchSelector,
			"no ManagedCluster matches the IBGU clusterLabelSelectors"), nil
	}

	// Index this IBGU's freshly synced per-cluster status so eligibility is decided
	// from the completed/failed actions it records as well as the ManagedCluster labels
	// (see effectiveActionLabels) — the same inputs the plan loop uses, so the set
	// returned here is exactly the set plan item 0's CGU is created from. On the first
	// reconcile no action has completed yet, so this reduces to the raw labels.
	statusByName := make(map[string]*ibguv1alpha1.ClusterState, len(ibgu.Status.Clusters))
	for i := range ibgu.Status.Clusters {
		statusByName[ibgu.Status.Clusters[i].Name] = &ibgu.Status.Clusters[i]
	}

	planItem := &ibgu.Spec.Plan[0]
	eligible := []string{}
	for _, name := range base {
		cluster := &clusterv1.ManagedCluster{}
		if getErr := reader.Get(ctx, types.NamespacedName{Name: name}, cluster); getErr != nil {
			if errors.IsNotFound(getErr) {
				continue
			}
			return nil, preflightOutcome{}, fmt.Errorf("failed to get managed cluster %q: %w", name, getErr)
		}
		if utils.ClusterMatchesPlanItem(effectiveActionLabels(cluster.GetLabels(), statusByName[name]), planItem) {
			eligible = append(eligible, name)
		}
	}
	if len(eligible) == 0 {
		return nil, terminalOutcome(utils.ConditionReasons.NoEligibleClusters,
			fmt.Sprintf("no cluster is eligible for plan item 0 (actions %v)", planItem.Actions)), nil
	}
	sort.Strings(eligible)
	return eligible, passOutcome(), nil
}

// ensurePrepReferencesResolve is the reference-resolution pre-flight gate. A
// missing BackupContent/ConfigMap is terminal. It reads through the uncached
// pre-flight reader so a reference applied alongside the IBGU in the same GitOps
// sync is not read as NotFound under informer-cache lag and wrongly blocked.
func (r *IBGUReconciler) ensurePrepReferencesResolve(ctx context.Context, ibgu *ibguv1alpha1.ImageBasedGroupUpgrade) (preflightOutcome, error) {
	if !planHasPrep(ibgu) {
		return passOutcome(), nil
	}
	missing, err := r.resolvePrepReferences(ctx, r.preflightReader(), ibgu)
	if err != nil {
		return preflightOutcome{}, fmt.Errorf("error resolving Prep references: %w", err)
	}
	if len(missing) > 0 {
		return terminalOutcome(utils.ConditionReasons.UnresolvableDenpendency,
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

// getSecretManifest builds the spoke pull-secret manifest for the IBGU's
// seedImageRef.pullSecretRef. A plan with no pullSecretRef has nothing to copy and
// returns (nil, nil). Otherwise the secret is read through the uncached pre-flight
// reader during the first (Prep) CGU creation, in the same reconcile the reference
// gate resolved it, so cache lag cannot turn a just-resolved secret into a NotFound
// here. Because the reference gate has already established the secret exists
// (CNF-30176), any read failure — including NotFound — and any serialization failure
// is a genuine fault and is returned as an error rather than silently dropped: a
// dropped secret would create the Prep ManifestWorkReplicaSet (written once, never
// updated) without the credentials a private seed image needs, on every batch of the
// IBGU. This mirrors getConfigMapManifests.
func (r *IBGUReconciler) getSecretManifest(
	ctx context.Context, ibgu *ibguv1alpha1.ImageBasedGroupUpgrade,
) (*mwv1.Manifest, error) {
	if ibgu.Spec.IBUSpec.SeedImageRef.PullSecretRef == nil {
		r.Log.Info("No PullSecretRef in IBGU. Skip adding secret to IBGU manifests")
		return nil, nil
	}
	secretName := ibgu.Spec.IBUSpec.SeedImageRef.PullSecretRef.Name
	secret := &corev1.Secret{}
	if err := r.preflightReader().Get(ctx, types.NamespacedName{
		Name:      secretName,
		Namespace: ibgu.GetNamespace(),
	}, secret); err != nil {
		return nil, fmt.Errorf("failed to get pull secret %q: %w", secretName, err)
	}
	secret.ResourceVersion = ""
	secret.ManagedFields = []metav1.ManagedFieldsEntry{}
	secret.CreationTimestamp = metav1.Time{}
	secret.UID = ""
	secret.Namespace = lcav1.DefaultNamespace
	secretBytes, err := utils.ObjectToByteArray(secret)
	if err != nil {
		return nil, fmt.Errorf("failed to convert pull secret %q to []bytes: %w", secretName, err)
	}
	return &mwv1.Manifest{RawExtension: runtime.RawExtension{Raw: secretBytes}}, nil
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
	// pre-flight reference gate has already validated every oadpContent reference
	// before this Prep CGU is created. The read goes through the uncached pre-flight
	// reader — the same source the gate used, in the same reconcile — so cache lag
	// cannot turn a reference the gate just resolved into a NotFound here (CNF-30176).
	// A Get/convert failure is therefore a genuine fault (e.g. an API error) rather
	// than the expected missing-backup case: return the error instead of skipping it.
	for _, cmRef := range ibgu.Spec.IBUSpec.OADPContent { //nolint:staticcheck // SA1019: OADPContent is honoured for backward compatibility.
		cm := &corev1.ConfigMap{}
		if err := r.preflightReader().Get(ctx, types.NamespacedName{
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
// As with getConfigMapManifests, a Get/convert failure is returned rather than skipped,
// and the read goes through the uncached pre-flight reader: the reference gate has
// already validated every backupContent reference before this Prep CGU is created, in
// the same reconcile and against the same uncached source, so cache lag cannot turn a
// just-resolved reference into a NotFound here — a failure is a genuine fault (CNF-30176).
func (r *IBGUReconciler) getBackupContentManifests(ctx context.Context, ibgu *ibguv1alpha1.ImageBasedGroupUpgrade) ([]mwv1.Manifest, error) {
	manifests := []mwv1.Manifest{}
	for _, bcRef := range ibgu.Spec.IBUSpec.BackupContent {
		hubBC := &ibguv1alpha1.BackupContent{}
		if err := r.preflightReader().Get(ctx, types.NamespacedName{
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
