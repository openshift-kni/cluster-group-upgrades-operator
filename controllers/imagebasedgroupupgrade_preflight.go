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
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "open-cluster-management.io/api/cluster/v1"

	"github.com/openshift-kni/cluster-group-upgrades-operator/controllers/utils"
	ibguv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/imagebasedgroupupgrades/v1alpha1"
	lcav1 "github.com/openshift-kni/lifecycle-agent/api/imagebasedupgrade/v1"
)

// preflightBlock is a terminal pre-flight verdict: the IBGU creates no CGU, records
// the terminal Succeeded condition, and is never revived. Recovery is
// delete-and-recreate. A gate returns a nil *preflightBlock to mean "pass" (it found
// nothing wrong); a non-nil block carries the reason and message ensureManifests
// records via setTerminal.
//
// Every pre-flight failure is a block: an unresolvable reference (BackupContent /
// ConfigMap / pull Secret NotFound), an empty cluster set (NoClustersMatchSelector
// or NoEligibleClusters), a seed image that cannot be read (registry unreachable,
// pull secret missing/unreadable, HTTPS/transport error, config-size-cap failure),
// a definitive content verdict (backupContent paired with a pre-bump seed), a spoke
// whose availability cannot be confirmed (SpokeClusterUnavailable), and a spoke
// whose expected seed format differs from the image's (SeedFormatMismatch). The
// start deadline (PreflightWindowExpired) is also terminal, but ensureManifests
// records it directly through setTerminal rather than via a gate.
//
// Pre-flight reads go through the manager's uncached API reader (preflightReader),
// so informer-cache lag cannot produce a false block.
type preflightBlock struct {
	reason  utils.ConditionReason
	message string
}

// preflightStartDeadline bounds how long after metadata.creationTimestamp the
// first CGU may be created on the passing pre-flight path. An IBGU whose first
// reconcile lands after this window (for example because TALM was down when it was
// created) is blocked with PreflightWindowExpired rather than starting a rollout
// outside the chosen maintenance window. It applies only before the first CGU —
// appended items are never gated on it.
const preflightStartDeadline = 5 * time.Minute

// ensureSeedFormatCompatible runs the hub-side seed-format compatibility
// pre-flight (CNF-29242 image side, design D4 check 1; CNF-30039 spoke side, design
// D3 and D4 check 2). It returns a *preflightBlock: a block when the seed image
// cannot be read (fail-closed), a block when backupContent is paired with a pre-bump
// seed (a definitive verdict on the format label actually read), a block when a spoke
// is unavailable or its expected format differs from the image's, nil when the plan
// may proceed, and an error only for a genuine infrastructure fault (so Reconcile
// requeues with backoff rather than recording a misleading condition).
//
// It reads the seed image format once for every in-scope IBGU — the spoke-side
// comparison (D4 check 2) needs it on every IBGU, not only those that set
// backupContent — and compares it against each spoke's expected format, resolved
// from the LCA ClusterClaim (D3). The base ManagedCluster set is the one the
// empty-set gate already resolved (threaded in as baseClusters), so the whole
// pre-flight issues a single uncached List. No spoke ManifestWork is created.
func (r *IBGUReconciler) ensureSeedFormatCompatible(ctx context.Context, ibgu *ibguv1alpha1.ImageBasedGroupUpgrade, baseClusters []clusterv1.ManagedCluster) (*preflightBlock, error) {
	// The seed-format rule only applies to a plan that stages or applies the seed.
	// A recovery-only plan (Abort, AbortOnFailure, Rollback, FinalizeUpgrade,
	// FinalizeRollback) must never be blocked on the registry being readable: a
	// registry outage can be the very reason recovery is running, and the opt-out
	// annotation is not an acceptable answer for recovery. Upgrade stays in scope
	// because the backupContent backup is taken at Upgrade pre-pivot, so an
	// Upgrade-only follow-up can still stop a pruned-field upgrade before the pivot.
	if !planHasPrepOrUpgrade(ibgu) {
		return nil, nil
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
		return nil, nil
	}

	// Read the seed image's published format once for every in-scope IBGU. A failure
	// to read it (reference empty, pull secret missing, registry unreachable, label
	// absent or non-integer) blocks the IBGU — fail-closed. The pull secret is read
	// through the uncached reader (see readSeedImageFormat) so a cache-lagged secret
	// cannot cause a false block. Only a genuine infrastructure fault reading the pull
	// secret is returned as an error.
	imageFormat, unreadableReason, err := r.readSeedImageFormat(ctx, ibgu)
	if err != nil {
		return nil, err
	}
	if unreadableReason != "" {
		msg := fmt.Sprintf("cannot read the seed image %s label: %s",
			lcav1.SeedFormatOCILabel, unreadableReason)
		return &preflightBlock{utils.ConditionReasons.SeedImageUnreadable, msg}, nil
	}

	// Hub-only safety rule (D4 check 1): a backupContent IBGU paired with a seed
	// image older than the first format that supports backupContent is rejected
	// here, before any spoke comparison. This is a definitive verdict on the format
	// label actually read. LCA's own Prep seed check is a hard equality that does not
	// catch this pairing (the pruned field upgrades with no application backup), which
	// is why the hub rejects it up front. It runs before check 2 so a backupContent
	// IBGU on a pre-bump seed reports this specific reason rather than a per-spoke
	// mismatch.
	if len(ibgu.Spec.IBUSpec.BackupContent) > 0 && imageFormat < lcav1.BackupContentFirstSeedFormatVersion {
		msg := fmt.Sprintf(
			"spec.ibuSpec.backupContent is set but the seed image format version (%d) is older than "+
				"the first version that supports backupContent (%d)",
			imageFormat, lcav1.BackupContentFirstSeedFormatVersion)
		return &preflightBlock{utils.ConditionReasons.BackupContentUnsupportedSeed, msg}, nil
	}

	// Spoke-side check (D4 check 2): every spoke in the resolved base set must be
	// available and expect the image's format. The comparison is a pure function of
	// the already-listed ManagedClusters, so no additional read is issued here.
	return evaluateSpokeFormats(imageFormat, baseClusters), nil
}

// expectedSpokeFormat returns the seed format version a spoke expects (design D3),
// resolved solely from LCA's ClusterClaim (seedformatversion.lca.openshift.io). On
// ACM 2.14+ the reserved-suffix claim is never truncated, so the claim is the one
// definitive source: when it is present its value is the expected format; a
// non-integer value is an anomaly and is returned as an error (callers fail closed).
//
// When the claim is absent the spoke's LCA predates the seed-format claim, so it
// expects the pre-bump format: one less than SeedFormatVersion, the version a
// claim-publishing LCA reports. (In this release SeedFormatVersion equals
// BackupContentFirstSeedFormatVersion, so this also equals
// BackupContentFirstSeedFormatVersion - 1.) There is no fallback read — absence is
// definitive.
func expectedSpokeFormat(mc *clusterv1.ManagedCluster) (int, error) {
	for _, claim := range mc.Status.ClusterClaims {
		if claim.Name == lcav1.SeedFormatClusterClaimName {
			format, err := strconv.Atoi(strings.TrimSpace(claim.Value))
			if err != nil {
				return 0, fmt.Errorf("ClusterClaim %q has a non-integer value %q: %w",
					lcav1.SeedFormatClusterClaimName, claim.Value, err)
			}
			return format, nil
		}
	}
	return lcav1.SeedFormatVersion - 1, nil
}

// evaluateSpokeFormats compares every spoke in the resolved base set against the
// seed image's format (design D4 check 2) and returns a single terminal block, or
// nil when every spoke is compatible. It issues no reads: the ManagedClusters were
// listed once by the empty-set gate.
//
// For each cluster: an unconfirmed ManagedClusterConditionAvailable (false, unknown,
// or absent) makes the spoke unavailable — its expected format cannot be determined;
// otherwise the spoke's expected format (expectedSpokeFormat) is compared against the
// image's, and a parse error counts as a mismatch (fail-closed).
//
// Precedence: any unavailable spoke yields one SpokeClusterUnavailable block naming
// all unavailable clusters (unavailable wins, because an unavailable spoke's format
// is unknown and could also mismatch); otherwise any mismatch yields one
// SeedFormatMismatch block naming each mismatched cluster and the format it expects;
// otherwise nil. Every block is terminal (no requeue); recovery is delete-and-recreate.
func evaluateSpokeFormats(imageFormat int, clusters []clusterv1.ManagedCluster) *preflightBlock {
	var unavailable []string
	var mismatched []string
	for i := range clusters {
		mc := &clusters[i]
		if !meta.IsStatusConditionTrue(mc.Status.Conditions, clusterv1.ManagedClusterConditionAvailable) {
			unavailable = append(unavailable, mc.GetName())
			continue
		}
		expected, err := expectedSpokeFormat(mc)
		if err != nil {
			mismatched = append(mismatched, fmt.Sprintf("%s (unreadable seed-format claim)", mc.GetName()))
			continue
		}
		if expected != imageFormat {
			mismatched = append(mismatched, fmt.Sprintf("%s (expects %d)", mc.GetName(), expected))
		}
	}
	if len(unavailable) > 0 {
		return &preflightBlock{utils.ConditionReasons.SpokeClusterUnavailable,
			fmt.Sprintf("cannot verify seed-format compatibility: cluster(s) unavailable: %s",
				strings.Join(unavailable, ", "))}
	}
	if len(mismatched) > 0 {
		return &preflightBlock{utils.ConditionReasons.SeedFormatMismatch,
			fmt.Sprintf("seed image format version (%d) does not match the expected format of cluster(s): %s",
				imageFormat, strings.Join(mismatched, ", "))}
	}
	return nil
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
