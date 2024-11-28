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

package e2e

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vectorv1alpha1 "github.com/zcentric/vector-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("controller", Ordered, func() {
	Context("Operator", func() {
		It("should run successfully", func() {
			ctx := context.Background()

			By("Creating a Vector instance")
			vector := &vectorv1alpha1.Vector{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vector",
					Namespace: "default",
				},
				Spec: vectorv1alpha1.VectorSpec{
					Image: "timberio/vector:0.34.0-debian",
				},
			}
			Expect(k8sClient.Create(ctx, vector)).Should(Succeed())

			By("Verifying Vector status")
			vectorLookupKey := types.NamespacedName{Name: "test-vector", Namespace: "default"}
			createdVector := &vectorv1alpha1.Vector{}

			Eventually(func() bool {
				err := k8sClient.Get(ctx, vectorLookupKey, createdVector)
				if err != nil {
					return false
				}
				GinkgoWriter.Printf("Vector status: %+v\n", createdVector.Status)
				return createdVector.Status.ConfigHash != ""
			}, time.Second*10, time.Second*1).Should(BeTrue())

			By("Cleaning up Vector instance")
			Expect(k8sClient.Delete(ctx, vector)).Should(Succeed())
		})
	})
})
