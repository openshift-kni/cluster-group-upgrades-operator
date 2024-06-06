package utils

import (
	ranv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/clustergroupupgrades/v1alpha1"
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
	Completed                     ConditionReason
	ClusterSelectionCompleted     ConditionReason
	ValidationCompleted           ConditionReason
	BackupCompleted               ConditionReason
	PrecachingCompleted           ConditionReason
	Failed                        ConditionReason
	IncompleteBlockingCR          ConditionReason
	InProgress                    ConditionReason
	InvalidPlatformImage          ConditionReason
	MissingBlockingCR             ConditionReason
	NotAllManagedPoliciesExist    ConditionReason
	NotAllManifestTemplatesExist  ConditionReason
	AmbiguousManagedPoliciesNames ConditionReason
	NotEnabled                    ConditionReason
	NotStarted                    ConditionReason
	ClusterNotFound               ConditionReason
	NotPresent                    ConditionReason
	PartiallyDone                 ConditionReason
	PrecacheSpecIncomplete        ConditionReason
	PrecacheSpecIsWellFormed      ConditionReason
	TimedOut                      ConditionReason
	UnresolvableDenpendency       ConditionReason
}{
	Completed:                     "Completed",
	ClusterSelectionCompleted:     "ClusterSelectionCompleted",
	ValidationCompleted:           "ValidationCompleted",
	BackupCompleted:               "BackupCompleted",
	PrecachingCompleted:           "PrecachingCompleted",
	Failed:                        "Failed",
	IncompleteBlockingCR:          "IncompleteBlockingCR",
	InProgress:                    "InProgress",
	InvalidPlatformImage:          "InvalidPlatformImage",
	MissingBlockingCR:             "MissingBlockingCR",
	NotAllManagedPoliciesExist:    "NotAllManagedPoliciesExist",
	NotAllManifestTemplatesExist:  "NotAllManifestTemplatesExist",
	AmbiguousManagedPoliciesNames: "AmbiguousManagedPoliciesNames",
	NotEnabled:                    "NotEnabled",
	NotStarted:                    "NotStarted",
	ClusterNotFound:               "ClusterNotFound",
	NotPresent:                    "NotPresent",
	PartiallyDone:                 "PartiallyDone",
	PrecacheSpecIncomplete:        "PrecacheSpecIncomplete",
	PrecacheSpecIsWellFormed:      "PrecacheSpecIsWellFormed",
	TimedOut:                      "TimedOut",
	UnresolvableDenpendency:       "UnresolvableDenpendency",
}

// InProgressMessages defines the in progress messages for the conditions by rollout type
var InProgressMessages = map[ranv1alpha1.RolloutType]string{
	ranv1alpha1.RolloutTypes.Policy:       "Remediating non-compliant policies",
	ranv1alpha1.RolloutTypes.ManifestWork: "Rolling out manifestworks",
}

// TimeoutMessages defines the timeout messages for the conditions by rollout type
var TimeoutMessages = map[ranv1alpha1.RolloutType]string{
	ranv1alpha1.RolloutTypes.Policy:       "Policy remediation took too long",
	ranv1alpha1.RolloutTypes.ManifestWork: "Manifestwork rollout took too long",
}

// CompletedMessages defines the completed messages for the conditions by rollout type
var CompletedMessages = map[ranv1alpha1.RolloutType]string{
	ranv1alpha1.RolloutTypes.Policy:       "All clusters are compliant with all the managed policies",
	ranv1alpha1.RolloutTypes.ManifestWork: "All manifestworks rolled out successfully on all clusters",
}

// SetStatusCondition is a convenience wrapper for meta.SetStatusCondition that takes in the types defined here and converts them to strings
func SetStatusCondition(existingConditions *[]metav1.Condition, conditionType ConditionType, conditionReason ConditionReason, conditionStatus metav1.ConditionStatus, message string) {
	conditions := *existingConditions
	condition := meta.FindStatusCondition(*existingConditions, string(conditionType))
	if condition != nil &&
		condition.Status != conditionStatus &&
		conditions[len(conditions)-1].Type != string(conditionType) {
		meta.RemoveStatusCondition(existingConditions, string(conditionType))
	}
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
