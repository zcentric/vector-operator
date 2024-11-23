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
)

// VectorAPI defines the API configuration for Vector
type VectorAPI struct {
	// Address is the address to bind the API server to
	// +optional
	// +kubebuilder:default="0.0.0.0:8686"
	Address string `json:"address,omitempty"`

	// Enabled determines if the API server should be enabled
	// +optional
	// +kubebuilder:default=false
	Enabled *bool `json:"enabled,omitempty"`

	// Playground determines if the GraphQL playground should be enabled
	// +optional
	// +kubebuilder:default=false
	Playground *bool `json:"playground,omitempty"`
}

// VectorSpec defines the desired state of Vector
type VectorSpec struct {
	// Type specifies the type of Vector deployment (e.g., "agent")
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=agent
	Type string `json:"type"`

	// Image specifies the Vector container image to use
	// +kubebuilder:validation:Required
	Image string `json:"image"`

	// API configuration for Vector
	// +optional
	API *VectorAPI `json:"api,omitempty"`

	// DataDir specifies the data directory for Vector
	// +optional
	// +kubebuilder:default="/tmp/vector-data-dir"
	DataDir string `json:"data_dir,omitempty"`

	// ExpireMetricsSecs specifies how long to keep metrics before expiring them
	// +optional
	// +kubebuilder:default=30
	ExpireMetricsSecs *int32 `json:"expire_metrics_secs,omitempty"`

	// Sources defines the Vector sources configuration
	// +optional
	// +kubebuilder:pruning:PreserveUnknownFields
	Sources map[string]Source `json:"sources,omitempty"`

	// Transforms defines the Vector transforms configuration
	// +optional
	// +kubebuilder:pruning:PreserveUnknownFields
	Transforms map[string]Transform `json:"transforms,omitempty"`

	// Sinks defines the Vector sinks configuration
	// +optional
	// +kubebuilder:pruning:PreserveUnknownFields
	Sinks map[string]Sink `json:"sinks,omitempty"`
}

// VectorStatus defines the observed state of Vector
type VectorStatus struct {
	// Conditions represent the latest available observations of Vector's state
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Namespaced
//+kubebuilder:printcolumn:name="Type",type="string",JSONPath=".spec.type"
//+kubebuilder:printcolumn:name="Image",type="string",JSONPath=".spec.image"
//+kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// Vector is the Schema for the vectors API
type Vector struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VectorSpec   `json:"spec,omitempty"`
	Status VectorStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// VectorList contains a list of Vector
type VectorList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Vector `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Vector{}, &VectorList{})
}
