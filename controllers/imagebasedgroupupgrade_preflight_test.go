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
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/openshift-kni/cluster-group-upgrades-operator/controllers/utils"
	"github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/clustergroupupgrades/v1alpha1"
	ibguv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/imagebasedgroupupgrades/v1alpha1"
	lcav1 "github.com/openshift-kni/lifecycle-agent/api/imagebasedupgrade/v1"
)

// fakeSeedImageReader is an injectable utils.SeedImageReader that records whether
// it was called and returns a canned format/error, so the gate's branches can be
// exercised without real registry egress.
type fakeSeedImageReader struct {
	format           int
	err              error
	called           bool
	calledImageRef   string
	calledDockerConf []byte
}

func (f *fakeSeedImageReader) ReadSeedFormatVersion(_ context.Context, imageRef string, dockerConfigJSON []byte) (int, error) {
	f.called = true
	f.calledImageRef = imageRef
	f.calledDockerConf = dockerConfigJSON
	return f.format, f.err
}

// preBumpFormat is one older than the first format that supports backupContent,
// so a backupContent IBGU paired with it must be rejected.
const preBumpFormat = lcav1.BackupContentFirstSeedFormatVersion - 1

func backupContentIBGU() *ibguv1alpha1.ImageBasedGroupUpgrade {
	ibgu := newIBGUForResolution([]ibguv1alpha1.PlanItem{{Actions: []string{ibguv1alpha1.Prep}}})
	ibgu.Spec.IBUSpec.BackupContent = []lcav1.BackupContentRef{{Name: "bc1"}}
	return ibgu
}

func TestEnsureSeedFormatCompatible(t *testing.T) {
	tests := []struct {
		name           string
		mutate         func(ibgu *ibguv1alpha1.ImageBasedGroupUpgrade)
		reader         *fakeSeedImageReader
		pullSecret     *corev1.Secret
		expectClass    preflightClass
		expectReason   utils.ConditionReason
		expectReadCall bool
		expectEvent    bool
	}{
		{
			name:           "no backupContent passes through without reading the image",
			mutate:         func(ibgu *ibguv1alpha1.ImageBasedGroupUpgrade) { ibgu.Spec.IBUSpec.BackupContent = nil },
			reader:         &fakeSeedImageReader{format: preBumpFormat},
			expectClass:    preflightPass,
			expectReadCall: false,
		},
		{
			name:           "backupContent with a pre-bump seed is terminal",
			reader:         &fakeSeedImageReader{format: preBumpFormat},
			expectClass:    preflightTerminal,
			expectReason:   utils.ConditionReasons.BackupContentUnsupportedSeed,
			expectReadCall: true,
		},
		{
			name:           "backupContent with a supported seed proceeds",
			reader:         &fakeSeedImageReader{format: lcav1.BackupContentFirstSeedFormatVersion},
			expectClass:    preflightPass,
			expectReadCall: true,
		},
		{
			name:           "unreadable image label is terminal (fail-closed)",
			reader:         &fakeSeedImageReader{err: fmt.Errorf("registry unreachable")},
			expectClass:    preflightTerminal,
			expectReason:   utils.ConditionReasons.SeedImageUnreadable,
			expectReadCall: true,
		},
		{
			name: "opt-out annotation skips the check and reports an event",
			mutate: func(ibgu *ibguv1alpha1.ImageBasedGroupUpgrade) {
				ibgu.SetAnnotations(map[string]string{utils.IBGUSkipSeedCompatibilityAnnotation: ""})
			},
			reader:         &fakeSeedImageReader{format: preBumpFormat},
			expectClass:    preflightPass,
			expectReadCall: false,
			expectEvent:    true,
		},
		{
			name: "missing pull secret is terminal (unreadable)",
			mutate: func(ibgu *ibguv1alpha1.ImageBasedGroupUpgrade) {
				ibgu.Spec.IBUSpec.SeedImageRef.PullSecretRef = &lcav1.PullSecretRef{Name: "missing"}
			},
			reader:         &fakeSeedImageReader{format: lcav1.BackupContentFirstSeedFormatVersion},
			expectClass:    preflightTerminal,
			expectReason:   utils.ConditionReasons.SeedImageUnreadable,
			expectReadCall: false,
		},
		{
			name: "recovery-only plan is not gated on a readable registry",
			mutate: func(ibgu *ibguv1alpha1.ImageBasedGroupUpgrade) {
				ibgu.Spec.Plan = []ibguv1alpha1.PlanItem{{Actions: []string{ibguv1alpha1.Rollback}}}
			},
			reader:         &fakeSeedImageReader{err: fmt.Errorf("registry unreachable")},
			expectClass:    preflightPass,
			expectReadCall: false,
		},
		{
			name: "upgrade-only plan is still checked",
			mutate: func(ibgu *ibguv1alpha1.ImageBasedGroupUpgrade) {
				ibgu.Spec.Plan = []ibguv1alpha1.PlanItem{{Actions: []string{ibguv1alpha1.Upgrade}}}
			},
			reader:         &fakeSeedImageReader{format: preBumpFormat},
			expectClass:    preflightTerminal,
			expectReason:   utils.ConditionReasons.BackupContentUnsupportedSeed,
			expectReadCall: true,
		},
		{
			name: "pull secret is read and passed to the reader",
			mutate: func(ibgu *ibguv1alpha1.ImageBasedGroupUpgrade) {
				ibgu.Spec.IBUSpec.SeedImageRef.PullSecretRef = &lcav1.PullSecretRef{Name: "pull"}
			},
			pullSecret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "pull", Namespace: "namespace"},
				Type:       corev1.SecretTypeDockerConfigJson,
				Data:       map[string][]byte{corev1.DockerConfigJsonKey: []byte(`{"auths":{}}`)},
			},
			reader:         &fakeSeedImageReader{format: lcav1.BackupContentFirstSeedFormatVersion},
			expectClass:    preflightPass,
			expectReadCall: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ibgu := backupContentIBGU()
			if test.mutate != nil {
				test.mutate(ibgu)
			}

			objs := []client.Object{}
			fakeClient, err := getFakeClientFromObjects(objs...)
			assert.NoError(t, err)
			if test.pullSecret != nil {
				assert.NoError(t, fakeClient.Create(context.TODO(), test.pullSecret))
			}

			recorder := record.NewFakeRecorder(10)
			reconciler := IBGUReconciler{
				Client:          fakeClient,
				Scheme:          testscheme,
				Log:             logr.Discard(),
				Recorder:        recorder,
				SeedImageReader: test.reader,
			}

			outcome, err := reconciler.ensureSeedFormatCompatible(context.TODO(), ibgu)
			assert.NoError(t, err)
			assert.Equal(t, test.expectClass, outcome.class)
			assert.Equal(t, test.expectReadCall, test.reader.called)
			if test.expectReason != "" {
				assert.Equal(t, test.expectReason, outcome.reason)
			}
			// The gate only classifies; it never writes a condition itself (that is
			// ensureManifests's job once it has the outcome).
			assert.Nil(t, progressingCondition(ibgu))
			assert.Nil(t, succeededCondition(ibgu))

			if test.expectEvent {
				select {
				case e := <-recorder.Events:
					assert.Contains(t, e, string(utils.ConditionReasons.SeedCompatibilityCheckSkipped))
				default:
					t.Fatalf("expected a %s event, got none", utils.ConditionReasons.SeedCompatibilityCheckSkipped)
				}
			}
		})
	}
}

// TestEnsureSeedFormatCompatibleInfraFault verifies that a genuine (non-NotFound)
// error reading the pull secret is returned as an error — so Reconcile requeues
// with backoff — rather than being classified as a fail-closed "unreadable" block.
func TestEnsureSeedFormatCompatibleInfraFault(t *testing.T) {
	ibgu := backupContentIBGU()
	ibgu.Spec.IBUSpec.SeedImageRef.PullSecretRef = &lcav1.PullSecretRef{Name: "pull"}

	base, err := getFakeClientFromObjects()
	assert.NoError(t, err)
	faultyClient := interceptor.NewClient(base, interceptor.Funcs{
		Get: func(_ context.Context, _ client.WithWatch, _ client.ObjectKey, obj client.Object, _ ...client.GetOption) error {
			if _, ok := obj.(*corev1.Secret); ok {
				return apierrors.NewInternalError(fmt.Errorf("etcd unavailable"))
			}
			return nil
		},
	})

	reader := &fakeSeedImageReader{format: lcav1.BackupContentFirstSeedFormatVersion}
	reconciler := IBGUReconciler{
		Client: faultyClient, Scheme: testscheme, Log: logr.Discard(),
		SeedImageReader: reader,
	}

	outcome, err := reconciler.ensureSeedFormatCompatible(context.TODO(), ibgu)
	assert.Error(t, err)
	assert.Equal(t, preflightPass, outcome.class)
	assert.False(t, reader.called)
	// No condition is recorded for a transient fault.
	assert.Nil(t, progressingCondition(ibgu))
	assert.Nil(t, succeededCondition(ibgu))
}

// TestEnsureManifestsSeedFormatGate verifies the seed-format gate as wired into
// ensureManifests: with its references resolved it rejects a pre-bump seed
// terminally before any CGU is created, it runs after the reference gate, and it
// only runs before the first CGU.
func TestEnsureManifestsSeedFormatGate(t *testing.T) {
	t.Run("backupContent with pre-bump seed is terminal before any CGU", func(t *testing.T) {
		ibgu := backupContentIBGU()

		fakeClient, err := getFakeClientFromObjects()
		assert.NoError(t, err)
		// bc1 exists so the reference gate passes and the seed gate (run last) decides.
		assert.NoError(t, fakeClient.Create(context.TODO(), hubBackupContent("bc1", "namespace")))
		assert.NoError(t, fakeClient.Create(context.TODO(),
			managedClusterWithLabels("cluster1", map[string]string{"common": "true"})))

		reconciler := IBGUReconciler{
			Client: fakeClient, Scheme: testscheme, Log: logr.Discard(),
			Recorder:        record.NewFakeRecorder(10),
			SeedImageReader: &fakeSeedImageReader{format: preBumpFormat},
		}
		blocked, err := reconciler.ensureManifests(context.TODO(), ibgu)
		assert.NoError(t, err)
		// Terminal: no requeue.
		assert.False(t, blocked)

		// The terminal state is recorded on Succeeded (and mirrored onto Progressing).
		succeeded := succeededCondition(ibgu)
		assert.NotNil(t, succeeded)
		assert.Equal(t, metav1.ConditionFalse, succeeded.Status)
		assert.Equal(t, string(utils.ConditionReasons.BackupContentUnsupportedSeed), succeeded.Reason)
		assert.Equal(t, string(utils.ConditionReasons.BackupContentUnsupportedSeed), progressingCondition(ibgu).Reason)

		cgu := &v1alpha1.ClusterGroupUpgrade{}
		err = reconciler.Get(context.TODO(),
			types.NamespacedName{Name: "name-prep-0", Namespace: "namespace"}, cgu)
		assert.True(t, apierrors.IsNotFound(err))
	})

	t.Run("reference gate runs before the seed gate", func(t *testing.T) {
		// Both the reference (missing BackupContent) and the seed (pre-bump) would
		// fail; the reference reason must win because the hub-only reference gate runs
		// before the registry read, which must therefore not happen.
		ibgu := backupContentIBGU()

		fakeClient, err := getFakeClientFromObjects()
		assert.NoError(t, err)
		assert.NoError(t, fakeClient.Create(context.TODO(),
			managedClusterWithLabels("cluster1", map[string]string{"common": "true"})))

		reader := &fakeSeedImageReader{format: preBumpFormat}
		reconciler := IBGUReconciler{
			Client: fakeClient, Scheme: testscheme, Log: logr.Discard(),
			Recorder:        record.NewFakeRecorder(10),
			SeedImageReader: reader,
		}
		blocked, err := reconciler.ensureManifests(context.TODO(), ibgu)
		assert.NoError(t, err)
		// The missing reference is terminal: no requeue.
		assert.False(t, blocked)
		// The seed gate never ran: no registry read.
		assert.False(t, reader.called)

		// The terminal block carries the reference reason, not the seed reason.
		succeeded := succeededCondition(ibgu)
		assert.NotNil(t, succeeded)
		assert.Equal(t, metav1.ConditionFalse, succeeded.Status)
		assert.Equal(t, string(utils.ConditionReasons.UnresolvableDenpendency), succeeded.Reason)
		assert.Equal(t, string(utils.ConditionReasons.UnresolvableDenpendency), progressingCondition(ibgu).Reason)
	})

	t.Run("compatible seed with resolved references creates a CGU", func(t *testing.T) {
		ibgu := backupContentIBGU()

		fakeClient, err := getFakeClientFromObjects()
		assert.NoError(t, err)
		assert.NoError(t, fakeClient.Create(context.TODO(), hubBackupContent("bc1", "namespace")))
		assert.NoError(t, fakeClient.Create(context.TODO(),
			managedClusterWithLabels("cluster1", map[string]string{"common": "true"})))

		reader := &fakeSeedImageReader{format: lcav1.BackupContentFirstSeedFormatVersion}
		reconciler := IBGUReconciler{
			Client: fakeClient, Scheme: testscheme, Log: logr.Discard(),
			SeedImageReader: reader,
		}
		blocked, err := reconciler.ensureManifests(context.TODO(), ibgu)
		assert.NoError(t, err)
		assert.False(t, blocked)
		assert.True(t, reader.called)

		cgu := &v1alpha1.ClusterGroupUpgrade{}
		err = reconciler.Get(context.TODO(),
			types.NamespacedName{Name: "name-prep-0", Namespace: "namespace"}, cgu)
		assert.NoError(t, err)
	})

	t.Run("in-progress IBGU is not re-checked", func(t *testing.T) {
		// A two-item plan whose plan item 0 CGU has completed but whose Upgrade item is
		// still to roll out, so the IBGU is genuinely in progress (not finished).
		ibgu := newIBGUForResolution([]ibguv1alpha1.PlanItem{
			{Actions: []string{ibguv1alpha1.Prep}},
			{Actions: []string{ibguv1alpha1.Upgrade}},
		})
		ibgu.Spec.IBUSpec.BackupContent = []lcav1.BackupContentRef{{Name: "bc1"}}

		fakeClient, err := getFakeClientFromObjects()
		assert.NoError(t, err)
		// A CGU already exists for plan item 0, so no pre-flight gate runs.
		prepCGU := completedPrepCGU([]string{"cluster1"})
		assert.NoError(t, fakeClient.Create(context.TODO(), &prepCGU))
		assert.NoError(t, fakeClient.Create(context.TODO(),
			managedClusterWithLabels("cluster1", map[string]string{
				"common": "true", "lcm.openshift.io/ibgu-prep-completed": ""})))

		reader := &fakeSeedImageReader{format: preBumpFormat}
		reconciler := IBGUReconciler{
			Client: fakeClient, Scheme: testscheme, Log: logr.Discard(),
			SeedImageReader: reader,
		}
		blocked, err := reconciler.ensureManifests(context.TODO(), ibgu)
		assert.NoError(t, err)
		assert.False(t, blocked)
		assert.False(t, reader.called)

		// The Upgrade item is still rolling out, so no terminal state is recorded.
		assert.Nil(t, succeededCondition(ibgu))
		assert.Equal(t, string(utils.ConditionReasons.InProgress), progressingCondition(ibgu).Reason)
	})

	// A finished IBGU (Succeeded present) must not be re-gated on a later reconcile:
	// an unreadable seed must not overwrite its terminal state.
	t.Run("finished IBGU with Succeeded present is not re-gated", func(t *testing.T) {
		ibgu := backupContentIBGU()
		utils.SetStatusCondition(&ibgu.Status.Conditions,
			utils.ConditionTypes.Succeeded, utils.ConditionReasons.Completed,
			metav1.ConditionTrue, "All plan steps are completed")

		fakeClient, err := getFakeClientFromObjects()
		assert.NoError(t, err)
		assert.NoError(t, fakeClient.Create(context.TODO(), hubBackupContent("bc1", "namespace")))
		// A cluster that now matches the selector appears after completion; it must
		// not pull plan item 0 back into a rollout.
		assert.NoError(t, fakeClient.Create(context.TODO(),
			managedClusterWithLabels("late", map[string]string{"common": "true"})))

		reader := &fakeSeedImageReader{err: fmt.Errorf("registry unreachable")}
		reconciler := IBGUReconciler{
			Client: fakeClient, Scheme: testscheme, Log: logr.Discard(),
			SeedImageReader: reader,
		}
		blocked, err := reconciler.ensureManifests(context.TODO(), ibgu)
		assert.NoError(t, err)
		assert.False(t, blocked)
		// The short-circuit fires before any gate: no registry read.
		assert.False(t, reader.called)

		// The terminal Succeeded state is preserved; the unreadable seed did not change it.
		succeeded := succeededCondition(ibgu)
		assert.NotNil(t, succeeded)
		assert.Equal(t, string(utils.ConditionReasons.Completed), succeeded.Reason)

		// No CGU is created.
		cgu := &v1alpha1.ClusterGroupUpgrade{}
		err = reconciler.Get(context.TODO(),
			types.NamespacedName{Name: "name-prep-0", Namespace: "namespace"}, cgu)
		assert.True(t, apierrors.IsNotFound(err))
	})
}

// newUnreadableSeedReconciler builds a backupContent Prep IBGU with a resolvable
// reference and a matching cluster, so the empty-set and reference gates pass and the
// seed gate (unreadable registry) is the gate under test.
func newUnreadableSeedReconciler(t *testing.T) (*ibguv1alpha1.ImageBasedGroupUpgrade, *fakeSeedImageReader, *record.FakeRecorder, IBGUReconciler) {
	t.Helper()
	ibgu := backupContentIBGU()
	fakeClient, err := getFakeClientFromObjects()
	assert.NoError(t, err)
	assert.NoError(t, fakeClient.Create(context.TODO(), hubBackupContent("bc1", "namespace")))
	assert.NoError(t, fakeClient.Create(context.TODO(),
		managedClusterWithLabels("cluster1", map[string]string{"common": "true"})))
	reader := &fakeSeedImageReader{err: fmt.Errorf("registry unreachable")}
	recorder := record.NewFakeRecorder(10)
	reconciler := IBGUReconciler{
		Client: fakeClient, Scheme: testscheme, Log: logr.Discard(),
		Recorder: recorder, SeedImageReader: reader,
	}
	return ibgu, reader, recorder, reconciler
}

// TestSeedUnreadableIsTerminalImmediately proves AC 1: an unreadable seed registry
// on the first reconcile blocks the IBGU terminally (Succeeded=False,
// SeedImageUnreadable), creates no CGU, and does not requeue — there is no grace
// window. It is terminal whether the IBGU was created just now or long ago.
func TestSeedUnreadableIsTerminalImmediately(t *testing.T) {
	for _, createdAgo := range []time.Duration{0, 10 * time.Minute} {
		t.Run(fmt.Sprintf("created %s ago", createdAgo), func(t *testing.T) {
			ibgu, reader, _, reconciler := newUnreadableSeedReconciler(t)
			ibgu.CreationTimestamp = metav1.NewTime(metav1.Now().Add(-createdAgo))

			requeue, err := reconciler.ensureManifests(context.TODO(), ibgu)
			assert.NoError(t, err)
			assert.False(t, requeue) // terminal: no requeue
			assert.True(t, reader.called)

			succeeded := succeededCondition(ibgu)
			assert.NotNil(t, succeeded)
			assert.Equal(t, metav1.ConditionFalse, succeeded.Status)
			assert.Equal(t, string(utils.ConditionReasons.SeedImageUnreadable), succeeded.Reason)
			assert.Equal(t, string(utils.ConditionReasons.SeedImageUnreadable), progressingCondition(ibgu).Reason)

			cgu := &v1alpha1.ClusterGroupUpgrade{}
			err = reconciler.Get(context.TODO(),
				types.NamespacedName{Name: "name-prep-0", Namespace: "namespace"}, cgu)
			assert.True(t, apierrors.IsNotFound(err))
		})
	}
}

// TestRetryAnnotationHasNoEffect proves AC 5: the removed
// lcm.openshift.io/ibgu-preflight-retry-seconds annotation is ignored. Whatever its
// value, a pre-flight failure is terminal on the first reconcile and no
// InvalidPreflightRetrySeconds event is raised.
func TestRetryAnnotationHasNoEffect(t *testing.T) {
	for _, value := range []string{"0", "600", "9999", "nope", "-5"} {
		t.Run("annotation "+value, func(t *testing.T) {
			ibgu, _, recorder, reconciler := newUnreadableSeedReconciler(t)
			ibgu.CreationTimestamp = metav1.Now()
			ibgu.SetAnnotations(map[string]string{"lcm.openshift.io/ibgu-preflight-retry-seconds": value})

			requeue, err := reconciler.ensureManifests(context.TODO(), ibgu)
			assert.NoError(t, err)
			// Terminal on the first failure regardless of the annotation value.
			assert.False(t, requeue)
			succeeded := succeededCondition(ibgu)
			assert.NotNil(t, succeeded)
			assert.Equal(t, string(utils.ConditionReasons.SeedImageUnreadable), succeeded.Reason)

			// The annotation is not parsed, so no invalid-value Warning is ever raised.
			for len(recorder.Events) > 0 {
				e := <-recorder.Events
				assert.NotContains(t, e, "InvalidPreflightRetrySeconds")
			}
		})
	}
}

// TestBackupContentFoundViaUncachedReader proves AC 3: the pre-flight gates read
// through the uncached APIReader, so a BackupContent (or a matching ManagedCluster)
// that exists on the API server but has not yet landed in the cached client is found
// and the gate passes instead of producing a false terminal block. The gates are
// exercised directly because they are where the false block originates; the Prep-CGU
// manifest builders read through the same uncached reader (see getBackupContentManifests
// / getConfigMapManifests / getSecretManifest), so the pass path stays consistent
// within the one reconcile the gate passed.
func TestBackupContentFoundViaUncachedReader(t *testing.T) {
	t.Run("reference gate finds a cache-lagged BackupContent", func(t *testing.T) {
		ibgu := backupContentIBGU() // Prep plan with backupContent bc1

		// Cached client: bc1 absent (cache lag). Uncached reader: bc1 present.
		cachedClient, err := getFakeClientFromObjects()
		assert.NoError(t, err)
		uncachedReader, err := getFakeClientFromObjects()
		assert.NoError(t, err)
		assert.NoError(t, uncachedReader.Create(context.TODO(), hubBackupContent("bc1", "namespace")))

		reconciler := IBGUReconciler{
			Client: cachedClient, APIReader: uncachedReader, Scheme: testscheme, Log: logr.Discard(),
		}
		outcome, err := reconciler.ensurePrepReferencesResolve(context.TODO(), ibgu)
		assert.NoError(t, err)
		// The gate passed via the uncached read: no false UnresolvableDenpendency block.
		assert.Equal(t, preflightPass, outcome.class)

		// Control: with only the cached client (bc1 absent) the gate blocks terminally,
		// confirming the uncached reader is what defeats the lag.
		cachedOnly := IBGUReconciler{Client: cachedClient, Scheme: testscheme, Log: logr.Discard()}
		cachedOutcome, err := cachedOnly.ensurePrepReferencesResolve(context.TODO(), ibgu)
		assert.NoError(t, err)
		assert.Equal(t, preflightTerminal, cachedOutcome.class)
		assert.Equal(t, utils.ConditionReasons.UnresolvableDenpendency, cachedOutcome.reason)
	})

	t.Run("empty-set gate finds a cache-lagged ManagedCluster", func(t *testing.T) {
		ibgu := newIBGUForResolution([]ibguv1alpha1.PlanItem{{Actions: []string{ibguv1alpha1.Prep}}})

		// Cached client: no ManagedCluster. Uncached reader: the matching cluster.
		cachedClient, err := getFakeClientFromObjects()
		assert.NoError(t, err)
		uncachedReader, err := getFakeClientFromObjects()
		assert.NoError(t, err)
		assert.NoError(t, uncachedReader.Create(context.TODO(),
			managedClusterWithLabels("cluster1", map[string]string{"common": "true"})))

		reconciler := IBGUReconciler{
			Client: cachedClient, APIReader: uncachedReader, Scheme: testscheme, Log: logr.Discard(),
		}
		outcome, err := reconciler.ensureNonEmptyClusterSet(context.TODO(), ibgu)
		assert.NoError(t, err)
		// The gate passed via the uncached read: no false NoClustersMatchSelector block.
		assert.Equal(t, preflightPass, outcome.class)

		// Control: with only the cached client (no cluster) the gate blocks terminally.
		cachedOnly := IBGUReconciler{Client: cachedClient, Scheme: testscheme, Log: logr.Discard()}
		cachedOutcome, err := cachedOnly.ensureNonEmptyClusterSet(context.TODO(), ibgu)
		assert.NoError(t, err)
		assert.Equal(t, preflightTerminal, cachedOutcome.class)
		assert.Equal(t, utils.ConditionReasons.NoClustersMatchSelector, cachedOutcome.reason)
	})

	t.Run("end-to-end: Prep CGU is created from uncached references under cache lag", func(t *testing.T) {
		// The whole pre-first-CGU path (gates AND Prep-manifest building) must read the
		// uncached reader, so a BackupContent + ManagedCluster that exist only on the API
		// server (cache still lagging) produce a Prep CGU on the first reconcile rather
		// than a false block or a silently incomplete CGU.
		ibgu := backupContentIBGU() // Prep plan with backupContent bc1
		ibgu.CreationTimestamp = metav1.Now()

		// Cached client is empty (cache lag); the uncached reader has both objects.
		cachedClient, err := getFakeClientFromObjects()
		assert.NoError(t, err)
		uncachedReader, err := getFakeClientFromObjects()
		assert.NoError(t, err)
		assert.NoError(t, uncachedReader.Create(context.TODO(), hubBackupContent("bc1", "namespace")))
		assert.NoError(t, uncachedReader.Create(context.TODO(),
			managedClusterWithLabels("cluster1", map[string]string{"common": "true"})))

		reconciler := IBGUReconciler{
			Client: cachedClient, APIReader: uncachedReader, Scheme: testscheme, Log: logr.Discard(),
			Recorder:        record.NewFakeRecorder(10),
			SeedImageReader: &fakeSeedImageReader{format: lcav1.BackupContentFirstSeedFormatVersion},
		}

		requeue, err := reconciler.ensureManifests(context.TODO(), ibgu)
		assert.NoError(t, err)
		assert.False(t, requeue)
		// No false terminal block.
		assert.Nil(t, succeededCondition(ibgu))
		// The Prep CGU is created on the frozen set resolved via the uncached reader.
		cgu := &v1alpha1.ClusterGroupUpgrade{}
		assert.NoError(t, reconciler.Get(context.TODO(),
			types.NamespacedName{Name: "name-prep-0", Namespace: "namespace"}, cgu))
		assert.Equal(t, []string{"cluster1"}, cgu.Spec.Clusters)
	})
}
