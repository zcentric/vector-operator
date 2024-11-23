package e2e

import (
	"context"
	"encoding/json"
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
		timeout  = time.Second * 30
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
			}
			Expect(k8sClient.Create(ctx, vector)).Should(Succeed())

			By("Creating a VectorPipeline with a complete configuration")
			sourceConfig := map[string]interface{}{
				"include": []string{"/var/log/apache2/*.log"},
				"type":    "apache_common",
			}
			sourceConfigJSON, err := json.Marshal(sourceConfig)
			Expect(err).ShouldNot(HaveOccurred())

			transformConfig := map[string]interface{}{
				"source": `. = parse_apache_log(.)`,
			}
			transformConfigJSON, err := json.Marshal(transformConfig)
			Expect(err).ShouldNot(HaveOccurred())

			sinkConfig := map[string]interface{}{
				"endpoints": []string{"http://elasticsearch:9200"},
				"index":     "vector-logs-%F",
			}
			sinkConfigJSON, err := json.Marshal(sinkConfig)
			Expect(err).ShouldNot(HaveOccurred())

			pipeline := &vectorv1alpha1.VectorPipeline{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "e2e-pipeline",
					Namespace: "default",
				},
				Spec: vectorv1alpha1.VectorPipelineSpec{
					VectorRef: "e2e-vector",
					Sources: map[string]vectorv1alpha1.Source{
						"apache_logs": {
							Type: "file",
							Config: runtime.RawExtension{
								Raw: sourceConfigJSON,
							},
						},
					},
					Transforms: map[string]vectorv1alpha1.Transform{
						"parse_logs": {
							Type: "remap",
							Config: runtime.RawExtension{
								Raw: transformConfigJSON,
							},
						},
					},
					Sinks: map[string]vectorv1alpha1.Sink{
						"elasticsearch_out": {
							Type: "elasticsearch",
							Config: runtime.RawExtension{
								Raw: sinkConfigJSON,
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, pipeline)).Should(Succeed())

			By("Verifying the pipeline status condition")
			pipelineLookupKey := types.NamespacedName{Name: "e2e-pipeline", Namespace: "default"}
			createdPipeline := &vectorv1alpha1.VectorPipeline{}

			Eventually(func() bool {
				err := k8sClient.Get(ctx, pipelineLookupKey, createdPipeline)
				if err != nil {
					return false
				}
				for _, condition := range createdPipeline.Status.Conditions {
					if condition.Type == "VectorRefValid" && condition.Status == metav1.ConditionTrue {
						return true
					}
				}
				return false
			}, timeout, interval).Should(BeTrue())

			By("Verifying the generated ConfigMap")
			configMapLookupKey := types.NamespacedName{Name: "e2e-vector", Namespace: "default"}
			createdConfigMap := &corev1.ConfigMap{}

			Eventually(func() error {
				return k8sClient.Get(ctx, configMapLookupKey, createdConfigMap)
			}, timeout, interval).Should(Succeed())

			By("Validating the YAML configuration")
			config := createdConfigMap.Data["vector.yaml"]
			var parsedConfig map[string]interface{}
			Expect(yaml.Unmarshal([]byte(config), &parsedConfig)).Should(Succeed())

			Expect(parsedConfig).Should(HaveKey("sources"))
			Expect(parsedConfig).Should(HaveKey("transforms"))
			Expect(parsedConfig).Should(HaveKey("sinks"))

			sources := parsedConfig["sources"].(map[string]interface{})
			Expect(sources).Should(HaveKey("apache_logs"))

			transforms := parsedConfig["transforms"].(map[string]interface{})
			Expect(transforms).Should(HaveKey("parse_logs"))

			sinks := parsedConfig["sinks"].(map[string]interface{})
			Expect(sinks).Should(HaveKey("elasticsearch_out"))
		})
	})
})
