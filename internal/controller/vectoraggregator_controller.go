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
	"reflect"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	vectorv1alpha1 "github.com/zcentric/vector-operator/api/v1alpha1"
	"github.com/zcentric/vector-operator/internal/metrics"
	"github.com/zcentric/vector-operator/internal/utils"
)

// VectorAggregatorReconciler reconciles a VectorAggregator object
type VectorAggregatorReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=vector.zcentric.com,resources=vectoraggregators,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=vector.zcentric.com,resources=vectoraggregators/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=vector.zcentric.com,resources=vectoraggregators/finalizers,verbs=update
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete

// Reconcile handles the reconciliation loop for VectorAggregator resources
func (r *VectorAggregatorReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	startTime := time.Now()
	defer func() {
		metrics.ReconciliationDuration.WithLabelValues("aggregator", req.Namespace).Observe(time.Since(startTime).Seconds())
		metrics.ReconciliationCount.WithLabelValues("aggregator", "total", req.Namespace).Inc()
	}()

	// Fetch the VectorAggregator instance
	var vectorAggregator vectorv1alpha1.VectorAggregator
	if err := r.Get(ctx, req.NamespacedName, &vectorAggregator); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Create or update the deployment
	deployment := &appsv1.Deployment{}
	err := r.Get(ctx, req.NamespacedName, deployment)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			log.Error(err, "Failed to get Deployment")
			return ctrl.Result{}, err
		}

		// Create new deployment
		deployment = r.deploymentForVectorAggregator(&vectorAggregator)
		if err := r.Create(ctx, deployment); err != nil {
			log.Error(err, "Failed to create Deployment")
			return ctrl.Result{}, err
		}

		log.Info("Created deployment", "Deployment.Namespace", deployment.Namespace, "Deployment.Name", deployment.Name)
		return ctrl.Result{Requeue: true}, nil
	}

	// Update existing deployment if needed
	if needsUpdate(deployment, &vectorAggregator) {
		newDeployment := r.deploymentForVectorAggregator(&vectorAggregator)
		deployment.Spec = newDeployment.Spec
		if err := r.Update(ctx, deployment); err != nil {
			log.Error(err, "Failed to update Deployment")
			return ctrl.Result{}, err
		}
		log.Info("Updated deployment", "Deployment.Namespace", deployment.Namespace, "Deployment.Name", deployment.Name)
	}

	// Update pod metrics
	if deployment != nil {
		metrics.VectorPodsDesired.WithLabelValues("aggregator", req.Namespace).Set(float64(*deployment.Spec.Replicas))
		metrics.VectorPodsAvailable.WithLabelValues("aggregator", req.Namespace).Set(float64(deployment.Status.AvailableReplicas))
		metrics.VectorPodsUnavailable.WithLabelValues("aggregator", req.Namespace).Set(float64(*deployment.Spec.Replicas - deployment.Status.AvailableReplicas))
	}

	// Update health metrics
	if vectorAggregator.Status.Conditions != nil {
		readyCondition := meta.FindStatusCondition(vectorAggregator.Status.Conditions, VectorReadyCondition)
		if readyCondition != nil && readyCondition.Status == metav1.ConditionTrue {
			metrics.ResourceHealth.WithLabelValues("aggregator", vectorAggregator.Name, req.Namespace).Set(1)
		} else {
			metrics.ResourceHealth.WithLabelValues("aggregator", vectorAggregator.Name, req.Namespace).Set(0)
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager
func (r *VectorAggregatorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&vectorv1alpha1.VectorAggregator{}).
		Owns(&appsv1.Deployment{}).
		Complete(r)
}

// deploymentForVectorAggregator returns a vector Deployment object
func (r *VectorAggregatorReconciler) deploymentForVectorAggregator(v *vectorv1alpha1.VectorAggregator) *appsv1.Deployment {
	ls := labelsForVectorAggregator(v.Name)
	replicas := v.Spec.Replicas

	// Combine default volumes with user-defined volumes
	volumes := []corev1.Volume{
		{
			Name: "data",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
	}
	volumes = append(volumes, v.Spec.Volumes...)

	// Combine default volume mounts with user-defined volume mounts
	volumeMounts := []corev1.VolumeMount{
		{
			Name:      "data",
			MountPath: v.Spec.DataDir,
		},
	}
	volumeMounts = append(volumeMounts, v.Spec.VolumeMounts...)

	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      v.Name,
			Namespace: v.Namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: ls,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: ls,
				},
				Spec: corev1.PodSpec{
					TopologySpreadConstraints: v.Spec.TopologySpreadConstraints,
					ImagePullSecrets:          v.Spec.ImagePullSecrets,
					Containers: []corev1.Container{{
						Image: v.Spec.Image,
						Name:  "vector",
						Ports: []corev1.ContainerPort{{
							ContainerPort: 8686,
							Name:          "api",
						}},
						Env:          utils.MergeEnvVars(utils.GetVectorEnvVars(), v.Spec.Env),
						Resources:    v.Spec.Resources,
						VolumeMounts: volumeMounts,
					}},
					Volumes:     volumes,
					Tolerations: v.Spec.Tolerations,
				},
			},
		},
	}

	// Set owner reference
	ctrl.SetControllerReference(v, dep, r.Scheme)
	return dep
}

// labelsForVectorAggregator returns the labels for selecting the resources
func labelsForVectorAggregator(name string) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":      "vector",
		"app.kubernetes.io/instance":  name,
		"app.kubernetes.io/component": "aggregator",
		"app.kubernetes.io/part-of":   "vector-operator",
	}
}

// needsUpdate determines if the deployment needs to be updated
func needsUpdate(deployment *appsv1.Deployment, v *vectorv1alpha1.VectorAggregator) bool {
	if deployment.Spec.Replicas == nil || *deployment.Spec.Replicas != v.Spec.Replicas {
		return true
	}

	if len(deployment.Spec.Template.Spec.Containers) == 0 {
		return true
	}

	container := deployment.Spec.Template.Spec.Containers[0]

	// Check if image has changed
	if container.Image != v.Spec.Image {
		return true
	}

	// Check if environment variables have changed
	expectedEnv := utils.MergeEnvVars(utils.GetVectorEnvVars(), v.Spec.Env)
	if !reflect.DeepEqual(container.Env, expectedEnv) {
		return true
	}

	// Check if resources have changed
	if !reflect.DeepEqual(container.Resources, v.Spec.Resources) {
		return true
	}

	// Check if topology spread constraints have changed
	if !reflect.DeepEqual(deployment.Spec.Template.Spec.TopologySpreadConstraints, v.Spec.TopologySpreadConstraints) {
		return true
	}

	// Check if volumes have changed
	defaultVolumes := []corev1.Volume{
		{
			Name: "data",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
	}
	expectedVolumes := append(defaultVolumes, v.Spec.Volumes...)
	if !reflect.DeepEqual(deployment.Spec.Template.Spec.Volumes, expectedVolumes) {
		return true
	}

	// Check if volume mounts have changed
	defaultVolumeMounts := []corev1.VolumeMount{
		{
			Name:      "data",
			MountPath: v.Spec.DataDir,
		},
	}
	expectedVolumeMounts := append(defaultVolumeMounts, v.Spec.VolumeMounts...)
	if !reflect.DeepEqual(container.VolumeMounts, expectedVolumeMounts) {
		return true
	}

	// Check if imagePullSecrets have changed
	if !reflect.DeepEqual(deployment.Spec.Template.Spec.ImagePullSecrets, v.Spec.ImagePullSecrets) {
		return true
	}

	// Check if tolerations have changed
	if !reflect.DeepEqual(deployment.Spec.Template.Spec.Tolerations, v.Spec.Tolerations) {
		return true
	}

	return false
}
