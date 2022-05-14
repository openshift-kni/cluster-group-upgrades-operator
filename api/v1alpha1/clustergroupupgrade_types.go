/*
Copyright 2021.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// RemediationStrategySpec defines the remediation policy
type RemediationStrategySpec struct {
	// Canaries defines the list of managed clusters that should be remediated first when remediateAction is set to enforce
	Canaries []string `json:"canaries,omitempty"`
	//kubebuilder:validation:Minimum=1
	MaxConcurrency int `json:"maxConcurrency"`
	//+kubebuilder:default=240
	Timeout int `json:"timeout,omitempty"`
}

// BlockingCR defines the Upgrade CRs that block the current CR from running if not completed
type BlockingCR struct {
	Name      string `json:"name,omitempty"`
	Namespace string `json:"namespace,omitempty"`
}

// Actions defines the actions to be done either before or after the managedPolicies are remediated
type Actions struct {
	BeforeEnable    BeforeEnable    `json:"beforeEnable,omitempty"`
	AfterCompletion AfterCompletion `json:"afterCompletion,omitempty"`
}

// BeforeEnable defines the actions to be done before starting upgrade
type BeforeEnable struct {
	// This field defines a map of key/value pairs that identify the cluster labels
	// to be added to the specified clusters. Labels applied to the clusters either
	// defined in spec.clusters or selected by spec.clusterSelector.
	AddClusterLabels map[string]string `json:"addClusterLabels,omitempty"`
	// This field defines a map of key/value pairs that identify the cluster labels
	// to be deleted for the specified clusters. Labels applied to the clusters either
	// defined in spec.clusters or selected by spec.clusterSelector.
	DeleteClusterLabels map[string]string `json:"deleteClusterLabels,omitempty"`
}

// AfterCompletion defines the actions to be done after upgrade is completed
type AfterCompletion struct {
	// This field defines a map of key/value pairs that identify the cluster labels
	// to be added to the specified clusters. Labels applied to the clusters either
	// defined in spec.clusters or selected by spec.clusterSelector.
	AddClusterLabels map[string]string `json:"addClusterLabels,omitempty"`
	// This field defines a map of key/value pairs that identify the cluster labels
	// to be deleted for the specified clusters. Labels applied to the clusters either
	// defined in spec.clusters or selected by spec.clusterSelector.
	DeleteClusterLabels map[string]string `json:"deleteClusterLabels,omitempty"`
	// This field defines whether clean up the resources created for upgrade
	//+kubebuilder:default=true
	DeleteObjects *bool `json:"deleteObjects,omitempty"`
}

// OperatorUpgradeSpec defines the configuration of an operator upgrade
type OperatorUpgradeSpec struct {
	Channel   string `json:"channel,omitempty"`
	Name      string `json:"name,omitempty"`
	Namespace string `json:"namespace,omitempty"`
}

// ClusterGroupUpgradeSpec defines the desired state of ClusterGroupUpgrade
//+kubebuilder:printcolumn:name="Compliance Percentage",type="integer",JSONPath=".status.status.compliancePercentage"
type ClusterGroupUpgradeSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// This field determines whether the cluster would be running a backup prior to the upgrade.
	//+kubebuilder:default=false
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Backup",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:bool"}
	Backup bool `json:"backup,omitempty"`
	// This field determines whether container image pre-caching will be done on all the clusters
	// matching the selector.
	// If required, the pre-caching process starts immediately on all clusters irrespectively of
	// the value of the "enable" flag
	//+kubebuilder:default=false
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="PreCaching",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:bool"}
	PreCaching bool `json:"preCaching,omitempty"`
	// This field determines when the upgrade starts. While false, the upgrade doesn't start. The policies,
	// placement rules and placement bindings are created, but clusters are not added to the placement rule.
	// Once set to true, the clusters start being upgraded, one batch at a time.
	//+kubebuilder:default=true
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Enable",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:bool"}
	Enable *bool `json:"enable,omitempty"`
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Clusters",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	Clusters []string `json:"clusters,omitempty"`
	// This field holds a label common to multiple clusters that will be updated.
	// The expected format is as follows:
	// clusterSelector:
	//   - label1Name=label1Value
	//   - label2Name=label2Value
	// If the value is empty, then the expected format is:
	// clusterSelector:
	//   - label1Name
	// All the clusters matching the labels specified in clusterSelector will be included
	// in the update plan.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Cluster Selector",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	ClusterSelector []string `json:"clusterSelector,omitempty"`
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Remediation Strategy",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	RemediationStrategy *RemediationStrategySpec `json:"remediationStrategy"`
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Managed Policies",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	ManagedPolicies []string `json:"managedPolicies,omitempty"`
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Blocking CRs",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	BlockingCRs []BlockingCR `json:"blockingCRs,omitempty"`
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Actions",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	Actions Actions `json:"actions,omitempty"`
}

// ClusterRemediationProgress stores the remediation progress of a cluster
type ClusterRemediationProgress struct {
	// State should be one of the following: ClusterRemediationNotStarted, ClusterRemediationInProgress, ClusterRemediationCompleted
	State       string `json:"state,omitempty"`
	PolicyIndex *int   `json:"policyIndex,omitempty"`
}

// UpgradeStatus defines the observed state of the upgrade
type UpgradeStatus struct {
	StartedAt             metav1.Time `json:"startedAt,omitempty"`
	CompletedAt           metav1.Time `json:"completedAt,omitempty"`
	CurrentBatch          int         `json:"currentBatch,omitempty"`
	CurrentBatchStartedAt metav1.Time `json:"currentBatchStartedAt,omitempty"`

	CurrentBatchRemediationProgress map[string]*ClusterRemediationProgress `json:"currentBatchRemediationProgress,omitempty"`
}

// ManagedPolicyForUpgrade defines the observed state of a Policy
type ManagedPolicyForUpgrade struct {
	Name      string `json:"name,omitempty"`
	Namespace string `json:"namespace,omitempty"`
}

// PrecachingSpec defines the pre-caching software spec derived from policies
type PrecachingSpec struct {
	PlatformImage                string   `json:"platformImage,omitempty"`
	OperatorsIndexes             []string `json:"operatorsIndexes,omitempty"`
	OperatorsPackagesAndChannels []string `json:"operatorsPackagesAndChannels,omitempty"`
}

// PrecachingStatus defines the observed pre-caching status
type PrecachingStatus struct {
	Spec     *PrecachingSpec   `json:"spec,omitempty"`
	Status   map[string]string `json:"status,omitempty"`
	Clusters []string          `json:"clusters,omitempty"`
}

// BackupStatus defines the observed backup status
type BackupStatus struct {
	Status   map[string]string `json:"status,omitempty"`
	Clusters []string          `json:"clusters,omitempty"`
}

// PolicyContent defines the details of an object configured through a Policy
type PolicyContent struct {
	Kind      string  `json:"kind,omitempty"`
	Name      string  `json:"name,omitempty"`
	Namespace *string `json:"namespace,omitempty"`
}

// ClusterGroupUpgradeStatus defines the observed state of ClusterGroupUpgrade
type ClusterGroupUpgradeStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	// +operator-sdk:csv:customresourcedefinitions:type=status,displayName="Placement Bindings"
	PlacementBindings []string `json:"placementBindings,omitempty"`
	// +operator-sdk:csv:customresourcedefinitions:type=status,displayName="Placement Rules"
	PlacementRules []string `json:"placementRules,omitempty"`
	// +operator-sdk:csv:customresourcedefinitions:type=status,displayName="Copied Policies"
	CopiedPolicies []string `json:"copiedPolicies,omitempty"`
	// +operator-sdk:csv:customresourcedefinitions:type=status,displayName="Conditions"
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// +operator-sdk:csv:customresourcedefinitions:type=status,displayName="Remediation Plan"
	RemediationPlan [][]string `json:"remediationPlan,omitempty"`
	// +operator-sdk:csv:customresourcedefinitions:type=status,displayName="Managed Policies Namespace"
	ManagedPoliciesNs map[string]string `json:"managedPoliciesNs,omitempty"`
	// +operator-sdk:csv:customresourcedefinitions:type=status,displayName="Safe Resource Names"
	SafeResourceNames map[string]string `json:"safeResourceNames,omitempty"`
	// Contains the managed policies (and the namespaces) that have NonCompliant clusters
	// that require updating.
	// +operator-sdk:csv:customresourcedefinitions:type=status,displayName="Managed Policies For Upgrade"
	ManagedPoliciesForUpgrade []ManagedPolicyForUpgrade `json:"managedPoliciesForUpgrade,omitempty"`
	// +operator-sdk:csv:customresourcedefinitions:type=status,displayName="Managed Policies Compliant Before Upgrade"
	ManagedPoliciesCompliantBeforeUpgrade []string `json:"managedPoliciesCompliantBeforeUpgrade,omitempty"`
	// +operator-sdk:csv:customresourcedefinitions:type=status,displayName="Managed Policies Content"
	ManagedPoliciesContent map[string]string `json:"managedPoliciesContent,omitempty"`
	// +operator-sdk:csv:customresourcedefinitions:type=status,displayName="Status"
	Status UpgradeStatus `json:"status,omitempty"`
	// +operator-sdk:csv:customresourcedefinitions:type=status,displayName="Precaching"
	Precaching *PrecachingStatus `json:"precaching,omitempty"`
	// +operator-sdk:csv:customresourcedefinitions:type=status,displayName="Backup"
	Backup *BackupStatus `json:"backup,omitempty"`
	// +operator-sdk:csv:customresourcedefinitions:type=status,displayName="Computed Maximum Concurrency"
	ComputedMaxConcurrency int `json:"computedMaxConcurrency,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:path=clustergroupupgrades,shortName=cgu

// ClusterGroupUpgrade is the Schema for the ClusterGroupUpgrades API
//+operator-sdk:csv:customresourcedefinitions:displayName="Cluster Group Upgrade",resources={{Namespace, v1},{Deployment,apps/v1}}
type ClusterGroupUpgrade struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClusterGroupUpgradeSpec   `json:"spec,omitempty"`
	Status ClusterGroupUpgradeStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// ClusterGroupUpgradeList contains a list of ClusterGroupUpgrade
type ClusterGroupUpgradeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterGroupUpgrade `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ClusterGroupUpgrade{}, &ClusterGroupUpgradeList{})
}
