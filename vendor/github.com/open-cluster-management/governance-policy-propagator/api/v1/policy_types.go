// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package v1

import (
	appsv1 "github.com/open-cluster-management/multicloud-operators-placementrule/pkg/apis/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
)

// RemediationAction describes weather to enforce or inform
// +kubebuilder:validation:Enum=Inform;inform;Enforce;enforce
type RemediationAction string

const (
	// Enforce is an remediationAction to make changes
	Enforce RemediationAction = "Enforce"

	// Inform is an remediationAction to only inform
	Inform RemediationAction = "Inform"
)

// PolicyTemplate template for custom security policy
type PolicyTemplate struct {
	// +kubebuilder:pruning:PreserveUnknownFields
	ObjectDefinition runtime.RawExtension `json:"objectDefinition"`
}

// ComplianceState shows the state of enforcement
type ComplianceState string

const (
	// Compliant is an ComplianceState
	Compliant ComplianceState = "Compliant"

	// NonCompliant is an ComplianceState
	NonCompliant ComplianceState = "NonCompliant"
)

// PolicySpec defines the desired state of Policy
type PolicySpec struct {
	Disabled          bool              `json:"disabled"`
	RemediationAction RemediationAction `json:"remediationAction,omitempty"` // Enforce, Inform
	PolicyTemplates   []*PolicyTemplate `json:"policy-templates"`
}

// PlacementDecision defines the decision made by controller
type PlacementDecision struct {
	ClusterName      string `json:"clusterName,omitempty"`
	ClusterNamespace string `json:"clusterNamespace,omitempty"`
}

// Placement defines the placement results
type Placement struct {
	PlacementBinding string                     `json:"placementBinding,omitempty"`
	PlacementRule    string                     `json:"placementRule,omitempty"`
	Placement        string                     `json:"placement,omitempty"`
	Decisions        []appsv1.PlacementDecision `json:"decisions,omitempty"`
}

// CompliancePerClusterStatus defines compliance per cluster status
type CompliancePerClusterStatus struct {
	ComplianceState  ComplianceState `json:"compliant,omitempty"`
	ClusterName      string          `json:"clustername,omitempty"`
	ClusterNamespace string          `json:"clusternamespace,omitempty"`
}

// DetailsPerTemplate defines compliance details and history
type DetailsPerTemplate struct {
	// +kubebuilder:pruning:PreserveUnknownFields
	TemplateMeta    metav1.ObjectMeta   `json:"templateMeta,omitempty"`
	ComplianceState ComplianceState     `json:"compliant,omitempty"`
	History         []ComplianceHistory `json:"history,omitempty"`
}

// ComplianceHistory defines compliance details history
type ComplianceHistory struct {
	LastTimestamp metav1.Time `json:"lastTimestamp,omitempty" protobuf:"bytes,7,opt,name=lastTimestamp"`
	Message       string      `json:"message,omitempty" protobuf:"bytes,4,opt,name=message"`
	EventName     string      `json:"eventName,omitempty"`
}

// PolicyStatus defines the observed state of Policy
type PolicyStatus struct {
	Placement []*Placement                  `json:"placement,omitempty"` // used by root policy
	Status    []*CompliancePerClusterStatus `json:"status,omitempty"`    // used by root policy

	// +kubebuilder:validation:Enum=Compliant;NonCompliant
	ComplianceState ComplianceState       `json:"compliant,omitempty"` // used by replicated policy
	Details         []*DetailsPerTemplate `json:"details,omitempty"`   // used by replicated policy
}

//+kubebuilder:object:root=true

// Policy is the Schema for the policies API
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=policies,scope=Namespaced
// +kubebuilder:resource:path=policies,shortName=plc
// +kubebuilder:printcolumn:name="Remediation action",type="string",JSONPath=".spec.remediationAction"
// +kubebuilder:printcolumn:name="Compliance state",type="string",JSONPath=".status.compliant"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type Policy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec   PolicySpec   `json:"spec"`
	Status PolicyStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// PolicyList contains a list of Policy
type PolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Policy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Policy{}, &PolicyList{})
}
