package utils

import (
	"context"
	"testing"

	ranv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/clustergroupupgrades/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	mwv1 "open-cluster-management.io/api/work/v1"
	mwv1alpha1 "open-cluster-management.io/api/work/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func init() {
	testscheme.AddKnownTypes(mwv1.SchemeGroupVersion, &mwv1.ManifestWork{}, &mwv1.ManifestWorkList{})
	testscheme.AddKnownTypes(mwv1alpha1.SchemeGroupVersion, &mwv1alpha1.ManifestWorkReplicaSet{}, &mwv1alpha1.ManifestWorkReplicaSetList{})
}

var (
	fieldValueBooleanTrue = true
	fieldValueStringTrue  = "True"
	fieldValueFalse       = "False"
)

func TestIsManifestWorkCompleted(t *testing.T) {
	tests := []struct {
		name    string
		mw      *mwv1.ManifestWork
		want    bool
		wantErr bool
	}{
		{
			name: "ManifestWork not ready",
			mw:   &mwv1.ManifestWork{},
			want: false,
		},
		{
			name: "ManifestWork with malform annotation",
			mw: &mwv1.ManifestWork{
				ObjectMeta: v1.ObjectMeta{
					Annotations: map[string]string{
						manifestWorkExpectedValuesAnnotation: `bad value`,
					},
				},
				Status: mwv1.ManifestWorkStatus{
					Conditions: []v1.Condition{
						{
							Type:   mwv1.WorkApplied,
							Status: v1.ConditionTrue,
						},
						{
							Type:   mwv1.WorkAvailable,
							Status: v1.ConditionTrue,
						},
					},
				},
			},
			want:    false,
			wantErr: true,
		},
		{
			name: "ManifestWork ready without feedback values",
			mw: &mwv1.ManifestWork{
				ObjectMeta: v1.ObjectMeta{
					Annotations: map[string]string{
						manifestWorkExpectedValuesAnnotation: `[{"manifestIndex":0,"name":"isPrepCompleted","value":"True"}]`,
					},
				},
				Status: mwv1.ManifestWorkStatus{
					Conditions: []v1.Condition{
						{
							Type:   mwv1.WorkApplied,
							Status: v1.ConditionTrue,
						},
						{
							Type:   mwv1.WorkAvailable,
							Status: v1.ConditionTrue,
						},
					},
				},
			},
			want: false,
		},
		{
			name: "ManifestWork ready with feedback values that don't match",
			mw: &mwv1.ManifestWork{
				ObjectMeta: v1.ObjectMeta{
					Annotations: map[string]string{
						manifestWorkExpectedValuesAnnotation: `[{"manifestIndex":0,"name":"isPrepCompleted","value":"True"}]`,
					},
				},
				Status: mwv1.ManifestWorkStatus{
					Conditions: []v1.Condition{
						{
							Type:   mwv1.WorkApplied,
							Status: v1.ConditionTrue,
						},
						{
							Type:   mwv1.WorkAvailable,
							Status: v1.ConditionTrue,
						},
					},
					ResourceStatus: mwv1.ManifestResourceStatus{
						Manifests: []mwv1.ManifestCondition{
							{
								ResourceMeta: mwv1.ManifestResourceMeta{
									Ordinal: 0,
								},
								Conditions: []v1.Condition{
									{
										Type:   mwv1.ManifestApplied,
										Status: v1.ConditionTrue,
									},
									{
										Type:   mwv1.WorkAvailable,
										Status: v1.ConditionTrue,
									},
									{
										Type:   statusFeedbackConditionType,
										Status: v1.ConditionTrue,
									},
								},
								StatusFeedbacks: mwv1.StatusFeedbackResult{
									Values: []mwv1.FeedbackValue{
										{
											Name: "isPrepCompleted",
											Value: mwv1.FieldValue{
												Type:   "String",
												String: &fieldValueFalse,
											},
										},
									},
								},
							},
						},
					},
				},
			},
			want: false,
		},
		{
			name: "ManifestWork ready with matching feedback values",
			mw: &mwv1.ManifestWork{
				ObjectMeta: v1.ObjectMeta{
					Annotations: map[string]string{
						manifestWorkExpectedValuesAnnotation: `[{"manifestIndex":0,"name":"isPrepCompleted","value":"True"}]`,
					},
				},
				Status: mwv1.ManifestWorkStatus{
					Conditions: []v1.Condition{
						{
							Type:   mwv1.WorkApplied,
							Status: v1.ConditionTrue,
						},
						{
							Type:   mwv1.WorkAvailable,
							Status: v1.ConditionTrue,
						},
					},
					ResourceStatus: mwv1.ManifestResourceStatus{
						Manifests: []mwv1.ManifestCondition{
							{
								ResourceMeta: mwv1.ManifestResourceMeta{
									Ordinal: 0,
								},
								Conditions: []v1.Condition{
									{
										Type:   mwv1.ManifestApplied,
										Status: v1.ConditionTrue,
									},
									{
										Type:   mwv1.WorkAvailable,
										Status: v1.ConditionTrue,
									},
									{
										Type:   statusFeedbackConditionType,
										Status: v1.ConditionTrue,
									},
								},
								StatusFeedbacks: mwv1.StatusFeedbackResult{
									Values: []mwv1.FeedbackValue{
										{
											Name: "isPrepCompleted",
											Value: mwv1.FieldValue{
												Type:   mwv1.String,
												String: &fieldValueStringTrue,
											},
										},
									},
								},
							},
						},
					},
				},
			},
			want: true,
		},
		{
			name: "ManifestWork ready with matching boolean feedback values",
			mw: &mwv1.ManifestWork{
				ObjectMeta: v1.ObjectMeta{
					Annotations: map[string]string{
						// lower case true as how strconv.FormatBool prints bools
						manifestWorkExpectedValuesAnnotation: `[{"manifestIndex":0,"name":"isPrepCompleted","value":"true"}]`,
					},
				},
				Status: mwv1.ManifestWorkStatus{
					Conditions: []v1.Condition{
						{
							Type:   mwv1.WorkApplied,
							Status: v1.ConditionTrue,
						},
						{
							Type:   mwv1.WorkAvailable,
							Status: v1.ConditionTrue,
						},
					},
					ResourceStatus: mwv1.ManifestResourceStatus{
						Manifests: []mwv1.ManifestCondition{
							{
								ResourceMeta: mwv1.ManifestResourceMeta{
									Ordinal: 0,
								},
								Conditions: []v1.Condition{
									{
										Type:   mwv1.ManifestApplied,
										Status: v1.ConditionTrue,
									},
									{
										Type:   mwv1.WorkAvailable,
										Status: v1.ConditionTrue,
									},
									{
										Type:   statusFeedbackConditionType,
										Status: v1.ConditionTrue,
									},
								},
								StatusFeedbacks: mwv1.StatusFeedbackResult{
									Values: []mwv1.FeedbackValue{
										{
											Name: "isPrepCompleted",
											Value: mwv1.FieldValue{
												Type:    mwv1.Boolean,
												Boolean: &fieldValueBooleanTrue,
											},
										},
									},
								},
							},
						},
					},
				},
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := IsManifestWorkCompleted(tt.mw)
			if (err != nil) != tt.wantErr {
				t.Errorf("IsManifestWorkCompleted() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("IsManifestWorkCompleted() = %v, want %v", got, tt.want)
			}
		})
	}
}

// stageFeedbackManifestWork builds a ManifestWork whose single manifest carries
// the given status feedback values. When ready is true the manifest is applied,
// available and has synced its feedback (so GetStageFailureFromManifestWork will
// trust it); when false the StatusFeedbackSynced condition is omitted.
func stageFeedbackManifestWork(ready bool, values []mwv1.FeedbackValue) *mwv1.ManifestWork {
	conditions := []v1.Condition{
		{Type: mwv1.ManifestApplied, Status: v1.ConditionTrue},
		{Type: mwv1.ManifestAvailable, Status: v1.ConditionTrue},
	}
	if ready {
		conditions = append(conditions, v1.Condition{Type: statusFeedbackConditionType, Status: v1.ConditionTrue})
	}
	return &mwv1.ManifestWork{
		Status: mwv1.ManifestWorkStatus{
			ResourceStatus: mwv1.ManifestResourceStatus{
				Manifests: []mwv1.ManifestCondition{
					{
						ResourceMeta:    mwv1.ManifestResourceMeta{Ordinal: 0},
						Conditions:      conditions,
						StatusFeedbacks: mwv1.StatusFeedbackResult{Values: values},
					},
				},
			},
		},
	}
}

func stringFeedback(name, value string) mwv1.FeedbackValue {
	v := value
	return mwv1.FeedbackValue{
		Name:  name,
		Value: mwv1.FieldValue{Type: mwv1.String, String: &v},
	}
}

func TestGetStageFailureFromManifestWork(t *testing.T) {
	tests := []struct {
		name        string
		mw          *mwv1.ManifestWork
		wantFailed  bool
		wantMessage string
	}{
		{
			name:       "nil manifestwork",
			mw:         nil,
			wantFailed: false,
		},
		{
			name: "absent completion feedback is in-progress not a failure",
			mw: stageFeedbackManifestWork(true, []mwv1.FeedbackValue{
				stringFeedback("upgradeInProgressConditionMessage", "upgrading"),
			}),
			wantFailed: false,
		},
		{
			name: "UpgradeCompleted False with message is a failure",
			mw: stageFeedbackManifestWork(true, []mwv1.FeedbackValue{
				stringFeedback("isUpgradeCompleted", "False"),
				stringFeedback("upgradeCompletedConditionMessages", "LCA: upgrade failed: disk full"),
			}),
			wantFailed:  true,
			wantMessage: "LCA: upgrade failed: disk full",
		},
		{
			name: "UpgradeCompleted False without message falls back to a generic message",
			mw: stageFeedbackManifestWork(true, []mwv1.FeedbackValue{
				stringFeedback("isUpgradeCompleted", "False"),
			}),
			wantFailed:  true,
			wantMessage: "LCA reported stage Upgrade failed",
		},
		{
			name: "PrepCompleted False with message is a failure",
			mw: stageFeedbackManifestWork(true, []mwv1.FeedbackValue{
				stringFeedback("isPrepCompleted", "False"),
				stringFeedback("prepCompletedConditionMessages", "LCA: prep failed"),
			}),
			wantFailed:  true,
			wantMessage: "LCA: prep failed",
		},
		{
			name: "UpgradeCompleted True is not a failure",
			mw: stageFeedbackManifestWork(true, []mwv1.FeedbackValue{
				stringFeedback("isUpgradeCompleted", "True"),
			}),
			wantFailed: false,
		},
		{
			name: "Idle False is not a failure (Idle excluded by design)",
			mw: stageFeedbackManifestWork(true, []mwv1.FeedbackValue{
				stringFeedback("isIdle", "False"),
				stringFeedback("idleConditionMessages", "aborting"),
			}),
			wantFailed: false,
		},
		{
			name: "UpgradeCompleted False on an unsynced manifest is not trusted",
			mw: stageFeedbackManifestWork(false, []mwv1.FeedbackValue{
				stringFeedback("isUpgradeCompleted", "False"),
				stringFeedback("upgradeCompletedConditionMessages", "LCA: upgrade failed"),
			}),
			wantFailed: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotFailed, gotMessage := GetStageFailureFromManifestWork(tt.mw)
			if gotFailed != tt.wantFailed {
				t.Errorf("GetStageFailureFromManifestWork() failed = %v, want %v", gotFailed, tt.wantFailed)
			}
			if gotMessage != tt.wantMessage {
				t.Errorf("GetStageFailureFromManifestWork() message = %q, want %q", gotMessage, tt.wantMessage)
			}
		})
	}
}

func TestGetFieldValue(t *testing.T) {
	str, boolean, integer, raw := "True", false, int64(42), `{"a":1}`
	mc := &mwv1.ManifestCondition{StatusFeedbacks: mwv1.StatusFeedbackResult{Values: []mwv1.FeedbackValue{
		{Name: "str", Value: mwv1.FieldValue{Type: mwv1.String, String: &str}},
		{Name: "bool", Value: mwv1.FieldValue{Type: mwv1.Boolean, Boolean: &boolean}},
		{Name: "int", Value: mwv1.FieldValue{Type: mwv1.Integer, Integer: &integer}},
		{Name: "raw", Value: mwv1.FieldValue{Type: mwv1.JsonRaw, JsonRaw: &raw}},
	}}}
	assert.Equal(t, "True", getFieldValue(mc, "str"))
	assert.Equal(t, "false", getFieldValue(mc, "bool"))
	assert.Equal(t, "42", getFieldValue(mc, "int"))
	assert.Equal(t, `{"a":1}`, getFieldValue(mc, "raw"))
	assert.Equal(t, "", getFieldValue(mc, "missing"))
}

const (
	mwTestNamespace = "ztp-group"
	mwTestTemplate  = "ibgu-prep"
)

func newMWTestCGU() *ranv1alpha1.ClusterGroupUpgrade {
	return &ranv1alpha1.ClusterGroupUpgrade{
		ObjectMeta: v1.ObjectMeta{
			Name: "cgu", Namespace: mwTestNamespace,
			Annotations: map[string]string{NameSuffixAnnotation: "abcde"},
		},
		Spec: ranv1alpha1.ClusterGroupUpgradeSpec{ManifestWorkTemplates: []string{mwTestTemplate}},
	}
}

func newMWTestTemplate() *mwv1alpha1.ManifestWorkReplicaSet {
	mwrs := &mwv1alpha1.ManifestWorkReplicaSet{ObjectMeta: v1.ObjectMeta{
		Name: mwTestTemplate, Namespace: mwTestNamespace,
		Annotations: map[string]string{manifestWorkExpectedValuesAnnotation: `[{"manifestIndex":0,"name":"isReady","value":"True"}]`},
	}}
	mwrs.Spec.ManifestWorkTemplate.Workload.Manifests = []mwv1.Manifest{{RawExtension: runtime.RawExtension{Raw: []byte(`{"kind":"ConfigMap"}`)}}}
	return mwrs
}

func TestManifestWorkClientHelpers(t *testing.T) {
	ctx := context.Background()
	cgu := newMWTestCGU()
	c, err := getFakeClientFromObjects(newMWTestTemplate())
	require.NoError(t, err)

	manifests, err := GetManifestsFromTemplate(ctx, c, types.NamespacedName{Name: mwTestTemplate, Namespace: mwTestNamespace})
	require.NoError(t, err)
	require.Len(t, manifests, 1)
	assert.JSONEq(t, `{"kind":"ConfigMap"}`, string(manifests[0].Raw))

	_, err = GetManifestsFromTemplate(ctx, c, types.NamespacedName{Name: "missing", Namespace: mwTestNamespace})
	assert.True(t, apierrors.IsNotFound(err))

	_, err = GetManifestWorkForCluster(ctx, c, cgu, 0, "spoke1")
	assert.True(t, apierrors.IsNotFound(err))

	require.NoError(t, CreateManifestWorkForCluster(ctx, c, cgu, 0, "spoke1"))
	mw, err := GetManifestWorkForCluster(ctx, c, cgu, 0, "spoke1")
	require.NoError(t, err)
	assert.Equal(t, "ztp-group.ibgu-prep-abcde", mw.Name)
	assert.Equal(t, "spoke1", mw.Namespace)
	assert.Equal(t, map[string]string{
		"openshift-cluster-group-upgrades/clusterGroupUpgrade":          "cgu",
		"openshift-cluster-group-upgrades/clusterGroupUpgradeNamespace": mwTestNamespace,
	}, mw.Labels)
	assert.Equal(t, newMWTestTemplate().Annotations[manifestWorkExpectedValuesAnnotation], mw.Annotations[manifestWorkExpectedValuesAnnotation])
	assert.Equal(t, newMWTestTemplate().Spec.ManifestWorkTemplate, mw.Spec)

	// Creating it again surfaces the AlreadyExists error.
	assert.True(t, apierrors.IsAlreadyExists(CreateManifestWorkForCluster(ctx, c, cgu, 0, "spoke1")))

	// A missing template is an error and creates nothing.
	missing := newMWTestCGU()
	missing.Spec.ManifestWorkTemplates = []string{"missing"}
	assert.True(t, apierrors.IsNotFound(CreateManifestWorkForCluster(ctx, c, missing, 0, "spoke2")))
	list := &mwv1.ManifestWorkList{}
	require.NoError(t, c.List(ctx, list, client.InNamespace("spoke2")))
	assert.Empty(t, list.Items)
}

func TestCleanupManifestWorkForBatch(t *testing.T) {
	ctx := context.Background()
	newCGU := func() *ranv1alpha1.ClusterGroupUpgrade {
		cgu := newMWTestCGU()
		cgu.Status.RemediationPlan = [][]string{{"spoke1", "spoke2"}, {"spoke3"}}
		return cgu
	}
	setup := func(t *testing.T) client.Client {
		c, err := getFakeClientFromObjects(newMWTestTemplate())
		require.NoError(t, err)
		cgu := newCGU()
		for _, cluster := range []string{"spoke1", "spoke2", "spoke3"} {
			require.NoError(t, CreateManifestWorkForCluster(ctx, c, cgu, 0, cluster))
		}
		// Owned by another CGU in the same namespace: must survive.
		require.NoError(t, c.Create(ctx, &mwv1.ManifestWork{ObjectMeta: v1.ObjectMeta{
			Name: "other", Namespace: "spoke1",
			Labels: map[string]string{"openshift-cluster-group-upgrades/clusterGroupUpgrade": "other-cgu"},
		}}))
		return c
	}
	remaining := func(t *testing.T, c client.Client) []string {
		list := &mwv1.ManifestWorkList{}
		require.NoError(t, c.List(ctx, list))
		var out []string
		for _, mw := range list.Items {
			out = append(out, mw.Namespace+"/"+mw.Name)
		}
		return out
	}

	t.Run("deletes only the batch's owned manifestworks", func(t *testing.T) {
		c := setup(t)
		require.NoError(t, CleanupManifestWorkForBatch(ctx, c, newCGU(), 0))
		assert.ElementsMatch(t, []string{"spoke1/other", "spoke3/ztp-group.ibgu-prep-abcde"}, remaining(t, c))
		// Running it again is a no-op.
		require.NoError(t, CleanupManifestWorkForBatch(ctx, c, newCGU(), 0))
	})

	t.Run("policy rollout is a no-op", func(t *testing.T) {
		c := setup(t)
		cgu := newCGU()
		cgu.Spec.ManifestWorkTemplates = nil
		require.NoError(t, CleanupManifestWorkForBatch(ctx, c, cgu, 0))
		assert.Len(t, remaining(t, c), 4)
	})
}
