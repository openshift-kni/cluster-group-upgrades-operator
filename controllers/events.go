package controllers

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"

	utils "github.com/openshift-kni/cluster-group-upgrades-operator/controllers/utils"
	cguv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/clustergroupupgrades/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	eventsv1 "k8s.io/api/events/v1"
	apiValidation "k8s.io/apimachinery/pkg/api/validation"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/reference"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// This is the relationship between the CGU Event Reasons and the CGU Event Actions:
// CguCreated (Reason)
// - BuildingRemediationPlan (Action) - When a new ClusterGroupUpgrade has triggered the reconciliation.
// CguStarted (Reason) - When the CGU's enable field is true.
// - PoliciesRemediationStarted (Action): When the policies remediation is started for the whole ClusterGroupUpgrade.
// - PoliciesRemediationInBatchStarted (Action): When the policies remediation is started for a batch of the ClusterGroupUpgrade.
// - PoliciesRemediationInClusterStarted (Action): When the policies remediation is started for a cluster of the ClusterGroupUpgrade.
// CguSuccess (Reason)
// - PoliciesRemediationCompleted (Action): When the policies remediation is completed for the whole ClusterGroupUpgrade.
// - PoliciesRemediationInBatchCompleted (Action): When the policies remediation is completed for a batch of the ClusterGroupUpgrade.
// - PoliciesRemediationInClusterCompleted (Action): When the policies remediation is completed for a cluster of the ClusterGroupUpgrade.
// CguTimedout (Reason)
// - PoliciesRemediationTimeout (Action): When the policies remediation is timed out for the whole ClusterGroupUpgrade.
// - PoliciesRemediationInBatchTimeout (Action): When the policies remediation is timed out for a batch of the ClusterGroupUpgrade.
// CguValidationFailure (Reason)
// - PoliciesRemediationOnHoldDueToValidationFailure (Action): When the policies remediation is on hold due to a validation failure.

// CGU Event Reasons
const (
	CGUEventReasonCreated  = "CguCreated"
	CGUEventReasonStarted  = "CguStarted"
	CGUEventReasonSuccess  = "CguSuccess"
	CGUEventReasonTimedout = "CguTimedout"

	CGUEventReasonValidationFailure = "CguValidationFailure"
)

// CGU Event Actions
const (
	CGUEventActionBuildingRemediationPlan    = "BuildingRemediationPlan"
	CGUEventActionStartRemediation           = "PoliciesRemediationStarted"
	CGUEventActionCompleteRemediation        = "PoliciesRemediationCompleted"
	CGUEventActionRemediationTimeout         = "PoliciesRemediationTimeout"
	CGUEventActionStartBatchRemediation      = "PoliciesRemediationInBatchStarted"
	CGUEventActionCompleteBatchRemediation   = "PoliciesRemediationInBatchCompleted"
	CGUEventActionBatchRemediationTimeout    = "PoliciesRemediationInBatchTimeout"
	CGUEventActionStartClusterRemediation    = "PoliciesRemediationInClusterStarted"
	CGUEventActionCompleteClusterRemediation = "PoliciesRemediationInClusterCompleted"
	CGUEventActionValidate                   = "PoliciesRemediationOnHoldDueToValidationFailure"
)

// CGU Event Messages
const (
	CGUEventMsgFmtCreated           = "New ClusterGroupUpgrade found: %s"
	CGUEventMsgFmtStarted           = "ClusterGroupUpgrade %s started remediating policies"
	CGUEventMsgFmtUpgradeSuccess    = "ClusterGroupUpgrade %s succeeded remediating policies"
	CGUEventMsgFmtUpgradeTimedout   = "ClusterGroupUpgrade %s timed-out remediating policies"
	CGUEventMsgFmtValidationFailure = "ClusterGroupUpgrade %s: validation failure (%s): %s"

	CGUEventMsgFmtBatchUpgradeStarted  = "ClusterGroupUpgrade %s: batch index %d upgrade started"
	CGUEventMsgFmtBatchUpgradeSuccess  = "ClusterGroupUpgrade %s: all clusters in the batch index %d are compliant with managed policies"
	CGUEventMsgFmtBatchUpgradeTimedout = "ClusterGroupUpgrade %s: some clusters in the batch index %d timed out remediating policies"

	CGUEventMsgFmtClusterUpgradeSuccess = "ClusterGroupUpgrade %s: cluster %s upgrade finished successfully"
	CGUEventMsgFmtClusterUpgradeStarted = "ClusterGroupUpgrade %s: cluster %s upgrade started"
	// CGUEventMsgFmtClusterUpgradeTimedout = "ClusterGroupUpgrade %s: cluster %s timed out remediating policies"
)

// CGU Validation Failure literals for the event's message.
const (
	CGUValidationFailureMissingClusters   = "missing clusters"
	CGUValidationFailureMissingPolicies   = "missing policies"
	CGUValidationFailureInvalidPolicies   = "invalid policies"
	CGUValidationFailureAmbiguousPolicies = "ambiguous polcies"
)

// Event annotation keys
const (
	CGUEventAnnotationKeyPrefix = "cgu.openshift.io"

	CGUEventAnnotationKeyEvType                = CGUEventAnnotationKeyPrefix + "/event-type"
	CGUEventAnnotationKeyBatchClustersList     = CGUEventAnnotationKeyPrefix + "/batch-clusters"
	CGUEventAnnotationKeyBatchClustersCount    = CGUEventAnnotationKeyPrefix + "/batch-clusters-count"
	CGUEventAnnotationKeyClusterName           = CGUEventAnnotationKeyPrefix + "/cluster-name"
	CGUEventAnnotationKeyTimedoutClustersList  = CGUEventAnnotationKeyPrefix + "/timedout-clusters"
	CGUEventAnnotationKeyTimedoutClustersCount = CGUEventAnnotationKeyPrefix + "/timedout-clusters-count"
	CGUEventAnnotationKeyTotalBatchesCount     = CGUEventAnnotationKeyPrefix + "/total-batches-count"
	CGUEventAnnotationKeyTotalClustersCount    = CGUEventAnnotationKeyPrefix + "/total-clusters-count"

	// Validation failures
	CGUEventAnnotationKeyMissingClustersList   = CGUEventAnnotationKeyPrefix + "/missing-clusters"
	CGUEventAnnotationKeyMissingClustersCount  = CGUEventAnnotationKeyPrefix + "/missing-clusters-count"
	CGUEventAnnotationKeyMissingPoliciesList   = CGUEventAnnotationKeyPrefix + "/missing-policies"
	CGUEventAnnotationKeyInvalidPoliciesList   = CGUEventAnnotationKeyPrefix + "/invalid-policies"
	CGUEventAnnotationKeyAmbiguousPoliciesList = CGUEventAnnotationKeyPrefix + "/ambiguous-policies"
)

// Values for the CGUEventAnnotationKeyEvType key
const (
	CGUAnnEventGlobalUpgrade  = "global"
	CGUAnnEventBatchUpgrade   = "batch"
	CGUAnnEventClusterUpgrade = "cluster"
)

const (
	maxEventAnnsSize = apiValidation.TotalAnnotationSizeLimitB
)

// CGU Validation errors
type PoliciesValidationFailureType string

const (
	CGUValidationErrorMsgMissingCluster = "missing clusters"

	CGUValidationErrorMsgNone              PoliciesValidationFailureType = "none"
	CGUValidationErrorMsgMissingPolicies   PoliciesValidationFailureType = "missing policies"
	CGUValidationErrorMsgAmbiguousPolicies PoliciesValidationFailureType = "ambiguous policies"
	CGUValidationErrorMsgInvalidPolicies   PoliciesValidationFailureType = "invalid policies"
)

// Emitter creates events/v1 Event resources directly via the Kubernetes API,
// with full control over annotations.
//
// The standard EventRecorder from client-go does not expose annotations
// in the new events/v1 API. This fills that gap for controllers
// that emit low-volume, unique lifecycle events where the EventBroadcaster's
// dedup and rate-limiting add no value but custom annotations are needed.
type Emitter struct {
	client     client.Client
	scheme     *runtime.Scheme
	controller string
	instance   string
}

// NewEmitter returns an Emitter that reports events under the given controller name.
// The scheme is needed to derive GroupVersionKind from runtime objects.
func NewEmitter(c client.Client, scheme *runtime.Scheme, controller string) *Emitter {
	instance, _ := os.Hostname()
	return &Emitter{
		client:     c,
		scheme:     scheme,
		controller: controller,
		instance:   instance,
	}
}

// Emit creates a single events/v1 Event resource in the cluster.
//
// obj is the object the event is about (the "regarding" object).
// annotations may be nil for events that don't carry extra metadata.
// eventType is "Normal" or "Warning" (use corev1.EventType* constants).
// reason is a short, CamelCase machine-readable string (e.g. "CguStarted").
// action describes what the controller did (e.g. "StartRemediation").
// note is the human-readable event message.
// related is an optional secondary object (e.g. the ManagedCluster being remediated).
//
// Safe to call on a nil receiver — the call is silently ignored.
func (e *Emitter) Emit(ctx context.Context, obj runtime.Object, annotations map[string]string,
	eventType, reason, action, note string, related *corev1.ObjectReference) error {

	if e == nil {
		return nil
	}

	ref, err := reference.GetReference(e.scheme, obj)
	if err != nil {
		return fmt.Errorf("building object reference: %w", err)
	}

	ev := &eventsv1.Event{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: ref.Name + "-",
			Namespace:    ref.Namespace,
			Annotations:  annotations,
		},
		EventTime:           metav1.NowMicro(),
		ReportingController: e.controller,
		ReportingInstance:   e.instance,
		Action:              action,
		Reason:              reason,
		Note:                note,
		Type:                eventType,
		Regarding: corev1.ObjectReference{
			Kind:            ref.Kind,
			APIVersion:      ref.APIVersion,
			Name:            ref.Name,
			Namespace:       ref.Namespace,
			UID:             ref.UID,
			ResourceVersion: ref.ResourceVersion,
		},
		Related: related,
	}

	return e.client.Create(ctx, ev)
}

// emitEvent is a helper that calls the Emitter and logs failures.
// Events are best-effort; errors are logged but never propagated to the caller.
func (r *ClusterGroupUpgradeReconciler) emitEvent(cgu *cguv1alpha1.ClusterGroupUpgrade,
	annotations map[string]string, eventType, reason, action, note string, related *corev1.ObjectReference) {

	if err := r.EventEmitter.Emit(context.TODO(), cgu, annotations, eventType, reason, action, note, related); err != nil {
		r.Log.Error(err, "failed to emit event", "reason", reason, "cgu", cgu.Namespace+"/"+cgu.Name)
	}
}

func (r *ClusterGroupUpgradeReconciler) sendEventCGUCreated(cgu *cguv1alpha1.ClusterGroupUpgrade) {
	evMsg := fmt.Sprintf(CGUEventMsgFmtCreated, cgu.Name)

	evAnns := map[string]string{
		CGUEventAnnotationKeyEvType: CGUAnnEventGlobalUpgrade,
	}

	r.emitEvent(cgu,
		evAnns,
		corev1.EventTypeNormal,
		CGUEventReasonCreated,
		CGUEventActionBuildingRemediationPlan,
		evMsg,
		nil,
	)
}

func (r *ClusterGroupUpgradeReconciler) sendEventCGUStarted(cgu *cguv1alpha1.ClusterGroupUpgrade) {
	evMsg := fmt.Sprintf(CGUEventMsgFmtStarted, cgu.Name)

	clustersCount := getTotalClustersNum(cgu)
	batchesCount := len(cgu.Status.RemediationPlan)

	evAnns := map[string]string{
		CGUEventAnnotationKeyEvType:             CGUAnnEventGlobalUpgrade,
		CGUEventAnnotationKeyTotalClustersCount: fmt.Sprint(clustersCount),
		CGUEventAnnotationKeyTotalBatchesCount:  fmt.Sprint(batchesCount),
	}

	r.emitEvent(cgu,
		evAnns,
		corev1.EventTypeNormal,
		CGUEventReasonStarted,
		CGUEventActionStartRemediation,
		evMsg,
		nil,
	)
}

func (r *ClusterGroupUpgradeReconciler) sendEventCGUSuccess(cgu *cguv1alpha1.ClusterGroupUpgrade) {
	evMsg := fmt.Sprintf(CGUEventMsgFmtUpgradeSuccess, cgu.Name)

	evAnns := map[string]string{
		CGUEventAnnotationKeyEvType:             CGUAnnEventGlobalUpgrade,
		CGUEventAnnotationKeyTotalClustersCount: fmt.Sprint(getTotalClustersNum(cgu)),
	}

	r.emitEvent(cgu,
		evAnns,
		corev1.EventTypeNormal,
		CGUEventReasonSuccess,
		CGUEventActionCompleteRemediation,
		evMsg,
		nil,
	)
}

func (r *ClusterGroupUpgradeReconciler) sendEventCGUTimedout(cgu *cguv1alpha1.ClusterGroupUpgrade) {
	evMsg := fmt.Sprintf(CGUEventMsgFmtUpgradeTimedout, cgu.Name)

	timedoutClusters := []string{}
	for _, clusterState := range cgu.Status.Clusters {
		if clusterState.State == utils.ClusterRemediationTimedout {
			timedoutClusters = append(timedoutClusters, clusterState.Name)
		}
	}

	evAnns := map[string]string{
		CGUEventAnnotationKeyEvType:                CGUAnnEventGlobalUpgrade,
		CGUEventAnnotationKeyTimedoutClustersCount: fmt.Sprint(len(timedoutClusters)),
		CGUEventAnnotationKeyTimedoutClustersList:  strings.Join(timedoutClusters, ","),
		CGUEventAnnotationKeyTotalClustersCount:    fmt.Sprint(getTotalClustersNum(cgu)),
	}

	truncateAnnotations(evAnns, maxEventAnnsSize)

	r.emitEvent(cgu,
		evAnns,
		corev1.EventTypeWarning,
		CGUEventReasonTimedout,
		CGUEventActionRemediationTimeout,
		evMsg,
		nil,
	)
}

func (r *ClusterGroupUpgradeReconciler) sendEventCGUBatchUpgradeStarted(cgu *cguv1alpha1.ClusterGroupUpgrade) {
	batchClusters := []string{}
	for clusterName := range cgu.Status.Status.CurrentBatchRemediationProgress {
		batchClusters = append(batchClusters, clusterName)
	}
	// CurrentBatchRemediationProgress is a map, sort the cluster name list for
	// generating deterministic event payload string
	slices.Sort(batchClusters)

	evMsg := fmt.Sprintf(CGUEventMsgFmtBatchUpgradeStarted, cgu.Name, cgu.Status.Status.CurrentBatch)

	evAnns := map[string]string{
		CGUEventAnnotationKeyEvType:             CGUAnnEventBatchUpgrade,
		CGUEventAnnotationKeyBatchClustersCount: fmt.Sprint(len(batchClusters)),
		CGUEventAnnotationKeyTotalClustersCount: fmt.Sprint(getTotalClustersNum(cgu)),
		CGUEventAnnotationKeyBatchClustersList:  strings.Join(batchClusters, ","),
	}

	truncateAnnotations(evAnns, maxEventAnnsSize)

	r.emitEvent(cgu,
		evAnns,
		corev1.EventTypeNormal,
		CGUEventReasonStarted,
		CGUEventActionStartBatchRemediation,
		evMsg,
		nil,
	)
}

func (r *ClusterGroupUpgradeReconciler) sendEventCGUBatchUpgradeSuccess(cgu *cguv1alpha1.ClusterGroupUpgrade) {
	evMsg := fmt.Sprintf(CGUEventMsgFmtBatchUpgradeSuccess, cgu.Name, cgu.Status.Status.CurrentBatch)

	batchClusters := []string{}
	for clusterName := range cgu.Status.Status.CurrentBatchRemediationProgress {
		batchClusters = append(batchClusters, clusterName)
	}
	// CurrentBatchRemediationProgress is a map, sort the cluster name list for
	// generating deterministic event payload string
	slices.Sort(batchClusters)

	batchClustersCount := len(batchClusters)
	totalClustersCount := getTotalClustersNum(cgu)

	evAnns := map[string]string{
		CGUEventAnnotationKeyEvType:             CGUAnnEventBatchUpgrade,
		CGUEventAnnotationKeyBatchClustersCount: fmt.Sprint(batchClustersCount),
		CGUEventAnnotationKeyTotalClustersCount: fmt.Sprint(totalClustersCount),
		CGUEventAnnotationKeyBatchClustersList:  strings.Join(batchClusters, ","),
	}

	truncateAnnotations(evAnns, maxEventAnnsSize)

	r.emitEvent(cgu,
		evAnns,
		corev1.EventTypeNormal,
		CGUEventReasonSuccess,
		CGUEventActionCompleteBatchRemediation,
		evMsg,
		nil,
	)
}

func (r *ClusterGroupUpgradeReconciler) sendEventCGUBatchUpgradeTimedout(cgu *cguv1alpha1.ClusterGroupUpgrade) {
	evMsg := fmt.Sprintf(CGUEventMsgFmtBatchUpgradeTimedout, cgu.Name, cgu.Status.Status.CurrentBatch)

	batchClustersCount := 0
	timedoutClusters := []string{}
	for clusterName, clusterProgress := range cgu.Status.Status.CurrentBatchRemediationProgress {
		batchClustersCount++
		if clusterProgress.State == cguv1alpha1.InProgress {
			timedoutClusters = append(timedoutClusters, clusterName)
		}
	}

	timedoutClustersCount := len(timedoutClusters)
	totalClustersCount := getTotalClustersNum(cgu)

	evAnns := map[string]string{
		CGUEventAnnotationKeyEvType:                CGUAnnEventBatchUpgrade,
		CGUEventAnnotationKeyTimedoutClustersCount: fmt.Sprint(timedoutClustersCount),
		CGUEventAnnotationKeyTimedoutClustersList:  strings.Join(timedoutClusters, ","),
		CGUEventAnnotationKeyBatchClustersCount:    fmt.Sprint(batchClustersCount),
		CGUEventAnnotationKeyTotalClustersCount:    fmt.Sprint(totalClustersCount),
	}

	truncateAnnotations(evAnns, maxEventAnnsSize)

	r.emitEvent(cgu,
		evAnns,
		corev1.EventTypeWarning,
		CGUEventReasonTimedout,
		CGUEventActionBatchRemediationTimeout,
		evMsg,
		nil,
	)
}

func (r *ClusterGroupUpgradeReconciler) sendEventCGUClusterUpgradeStarted(cgu *cguv1alpha1.ClusterGroupUpgrade, clusterName string) {
	evMsg := fmt.Sprintf(CGUEventMsgFmtClusterUpgradeStarted, cgu.Name, clusterName)

	evAnns := map[string]string{
		CGUEventAnnotationKeyEvType:      CGUAnnEventClusterUpgrade,
		CGUEventAnnotationKeyClusterName: clusterName,
	}

	truncateAnnotations(evAnns, maxEventAnnsSize)

	managedClusterRef := &corev1.ObjectReference{
		APIVersion: clusterv1.GroupVersion.String(),
		Kind:       "ManagedCluster",
		Name:       clusterName,
	}

	r.emitEvent(cgu,
		evAnns,
		corev1.EventTypeNormal,
		CGUEventReasonStarted,
		CGUEventActionStartClusterRemediation,
		evMsg,
		managedClusterRef,
	)
}

func (r *ClusterGroupUpgradeReconciler) sendEventCGUClusterUpgradeSuccess(cgu *cguv1alpha1.ClusterGroupUpgrade, clusterName string) {
	evMsg := fmt.Sprintf(CGUEventMsgFmtClusterUpgradeSuccess, cgu.Name, clusterName)

	evAnns := map[string]string{
		CGUEventAnnotationKeyEvType:      CGUAnnEventClusterUpgrade,
		CGUEventAnnotationKeyClusterName: clusterName,
	}

	truncateAnnotations(evAnns, maxEventAnnsSize)

	managedClusterRef := &corev1.ObjectReference{
		APIVersion: clusterv1.GroupVersion.String(),
		Kind:       "ManagedCluster",
		Name:       clusterName,
	}

	r.emitEvent(cgu,
		evAnns,
		corev1.EventTypeNormal,
		CGUEventReasonSuccess,
		CGUEventActionCompleteClusterRemediation,
		evMsg,
		managedClusterRef,
	)
}

// func (r *ClusterGroupUpgradeReconciler) sendEventCGUClusterUpgradeTimedout(cgu *cguv1alpha1.ClusterGroupUpgrade, clusterName string) {
// 	evMsg := fmt.Sprintf(CGUEventMsgFmtClusterUpgradeTimedout, cgu.Name, clusterName)

// 	evAnns := map[string]string{
// 		CGUEventAnnotationKeyEvType:      CGUAnnEventClusterUpgrade,
// 		CGUEventAnnotationKeyClusterName: clusterName,
// 	}

// 	truncateAnnotations(evAnns, maxEventAnnsSize)

// 	r.Recorder.AnnotatedEventf(cgu,
// 		evAnns,
// 		corev1.EventTypeNormal,
// 		CGUEventReasonTimedout, evMsg)
// }

func (r *ClusterGroupUpgradeReconciler) sendEventCGUValidationFailureMissingClusters(cgu *cguv1alpha1.ClusterGroupUpgrade, clusterNames []string) {
	clusterNamesStr := strings.Join(clusterNames, ",")
	evMsg := fmt.Sprintf(CGUEventMsgFmtValidationFailure, cgu.Name, CGUValidationErrorMsgMissingCluster, clusterNamesStr)

	evAnns := map[string]string{
		CGUEventAnnotationKeyMissingClustersCount: fmt.Sprint(len(clusterNames)),
		CGUEventAnnotationKeyMissingClustersList:  clusterNamesStr,
	}

	truncateAnnotations(evAnns, maxEventAnnsSize)

	r.emitEvent(cgu,
		evAnns,
		corev1.EventTypeNormal,
		CGUEventReasonValidationFailure,
		CGUEventActionValidate,
		evMsg,
		nil,
	)
}

func (r *ClusterGroupUpgradeReconciler) sendEventCGUVPoliciesValidationFailure(cgu *cguv1alpha1.ClusterGroupUpgrade, failureType PoliciesValidationFailureType, info policiesInfo) {
	r.Log.Info("Sending policies validation failure event",
		"cgu", cgu.Namespace+"/"+cgu.Name,
		"failureType", string(failureType),
	)

	var evMsg string
	anns := map[string]string{}

	switch failureType {
	case CGUValidationErrorMsgMissingPolicies:
		missingPoliciesStr := strings.Join(info.missingPolicies, ",")
		evMsg = fmt.Sprintf(CGUEventMsgFmtValidationFailure, cgu.Name, failureType, missingPoliciesStr)
		anns[CGUEventAnnotationKeyMissingPoliciesList] = missingPoliciesStr
	case CGUValidationErrorMsgInvalidPolicies:
		invalidPoliciesStr := strings.Join(info.invalidPolicies, ",")
		evMsg = fmt.Sprintf(CGUEventMsgFmtValidationFailure, cgu.Name, failureType, invalidPoliciesStr)
		anns[CGUEventAnnotationKeyInvalidPoliciesList] = invalidPoliciesStr
	case CGUValidationFailureAmbiguousPolicies:
		ambiguousPolicies := []string{}
		for policy := range info.duplicatedPoliciesNs {
			ambiguousPolicies = append(ambiguousPolicies, policy)
		}

		ambiguousPoliciesStr := strings.Join(ambiguousPolicies, ",")

		evMsg = fmt.Sprintf(CGUEventMsgFmtValidationFailure, cgu.Name, failureType, ambiguousPoliciesStr)
		anns[CGUEventAnnotationKeyAmbiguousPoliciesList] = ambiguousPoliciesStr
	}

	truncateAnnotations(anns, maxEventAnnsSize)

	r.emitEvent(cgu,
		anns,
		corev1.EventTypeWarning,
		CGUEventReasonValidationFailure,
		CGUEventActionValidate,
		evMsg,
		nil,
	)
}

// truncateAnnotations shrinks annotations whose values can grow unbounded
// (batch clusters, timedout clusters, etc.) to stay within the Kubernetes
// annotation size limit of 64 KiB.
//
// No truncation is made if maxSize is 0.
// nolint: unparam
func truncateAnnotations(anns map[string]string, maxSize int) {
	canBeTruncatedAnnKeys := map[string]bool{
		CGUEventAnnotationKeyBatchClustersList:    true,
		CGUEventAnnotationKeyTimedoutClustersList: true,
		CGUEventAnnotationKeyMissingClustersList:  true,
	}

	var totalAnnsSize int64
	for k, v := range anns {
		totalAnnsSize += int64(len(k)) + int64(len(v))
	}

	if totalAnnsSize <= int64(maxSize) {
		return
	}

	sizeToShrink := totalAnnsSize - int64(maxSize)

	for k, v := range anns {
		if !canBeTruncatedAnnKeys[k] {
			continue
		}

		maxListStrLen := int64(len(v)) - sizeToShrink
		anns[k] = truncateListString(v, maxListStrLen)

		// Only one truncatable annotation per event, so we're done.
		break
	}
}

// truncateListString keeps only the comma-separated elements that fit in maxSize.
func truncateListString(listStr string, maxSize int64) string {
	newElems := []string{}

	elems := strings.Split(listStr, ",")
	for _, elem := range elems {
		newElems = append(newElems, elem)

		newElemsStr := strings.Join(newElems, ",")
		if int64(len(newElemsStr)) > maxSize {
			newElems = newElems[:len(newElems)-1]
			return strings.Join(newElems, ",")
		}
	}

	return strings.Join(newElems, ",")
}

func getTotalClustersNum(cgu *cguv1alpha1.ClusterGroupUpgrade) int {
	total := 0
	for _, clusters := range cgu.Status.RemediationPlan {
		total += len(clusters)
	}

	return total
}
