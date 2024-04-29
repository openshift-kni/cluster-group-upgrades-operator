package v1alpha1

import (
	lcav1alpha1 "github.com/openshift-kni/lifecycle-agent/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:generate=true

// ImageBasedUpgradeAction defines the type for the actions field
// +kubebuilder:validation:Enum=Abort;Prep;Upgrade;Rollback;Finalize
type ImageBasedUpgradeAction string

// IBUActions defines the string values for valid actions
var IBUActions = struct {
	ImageBasedUpgradeAction
	Prep     ImageBasedUpgradeAction
	Upgrade  ImageBasedUpgradeAction
	Rollback ImageBasedUpgradeAction
	Abort    ImageBasedUpgradeAction
	Finalize ImageBasedUpgradeAction
}{
	Prep:     "Prep",
	Upgrade:  "Upgrade",
	Rollback: "Rollback",
	Finalize: "Finalize",
	Abort:    "Abort",
}

// RolloutStrategy defines how to rollout ibu
type RolloutStrategy struct {
	//kubebuilder:validation:Minimum=1
	MaxConcurrency int `json:"maxConcurrency"`
	//+kubebuilder:default=240
	Timeout int `json:"timeout,omitempty"`
}

// ClusterGroupImageBasedUpgradeSpec defines the desired state of ClusterGroupImageBasedUpgrade
// +kubebuilder:validation:XValidation:message="Invalid list of actions", rule="self.actions==['Prep'] || self.actions==['Prep','Upgrade'] || self.actions==['Prep','Upgrade','Finalize'] || self.actions==['Rollback'] || self.actions==['Rollback', 'Finalize'] || self.actions==['Upgrade'] || self.actions==['Finalize']"
type ClusterGroupImageBasedUpgradeSpec struct {
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="IBU Spec"
	IBUSpec lcav1alpha1.ImageBasedUpgradeSpec `json:"ibuSpec"`
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Clusters",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	Clusters []string `json:"clusters,omitempty"`
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Cluster Label Selectors",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	ClusterLabelSelectors []metav1.LabelSelector `json:"clusterLabelSelectors,omitempty"`
	//kubebuilder:validation:Minimum=1
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Actions"
	Actions []ImageBasedUpgradeAction `json:"actions"`
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Rollout Strategy",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	RolloutStrategy RolloutStrategy `json:"rolloutStrategy,omitempty"`
}

// ClusterGroupImageBasedUpgradeStatus is the status field for ClusterGroupImageBasedUpgrade
type ClusterGroupImageBasedUpgradeStatus struct {
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=clustergroupimagebasedupgrades,shortName=cgibu
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// ClusterGroupImageBasedUpgrade is the schema for upgrading a group of clusters using IBU
type ClusterGroupImageBasedUpgrade struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClusterGroupImageBasedUpgradeSpec   `json:"spec,omitempty"`
	Status ClusterGroupImageBasedUpgradeStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// ClusterGroupImageBasedUpgradeList contains a list of ClusterGroupImageBasedUpgrade
type ClusterGroupImageBasedUpgradeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterGroupImageBasedUpgrade `json:"items"`
}
