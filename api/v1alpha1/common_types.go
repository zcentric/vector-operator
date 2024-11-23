/*
Copyright 2024.

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
	"k8s.io/apimachinery/pkg/runtime"
)

// Source defines a Vector source configuration
// +kubebuilder:pruning:PreserveUnknownFields
type Source struct {
	// Type specifies the type of the source
	// +kubebuilder:validation:Required
	Type string `json:"type"`

	// ExtraLabelSelector specifies additional label selectors for kubernetes_logs source
	// +optional
	ExtraLabelSelector string `json:"extra_label_selector,omitempty"`

	// Config contains the source-specific configuration
	// +optional
	// +kubebuilder:pruning:PreserveUnknownFields
	Config runtime.RawExtension `json:"config,omitempty"`
}

// TransformCondition defines the condition for a filter transform
// +kubebuilder:pruning:PreserveUnknownFields
type TransformCondition struct {
	// Type specifies the condition type
	// +kubebuilder:validation:Required
	Type string `json:"type"`

	// Source contains the condition expression
	// +kubebuilder:validation:Required
	Source string `json:"source"`
}

// Transform defines a Vector transform configuration
// +kubebuilder:pruning:PreserveUnknownFields
type Transform struct {
	// Type specifies the type of the transform
	// +kubebuilder:validation:Required
	Type string `json:"type"`

	// Inputs specifies the input sources or transforms
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	Inputs []string `json:"inputs"`

	// Source contains the transform expression (for remap transform)
	// +optional
	Source string `json:"source,omitempty"`

	// Condition specifies the filter condition (for filter transform)
	// +optional
	Condition *TransformCondition `json:"condition,omitempty"`

	// Config contains the transform-specific configuration
	// +optional
	// +kubebuilder:pruning:PreserveUnknownFields
	Config runtime.RawExtension `json:"config,omitempty"`
}

// SinkEncoding defines the encoding configuration for a sink
// +kubebuilder:pruning:PreserveUnknownFields
type SinkEncoding struct {
	// Codec specifies the encoding codec
	// +kubebuilder:validation:Required
	Codec string `json:"codec"`
}

// Sink defines a Vector sink configuration
// +kubebuilder:pruning:PreserveUnknownFields
type Sink struct {
	// Type specifies the type of the sink
	// +kubebuilder:validation:Required
	Type string `json:"type"`

	// Encoding specifies the sink encoding configuration
	// +optional
	Encoding *SinkEncoding `json:"encoding,omitempty"`

	// Inputs specifies the input sources or transforms
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	Inputs []string `json:"inputs"`

	// Config contains the sink-specific configuration
	// +optional
	// +kubebuilder:pruning:PreserveUnknownFields
	Config runtime.RawExtension `json:"config,omitempty"`
}
