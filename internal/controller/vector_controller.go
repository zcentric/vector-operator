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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
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
	vectorFinalizer      = "vector.zcentric.com/finalizer"
	configHashAnnotation = "vector.zcentric.com/config-hash"
)

// VectorReconciler reconciles a Vector object
type VectorReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// calculateConfigHash generates a hash of the ConfigMap data
func calculateConfigHash(configMap *corev1.ConfigMap) string {
	data, err := json.Marshal(configMap.Data)
	if err != nil {
		return ""
	}
	hasher := sha256.New()
	hasher.Write(data)
	return hex.EncodeToString(hasher.Sum(nil))
}

// +kubebuilder:rbac:groups=vector.zcentric.com,resources=vectors,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=vector.zcentric.com,resources=vectors/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=vector.zcentric.com,resources=vectors/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=serviceaccounts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles;clusterrolebindings,verbs=get;list;watch;create;update;patch;delete

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

	// Create or update common resources
	if err := r.reconcileServiceAccount(ctx, vector); err != nil {
		logger.Error(err, "Failed to reconcile ServiceAccount")
		return ctrl.Result{}, err
	}

	if err := r.reconcileRBAC(ctx, vector); err != nil {
		logger.Error(err, "Failed to reconcile RBAC")
		return ctrl.Result{}, err
	}

	// Reconcile ConfigMap first to get the hash
	configHash, err := r.reconcileConfigMap(ctx, vector)
	if err != nil {
		logger.Error(err, "Failed to reconcile ConfigMap")
		return ctrl.Result{}, err
	}

	// Store the config hash in the Vector status if it has changed
	if vector.Status.ConfigHash != configHash {
		vector.Status.ConfigHash = configHash
		if err := r.Status().Update(ctx, vector); err != nil {
			logger.Error(err, "Failed to update Vector status with config hash")
			return ctrl.Result{}, err
		}
		// Requeue to ensure the status update is processed
		return ctrl.Result{Requeue: true}, nil
	}

	// Reconcile the DaemonSet
	return r.reconcileAgent(ctx, vector)
}

// daemonSetForVector returns a vector DaemonSet object
func (r *VectorReconciler) daemonSetForVector(v *vectorv1alpha1.Vector) *appsv1.DaemonSet {
	ls := labelsForVector(v.Name)

	// Create annotations map with config hash
	annotations := make(map[string]string)
	if v.Status.ConfigHash != "" {
		annotations[configHashAnnotation] = v.Status.ConfigHash
	}

	// Create pod template annotations
	podAnnotations := make(map[string]string)
	for k, v := range annotations {
		podAnnotations[k] = v
	}

	// Combine default volumes with user-defined volumes
	volumes := []corev1.Volume{
		{
			Name: "data",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
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
	}
	volumes = append(volumes, v.Spec.Volumes...)

	// Combine default volume mounts with user-defined volume mounts
	volumeMounts := []corev1.VolumeMount{
		{
			Name:      "config",
			MountPath: "/etc/vector",
		},
		{
			Name:      "data",
			MountPath: v.Spec.DataDir,
		},
	}
	volumeMounts = append(volumeMounts, v.Spec.VolumeMounts...)

	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:        v.Name,
			Namespace:   v.Namespace,
			Annotations: annotations,
		},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: ls,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      ls,
					Annotations: podAnnotations,
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: v.Name,
					Tolerations:        v.Spec.Tolerations,
					ImagePullSecrets:   v.Spec.ImagePullSecrets,
					Containers: []corev1.Container{
						{
							Image: v.Spec.Image,
							Name:  "vector",
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: 8686,
									Name:          "api",
								},
							},
							Env:          v.Spec.Env,
							Resources:    v.Spec.Resources,
							VolumeMounts: volumeMounts,
						},
					},
					Volumes: volumes,
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

// daemonSetNeedsUpdate returns true if the daemonset needs to be updated
func daemonSetNeedsUpdate(vector *vectorv1alpha1.Vector, daemonset *appsv1.DaemonSet) bool {
	if len(daemonset.Spec.Template.Spec.Containers) == 0 {
		return true
	}

	container := daemonset.Spec.Template.Spec.Containers[0]

	// Check if tolerations have changed
	currentTolerations := daemonset.Spec.Template.Spec.Tolerations
	if len(currentTolerations) != len(vector.Spec.Tolerations) {
		return true
	}
	for i, toleration := range currentTolerations {
		if i >= len(vector.Spec.Tolerations) || toleration != vector.Spec.Tolerations[i] {
			return true
		}
	}

	// Check if environment variables have changed
	if !reflect.DeepEqual(container.Env, vector.Spec.Env) {
		return true
	}

	// Check if resources have changed
	if !reflect.DeepEqual(container.Resources, vector.Spec.Resources) {
		return true
	}

	// Check if volumes have changed
	if !reflect.DeepEqual(daemonset.Spec.Template.Spec.Volumes, append([]corev1.Volume{
		{
			Name: "data",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
		{
			Name: "config",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: vector.Name + "-config",
					},
				},
			},
		},
	}, vector.Spec.Volumes...)) {
		return true
	}

	// Check if volume mounts have changed
	if !reflect.DeepEqual(container.VolumeMounts, append([]corev1.VolumeMount{
		{
			Name:      "config",
			MountPath: "/etc/vector",
		},
		{
			Name:      "data",
			MountPath: vector.Spec.DataDir,
		},
	}, vector.Spec.VolumeMounts...)) {
		return true
	}

	// Check if imagePullSecrets have changed
	if !reflect.DeepEqual(daemonset.Spec.Template.Spec.ImagePullSecrets, vector.Spec.ImagePullSecrets) {
		return true
	}

	// Check if config hash has changed in either the DaemonSet or pod template annotations
	currentHash := daemonset.Annotations[configHashAnnotation]
	currentTemplateHash := daemonset.Spec.Template.Annotations[configHashAnnotation]
	if currentHash != vector.Status.ConfigHash || currentTemplateHash != vector.Status.ConfigHash {
		return true
	}

	return container.Image != vector.Spec.Image
}

// reconcileConfigMap creates or updates the Vector ConfigMap and returns its hash
func (r *VectorReconciler) reconcileConfigMap(ctx context.Context, v *vectorv1alpha1.Vector) (string, error) {
	logger := log.FromContext(ctx)

	// Build the configuration sections
	configData := make(map[string]interface{})

	// Add global options
	configData["data_dir"] = "/var/lib/vector"

	// Add API configuration
	apiConfig := map[string]interface{}{
		"enabled": false,
	}
	if v.Spec.API != nil {
		apiConfig["enabled"] = *v.Spec.API.Enabled
		if v.Spec.API.Address != "" {
			apiConfig["address"] = v.Spec.API.Address
		}
	}
	configData["api"] = apiConfig

	// Get all VectorPipelines that reference this Vector
	var pipelineList vectorv1alpha1.VectorPipelineList
	if err := r.List(ctx, &pipelineList, client.InNamespace(v.Namespace)); err != nil {
		return "", err
	}

	// Initialize sources, transforms, and sinks maps
	sources := make(map[string]interface{})
	transforms := make(map[string]interface{})
	sinks := make(map[string]interface{})

	// Add sources, transforms, and sinks from each pipeline
	for _, pipeline := range pipelineList.Items {
		if pipeline.Spec.VectorRef == v.Name {
			// Process Sources
			if pipeline.Spec.Sources.Raw != nil {
				var sourcesMap map[string]interface{}
				if err := json.Unmarshal(pipeline.Spec.Sources.Raw, &sourcesMap); err != nil {
					logger.Error(err, "Failed to unmarshal Sources")
					continue
				}
				for k, v := range sourcesMap {
					sources[k] = v
				}
			}

			// Process Transforms
			if pipeline.Spec.Transforms.Raw != nil {
				var transformsMap map[string]interface{}
				if err := json.Unmarshal(pipeline.Spec.Transforms.Raw, &transformsMap); err != nil {
					logger.Error(err, "Failed to unmarshal Transforms")
					continue
				}
				for k, v := range transformsMap {
					transforms[k] = v
				}
			}

			// Process Sinks
			if pipeline.Spec.Sinks.Raw != nil {
				var sinksMap map[string]interface{}
				if err := json.Unmarshal(pipeline.Spec.Sinks.Raw, &sinksMap); err != nil {
					logger.Error(err, "Failed to unmarshal Sinks")
					continue
				}
				for k, v := range sinksMap {
					sinks[k] = v
				}
			}
		}
	}

	// Add the sections if they have content
	if len(sources) > 0 {
		configData["sources"] = sources
	}
	if len(transforms) > 0 {
		configData["transforms"] = transforms
	}
	if len(sinks) > 0 {
		configData["sinks"] = sinks
	}

	// Convert to YAML
	configYAML, err := yaml.Marshal(configData)
	if err != nil {
		return "", err
	}

	// Create or update ConfigMap
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      v.Name + "-config",
			Namespace: v.Namespace,
		},
		Data: map[string]string{
			"vector.yaml": string(configYAML),
		},
	}

	// Set Vector instance as the owner
	if err := ctrl.SetControllerReference(v, cm, r.Scheme); err != nil {
		return "", err
	}

	// Calculate hash before creating/updating
	configHash := calculateConfigHash(cm)

	// Create or update the ConfigMap
	existingCM := &corev1.ConfigMap{}
	err = r.Get(ctx, types.NamespacedName{Name: cm.Name, Namespace: cm.Namespace}, existingCM)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Creating new ConfigMap",
				"name", cm.Name,
				"namespace", cm.Namespace)
			if err := r.Create(ctx, cm); err != nil {
				return "", err
			}
		} else {
			return "", err
		}
	} else {
		// Only update if the content has changed
		if existingCM.Data["vector.yaml"] != cm.Data["vector.yaml"] {
			logger.Info("Updating existing ConfigMap",
				"name", existingCM.Name,
				"namespace", existingCM.Namespace)
			existingCM.Data = cm.Data
			if err := r.Update(ctx, existingCM); err != nil {
				return "", err
			}
		}
	}

	return configHash, nil
}

// reconcileAgent handles the Vector DaemonSet
func (r *VectorReconciler) reconcileAgent(ctx context.Context, vector *vectorv1alpha1.Vector) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Check if the daemonset already exists, if not create a new one
	daemonset := &appsv1.DaemonSet{}
	err := r.Get(ctx, types.NamespacedName{Name: vector.Name, Namespace: vector.Namespace}, daemonset)
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
		// Update container image, environment variables, resources, and volumes
		newDaemonSet := r.daemonSetForVector(vector)
		daemonset.Spec = newDaemonSet.Spec

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

	return ctrl.Result{RequeueAfter: time.Minute}, nil
}

// reconcileRBAC creates or updates the ClusterRole and ClusterRoleBinding for the Vector instance
func (r *VectorReconciler) reconcileRBAC(ctx context.Context, v *vectorv1alpha1.Vector) error {
	logger := log.FromContext(ctx)

	// Create ClusterRole
	cr := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("vector-%s-%s", v.Namespace, v.Name),
			Labels: map[string]string{
				"app.kubernetes.io/name":       "Vector",
				"app.kubernetes.io/instance":   v.Name,
				"app.kubernetes.io/namespace":  v.Namespace,
				"app.kubernetes.io/managed-by": "vector-operator",
			},
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"pods", "namespaces", "nodes"},
				Verbs:     []string{"get", "list", "watch"},
			},
		},
	}

	// Create or update ClusterRole
	existingCR := &rbacv1.ClusterRole{}
	err := r.Get(ctx, types.NamespacedName{Name: cr.Name}, existingCR)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Creating new ClusterRole", "name", cr.Name)
			return r.Create(ctx, cr)
		}
		return err
	}

	// Update existing ClusterRole
	existingCR.Rules = cr.Rules
	existingCR.Labels = cr.Labels
	if err := r.Update(ctx, existingCR); err != nil {
		return err
	}

	// Create ClusterRoleBinding
	crb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("vector-%s-%s", v.Namespace, v.Name),
			Labels: map[string]string{
				"app.kubernetes.io/name":       "Vector",
				"app.kubernetes.io/instance":   v.Name,
				"app.kubernetes.io/namespace":  v.Namespace,
				"app.kubernetes.io/managed-by": "vector-operator",
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     cr.Name,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      v.Name,
				Namespace: v.Namespace,
			},
		},
	}

	// Create or update ClusterRoleBinding
	existingCRB := &rbacv1.ClusterRoleBinding{}
	err = r.Get(ctx, types.NamespacedName{Name: crb.Name}, existingCRB)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Creating new ClusterRoleBinding", "name", crb.Name)
			return r.Create(ctx, crb)
		}
		return err
	}

	// Update existing ClusterRoleBinding
	existingCRB.RoleRef = crb.RoleRef
	existingCRB.Subjects = crb.Subjects
	existingCRB.Labels = crb.Labels
	return r.Update(ctx, existingCRB)
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

	// Delete ServiceAccount if it exists
	sa := &corev1.ServiceAccount{}
	err = r.Get(ctx, types.NamespacedName{Name: v.Name, Namespace: v.Namespace}, sa)
	if err == nil {
		logger.Info("Deleting ServiceAccount", "name", sa.Name)
		if err := r.Delete(ctx, sa); err != nil && !errors.IsNotFound(err) {
			return err
		}
	}

	// Delete ClusterRole if it exists
	cr := &rbacv1.ClusterRole{}
	crName := fmt.Sprintf("vector-%s-%s", v.Namespace, v.Name)
	err = r.Get(ctx, types.NamespacedName{Name: crName}, cr)
	if err == nil {
		logger.Info("Deleting ClusterRole", "name", cr.Name)
		if err := r.Delete(ctx, cr); err != nil && !errors.IsNotFound(err) {
			return err
		}
	}

	// Delete ClusterRoleBinding if it exists
	crb := &rbacv1.ClusterRoleBinding{}
	crbName := fmt.Sprintf("vector-%s-%s", v.Namespace, v.Name)
	err = r.Get(ctx, types.NamespacedName{Name: crbName}, crb)
	if err == nil {
		logger.Info("Deleting ClusterRoleBinding", "name", crb.Name)
		if err := r.Delete(ctx, crb); err != nil && !errors.IsNotFound(err) {
			return err
		}
	}

	return nil
}

// reconcileServiceAccount creates or updates the Vector ServiceAccount
func (r *VectorReconciler) reconcileServiceAccount(ctx context.Context, v *vectorv1alpha1.Vector) error {
	logger := log.FromContext(ctx)

	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      v.Name,
			Namespace: v.Namespace,
		},
	}

	// Add annotations if specified
	if v.Spec.ServiceAccount != nil && v.Spec.ServiceAccount.Annotations != nil {
		sa.ObjectMeta.Annotations = v.Spec.ServiceAccount.Annotations
	}

	// Set Vector instance as the owner
	if err := ctrl.SetControllerReference(v, sa, r.Scheme); err != nil {
		return err
	}

	// Create or update the ServiceAccount
	existingSA := &corev1.ServiceAccount{}
	err := r.Get(ctx, types.NamespacedName{Name: sa.Name, Namespace: sa.Namespace}, existingSA)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Creating new ServiceAccount",
				"name", sa.Name,
				"namespace", sa.Namespace)
			return r.Create(ctx, sa)
		}
		return err
	}

	// Update annotations if they've changed
	if !reflect.DeepEqual(existingSA.Annotations, sa.Annotations) {
		existingSA.Annotations = sa.Annotations
		logger.Info("Updating ServiceAccount annotations",
			"name", existingSA.Name,
			"namespace", existingSA.Namespace)
		return r.Update(ctx, existingSA)
	}

	return nil
}

// labelsForVector returns the labels for selecting the resources
func labelsForVector(name string) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":       "Vector",
		"app.kubernetes.io/instance":   name,
		"app.kubernetes.io/managed-by": "vector-operator",
	}
}

// updateVectorStatus updates the Status field of the Vector resource
func (r *VectorReconciler) updateVectorStatus(ctx context.Context, vector *vectorv1alpha1.Vector, obj runtime.Object) error {
	// Update the status condition based on the object type and status
	condition := metav1.Condition{
		Type:               "Available",
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: vector.Generation,
	}

	switch v := obj.(type) {
	case *appsv1.DaemonSet:
		condition.Reason = "DaemonSetAvailable"
		condition.Message = "Vector daemonset is available"
		if v.Status.NumberReady == 0 {
			condition.Status = metav1.ConditionFalse
			condition.Reason = "DaemonSetUnavailable"
			condition.Message = "Vector daemonset is not available"
		}
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
		Owns(&corev1.ServiceAccount{}).
		Complete(r)
}
