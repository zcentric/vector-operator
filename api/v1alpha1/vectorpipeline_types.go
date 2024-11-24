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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
)

// VectorPipelineSpec defines the desired state of VectorPipeline
type VectorPipelineSpec struct {
	// VectorRef references the Vector CR to use
	// +kubebuilder:validation:Required
	VectorRef string `json:"vectorRef"`

	// Sources defines the Vector sources configuration
	// +optional
	// +kubebuilder:validation:Type=object
	// +kubebuilder:pruning:PreserveUnknownFields
	Sources runtime.RawExtension `json:"sources,omitempty"`

	// Transforms defines the Vector transforms configuration
	// +optional
	// +kubebuilder:validation:Type=object
	// +kubebuilder:pruning:PreserveUnknownFields
	Transforms runtime.RawExtension `json:"transforms,omitempty"`

	// Sinks defines the Vector sinks configuration
	// +optional
	// +kubebuilder:validation:Type=object
	// +kubebuilder:pruning:PreserveUnknownFields
	Sinks runtime.RawExtension `json:"sinks,omitempty"`
}

// VectorPipelineStatus defines the observed state of VectorPipeline
type VectorPipelineStatus struct {
	// Conditions represent the latest available observations of VectorPipeline's state
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Namespaced
//+kubebuilder:printcolumn:name="VectorRef",type="string",JSONPath=".spec.vectorRef"
//+kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.conditions[?(@.type==\"VectorRefValid\")].status"
//+kubebuilder:printcolumn:name="Message",type="string",JSONPath=".status.conditions[?(@.type==\"VectorRefValid\")].message"
//+kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// VectorPipeline is the Schema for the vectorpipelines API
type VectorPipeline struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VectorPipelineSpec   `json:"spec,omitempty"`
	Status VectorPipelineStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// VectorPipelineList contains a list of VectorPipeline
type VectorPipelineList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VectorPipeline `json:"items"`
}

func init() {
	SchemeBuilder.Register(&VectorPipeline{}, &VectorPipelineList{})
}
