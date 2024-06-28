package v1alpha1

import (
	lcav1 "github.com/openshift-kni/lifecycle-agent/api/imagebasedupgrade/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:generate=true

// ImageBasedUpgradeAction defines the type of the action to perform. Abort will stop the upgrade by setting the IBU stage to Idle. Finalize will remove the previous stateroot after upgrade is done by setting the IBU stage to Idle. Prep will start preparing the upgrade by setting the IBU stage to Prep. Upgrade will start the upgrade process by setting the IBU stage to Upgrade. Rollback will pivot back to previous stateroot by setting the IBU stage to Rollback.
type ImageBasedUpgradeAction struct {
	// +kubebuilder:validation:Enum=Abort;Prep;Upgrade;Rollback;Finalize
	Action string `json:"action"`
}

func (i ImageBasedUpgradeAction) String() string {
	return i.Action
}

var (
	// Prep defines the preparing stage for image based upgrade
	Prep = ImageBasedUpgradeAction{"Prep"}
	// Upgrade defines the upgrading stage for image based upgrade
	Upgrade = ImageBasedUpgradeAction{"Upgrade"}
	// Rollback defines the rollback stage for image based upgrade
	Rollback = ImageBasedUpgradeAction{"Rollback"}
	// Finalize defines the finalizing stage for image based upgrade
	Finalize = ImageBasedUpgradeAction{"Finalize"}
	// Abort defines the aborting stage for image based upgrade
	Abort = ImageBasedUpgradeAction{"Abort"}
	// Unknown defines an unknown ImageBasedUpgrade action
	Unknown = ImageBasedUpgradeAction{""}
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
	//kubebuilder:validation:Minimum=1
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Actions"
	// kubebuilder:validation:XValidation:message="Invalid list of actions", rule="self.map(x,x.action)==['Prep'] || self.map(x,x.action)==['Prep','Upgrade'] || self.map(x,x.action)==['Prep','Upgrade','Finalize'] || self.map(x,x.action)==['Rollback'] || self.map(x,x.action)==['Rollback', 'Finalize'] || self.map(x,x.action)==['Upgrade'] || self.map(x,x.action)==['Upgrade','Finalize'] || self.map(x,x.action)==['Finalize'] || self.map(x,x.action)==['Abort'] || self.map(x,x.action)==['Prep', 'Abort'] || self.map(x,x.action)==['Prep', 'Upgrade', 'Rollback'] || self.map(x,x.action)==['Prep', 'Upgrade', 'Rollback', 'Finalize'] || self.map(x,x.action)==['Upgrade','Rollback'] || self.map(x,x.action)==['Upgrade','Rollback','Finalize']"
	// +kubebuilder:validation:XValidation:message="You can only add actions to the list", rule="oldSelf.all(x, (x in self))"
	// +kubebuilder:validation:MaxItems=4
	Actions []ImageBasedUpgradeAction `json:"actions"`
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Rollout Strategy",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	//+kubebuilder:validation:Required
	RolloutStrategy RolloutStrategy `json:"rolloutStrategy,omitempty"`
}

// ClusterState defines the current state of a cluster
type ClusterState struct {
	Name          string  `json:"name"`
	State         string  `json:"state"`
	CurrentAction *string `json:"CurrentAction,omitempty"`
	Message       *string `json:"message,omitempty"`
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
