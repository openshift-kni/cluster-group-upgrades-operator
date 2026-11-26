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
	"sort"
	"strconv"
	"strings"
	"time"

	ocpv1 "github.com/openshift/api/config/v1"
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
// pre-flight (CNF-29242 image side; CNF-30039 spoke side). It returns a
// *preflightBlock: a block when the seed image
// cannot be read (fail-closed), a block when backupContent is paired with a pre-bump
// seed (a definitive verdict on the format label actually read), a block when a spoke
// is unavailable or its expected format differs from the image's, nil when the plan
// may proceed, and an error only for a genuine infrastructure fault (so Reconcile
// requeues with backoff rather than recording a misleading condition).
//
// It reads the seed image format once for every in-scope IBGU — the spoke-side
// comparison needs it on every IBGU, not only those that set
// backupContent — and compares it against each spoke's expected format, resolved
// from the LCA ClusterClaim. The spokes it checks are plan item 0's eligible
// ManagedClusters — the frozen set the first CGU acts on — which the empty-set gate
// already resolved (threaded in as eligibleClusters), so the whole pre-flight issues a
// single uncached List. Scoping to the eligible set (not the full base set) means a
// selector-matching but plan-ineligible spoke no CGU can target never terminally
// blocks the IBGU; a targeted spoke that is unavailable or incompatible still blocks.
// For a Prep plan the eligible set equals the base set, so Prep is unaffected. No
// spoke ManifestWork is created.
func (r *IBGUReconciler) ensureSeedFormatCompatible(ctx context.Context, ibgu *ibguv1alpha1.ImageBasedGroupUpgrade, eligibleClusters []clusterv1.ManagedCluster) (*preflightBlock, error) {
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

	// Hub-only safety rule: a backupContent IBGU paired with a seed
	// image older than the first format that supports backupContent is rejected
	// here, before any spoke comparison. This is a definitive verdict on the format
	// label actually read. LCA's own Prep seed check is a hard equality that does not
	// catch this pairing (the pruned field upgrades with no application backup), which
	// is why the hub rejects it up front. It runs before the spoke comparison so a backupContent
	// IBGU on a pre-bump seed reports this specific reason rather than a per-spoke
	// mismatch.
	if len(ibgu.Spec.IBUSpec.BackupContent) > 0 && imageFormat < lcav1.BackupContentFirstSeedFormatVersion {
		msg := fmt.Sprintf(
			"spec.ibuSpec.backupContent is set but the seed image format version (%d) is older than "+
				"the first version that supports backupContent (%d)",
			imageFormat, lcav1.BackupContentFirstSeedFormatVersion)
		return &preflightBlock{utils.ConditionReasons.BackupContentUnsupportedSeed, msg}, nil
	}

	// Spoke-side check: every eligible spoke (the frozen set the plan's
	// CGUs act on) must be available and expect the image's format. The comparison is a
	// pure function of the already-listed ManagedClusters, so no additional read is
	// issued here.
	return evaluateSpokeAvailabilityAndFormat(imageFormat, eligibleClusters), nil
}

// expectedSpokeFormat returns the seed format version a spoke expects,
// resolved solely from LCA's ClusterClaim (seedformatversion.lca.openshift.io). On
// ACM 2.14+ the reserved-suffix claim is never truncated, so the claim is the one
// definitive source: when it is present its value is the expected format; a
// non-integer value is an anomaly and is returned as an error (callers fail closed).
//
// When the claim is absent the spoke's LCA predates the seed-format claim, so it
// expects the pre-bump format: one less than BackupContentFirstSeedFormatVersion,
// the last format before claim-publishing LCA. BackupContentFirstSeedFormatVersion
// is used as the anchor because it is a fixed historical marker that never changes,
// whereas SeedFormatVersion advances with each new LCA release. There is no fallback
// read — absence is definitive.
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
	return lcav1.BackupContentFirstSeedFormatVersion - 1, nil
}

// evaluateSpokeAvailabilityAndFormat compares every spoke in plan item 0's eligible set against the
// seed image's format and returns a single terminal block, or
// nil when every spoke is compatible. It issues no reads: the ManagedClusters were
// listed once by the empty-set gate.
//
// For each cluster: an unconfirmed ManagedClusterConditionAvailable (false, unknown,
// or absent) makes the spoke unavailable — its expected format cannot be determined;
// otherwise the spoke's expected format (expectedSpokeFormat) is compared against the
// image's, and a parse error counts as a mismatch (fail-closed).
//
// Precedence: any unavailable spoke yields one SpokeClusterUnavailable block naming
// the unavailable clusters (unavailable wins, because an unavailable spoke's format
// is unknown and could also mismatch); otherwise any mismatch yields one
// SeedFormatMismatch block naming the mismatched clusters and the format each expects;
// otherwise nil. The name list in either message is bounded by summarizeClusters so a
// large fleet cannot push the terminal condition message past the CRD limit. Every
// block is terminal (no requeue); recovery is delete-and-recreate.
func evaluateSpokeAvailabilityAndFormat(imageFormat int, clusters []clusterv1.ManagedCluster) *preflightBlock {
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
				summarizeClusters(unavailable))}
	}
	if len(mismatched) > 0 {
		return &preflightBlock{utils.ConditionReasons.SeedFormatMismatch,
			fmt.Sprintf("seed image format version (%d) does not match the expected format of cluster(s): %s",
				imageFormat, summarizeClusters(mismatched))}
	}
	return nil
}

// clusterListSampleBudget caps the number of characters summarizeClusters spends on
// the joined cluster-name sample. It is held well under the IBGU CRD's 32768-character
// limit on a condition message (metadata.conditions[].message) so the surrounding
// terminal-block text — the reason prefix, the seed format version, and the
// "(+N more)" suffix — always fits alongside the sample. Without this bound a block
// that names every spoke in a large fleet could exceed the CRD limit, the status
// update that records the terminal reason would be rejected, and the IBGU would never
// show why it blocked.
const clusterListSampleBudget = 24000

// summarizeClusters joins cluster identifiers into a comma-separated sample bounded to
// clusterListSampleBudget characters, appending " (+N more)" when the sample does not
// cover the whole list. It always emits at least one name (so the message still
// identifies a blocking cluster even if that one name is pathologically long) and so
// keeps the terminal condition message within the CRD limit for an arbitrarily large
// fleet. An empty input yields an empty string.
func summarizeClusters(names []string) string {
	if len(names) == 0 {
		return ""
	}
	var b strings.Builder
	shown := 0
	for i, name := range names {
		sep := ""
		if i > 0 {
			sep = ", "
		}
		// Always include the first name; stop before the sample would exceed the
		// budget once at least one name is shown.
		if shown > 0 && b.Len()+len(sep)+len(name) > clusterListSampleBudget {
			break
		}
		b.WriteString(sep)
		b.WriteString(name)
		shown++
	}
	if shown < len(names) {
		fmt.Fprintf(&b, " (+%d more)", len(names)-shown)
	}
	return b.String()
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
	// Trust the same CAs the cluster uses for image pulls, so a seed image on a
	// private registry the cluster trusts is readable here too.
	clusterCAs, err := r.registryTrustBundle(ctx)
	if err != nil {
		return 0, "", err
	}
	format, err := reader.ReadSeedFormatVersion(ctx, imageRef, dockerConfigJSON, clusterCAs)
	if err != nil {
		return 0, err.Error(), nil
	}
	return format, "", nil
}

// registryTrustBundle returns the CAs the cluster trusts for image pulls, as a PEM
// bundle to add to the system pool for the seed-image registry handshake. Two sources:
//
//   - image.config.openshift.io/cluster.spec.additionalTrustedCA: the ConfigMap in
//     openshift-config whose keys are registry hosts, holding the CAs for image pulls;
//   - openshift-config-managed/trusted-ca-bundle: the merged bundle the cluster network
//     operator maintains from the system store plus proxy.spec.trustedCA.
//
// Together they cover a private seed-image registry whether its CA was delivered through
// image.spec.additionalTrustedCA (the usual disconnected setup) or through the proxy
// trust configuration. A cluster-scoped object that does not exist is not an error: a
// non-OpenShift cluster, or a hub with no extra CAs, simply yields no bundle. A genuine
// read failure is returned so Reconcile retries, because proceeding without the CAs
// would turn a transient API error into a terminal SeedImageUnreadable block.
func (r *IBGUReconciler) registryTrustBundle(ctx context.Context) ([]byte, error) {
	reader := r.preflightReader()
	var bundle []byte

	image := &ocpv1.Image{}
	err := reader.Get(ctx, types.NamespacedName{Name: "cluster"}, image)
	switch {
	case err == nil:
		if name := image.Spec.AdditionalTrustedCA.Name; name != "" {
			configMap := &corev1.ConfigMap{}
			err := reader.Get(ctx, types.NamespacedName{Namespace: "openshift-config", Name: name}, configMap)
			switch {
			case err == nil:
				// Every value is a CA for one registry host.
				keys := make([]string, 0, len(configMap.Data))
				for key := range configMap.Data {
					keys = append(keys, key)
				}
				sort.Strings(keys)
				for _, key := range keys {
					bundle = append(bundle, []byte(configMap.Data[key])...)
					bundle = append(bundle, '\n')
				}
			case apierrors.IsNotFound(err):
				// Referenced but absent: nothing to add. The registry read below either
				// works with the system pool or reports the failure per cluster.
			default:
				return nil, fmt.Errorf("failed to get image CA ConfigMap openshift-config/%s: %w", name, err)
			}
		}
	case apierrors.IsNotFound(err), meta.IsNoMatchError(err):
		// Not an OpenShift cluster, or no image config to read.
	default:
		return nil, fmt.Errorf("failed to get the cluster image config: %w", err)
	}

	trustedCABundle := &corev1.ConfigMap{}
	err = reader.Get(ctx, types.NamespacedName{
		Namespace: "openshift-config-managed",
		Name:      "trusted-ca-bundle",
	}, trustedCABundle)
	switch {
	case err == nil:
		if data, ok := trustedCABundle.Data["ca-bundle.crt"]; ok {
			bundle = append(bundle, []byte(data)...)
			bundle = append(bundle, '\n')
		}
	case apierrors.IsNotFound(err), meta.IsNoMatchError(err):
		// No merged bundle on this cluster.
	default:
		return nil, fmt.Errorf("failed to get the cluster trust bundle: %w", err)
	}

	return bundle, nil
}
