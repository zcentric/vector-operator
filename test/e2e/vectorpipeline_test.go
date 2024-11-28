package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vectorv1alpha1 "github.com/zcentric/vector-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/yaml"
)

var _ = Describe("VectorPipeline E2E", func() {
	const (
		timeout  = time.Second * 120 // Increased timeout to 2 minutes
		interval = time.Second * 1
	)

	Context("When deploying a complete Vector configuration", func() {
		It("Should successfully create and validate the pipeline configuration", func() {
			ctx := context.Background()

			By("Creating a Vector instance")
			vector := &vectorv1alpha1.Vector{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "e2e-vector",
					Namespace: "default",
				},
				Spec: vectorv1alpha1.VectorSpec{
					Image: "timberio/vector:0.34.0-debian",
				},
			}
			Expect(k8sClient.Create(ctx, vector)).Should(Succeed())

			// Wait for Vector instance to be ready
			vectorLookupKey := types.NamespacedName{Name: "e2e-vector", Namespace: "default"}
			createdVector := &vectorv1alpha1.Vector{}
			Eventually(func() error {
				err := k8sClient.Get(ctx, vectorLookupKey, createdVector)
				if err != nil {
					return fmt.Errorf("failed to get Vector: %v", err)
				}
				GinkgoWriter.Printf("Vector status: %+v\n", createdVector.Status)
				return nil
			}, timeout, interval).Should(Succeed(), "Vector instance should be created")

			By("Creating a VectorPipeline with a complete configuration")
			sources := map[string]interface{}{
				"apache_logs": map[string]interface{}{
					"type":    "file",
					"include": []string{"/var/log/apache2/*.log"},
					"format":  "apache_common",
				},
			}
			sourcesJSON, err := json.Marshal(sources)
			Expect(err).ShouldNot(HaveOccurred())

			transforms := map[string]interface{}{
				"parse_logs": map[string]interface{}{
					"type":   "remap",
					"inputs": []string{"apache_logs"},
					"source": ". = parse_apache_log(.)",
				},
			}
			transformsJSON, err := json.Marshal(transforms)
			Expect(err).ShouldNot(HaveOccurred())

			sinks := map[string]interface{}{
				"elasticsearch_out": map[string]interface{}{
					"type":      "elasticsearch",
					"inputs":    []string{"parse_logs"},
					"endpoints": []string{"http://elasticsearch:9200"},
					"index":     "vector-logs-%F",
				},
			}
			sinksJSON, err := json.Marshal(sinks)
			Expect(err).ShouldNot(HaveOccurred())

			pipeline := &vectorv1alpha1.VectorPipeline{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "e2e-pipeline",
					Namespace: "default",
				},
				Spec: vectorv1alpha1.VectorPipelineSpec{
					VectorRef: "e2e-vector",
					Sources: runtime.RawExtension{
						Raw: sourcesJSON,
					},
					Transforms: runtime.RawExtension{
						Raw: transformsJSON,
					},
					Sinks: runtime.RawExtension{
						Raw: sinksJSON,
					},
				},
			}
			Expect(k8sClient.Create(ctx, pipeline)).Should(Succeed())

			By("Verifying the pipeline status condition")
			pipelineLookupKey := types.NamespacedName{Name: "e2e-pipeline", Namespace: "default"}
			createdPipeline := &vectorv1alpha1.VectorPipeline{}

			Eventually(func() (bool, error) {
				err := k8sClient.Get(ctx, pipelineLookupKey, createdPipeline)
				if err != nil {
					return false, fmt.Errorf("failed to get pipeline: %v", err)
				}

				// Log current conditions and status for debugging
				GinkgoWriter.Printf("Pipeline status conditions: %+v\n", createdPipeline.Status.Conditions)

				// Verify Vector reference again
				vector := &vectorv1alpha1.Vector{}
				if err := k8sClient.Get(ctx, vectorLookupKey, vector); err != nil {
					return false, fmt.Errorf("failed to get referenced Vector: %v", err)
				}
				GinkgoWriter.Printf("Referenced Vector exists: %s/%s\n", vector.Namespace, vector.Name)

				for _, condition := range createdPipeline.Status.Conditions {
					if condition.Type == "VectorRefValid" {
						if condition.Status == metav1.ConditionTrue {
							return true, nil
						}
						return false, fmt.Errorf("VectorRefValid condition is %s: %s", condition.Status, condition.Message)
					}
				}
				return false, fmt.Errorf("VectorRefValid condition not found")
			}, timeout, interval).Should(BeTrue())

			By("Verifying the generated ConfigMap")
			configMapLookupKey := types.NamespacedName{Name: "e2e-vector-config", Namespace: "default"}
			createdConfigMap := &corev1.ConfigMap{}

			Eventually(func() error {
				err := k8sClient.Get(ctx, configMapLookupKey, createdConfigMap)
				if err != nil {
					return fmt.Errorf("failed to get ConfigMap: %v", err)
				}
				GinkgoWriter.Printf("ConfigMap found: %s/%s\n", createdConfigMap.Namespace, createdConfigMap.Name)
				return nil
			}, timeout, interval).Should(Succeed())

			By("Validating the YAML configuration")
			config := createdConfigMap.Data["vector.yaml"]
			var parsedConfig map[string]interface{}
			Expect(yaml.Unmarshal([]byte(config), &parsedConfig)).Should(Succeed())

			Expect(parsedConfig).Should(HaveKey("sources"))
			Expect(parsedConfig).Should(HaveKey("transforms"))
			Expect(parsedConfig).Should(HaveKey("sinks"))

			sources = parsedConfig["sources"].(map[string]interface{})
			Expect(sources).Should(HaveKey("apache_logs"))

			transforms = parsedConfig["transforms"].(map[string]interface{})
			Expect(transforms).Should(HaveKey("parse_logs"))

			sinks = parsedConfig["sinks"].(map[string]interface{})
			Expect(sinks).Should(HaveKey("elasticsearch_out"))
		})
	})
})
