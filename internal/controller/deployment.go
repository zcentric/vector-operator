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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	vectorv1alpha1 "github.com/zcentric/vector-operator/api/v1alpha1"
)

// deploymentForVector returns a vector Deployment object
func (r *VectorReconciler) deploymentForVector(v *vectorv1alpha1.Vector) *appsv1.Deployment {
	ls := labelsForVector(v.Name)
	replicas := v.Spec.Replicas
	if replicas == 0 {
		replicas = 1 // Set default to 1 if not specified
	}

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
					ServiceAccountName: v.Name,
					Tolerations:        v.Spec.Tolerations,
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
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "config",
									MountPath: v.Spec.DataDir,
								},
								{
									Name:      "data",
									MountPath: "/var/lib/vector",
								},
							},
						},
					},
					Volumes: []corev1.Volume{
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
					},
				},
			},
		},
	}

	// Set Vector instance as the owner and controller
	if err := ctrl.SetControllerReference(v, dep, r.Scheme); err != nil {
		return nil
	}
	return dep
}

// deploymentNeedsUpdate returns true if the deployment needs to be updated
func deploymentNeedsUpdate(vector *vectorv1alpha1.Vector, deployment *appsv1.Deployment) bool {
	if len(deployment.Spec.Template.Spec.Containers) == 0 {
		return true
	}

	currentReplicas := int32(1)
	if deployment.Spec.Replicas != nil {
		currentReplicas = *deployment.Spec.Replicas
	}

	targetReplicas := vector.Spec.Replicas
	if targetReplicas == 0 {
		targetReplicas = 1
	}

	// Check if tolerations have changed
	currentTolerations := deployment.Spec.Template.Spec.Tolerations
	if len(currentTolerations) != len(vector.Spec.Tolerations) {
		return true
	}
	for i, toleration := range currentTolerations {
		if i >= len(vector.Spec.Tolerations) || toleration != vector.Spec.Tolerations[i] {
			return true
		}
	}

	return deployment.Spec.Template.Spec.Containers[0].Image != vector.Spec.Image ||
		currentReplicas != targetReplicas
}
