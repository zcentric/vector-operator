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

// VectorAggregatorSpec defines the desired state of VectorAggregator
type VectorAggregatorSpec struct {
	// Image specifies the Vector container image to use
	// +kubebuilder:validation:Required
	Image string `json:"image"`

	// ImagePullSecrets is a list of references to secrets in the same namespace to use for pulling the Vector image
	// +optional
	ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`

	// Replicas is the number of Vector pods to run
	// +optional
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=0
	Replicas int32 `json:"replicas,omitempty"`

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

	// TopologySpreadConstraints describes how a group of pods ought to spread across topology domains
	// +optional
	TopologySpreadConstraints []corev1.TopologySpreadConstraint `json:"topologySpreadConstraints,omitempty"`

	// Volumes defines additional volumes to be added to the Vector pod
	// +optional
	Volumes []corev1.Volume `json:"volumes,omitempty"`

	// VolumeMounts defines additional volume mounts to be added to the Vector container
	// +optional
	VolumeMounts []corev1.VolumeMount `json:"volumeMounts,omitempty"`
}

// VectorAggregatorStatus defines the observed state of VectorAggregator
type VectorAggregatorStatus struct {
	// Conditions represent the latest available observations of VectorAggregator's state
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

	// ValidationSummary provides a summary of pipeline validations
	// +optional
	ValidationSummary string `json:"validationSummary,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Namespaced
//+kubebuilder:printcolumn:name="Image",type="string",JSONPath=".spec.image"
//+kubebuilder:printcolumn:name="Replicas",type="integer",JSONPath=".spec.replicas"
//+kubebuilder:printcolumn:name="Validation Status",type="string",JSONPath=".status.validationSummary"
//+kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// VectorAggregator is the Schema for the vectoraggregators API
type VectorAggregator struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VectorAggregatorSpec   `json:"spec,omitempty"`
	Status VectorAggregatorStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// VectorAggregatorList contains a list of VectorAggregator
type VectorAggregatorList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VectorAggregator `json:"items"`
}

func init() {
	SchemeBuilder.Register(&VectorAggregator{}, &VectorAggregatorList{})
}
