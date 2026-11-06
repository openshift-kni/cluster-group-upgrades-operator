/*
Copyright 2023.

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

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// DefaultNamespace is LCA's default install namespace: the namespace the
// telco-reference configuration installs LCA into, and the one TALM copies
// BackupContent CRs and pull secrets into on the spoke. LCA resolves the
// name-only references in an IBU spec (spec.seedImageRef.pullSecretRef and
// spec.backupContent) in its own install namespace, which defaults to this value
// (see common.OperatorNamespace). It is exported so that hub consumers
// (TALM/IBGU) can rely on the same value rather than hard-coding it.
const DefaultNamespace = "openshift-lifecycle-agent"

// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=imagebasedupgrades,scope=Cluster,shortName=ibu
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="Desired Stage",type="string",JSONPath=".spec.stage"
// +kubebuilder:printcolumn:name="State",type="string",JSONPath=".status.conditions[-1:].reason"
// +kubebuilder:printcolumn:name="Details",type="string",JSONPath=".status.conditions[-1:].message"
// +kubebuilder:validation:XValidation:message="ibu is a singleton, metadata.name must be 'upgrade'", rule="self.metadata.name == 'upgrade'"
// +kubebuilder:validation:XValidation:message="can not change spec.seedImageRef while ibu is in progress", rule="!has(oldSelf.status) || oldSelf.status.conditions.exists(c, c.type=='Idle' && c.status=='True') || self.spec.stage=='Idle' || has(oldSelf.spec.seedImageRef) && has(self.spec.seedImageRef) && oldSelf.spec.seedImageRef==self.spec.seedImageRef || !has(self.spec.seedImageRef) && !has(oldSelf.spec.seedImageRef)"
// +kubebuilder:validation:XValidation:message="can not change spec.oadpContent while ibu is in progress", rule="!has(oldSelf.status) || oldSelf.status.conditions.exists(c, c.type=='Idle' && c.status=='True') || self.spec.stage=='Idle' || has(oldSelf.spec.oadpContent) && has(self.spec.oadpContent) && oldSelf.spec.oadpContent==self.spec.oadpContent || !has(self.spec.oadpContent) && !has(oldSelf.spec.oadpContent)"
// +kubebuilder:validation:XValidation:message="can not change spec.backupContent while ibu is in progress", rule="!has(oldSelf.status) || oldSelf.status.conditions.exists(c, c.type=='Idle' && c.status=='True') || self.spec.stage=='Idle' || has(oldSelf.spec.backupContent) && has(self.spec.backupContent) && oldSelf.spec.backupContent==self.spec.backupContent || !has(self.spec.backupContent) && !has(oldSelf.spec.backupContent)"
// +kubebuilder:validation:XValidation:message="can not change spec.extraManifests while ibu is in progress", rule="!has(oldSelf.status) || oldSelf.status.conditions.exists(c, c.type=='Idle' && c.status=='True') || self.spec.stage=='Idle' || has(oldSelf.spec.extraManifests) && has(self.spec.extraManifests) && oldSelf.spec.extraManifests==self.spec.extraManifests || !has(self.spec.extraManifests) && !has(oldSelf.spec.extraManifests)"
// +kubebuilder:validation:XValidation:message="can not change spec.autoRollbackOnFailure while ibu is in progress", rule="!has(oldSelf.status) || oldSelf.status.conditions.exists(c, c.type=='Idle' && c.status=='True') || self.spec.stage=='Idle' || has(oldSelf.spec.autoRollbackOnFailure) && has(self.spec.autoRollbackOnFailure) && oldSelf.spec.autoRollbackOnFailure==self.spec.autoRollbackOnFailure || !has(self.spec.autoRollbackOnFailure) && !has(oldSelf.spec.autoRollbackOnFailure)"
// +kubebuilder:validation:XValidation:message="the stage transition is not permitted. Please refer to status.validNextStages for valid transitions. If status.validNextStages is not present, it indicates that no transitions are currently allowed", rule="!has(oldSelf.status) || has(oldSelf.status.validNextStages) && self.spec.stage in oldSelf.status.validNextStages || has(oldSelf.spec.stage) && has(self.spec.stage) && oldSelf.spec.stage==self.spec.stage"
// +operator-sdk:csv:customresourcedefinitions:displayName="Image-based Cluster Upgrade",resources={{Namespace, v1},{Deployment,apps/v1}}

// ImageBasedUpgrade is the Schema for the ImageBasedUpgrades API
type ImageBasedUpgrade struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ImageBasedUpgradeSpec   `json:"spec,omitempty"`
	Status ImageBasedUpgradeStatus `json:"status,omitempty"`
}

// ImageBasedUpgradeStage defines the type for the IBU stage field
type ImageBasedUpgradeStage string

// idleValue is the shared string literal used by both the Idle stage and the
// Idle condition type. They intentionally share the same value, so a single
// private constant removes the duplication (and the goconst suppression) while
// making that intended equality explicit.
const idleValue = "Idle"

// Stages defines the string values for valid stages.
//
// Although declared as a var for ergonomic field access, its fields form a
// stable public contract and must be treated as immutable: do not reassign
// Stages or any of its fields at runtime.
var Stages = struct {
	Idle     ImageBasedUpgradeStage
	Prep     ImageBasedUpgradeStage
	Upgrade  ImageBasedUpgradeStage
	Rollback ImageBasedUpgradeStage
}{
	Idle:     idleValue,
	Prep:     "Prep",
	Upgrade:  "Upgrade",
	Rollback: "Rollback",
}

// ConditionType is a string representing the condition's type
type ConditionType string

// ConditionTypes define the different types of conditions that will be set.
//
// Although declared as a var for ergonomic field access, its fields form a
// stable consumer contract (downstream consumers such as TALM string-match
// these values) and must be treated as immutable: do not reassign
// ConditionTypes or any of its fields at runtime.
var ConditionTypes = struct {
	Idle               ConditionType
	PrepInProgress     ConditionType
	PrepCompleted      ConditionType
	UpgradeInProgress  ConditionType
	UpgradeCompleted   ConditionType
	RollbackInProgress ConditionType
	RollbackCompleted  ConditionType
	SeedGenInProgress  ConditionType
	SeedGenCompleted   ConditionType
	ConfigInProgress   ConditionType
	ConfigCompleted    ConditionType
}{
	Idle:               idleValue,
	PrepInProgress:     "PrepInProgress",
	PrepCompleted:      "PrepCompleted",
	UpgradeInProgress:  "UpgradeInProgress",
	UpgradeCompleted:   "UpgradeCompleted",
	RollbackInProgress: "RollbackInProgress",
	RollbackCompleted:  "RollbackCompleted",
	SeedGenInProgress:  "SeedGenInProgress",
	SeedGenCompleted:   "SeedGenCompleted",
	ConfigInProgress:   "ConfigInProgress",
	ConfigCompleted:    "ConfigCompleted",
}

// Developer note (not part of the user-facing API description; kept in a separate
// comment block so controller-gen does not emit it into the CRD/CSV): the
// mutual-exclusion rule below is placed on this spec type rather than on the
// ImageBasedUpgrade object so that the IBGU CRD, which embeds this struct, inherits
// it on regeneration and the hub rejects a bad IBGU at creation rather than at
// spoke Prep.

// ImageBasedUpgradeSpec defines the desired state of ImageBasedUpgrade
// +kubebuilder:validation:XValidation:message="oadpContent and backupContent are mutually exclusive",rule="!(has(self.oadpContent) && size(self.oadpContent) > 0 && has(self.backupContent) && size(self.backupContent) > 0)"
type ImageBasedUpgradeSpec struct {
	//+kubebuilder:validation:Enum=Idle;Prep;Upgrade;Rollback
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Stage"
	Stage ImageBasedUpgradeStage `json:"stage,omitempty"`
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Seed Image Reference"
	SeedImageRef SeedImageRef `json:"seedImageRef,omitempty"`
	// Deprecated: OADPContent defines the list of ConfigMap resources that contain the OADP Backup and Restore CRs.
	// Use BackupContent instead. OADPContent remains honoured for backward compatibility; the release that removes it
	// is TBD. OADPContent and BackupContent are mutually exclusive on a single IBU.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="OADP Content (Deprecated)"
	OADPContent []ConfigMapRef `json:"oadpContent,omitempty"`
	// BackupContent defines the list of name-only references to BackupContent CRs that declare the backup/restore
	// content for the upgrade. The referenced BackupContent CRs are read from LCA's install namespace; the references
	// carry no namespace.
	// BackupContent is the replacement for the deprecated OADPContent; the two are mutually exclusive on a single IBU.
	// Entries are keyed by name: duplicate references are rejected at admission.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Backup Content"
	// +listType=map
	// +listMapKey=name
	BackupContent []BackupContentRef `json:"backupContent,omitempty"`
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Extra Manifests"
	// ExtraManifests defines the list of ConfigMap resources that contain the user-specific extra manifests to be
	// applied during the upgrade post-pivot stage.
	// Users can also add their custom catalog sources that may want to retain after the upgrade.
	ExtraManifests []ConfigMapRef `json:"extraManifests,omitempty"`
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Auto Rollback On Failure"
	AutoRollbackOnFailure *AutoRollbackOnFailure `json:"autoRollbackOnFailure,omitempty"`
}

// SeedImageRef defines the seed image and OCP version for the upgrade
type SeedImageRef struct {
	// Version defines the target platform version. The value must match the version of the seed image.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	Version string `json:"version,omitempty"`
	// Image defines the full pull-spec of the seed container image to use.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Pattern="^([a-z0-9]+://)?[\\S]+$"
	Image string `json:"image,omitempty"`
	// PullSecretRef defines the name-only reference to a secret with credentials to pull container images. The
	// referenced secret is read from LCA's install namespace; the reference carries no namespace.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Pull Secret Reference"
	PullSecretRef *PullSecretRef `json:"pullSecretRef,omitempty"`
}

// AutoRollbackOnFailure defines automatic rollback settings if the upgrade fails or if the upgrade does not
// complete within the specified time limit.
type AutoRollbackOnFailure struct {
	// InitMonitorTimeoutSeconds defines the time frame in seconds. If not defined or set to 0, the default value of
	// 1800 seconds (30 minutes) is used.
	// +kubebuilder:validation:Minimum=0
	//+operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors={"urn:alm:descriptor:com.tectonic.ui:number"}
	InitMonitorTimeoutSeconds int `json:"initMonitorTimeoutSeconds,omitempty"` // LCA Init Monitor watchdog timeout, in seconds. Value = 0 is treated as "use default" when writing config file in Prep stage
}

// ConfigMapRef defines a reference to a config map
type ConfigMapRef struct {
	// +kubebuilder:validation:Required
	// +required
	//+operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	Name string `json:"name"`

	// +kubebuilder:validation:Required
	// +required
	//+operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	Namespace string `json:"namespace"`
}

// PullSecretRef defines a reference to a secret with credentials for pulling container images
type PullSecretRef struct {
	// +kubebuilder:validation:Required
	// +required
	//+operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	Name string `json:"name"`
}

// BackupContentRef defines a name-only reference to a BackupContent CR. The
// referenced CR is resolved in LCA's install namespace, so the reference carries
// no namespace (unlike ConfigMapRef).
type BackupContentRef struct {
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +required
	//+operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	Name string `json:"name"`
}

// ImageBasedUpgradeStatus defines the observed state of ImageBasedUpgrade
type ImageBasedUpgradeStatus struct {
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// +operator-sdk:csv:customresourcedefinitions:type=status,displayName="Conditions",xDescriptors={"urn:alm:descriptor:io.kubernetes.conditions"}
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// +operator-sdk:csv:customresourcedefinitions:type=status,displayName="Valid Next Stage"
	ValidNextStages []ImageBasedUpgradeStage `json:"validNextStages,omitempty"`
	// RollbackAvailabilityExpiration reflects the point at which rolling back may require manual recovery from expired control plane certificates.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	RollbackAvailabilityExpiration metav1.Time `json:"rollbackAvailabilityExpiration,omitempty"`
	// History stores timing info of different IBU stages and their important phases
	// +optional
	History []*History `json:"history,omitempty"`
}

type History struct {
	// Stage The desired stage name read from spec
	Stage ImageBasedUpgradeStage `json:"stage,omitempty"`
	// Phase Allows for a more granular view of the important tasks that are performed when moving a desired Stage
	Phases []*Phase `json:"phases,omitempty"`
	// StartTime A timestamp to indicate the Stage has started
	StartTime metav1.Time `json:"startTime,omitempty"`
	// CompletionTime A timestamp indicating the Stage completed.
	// This is only available when a Stage completes successfully
	CompletionTime metav1.Time `json:"completionTime,omitempty"`
}

type Phase struct {
	// Phase current phase within a Stage
	Phase string `json:"phase,omitempty"`
	// StartTime A timestamp indicating the Phase has started
	StartTime metav1.Time `json:"startTime,omitempty"`
	// CompletionTime A timestamp indicating the phase completed.
	// This is only available when a Phase completes successfully
	CompletionTime metav1.Time `json:"completionTime,omitempty"`
}

// +kubebuilder:object:root=true

// ImageBasedUpgradeList contains a list of ImageBasedUpgrade
type ImageBasedUpgradeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ImageBasedUpgrade `json:"items"`
}
