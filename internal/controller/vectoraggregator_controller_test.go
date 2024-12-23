package controller

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	vectorv1alpha1 "github.com/zcentric/vector-operator/api/v1alpha1"
	"github.com/zcentric/vector-operator/internal/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestVectorAggregatorController(t *testing.T) {
	// Register the scheme
	s := scheme.Scheme
	vectorv1alpha1.AddToScheme(s)

	// Create a test VectorAggregator instance
	vectorAggregator := &vectorv1alpha1.VectorAggregator{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-aggregator",
			Namespace: "default",
		},
		Spec: vectorv1alpha1.VectorAggregatorSpec{
			Image:    "timberio/vector:latest",
			Replicas: 1,
			DataDir:  "/vector-data",
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("100m"),
					corev1.ResourceMemory: resource.MustParse("128Mi"),
				},
			},
		},
	}

	// Create a fake client
	client := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(vectorAggregator).
		Build()

	// Create the reconciler
	r := &VectorAggregatorReconciler{
		Client: client,
		Scheme: s,
	}

	// Test the reconciliation
	t.Run("Initial Reconciliation", func(t *testing.T) {
		req := ctrl.Request{
			NamespacedName: types.NamespacedName{
				Name:      "test-aggregator",
				Namespace: "default",
			},
		}

		_, err := r.Reconcile(context.Background(), req)
		assert.NoError(t, err)

		// Verify deployment was created
		deployment := &appsv1.Deployment{}
		err = client.Get(context.Background(), req.NamespacedName, deployment)
		assert.NoError(t, err)
		assert.Equal(t, "test-aggregator", deployment.Name)
		assert.Equal(t, int32(1), *deployment.Spec.Replicas)
	})

	t.Run("Test Labels", func(t *testing.T) {
		labels := labelsForVectorAggregator("test-aggregator")
		assert.Equal(t, "vector", labels["app.kubernetes.io/name"])
		assert.Equal(t, "test-aggregator", labels["app.kubernetes.io/instance"])
		assert.Equal(t, "aggregator", labels["app.kubernetes.io/component"])
		assert.Equal(t, "vector-operator", labels["app.kubernetes.io/part-of"])
	})

	t.Run("Test NeedsUpdate", func(t *testing.T) {
		deployment := r.deploymentForVectorAggregator(vectorAggregator)

		// Should not need update when nothing changed
		assert.False(t, needsUpdate(deployment, vectorAggregator))

		// Should need update when replicas changed
		newReplicas := int32(2)
		vectorAggregator.Spec.Replicas = newReplicas
		assert.True(t, needsUpdate(deployment, vectorAggregator))

		// Should need update when image changed
		vectorAggregator.Spec.Image = "timberio/vector:0.1.0"
		assert.True(t, needsUpdate(deployment, vectorAggregator))

		// Should need update when resources changed
		vectorAggregator.Spec.Resources.Requests[corev1.ResourceCPU] = resource.MustParse("200m")
		assert.True(t, needsUpdate(deployment, vectorAggregator))
	})

	t.Run("Test Deployment Configuration", func(t *testing.T) {
		deployment := r.deploymentForVectorAggregator(vectorAggregator)

		// Test basic deployment configuration
		assert.Equal(t, vectorAggregator.Spec.Image, deployment.Spec.Template.Spec.Containers[0].Image)
		assert.Equal(t, "vector", deployment.Spec.Template.Spec.Containers[0].Name)

		// Test volume configuration
		assert.Equal(t, 1, len(deployment.Spec.Template.Spec.Volumes))
		assert.Equal(t, "data", deployment.Spec.Template.Spec.Volumes[0].Name)

		// Test volume mounts
		container := deployment.Spec.Template.Spec.Containers[0]
		assert.Equal(t, 1, len(container.VolumeMounts))
		assert.Equal(t, "data", container.VolumeMounts[0].Name)
		assert.Equal(t, vectorAggregator.Spec.DataDir, container.VolumeMounts[0].MountPath)

		// Test environment variables
		expectedEnv := utils.MergeEnvVars(utils.GetVectorEnvVars(), vectorAggregator.Spec.Env)
		assert.Equal(t, expectedEnv, container.Env)
	})
}

func TestAggregatorSetupWithManager(t *testing.T) {
	// Register the scheme
	s := scheme.Scheme
	vectorv1alpha1.AddToScheme(s)

	// Create a fake client
	client := fake.NewClientBuilder().
		WithScheme(s).
		Build()

	// Create the reconciler
	r := &VectorAggregatorReconciler{
		Client: client,
		Scheme: s,
	}

	// Create a new manager
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: s,
	})
	assert.NoError(t, err)

	// Test SetupWithManager
	err = r.SetupWithManager(mgr)
	assert.NoError(t, err)
}
