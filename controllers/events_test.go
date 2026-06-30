package controllers

import (
	"testing"

	"github.com/go-logr/logr"
	ranv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/clustergroupupgrades/v1alpha1"
	"github.com/stretchr/testify/assert"
	eventsv1 "k8s.io/api/events/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func Test_truncateAnnotations(t *testing.T) {
	longList := "cluster1,cluster2,cluster3,cluster4,cluster5,cluster6,cluster7,cluster8"

	// markerOverhead is the space needed for the truncation marker annotation
	// (key + value where value = the truncated annotation's key).
	markerOverhead := func(truncatedKey string) int {
		return len(CGUEventAnnotationKeyTruncated) + len(truncatedKey)
	}

	type args struct {
		anns          map[string]string
		maxSize       int
		truncatedAnns map[string]string
	}
	tests := []struct {
		name string
		args args
	}{
		{
			name: "maxSize = 0, don't truncate",
			args: args{
				anns:          map[string]string{"k": "v"},
				maxSize:       0,
				truncatedAnns: map[string]string{"k": "v"},
			},
		},
		{
			name: "no truncatable keys present, skip even if over limit",
			args: args{
				anns:          map[string]string{"k": "v"},
				maxSize:       1,
				truncatedAnns: map[string]string{"k": "v"},
			},
		},
		{
			name: "no truncation needed when under limit",
			args: args{
				anns: map[string]string{
					"k":                                    "v",
					CGUEventAnnotationKeyBatchClustersList: "cluster1,cluster2",
				},
				maxSize: 500,
				truncatedAnns: map[string]string{
					"k":                                    "v",
					CGUEventAnnotationKeyBatchClustersList: "cluster1,cluster2",
				},
			},
		},
		{
			name: "truncate batch clusters list with marker",
			args: func() args {
				key := CGUEventAnnotationKeyBatchClustersList
				// maxSize allows "cluster1" (8 bytes) to remain after reserving marker space.
				// total_input = len("k") + len("v") + len(key) + len(longList)
				// We pick maxSize so that maxListStrLen = len(longList) - (total + markerOverhead - maxSize)
				// falls between len("cluster1") and len("cluster1,cluster2").
				totalInput := len("k") + len("v") + len(key) + len(longList)
				maxSize := totalInput + markerOverhead(key) - len(longList) + 10 // leaves room for ~10 chars
				return args{
					anns: map[string]string{
						"k": "v",
						key: longList,
					},
					maxSize: maxSize,
					truncatedAnns: map[string]string{
						"k":                         "v",
						key:                         "cluster1",
						CGUEventAnnotationKeyTruncated: key,
					},
				}
			}(),
		},
		{
			name: "truncate timedout clusters list with marker",
			args: func() args {
				key := CGUEventAnnotationKeyTimedoutClustersList
				totalInput := len("k") + len("v") + len(key) + len(longList)
				maxSize := totalInput + markerOverhead(key) - len(longList) + 10
				return args{
					anns: map[string]string{
						"k": "v",
						key: longList,
					},
					maxSize: maxSize,
					truncatedAnns: map[string]string{
						"k":                         "v",
						key:                         "cluster1",
						CGUEventAnnotationKeyTruncated: key,
					},
				}
			}(),
		},
		{
			name: "truncate missing clusters list with marker",
			args: func() args {
				key := CGUEventAnnotationKeyMissingClustersList
				totalInput := len("k") + len("v") + len(key) + len(longList)
				maxSize := totalInput + markerOverhead(key) - len(longList) + 10
				return args{
					anns: map[string]string{
						"k": "v",
						key: longList,
					},
					maxSize: maxSize,
					truncatedAnns: map[string]string{
						"k":                         "v",
						key:                         "cluster1",
						CGUEventAnnotationKeyTruncated: key,
					},
				}
			}(),
		},
		{
			name: "truncate missing policies list with marker",
			args: func() args {
				key := CGUEventAnnotationKeyMissingPoliciesList
				longPolicies := "policy-aaa,policy-bbb,policy-ccc,policy-ddd,policy-eee,policy-fff,policy-ggg"
				totalInput := len("k") + len("v") + len(key) + len(longPolicies)
				maxSize := totalInput + markerOverhead(key) - len(longPolicies) + 12
				return args{
					anns: map[string]string{
						"k": "v",
						key: longPolicies,
					},
					maxSize: maxSize,
					truncatedAnns: map[string]string{
						"k":                         "v",
						key:                         "policy-aaa",
						CGUEventAnnotationKeyTruncated: key,
					},
				}
			}(),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			truncateAnnotations(tt.args.anns, tt.args.maxSize)
			assert.Equal(t, tt.args.truncatedAnns, tt.args.anns)
		})
	}
}

func Test_truncateListString(t *testing.T) {
	type args struct {
		listStr string
		maxSize int64
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "truncate all",
			args: args{
				listStr: "elem1, elem2",
				maxSize: 0,
			},
			want: "",
		},
		{
			name: "truncate second element",
			args: args{
				listStr: "elem1,elem2",
				maxSize: 5,
			},
			want: "elem1",
		},
		{
			name: "truncate last two elements",
			args: args{
				listStr: "elem1,elem2,elem3",
				maxSize: 5,
			},
			want: "elem1",
		},
		{
			name: "truncate last element",
			args: args{
				listStr: "elem1,elem2,elem3",
				maxSize: 11, // 5*2 + separator ","
			},
			want: "elem1,elem2",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := truncateListString(tt.args.listStr, tt.args.maxSize); got != tt.want {
				t.Errorf("truncateListString() = %v, want %v", got, tt.want)
			}
		})
	}
}

func newTestReconciler(t *testing.T) (*ClusterGroupUpgradeReconciler, client.Client) {
	t.Helper()

	s := runtime.NewScheme()
	_ = ranv1alpha1.AddToScheme(s)
	_ = eventsv1.AddToScheme(s)

	fakeClient := fake.NewClientBuilder().WithScheme(s).Build()
	return &ClusterGroupUpgradeReconciler{
		Client:       fakeClient,
		Log:          logr.Discard(),
		Scheme:       s,
		EventEmitter: NewEmitter(fakeClient, s, "ClusterGroupUpgrade"),
	}, fakeClient
}

func newTestCGU() *ranv1alpha1.ClusterGroupUpgrade {
	return &ranv1alpha1.ClusterGroupUpgrade{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cgu",
			Namespace: "default",
		},
	}
}

func Test_sendEventCGUValidationFailureMissingClusters(t *testing.T) {
	reconciler, fakeClient := newTestReconciler(t)
	cgu := newTestCGU()

	missingClusters := []string{"cluster-a", "cluster-b"}
	reconciler.sendEventCGUValidationFailureMissingClusters(t.Context(), cgu, missingClusters)

	var eventList eventsv1.EventList
	assert.NoError(t, fakeClient.List(t.Context(), &eventList))
	assert.Len(t, eventList.Items, 1)

	ev := eventList.Items[0]
	assert.Equal(t, "Warning", ev.Type)
	assert.Equal(t, CGUEventReasonValidationFailure, ev.Reason)
	assert.Equal(t, CGUEventActionValidate, ev.Action)
	assert.Contains(t, ev.Note, "missing clusters")
	assert.Contains(t, ev.Note, "cluster-a,cluster-b")
	assert.Equal(t, "2", ev.Annotations[CGUEventAnnotationKeyMissingClustersCount])
	assert.Equal(t, "cluster-a,cluster-b", ev.Annotations[CGUEventAnnotationKeyMissingClustersList])
}

func Test_sendEventCGUVPoliciesValidationFailure(t *testing.T) {
	tests := []struct {
		name            string
		failureType     PoliciesValidationFailureType
		info            policiesInfo
		wantAnnotation  string
		wantAnnotations map[string]string
		wantMsgContains string
	}{
		{
			name:        "missing policies",
			failureType: CGUValidationErrorMsgMissingPolicies,
			info: policiesInfo{
				missingPolicies: []string{"policy-x", "policy-y"},
			},
			wantMsgContains: "missing policies",
			wantAnnotations: map[string]string{
				CGUEventAnnotationKeyMissingPoliciesList: "policy-x,policy-y",
			},
		},
		{
			name:        "invalid policies",
			failureType: CGUValidationErrorMsgInvalidPolicies,
			info: policiesInfo{
				invalidPolicies: []string{"bad-policy-1", "bad-policy-2"},
			},
			wantMsgContains: "invalid policies",
			wantAnnotations: map[string]string{
				CGUEventAnnotationKeyInvalidPoliciesList: "bad-policy-1,bad-policy-2",
			},
		},
		{
			name:        "ambiguous policies",
			failureType: CGUValidationErrorMsgAmbiguousPolicies,
			info: policiesInfo{
				duplicatedPoliciesNs: map[string][]string{
					"policy-zebra": {"ns1", "ns2"},
					"policy-alpha": {"ns3", "ns4"},
					"policy-mike":  {"ns5"},
				},
			},
			wantMsgContains: "ambiguous policies",
			wantAnnotations: map[string]string{
				CGUEventAnnotationKeyAmbiguousPoliciesList: "policy-alpha,policy-mike,policy-zebra",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reconciler, fakeClient := newTestReconciler(t)
			cgu := newTestCGU()

			reconciler.sendEventCGUVPoliciesValidationFailure(t.Context(), cgu, tt.failureType, tt.info)

			var eventList eventsv1.EventList
			assert.NoError(t, fakeClient.List(t.Context(), &eventList))
			assert.Len(t, eventList.Items, 1)

			ev := eventList.Items[0]
			assert.Equal(t, "Warning", ev.Type)
			assert.Equal(t, CGUEventReasonValidationFailure, ev.Reason)
			assert.Equal(t, CGUEventActionValidate, ev.Action)
			assert.Contains(t, ev.Note, tt.wantMsgContains)

			for k, v := range tt.wantAnnotations {
				assert.Equal(t, v, ev.Annotations[k], "annotation %s", k)
			}
		})
	}
}

func Test_sendEventCGUBatchUpgradeStarted_DeterministicClusterOrder(t *testing.T) {
	// This test verifies that the batch cluster list in event annotations is deterministic
	// regardless of map iteration order

	s := runtime.NewScheme()
	_ = ranv1alpha1.AddToScheme(s)
	_ = eventsv1.AddToScheme(s)

	fakeClient := fake.NewClientBuilder().WithScheme(s).Build()

	reconciler := &ClusterGroupUpgradeReconciler{
		Client:       fakeClient,
		Log:          logr.Discard(),
		Scheme:       s,
		EventEmitter: NewEmitter(fakeClient, s, "ClusterGroupUpgrade"),
	}

	cgu := &ranv1alpha1.ClusterGroupUpgrade{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cgu",
			Namespace: "default",
		},
		Status: ranv1alpha1.ClusterGroupUpgradeStatus{
			Status: ranv1alpha1.UpgradeStatus{
				CurrentBatch: 1,
				CurrentBatchRemediationProgress: map[string]*ranv1alpha1.ClusterRemediationProgress{
					"cluster-zebra":   {},
					"cluster-alpha":   {},
					"cluster-charlie": {},
					"cluster-bravo":   {},
				},
			},
			Clusters: []ranv1alpha1.ClusterState{
				{Name: "cluster-zebra"},
				{Name: "cluster-alpha"},
				{Name: "cluster-charlie"},
				{Name: "cluster-bravo"},
			},
		},
	}

	for i := 0; i < 5; i++ {
		reconciler.sendEventCGUBatchUpgradeStarted(t.Context(), cgu)
	}

	var eventList eventsv1.EventList
	assert.NoError(t, fakeClient.List(t.Context(), &eventList))
	assert.Len(t, eventList.Items, 5)

	assert.Equal(t, "cluster-alpha,cluster-bravo,cluster-charlie,cluster-zebra",
		eventList.Items[0].Annotations[CGUEventAnnotationKeyBatchClustersList])

	for i := 1; i < len(eventList.Items); i++ {
		assert.Equal(t, eventList.Items[0].Annotations[CGUEventAnnotationKeyBatchClustersList],
			eventList.Items[i].Annotations[CGUEventAnnotationKeyBatchClustersList],
			"Event %d batch-clusters annotation should match event 0 (deterministic order)", i)
	}
}

func Test_sendEventCGUBatchUpgradeSuccess_DeterministicClusterOrder(t *testing.T) {
	// This test verifies that the batch cluster list in event annotations is deterministic
	// regardless of map iteration order
	s := runtime.NewScheme()
	_ = ranv1alpha1.AddToScheme(s)
	_ = eventsv1.AddToScheme(s)

	fakeClient := fake.NewClientBuilder().WithScheme(s).Build()

	reconciler := &ClusterGroupUpgradeReconciler{
		Client:       fakeClient,
		Log:          logr.Discard(),
		Scheme:       s,
		EventEmitter: NewEmitter(fakeClient, s, "ClusterGroupUpgrade"),
	}

	cgu := &ranv1alpha1.ClusterGroupUpgrade{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cgu",
			Namespace: "default",
		},
		Status: ranv1alpha1.ClusterGroupUpgradeStatus{
			Status: ranv1alpha1.UpgradeStatus{
				CurrentBatch: 1,
				CurrentBatchRemediationProgress: map[string]*ranv1alpha1.ClusterRemediationProgress{
					"cluster-zebra":   {},
					"cluster-alpha":   {},
					"cluster-charlie": {},
					"cluster-bravo":   {},
				},
			},
			Clusters: []ranv1alpha1.ClusterState{
				{Name: "cluster-zebra"},
				{Name: "cluster-alpha"},
				{Name: "cluster-charlie"},
				{Name: "cluster-bravo"},
			},
		},
	}

	for i := 0; i < 5; i++ {
		reconciler.sendEventCGUBatchUpgradeSuccess(t.Context(), cgu)
	}

	var eventList eventsv1.EventList
	assert.NoError(t, fakeClient.List(t.Context(), &eventList))
	assert.Len(t, eventList.Items, 5)

	assert.Equal(t, "cluster-alpha,cluster-bravo,cluster-charlie,cluster-zebra",
		eventList.Items[0].Annotations[CGUEventAnnotationKeyBatchClustersList])

	for i := 1; i < len(eventList.Items); i++ {
		assert.Equal(t, eventList.Items[0].Annotations[CGUEventAnnotationKeyBatchClustersList],
			eventList.Items[i].Annotations[CGUEventAnnotationKeyBatchClustersList],
			"Event %d batch-clusters annotation should match event 0 (deterministic order)", i)
	}
}

func Test_sendEventCGUBatchUpgradeTimedout_DeterministicClusterOrder(t *testing.T) {
	reconciler, fakeClient := newTestReconciler(t)

	cgu := &ranv1alpha1.ClusterGroupUpgrade{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cgu",
			Namespace: "default",
		},
		Status: ranv1alpha1.ClusterGroupUpgradeStatus{
			Status: ranv1alpha1.UpgradeStatus{
				CurrentBatch: 1,
				CurrentBatchRemediationProgress: map[string]*ranv1alpha1.ClusterRemediationProgress{
					"cluster-zebra":   {State: ranv1alpha1.InProgress},
					"cluster-alpha":   {State: ranv1alpha1.InProgress},
					"cluster-charlie": {State: ranv1alpha1.InProgress},
					"cluster-bravo":   {State: ranv1alpha1.InProgress},
				},
			},
		},
	}

	for i := 0; i < 5; i++ {
		reconciler.sendEventCGUBatchUpgradeTimedout(t.Context(), cgu)
	}

	var eventList eventsv1.EventList
	assert.NoError(t, fakeClient.List(t.Context(), &eventList))
	assert.Len(t, eventList.Items, 5)

	assert.Equal(t, "cluster-alpha,cluster-bravo,cluster-charlie,cluster-zebra",
		eventList.Items[0].Annotations[CGUEventAnnotationKeyTimedoutClustersList])

	for i := 1; i < len(eventList.Items); i++ {
		assert.Equal(t, eventList.Items[0].Annotations[CGUEventAnnotationKeyTimedoutClustersList],
			eventList.Items[i].Annotations[CGUEventAnnotationKeyTimedoutClustersList],
			"Event %d timedout-clusters annotation should match event 0 (deterministic order)", i)
	}
}
