/*
Copyright 2026.

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
	"math"
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/openshift-kni/cluster-group-upgrades-operator/controllers/utils"
	ibguv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/imagebasedgroupupgrades/v1alpha1"
	lcav1 "github.com/openshift-kni/lifecycle-agent/api/imagebasedupgrade/v1"
)

// preflightClass is a pre-flight gate's verdict on whether the plan may proceed,
// and if not, whether the failure is transient (retried within a grace window
// before becoming terminal) or immediately terminal.
type preflightClass int

const (
	// preflightPass means the gate found nothing wrong; the plan may proceed.
	preflightPass preflightClass = iota
	// preflightTransient means the failure could be GitOps-sync / propagation lag
	// or a transient infrastructure fault: it is retried within the grace window
	// (measured from metadata.creationTimestamp) and only becomes terminal once the
	// window expires. The cached manager client can return NotFound for a
	// BackupContent/ConfigMap or a ManagedCluster label that a single GitOps sync
	// applied alongside the IBGU, so emptiness and reference-not-found are lag-prone
	// rather than definitively wrong.
	preflightTransient
	// preflightTerminal means a definitive verdict on content actually read: the
	// IBGU is blocked immediately with no grace window, creates no CGU, and is never
	// revived. Recovery is delete-and-recreate.
	preflightTerminal
)

// preflightOutcome is the result a pre-flight gate returns to ensureManifests,
// which owns the condition/requeue decision: a transient outcome requeues within
// the grace window (handleTransientPreflight), a terminal outcome records the
// terminal Succeeded condition (setTerminal), and a pass falls through to the
// plan loop.
//
// Classification contract (so a gate added later maps onto the same classes):
//   - A failure that could be sync/propagation lag or a transient infrastructure
//     fault is preflightTransient: an unresolvable reference (BackupContent /
//     ConfigMap NotFound), an empty cluster set (NoClustersMatchSelector or
//     NoEligibleClusters), and a seed image that cannot be read (registry
//     unreachable, pull secret missing/unreadable, HTTPS/transport error,
//     config-size-cap failure). For CNF-30039's spoke-side seed check: a spoke
//     that is unavailable or whose read-only ManifestWork feedback has not yet
//     returned is also preflightTransient.
//   - A definitive verdict on content actually read is preflightTerminal:
//     backupContent paired with a pre-bump seed, and (CNF-30039) a seed-format
//     mismatch where both the seed and target values were actually read.
type preflightOutcome struct {
	class   preflightClass
	reason  utils.ConditionReason
	message string
}

// passOutcome, transientOutcome and terminalOutcome construct the three
// preflightOutcome variants.
func passOutcome() preflightOutcome {
	return preflightOutcome{class: preflightPass}
}

func transientOutcome(reason utils.ConditionReason, message string) preflightOutcome {
	return preflightOutcome{class: preflightTransient, reason: reason, message: message}
}

func terminalOutcome(reason utils.ConditionReason, message string) preflightOutcome {
	return preflightOutcome{class: preflightTerminal, reason: reason, message: message}
}

// ensureSeedFormatCompatible runs the hub-side seed-format compatibility
// pre-flight (CNF-29242, design D4 check 1). It returns a preflightOutcome:
// terminal when backupContent is paired with a pre-bump seed (a definitive
// verdict on the format label actually read), transient when the seed image
// cannot be read (fail-closed, retried within the grace window), pass when the
// plan may proceed, and an error only for a genuine infrastructure fault (so
// Reconcile requeues with backoff rather than recording a misleading condition).
//
// This is the image side of the check: its only decision is the hub-only
// backupContent safety rule, whose single input is the seed image's format
// label. An IBGU that does not set spec.ibuSpec.backupContent therefore has
// nothing to verify here and is passed through untouched — the per-spoke format
// comparison that reads the image format for every IBGU arrives with the
// spoke-side part of the check (CNF-30039). Scoping the registry read to
// backupContent IBGUs keeps this part from imposing hub→registry egress on
// IBGUs that gain nothing from it yet.
func (r *IBGUReconciler) ensureSeedFormatCompatible(ctx context.Context, ibgu *ibguv1alpha1.ImageBasedGroupUpgrade) (preflightOutcome, error) {
	if len(ibgu.Spec.IBUSpec.BackupContent) == 0 {
		return passOutcome(), nil
	}

	// The seed-format rule only applies to a plan that stages or applies the seed.
	// A recovery-only plan (Abort, AbortOnFailure, Rollback, FinalizeUpgrade,
	// FinalizeRollback) must never be blocked on the registry being readable: a
	// registry outage can be the very reason recovery is running, and the opt-out
	// annotation is not an acceptable answer for recovery. Upgrade stays in scope
	// because the backupContent backup is taken at Upgrade pre-pivot, so an
	// Upgrade-only follow-up can still stop a pruned-field upgrade before the pivot.
	if !planHasPrepOrUpgrade(ibgu) {
		return passOutcome(), nil
	}

	// Opt-out escape hatch for lab use: skip the check and surface the skip as an
	// event. The skip is deliberately not written as a condition: the plan→CGU loop
	// owns Progressing and would immediately overwrite it, so an event is the
	// durable, observable record of the opt-out.
	if _, ok := ibgu.GetAnnotations()[utils.IBGUSkipSeedCompatibilityAnnotation]; ok {
		r.Log.Info("Seed-format compatibility pre-flight skipped by opt-out annotation",
			"ibgu", ibgu.GetName(), "annotation", utils.IBGUSkipSeedCompatibilityAnnotation)
		if r.Recorder != nil {
			r.Recorder.Event(ibgu, corev1.EventTypeNormal,
				string(utils.ConditionReasons.SeedCompatibilityCheckSkipped),
				fmt.Sprintf("Seed-format compatibility pre-flight skipped by %q annotation",
					utils.IBGUSkipSeedCompatibilityAnnotation))
		}
		return passOutcome(), nil
	}

	// Read the seed image's published format once. A failure to read it (reference
	// empty, pull secret missing, registry unreachable, label absent or non-integer)
	// is transient — fail-closed but retried within the grace window, since it can be
	// a transient infrastructure fault. Only a genuine infrastructure fault reading
	// the pull secret is returned as an error.
	imageFormat, unreadableReason, err := r.readSeedImageFormat(ctx, ibgu)
	if err != nil {
		return preflightOutcome{}, err
	}
	if unreadableReason != "" {
		msg := fmt.Sprintf("cannot read the seed image %s label: %s",
			lcav1.SeedFormatOCILabel, unreadableReason)
		return transientOutcome(utils.ConditionReasons.SeedImageUnreadable, msg), nil
	}

	// Hub-only safety rule (D4 check 1): a backupContent IBGU paired with a seed
	// image older than the first format that supports backupContent is rejected
	// here, with no spoke read. This is a definitive verdict on the format label
	// actually read, so it is terminal with no grace window. LCA's own Prep seed
	// check is a hard equality that does not catch this pairing (the pruned field
	// upgrades with no application backup), which is why the hub rejects it up front.
	if imageFormat < lcav1.BackupContentFirstSeedFormatVersion {
		msg := fmt.Sprintf(
			"spec.ibuSpec.backupContent is set but the seed image format version (%d) is older than "+
				"the first version that supports backupContent (%d)",
			imageFormat, lcav1.BackupContentFirstSeedFormatVersion)
		return terminalOutcome(utils.ConditionReasons.BackupContentUnsupportedSeed, msg), nil
	}

	return passOutcome(), nil
}

// planHasPrepOrUpgrade reports whether the IBGU plan contains a Prep or Upgrade
// action — the only actions that stage (Prep) or apply (Upgrade) the seed and so
// depend on the seed format. Recovery-only plans contain none of these and must
// not be gated on a readable registry.
func planHasPrepOrUpgrade(ibgu *ibguv1alpha1.ImageBasedGroupUpgrade) bool {
	for _, item := range ibgu.Spec.Plan {
		for _, action := range item.Actions {
			if action == ibguv1alpha1.Prep || action == ibguv1alpha1.Upgrade {
				return true
			}
		}
	}
	return false
}

// readSeedImageFormat resolves the IBGU's seed image reference and reads its
// published seed format version from the registry, authenticating with the pull
// secret the IBGU references (if any). The registry read is delegated to the
// reconciler's SeedImageReader so tests can inject a fake and avoid real egress.
//
// It returns (format, "", nil) on success, (0, reason, nil) when the format
// cannot be read and the IBGU must block fail-closed (reason explains why), and
// (0, "", err) only for a genuine infrastructure fault (a non-NotFound error
// reading the pull secret) that should requeue with backoff.
func (r *IBGUReconciler) readSeedImageFormat(ctx context.Context, ibgu *ibguv1alpha1.ImageBasedGroupUpgrade) (int, string, error) {
	imageRef := ibgu.Spec.IBUSpec.SeedImageRef.Image
	if imageRef == "" {
		return 0, "spec.ibuSpec.seedImageRef.image is empty", nil
	}

	var dockerConfigJSON []byte
	if pullSecretRef := ibgu.Spec.IBUSpec.SeedImageRef.PullSecretRef; pullSecretRef != nil {
		secret := &corev1.Secret{}
		if err := r.Get(ctx, types.NamespacedName{
			Name:      pullSecretRef.Name,
			Namespace: ibgu.GetNamespace(),
		}, secret); err != nil {
			if apierrors.IsNotFound(err) {
				return 0, fmt.Sprintf("pull secret %q not found", pullSecretRef.Name), nil
			}
			return 0, "", fmt.Errorf("failed to get pull secret %q: %w", pullSecretRef.Name, err)
		}
		dockerConfigJSON = secret.Data[corev1.DockerConfigJsonKey]
		if len(dockerConfigJSON) == 0 {
			return 0, fmt.Sprintf("pull secret %q has no %s data", pullSecretRef.Name, corev1.DockerConfigJsonKey), nil
		}
	}

	reader := r.SeedImageReader
	if reader == nil {
		reader = utils.NewSeedImageReader()
	}
	format, err := reader.ReadSeedFormatVersion(ctx, imageRef, dockerConfigJSON)
	if err != nil {
		return 0, err.Error(), nil
	}
	return format, "", nil
}

// maxPreflightRetrySeconds is the largest grace window, in seconds, that can be
// converted to a time.Duration (nanoseconds) without overflowing int64. A value
// above this would wrap to a negative duration when multiplied by time.Second,
// turning the grace window into an immediate terminal block, so it is rejected as
// invalid like any other out-of-range value.
const maxPreflightRetrySeconds = math.MaxInt64 / int64(time.Second)

// parsePreflightRetrySeconds parses the lcm.openshift.io/ibgu-preflight-retry-seconds
// annotation without side effects, returning the grace window in seconds and whether
// the annotation value was valid. An absent annotation is valid and yields the
// default; a non-integer, negative, or overflowing value (one that would wrap
// time.Duration past int64 when multiplied by time.Second) is invalid and yields the
// default. The side-effect-free form lets callers that run after the transient handler
// (which has already warned about an invalid value this reconcile) reuse the value
// without emitting a duplicate Warning event.
func parsePreflightRetrySeconds(ibgu *ibguv1alpha1.ImageBasedGroupUpgrade) (seconds int, valid bool) {
	raw, ok := ibgu.GetAnnotations()[utils.IBGUPreflightRetrySecondsAnnotation]
	if !ok {
		return utils.DefaultPreflightRetrySeconds, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 || int64(n) > maxPreflightRetrySeconds {
		return utils.DefaultPreflightRetrySeconds, false
	}
	return n, true
}

// preflightRetrySeconds returns the transient pre-flight grace window, in seconds,
// for this IBGU: the value of the lcm.openshift.io/ibgu-preflight-retry-seconds
// annotation when it parses as a non-negative integer, otherwise
// DefaultPreflightRetrySeconds. An invalid value (non-integer or negative) falls
// back to the default and raises a Warning event so the operator notices the
// typo, rather than silently using a window they did not intend.
func (r *IBGUReconciler) preflightRetrySeconds(ibgu *ibguv1alpha1.ImageBasedGroupUpgrade) int {
	seconds, valid := parsePreflightRetrySeconds(ibgu)
	if !valid {
		raw := ibgu.GetAnnotations()[utils.IBGUPreflightRetrySecondsAnnotation]
		msg := fmt.Sprintf("invalid %s value %q; using the default of %d seconds",
			utils.IBGUPreflightRetrySecondsAnnotation, raw, utils.DefaultPreflightRetrySeconds)
		r.Log.Info("IBGU " + msg)
		if r.Recorder != nil {
			r.Recorder.Event(ibgu, corev1.EventTypeWarning, "InvalidPreflightRetrySeconds", msg)
		}
	}
	return seconds
}

// transientPreflightRequeueAfter returns how long to wait before re-evaluating a
// transient pre-flight block. Nothing the controller watches changes while the block
// is open — no CGU exists yet and the IBGU generation is unchanged — so the requeue is
// what drives re-evaluation before the grace deadline. It requeues at the earlier of
// one minute and the time remaining before the deadline (creationTimestamp + window)
// minus a small buffer, with a floor of one second, so the last re-check lands inside
// the window rather than just after it. Without the deadline-aware interval a window
// shorter than a minute would requeue past its own deadline and block the IBGU
// terminally even if the cause cleared in time.
func transientPreflightRequeueAfter(ibgu *ibguv1alpha1.ImageBasedGroupUpgrade) ctrl.Result {
	const buffer = 5 * time.Second
	const floor = 1 * time.Second
	seconds, _ := parsePreflightRetrySeconds(ibgu)
	deadline := ibgu.GetCreationTimestamp().Add(time.Duration(seconds) * time.Second)
	interval := deadline.Sub(metav1.Now().Time) - buffer
	if interval > time.Minute {
		interval = time.Minute
	}
	if interval < floor {
		interval = floor
	}
	return ctrl.Result{RequeueAfter: interval}
}

// handleTransientPreflight applies the grace window to a transient pre-flight
// outcome. The window is measured from metadata.creationTimestamp — the moment
// the operator acted — so it cannot be reset or lost, and a TALM outage that
// delays the first reconcile past the window goes straight to terminal. While the
// window is open it records a non-terminal Progressing=False "retrying" condition
// (Succeeded is intentionally not set, so ensureManifests does not short-circuit)
// and requeues; once the window has elapsed it records the terminal Succeeded
// condition with the last reason and does not requeue. It returns true to requeue
// (still within the window) and false once terminal.
func (r *IBGUReconciler) handleTransientPreflight(ibgu *ibguv1alpha1.ImageBasedGroupUpgrade, outcome preflightOutcome) bool {
	grace := time.Duration(r.preflightRetrySeconds(ibgu)) * time.Second
	elapsed := metav1.Now().Sub(ibgu.GetCreationTimestamp().Time)
	if elapsed < grace {
		msg := fmt.Sprintf("Retrying pre-flight check within the %s grace window since creation: %s",
			grace, outcome.message)
		r.Log.Info("IBGU pre-flight check failed transiently, retrying within grace window",
			"reason", outcome.reason, "elapsed", elapsed.String(), "grace", grace.String())
		utils.SetStatusCondition(&ibgu.Status.Conditions,
			utils.ConditionTypes.Progressing,
			outcome.reason,
			metav1.ConditionFalse,
			msg)
		return true
	}
	r.setTerminal(ibgu, outcome.reason,
		fmt.Sprintf("%s; the %s pre-flight grace window since creation has expired", outcome.message, grace))
	return false
}
