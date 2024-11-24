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

// daemonSetForVector returns a vector DaemonSet object
func (r *VectorReconciler) daemonSetForVector(v *vectorv1alpha1.Vector) *appsv1.DaemonSet {
	ls := labelsForVector(v.Name)

	// Define default toleration
	defaultToleration := []corev1.Toleration{
		{
			Operator: corev1.TolerationOpExists,
		},
	}

	// Use custom tolerations if specified, otherwise use default
	tolerations := defaultToleration
	if v.Spec.Agent != nil && len(v.Spec.Agent.Tolerations) > 0 {
		tolerations = v.Spec.Agent.Tolerations
	}

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
					ServiceAccountName: v.Name,
					Tolerations:        tolerations,
					Containers: []corev1.Container{
						{
							Image: v.Spec.Image,
							Name:  "vector",
							Env: []corev1.EnvVar{
								{
									Name: "VECTOR_SELF_NODE_NAME",
									ValueFrom: &corev1.EnvVarSource{
										FieldRef: &corev1.ObjectFieldSelector{
											APIVersion: "v1",
											FieldPath:  "spec.nodeName",
										},
									},
								},
								{
									Name: "VECTOR_SELF_POD_NAME",
									ValueFrom: &corev1.EnvVarSource{
										FieldRef: &corev1.ObjectFieldSelector{
											APIVersion: "v1",
											FieldPath:  "metadata.name",
										},
									},
								},
								{
									Name: "VECTOR_SELF_POD_NAMESPACE",
									ValueFrom: &corev1.EnvVarSource{
										FieldRef: &corev1.ObjectFieldSelector{
											APIVersion: "v1",
											FieldPath:  "metadata.namespace",
										},
									},
								},
								{
									Name:  "PROCFS_ROOT",
									Value: "/host/proc",
								},
								{
									Name:  "SYSFS_ROOT",
									Value: "/host/sys",
								},
							},
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
								{
									Name:      "var-log",
									MountPath: "/var/log",
									ReadOnly:  true,
								},
								{
									Name:      "var-lib",
									MountPath: "/var/lib",
									ReadOnly:  true,
								},
								{
									Name:      "procfs",
									MountPath: "/host/proc",
									ReadOnly:  true,
								},
								{
									Name:      "sysfs",
									MountPath: "/host/sys",
									ReadOnly:  true,
								},
							},
						},
					},
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
							Name: "data",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/var/lib/vector",
								},
							},
						},
						{
							Name: "var-lib",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/var/lib",
								},
							},
						},
						{
							Name: "procfs",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/proc",
								},
							},
						},
						{
							Name: "sysfs",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/sys",
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

// daemonSetNeedsUpdate returns true if the daemonset needs to be updated
func daemonSetNeedsUpdate(vector *vectorv1alpha1.Vector, daemonset *appsv1.DaemonSet) bool {
	if len(daemonset.Spec.Template.Spec.Containers) == 0 {
		return true
	}
	return daemonset.Spec.Template.Spec.Containers[0].Image != vector.Spec.Image ||
		!tolerationsEqual(daemonset.Spec.Template.Spec.Tolerations, getTolerations(vector))
}

// tolerationsEqual compares two slices of tolerations
func tolerationsEqual(a, b []corev1.Toleration) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Key != b[i].Key ||
			a[i].Operator != b[i].Operator ||
			a[i].Value != b[i].Value ||
			a[i].Effect != b[i].Effect {
			return false
		}
	}
	return true
}

// getTolerations returns the tolerations to use for the DaemonSet
func getTolerations(vector *vectorv1alpha1.Vector) []corev1.Toleration {
	defaultToleration := []corev1.Toleration{
		{
			Operator: corev1.TolerationOpExists,
		},
	}

	if vector.Spec.Agent != nil && len(vector.Spec.Agent.Tolerations) > 0 {
		return vector.Spec.Agent.Tolerations
	}
	return defaultToleration
}
