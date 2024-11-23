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

package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/yaml"

	vectorv1alpha1 "github.com/zcentric/vector-operator/api/v1alpha1"
)

const (
	vectorFinalizer = "vector.zcentric.com/finalizer"
)

// VectorReconciler reconciles a Vector object
type VectorReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=vector.zcentric.com,resources=vectors,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=vector.zcentric.com,resources=vectors/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=vector.zcentric.com,resources=vectors/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch

func (r *VectorReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the Vector instance
	vector := &vectorv1alpha1.Vector{}
	err := r.Get(ctx, req.NamespacedName, vector)
	if err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Add finalizer if it doesn't exist
	if !controllerutil.ContainsFinalizer(vector, vectorFinalizer) {
		controllerutil.AddFinalizer(vector, vectorFinalizer)
		if err := r.Update(ctx, vector); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Handle deletion
	if !vector.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(vector, vectorFinalizer) {
			// Clean up resources
			if err := r.cleanupResources(ctx, vector); err != nil {
				return ctrl.Result{}, err
			}

			// Remove finalizer
			controllerutil.RemoveFinalizer(vector, vectorFinalizer)
			if err := r.Update(ctx, vector); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	// Handle agent type with DaemonSet
	if vector.Spec.Agent.Type == "agent" {
		// Create or update the ConfigMap
		if err := r.reconcileConfigMap(ctx, vector); err != nil {
			logger.Error(err, "Failed to reconcile ConfigMap")
			return ctrl.Result{}, err
		}

		// Check if the daemonset already exists, if not create a new one
		daemonset := &appsv1.DaemonSet{}
		err = r.Get(ctx, types.NamespacedName{Name: vector.Name, Namespace: vector.Namespace}, daemonset)
		if err != nil && errors.IsNotFound(err) {
			// Define a new daemonset
			ds := r.daemonSetForVector(vector)
			logger.Info("Creating a new DaemonSet", "DaemonSet.Namespace", ds.Namespace, "DaemonSet.Name", ds.Name)
			err = r.Create(ctx, ds)
			if err != nil {
				logger.Error(err, "Failed to create new DaemonSet", "DaemonSet.Namespace", ds.Namespace, "DaemonSet.Name", ds.Name)
				if r.Recorder != nil {
					r.Recorder.Event(vector, corev1.EventTypeWarning, "Failed", "Failed to create Vector daemonset")
				}
				return ctrl.Result{}, err
			}
			if r.Recorder != nil {
				r.Recorder.Event(vector, corev1.EventTypeNormal, "Created", "Created Vector daemonset")
			}
			return ctrl.Result{Requeue: true}, nil
		} else if err != nil {
			logger.Error(err, "Failed to get DaemonSet")
			return ctrl.Result{}, err
		}

		// Update daemonset if needed
		if daemonSetNeedsUpdate(vector, daemonset) {
			daemonset.Spec.Template.Spec.Containers[0].Image = vector.Spec.Agent.Image
			err = r.Update(ctx, daemonset)
			if err != nil {
				logger.Error(err, "Failed to update DaemonSet", "DaemonSet.Namespace", daemonset.Namespace, "DaemonSet.Name", daemonset.Name)
				return ctrl.Result{}, err
			}
			if r.Recorder != nil {
				r.Recorder.Event(vector, corev1.EventTypeNormal, "Updated", "Updated Vector daemonset")
			}
		}

		// Update the Vector status
		if err := r.updateVectorStatus(ctx, vector, daemonset); err != nil {
			logger.Error(err, "Failed to update Vector status")
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{RequeueAfter: time.Minute}, nil
}

// cleanupResources removes all resources owned by the Vector CR
func (r *VectorReconciler) cleanupResources(ctx context.Context, v *vectorv1alpha1.Vector) error {
	logger := log.FromContext(ctx)

	// Delete DaemonSet if it exists
	ds := &appsv1.DaemonSet{}
	err := r.Get(ctx, types.NamespacedName{Name: v.Name, Namespace: v.Namespace}, ds)
	if err == nil {
		logger.Info("Deleting DaemonSet", "name", ds.Name)
		if err := r.Delete(ctx, ds); err != nil && !errors.IsNotFound(err) {
			return err
		}
	}

	// Delete ConfigMap if it exists
	cm := &corev1.ConfigMap{}
	err = r.Get(ctx, types.NamespacedName{Name: v.Name + "-config", Namespace: v.Namespace}, cm)
	if err == nil {
		logger.Info("Deleting ConfigMap", "name", cm.Name)
		if err := r.Delete(ctx, cm); err != nil && !errors.IsNotFound(err) {
			return err
		}
	}

	return nil
}

// reconcileConfigMap creates or updates the Vector ConfigMap
func (r *VectorReconciler) reconcileConfigMap(ctx context.Context, v *vectorv1alpha1.Vector) error {
	logger := log.FromContext(ctx)

	// Build the configuration sections
	var configParts []string

	// Add global options
	configParts = append(configParts, "# Set global options", fmt.Sprintf("data_dir: \"%s\"", "/var/lib/vector"))

	// Add API configuration
	configParts = append(configParts, "\n# Vector's API (disabled by default)", "# Enable and try it out with the 'vector top' command")
	if v.Spec.Agent.API != nil {
		apiConfig := fmt.Sprintf("api:\n  enabled: %v", *v.Spec.Agent.API.Enabled)
		if v.Spec.Agent.API.Address != "" {
			apiConfig += fmt.Sprintf("\n  address: \"%s\"", v.Spec.Agent.API.Address)
		}
		configParts = append(configParts, apiConfig)
	} else {
		configParts = append(configParts, "api:\n  enabled: false")
	}

	// Add sources if any
	if len(v.Spec.Agent.Sources) > 0 {
		sourcesYaml, err := yaml.Marshal(v.Spec.Agent.Sources)
		if err != nil {
			return fmt.Errorf("failed to marshal sources: %w", err)
		}
		configParts = append(configParts, "\n# Sources configuration", fmt.Sprintf("sources:\n%s", indent(string(sourcesYaml), "  ")))
	}

	// Add transforms if any
	if len(v.Spec.Agent.Transforms) > 0 {
		transformsYaml, err := yaml.Marshal(v.Spec.Agent.Transforms)
		if err != nil {
			return fmt.Errorf("failed to marshal transforms: %w", err)
		}
		configParts = append(configParts, "\n# Transforms configuration", fmt.Sprintf("transforms:\n%s", indent(string(transformsYaml), "  ")))
	}

	// Add sinks if any
	if len(v.Spec.Agent.Sinks) > 0 {
		sinksYaml, err := yaml.Marshal(v.Spec.Agent.Sinks)
		if err != nil {
			return fmt.Errorf("failed to marshal sinks: %w", err)
		}
		configParts = append(configParts, "\n# Sinks configuration", fmt.Sprintf("sinks:\n%s", indent(string(sinksYaml), "  ")))
	}

	// Join all parts together
	configStr := strings.Join(configParts, "\n")

	// Create or update ConfigMap
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      v.Name + "-config",
			Namespace: v.Namespace,
		},
		Data: map[string]string{
			"vector.yaml": configStr,
		},
	}

	// Set Vector instance as the owner
	if err := ctrl.SetControllerReference(v, cm, r.Scheme); err != nil {
		return err
	}

	// Create or update the ConfigMap
	existingCM := &corev1.ConfigMap{}
	err := r.Get(ctx, types.NamespacedName{Name: cm.Name, Namespace: cm.Namespace}, existingCM)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Creating new ConfigMap",
				"name", cm.Name,
				"namespace", cm.Namespace)
			return r.Create(ctx, cm)
		}
		return err
	}

	logger.Info("Updating existing ConfigMap",
		"name", existingCM.Name,
		"namespace", existingCM.Namespace)

	existingCM.Data = cm.Data
	return r.Update(ctx, existingCM)
}

// indent adds the specified number of spaces to the start of each line
func indent(s string, indent string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = indent + line
		}
	}
	return strings.Join(lines, "\n")
}

// daemonSetForVector returns a vector DaemonSet object
func (r *VectorReconciler) daemonSetForVector(v *vectorv1alpha1.Vector) *appsv1.DaemonSet {
	ls := labelsForVector(v.Name)

	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      v.Name,
			Namespace: v.Namespace,
		},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: ls,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: ls,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Image: v.Spec.Agent.Image,
						Name:  "vector",
						Ports: []corev1.ContainerPort{{
							ContainerPort: 8686,
							Name:          "api",
						}},
						VolumeMounts: []corev1.VolumeMount{
							{
								Name:      "var-log",
								MountPath: "/var/log",
								ReadOnly:  true,
							},
							{
								Name:      "config",
								MountPath: "/etc/vector",
							},
						},
					}},
					Volumes: []corev1.Volume{
						{
							Name: "var-log",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/var/log",
								},
							},
						},
						{
							Name: "config",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: v.Name + "-config",
									},
								},
							},
						},
					},
				},
			},
		},
	}

	// Set Vector instance as the owner and controller
	if err := ctrl.SetControllerReference(v, ds, r.Scheme); err != nil {
		return nil
	}
	return ds
}

// labelsForVector returns the labels for selecting the resources
func labelsForVector(name string) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":       "Vector",
		"app.kubernetes.io/instance":   name,
		"app.kubernetes.io/managed-by": "vector-operator",
	}
}

// daemonSetNeedsUpdate returns true if the daemonset needs to be updated
func daemonSetNeedsUpdate(vector *vectorv1alpha1.Vector, daemonset *appsv1.DaemonSet) bool {
	if len(daemonset.Spec.Template.Spec.Containers) == 0 {
		return true
	}
	return daemonset.Spec.Template.Spec.Containers[0].Image != vector.Spec.Agent.Image
}

// updateVectorStatus updates the Status field of the Vector resource
func (r *VectorReconciler) updateVectorStatus(ctx context.Context, vector *vectorv1alpha1.Vector, daemonset *appsv1.DaemonSet) error {
	// Update the status condition based on the daemonset status
	condition := metav1.Condition{
		Type:               "Available",
		Status:             metav1.ConditionTrue,
		Reason:             "DaemonSetAvailable",
		Message:            "Vector daemonset is available",
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: vector.Generation,
	}

	if daemonset.Status.NumberReady == 0 {
		condition.Status = metav1.ConditionFalse
		condition.Reason = "DaemonSetUnavailable"
		condition.Message = "Vector daemonset is not available"
	}

	// Update the condition
	currentConditions := vector.Status.Conditions
	for i, existingCondition := range currentConditions {
		if existingCondition.Type == condition.Type {
			if existingCondition.Status != condition.Status {
				currentConditions[i] = condition
			}
			return nil
		}
	}
	vector.Status.Conditions = append(currentConditions, condition)

	return r.Status().Update(ctx, vector)
}

// SetupWithManager sets up the controller with the Manager.
func (r *VectorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&vectorv1alpha1.Vector{}).
		Owns(&appsv1.DaemonSet{}).
		Owns(&corev1.ConfigMap{}).
		Complete(r)
}
