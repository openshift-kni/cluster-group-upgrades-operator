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

// PlatformUpgradeSpec defines the desired state of PlatformUpgrade
type PlatformUpgradeSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Group specifies the name of the Group object of the PlatformUpgrade.
	Group               string                  `json:"group,omitempty"`
	Clusters            []string                `json:"clusters,omitempty"`
	RemediationStrategy RemediationStrategySpec `json:"remediationStrategy,omitempty"`
	RemediationAction   string                  `json:"remediationAction,omitempty"`
	Channel             string                  `json:"channel,omitempty"`
	Version             string                  `json:"version,omitempty"`
	Upstream            string                  `json:"upstream,omitempty"`
}

// PlatformUpgradeStatus defines the observed state of PlatformUpgrade
type PlatformUpgradeStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	PlacementBindings []string       `json:"placementBindings"`
	PlacementRules    []string       `json:"placementRules"`
	Policies          []PolicyStatus `json:"policies"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// PlatformUpgrade is the Schema for the platformupgrades API
type PlatformUpgrade struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PlatformUpgradeSpec   `json:"spec,omitempty"`
	Status PlatformUpgradeStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// PlatformUpgradeList contains a list of PlatformUpgrade
type PlatformUpgradeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PlatformUpgrade `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PlatformUpgrade{}, &PlatformUpgradeList{})
}
