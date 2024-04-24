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
