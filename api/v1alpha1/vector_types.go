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
	corev1 "k8s.io/api/core/v1"
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

// ServiceAccountSpec defines the configuration for the Vector ServiceAccount
type ServiceAccountSpec struct {
	// Annotations to be added to the ServiceAccount
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
}

// VectorSpec defines the desired state of Vector
type VectorSpec struct {
	// Image specifies the Vector container image to use
	// +kubebuilder:validation:Required
	Image string `json:"image"`

	// ImagePullSecrets is a list of references to secrets in the same namespace to use for pulling the Vector image
	// +optional
	ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`

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

	// ServiceAccount configuration for Vector
	// +optional
	ServiceAccount *ServiceAccountSpec `json:"serviceAccount,omitempty"`

	// Tolerations defines the pod's tolerations
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// Env defines the environment variables to be added to the Vector container
	// +optional
	Env []corev1.EnvVar `json:"env,omitempty"`

	// Resources defines the resource requirements for the Vector container
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// Volumes defines additional volumes to be added to the Vector pod
	// +optional
	Volumes []corev1.Volume `json:"volumes,omitempty"`

	// VolumeMounts defines additional volume mounts to be added to the Vector container
	// +optional
	VolumeMounts []corev1.VolumeMount `json:"volumeMounts,omitempty"`
}

// VectorStatus defines the observed state of Vector
type VectorStatus struct {
	// Conditions represent the latest available observations of Vector's state
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ConfigHash represents the current hash of the Vector configuration
	// +optional
	ConfigHash string `json:"configHash,omitempty"`

	// ValidatedPipelines tracks which pipelines have been validated and their generation
	// +optional
	ValidatedPipelines map[string]int64 `json:"validatedPipelines,omitempty"`

	// PipelineValidationStatus shows the validation status of each pipeline
	// +optional
	PipelineValidationStatus map[string]PipelineValidation `json:"pipelineValidationStatus,omitempty"`
}

// PipelineValidation represents the validation status of a pipeline
// +k8s:deepcopy-gen=true
type PipelineValidation struct {
	// Status indicates if the pipeline is validated
	// +kubebuilder:validation:Enum=Validated;Failed
	Status string `json:"status"`

	// Message provides additional details about the validation
	// +optional
	Message string `json:"message,omitempty"`

	// LastValidated is the timestamp of the last validation
	// +optional
	LastValidated metav1.Time `json:"lastValidated,omitempty"`

	// Generation is the generation of the pipeline that was validated
	Generation int64 `json:"generation"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Namespaced
//+kubebuilder:printcolumn:name="Image",type="string",JSONPath=".spec.image"
//+kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
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
