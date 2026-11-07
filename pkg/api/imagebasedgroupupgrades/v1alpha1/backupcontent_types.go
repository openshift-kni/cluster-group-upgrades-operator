package v1alpha1

import (
	lcabcv1 "github.com/openshift-kni/lifecycle-agent/api/backupcontent/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=backupcontents,scope=Namespaced,shortName=bkc
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// BackupContent is a hub-side, curated backup definition for an upgrade path. It
// outlives any single IBGU; its spec is immutable, so a change is a new entry and
// a new IBGU. The object name is reused verbatim as the spoke BackupContent
// (lca.openshift.io) name, so it must be a valid LCA object name.
//
// Spec is LCA's exported BackupContentSpec type directly, with no wrapper field,
// so a hub entry and its spoke copy differ only in apiVersion and namespace.
// +operator-sdk:csv:customresourcedefinitions:displayName="Backup Content"
type BackupContent struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +kubebuilder:validation:Required
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="spec is immutable"
	Spec lcabcv1.BackupContentSpec `json:"spec"`
}

//+kubebuilder:object:root=true

// BackupContentList contains a list of BackupContent
type BackupContentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []BackupContent `json:"items"`
}
