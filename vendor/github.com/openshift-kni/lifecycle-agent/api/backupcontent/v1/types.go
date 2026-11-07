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

// Developer note (not part of the user-facing API description; kept in a separate
// comment block so controller-gen does not emit it into the CRD/CSV): the
// backup/restore engine that honours this input is implemented separately
// (CNF-29691). BackupContent deliberately has no +kubebuilder:subresource:status
// marker; the Status field below is an empty placeholder that exists only because
// operator-sdk's CSV generator requires every owned CRD type to declare a status,
// and nothing ever reports into it.

//+kubebuilder:object:root=true
//+kubebuilder:resource:path=backupcontents,scope=Namespaced,shortName=bkc
//+kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// BackupContent is the Schema for the backupcontents API. It is LCA's declarative
// backup/restore input, replacing the Velero Backup and Restore CRs that were
// previously embedded in the ConfigMaps referenced by IBU spec.oadpContent.
//
// BackupContent CRs are inert data: nothing reconciles them, they carry no status
// subresource, and they are applied directly to the cluster (not embedded inside
// ConfigMaps). An IBU references them by name through spec.backupContent; the
// reference carries no namespace and is resolved in LCA's install namespace.
type BackupContent struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   BackupContentSpec   `json:"spec,omitempty"`
	Status BackupContentStatus `json:"status,omitempty"`
}

// Developer note (not part of the user-facing API description; kept in a separate
// comment block so controller-gen does not emit it into the CRD/CSV): this type
// exists solely to satisfy operator-sdk's CSV generator, which requires every owned
// CRD type to declare a status. Do not add fields or a
// +kubebuilder:subresource:status marker.

// BackupContentStatus is intentionally empty. BackupContent is inert, pure
// declarative input with no status subresource; backup and restore outcomes are
// reported through IBU status and LCA logs only.
type BackupContentStatus struct {
}

// BackupContentSpec defines the declarative backup/restore content for a single
// BackupContent object. It is exported as a named type so that the hub (TALM/IBGU)
// can embed the same struct rather than duplicating or wrapping it. A single
// BackupContent collapses what Velero split across separate Backup and Restore CRs.
type BackupContentSpec struct {
	// BackupWave is the order in which this BackupContent is processed during
	// backup. It is optional and independent of RestoreWave. When omitted, this
	// BackupContent is processed in the last backup wave, after all explicitly
	// numbered waves.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=100
	BackupWave *int32 `json:"backupWave,omitempty"`

	// RestoreWave is the order in which this BackupContent is processed during
	// restore. It is optional and independent of BackupWave. When omitted, this
	// BackupContent is processed in the last restore wave, after all explicitly
	// numbered waves.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=100
	RestoreWave *int32 `json:"restoreWave,omitempty"`

	// Namespaces is the list of namespaces in scope for the namespaced-resource
	// entries of this BackupContent.
	Namespaces []string `json:"namespaces,omitempty"`

	// NamespacedResources lists the namespaced resources to back up and restore.
	// Every entry must specify a kind. An entry without names selects all
	// resources of that kind in scope (optionally filtered by LabelSelector).
	NamespacedResources []ResourceSelector `json:"namespacedResources,omitempty"`

	// ExcludedNamespacedResources lists namespaced resources to exclude from the
	// backup. Every entry must specify a kind. LabelSelector does not apply to this
	// list: exclusions stay kind- and name-based.
	ExcludedNamespacedResources []ResourceSelector `json:"excludedNamespacedResources,omitempty"`

	// ClusterScopedResources lists the cluster-scoped resources to back up and
	// restore. Every entry must specify a kind. An entry without names selects all
	// resources of that kind (optionally filtered by LabelSelector).
	ClusterScopedResources []ResourceSelector `json:"clusterScopedResources,omitempty"`

	// ExcludedClusterScopedResources lists cluster-scoped resources to exclude from
	// the backup. Every entry must specify a kind. LabelSelector does not apply to
	// this list: exclusions stay kind- and name-based.
	ExcludedClusterScopedResources []ResourceSelector `json:"excludedClusterScopedResources,omitempty"`

	// LabelSelector is a single selector applied only to the entries in
	// NamespacedResources and ClusterScopedResources that do not specify names:
	// for those entries, only resources of that kind matching the selector are
	// backed up. Entries that do specify names are taken as given and the selector
	// does not apply to them. The selector does not apply to the excluded lists.
	LabelSelector *metav1.LabelSelector `json:"labelSelector,omitempty"`
}

// ResourceSelector identifies a group of resources by kind, with an optional list
// of names. It is used by both the included and excluded resource lists.
type ResourceSelector struct {
	// Kind is the resource kind, optionally qualified by its API group
	// (for example "deployments.apps" or "routes.route.openshift.io"). It is
	// required on every entry.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +required
	Kind string `json:"kind"`

	// Names optionally restricts the entry to specific object names. If absent or
	// empty, all resources of that kind in scope are selected (subject to the
	// top-level LabelSelector for the included lists). A single BackupContent may
	// mix named and unnamed entries.
	Names []string `json:"names,omitempty"`
}

//+kubebuilder:object:root=true

// BackupContentList contains a list of BackupContent
type BackupContentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []BackupContent `json:"items"`
}
