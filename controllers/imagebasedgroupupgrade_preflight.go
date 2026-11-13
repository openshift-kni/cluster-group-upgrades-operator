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
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

	"github.com/openshift-kni/cluster-group-upgrades-operator/controllers/utils"
	ibguv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/imagebasedgroupupgrades/v1alpha1"
	lcav1 "github.com/openshift-kni/lifecycle-agent/api/imagebasedupgrade/v1"
)

// preflightClass is a pre-flight gate's verdict on whether the plan may proceed.
type preflightClass int

const (
	// preflightPass means the gate found nothing wrong; the plan may proceed.
	preflightPass preflightClass = iota
	// preflightTerminal means the IBGU is blocked immediately: it creates no CGU,
	// records the terminal Succeeded condition, never requeues, and is never revived.
	// Recovery is delete-and-recreate.
	preflightTerminal
)

// preflightOutcome is the result a pre-flight gate returns to ensureManifests,
// which owns the condition decision: a terminal outcome records the terminal
// Succeeded condition (setTerminal), and a pass falls through to the plan loop.
//
// Classification contract (so a gate added later maps onto the same classes):
//   - Every pre-flight failure is preflightTerminal: the IBGU is blocked at once,
//     with no retry. This covers an unresolvable reference (BackupContent /
//     ConfigMap NotFound), an empty cluster set (NoClustersMatchSelector or
//     NoEligibleClusters), a seed image that cannot be read (registry unreachable,
//     pull secret missing/unreadable, HTTPS/transport error, config-size-cap
//     failure), and a definitive content verdict (backupContent paired with a
//     pre-bump seed). For CNF-30039's spoke-side seed check, a seed-format mismatch
//     read from the LCA ClusterClaim is likewise terminal.
//   - preflightPass is the only non-terminal class: the gate found nothing wrong.
//
// Pre-flight reads go through the manager's uncached API reader (preflightReader),
// so informer-cache lag cannot produce a false block — which is what lets every
// failure be terminal immediately, with no grace window (CNF-30176).
type preflightOutcome struct {
	class   preflightClass
	reason  utils.ConditionReason
	message string
}

// passOutcome and terminalOutcome construct the two preflightOutcome variants.
func passOutcome() preflightOutcome {
	return preflightOutcome{class: preflightPass}
}

func terminalOutcome(reason utils.ConditionReason, message string) preflightOutcome {
	return preflightOutcome{class: preflightTerminal, reason: reason, message: message}
}

// preflightStartDeadline bounds how long after metadata.creationTimestamp the
// first CGU may be created on the passing pre-flight path. An IBGU whose first
// reconcile lands after this window (for example because TALM was down when it was
// created) is blocked with PreflightWindowExpired rather than starting a rollout
// outside the chosen maintenance window. It is a fixed constant with no annotation
// override, and applies only before the first CGU — appended items are never gated
// on it (CNF-30176).
const preflightStartDeadline = 5 * time.Minute

// ensureSeedFormatCompatible runs the hub-side seed-format compatibility
// pre-flight (CNF-29242, design D4 check 1). It returns a preflightOutcome:
// terminal when backupContent is paired with a pre-bump seed (a definitive
// verdict on the format label actually read), terminal when the seed image
// cannot be read (fail-closed), pass when the plan may proceed, and an error only
// for a genuine infrastructure fault (so Reconcile requeues with backoff rather
// than recording a misleading condition).
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
	// is terminal — fail-closed, with no retry. The pull secret is read through the
	// uncached reader (see readSeedImageFormat) so a cache-lagged secret cannot cause
	// a false block. Only a genuine infrastructure fault reading the pull secret is
	// returned as an error.
	imageFormat, unreadableReason, err := r.readSeedImageFormat(ctx, ibgu)
	if err != nil {
		return preflightOutcome{}, err
	}
	if unreadableReason != "" {
		msg := fmt.Sprintf("cannot read the seed image %s label: %s",
			lcav1.SeedFormatOCILabel, unreadableReason)
		return terminalOutcome(utils.ConditionReasons.SeedImageUnreadable, msg), nil
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
		// Pre-flight read: use the uncached reader so a pull secret applied alongside
		// the IBGU (e.g. in the same GitOps sync) is not read as missing before the
		// informer cache catches up and wrongly blocked as SeedImageUnreadable.
		if err := r.preflightReader().Get(ctx, types.NamespacedName{
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
