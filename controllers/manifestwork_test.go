package controllers

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/go-logr/logr"
	"github.com/openshift-kni/cluster-group-upgrades-operator/controllers/utils"
	ranv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/clustergroupupgrades/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	mwv1 "open-cluster-management.io/api/work/v1"
	mwv1alpha1 "open-cluster-management.io/api/work/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

const (
	mwTestNamespace = "ztp-group"
	mwTestCGUName   = "cgu-mw"
	mwTestTemplate0 = "tmpl-prep"
	mwTestTemplate1 = "tmpl-upgrade"
	mwTestCluster1  = "spoke1"
	mwTestCluster2  = "spoke2"
	mwTestCluster3  = "spoke3"
	mwTestExpected  = `[{"manifestIndex":0,"name":"isReady","value":"True"}]`

	cguNameLabel           = "openshift-cluster-group-upgrades/clusterGroupUpgrade"
	cguNamespaceLabel      = "openshift-cluster-group-upgrades/clusterGroupUpgradeNamespace"
	expectedValuesAnnotKey = "openshift-cluster-group-upgrades/expectedValues"
)

// errEtcdUnavailable is the transient server error injected by the failing clients.
var errEtcdUnavailable = apierrors.NewInternalError(fmt.Errorf("etcd unavailable"))

// newManifestWorkCGU returns a ManifestWork-rollout CGU with a fixed name suffix,
// so the generated ManifestWork names are deterministic.
func newManifestWorkCGU() *ranv1alpha1.ClusterGroupUpgrade {
	return &ranv1alpha1.ClusterGroupUpgrade{
		ObjectMeta: metav1.ObjectMeta{
			Name:        mwTestCGUName,
			Namespace:   mwTestNamespace,
			Annotations: map[string]string{utils.NameSuffixAnnotation: "abcde"},
		},
		Spec: ranv1alpha1.ClusterGroupUpgradeSpec{
			ManifestWorkTemplates: []string{mwTestTemplate0, mwTestTemplate1},
		},
	}
}

// mwName returns the ManifestWork name the controller uses for the template at index.
func mwName(cgu *ranv1alpha1.ClusterGroupUpgrade, index int) string {
	return utils.GetSafeResourceName(cgu.Namespace+"."+cgu.Spec.ManifestWorkTemplates[index], "", cgu, utils.MaxObjectNameLength)
}

// newManifestWork returns a ManifestWork for the template at index on cluster,
// labelled as owned by cgu, with Applied/Available set to ready.
func newManifestWork(cgu *ranv1alpha1.ClusterGroupUpgrade, index int, cluster string, ready bool) *mwv1.ManifestWork {
	status := metav1.ConditionFalse
	if ready {
		status = metav1.ConditionTrue
	}
	return &mwv1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name:      mwName(cgu, index),
			Namespace: cluster,
			Labels: map[string]string{
				cguNameLabel:      cgu.Name,
				cguNamespaceLabel: cgu.Namespace,
			},
		},
		Status: mwv1.ManifestWorkStatus{
			Conditions: []metav1.Condition{
				{Type: mwv1.WorkApplied, Status: status},
				{Type: mwv1.WorkAvailable, Status: status},
			},
		},
	}
}

func newManifestWorkTemplate(name string, manifests ...string) *mwv1alpha1.ManifestWorkReplicaSet {
	mwrs := &mwv1alpha1.ManifestWorkReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   mwTestNamespace,
			Annotations: map[string]string{expectedValuesAnnotKey: mwTestExpected},
		},
	}
	for _, m := range manifests {
		mwrs.Spec.ManifestWorkTemplate.Workload.Manifests = append(mwrs.Spec.ManifestWorkTemplate.Workload.Manifests,
			mwv1.Manifest{RawExtension: runtime.RawExtension{Raw: []byte(m)}})
	}
	return mwrs
}

func newMWReconciler(c client.Client) *ClusterGroupUpgradeReconciler {
	return &ClusterGroupUpgradeReconciler{Client: c, Log: logr.Discard(), Scheme: testscheme}
}

// failingGetClient wraps c so that Get on an object of the same type as target
// returns an internal error; other calls pass through.
func failingGetClient(c client.WithWatch, target client.Object) client.WithWatch {
	return interceptor.NewClient(c, interceptor.Funcs{
		Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			if reflect.TypeOf(obj) == reflect.TypeOf(target) {
				return errEtcdUnavailable
			}
			return cl.Get(ctx, key, obj, opts...)
		},
	})
}

func progress(state string, index int) *ranv1alpha1.ClusterRemediationProgress {
	return &ranv1alpha1.ClusterRemediationProgress{State: state, ManifestWorkIndex: &index}
}

func TestValidateManifestWorkTemplates(t *testing.T) {
	tests := []struct {
		name          string
		objs          []client.Object
		failGet       bool
		wantManifests []string
		wantMissing   []string
		wantErr       bool
	}{
		{
			name: "all templates found, manifests concatenated in order",
			objs: []client.Object{
				newManifestWorkTemplate(mwTestTemplate0, `{"kind":"A"}`),
				newManifestWorkTemplate(mwTestTemplate1, `{"kind":"B"}`, `{"kind":"C"}`),
			},
			wantManifests: []string{`{"kind":"A"}`, `{"kind":"B"}`, `{"kind":"C"}`},
		},
		{
			name:          "missing template is reported, not an error",
			objs:          []client.Object{newManifestWorkTemplate(mwTestTemplate1, `{"kind":"B"}`)},
			wantManifests: []string{`{"kind":"B"}`},
			wantMissing:   []string{mwTestTemplate0},
		},
		{
			name:        "all templates missing",
			wantMissing: []string{mwTestTemplate0, mwTestTemplate1},
		},
		{
			name:    "non-NotFound error is returned",
			objs:    []client.Object{newManifestWorkTemplate(mwTestTemplate0, `{"kind":"A"}`)},
			failGet: true,
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c, err := getFakeClientFromObjects(tc.objs...)
			require.NoError(t, err)
			if tc.failGet {
				c = failingGetClient(c, &mwv1alpha1.ManifestWorkReplicaSet{})
			}
			r := newMWReconciler(c)

			manifests, missing, err := r.validateManifestWorkTemplates(context.Background(), newManifestWorkCGU())
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			var got []string
			for _, m := range manifests {
				got = append(got, string(m.Raw))
			}
			assert.Equal(t, tc.wantManifests, got)
			assert.Equal(t, tc.wantMissing, missing)
		})
	}
}

func TestGetNextManifestWorkForCluster(t *testing.T) {
	tests := []struct {
		name            string
		startIndex      int
		existing        func(cgu *ranv1alpha1.ClusterGroupUpgrade) []client.Object
		failGet         bool
		failDelete      bool
		wantIndex       int
		wantErr         bool
		wantPrevDeleted bool
	}{
		{
			name:       "current completed advances index",
			startIndex: 0,
			existing: func(cgu *ranv1alpha1.ClusterGroupUpgrade) []client.Object {
				return []client.Object{newManifestWork(cgu, 0, mwTestCluster1, true)}
			},
			wantIndex: 1,
		},
		{
			name:       "current not ready keeps index",
			startIndex: 0,
			existing: func(cgu *ranv1alpha1.ClusterGroupUpgrade) []client.Object {
				return []client.Object{newManifestWork(cgu, 0, mwTestCluster1, false)}
			},
			wantIndex: 0,
		},
		{
			name:       "current completed with unparsable expected values returns error",
			startIndex: 0,
			existing: func(cgu *ranv1alpha1.ClusterGroupUpgrade) []client.Object {
				mw := newManifestWork(cgu, 0, mwTestCluster1, true)
				mw.Annotations = map[string]string{expectedValuesAnnotKey: "not-json"}
				return []client.Object{mw}
			},
			wantIndex: 0,
			wantErr:   true,
		},
		{
			name:       "first manifestwork not created yet",
			startIndex: 0,
			wantIndex:  0,
		},
		{
			name:       "current not created, previous still present is deleted",
			startIndex: 1,
			existing: func(cgu *ranv1alpha1.ClusterGroupUpgrade) []client.Object {
				return []client.Object{newManifestWork(cgu, 0, mwTestCluster1, true)}
			},
			wantIndex:       1,
			wantPrevDeleted: true,
		},
		{
			name:            "current and previous both absent",
			startIndex:      1,
			wantIndex:       1,
			wantPrevDeleted: true,
		},
		{
			name:       "error deleting the previous manifestwork is returned",
			startIndex: 1,
			existing: func(cgu *ranv1alpha1.ClusterGroupUpgrade) []client.Object {
				return []client.Object{newManifestWork(cgu, 0, mwTestCluster1, true)}
			},
			failDelete: true,
			wantIndex:  1,
			wantErr:    true,
		},
		{
			name:       "non-NotFound error is returned",
			startIndex: 0,
			failGet:    true,
			wantIndex:  0,
			wantErr:    true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cgu := newManifestWorkCGU()
			var objs []client.Object
			if tc.existing != nil {
				objs = tc.existing(cgu)
			}
			c, err := getFakeClientFromObjects(objs...)
			require.NoError(t, err)
			var rc client.Client = c
			if tc.failGet {
				rc = failingGetClient(c, &mwv1.ManifestWork{})
			}
			if tc.failDelete {
				rc = interceptor.NewClient(c, interceptor.Funcs{
					Delete: func(context.Context, client.WithWatch, client.Object, ...client.DeleteOption) error {
						return errEtcdUnavailable
					},
				})
			}
			r := newMWReconciler(rc)

			index, _, err := r.getNextManifestWorkForCluster(context.Background(), cgu, mwTestCluster1, tc.startIndex)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tc.wantIndex, index)

			if tc.wantPrevDeleted {
				err = c.Get(context.Background(), types.NamespacedName{Name: mwName(cgu, tc.startIndex-1), Namespace: mwTestCluster1}, &mwv1.ManifestWork{})
				assert.True(t, apierrors.IsNotFound(err), "previous manifestwork should be gone, got %v", err)
			}
		})
	}
}

func TestUpdateManifestWorkForCurrentBatch(t *testing.T) {
	type mwRef struct {
		cluster string
		index   int
	}
	tests := []struct {
		name        string
		progress    map[string]*ranv1alpha1.ClusterRemediationProgress
		existing    func(cgu *ranv1alpha1.ClusterGroupUpgrade) []client.Object
		templates   []client.Object
		failGet     bool
		wantErr     bool
		wantCreated []mwRef
		wantKept    []mwRef // pre-existing works that must not be recreated
		wantAbsent  []mwRef
	}{
		{
			name: "first manifestwork is created from the template",
			progress: map[string]*ranv1alpha1.ClusterRemediationProgress{
				mwTestCluster1: progress(ranv1alpha1.InProgress, 0),
			},
			templates:   []client.Object{newManifestWorkTemplate(mwTestTemplate0, `{"kind":"A"}`)},
			wantCreated: []mwRef{{mwTestCluster1, 0}},
		},
		{
			name: "completed cluster is skipped",
			progress: map[string]*ranv1alpha1.ClusterRemediationProgress{
				mwTestCluster1: progress(ranv1alpha1.Completed, 0),
				mwTestCluster2: progress(ranv1alpha1.InProgress, 0),
			},
			templates:   []client.Object{newManifestWorkTemplate(mwTestTemplate0, `{"kind":"A"}`)},
			wantCreated: []mwRef{{mwTestCluster2, 0}},
			wantAbsent:  []mwRef{{mwTestCluster1, 0}},
		},
		{
			name: "next manifestwork waits while the previous one exists",
			progress: map[string]*ranv1alpha1.ClusterRemediationProgress{
				mwTestCluster1: progress(ranv1alpha1.InProgress, 1),
			},
			existing: func(cgu *ranv1alpha1.ClusterGroupUpgrade) []client.Object {
				return []client.Object{newManifestWork(cgu, 0, mwTestCluster1, true)}
			},
			templates:  []client.Object{newManifestWorkTemplate(mwTestTemplate1, `{"kind":"B"}`)},
			wantAbsent: []mwRef{{mwTestCluster1, 1}},
		},
		{
			name: "next manifestwork is created once the previous one is gone",
			progress: map[string]*ranv1alpha1.ClusterRemediationProgress{
				mwTestCluster1: progress(ranv1alpha1.InProgress, 1),
			},
			templates:   []client.Object{newManifestWorkTemplate(mwTestTemplate1, `{"kind":"B"}`)},
			wantCreated: []mwRef{{mwTestCluster1, 1}},
		},
		{
			name: "existing current manifestwork is left alone",
			progress: map[string]*ranv1alpha1.ClusterRemediationProgress{
				mwTestCluster1: progress(ranv1alpha1.InProgress, 0),
			},
			existing: func(cgu *ranv1alpha1.ClusterGroupUpgrade) []client.Object {
				return []client.Object{newManifestWork(cgu, 0, mwTestCluster1, false)}
			},
			templates: []client.Object{newManifestWorkTemplate(mwTestTemplate0, `{"kind":"A"}`)},
			wantKept:  []mwRef{{mwTestCluster1, 0}},
		},
		{
			name: "missing template returns error",
			progress: map[string]*ranv1alpha1.ClusterRemediationProgress{
				mwTestCluster1: progress(ranv1alpha1.InProgress, 0),
			},
			wantErr:    true,
			wantAbsent: []mwRef{{mwTestCluster1, 0}},
		},
		{
			name: "error looking up the current manifestwork is returned",
			progress: map[string]*ranv1alpha1.ClusterRemediationProgress{
				mwTestCluster1: progress(ranv1alpha1.InProgress, 0),
			},
			templates:  []client.Object{newManifestWorkTemplate(mwTestTemplate0, `{"kind":"A"}`)},
			failGet:    true,
			wantErr:    true,
			wantAbsent: []mwRef{{mwTestCluster1, 0}},
		},
		{
			name: "error looking up the previous manifestwork is returned",
			progress: map[string]*ranv1alpha1.ClusterRemediationProgress{
				mwTestCluster1: progress(ranv1alpha1.InProgress, 1),
			},
			templates: []client.Object{newManifestWorkTemplate(mwTestTemplate1, `{"kind":"B"}`)},
			failGet:   true,
			wantErr:   true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cgu := newManifestWorkCGU()
			cgu.Status.Status.CurrentBatchRemediationProgress = tc.progress
			objs := append([]client.Object{}, tc.templates...)
			if tc.existing != nil {
				objs = append(objs, tc.existing(cgu)...)
			}
			c, err := getFakeClientFromObjects(objs...)
			require.NoError(t, err)
			var rc client.Client = c
			if tc.failGet {
				rc = failingGetClient(c, &mwv1.ManifestWork{})
			}
			r := newMWReconciler(rc)

			err = r.updateManifestWorkForCurrentBatch(context.Background(), cgu)
			if tc.wantErr {
				assert.Error(t, err)
				if tc.failGet {
					// The wrapped error must still classify as the original API error.
					assert.True(t, apierrors.IsInternalError(err), "got %v", err)
				}
			} else {
				assert.NoError(t, err)
			}

			for _, ref := range tc.wantCreated {
				mw := &mwv1.ManifestWork{}
				err := c.Get(context.Background(), types.NamespacedName{Name: mwName(cgu, ref.index), Namespace: ref.cluster}, mw)
				require.NoError(t, err, "manifestwork %d for %s", ref.index, ref.cluster)
				assert.Equal(t, cgu.Name, mw.Labels[cguNameLabel])
				assert.Equal(t, cgu.Namespace, mw.Labels[cguNamespaceLabel])
				assert.Equal(t, mwTestExpected, mw.Annotations[expectedValuesAnnotKey])
				assert.Len(t, mw.Spec.Workload.Manifests, 1)
			}
			for _, ref := range tc.wantKept {
				mw := &mwv1.ManifestWork{}
				err := c.Get(context.Background(), types.NamespacedName{Name: mwName(cgu, ref.index), Namespace: ref.cluster}, mw)
				require.NoError(t, err, "manifestwork %d for %s", ref.index, ref.cluster)
				assert.Empty(t, mw.Annotations[expectedValuesAnnotKey], "pre-existing manifestwork was replaced")
			}
			for _, ref := range tc.wantAbsent {
				err := c.Get(context.Background(), types.NamespacedName{Name: mwName(cgu, ref.index), Namespace: ref.cluster}, &mwv1.ManifestWork{})
				assert.True(t, apierrors.IsNotFound(err), "manifestwork %d for %s should not exist, got %v", ref.index, ref.cluster, err)
			}
		})
	}
}

func TestHandleManifestWorkTimeoutForCluster(t *testing.T) {
	trueCond := func(ty string) metav1.Condition { return metav1.Condition{Type: ty, Status: metav1.ConditionTrue} }
	strVal := func(s string) mwv1.FieldValue { return mwv1.FieldValue{Type: mwv1.String, String: &s} }

	readyManifest := mwv1.ManifestCondition{
		ResourceMeta: mwv1.ManifestResourceMeta{Ordinal: 0},
		Conditions: []metav1.Condition{
			trueCond(mwv1.ManifestApplied), trueCond(mwv1.ManifestAvailable), trueCond("StatusFeedbackSynced"),
		},
		StatusFeedbacks: mwv1.StatusFeedbackResult{Values: []mwv1.FeedbackValue{{Name: "isReady", Value: strVal("True")}}},
	}
	notReadyManifest := mwv1.ManifestCondition{
		ResourceMeta: mwv1.ManifestResourceMeta{Ordinal: 1},
		Conditions: []metav1.Condition{
			trueCond(mwv1.ManifestApplied),
			{Type: mwv1.ManifestAvailable, Status: metav1.ConditionFalse},
		},
		StatusFeedbacks: mwv1.StatusFeedbackResult{Values: []mwv1.FeedbackValue{{Name: "conditionMessage", Value: strVal("pulling")}}},
	}

	tests := []struct {
		name       string
		progress   *ranv1alpha1.ClusterRemediationProgress
		withMW     bool
		failGet    bool
		wantErr    bool
		wantMWName string // "" means CurrentManifestWork stays nil
		check      func(t *testing.T, s *ranv1alpha1.ManifestWorkStatus)
	}{
		{
			name:     "missing index leaves state untouched",
			progress: &ranv1alpha1.ClusterRemediationProgress{State: ranv1alpha1.InProgress},
		},
		{
			name:     "index out of range leaves state untouched",
			progress: progress(ranv1alpha1.InProgress, 2),
		},
		{
			name:       "missing manifestwork records only the template name",
			progress:   progress(ranv1alpha1.InProgress, 1),
			wantMWName: mwTestTemplate1,
			check: func(t *testing.T, s *ranv1alpha1.ManifestWorkStatus) {
				assert.Empty(t, s.Status.Manifests)
			},
		},
		{
			name:       "manifest status is trimmed",
			progress:   progress(ranv1alpha1.InProgress, 0),
			withMW:     true,
			wantMWName: mwTestTemplate0,
			check: func(t *testing.T, s *ranv1alpha1.ManifestWorkStatus) {
				require.Len(t, s.Status.Manifests, 2)
				ready, notReady := s.Status.Manifests[0], s.Status.Manifests[1]
				// Ready manifest keeps only its first condition and its feedback.
				assert.Len(t, ready.Conditions, 1)
				assert.Len(t, ready.StatusFeedbacks.Values, 1)
				// Not-ready manifest keeps its conditions and drops feedback.
				assert.Len(t, notReady.Conditions, 2)
				assert.Nil(t, notReady.StatusFeedbacks.Values)
			},
		},
		{
			name:       "non-NotFound error is returned",
			progress:   progress(ranv1alpha1.InProgress, 0),
			withMW:     true,
			failGet:    true,
			wantErr:    true,
			wantMWName: mwTestTemplate0,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cgu := newManifestWorkCGU()
			cgu.Status.Status.CurrentBatchRemediationProgress = map[string]*ranv1alpha1.ClusterRemediationProgress{
				mwTestCluster1: tc.progress,
			}
			var objs []client.Object
			if tc.withMW {
				mw := newManifestWork(cgu, 0, mwTestCluster1, true)
				mw.Status.ResourceStatus.Manifests = []mwv1.ManifestCondition{
					*readyManifest.DeepCopy(), *notReadyManifest.DeepCopy(),
				}
				objs = append(objs, mw)
			}
			c, err := getFakeClientFromObjects(objs...)
			require.NoError(t, err)
			var rc client.Client = c
			if tc.failGet {
				rc = failingGetClient(c, &mwv1.ManifestWork{})
			}
			r := newMWReconciler(rc)

			state := ranv1alpha1.ClusterState{Name: mwTestCluster1, State: utils.ClusterRemediationTimedout}
			err = r.handleManifestWorkTimeoutForCluster(context.Background(), cgu, mwTestCluster1, &state)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			if tc.wantMWName == "" {
				assert.Nil(t, state.CurrentManifestWork)
				return
			}
			require.NotNil(t, state.CurrentManifestWork)
			assert.Equal(t, tc.wantMWName, state.CurrentManifestWork.Name)
			if tc.check != nil {
				tc.check(t, state.CurrentManifestWork)
			}
		})
	}
}

func TestCleanupManifestWork(t *testing.T) {
	// Batch 0: spoke1, spoke2. Batch 1: spoke3.
	plan := [][]string{{mwTestCluster1, mwTestCluster2}, {mwTestCluster3}}
	allClusters := []string{mwTestCluster1, mwTestCluster2, mwTestCluster3}

	tests := []struct {
		name         string
		currentBatch int
		policyCGU    bool
		failDelete   bool
		cleanup      func(r *ClusterGroupUpgradeReconciler, ctx context.Context, cgu *ranv1alpha1.ClusterGroupUpgrade) error
		wantRemain   []string
	}{
		{
			name:         "current batch: nothing before the first batch starts",
			currentBatch: 0,
			cleanup:      (*ClusterGroupUpgradeReconciler).cleanupManifestWorkForCurrentBatch,
			wantRemain:   allClusters,
		},
		{
			name:         "current batch: deletes only the current batch",
			currentBatch: 2,
			cleanup:      (*ClusterGroupUpgradeReconciler).cleanupManifestWorkForCurrentBatch,
			wantRemain:   []string{mwTestCluster1, mwTestCluster2},
		},
		{
			name:         "previous batch: nothing on the first batch",
			currentBatch: 1,
			cleanup:      (*ClusterGroupUpgradeReconciler).cleanupManifestWorkForPreviousBatch,
			wantRemain:   allClusters,
		},
		{
			name:         "previous batch: deletes only the previous batch",
			currentBatch: 2,
			cleanup:      (*ClusterGroupUpgradeReconciler).cleanupManifestWorkForPreviousBatch,
			wantRemain:   []string{mwTestCluster3},
		},
		{
			name:         "final: no current batch cleans every batch",
			currentBatch: 0,
			cleanup:      (*ClusterGroupUpgradeReconciler).finalCleanupManifestWork,
			wantRemain:   nil,
		},
		{
			name:         "final: cleans batches before the current one",
			currentBatch: 2,
			cleanup:      (*ClusterGroupUpgradeReconciler).finalCleanupManifestWork,
			wantRemain:   []string{mwTestCluster3},
		},
		{
			name:         "policy rollout is a no-op",
			currentBatch: 0,
			policyCGU:    true,
			cleanup:      (*ClusterGroupUpgradeReconciler).finalCleanupManifestWork,
			wantRemain:   allClusters,
		},
		{
			name:         "final: delete error is returned and stops cleanup",
			currentBatch: 2,
			failDelete:   true,
			cleanup:      (*ClusterGroupUpgradeReconciler).finalCleanupManifestWork,
			wantRemain:   allClusters,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cgu := newManifestWorkCGU()
			cgu.Status.RemediationPlan = plan
			cgu.Status.Status.CurrentBatch = tc.currentBatch
			var objs []client.Object
			for _, cluster := range allClusters {
				objs = append(objs, newManifestWork(cgu, 0, cluster, true))
			}
			// An unrelated manifestwork in the same namespace must survive.
			other := newManifestWork(cgu, 0, mwTestCluster1, true)
			other.Name = "unrelated"
			other.Labels = map[string]string{cguNameLabel: "other-cgu"}
			objs = append(objs, other)
			if tc.policyCGU {
				cgu.Spec.ManifestWorkTemplates = nil
			}

			c, err := getFakeClientFromObjects(objs...)
			require.NoError(t, err)
			var rc client.Client = c
			if tc.failDelete {
				rc = interceptor.NewClient(c, interceptor.Funcs{
					DeleteAllOf: func(context.Context, client.WithWatch, client.Object, ...client.DeleteAllOfOption) error {
						return errEtcdUnavailable
					},
				})
			}
			r := newMWReconciler(rc)

			err = tc.cleanup(r, context.Background(), cgu)
			if tc.failDelete {
				assert.ErrorContains(t, err, "failed to cleanup manifestworks for batch 0")
			} else {
				require.NoError(t, err)
			}

			list := &mwv1.ManifestWorkList{}
			require.NoError(t, c.List(context.Background(), list))
			var remain []string
			for _, mw := range list.Items {
				if mw.Name == "unrelated" {
					continue
				}
				remain = append(remain, mw.Namespace)
			}
			assert.ElementsMatch(t, tc.wantRemain, remain)
			assert.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: "unrelated", Namespace: mwTestCluster1}, &mwv1.ManifestWork{}))
		})
	}
}
