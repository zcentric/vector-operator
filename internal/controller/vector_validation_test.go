package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	vectorv1alpha1 "github.com/zcentric/vector-operator/api/v1alpha1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/yaml"
)

func TestVectorValidation(t *testing.T) {
	// Register the scheme
	s := scheme.Scheme
	vectorv1alpha1.AddToScheme(s)

	// Create a test Vector instance
	vector := &vectorv1alpha1.Vector{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-vector",
			Namespace: "default",
		},
		Spec: vectorv1alpha1.VectorSpec{
			Image: "timberio/vector:latest",
			API: &vectorv1alpha1.VectorAPI{
				Enabled: pointer.Bool(true),
				Address: "0.0.0.0:8686",
			},
		},
	}

	// Create test pipeline with sources, transforms, and sinks
	sources := map[string]interface{}{
		"test_source": map[string]interface{}{
			"type":    "file",
			"include": []string{"/var/log/*.log"},
		},
	}
	sourcesJSON, err := json.Marshal(sources)
	assert.NoError(t, err)

	transforms := map[string]interface{}{
		"test_transform": map[string]interface{}{
			"type":   "remap",
			"inputs": []string{"test_source"},
			"source": ". = parse_json!(.message)",
		},
	}
	transformsJSON, err := json.Marshal(transforms)
	assert.NoError(t, err)

	sinks := map[string]interface{}{
		"test_sink": map[string]interface{}{
			"type":   "console",
			"inputs": []string{"test_transform"},
			"encoding": map[string]interface{}{
				"codec": "json",
			},
		},
	}
	sinksJSON, err := json.Marshal(sinks)
	assert.NoError(t, err)

	pipeline := &vectorv1alpha1.VectorPipeline{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pipeline",
			Namespace: "default",
		},
		Spec: vectorv1alpha1.VectorPipelineSpec{
			VectorRef: "test-vector",
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
		Status: vectorv1alpha1.VectorPipelineStatus{
			Conditions: []metav1.Condition{
				{
					Type:   ConfigValidCondition,
					Status: metav1.ConditionTrue,
				},
			},
		},
	}

	// Create the Vector's ConfigMap
	vectorConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-config", vector.Name),
			Namespace: vector.Namespace,
		},
		Data: map[string]string{
			"vector.yaml": "initial: config",
		},
	}

	// Create a fake client
	client := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(vector, pipeline, vectorConfigMap).
		WithStatusSubresource(&vectorv1alpha1.Vector{}, &vectorv1alpha1.VectorPipeline{}).
		Build()

	// Create the reconciler
	r := &VectorPipelineReconciler{
		Client: client,
		Scheme: s,
	}

	t.Run("Test Config Generation", func(t *testing.T) {
		config, err := r.generateConfigForValidation(context.Background(), vector, pipeline)
		assert.NoError(t, err)
		assert.NotEmpty(t, config)

		// Parse the generated config to verify its structure
		var configMap map[string]interface{}
		err = yaml.Unmarshal([]byte(config), &configMap)
		assert.NoError(t, err)

		// Verify required sections exist
		assert.Contains(t, configMap, "data_dir")
		assert.Contains(t, configMap, "api")
		assert.Contains(t, configMap, "sources")
		assert.Contains(t, configMap, "transforms")
		assert.Contains(t, configMap, "sinks")

		// Verify API configuration
		apiConfig := configMap["api"].(map[string]interface{})
		assert.Equal(t, true, apiConfig["enabled"])
		assert.Equal(t, "0.0.0.0:8686", apiConfig["address"])

		// Verify pipeline components
		sourcesConfig := configMap["sources"].(map[string]interface{})
		assert.Contains(t, sourcesConfig, "test_source")
		assert.Equal(t, "file", sourcesConfig["test_source"].(map[string]interface{})["type"])

		transformsConfig := configMap["transforms"].(map[string]interface{})
		assert.Contains(t, transformsConfig, "test_transform")
		assert.Equal(t, "remap", transformsConfig["test_transform"].(map[string]interface{})["type"])

		sinksConfig := configMap["sinks"].(map[string]interface{})
		assert.Contains(t, sinksConfig, "test_sink")
		assert.Equal(t, "console", sinksConfig["test_sink"].(map[string]interface{})["type"])
	})

	t.Run("Test Validation Job Creation", func(t *testing.T) {
		// Create a ConfigMap for validation with the correct name
		configMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      getValidationConfigMapName(pipeline.Name),
				Namespace: "default",
			},
			Data: map[string]string{
				"vector.yaml": "test: data",
			},
		}
		err := client.Create(context.Background(), configMap)
		assert.NoError(t, err)

		// Create a channel to receive the job status
		jobDone := make(chan error)

		// Start validation in a goroutine
		go func() {
			err := r.validateVectorConfig(context.Background(), "default", configMap.Name, pipeline.Name, pipeline)
			jobDone <- err
		}()

		// Wait a short time for the job to be created
		time.Sleep(time.Second)

		// Verify job was created with correct configuration
		job := &batchv1.Job{}
		err = client.Get(context.Background(), types.NamespacedName{
			Name:      "vector-validate-test-pipeline",
			Namespace: "default",
		}, job)
		assert.NoError(t, err)

		// Verify job configuration
		assert.Equal(t, "vector-validate-test-pipeline", job.Name)
		assert.Equal(t, "default", job.Namespace)
		assert.Equal(t, int32(0), *job.Spec.BackoffLimit)
		assert.Equal(t, "timberio/vector:latest-alpine", job.Spec.Template.Spec.Containers[0].Image)
		assert.Equal(t, "vector-validate", job.Spec.Template.Spec.Containers[0].Name)

		// Verify volume mounts
		assert.Equal(t, 1, len(job.Spec.Template.Spec.Containers[0].VolumeMounts))
		assert.Equal(t, "config-to-validate", job.Spec.Template.Spec.Containers[0].VolumeMounts[0].Name)
		assert.Equal(t, "/etc/vector", job.Spec.Template.Spec.Containers[0].VolumeMounts[0].MountPath)

		// Verify volumes
		assert.Equal(t, 1, len(job.Spec.Template.Spec.Volumes))
		assert.Equal(t, "config-to-validate", job.Spec.Template.Spec.Volumes[0].Name)
		assert.Equal(t, configMap.Name, job.Spec.Template.Spec.Volumes[0].ConfigMap.LocalObjectReference.Name)

		// Update job status to complete validation
		job.Status.Succeeded = 1
		err = client.Status().Update(context.Background(), job)
		assert.NoError(t, err)

		// Wait for validation to complete or timeout
		select {
		case err := <-jobDone:
			assert.NoError(t, err)
		case <-time.After(5 * time.Second):
			t.Fatal("Validation job timed out")
		}

		// Verify that the Vector's ConfigMap was updated
		updatedConfigMap := &corev1.ConfigMap{}
		err = client.Get(context.Background(), types.NamespacedName{
			Name:      fmt.Sprintf("%s-config", vector.Name),
			Namespace: vector.Namespace,
		}, updatedConfigMap)
		assert.NoError(t, err)
		assert.Equal(t, configMap.Data, updatedConfigMap.Data)
	})
}
