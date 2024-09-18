package v1alpha1

import (
	lcav1 "github.com/openshift-kni/lifecycle-agent/api/imagebasedupgrade/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:generate=true

const (
	Prep             = "Prep"
	Upgrade          = "Upgrade"
	Rollback         = "Rollback"
	Abort            = "Abort"
	AbortOnFailure   = "AbortOnFailure"
	FinalizeRollback = "FinalizeRollback"
	FinalizeUpgrade  = "FinalizeUpgrade"
)

// RolloutStrategy defines how to rollout ibu
type RolloutStrategy struct {
	//kubebuilder:validation:Minimum=1
	MaxConcurrency int `json:"maxConcurrency"`
	//+kubebuilder:default=240
	Timeout int `json:"timeout,omitempty"`
}

// ImageBasedGroupUpgradeSpec defines the desired state of ImageBasedGroupUpgrade
type ImageBasedGroupUpgradeSpec struct {
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="IBU Spec",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	//+kubebuilder:validation:Required
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="ibuSpec is immutable"
	IBUSpec lcav1.ImageBasedUpgradeSpec `json:"ibuSpec"`
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Cluster Label Selectors",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="clusterLabelSelectors is immutable"
	ClusterLabelSelectors []metav1.LabelSelector `json:"clusterLabelSelectors,omitempty"`
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Plan",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	// +kubebuilder:validation:MaxItems=6
	// +kubebuilder:validation:XValidation:rule="oldSelf.all(element, element in self)",message="plan is append only"
	// +kubebuilder:validation:XValidation:rule="[[['Prep']], [['Prep'], ['Upgrade']], [['Prep', 'Upgrade']], [['Prep'], ['Upgrade'], ['FinalizeUpgrade']], [['Prep'], ['Upgrade', 'FinalizeUpgrade']], [['Prep', 'Upgrade'], ['FinalizeUpgrade']], [['Prep', 'Upgrade', 'FinalizeUpgrade']], [['Rollback']], [['Rollback'], ['FinalizeRollback']], [['Rollback', 'FinalizeRollback']], [['Upgrade']], [['Upgrade'], ['FinalizeUpgrade']], [['Upgrade', 'FinalizeUpgrade']], [['FinalizeUpgrade']],[['FinalizeRollback']], [['Abort']],[['AbortOnFailure']], [['Prep'], ['Abort']], [['Prep'], ['AbortOnFailure']],[['Prep'], ['AbortOnFailure'], ['Upgrade']],[['Prep'], ['AbortOnFailure'], ['Upgrade'], ['AbortOnFailure']],[['Prep'], ['Upgrade'], ['AbortOnFailure']],[['Prep', 'Upgrade'], ['AbortOnFailure']],[['Prep'], ['AbortOnFailure'], ['Upgrade'], ['AbortOnFailure'], ['FinalizeUpgrade']],[['Prep'], ['Upgrade'], ['AbortOnFailure'], ['FinalizeUpgrade']],[['Prep', 'Upgrade'], ['AbortOnFailure'], ['FinalizeUpgrade']]].exists(x, x==self.map(y, y.actions))",message="invalid combinations of actions in the plan"
	Plan []PlanItem `json:"plan"`
}

type PlanItem struct {
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MaxItems=4
	Actions []string `json:"actions"`
	// +kubebuilder:validation:Required
	RolloutStrategy RolloutStrategy `json:"rolloutStrategy"`
}

// ActionMessage defines the action and its message
type ActionMessage struct {
	Action  string `json:"action"`
	Message string `json:"message,omitempty"`
}

// ClusterState defines the current state of a cluster
type ClusterState struct {
	Name             string          `json:"name"`
	CompletedActions []ActionMessage `json:"completedActions,omitempty"`
	FailedActions    []ActionMessage `json:"failedActions,omitempty"`
	CurrentAction    *ActionMessage  `json:"currentAction,omitempty"`
}

// ImageBasedGroupUpgradeStatus is the status field for ImageBasedGroupUpgrade
type ImageBasedGroupUpgradeStatus struct {
	// +operator-sdk:csv:customresourcedefinitions:type=status,displayName="Status"
	ObservedGeneration int64       `json:"observedGeneration,omitempty"`
	StartedAt          metav1.Time `json:"startedAt,omitempty"`
	CompletedAt        metav1.Time `json:"completedAt,omitempty"`
	// +operator-sdk:csv:customresourcedefinitions:type=status,displayName="Conditions"
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	Clusters   []ClusterState     `json:"clusters,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=imagebasedgroupupgrades,shortName=ibgu
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:validation:XValidation:message="Name length must be less than 230 characters", rule="size(self.metadata.name) < 230"

// ImageBasedGroupUpgrade is the schema for upgrading a group of clusters using IBU
// +operator-sdk:csv:customresourcedefinitions:displayName="Image-Based Group Upgrade",resources={{Namespace, v1},{Deployment,apps/v1}}
type ImageBasedGroupUpgrade struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ImageBasedGroupUpgradeSpec   `json:"spec,omitempty"`
	Status ImageBasedGroupUpgradeStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// ImageBasedGroupUpgradeList contains a list of ImageBasedGroupUpgrade
type ImageBasedGroupUpgradeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ImageBasedGroupUpgrade `json:"items"`
}
