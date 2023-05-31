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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

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

func init() {
	SchemeBuilder.Register(&PreCachingConfig{}, &PreCachingConfigList{})
}
