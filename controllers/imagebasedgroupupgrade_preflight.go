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

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/openshift-kni/cluster-group-upgrades-operator/controllers/utils"
	ibguv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/imagebasedgroupupgrades/v1alpha1"
	lcav1 "github.com/openshift-kni/lifecycle-agent/api/imagebasedupgrade/v1"
)

// ensureSeedFormatCompatible runs the hub-side seed-format compatibility
// pre-flight (CNF-29242, design D4 check 1). It returns true when the IBGU must
// be blocked (a blocking Progressing condition is set and no CGU should be
// created), false when the plan may proceed, and an error only for a genuine
// infrastructure fault (so Reconcile requeues with backoff rather than recording
// a misleading condition).
//
// This is the image side of the check: its only decision is the hub-only
// backupContent safety rule, whose single input is the seed image's format
// label. An IBGU that does not set spec.ibuSpec.backupContent therefore has
// nothing to verify here and is passed through untouched — the per-spoke format
// comparison that reads the image format for every IBGU arrives with the
// spoke-side part of the check (CNF-30039). Scoping the registry read to
// backupContent IBGUs keeps this part from imposing hub→registry egress on
// IBGUs that gain nothing from it yet.
func (r *IBGUReconciler) ensureSeedFormatCompatible(ctx context.Context, ibgu *ibguv1alpha1.ImageBasedGroupUpgrade) (bool, error) {
	if len(ibgu.Spec.IBUSpec.BackupContent) == 0 {
		return false, nil
	}

	// Opt-out escape hatch for lab use: skip the check and surface the skip as an
	// event. The skip is deliberately not written as a Progressing condition: the
	// plan→CGU loop owns the Progressing condition and would immediately overwrite
	// it, so an event is the durable, observable record of the opt-out.
	if _, ok := ibgu.GetAnnotations()[utils.IBGUSkipSeedCompatibilityAnnotation]; ok {
		r.Log.Info("Seed-format compatibility pre-flight skipped by opt-out annotation",
			"ibgu", ibgu.GetName(), "annotation", utils.IBGUSkipSeedCompatibilityAnnotation)
		if r.Recorder != nil {
			r.Recorder.Event(ibgu, corev1.EventTypeNormal,
				string(utils.ConditionReasons.SeedCompatibilityCheckSkipped),
				fmt.Sprintf("Seed-format compatibility pre-flight skipped by %q annotation",
					utils.IBGUSkipSeedCompatibilityAnnotation))
		}
		return false, nil
	}

	// Read the seed image's published format once. A failure to read it
	// (reference empty, pull secret missing, registry unreachable, label absent or
	// non-integer) blocks fail-closed; the block requeues, so a transient failure
	// self-heals on the next reconcile. Only a genuine infrastructure fault reading
	// the pull secret is returned as an error.
	imageFormat, unreadableReason, err := r.readSeedImageFormat(ctx, ibgu)
	if err != nil {
		return false, err
	}
	if unreadableReason != "" {
		msg := fmt.Sprintf("Blocked: cannot read the seed image %s label: %s",
			lcav1.SeedFormatOCILabel, unreadableReason)
		r.Log.Info("IBGU blocked: seed image format label unreadable, no CGU created", "reason", unreadableReason)
		utils.SetStatusCondition(&ibgu.Status.Conditions,
			utils.ConditionTypes.Progressing,
			utils.ConditionReasons.SeedImageUnreadable,
			metav1.ConditionFalse,
			msg)
		return true, nil
	}

	// Hub-only safety rule (D4 check 1): a backupContent IBGU paired with a seed
	// image older than the first format that supports backupContent is rejected
	// here, with no spoke read. LCA's own Prep seed check is a hard equality that
	// does not catch this pairing (the pruned field upgrades with no application
	// backup), which is why the hub rejects it up front.
	if imageFormat < lcav1.BackupContentFirstSeedFormatVersion {
		msg := fmt.Sprintf(
			"Blocked: spec.ibuSpec.backupContent is set but the seed image format version (%d) is older than "+
				"the first version that supports backupContent (%d)",
			imageFormat, lcav1.BackupContentFirstSeedFormatVersion)
		r.Log.Info("IBGU blocked: backupContent set with a pre-bump seed image, no CGU created",
			"imageFormat", imageFormat, "firstSupported", lcav1.BackupContentFirstSeedFormatVersion)
		utils.SetStatusCondition(&ibgu.Status.Conditions,
			utils.ConditionTypes.Progressing,
			utils.ConditionReasons.BackupContentUnsupportedSeed,
			metav1.ConditionFalse,
			msg)
		return true, nil
	}

	return false, nil
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
