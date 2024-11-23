package controller

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vectorv1alpha1 "github.com/zcentric/vector-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("VectorPipeline Controller", func() {
	const (
		timeout  = time.Second * 10
		interval = time.Millisecond * 250

		VectorPipelineName = "test-pipeline"
		VectorName         = "test-vector"
		Namespace          = "default"
	)

	Context("When creating a VectorPipeline", func() {
		ctx := context.Background()

		It("Should properly handle Vector reference validation", func() {
			By("Creating a new VectorPipeline without Vector")
			sourceConfig := map[string]interface{}{
				"path": "/var/log/test.log",
			}
			sourceConfigJSON, err := json.Marshal(sourceConfig)
			Expect(err).ShouldNot(HaveOccurred())

			pipeline := &vectorv1alpha1.VectorPipeline{
				ObjectMeta: metav1.ObjectMeta{
					Name:      VectorPipelineName,
					Namespace: Namespace,
				},
				Spec: vectorv1alpha1.VectorPipelineSpec{
					VectorRef: VectorName,
					Sources: map[string]vectorv1alpha1.Source{
						"test-source": {
							Type: "file",
							Config: runtime.RawExtension{
								Raw: sourceConfigJSON,
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, pipeline)).Should(Succeed())

			// Verify condition is set to false due to missing Vector
			pipelineLookupKey := types.NamespacedName{Name: VectorPipelineName, Namespace: Namespace}
			createdPipeline := &vectorv1alpha1.VectorPipeline{}

			Eventually(func() bool {
				err := k8sClient.Get(ctx, pipelineLookupKey, createdPipeline)
				if err != nil {
					return false
				}
				for _, condition := range createdPipeline.Status.Conditions {
					if condition.Type == VectorRefCondition {
						return condition.Status == metav1.ConditionFalse
					}
				}
				return false
			}, timeout, interval).Should(BeTrue())

			By("Creating the referenced Vector")
			vector := &vectorv1alpha1.Vector{
				ObjectMeta: metav1.ObjectMeta{
					Name:      VectorName,
					Namespace: Namespace,
				},
			}
			Expect(k8sClient.Create(ctx, vector)).Should(Succeed())

			// Verify condition is updated to true
			Eventually(func() bool {
				err := k8sClient.Get(ctx, pipelineLookupKey, createdPipeline)
				if err != nil {
					return false
				}
				for _, condition := range createdPipeline.Status.Conditions {
					if condition.Type == VectorRefCondition {
						return condition.Status == metav1.ConditionTrue
					}
				}
				return false
			}, timeout, interval).Should(BeTrue())
		})

		It("Should create ConfigMap with merged pipeline configurations", func() {
			By("Creating multiple pipelines with the same Vector reference")
			sourceConfig1 := map[string]interface{}{
				"path": "/var/log/1.log",
			}
			sourceConfig1JSON, err := json.Marshal(sourceConfig1)
			Expect(err).ShouldNot(HaveOccurred())

			sourceConfig2 := map[string]interface{}{
				"path": "/var/log/2.log",
			}
			sourceConfig2JSON, err := json.Marshal(sourceConfig2)
			Expect(err).ShouldNot(HaveOccurred())

			pipeline1 := &vectorv1alpha1.VectorPipeline{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pipeline1",
					Namespace: Namespace,
				},
				Spec: vectorv1alpha1.VectorPipelineSpec{
					VectorRef: VectorName,
					Sources: map[string]vectorv1alpha1.Source{
						"source1": {
							Type: "file",
							Config: runtime.RawExtension{
								Raw: sourceConfig1JSON,
							},
						},
					},
				},
			}

			pipeline2 := &vectorv1alpha1.VectorPipeline{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pipeline2",
					Namespace: Namespace,
				},
				Spec: vectorv1alpha1.VectorPipelineSpec{
					VectorRef: VectorName,
					Sources: map[string]vectorv1alpha1.Source{
						"source2": {
							Type: "file",
							Config: runtime.RawExtension{
								Raw: sourceConfig2JSON,
							},
						},
					},
				},
			}

			Expect(k8sClient.Create(ctx, pipeline1)).Should(Succeed())
			Expect(k8sClient.Create(ctx, pipeline2)).Should(Succeed())

			// Verify ConfigMap is created with merged configurations
			configMapLookupKey := types.NamespacedName{Name: VectorName, Namespace: Namespace}
			createdConfigMap := &corev1.ConfigMap{}

			Eventually(func() bool {
				err := k8sClient.Get(ctx, configMapLookupKey, createdConfigMap)
				if err != nil {
					return false
				}
				config := createdConfigMap.Data["vector.yaml"]
				return len(config) > 0 &&
					strings.Contains(config, "source1") &&
					strings.Contains(config, "source2")
			}, timeout, interval).Should(BeTrue())
		})

		AfterEach(func() {
			// Cleanup
			k8sClient.DeleteAllOf(ctx, &vectorv1alpha1.VectorPipeline{}, client.InNamespace(Namespace))
			k8sClient.DeleteAllOf(ctx, &vectorv1alpha1.Vector{}, client.InNamespace(Namespace))
			k8sClient.DeleteAllOf(ctx, &corev1.ConfigMap{}, client.InNamespace(Namespace))
		})
	})
})
