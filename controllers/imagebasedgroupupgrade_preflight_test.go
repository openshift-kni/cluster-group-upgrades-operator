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
	clusterv1 "open-cluster-management.io/api/cluster/v1"
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

func (f *fakeSeedImageReader) ReadSeedFormatVersion(
	_ context.Context, imageRef string, dockerConfigJSON, clusterCAs []byte,
) (int, error) {
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
	// bumpedImage is the current seed format (SeedFormatVersion); a spoke with no
	// claim expects the pre-bump format (preBumpFormat), so it mismatches a bumped
	// image and matches a pre-bump one.
	const bumpedImage = lcav1.SeedFormatVersion

	tests := []struct {
		name           string
		mutate         func(ibgu *ibguv1alpha1.ImageBasedGroupUpgrade)
		reader         *fakeSeedImageReader
		pullSecret     *corev1.Secret
		baseClusters   []clusterv1.ManagedCluster
		expectBlock    bool
		expectReason   utils.ConditionReason
		expectReadCall bool
		expectEvent    bool
	}{
		{
			name:           "no backupContent still reads the image and runs the spoke check",
			mutate:         func(ibgu *ibguv1alpha1.ImageBasedGroupUpgrade) { ibgu.Spec.IBUSpec.BackupContent = nil },
			reader:         &fakeSeedImageReader{format: preBumpFormat},
			baseClusters:   []clusterv1.ManagedCluster{*availableClusterWithSeedFormat("cluster1", nil, preBumpFormat)},
			expectBlock:    false,
			expectReadCall: true,
		},
		{
			name:           "backupContent with a pre-bump seed is terminal",
			reader:         &fakeSeedImageReader{format: preBumpFormat},
			expectBlock:    true,
			expectReason:   utils.ConditionReasons.BackupContentUnsupportedSeed,
			expectReadCall: true,
		},
		{
			name:           "backupContent with a supported seed and matching spoke proceeds",
			reader:         &fakeSeedImageReader{format: lcav1.BackupContentFirstSeedFormatVersion},
			baseClusters:   []clusterv1.ManagedCluster{*availableClusterWithSeedFormat("cluster1", nil, lcav1.BackupContentFirstSeedFormatVersion)},
			expectBlock:    false,
			expectReadCall: true,
		},
		{
			name:           "spoke whose claim mismatches the image is terminal",
			reader:         &fakeSeedImageReader{format: bumpedImage},
			baseClusters:   []clusterv1.ManagedCluster{*availableClusterWithSeedFormat("cluster1", nil, preBumpFormat)},
			expectBlock:    true,
			expectReason:   utils.ConditionReasons.SeedFormatMismatch,
			expectReadCall: true,
		},
		{
			name:           "unavailable spoke is terminal",
			reader:         &fakeSeedImageReader{format: bumpedImage},
			baseClusters:   []clusterv1.ManagedCluster{*managedClusterWithLabels("cluster1", nil)},
			expectBlock:    true,
			expectReason:   utils.ConditionReasons.SpokeClusterUnavailable,
			expectReadCall: true,
		},
		{
			name:           "unreadable image label is terminal (fail-closed)",
			reader:         &fakeSeedImageReader{err: fmt.Errorf("registry unreachable")},
			expectBlock:    true,
			expectReason:   utils.ConditionReasons.SeedImageUnreadable,
			expectReadCall: true,
		},
		{
			name: "opt-out annotation skips the check and reports an event",
			mutate: func(ibgu *ibguv1alpha1.ImageBasedGroupUpgrade) {
				ibgu.SetAnnotations(map[string]string{utils.IBGUSkipSeedCompatibilityAnnotation: ""})
			},
			reader:         &fakeSeedImageReader{format: preBumpFormat},
			expectBlock:    false,
			expectReadCall: false,
			expectEvent:    true,
		},
		{
			name: "missing pull secret is terminal (unreadable)",
			mutate: func(ibgu *ibguv1alpha1.ImageBasedGroupUpgrade) {
				ibgu.Spec.IBUSpec.SeedImageRef.PullSecretRef = &lcav1.PullSecretRef{Name: "missing"}
			},
			reader:         &fakeSeedImageReader{format: lcav1.BackupContentFirstSeedFormatVersion},
			expectBlock:    true,
			expectReason:   utils.ConditionReasons.SeedImageUnreadable,
			expectReadCall: false,
		},
		{
			name: "recovery-only plan is not gated on a readable registry",
			mutate: func(ibgu *ibguv1alpha1.ImageBasedGroupUpgrade) {
				ibgu.Spec.Plan = []ibguv1alpha1.PlanItem{{Actions: []string{ibguv1alpha1.Rollback}}}
			},
			reader:         &fakeSeedImageReader{err: fmt.Errorf("registry unreachable")},
			expectBlock:    false,
			expectReadCall: false,
		},
		{
			name: "upgrade-only plan is still checked",
			mutate: func(ibgu *ibguv1alpha1.ImageBasedGroupUpgrade) {
				ibgu.Spec.Plan = []ibguv1alpha1.PlanItem{{Actions: []string{ibguv1alpha1.Upgrade}}}
			},
			reader:         &fakeSeedImageReader{format: preBumpFormat},
			expectBlock:    true,
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
			expectBlock:    false,
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
				Client: fakeClient, APIReader: fakeClient,
				Scheme:          testscheme,
				Log:             logr.Discard(),
				Recorder:        recorder,
				SeedImageReader: test.reader,
			}

			block, err := reconciler.ensureSeedFormatCompatible(context.TODO(), ibgu, test.baseClusters)
			assert.NoError(t, err)
			if test.expectBlock {
				assert.NotNil(t, block)
				if test.expectReason != "" {
					assert.Equal(t, test.expectReason, block.reason)
				}
			} else {
				assert.Nil(t, block)
			}
			assert.Equal(t, test.expectReadCall, test.reader.called)
			// The gate only classifies; it never writes a condition itself (that is
			// ensureManifests's job once it has the block).
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
		Client: faultyClient, APIReader: faultyClient, Scheme: testscheme, Log: logr.Discard(),
		SeedImageReader: reader,
	}

	block, err := reconciler.ensureSeedFormatCompatible(context.TODO(), ibgu, nil)
	assert.Error(t, err)
	assert.Nil(t, block)
	assert.False(t, reader.called)
	// No condition is recorded for a transient fault.
	assert.Nil(t, progressingCondition(ibgu))
	assert.Nil(t, succeededCondition(ibgu))
}

// TestEvaluateSpokeFormats covers the pure spoke-side resolution and
// comparison: each branch of expectedSpokeFormat/evaluateSpokeAvailabilityAndFormat
// with no I/O. preBumpFormat (BackupContentFirstSeedFormatVersion-1) is the format a
// claimless spoke expects.
func TestEvaluateSpokeFormats(t *testing.T) {
	const bumpedImage = lcav1.SeedFormatVersion                        // current format
	const preBumpImage = lcav1.BackupContentFirstSeedFormatVersion - 1 // == preBumpFormat

	// available builds an Available=True ManagedCluster carrying the given raw
	// seed-format claim value; an empty value means no claim is published.
	available := func(name, claimValue string) clusterv1.ManagedCluster {
		mc := clusterv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Status: clusterv1.ManagedClusterStatus{
				Conditions: []metav1.Condition{{
					Type:               clusterv1.ManagedClusterConditionAvailable,
					Status:             metav1.ConditionTrue,
					Reason:             "ManagedClusterAvailable",
					LastTransitionTime: metav1.Now(),
				}},
			},
		}
		if claimValue != "" {
			mc.Status.ClusterClaims = []clusterv1.ManagedClusterClaim{
				{Name: lcav1.SeedFormatClusterClaimName, Value: claimValue},
			}
		}
		return mc
	}
	// unavailable builds a ManagedCluster with no Available=True condition.
	unavailable := func(name string) clusterv1.ManagedCluster {
		return clusterv1.ManagedCluster{ObjectMeta: metav1.ObjectMeta{Name: name}}
	}

	tests := []struct {
		name         string
		imageFormat  int
		clusters     []clusterv1.ManagedCluster
		expectBlock  bool
		expectReason utils.ConditionReason
		expectNames  []string // substrings the block message must contain
		absentNames  []string // substrings the block message must not contain
	}{
		{
			name:        "available spokes with a matching claim pass",
			imageFormat: bumpedImage,
			clusters:    []clusterv1.ManagedCluster{available("c1", "5"), available("c2", "5")},
			expectBlock: false,
		},
		{
			name:        "empty cluster set passes",
			imageFormat: bumpedImage,
			clusters:    nil,
			expectBlock: false,
		},
		{
			name:         "unavailable spoke blocks and is named",
			imageFormat:  bumpedImage,
			clusters:     []clusterv1.ManagedCluster{unavailable("c1")},
			expectBlock:  true,
			expectReason: utils.ConditionReasons.SpokeClusterUnavailable,
			expectNames:  []string{"c1"},
		},
		{
			name:         "claim that mismatches the image blocks, naming the cluster and expected format",
			imageFormat:  bumpedImage,
			clusters:     []clusterv1.ManagedCluster{available("c1", "4")},
			expectBlock:  true,
			expectReason: utils.ConditionReasons.SeedFormatMismatch,
			expectNames:  []string{"c1", "expects 4"},
		},
		{
			name:         "claim absent against a bumped image mismatches (expects pre-bump)",
			imageFormat:  bumpedImage,
			clusters:     []clusterv1.ManagedCluster{available("c1", "")},
			expectBlock:  true,
			expectReason: utils.ConditionReasons.SeedFormatMismatch,
			expectNames:  []string{"c1", fmt.Sprintf("expects %d", preBumpImage)},
		},
		{
			name:        "claim absent against a pre-bump image passes",
			imageFormat: preBumpImage,
			clusters:    []clusterv1.ManagedCluster{available("c1", "")},
			expectBlock: false,
		},
		{
			name:         "non-integer claim mismatches (fail-closed)",
			imageFormat:  bumpedImage,
			clusters:     []clusterv1.ManagedCluster{available("c1", "not-a-number")},
			expectBlock:  true,
			expectReason: utils.ConditionReasons.SeedFormatMismatch,
			expectNames:  []string{"c1"},
		},
		{
			name:         "multiple mismatched spokes are all named",
			imageFormat:  bumpedImage,
			clusters:     []clusterv1.ManagedCluster{available("c1", "4"), available("c2", "")},
			expectBlock:  true,
			expectReason: utils.ConditionReasons.SeedFormatMismatch,
			expectNames:  []string{"c1", "c2"},
		},
		{
			name:         "unavailable takes precedence over mismatch; only unavailable spokes are named",
			imageFormat:  bumpedImage,
			clusters:     []clusterv1.ManagedCluster{available("bad-format", "4"), unavailable("down1"), unavailable("down2")},
			expectBlock:  true,
			expectReason: utils.ConditionReasons.SpokeClusterUnavailable,
			expectNames:  []string{"down1", "down2"},
			absentNames:  []string{"bad-format"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			block := evaluateSpokeAvailabilityAndFormat(test.imageFormat, test.clusters)
			if !test.expectBlock {
				assert.Nil(t, block)
				return
			}
			assert.NotNil(t, block)
			assert.Equal(t, test.expectReason, block.reason)
			for _, name := range test.expectNames {
				assert.Contains(t, block.message, name)
			}
			for _, name := range test.absentNames {
				assert.NotContains(t, block.message, name)
			}
		})
	}
}

// TestEvaluateSpokeFormatsBoundsMessage verifies that on a large fleet the terminal
// block message stays within the IBGU CRD's 32768-character condition-message limit:
// summarizeClusters truncates the name list and appends a "(+N more)" suffix instead
// of joining every name, so the status write that records the terminal reason is never
// rejected for being too long. The first blocking cluster is still named so the message
// remains actionable. Both terminal branches (unavailable and mismatch) are covered.
func TestEvaluateSpokeFormatsBoundsMessage(t *testing.T) {
	const crdConditionMessageLimit = 32768
	const fleetSize = 5000

	unavailableFleet := make([]clusterv1.ManagedCluster, 0, fleetSize)
	mismatchedFleet := make([]clusterv1.ManagedCluster, 0, fleetSize)
	for i := 0; i < fleetSize; i++ {
		// Realistic DNS-subdomain-style spoke names so the joined list is long.
		name := fmt.Sprintf("spoke-cluster-with-a-fairly-long-name-%05d.example.com", i)
		unavailableFleet = append(unavailableFleet,
			clusterv1.ManagedCluster{ObjectMeta: metav1.ObjectMeta{Name: name}})
		mismatched := clusterv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Status: clusterv1.ManagedClusterStatus{
				Conditions: []metav1.Condition{{
					Type:               clusterv1.ManagedClusterConditionAvailable,
					Status:             metav1.ConditionTrue,
					Reason:             "ManagedClusterAvailable",
					LastTransitionTime: metav1.Now(),
				}},
				// Claim value that mismatches the image format below.
				ClusterClaims: []clusterv1.ManagedClusterClaim{
					{Name: lcav1.SeedFormatClusterClaimName, Value: "4"},
				},
			},
		}
		mismatchedFleet = append(mismatchedFleet, mismatched)
	}

	tests := []struct {
		name         string
		clusters     []clusterv1.ManagedCluster
		expectReason utils.ConditionReason
	}{
		{
			name:         "unavailable branch is bounded",
			clusters:     unavailableFleet,
			expectReason: utils.ConditionReasons.SpokeClusterUnavailable,
		},
		{
			name:         "mismatch branch is bounded",
			clusters:     mismatchedFleet,
			expectReason: utils.ConditionReasons.SeedFormatMismatch,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			block := evaluateSpokeAvailabilityAndFormat(lcav1.SeedFormatVersion, test.clusters)
			assert.NotNil(t, block)
			assert.Equal(t, test.expectReason, block.reason)
			assert.LessOrEqual(t, len(block.message), crdConditionMessageLimit,
				"terminal condition message must stay within the CRD limit")
			// The message stays actionable: it names at least the first spoke and
			// reports how many more were omitted.
			assert.Contains(t, block.message, "spoke-cluster-with-a-fairly-long-name-00000.example.com")
			assert.Contains(t, block.message, "more)")
		})
	}
}

// TestSummarizeClusters covers the bounded-sample helper directly: an empty list, a
// short list that is reported in full, and a list long enough to be truncated with a
// "(+N more)" suffix while staying within the sample budget.
func TestSummarizeClusters(t *testing.T) {
	assert.Equal(t, "", summarizeClusters(nil))
	assert.Equal(t, "a, b, c", summarizeClusters([]string{"a", "b", "c"}))

	// A pathologically long single name is still emitted in full (the message must
	// always identify a blocking cluster).
	long := make([]byte, clusterListSampleBudget+100)
	for i := range long {
		long[i] = 'x'
	}
	single := summarizeClusters([]string{string(long)})
	assert.Equal(t, string(long), single)

	// Many short names are truncated to a bounded sample with a "(+N more)" suffix.
	many := make([]string, 10000)
	for i := range many {
		many[i] = fmt.Sprintf("cluster-%05d", i)
	}
	out := summarizeClusters(many)
	assert.LessOrEqual(t, len(out), clusterListSampleBudget+32) // sample budget + short suffix
	assert.Contains(t, out, "cluster-00000")
	assert.Contains(t, out, "more)")
}

// TestRunPreflightGatesScopesSpokeCheckToEligibleSet verifies the spoke-side
// seed-format check runs against plan item 0's eligible set, not the full base set
// (the Developer-of-Record decision on CNF-30039): an unavailable spoke that matches
// the IBGU selectors but is ineligible for the plan (no CGU can target it) must not
// block, while an unavailable spoke that is eligible must still block. An Upgrade-only
// plan is the case where the two sets differ — eligibility requires the Prep-completed
// label — whereas a Prep plan's eligible set equals its base set.
func TestRunPreflightGatesScopesSpokeCheckToEligibleSet(t *testing.T) {
	prepCompleted := map[string]string{
		"common": "true",
		fmt.Sprintf(utils.IBGUActionCompletedLabelTemplate, "prep"): "true",
	}
	baseOnly := map[string]string{"common": "true"}

	upgradeOnlyIBGU := func() *ibguv1alpha1.ImageBasedGroupUpgrade {
		return newIBGUForResolution([]ibguv1alpha1.PlanItem{{Actions: []string{ibguv1alpha1.Upgrade}}})
	}
	newReconciler := func(fakeClient client.Client) IBGUReconciler {
		return IBGUReconciler{
			Client: fakeClient, APIReader: fakeClient, Scheme: testscheme, Log: logr.Discard(),
			Recorder:        record.NewFakeRecorder(10),
			SeedImageReader: &fakeSeedImageReader{format: lcav1.SeedFormatVersion},
		}
	}

	t.Run("an unavailable ineligible spoke does not block", func(t *testing.T) {
		ibgu := upgradeOnlyIBGU()
		fakeClient, err := getFakeClientFromObjects()
		assert.NoError(t, err)
		// Eligible (Prep-completed), available, matching claim: the only spoke a CGU in
		// this plan can target, and it passes.
		assert.NoError(t, fakeClient.Create(context.TODO(),
			availableClusterWithSeedFormat("eligible-ok", prepCompleted, lcav1.SeedFormatVersion)))
		// Matches the IBGU selector but lacks the Prep-completed label, so no CGU targets
		// it; it is unavailable, which would block the IBGU if the check used the base set.
		assert.NoError(t, fakeClient.Create(context.TODO(),
			managedClusterWithLabels("ineligible-down", baseOnly)))

		reconciler := newReconciler(fakeClient)
		frozen, block, err := reconciler.runPreflightGates(context.TODO(), ibgu)
		assert.NoError(t, err)
		// The ineligible unavailable spoke is outside the eligible set, so it does not block.
		assert.Nil(t, block)
		// Only the eligible spoke is frozen.
		assert.Equal(t, []string{"eligible-ok"}, frozen)
	})

	t.Run("an unavailable eligible spoke still blocks", func(t *testing.T) {
		ibgu := upgradeOnlyIBGU()
		fakeClient, err := getFakeClientFromObjects()
		assert.NoError(t, err)
		// Eligible (Prep-completed) but unavailable: a CGU would target it, so its
		// unavailability must terminally block the IBGU.
		assert.NoError(t, fakeClient.Create(context.TODO(),
			managedClusterWithLabels("eligible-down", prepCompleted)))

		reconciler := newReconciler(fakeClient)
		_, block, err := reconciler.runPreflightGates(context.TODO(), ibgu)
		assert.NoError(t, err)
		assert.NotNil(t, block)
		assert.Equal(t, utils.ConditionReasons.SpokeClusterUnavailable, block.reason)
		assert.Contains(t, block.message, "eligible-down")
	})
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
			Client: fakeClient, APIReader: fakeClient, Scheme: testscheme, Log: logr.Discard(),
			Recorder:        record.NewFakeRecorder(10),
			SeedImageReader: &fakeSeedImageReader{format: preBumpFormat},
		}
		err = ensureManifestsForTest(t, &reconciler, ibgu)
		assert.NoError(t, err)

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
			Client: fakeClient, APIReader: fakeClient, Scheme: testscheme, Log: logr.Discard(),
			Recorder:        record.NewFakeRecorder(10),
			SeedImageReader: reader,
		}
		err = ensureManifestsForTest(t, &reconciler, ibgu)
		assert.NoError(t, err)
		// The seed gate never ran: no registry read.
		assert.False(t, reader.called)

		// The terminal block carries the reference reason, not the seed reason.
		succeeded := succeededCondition(ibgu)
		assert.NotNil(t, succeeded)
		assert.Equal(t, metav1.ConditionFalse, succeeded.Status)
		assert.Equal(t, string(utils.ConditionReasons.UnresolvableDenpendency), succeeded.Reason)
		assert.Equal(t, string(utils.ConditionReasons.UnresolvableDenpendency), progressingCondition(ibgu).Reason)
	})

	t.Run("compatible seed with an available matching spoke creates a CGU", func(t *testing.T) {
		ibgu := backupContentIBGU()

		fakeClient, err := getFakeClientFromObjects()
		assert.NoError(t, err)
		assert.NoError(t, fakeClient.Create(context.TODO(), hubBackupContent("bc1", "namespace")))
		// An available spoke publishing the matching seed-format claim passes the spoke comparison.
		assert.NoError(t, fakeClient.Create(context.TODO(),
			availableClusterWithSeedFormat("cluster1", map[string]string{"common": "true"},
				lcav1.BackupContentFirstSeedFormatVersion)))

		reader := &fakeSeedImageReader{format: lcav1.BackupContentFirstSeedFormatVersion}
		reconciler := IBGUReconciler{
			Client: fakeClient, APIReader: fakeClient, Scheme: testscheme, Log: logr.Discard(),
			SeedImageReader: reader,
		}
		err = ensureManifestsForTest(t, &reconciler, ibgu)
		assert.NoError(t, err)
		assert.True(t, reader.called)

		cgu := &v1alpha1.ClusterGroupUpgrade{}
		err = reconciler.Get(context.TODO(),
			types.NamespacedName{Name: "name-prep-0", Namespace: "namespace"}, cgu)
		assert.NoError(t, err)
	})

	t.Run("spoke whose claim mismatches the image is terminal before any CGU", func(t *testing.T) {
		// No backupContent, so the hub-only safety rule is skipped and the spoke comparison is
		// the gate under test: a claimless spoke expects the pre-bump format and so
		// mismatches a bumped image.
		ibgu := newIBGUForResolution([]ibguv1alpha1.PlanItem{{Actions: []string{ibguv1alpha1.Prep}}})

		fakeClient, err := getFakeClientFromObjects()
		assert.NoError(t, err)
		assert.NoError(t, fakeClient.Create(context.TODO(),
			availableClusterWithSeedFormat("cluster1", map[string]string{"common": "true"}, preBumpFormat)))

		reader := &fakeSeedImageReader{format: lcav1.SeedFormatVersion}
		reconciler := IBGUReconciler{
			Client: fakeClient, APIReader: fakeClient, Scheme: testscheme, Log: logr.Discard(),
			SeedImageReader: reader,
		}
		err = ensureManifestsForTest(t, &reconciler, ibgu)
		assert.NoError(t, err)
		assert.True(t, reader.called)

		succeeded := succeededCondition(ibgu)
		assert.NotNil(t, succeeded)
		assert.Equal(t, metav1.ConditionFalse, succeeded.Status)
		assert.Equal(t, string(utils.ConditionReasons.SeedFormatMismatch), succeeded.Reason)
		assert.Equal(t, string(utils.ConditionReasons.SeedFormatMismatch), progressingCondition(ibgu).Reason)
		assert.Contains(t, succeeded.Message, "cluster1")

		cgu := &v1alpha1.ClusterGroupUpgrade{}
		assert.True(t, apierrors.IsNotFound(reconciler.Get(context.TODO(),
			types.NamespacedName{Name: "name-prep-0", Namespace: "namespace"}, cgu)))
	})

	t.Run("unavailable spoke is terminal before any CGU", func(t *testing.T) {
		ibgu := newIBGUForResolution([]ibguv1alpha1.PlanItem{{Actions: []string{ibguv1alpha1.Prep}}})

		fakeClient, err := getFakeClientFromObjects()
		assert.NoError(t, err)
		// managedClusterWithLabels sets no Available condition, so the spoke is unavailable.
		assert.NoError(t, fakeClient.Create(context.TODO(),
			managedClusterWithLabels("cluster1", map[string]string{"common": "true"})))

		reader := &fakeSeedImageReader{format: lcav1.SeedFormatVersion}
		reconciler := IBGUReconciler{
			Client: fakeClient, APIReader: fakeClient, Scheme: testscheme, Log: logr.Discard(),
			SeedImageReader: reader,
		}
		err = ensureManifestsForTest(t, &reconciler, ibgu)
		assert.NoError(t, err)
		assert.True(t, reader.called)

		succeeded := succeededCondition(ibgu)
		assert.NotNil(t, succeeded)
		assert.Equal(t, metav1.ConditionFalse, succeeded.Status)
		assert.Equal(t, string(utils.ConditionReasons.SpokeClusterUnavailable), succeeded.Reason)
		assert.Equal(t, string(utils.ConditionReasons.SpokeClusterUnavailable), progressingCondition(ibgu).Reason)
		assert.Contains(t, succeeded.Message, "cluster1")

		cgu := &v1alpha1.ClusterGroupUpgrade{}
		assert.True(t, apierrors.IsNotFound(reconciler.Get(context.TODO(),
			types.NamespacedName{Name: "name-prep-0", Namespace: "namespace"}, cgu)))
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
			Client: fakeClient, APIReader: fakeClient, Scheme: testscheme, Log: logr.Discard(),
			SeedImageReader: reader,
		}
		err = ensureManifestsForTest(t, &reconciler, ibgu)
		assert.NoError(t, err)
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
			Client: fakeClient, APIReader: fakeClient, Scheme: testscheme, Log: logr.Discard(),
			SeedImageReader: reader,
		}
		err = ensureManifestsForTest(t, &reconciler, ibgu)
		assert.NoError(t, err)
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

	t.Run("unreadable seed is terminal and takes precedence over the start deadline", func(t *testing.T) {
		// The seed registry is unreachable, so the gate fails. This is terminal regardless
		// of how long ago the IBGU was created: a failing gate records its own reason
		// (SeedImageUnreadable), never the start-deadline reason, and creates no CGU — even
		// when creation is already past the start deadline.
		ibgu, reader, _, reconciler := newUnreadableSeedReconciler(t)
		ibgu.CreationTimestamp = metav1.NewTime(metav1.Now().Add(-(preflightStartDeadline + time.Minute)))

		err := ensureManifestsForTest(t, &reconciler, ibgu)
		assert.NoError(t, err)
		assert.True(t, reader.called)

		succeeded := succeededCondition(ibgu)
		assert.NotNil(t, succeeded)
		assert.Equal(t, metav1.ConditionFalse, succeeded.Status)
		assert.Equal(t, string(utils.ConditionReasons.SeedImageUnreadable), succeeded.Reason)
		assert.Equal(t, string(utils.ConditionReasons.SeedImageUnreadable), progressingCondition(ibgu).Reason)

		cgu := &v1alpha1.ClusterGroupUpgrade{}
		assert.True(t, apierrors.IsNotFound(reconciler.Get(context.TODO(),
			types.NamespacedName{Name: "name-prep-0", Namespace: "namespace"}, cgu)))
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
		Client: fakeClient, APIReader: fakeClient, Scheme: testscheme, Log: logr.Discard(),
		Recorder: recorder, SeedImageReader: reader,
	}
	return ibgu, reader, recorder, reconciler
}

// TestBackupContentFoundViaUncachedReader proves AC 3: the pre-flight gates read
// through the uncached APIReader, so a BackupContent (or a matching ManagedCluster)
// that exists on the API server but has not yet landed in the cached client is found
// and the gate passes instead of producing a false terminal block. The gates are
// exercised directly because they are where the false block originates; the Prep-CGU
// manifest builders read through the same uncached reader (see getBackupContentManifests
// / getConfigMapManifests / getSecretManifest), so the pass path stays consistent
// within the one reconcile the gate passed. Each control models the lag by passing a
// deliberately stale fake (missing the object) as APIReader.
func TestBackupContentFoundViaUncachedReader(t *testing.T) {
	t.Run("reference gate finds a cache-lagged BackupContent", func(t *testing.T) {
		ibgu := backupContentIBGU() // Prep plan with backupContent bc1

		// Lagging reader: bc1 absent. Up-to-date reader: bc1 present.
		laggingReader, err := getFakeClientFromObjects()
		assert.NoError(t, err)
		uncachedReader, err := getFakeClientFromObjects()
		assert.NoError(t, err)
		assert.NoError(t, uncachedReader.Create(context.TODO(), hubBackupContent("bc1", "namespace")))

		// resolvePrepReferences is the reference gate's core; reading through the
		// up-to-date reader resolves bc1, so nothing is reported missing.
		reconciler := IBGUReconciler{
			Client: uncachedReader, APIReader: uncachedReader, Scheme: testscheme, Log: logr.Discard(),
		}
		missing, err := reconciler.resolvePrepReferences(context.TODO(), reconciler.preflightReader(), ibgu)
		assert.NoError(t, err)
		assert.Empty(t, missing)

		// Control: reading through the lagging reader (bc1 absent) reports it missing,
		// confirming the uncached reader is what defeats the lag.
		missingCached, err := reconciler.resolvePrepReferences(context.TODO(), laggingReader, ibgu)
		assert.NoError(t, err)
		assert.Equal(t, []string{`BackupContent "bc1"`}, missingCached)
	})

	t.Run("empty-set gate finds a cache-lagged ManagedCluster", func(t *testing.T) {
		ibgu := newIBGUForResolution([]ibguv1alpha1.PlanItem{{Actions: []string{ibguv1alpha1.Prep}}})

		// Lagging reader: no ManagedCluster. Up-to-date reader: the matching cluster.
		laggingReader, err := getFakeClientFromObjects()
		assert.NoError(t, err)
		uncachedReader, err := getFakeClientFromObjects()
		assert.NoError(t, err)
		assert.NoError(t, uncachedReader.Create(context.TODO(),
			managedClusterWithLabels("cluster1", map[string]string{"common": "true"})))

		reconciler := IBGUReconciler{
			Client: uncachedReader, APIReader: uncachedReader, Scheme: testscheme, Log: logr.Discard(),
		}
		eligible, block, err := reconciler.ensureNonEmptyClusterSet(context.TODO(), ibgu)
		assert.NoError(t, err)
		// The gate passed via the uncached read: no false NoClustersMatchSelector block.
		assert.Nil(t, block)
		// The gate returns the eligible ManagedCluster objects resolved via the uncached
		// reader, which the first CGU is then created from.
		assert.Equal(t, []string{"cluster1"}, clusterNames(eligible))

		// Control: a lagging reader (no cluster) as APIReader blocks terminally.
		cachedOnly := IBGUReconciler{Client: laggingReader, APIReader: laggingReader, Scheme: testscheme, Log: logr.Discard()}
		_, cachedBlock, err := cachedOnly.ensureNonEmptyClusterSet(context.TODO(), ibgu)
		assert.NoError(t, err)
		assert.NotNil(t, cachedBlock)
		assert.Equal(t, utils.ConditionReasons.NoClustersMatchSelector, cachedBlock.reason)
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
			availableClusterWithSeedFormat("cluster1", map[string]string{"common": "true"},
				lcav1.BackupContentFirstSeedFormatVersion)))

		reconciler := IBGUReconciler{
			Client: cachedClient, APIReader: uncachedReader, Scheme: testscheme, Log: logr.Discard(),
			Recorder:        record.NewFakeRecorder(10),
			SeedImageReader: &fakeSeedImageReader{format: lcav1.BackupContentFirstSeedFormatVersion},
		}

		err = ensureManifestsForTest(t, &reconciler, ibgu)
		assert.NoError(t, err)
		// No false terminal block.
		assert.Nil(t, succeededCondition(ibgu))
		// The Prep CGU is created on the frozen set resolved via the uncached reader.
		cgu := &v1alpha1.ClusterGroupUpgrade{}
		assert.NoError(t, reconciler.Get(context.TODO(),
			types.NamespacedName{Name: "name-prep-0", Namespace: "namespace"}, cgu))
		assert.Equal(t, []string{"cluster1"}, frozenSetFromSelectors(cgu.Spec.ClusterLabelSelectors))
	})
}
