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
	mwv1 "open-cluster-management.io/api/work/v1"
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

// NamespacedCR defines the name and namespace of a custom resource
type NamespacedCR struct {
	Name      string `json:"name,omitempty"`
	Namespace string `json:"namespace,omitempty"`
}

// BlockingCR defines the Upgrade CRs that block the current CR from running if not completed
type BlockingCR NamespacedCR

// PreCachingConfigCR defines the reference to the pre-caching config CR
type PreCachingConfigCR NamespacedCR

// Actions defines the actions to be done either before or after the managedPolicies are remediated
type Actions struct {
	BeforeEnable    *BeforeEnable    `json:"beforeEnable,omitempty"`
	AfterCompletion *AfterCompletion `json:"afterCompletion,omitempty"`
}

// BeforeEnable defines the actions to be done before starting upgrade
type BeforeEnable struct {
	// This field defines a map of key/value pairs that identify the cluster labels
	// to be added or updated to the defined clusters.
	AddClusterLabels map[string]string `json:"addClusterLabels,omitempty"`
	// This field defines a map of key/value pairs that identify the cluster labels
	// to be deleted for the defined clusters.
	// Deprecated: Use RemoveClusterLabels instead.
	DeleteClusterLabels map[string]string `json:"deleteClusterLabels,omitempty"`
	// This field defines a list of labels to be removed for the defined clusters.
	RemoveClusterLabels []string `json:"removeClusterLabels,omitempty"`
	// This field defines a map of key/value pairs that identify the cluster annotations
	// to be added or updated to the defined clusters.
	AddClusterAnnotations map[string]string `json:"addClusterAnnotations,omitempty"`
	// This field defines a list of annotations to be removed for the defined clusters.
	RemoveClusterAnnotations []string `json:"removeClusterAnnotations,omitempty"`
}

// AfterCompletion defines the actions to be done after upgrade is completed
type AfterCompletion struct {
	// This field defines a map of key/value pairs that identify the cluster labels
	// to be added to the defined clusters.
	AddClusterLabels map[string]string `json:"addClusterLabels,omitempty"`
	// This field defines a map of key/value pairs that identify the cluster labels
	// to be deleted for the defined clusters.
	// Deprecated: Use RemoveClusterLabels instead.
	DeleteClusterLabels map[string]string `json:"deleteClusterLabels,omitempty"`
	// This field defines a list of labels to be removed for the defined clusters.
	RemoveClusterLabels []string `json:"removeClusterLabels,omitempty"`
	// This field defines a map of key/value pairs that identify the cluster annotations
	// to be added or updated to the defined clusters.
	AddClusterAnnotations map[string]string `json:"addClusterAnnotations,omitempty"`
	// This field defines a list of annotations to be removed for the defined clusters.
	RemoveClusterAnnotations []string `json:"removeClusterAnnotations,omitempty"`
	// This field defines whether clean up the resources created for upgrade
	//+kubebuilder:default=true
	DeleteObjects *bool `json:"deleteObjects,omitempty"`
}

// BatchTimeoutAction selections
var BatchTimeoutAction = struct {
	Continue string
	Abort    string
}{
	Continue: "Continue",
	Abort:    "Abort",
}

// OperatorUpgradeSpec defines the configuration of an operator upgrade
type OperatorUpgradeSpec struct {
	Channel   string `json:"channel,omitempty"`
	Name      string `json:"name,omitempty"`
	Namespace string `json:"namespace,omitempty"`
}

// TODO add validation so manifest templates and managed policies are not used at the same time

// ClusterGroupUpgradeSpec defines the desired state of ClusterGroupUpgrade
type ClusterGroupUpgradeSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// This field determines whether the cluster would be running a backup prior to the upgrade.
	// Deprecated: Use lcm.openshift.io/ImageBasedGroupUpgrade instead for SNO upgrades with built-in backup/rollback functionality
	//+kubebuilder:deprecatedversion:warning="ClusterGroupUpgradeSpec.Backup is deprecated, use lcm.openshift.io/ImageBasedGroupUpgrade instead for SNO upgrades with built-in backup/rollback functionality"
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
	// This field specifies a reference to a pre-caching config custom resource that contains the additional
	// pre-caching configurations.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="PreCachingConfigRef",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	PreCachingConfigRef PreCachingConfigCR `json:"preCachingConfigRef,omitempty"`
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
	// Deprecated: Use ClusterLabelSelectors instead
	//+kubebuilder:deprecatedversion:warning="ClusterGroupUpgradeSpec.ClusterSelector is deprecated, use ClusterLabelSelectors instead"
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Cluster Selector",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	ClusterSelector []string `json:"clusterSelector,omitempty"`
	// This field holds a list of expressions or labels that will be used to determine what clusters to include in the operation.
	// The expected format is as follows:
	// clusterLabelSelectors:
	//   - matchExpressions:
	//       - key: label1
	//         operator: In
	//         values:
	//           - value1a
	//           - value1b
	//   - matchLabels:
	//       label2: value2
	//   - matchExpressions:
	//       - key: label3
	//         operator: In
	//         values:
	//           - value3
	//     matchLabels:
	//       label4: value4
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Cluster Label Selectors",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	ClusterLabelSelectors []metav1.LabelSelector `json:"clusterLabelSelectors,omitempty"`
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Remediation Strategy",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	RemediationStrategy *RemediationStrategySpec `json:"remediationStrategy"`
	//+kubebuilder:validation:Optional
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Managed Policies",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	ManagedPolicies []string `json:"managedPolicies,omitempty"`
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Manifest Work Templates",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	//+kubebuilder:validation:Optional
	ManifestWorkTemplates []string `json:"manifestWorkTemplates"`
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Blocking CRs",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	BlockingCRs []BlockingCR `json:"blockingCRs,omitempty"`
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Actions",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	Actions Actions `json:"actions,omitempty"`
	// The Batch Timeout Action can be specified to control what happens when a batch times out. The default value is `Continue`.
	// The possible values are:
	//   - Continue
	//   - Abort
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="BatchTimeoutAction",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	BatchTimeoutAction string `json:"batchTimeoutAction,omitempty"`
}

// RolloutType is a string representing the rollout type
type RolloutType string

// RolloutTypes define the supported rollout types
var RolloutTypes = struct {
	ManifestWork RolloutType
	Policy       RolloutType
}{
	ManifestWork: "ManifestWork",
	Policy:       "Policy",
}

// RolloutType returns the rollout type based on the spec content
func (cgu ClusterGroupUpgrade) RolloutType() RolloutType {
	if len(cgu.Spec.ManifestWorkTemplates) > 0 {
		return RolloutTypes.ManifestWork
	}
	return RolloutTypes.Policy
}

// ClusterRemediationProgress stores the remediation progress of a cluster
type ClusterRemediationProgress struct {
	// State should be one of the following: NotStarted, InProgress, Completed
	State             string      `json:"state,omitempty"`
	ManifestWorkIndex *int        `json:"manifestWorkIndex,omitempty"`
	PolicyIndex       *int        `json:"policyIndex,omitempty"`
	FirstCompliantAt  metav1.Time `json:"firstComplaintAt,omitempty"`
}

// ClusterRemediationProgress possible states
const (
	NotStarted = "NotStarted"
	InProgress = "InProgress"
	Completed  = "Completed"
)

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

// PolicyStatus defines the status of a certain policy
type PolicyStatus struct {
	Name   string `json:"name"`
	Status string `json:"status,omitempty"`
}

// ManifestWorkStatus defines the status of a certain ManifestWork
type ManifestWorkStatus struct {
	Name   string                      `json:"name"`
	Status mwv1.ManifestResourceStatus `json:"status,omitempty"`
}

// ClusterState defines the final state of a cluster
type ClusterState struct {
	Name                string              `json:"name"`
	State               string              `json:"state"`
	CurrentPolicy       *PolicyStatus       `json:"currentPolicy,omitempty"`
	CurrentManifestWork *ManifestWorkStatus `json:"currentManifestWork,omitempty"`
}

// PrecachingSpec defines the pre-caching software spec derived from policies
type PrecachingSpec struct {
	PlatformImage                string   `json:"platformImage,omitempty"`
	OperatorsIndexes             []string `json:"operatorsIndexes,omitempty"`
	OperatorsPackagesAndChannels []string `json:"operatorsPackagesAndChannels,omitempty"`
	ExcludePrecachePatterns      []string `json:"excludePrecachePatterns,omitempty"`
	SpaceRequired                string   `json:"spaceRequired,omitempty"`
	AdditionalImages             []string `json:"additionalImages,omitempty"`
}

// PrecachingStatus defines the observed pre-caching status
type PrecachingStatus struct {
	Spec   *PrecachingSpec   `json:"spec,omitempty"`
	Status map[string]string `json:"status,omitempty"`
	//+kubebuilder:deprecatedversion:warning="PrecachingStatus.Clusters is deprecated"
	Clusters []string `json:"clusters,omitempty"`
}

// BackupStatus defines the observed backup status
type BackupStatus struct {
	StartedAt metav1.Time       `json:"startedAt,omitempty"`
	Status    map[string]string `json:"status,omitempty"`
	//+kubebuilder:deprecatedversion:warning="BackupStatus.Clusters is deprecated"
	Clusters []string `json:"clusters,omitempty"`
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
	// Deprecated
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
	// +operator-sdk:csv:customresourcedefinitions:type=status,displayName="Clusters"
	Clusters []ClusterState `json:"clusters,omitempty"`
	// +operator-sdk:csv:customresourcedefinitions:type=status,displayName="Status"
	Status UpgradeStatus `json:"status,omitempty"`
	// +operator-sdk:csv:customresourcedefinitions:type=status,displayName="Precaching"
	Precaching *PrecachingStatus `json:"precaching,omitempty"`
	// +operator-sdk:csv:customresourcedefinitions:type=status,displayName="Backup"
	Backup *BackupStatus `json:"backup,omitempty"`
	// +operator-sdk:csv:customresourcedefinitions:type=status,displayName="Computed Maximum Concurrency"
	ComputedMaxConcurrency int `json:"computedMaxConcurrency,omitempty"`
}

// +genclient
//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:path=clustergroupupgrades,shortName=cgu
//+kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
//+kubebuilder:printcolumn:name="State",type="string",JSONPath=".status.conditions[-1:].reason"
//+kubebuilder:printcolumn:name="Details",type="string",JSONPath=".status.conditions[-1:].message"

// ClusterGroupUpgrade is the Schema for the ClusterGroupUpgrades API
// +operator-sdk:csv:customresourcedefinitions:displayName="Cluster Group Upgrade",resources={{Namespace, v1},{Deployment,apps/v1}}
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

// PlatformPreCachingSpec modify the default pre-caching behavior and values derived by TALM.
type PlatformPreCachingSpec struct {
	// Override the pre-cached OpenShift platform image derived by TALM
	PlatformImage string `json:"platformImage,omitempty"`
	// Override the pre-cached OLM index images derived by TALM (list of image pull specs)
	OperatorsIndexes []string `json:"operatorsIndexes,omitempty"`
	// Override the pre-cached operator packages and channels derived by TALM (list of <package:channel> string entries)
	OperatorsPackagesAndChannels []string `json:"operatorsPackagesAndChannels,omitempty"`
	// Override the pre-caching workload image pull spec - typically derived by TALM from the operator
	// ClusterServiceVersion (csv) object.
	PreCacheImage string `json:"preCacheImage,omitempty"`
}

// PreCachingConfigSpec defines the desired state of PreCachingConfig
type PreCachingConfigSpec struct {
	// Important: Run "make generate" to regenerate code after modifying this file

	// Overrides modify the default pre-caching behaviour and values derived by TALM.
	Overrides PlatformPreCachingSpec `json:"overrides,omitempty"`
	// Amount of space required for the pre-caching job
	SpaceRequired string `json:"spaceRequired,omitempty"`
	// List of patterns to exclude from pre-caching
	ExcludePrecachePatterns []string `json:"excludePrecachePatterns,omitempty"`
	// List of additional image pull specs for the pre-caching job
	AdditionalImages []string `json:"additionalImages,omitempty"`
}

//+kubebuilder:object:root=true

// PreCachingConfig is the Schema for the precachingconfigs API
type PreCachingConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec PreCachingConfigSpec `json:"spec,omitempty"`
}

//+kubebuilder:object:root=true

// PreCachingConfigList contains a list of PreCachingConfig
type PreCachingConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PreCachingConfig `json:"items"`
}
