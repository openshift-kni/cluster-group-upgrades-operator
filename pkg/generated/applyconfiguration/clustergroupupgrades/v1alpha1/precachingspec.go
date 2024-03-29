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
// Code generated by applyconfiguration-gen. DO NOT EDIT.

package v1alpha1

// PrecachingSpecApplyConfiguration represents an declarative configuration of the PrecachingSpec type for use
// with apply.
type PrecachingSpecApplyConfiguration struct {
	PlatformImage                *string  `json:"platformImage,omitempty"`
	OperatorsIndexes             []string `json:"operatorsIndexes,omitempty"`
	OperatorsPackagesAndChannels []string `json:"operatorsPackagesAndChannels,omitempty"`
	ExcludePrecachePatterns      []string `json:"excludePrecachePatterns,omitempty"`
	SpaceRequired                *string  `json:"spaceRequired,omitempty"`
	AdditionalImages             []string `json:"additionalImages,omitempty"`
}

// PrecachingSpecApplyConfiguration constructs an declarative configuration of the PrecachingSpec type for use with
// apply.
func PrecachingSpec() *PrecachingSpecApplyConfiguration {
	return &PrecachingSpecApplyConfiguration{}
}

// WithPlatformImage sets the PlatformImage field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the PlatformImage field is set to the value of the last call.
func (b *PrecachingSpecApplyConfiguration) WithPlatformImage(value string) *PrecachingSpecApplyConfiguration {
	b.PlatformImage = &value
	return b
}

// WithOperatorsIndexes adds the given value to the OperatorsIndexes field in the declarative configuration
// and returns the receiver, so that objects can be build by chaining "With" function invocations.
// If called multiple times, values provided by each call will be appended to the OperatorsIndexes field.
func (b *PrecachingSpecApplyConfiguration) WithOperatorsIndexes(values ...string) *PrecachingSpecApplyConfiguration {
	for i := range values {
		b.OperatorsIndexes = append(b.OperatorsIndexes, values[i])
	}
	return b
}

// WithOperatorsPackagesAndChannels adds the given value to the OperatorsPackagesAndChannels field in the declarative configuration
// and returns the receiver, so that objects can be build by chaining "With" function invocations.
// If called multiple times, values provided by each call will be appended to the OperatorsPackagesAndChannels field.
func (b *PrecachingSpecApplyConfiguration) WithOperatorsPackagesAndChannels(values ...string) *PrecachingSpecApplyConfiguration {
	for i := range values {
		b.OperatorsPackagesAndChannels = append(b.OperatorsPackagesAndChannels, values[i])
	}
	return b
}

// WithExcludePrecachePatterns adds the given value to the ExcludePrecachePatterns field in the declarative configuration
// and returns the receiver, so that objects can be build by chaining "With" function invocations.
// If called multiple times, values provided by each call will be appended to the ExcludePrecachePatterns field.
func (b *PrecachingSpecApplyConfiguration) WithExcludePrecachePatterns(values ...string) *PrecachingSpecApplyConfiguration {
	for i := range values {
		b.ExcludePrecachePatterns = append(b.ExcludePrecachePatterns, values[i])
	}
	return b
}

// WithSpaceRequired sets the SpaceRequired field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the SpaceRequired field is set to the value of the last call.
func (b *PrecachingSpecApplyConfiguration) WithSpaceRequired(value string) *PrecachingSpecApplyConfiguration {
	b.SpaceRequired = &value
	return b
}

// WithAdditionalImages adds the given value to the AdditionalImages field in the declarative configuration
// and returns the receiver, so that objects can be build by chaining "With" function invocations.
// If called multiple times, values provided by each call will be appended to the AdditionalImages field.
func (b *PrecachingSpecApplyConfiguration) WithAdditionalImages(values ...string) *PrecachingSpecApplyConfiguration {
	for i := range values {
		b.AdditionalImages = append(b.AdditionalImages, values[i])
	}
	return b
}
