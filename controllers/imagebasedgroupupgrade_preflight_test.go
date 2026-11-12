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
	"strings"
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
			name:           "unreadable image label is transient (fail-closed)",
			reader:         &fakeSeedImageReader{err: fmt.Errorf("registry unreachable")},
			expectClass:    preflightTransient,
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
			name: "missing pull secret is transient (unreadable)",
			mutate: func(ibgu *ibguv1alpha1.ImageBasedGroupUpgrade) {
				ibgu.Spec.IBUSpec.SeedImageRef.PullSecretRef = &lcav1.PullSecretRef{Name: "missing"}
			},
			reader:         &fakeSeedImageReader{format: lcav1.BackupContentFirstSeedFormatVersion},
			expectClass:    preflightTransient,
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

// TestPreflightRetrySecondsParsing covers the annotation parser: a valid value is
// used, an absent one yields the default, and an invalid/negative one falls back to
// the default and raises a Warning event.
func TestPreflightRetrySecondsParsing(t *testing.T) {
	tests := []struct {
		name        string
		annotation  *string
		expect      int
		expectEvent bool
	}{
		{name: "absent uses the default", annotation: nil, expect: utils.DefaultPreflightRetrySeconds},
		{name: "valid value is used", annotation: ptr("120"), expect: 120},
		{name: "zero is used (terminal on first failure)", annotation: ptr("0"), expect: 0},
		{name: "non-integer falls back to default with event", annotation: ptr("soon"), expect: utils.DefaultPreflightRetrySeconds, expectEvent: true},
		{name: "negative falls back to default with event", annotation: ptr("-5"), expect: utils.DefaultPreflightRetrySeconds, expectEvent: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ibgu := newIBGUForResolution([]ibguv1alpha1.PlanItem{{Actions: []string{ibguv1alpha1.Prep}}})
			if test.annotation != nil {
				ibgu.SetAnnotations(map[string]string{utils.IBGUPreflightRetrySecondsAnnotation: *test.annotation})
			}
			recorder := record.NewFakeRecorder(10)
			reconciler := IBGUReconciler{Scheme: testscheme, Log: logr.Discard(), Recorder: recorder}

			assert.Equal(t, test.expect, reconciler.preflightRetrySeconds(ibgu))

			if test.expectEvent {
				select {
				case e := <-recorder.Events:
					assert.Contains(t, e, "Warning")
					assert.Contains(t, e, "InvalidPreflightRetrySeconds")
				default:
					t.Fatal("expected a Warning event for the invalid annotation, got none")
				}
			} else {
				assert.Len(t, recorder.Events, 0)
			}
		})
	}
}

// ptr returns a pointer to v, for optional test fields.
func ptr[T any](v T) *T { return &v }

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
		ibgu.CreationTimestamp = metav1.Now() // within the grace window

		fakeClient, err := getFakeClientFromObjects()
		assert.NoError(t, err)
		assert.NoError(t, fakeClient.Create(context.TODO(),
			managedClusterWithLabels("cluster1", map[string]string{"common": "true"})))

		reader := &fakeSeedImageReader{format: preBumpFormat}
		reconciler := IBGUReconciler{
			Client: fakeClient, Scheme: testscheme, Log: logr.Discard(),
			SeedImageReader: reader,
		}
		blocked, err := reconciler.ensureManifests(context.TODO(), ibgu)
		assert.NoError(t, err)
		// Reference is transient; within grace it requeues.
		assert.True(t, blocked)
		// The seed gate never ran: no registry read.
		assert.False(t, reader.called)

		cond := progressingCondition(ibgu)
		assert.NotNil(t, cond)
		assert.Equal(t, string(utils.ConditionReasons.UnresolvableDenpendency), cond.Reason)
		assert.NotEqual(t, string(utils.ConditionReasons.BackupContentUnsupportedSeed), cond.Reason)
		// Transient within grace does not set Succeeded.
		assert.Nil(t, succeededCondition(ibgu))
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

// TestPreflightGraceWindow exercises the transient grace window end-to-end through
// ensureManifests, using the seed-unreadable transient as the vehicle: within the
// window the IBGU retries (requeue, no Succeeded); once the window has elapsed it
// becomes terminal (Succeeded=False); the annotation changes the window; an invalid
// annotation falls back to the default and raises a Warning event; and 0 is terminal
// on the first failure.
func TestPreflightGraceWindow(t *testing.T) {
	// newUnreadableSeedIBGU builds a backupContent Prep IBGU with a resolvable
	// reference and a matching cluster, so the empty-set and reference gates pass and
	// the seed gate (unreadable) is the gate under test.
	newUnreadableSeedIBGU := func(t *testing.T) (*ibguv1alpha1.ImageBasedGroupUpgrade, *fakeSeedImageReader, *record.FakeRecorder, IBGUReconciler) {
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

	t.Run("within grace: retries without a terminal block", func(t *testing.T) {
		ibgu, reader, _, reconciler := newUnreadableSeedIBGU(t)
		ibgu.CreationTimestamp = metav1.Now()

		blocked, err := reconciler.ensureManifests(context.TODO(), ibgu)
		assert.NoError(t, err)
		assert.True(t, blocked) // requeue
		assert.True(t, reader.called)
		assert.Nil(t, succeededCondition(ibgu))
		assert.Equal(t, string(utils.ConditionReasons.SeedImageUnreadable), progressingCondition(ibgu).Reason)
	})

	t.Run("past grace: becomes terminal with the last reason", func(t *testing.T) {
		ibgu, _, _, reconciler := newUnreadableSeedIBGU(t)
		ibgu.CreationTimestamp = metav1.NewTime(metav1.Now().Add(-10 * time.Minute))

		blocked, err := reconciler.ensureManifests(context.TODO(), ibgu)
		assert.NoError(t, err)
		assert.False(t, blocked) // no requeue
		succeeded := succeededCondition(ibgu)
		assert.NotNil(t, succeeded)
		assert.Equal(t, metav1.ConditionFalse, succeeded.Status)
		assert.Equal(t, string(utils.ConditionReasons.SeedImageUnreadable), succeeded.Reason)
	})

	t.Run("annotation lengthens the window", func(t *testing.T) {
		ibgu, _, _, reconciler := newUnreadableSeedIBGU(t)
		// Created 6 minutes ago: past the default 5m window, but within a 10m override.
		ibgu.CreationTimestamp = metav1.NewTime(metav1.Now().Add(-6 * time.Minute))
		ibgu.SetAnnotations(map[string]string{utils.IBGUPreflightRetrySecondsAnnotation: "600"})

		blocked, err := reconciler.ensureManifests(context.TODO(), ibgu)
		assert.NoError(t, err)
		assert.True(t, blocked) // still within the extended window
		assert.Nil(t, succeededCondition(ibgu))
	})

	t.Run("invalid annotation falls back to default and warns", func(t *testing.T) {
		ibgu, _, recorder, reconciler := newUnreadableSeedIBGU(t)
		ibgu.CreationTimestamp = metav1.Now()
		ibgu.SetAnnotations(map[string]string{utils.IBGUPreflightRetrySecondsAnnotation: "nope"})

		blocked, err := reconciler.ensureManifests(context.TODO(), ibgu)
		assert.NoError(t, err)
		assert.True(t, blocked) // within the default window
		assert.Nil(t, succeededCondition(ibgu))

		var sawWarning bool
		for len(recorder.Events) > 0 {
			if e := <-recorder.Events; strings.Contains(e, "InvalidPreflightRetrySeconds") {
				sawWarning = true
			}
		}
		assert.True(t, sawWarning, "expected a Warning event for the invalid annotation")
	})

	t.Run("zero is terminal on the first failure", func(t *testing.T) {
		ibgu, _, _, reconciler := newUnreadableSeedIBGU(t)
		ibgu.CreationTimestamp = metav1.Now()
		ibgu.SetAnnotations(map[string]string{utils.IBGUPreflightRetrySecondsAnnotation: "0"})

		blocked, err := reconciler.ensureManifests(context.TODO(), ibgu)
		assert.NoError(t, err)
		assert.False(t, blocked) // terminal immediately
		succeeded := succeededCondition(ibgu)
		assert.NotNil(t, succeeded)
		assert.Equal(t, string(utils.ConditionReasons.SeedImageUnreadable), succeeded.Reason)
	})
}
