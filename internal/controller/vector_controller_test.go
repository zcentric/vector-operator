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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	vectorv1alpha1 "github.com/zcentric/vector-operator/api/v1alpha1"
)

var _ = Describe("Vector Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-vector"

		ctx := context.Background()
		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			By("creating the custom resource for the Kind Vector")
			enabled := false
			playground := false
			expireMetrics := int32(30)

			vector := &vectorv1alpha1.Vector{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: vectorv1alpha1.VectorSpec{
					Image: "timberio/vector:0.24.0-distroless-libc",
					API: &vectorv1alpha1.VectorAPI{
						Address:    "0.0.0.0:8686",
						Enabled:    &enabled,
						Playground: &playground,
					},
					DataDir:           "/tmp/vector-data-dir",
					ExpireMetricsSecs: &expireMetrics,
				},
			}
			Expect(k8sClient.Create(ctx, vector)).To(Succeed())
		})

		AfterEach(func() {
			resource := &vectorv1alpha1.Vector{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
		})

		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &VectorReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Checking if ConfigMap was created")
			configMap := &corev1.ConfigMap{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      resourceName + "-config",
					Namespace: "default",
				}, configMap)
			}).Should(Succeed())

			By("Verifying ConfigMap content")
			var config map[string]interface{}
			Expect(yaml.Unmarshal([]byte(configMap.Data["vector.yaml"]), &config)).To(Succeed())

			Expect(config["data_dir"]).To(Equal("/tmp/vector-data-dir"))
			api := config["api"].(map[interface{}]interface{})
			Expect(api["enabled"]).To(BeFalse())
			Expect(api["address"]).To(Equal("0.0.0.0:8686"))

			By("Checking if DaemonSet was created")
			daemonSet := &appsv1.DaemonSet{}
			Eventually(func() error {
				return k8sClient.Get(ctx, typeNamespacedName, daemonSet)
			}).Should(Succeed())

			By("Verifying DaemonSet configuration")
			container := daemonSet.Spec.Template.Spec.Containers[0]
			Expect(container.Image).To(Equal("timberio/vector:0.24.0-distroless-libc"))

			By("Verifying ConfigMap is mounted")
			var configVolume *corev1.Volume
			for _, vol := range daemonSet.Spec.Template.Spec.Volumes {
				if vol.Name == "config" {
					configVolume = &vol
					break
				}
			}
			Expect(configVolume).NotTo(BeNil())
			Expect(configVolume.ConfigMap.LocalObjectReference.Name).To(Equal(resourceName + "-config"))

			var configMount *corev1.VolumeMount
			for _, mount := range container.VolumeMounts {
				if mount.Name == "config" {
					configMount = &mount
					break
				}
			}
			Expect(configMount).NotTo(BeNil())
			Expect(configMount.MountPath).To(Equal("/etc/vector"))
		})
	})
})
