package utils

import (
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ConditionType is a string representing the condition's type
type ConditionType string

// ConditionTypes define the different types of conditions that will be set
var ConditionTypes = struct {
	BackupSuceeded     ConditionType
	ClustersSelected   ConditionType
	PrecacheSpecValid  ConditionType
	PrecachingSuceeded ConditionType
	Progressing        ConditionType
	Succeeded          ConditionType
	Validated          ConditionType
}{
	BackupSuceeded:     "BackupSuceeded",
	ClustersSelected:   "ClustersSelected",
	PrecacheSpecValid:  "PrecacheSpecValid",
	PrecachingSuceeded: "PrecachingSuceeded",
	Progressing:        "Progressing",
	Succeeded:          "Succeeded",
	Validated:          "Validated",
}

// ConditionReason is a string representing the condition's reason
type ConditionReason string

// ConditionReasons define the different reasons that conditions will be set for
var ConditionReasons = struct {
	Completed                  ConditionReason
	ClusterSelectionCompleted  ConditionReason
	ValidationCompleted        ConditionReason
	BackupCompleted            ConditionReason
	PrecachingCompleted        ConditionReason
	Failed                     ConditionReason
	IncompleteBlockingCR       ConditionReason
	InProgress                 ConditionReason
	InvalidPlatformImage       ConditionReason
	MissingBlockingCR          ConditionReason
	NotAllManagedPoliciesExist ConditionReason
	NotEnabled                 ConditionReason
	NotStarted                 ConditionReason
	ClusterNotFound            ConditionReason
	NotPresent                 ConditionReason
	PartiallyDone              ConditionReason
	PrecacheSpecIsWellFormed   ConditionReason
	TimedOut                   ConditionReason
}{
	Completed:                  "Completed",
	ClusterSelectionCompleted:  "ClusterSelectionCompleted",
	ValidationCompleted:        "ValidationCompleted",
	BackupCompleted:            "BackupCompleted",
	PrecachingCompleted:        "PrecachingCompleted",
	Failed:                     "Failed",
	IncompleteBlockingCR:       "IncompleteBlockingCR",
	InProgress:                 "InProgress",
	InvalidPlatformImage:       "InvalidPlatformImage",
	MissingBlockingCR:          "MissingBlockingCR",
	NotAllManagedPoliciesExist: "NotAllManagedPoliciesExist",
	NotEnabled:                 "NotEnabled",
	NotStarted:                 "NotStarted",
	ClusterNotFound:            "ClusterNotFound",
	NotPresent:                 "NotPresent",
	PartiallyDone:              "PartiallyDone",
	PrecacheSpecIsWellFormed:   "PrecacheSpecIsWellFormed",
	TimedOut:                   "TimedOut",
}

// SetStatusCondition is a convenience wrapper for meta.SetStatusCondition that takes in the types defined here and converts them to strings
func SetStatusCondition(existingConditions *[]metav1.Condition, conditionType ConditionType, conditionReason ConditionReason, conditionStatus metav1.ConditionStatus, message string) {
	meta.SetStatusCondition(
		existingConditions,
		metav1.Condition{
			Type:    string(conditionType),
			Status:  conditionStatus,
			Reason:  string(conditionReason),
			Message: message,
		},
	)
}
