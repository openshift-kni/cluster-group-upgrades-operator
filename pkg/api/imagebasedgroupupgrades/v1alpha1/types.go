package v1alpha1

import (
	lcav1 "github.com/openshift-kni/lifecycle-agent/api/imagebasedupgrade/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:generate=true

type PlanItem struct {
	//+kubebuilder:validation:Required
	Actions []string `json:"actions"`
	//+kubebuilder:validation:Required
	RolloutStrategy RolloutStrategy `json:"rolloutStrategy"`
}

const (
	// Prep defines the preparing stage for image based upgrade
	Prep = "Prep"
	// Upgrade defines the upgrading stage for image based upgrade
	Upgrade = "Upgrade"
	// Rollback defines the rollback stage for image based upgrade
	Rollback = "Rollback"
	// Finalize defines the finalizing stage for image based upgrade
	Finalize = "Finalize"
	// Abort defines the aborting stage for image based upgrade
	Abort = "Abort"
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
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="IBU Spec"
	//+kubebuilder:validation:Required
	IBUSpec lcav1.ImageBasedUpgradeSpec `json:"ibuSpec"`
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Clusters",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	Clusters []string `json:"clusters,omitempty"`
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Cluster Label Selectors",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	ClusterLabelSelectors []metav1.LabelSelector `json:"clusterLabelSelectors,omitempty"`
	Plan                  []PlanItem             `json:"plan"`
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
