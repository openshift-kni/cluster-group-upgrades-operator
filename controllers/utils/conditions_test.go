package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSetStatusCondition(t *testing.T) {

	testcases := []struct {
		name             string
		beforeConditions []metav1.Condition
		afterConditions  []metav1.Condition
		condition        metav1.Condition
	}{
		{
			"Add condition",
			[]metav1.Condition{},
			[]metav1.Condition{
				metav1.Condition{
					Type: string(ConditionTypes.Progressing),
				},
			},
			metav1.Condition{
				Type: string(ConditionTypes.Progressing),
			},
		},
		{
			"Setting same condition should not change order",
			[]metav1.Condition{
				metav1.Condition{
					Type: string(ConditionTypes.BackupSuceeded), Status: metav1.ConditionTrue,
				},
				metav1.Condition{
					Type: string(ConditionTypes.Progressing),
				},
			},
			[]metav1.Condition{
				metav1.Condition{
					Type: string(ConditionTypes.BackupSuceeded), Status: metav1.ConditionTrue,
				},
				metav1.Condition{
					Type: string(ConditionTypes.Progressing),
				},
			},
			metav1.Condition{
				Type: string(ConditionTypes.BackupSuceeded), Status: metav1.ConditionTrue,
			},
		},
		{
			"Change in the status should change the order",
			[]metav1.Condition{
				metav1.Condition{
					Type: string(ConditionTypes.BackupSuceeded), Status: metav1.ConditionTrue,
				},
				metav1.Condition{
					Type: string(ConditionTypes.Progressing),
				},
			},
			[]metav1.Condition{
				metav1.Condition{
					Type: string(ConditionTypes.Progressing),
				},
				metav1.Condition{
					Type: string(ConditionTypes.BackupSuceeded), Status: metav1.ConditionFalse,
				},
			},
			metav1.Condition{
				Type: string(ConditionTypes.BackupSuceeded), Status: metav1.ConditionFalse,
			},
		},
	}

	for _, tc := range testcases {
		SetStatusCondition(&tc.beforeConditions, ConditionType(tc.condition.Type), ConditionReason(tc.condition.Reason), tc.condition.Status, tc.condition.Message)
		assert.Equal(t, len(tc.beforeConditions), len(tc.afterConditions))
		for i := 0; i < len(tc.beforeConditions); i++ {
			assert.Equal(t, tc.beforeConditions[i].Type, tc.afterConditions[i].Type)
		}
	}
}
