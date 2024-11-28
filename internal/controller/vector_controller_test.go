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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
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

		var controllerReconciler *VectorReconciler

		BeforeEach(func() {
			// Create the default namespace
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "default",
				},
			}
			err := k8sClient.Create(ctx, ns)
			if err != nil {
				// Ignore if the namespace already exists
				Expect(err.Error()).To(ContainSubstring("already exists"))
			}

			// Clean up any existing resources
			vector := &vectorv1alpha1.Vector{}
			err = k8sClient.Get(ctx, typeNamespacedName, vector)
			if err == nil {
				// Delete the Vector CR and wait for cleanup
				Expect(k8sClient.Delete(ctx, vector)).To(Succeed())
				Eventually(func() bool {
					err := k8sClient.Get(ctx, typeNamespacedName, vector)
					return errors.IsNotFound(err)
				}, time.Second*30, time.Second).Should(BeTrue())
			}

			By("creating the custom resource for the Kind Vector")
			enabled := false
			playground := false
			expireMetrics := int32(30)

			vector = &vectorv1alpha1.Vector{
				ObjectMeta: metav1.ObjectMeta{
					Name:       resourceName,
					Namespace:  "default",
					Finalizers: []string{vectorFinalizer},
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

			// Create the reconciler
			controllerReconciler = &VectorReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
		})

		AfterEach(func() {
			// Clean up Vector resource and wait for finalizer to complete
			vector := &vectorv1alpha1.Vector{}
			err := k8sClient.Get(ctx, typeNamespacedName, vector)
			if err == nil {
				// Delete the Vector CR
				Expect(k8sClient.Delete(ctx, vector)).To(Succeed())

				// Trigger reconciliation to process the deletion
				_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: typeNamespacedName,
				})
				Expect(err).NotTo(HaveOccurred())

				// Wait for the Vector CR to be deleted
				Eventually(func() bool {
					err := k8sClient.Get(ctx, typeNamespacedName, vector)
					return errors.IsNotFound(err)
				}, time.Second*30, time.Second).Should(BeTrue())

				// Wait for all owned resources to be cleaned up
				// Check ConfigMap
				Eventually(func() bool {
					cm := &corev1.ConfigMap{}
					err := k8sClient.Get(ctx, types.NamespacedName{
						Name:      resourceName + "-config",
						Namespace: "default",
					}, cm)
					return errors.IsNotFound(err)
				}, time.Second*30, time.Second).Should(BeTrue())

				// Check ServiceAccount
				Eventually(func() bool {
					sa := &corev1.ServiceAccount{}
					err := k8sClient.Get(ctx, typeNamespacedName, sa)
					return errors.IsNotFound(err)
				}, time.Second*30, time.Second).Should(BeTrue())

				// Check ClusterRole
				Eventually(func() bool {
					cr := &rbacv1.ClusterRole{}
					err := k8sClient.Get(ctx, types.NamespacedName{
						Name: "vector-default-" + resourceName,
					}, cr)
					return errors.IsNotFound(err)
				}, time.Second*30, time.Second).Should(BeTrue())

				// Check ClusterRoleBinding
				Eventually(func() bool {
					crb := &rbacv1.ClusterRoleBinding{}
					err := k8sClient.Get(ctx, types.NamespacedName{
						Name: "vector-default-" + resourceName,
					}, crb)
					return errors.IsNotFound(err)
				}, time.Second*30, time.Second).Should(BeTrue())

				// Check DaemonSet
				Eventually(func() bool {
					ds := &appsv1.DaemonSet{}
					err := k8sClient.Get(ctx, typeNamespacedName, ds)
					return errors.IsNotFound(err)
				}, time.Second*30, time.Second).Should(BeTrue())
			}
		})

		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			// Perform multiple reconciliations to ensure resources are created
			for i := 0; i < 3; i++ {
				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: typeNamespacedName,
				})
				Expect(err).NotTo(HaveOccurred())
				time.Sleep(time.Second) // Give time for resources to be created
			}

			By("Checking if ConfigMap was created")
			configMap := &corev1.ConfigMap{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      resourceName + "-config",
					Namespace: "default",
				}, configMap)
			}, time.Second*30, time.Second).Should(Succeed())

			By("Verifying ConfigMap content")
			var config map[string]interface{}
			Expect(yaml.Unmarshal([]byte(configMap.Data["vector.yaml"]), &config)).To(Succeed())

			Expect(config["data_dir"]).To(Equal("/var/lib/vector"))
			api := config["api"].(map[interface{}]interface{})
			Expect(api["enabled"]).To(BeFalse())
			Expect(api["address"]).To(Equal("0.0.0.0:8686"))

			By("Checking if DaemonSet was created")
			daemonSet := &appsv1.DaemonSet{}
			Eventually(func() error {
				return k8sClient.Get(ctx, typeNamespacedName, daemonSet)
			}, time.Second*30, time.Second).Should(Succeed())

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
