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
		expectBlocked  bool
		expectReason   utils.ConditionReason
		expectReadCall bool
		expectEvent    bool
	}{
		{
			name:           "no backupContent passes through without reading the image",
			mutate:         func(ibgu *ibguv1alpha1.ImageBasedGroupUpgrade) { ibgu.Spec.IBUSpec.BackupContent = nil },
			reader:         &fakeSeedImageReader{format: preBumpFormat},
			expectBlocked:  false,
			expectReadCall: false,
		},
		{
			name:           "backupContent with a pre-bump seed is rejected",
			reader:         &fakeSeedImageReader{format: preBumpFormat},
			expectBlocked:  true,
			expectReason:   utils.ConditionReasons.BackupContentUnsupportedSeed,
			expectReadCall: true,
		},
		{
			name:           "backupContent with a supported seed proceeds",
			reader:         &fakeSeedImageReader{format: lcav1.BackupContentFirstSeedFormatVersion},
			expectBlocked:  false,
			expectReadCall: true,
		},
		{
			name:           "unreadable image label blocks fail-closed",
			reader:         &fakeSeedImageReader{err: fmt.Errorf("registry unreachable")},
			expectBlocked:  true,
			expectReason:   utils.ConditionReasons.SeedImageUnreadable,
			expectReadCall: true,
		},
		{
			name: "opt-out annotation skips the check and reports an event",
			mutate: func(ibgu *ibguv1alpha1.ImageBasedGroupUpgrade) {
				ibgu.SetAnnotations(map[string]string{utils.IBGUSkipSeedCompatibilityAnnotation: ""})
			},
			reader:         &fakeSeedImageReader{format: preBumpFormat},
			expectBlocked:  false,
			expectReadCall: false,
			expectEvent:    true,
		},
		{
			name: "missing pull secret blocks as unreadable",
			mutate: func(ibgu *ibguv1alpha1.ImageBasedGroupUpgrade) {
				ibgu.Spec.IBUSpec.SeedImageRef.PullSecretRef = &lcav1.PullSecretRef{Name: "missing"}
			},
			reader:         &fakeSeedImageReader{format: lcav1.BackupContentFirstSeedFormatVersion},
			expectBlocked:  true,
			expectReason:   utils.ConditionReasons.SeedImageUnreadable,
			expectReadCall: false,
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
			expectBlocked:  false,
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

			blocked, err := reconciler.ensureSeedFormatCompatible(context.TODO(), ibgu)
			assert.NoError(t, err)
			assert.Equal(t, test.expectBlocked, blocked)
			assert.Equal(t, test.expectReadCall, test.reader.called)

			cond := progressingCondition(ibgu)
			if test.expectReason != "" {
				assert.NotNil(t, cond)
				assert.Equal(t, metav1.ConditionFalse, cond.Status)
				assert.Equal(t, string(test.expectReason), cond.Reason)
			} else {
				// A pass-through or skip must not set a blocking seed condition.
				if cond != nil {
					assert.NotEqual(t, string(utils.ConditionReasons.SeedImageUnreadable), cond.Reason)
					assert.NotEqual(t, string(utils.ConditionReasons.BackupContentUnsupportedSeed), cond.Reason)
				}
			}

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
// with backoff — rather than being recorded as a fail-closed "unreadable" block.
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

	blocked, err := reconciler.ensureSeedFormatCompatible(context.TODO(), ibgu)
	assert.Error(t, err)
	assert.False(t, blocked)
	assert.False(t, reader.called)
	// No fail-closed condition is recorded for a transient fault.
	assert.Nil(t, progressingCondition(ibgu))
}

// TestEnsureManifestsSeedFormatGate verifies the seed-format gate as wired into
// ensureManifests: it blocks before any CGU is created, runs ahead of the
// reference gate, and only runs before the first CGU.
func TestEnsureManifestsSeedFormatGate(t *testing.T) {
	t.Run("backupContent with pre-bump seed is blocked before any CGU", func(t *testing.T) {
		ibgu := backupContentIBGU()

		fakeClient, err := getFakeClientFromObjects()
		assert.NoError(t, err)
		assert.NoError(t, fakeClient.Create(context.TODO(),
			managedClusterWithLabels("cluster1", map[string]string{"common": "true"})))

		reconciler := IBGUReconciler{
			Client: fakeClient, Scheme: testscheme, Log: logr.Discard(),
			SeedImageReader: &fakeSeedImageReader{format: preBumpFormat},
		}
		blocked, err := reconciler.ensureManifests(context.TODO(), ibgu)
		assert.NoError(t, err)
		assert.True(t, blocked)

		cond := progressingCondition(ibgu)
		assert.NotNil(t, cond)
		assert.Equal(t, metav1.ConditionFalse, cond.Status)
		assert.Equal(t, string(utils.ConditionReasons.BackupContentUnsupportedSeed), cond.Reason)

		cgu := &v1alpha1.ClusterGroupUpgrade{}
		err = reconciler.Get(context.TODO(),
			types.NamespacedName{Name: "name-prep-0", Namespace: "namespace"}, cgu)
		assert.True(t, apierrors.IsNotFound(err))
	})

	t.Run("seed gate runs before the reference gate", func(t *testing.T) {
		// Both the seed (pre-bump) and the reference (missing BackupContent) would
		// block; the seed reason must win because the seed gate runs first.
		ibgu := backupContentIBGU()

		fakeClient, err := getFakeClientFromObjects()
		assert.NoError(t, err)
		assert.NoError(t, fakeClient.Create(context.TODO(),
			managedClusterWithLabels("cluster1", map[string]string{"common": "true"})))

		reconciler := IBGUReconciler{
			Client: fakeClient, Scheme: testscheme, Log: logr.Discard(),
			SeedImageReader: &fakeSeedImageReader{format: preBumpFormat},
		}
		blocked, err := reconciler.ensureManifests(context.TODO(), ibgu)
		assert.NoError(t, err)
		assert.True(t, blocked)

		cond := progressingCondition(ibgu)
		assert.NotNil(t, cond)
		assert.Equal(t, string(utils.ConditionReasons.BackupContentUnsupportedSeed), cond.Reason)
		assert.NotEqual(t, string(utils.ConditionReasons.UnresolvableDenpendency), cond.Reason)
	})

	t.Run("compatible seed falls through to the reference gate", func(t *testing.T) {
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

		// With a compatible seed and a resolvable reference, the Prep CGU is created.
		cgu := &v1alpha1.ClusterGroupUpgrade{}
		err = reconciler.Get(context.TODO(),
			types.NamespacedName{Name: "name-prep-0", Namespace: "namespace"}, cgu)
		assert.NoError(t, err)
	})

	t.Run("in-progress IBGU is not re-checked", func(t *testing.T) {
		ibgu := backupContentIBGU()

		fakeClient, err := getFakeClientFromObjects()
		assert.NoError(t, err)
		// A CGU already exists for plan item 0, so neither pre-flight gate runs.
		prepCGU := completedPrepCGU([]string{"cluster1"})
		assert.NoError(t, fakeClient.Create(context.TODO(), &prepCGU))
		assert.NoError(t, fakeClient.Create(context.TODO(),
			managedClusterWithLabels("cluster1", map[string]string{"common": "true"})))

		reader := &fakeSeedImageReader{format: preBumpFormat}
		reconciler := IBGUReconciler{
			Client: fakeClient, Scheme: testscheme, Log: logr.Discard(),
			SeedImageReader: reader,
		}
		blocked, err := reconciler.ensureManifests(context.TODO(), ibgu)
		assert.NoError(t, err)
		assert.False(t, blocked)
		assert.False(t, reader.called)

		cond := progressingCondition(ibgu)
		assert.NotNil(t, cond)
		assert.NotEqual(t, string(utils.ConditionReasons.BackupContentUnsupportedSeed), cond.Reason)
	})

	// A backupContent IBGU that already reported completion without ever creating a
	// CGU (plan item 0 resolved to zero eligible clusters) must not be re-gated on a
	// later reconcile: an unreadable seed must not overwrite Completed with a block.
	t.Run("completed IBGU with no CGU is not re-gated", func(t *testing.T) {
		ibgu := backupContentIBGU()
		utils.SetStatusCondition(&ibgu.Status.Conditions,
			utils.ConditionTypes.Progressing, utils.ConditionReasons.Completed,
			metav1.ConditionFalse, "All plan steps are completed")

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
		// The seed gate is skipped: no registry read.
		assert.False(t, reader.called)

		// Completion is preserved; the unreadable seed did not overwrite it.
		cond := progressingCondition(ibgu)
		assert.NotNil(t, cond)
		assert.Equal(t, string(utils.ConditionReasons.Completed), cond.Reason)
		assert.NotEqual(t, string(utils.ConditionReasons.SeedImageUnreadable), cond.Reason)

		// No CGU is created for the completed, empty rollout.
		cgu := &v1alpha1.ClusterGroupUpgrade{}
		err = reconciler.Get(context.TODO(),
			types.NamespacedName{Name: "name-prep-0", Namespace: "namespace"}, cgu)
		assert.True(t, apierrors.IsNotFound(err))
	})
}
