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
	// block. It is required: production wires it from mgr.GetAPIReader(), and tests
	// pass their fake client (or a deliberately lagging fake).
	APIReader client.Reader
	// SeedImageReader reads the seed format version label from a seed image for the
	// seed-format compatibility pre-flight. It is an interface so tests can inject a
	// fake and avoid real registry egress; when nil the default registry-backed
	// reader is used.
	SeedImageReader utils.SeedImageReader
}

// preflightReader returns the manager's uncached API reader, used for every
// pre-flight read so a resource applied alongside the IBGU (for example in the same
// GitOps sync) is not read as missing before the informer cache catches up and
// wrongly blocked. APIReader is required (see the field doc); a nil reader is a
// wiring error, not a supported fallback.
func (r *IBGUReconciler) preflightReader() client.Reader {
	return r.APIReader
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
	// One CGU snapshot per reconcile. The status rebuild, the cluster labels derived
	// from it and the plan loop all read this list, so a plan item completing in the
	// same status write that sets its CGU's Succeeded condition is seen by the next
	// item's decision rather than a reconcile later.
	cguList, err := r.listOwnedCGUs(ctx, ibgu)
	if err != nil {
		r.Log.Error(err, "error listing cgus for ibgu")
		return requeueWithError(err)
	}

	// Rebuild Status.Clusters before ensureClusterLabels, so the labels that a plan
	// item's CGU selector depends on are written as early as possible in the reconcile.
	r.syncStatusFromCGUList(ctx, ibgu, cguList)

	err = r.ensureClusterLabels(ctx, ibgu)
	if err != nil {
		r.Log.Error(err, "error ensuring cluster labels")
		return requeueWithError(err)
	}

	err = r.ensureManifests(ctx, ibgu, cguList)
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

// listOwnedCGUs returns the CGUs owned by this IBGU. The cached list is used unless it
// is empty, and an empty one is confirmed through the uncached pre-flight reader: an
// empty list is what runs the pre-flight gates and can record a permanent block, so a
// first CGU this controller just created must not be missed under informer-cache lag.
// A non-empty cached list needs no confirmation.
func (r *IBGUReconciler) listOwnedCGUs(
	ctx context.Context, ibgu *ibguv1alpha1.ImageBasedGroupUpgrade,
) (*ranv1alpha1.ClusterGroupUpgradeList, error) {
	opts := &client.ListOptions{
		Namespace:     ibgu.Namespace,
		LabelSelector: labels.SelectorFromSet(labels.Set{utils.CGUOwnerIBGULabel: ibgu.Name}),
	}
	cguList := &ranv1alpha1.ClusterGroupUpgradeList{}
	if err := r.List(ctx, cguList, opts); err != nil {
		return nil, fmt.Errorf("failed to get CGUs for the IBGU: %w", err)
	}
	if len(cguList.Items) > 0 {
		return cguList, nil
	}
	uncachedCGUList := &ranv1alpha1.ClusterGroupUpgradeList{}
	if err := r.preflightReader().List(ctx, uncachedCGUList, opts); err != nil {
		return nil, fmt.Errorf("failed to get CGUs for the IBGU through the uncached reader: %w", err)
	}
	return uncachedCGUList, nil
}

// syncStatusFromCGUList rebuilds Status.Clusters from the supplied CGU snapshot rather
// than listing the CGUs itself. Reconcile passes the one snapshot it also hands to
// ensureManifests, so the status the plan loop and the completion record read is the
// same view the plan advances from.
func (r *IBGUReconciler) syncStatusFromCGUList(
	ctx context.Context, ibgu *ibguv1alpha1.ImageBasedGroupUpgrade,
	cguList *ranv1alpha1.ClusterGroupUpgradeList,
) {
	if len(cguList.Items) == 0 {
		return
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
						// does not skip the Status.Clusters write below. A missing
						// ManifestWork is expected (the stage is simply still being
						// applied) and is ignored silently.
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

// resolveBaseClusters returns the ManagedClusters matching the IBGU's
// clusterLabelSelectors: the base set the empty-set gate narrows to plan item 0's
// eligible clusters, which is what gets frozen (not the base set itself).
// Multiple selectors are unioned, mirroring CGU selector semantics. The reader is
// the uncached pre-flight reader, so cache lag cannot drop a matching cluster and
// cause a false empty-set block.
func (r *IBGUReconciler) resolveBaseClusters(ctx context.Context, reader client.Reader, ibgu *ibguv1alpha1.ImageBasedGroupUpgrade) ([]clusterv1.ManagedCluster, error) {
	byName := make(map[string]clusterv1.ManagedCluster)
	names := []string{}
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
			if _, exists := byName[mc.GetName()]; exists {
				continue
			}
			byName[mc.GetName()] = mc
			names = append(names, mc.GetName())
		}
	}
	sort.Strings(names)
	clusters := make([]clusterv1.ManagedCluster, 0, len(names))
	for _, name := range names {
		clusters = append(clusters, byName[name])
	}
	return clusters, nil
}

// getFrozenClusterSet returns the frozen cluster set — the clusters plan item 0 acts on —
// read back from the `name In (...)` requirement in plan item 0's CGU. Every later plan
// item receives this record as the base of its own selector rather than re-resolving
// the IBGU's selectors, so a later step can only act on clusters the IBGU started on.
func getFrozenClusterSet(ibgu *ibguv1alpha1.ImageBasedGroupUpgrade, cguList *ranv1alpha1.ClusterGroupUpgradeList) []string {
	if len(ibgu.Spec.Plan) == 0 {
		return nil
	}
	cguName := getCGUNameForPlanItem(ibgu, &ibgu.Spec.Plan[0], 0)
	for i := range cguList.Items {
		if cguList.Items[i].GetName() == cguName {
			return frozenSetFromSelectors(cguList.Items[i].Spec.ClusterLabelSelectors)
		}
	}
	// Plan item 0's CGU is absent (only if it was deleted out of band, which is
	// not detected), so there is no frozen set.
	return nil
}

// frozenSetFromSelectors extracts the frozen cluster set from a CGU's selectors: the
// values of its `name In (...)` requirement. Every entry carries the same values - a
// plan item whose action requirement is disjunctive gets one entry per disjunct - so
// the first one found is the set.
func frozenSetFromSelectors(selectors []metav1.LabelSelector) []string {
	for i := range selectors {
		for j := range selectors[i].MatchExpressions {
			requirement := selectors[i].MatchExpressions[j]
			if requirement.Key == utils.ClusterNameLabel && requirement.Operator == metav1.LabelSelectorOpIn {
				return requirement.Values
			}
		}
	}
	return nil
}

// ensureCGUForPlanItem reconciles a single plan item into its CGU and reports whether
// the item is completed: true when the item is done (its CGU carries the Succeeded
// condition, including when its selector matched no cluster), so the plan loop proceeds
// to the next item; false when the item's CGU exists but has not finished, or was just
// created this reconcile, so the plan loop stops and waits.
//
// The cluster set is not resolved here: ensureManifests passes the frozen set, and the
// CGU narrows it itself with the plan item's action requirement, as it did before IBGU
// resolved cluster lists (see utils.PlanItemClusterSelectors).
func (r *IBGUReconciler) ensureCGUForPlanItem(
	ctx context.Context,
	ibgu *ibguv1alpha1.ImageBasedGroupUpgrade,
	planItem *ibguv1alpha1.PlanItem, planItemIndex int,
	cguList *ranv1alpha1.ClusterGroupUpgradeList,
	frozen []string,
) (bool, error) {
	cguName := getCGUNameForPlanItem(ibgu, planItem, planItemIndex)

	for _, cgu := range cguList.Items {
		if cgu.GetName() == cguName {
			completed := utils.IsStatusConditionPresent(cgu.Status.Conditions, string(utils.ConditionTypes.Succeeded))
			r.Log.Info("CGU already exist for plan item", "cgu name", cguName, "completed", completed)
			return completed, nil
		}
	}

	if len(frozen) == 0 {
		// Only reachable when plan item 0's CGU is missing (deleted out of band, which is
		// unsupported and undetected). An `In` requirement with no values is not a valid
		// selector, so nothing can be targeted; treat the item as completed.
		r.Log.Info("No frozen cluster set for plan item, skipping CGU creation",
			"planItem actions", planItem.Actions, "planItemIndex", planItemIndex)
		return true, nil
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
				return false, fmt.Errorf("error getting configmap manifests: %w", err)
			}
			var bcManifests []mwv1.Manifest
			bcManifests, err = r.getBackupContentManifests(ctx, ibgu)
			if err != nil {
				return false, fmt.Errorf("error getting backupcontent manifests: %w", err)
			}
			manifests = append(manifests, bcManifests...)
			var secretManifest *mwv1.Manifest
			secretManifest, err = r.getSecretManifest(ctx, ibgu)
			if err != nil {
				return false, fmt.Errorf("error getting secret manifest: %w", err)
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
			return false, fmt.Errorf("error generating manifestworkreplicaset: %w", err)
		}
		manifestWorkReplicaSets = append(manifestWorkReplicaSets, mwrs)
		manifestWorkReplicaSetsNames = append(manifestWorkReplicaSetsNames, templateName)
	}

	clusterSelectors := utils.PlanItemClusterSelectors(frozen, planItem)

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
		cguName, ibgu, planItem, manifestWorkReplicaSetsNames, clusterSelectors, annotations, disableAutoImport)
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
func (r *IBGUReconciler) ensureManifests(
	ctx context.Context, ibgu *ibguv1alpha1.ImageBasedGroupUpgrade,
	cguList *ranv1alpha1.ClusterGroupUpgradeList,
) error {
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

	// frozen is the cluster set every plan item's CGU is confined to: the clusters plan
	// item 0 acts on (the IBGU's selector result narrowed by plan item 0's action). Before
	// the first CGU it comes from the empty-set gate, resolved once through the uncached
	// reader, so the gate and the first CGU can never disagree; once an owned CGU exists it
	// is read back from plan item 0's CGU, so later items never re-resolve.
	//
	// Only plan item 0 is evaluated up front. A first step that selects nothing is a spec
	// error and blocks the IBGU terminally; a later step that selects nothing (e.g.
	// AbortOnFailure when nothing failed) is a normal outcome, so its CGU is created anyway,
	// narrows the frozen set by label itself, selects nothing and completes.
	var frozen []string

	// Pre-flight gates run once, before the first CGU is created, so an IBGU already
	// in progress (an owned CGU exists) — including one resumed for an appended item —
	// is unaffected. On failure they create no CGU and block the IBGU terminally. The
	// list is the one Reconcile took, already confirmed uncached when empty, so an
	// empty list really is empty and an IBGU whose first CGU exists never records a
	// false permanent block.
	if len(cguList.Items) == 0 {
		var block *preflightBlock
		var err error
		frozen, block, err = r.runPreflightGates(ctx, ibgu)
		if err != nil {
			return err
		}
		if block != nil {
			r.setTerminal(ibgu, block.reason, block.message)
			return nil
		}
		// Honour the start deadline on the passing path too: even when every gate
		// passes, the first CGU must be created within preflightStartDeadline of
		// creationTimestamp. If TALM was down past that window and recovered, a
		// now-passing pre-flight must not start a rollout hours late (outside the
		// chosen maintenance window); block it instead. This applies only before the
		// first CGU — appended items (CGUs already exist) are never gated on the deadline.
		if metav1.Now().Sub(ibgu.GetCreationTimestamp().Time) >= preflightStartDeadline {
			r.setTerminal(ibgu, utils.ConditionReasons.PreflightWindowExpired,
				fmt.Sprintf("pre-flight checks passed but the %s start deadline since creation has "+
					"expired before the first CGU could be created; the IBGU started too late", preflightStartDeadline))
			return nil
		}
	} else {
		frozen = getFrozenClusterSet(ibgu, cguList)
	}

	for i := range ibgu.Spec.Plan {
		planItem := ibgu.Spec.Plan[i]
		completed, err := r.ensureCGUForPlanItem(ctx, ibgu, &planItem, i, cguList, frozen)
		if err != nil {
			return fmt.Errorf("error ensuring cgu for plan item: %w", err)
		}
		if !completed {
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
		}
		// Completed: proceed to the next plan item.
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
// reason. On the passing path it also returns plan item 0's eligible cluster set,
// resolved once by the empty-set gate. That set is the IBGU's frozen cluster set: the
// first CGU is created from it, and every later plan item is confined to it.
func (r *IBGUReconciler) runPreflightGates(ctx context.Context, ibgu *ibguv1alpha1.ImageBasedGroupUpgrade) ([]string, *preflightBlock, error) {
	// 1. Empty-set gate (D2): plan item 0 must resolve to at least one eligible
	// cluster. Its resolved eligible set is returned for reuse as the first CGU's
	// cluster set, along with the full base ManagedCluster set it listed (reused by
	// the seed-format gate below, so the whole pre-flight issues one uncached List).
	item0Clusters, baseClusters, block, err := r.ensureNonEmptyClusterSet(ctx, ibgu)
	if err != nil || block != nil {
		return nil, block, err
	}

	// 2. Reference-resolution gate: for a plan that contains a Prep item, every
	// backupContent and oadpContent reference (and the seed pull secret, if set) must
	// resolve on the hub. An Upgrade-only follow-up plan (no Prep item) is exempt
	// because it copies nothing — LCA fails such a plan at pre-pivot if a spoke copy is
	// missing. In every plan the CRD admits a Prep item is the first item (see the plan
	// XValidation rule), so this validates the Prep references before any CGU that
	// copies them is created. References are read through the uncached pre-flight reader
	// so one applied alongside the IBGU in the same GitOps sync is not read as NotFound
	// under informer-cache lag. resolvePrepReferences stays pure (it only reports the
	// missing names) so it can be unit-tested directly.
	if planHasPrep(ibgu) {
		missing, err := r.resolvePrepReferences(ctx, r.preflightReader(), ibgu)
		if err != nil {
			return nil, nil, fmt.Errorf("error resolving Prep references: %w", err)
		}
		if len(missing) > 0 {
			return nil, &preflightBlock{utils.ConditionReasons.UnresolvableDenpendency,
				fmt.Sprintf("unresolvable backup references: %s", strings.Join(missing, ", "))}, nil
		}
	}

	// 3. Seed-format compatibility gate (CNF-29242 image side, CNF-30039 spoke side):
	// the network read, run last. It reuses the base ManagedCluster set resolved by
	// the empty-set gate for its per-spoke format comparison, so no second List is
	// issued.
	block, err = r.ensureSeedFormatCompatible(ctx, ibgu, baseClusters)
	if err != nil {
		return nil, nil, fmt.Errorf("error checking seed format compatibility: %w", err)
	}
	return item0Clusters, block, nil
}

// ensureNonEmptyClusterSet is the empty-set pre-flight gate (design D2): it resolves
// plan item 0's eligible cluster set once, through the uncached pre-flight reader, and
// returns it as the IBGU's frozen cluster set, so the first CGU is created from the very
// set the gate evaluated — the gate and the CGU can never disagree, so there is no
// empty-set race to re-check. Later plan items are not evaluated here: a later step
// that selects nothing is a normal outcome, not a pre-flight failure. When the
// set is empty it distinguishes two terminal reasons: no ManagedCluster matches the
// selector (NoClustersMatchSelector), or the base set is non-empty but none is eligible
// for plan item 0's action (NoEligibleClusters). Eligibility is evaluated with the same
// selector the CGU receives, against the same uncached snapshot.
// Reading through the uncached reader means a ManagedCluster or label applied
// alongside the IBGU is not missed under informer-cache lag and wrongly reported as
// empty. The returned eligible set is meaningful only on a passing outcome.
//
// It also returns the full resolved base set (the ManagedClusters matching the
// IBGU's clusterLabelSelectors, with their Status) so the whole pre-flight needs
// only this one uncached List: the seed-format gate (ensureSeedFormatCompatible)
// reuses the slice for its per-spoke format comparison instead of listing again.
// The base set is returned only on a passing outcome.
func (r *IBGUReconciler) ensureNonEmptyClusterSet(ctx context.Context, ibgu *ibguv1alpha1.ImageBasedGroupUpgrade) ([]string, []clusterv1.ManagedCluster, *preflightBlock, error) {
	reader := r.preflightReader()
	base, err := r.resolveBaseClusters(ctx, reader, ibgu)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("error resolving base clusters: %w", err)
	}
	if len(base) == 0 {
		return nil, nil, &preflightBlock{utils.ConditionReasons.NoClustersMatchSelector,
			"no ManagedCluster matches the IBGU clusterLabelSelectors"}, nil
	}

	baseNames := make([]string, 0, len(base))
	for i := range base {
		baseNames = append(baseNames, base[i].GetName())
	}
	planItem := &ibgu.Spec.Plan[0]
	selectors := utils.PlanItemClusterSelectors(baseNames, planItem)
	parsed, err := parseLabelSelectors(selectors)
	if err != nil {
		return nil, nil, nil, err
	}

	eligible := []string{}
	for i := range base {
		// Match each ManagedCluster's actual labels, exactly as the CGU will, so a
		// cluster the CGU would not select (e.g. one missing ACM's name label) is not
		// counted eligible here either.
		if matchesAnySelector(parsed, labels.Set(base[i].GetLabels())) {
			eligible = append(eligible, base[i].GetName())
		}
	}
	if len(eligible) == 0 {
		return nil, nil, &preflightBlock{utils.ConditionReasons.NoEligibleClusters,
			fmt.Sprintf("no cluster is eligible for plan item 0 (actions %v)", planItem.Actions)}, nil
	}
	sort.Strings(eligible)
	return eligible, base, nil, nil
}

// parseLabelSelectors converts API label selectors into their matcher form. CGUs union
// their clusterLabelSelectors entries, so matching any one of them is eligibility.
func parseLabelSelectors(selectors []metav1.LabelSelector) ([]labels.Selector, error) {
	parsed := make([]labels.Selector, 0, len(selectors))
	for i := range selectors {
		selector, err := metav1.LabelSelectorAsSelector(&selectors[i])
		if err != nil {
			return nil, fmt.Errorf("invalid plan item cluster selector: %w", err)
		}
		parsed = append(parsed, selector)
	}
	return parsed, nil
}

// matchesAnySelector reports whether the cluster labels satisfy any of the selectors.
func matchesAnySelector(selectors []labels.Selector, clusterLabels labels.Set) bool {
	for _, selector := range selectors {
		if selector.Matches(clusterLabels) {
			return true
		}
	}
	return false
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
// here. Because the reference gate has already established the secret exists, any read
// failure — including NotFound — and any serialization failure
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
// since the reference-resolution gate (runPreflightGates) has already validated every
// reference before this Prep CGU is created (so a failure here is a genuine fault).
func (r *IBGUReconciler) getConfigMapManifests(ctx context.Context, ibgu *ibguv1alpha1.ImageBasedGroupUpgrade) ([]mwv1.Manifest, error) {
	manifests := []mwv1.Manifest{}
	// OADPContent is deprecated in favour of BackupContent (name-only references
	// that LCA resolves on the spoke), but it remains honoured for backward
	// compatibility until LCA removes it. Hub-side BackupContent references are
	// handled by getBackupContentManifests.
	//
	// In every plan the CRD admits a Prep item is the first plan item, so the
	// pre-flight reference gate has already validated every oadpContent reference
	// before this Prep CGU is created. The read goes through the uncached pre-flight
	// reader — the same source the gate used, in the same reconcile — so cache lag
	// cannot turn a reference the gate just resolved into a NotFound here.
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
// just-resolved reference into a NotFound here — a failure is a genuine fault.
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
				// a generation change means plan items were appended (the plan is
				// the only mutable spec field)
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
