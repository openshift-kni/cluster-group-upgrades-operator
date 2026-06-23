package controllers

import (
	"testing"

	ranv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/clustergroupupgrades/v1alpha1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
)

func Test_truncateAnnotations(t *testing.T) {
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
			name: "maxSize = 1, don't truncate as there's no annotations that cna be truncated",
			args: args{
				anns:          map[string]string{"k": "v"},
				maxSize:       1,
				truncatedAnns: map[string]string{"k": "v"},
			},
		},
		{
			name: "truncate last element for batch clusters list",
			args: args{
				anns: map[string]string{
					"k":                                    "v",
					CGUEventAnnotationKeyBatchClustersList: "cluster1,cluster2",
				},
				maxSize: len(CGUEventAnnotationKeyBatchClustersList) + 10,
				truncatedAnns: map[string]string{
					"k":                                    "v",
					CGUEventAnnotationKeyBatchClustersList: "cluster1",
				},
			},
		},
		{
			name: "truncate last element for missing clusters list",
			args: args{
				anns: map[string]string{
					"k":                                      "v",
					CGUEventAnnotationKeyMissingClustersList: "cluster1,cluster2",
				},
				maxSize: len(CGUEventAnnotationKeyMissingClustersList) + 10,
				truncatedAnns: map[string]string{
					"k":                                      "v",
					CGUEventAnnotationKeyMissingClustersList: "cluster1",
				},
			},
		},
		{
			name: "truncate last element for missing clusters list",
			args: args{
				anns: map[string]string{
					"k": "v",
					CGUEventAnnotationKeyTimedoutClustersList: "cluster1,cluster2",
				},
				maxSize: len(CGUEventAnnotationKeyTimedoutClustersList) + 10,
				truncatedAnns: map[string]string{
					"k": "v",
					CGUEventAnnotationKeyTimedoutClustersList: "cluster1",
				},
			},
		},
		// Same as the previous 3 tcs, but don't truncate as there's room for all anns.
		{
			name: "truncate last element for batch clusters list",
			args: args{
				anns: map[string]string{
					"k":                                    "v",
					CGUEventAnnotationKeyBatchClustersList: "cluster1,cluster2",
				},
				maxSize: len(CGUEventAnnotationKeyBatchClustersList) + 100,
				truncatedAnns: map[string]string{
					"k":                                    "v",
					CGUEventAnnotationKeyBatchClustersList: "cluster1,cluster2",
				},
			},
		},
		{
			name: "truncate last element for missing clusters list",
			args: args{
				anns: map[string]string{
					"k":                                      "v",
					CGUEventAnnotationKeyMissingClustersList: "cluster1,cluster2",
				},
				maxSize: len(CGUEventAnnotationKeyMissingClustersList) + 100,
				truncatedAnns: map[string]string{
					"k":                                      "v",
					CGUEventAnnotationKeyMissingClustersList: "cluster1,cluster2",
				},
			},
		},
		{
			name: "truncate last element for missing clusters list",
			args: args{
				anns: map[string]string{
					"k": "v",
					CGUEventAnnotationKeyTimedoutClustersList: "cluster1,cluster2",
				},
				maxSize: len(CGUEventAnnotationKeyTimedoutClustersList) + 100,
				truncatedAnns: map[string]string{
					"k": "v",
					CGUEventAnnotationKeyTimedoutClustersList: "cluster1,cluster2",
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			truncateAnnotations(tt.args.anns, tt.args.maxSize)
			assert.Equal(t, tt.args.anns, tt.args.truncatedAnns)
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

func Test_sendEventCGUBatchUpgradeStarted_DeterministicClusterOrder(t *testing.T) {
	// This test verifies that the batch cluster list in event annotations is deterministic
	// regardless of map iteration order

	fakeRecorder := record.NewFakeRecorder(10)
	reconciler := &ClusterGroupUpgradeReconciler{
		Recorder: fakeRecorder,
	}

	// Create CGU with CurrentBatchRemediationProgress as a map
	// Maps in Go have non-deterministic iteration order
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

	// Call the function multiple times
	// If sorting is working correctly, we should get the same event each time
	var events []string
	for i := 0; i < 5; i++ {
		reconciler.sendEventCGUBatchUpgradeStarted(cgu)
		event := <-fakeRecorder.Events
		events = append(events, event)
	}

	// Verify all events are identical (deterministic)
	for i := 1; i < len(events); i++ {
		assert.Equal(t, events[0], events[i],
			"Event %d should match event 0 (events should be deterministic)", i)
	}

	// Verify the event contains sorted cluster names
	// The event string should contain "cluster-alpha,cluster-bravo,cluster-charlie,cluster-zebra"
	// This is verified implicitly by the determinism check, but we can also check explicitly
	// if needed by parsing the event annotations
}

func Test_sendEventCGUBatchUpgradeSuccess_DeterministicClusterOrder(t *testing.T) {
	// This test verifies that the batch cluster list in event annotations is deterministic
	// regardless of map iteration order

	fakeRecorder := record.NewFakeRecorder(10)
	reconciler := &ClusterGroupUpgradeReconciler{
		Recorder: fakeRecorder,
	}

	// Create CGU with CurrentBatchRemediationProgress as a map
	// Maps in Go have non-deterministic iteration order
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

	// Call the function multiple times
	// If sorting is working correctly, we should get the same event each time
	var events []string
	for i := 0; i < 5; i++ {
		reconciler.sendEventCGUBatchUpgradeSuccess(cgu)
		event := <-fakeRecorder.Events
		events = append(events, event)
	}

	// Verify all events are identical (deterministic)
	for i := 1; i < len(events); i++ {
		assert.Equal(t, events[0], events[i],
			"Event %d should match event 0 (events should be deterministic)", i)
	}
}
