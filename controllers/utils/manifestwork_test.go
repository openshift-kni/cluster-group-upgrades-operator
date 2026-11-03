package utils

import (
	"testing"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	mwv1 "open-cluster-management.io/api/work/v1"
)

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
