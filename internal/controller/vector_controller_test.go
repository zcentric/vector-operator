package controller

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	vectorv1alpha1 "github.com/zcentric/vector-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestVectorController(t *testing.T) {
	// Register the scheme
	s := scheme.Scheme
	vectorv1alpha1.AddToScheme(s)

	// Create a test Vector instance
	vector := &vectorv1alpha1.Vector{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-vector",
			Namespace:  "default",
			Finalizers: []string{vectorFinalizer},
		},
		Spec: vectorv1alpha1.VectorSpec{
			Image: "timberio/vector:latest",
			API: &vectorv1alpha1.VectorAPI{
				Enabled: pointer.Bool(true),
				Address: "0.0.0.0:8686",
			},
		},
	}

	// Create a ServiceAccount
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-vector",
			Namespace: "default",
		},
	}

	// Create a ConfigMap
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-vector-config",
			Namespace: "default",
		},
		Data: map[string]string{
			"vector.yaml": "test: data",
		},
	}

	// Create a fake client with the Vector instance and ServiceAccount
	client := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(vector, sa, cm).
		WithStatusSubresource(&vectorv1alpha1.Vector{}).
		Build()

	// Create the reconciler
	r := &VectorReconciler{
		Client:   client,
		Scheme:   s,
		Recorder: &record.FakeRecorder{},
	}

	// Test the reconciliation
	t.Run("Initial Reconciliation", func(t *testing.T) {
		req := ctrl.Request{
			NamespacedName: types.NamespacedName{
				Name:      "test-vector",
				Namespace: "default",
			},
		}

		// First reconciliation
		result, err := r.Reconcile(context.Background(), req)
		assert.NoError(t, err)
		assert.True(t, result.Requeue)

		// Second reconciliation (after config hash is set)
		result, err = r.Reconcile(context.Background(), req)
		assert.NoError(t, err)

		// Verify DaemonSet was created
		ds := &appsv1.DaemonSet{}
		err = client.Get(context.Background(), req.NamespacedName, ds)
		assert.NoError(t, err)
		assert.Equal(t, "test-vector", ds.Name)
		assert.Equal(t, vector.Spec.Image, ds.Spec.Template.Spec.Containers[0].Image)
	})

	t.Run("Test ConfigMap Creation", func(t *testing.T) {
		// Get the created ConfigMap
		cm := &corev1.ConfigMap{}
		err := client.Get(context.Background(), types.NamespacedName{
			Name:      "test-vector-config",
			Namespace: "default",
		}, cm)
		assert.NoError(t, err)

		// Verify ConfigMap data
		assert.Contains(t, cm.Data, "vector.yaml")
		assert.NotEmpty(t, cm.Data["vector.yaml"])
	})

	t.Run("Test RBAC Resources", func(t *testing.T) {
		// Verify ServiceAccount
		sa := &corev1.ServiceAccount{}
		err := client.Get(context.Background(), types.NamespacedName{
			Name:      "test-vector",
			Namespace: "default",
		}, sa)
		assert.NoError(t, err)

		// Verify ClusterRole
		cr := &rbacv1.ClusterRole{}
		err = client.Get(context.Background(), types.NamespacedName{
			Name: "vector-default-test-vector",
		}, cr)
		assert.NoError(t, err)
		assert.Contains(t, cr.Rules[0].Resources, "pods")
		assert.Contains(t, cr.Rules[0].Resources, "nodes")

		// Verify ClusterRoleBinding
		crb := &rbacv1.ClusterRoleBinding{}
		err = client.Get(context.Background(), types.NamespacedName{
			Name: "vector-default-test-vector",
		}, crb)
		assert.NoError(t, err)
		assert.Equal(t, cr.Name, crb.RoleRef.Name)
	})

	t.Run("Test Status Updates", func(t *testing.T) {
		// Get a fresh copy of the Vector instance
		vector := &vectorv1alpha1.Vector{}
		err := client.Get(context.Background(), types.NamespacedName{
			Name:      "test-vector",
			Namespace: "default",
		}, vector)
		assert.NoError(t, err)

		// Create a DaemonSet with ready pods
		ds := &appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-vector",
				Namespace: "default",
			},
			Status: appsv1.DaemonSetStatus{
				NumberReady: 1,
			},
		}

		err = r.updateVectorStatus(context.Background(), vector, ds)
		assert.NoError(t, err)

		// Verify Vector status
		updatedVector := &vectorv1alpha1.Vector{}
		err = client.Get(context.Background(), types.NamespacedName{
			Name:      "test-vector",
			Namespace: "default",
		}, updatedVector)
		assert.NoError(t, err)

		condition := meta.FindStatusCondition(updatedVector.Status.Conditions, "Available")
		assert.NotNil(t, condition)
		assert.Equal(t, metav1.ConditionTrue, condition.Status)
	})

	t.Run("Test Finalizer", func(t *testing.T) {
		// Create a new Vector instance with deletion timestamp
		vectorToDelete := &vectorv1alpha1.Vector{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "test-vector-delete",
				Namespace:         "default",
				Finalizers:        []string{vectorFinalizer},
				DeletionTimestamp: &metav1.Time{Time: metav1.Now().Time},
			},
			Spec: vectorv1alpha1.VectorSpec{
				Image: "timberio/vector:latest",
			},
		}

		// Create the resources that should be cleaned up
		dsToDelete := &appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-vector-delete",
				Namespace: "default",
			},
		}
		ctrl.SetControllerReference(vectorToDelete, dsToDelete, s)

		cmToDelete := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-vector-delete-config",
				Namespace: "default",
			},
		}
		ctrl.SetControllerReference(vectorToDelete, cmToDelete, s)

		saToDelete := &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-vector-delete",
				Namespace: "default",
			},
		}
		ctrl.SetControllerReference(vectorToDelete, saToDelete, s)

		// Create a new client with these resources
		clientWithDeletion := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(vectorToDelete, dsToDelete, cmToDelete, saToDelete).
			WithStatusSubresource(&vectorv1alpha1.Vector{}).
			Build()

		// Create a new reconciler with the new client
		rWithDeletion := &VectorReconciler{
			Client:   clientWithDeletion,
			Scheme:   s,
			Recorder: &record.FakeRecorder{},
		}

		// Reconcile
		req := ctrl.Request{
			NamespacedName: types.NamespacedName{
				Name:      "test-vector-delete",
				Namespace: "default",
			},
		}

		// First reconciliation to remove finalizer
		_, err := rWithDeletion.Reconcile(context.Background(), req)
		assert.NoError(t, err)

		// Verify resources are cleaned up
		ds := &appsv1.DaemonSet{}
		err = clientWithDeletion.Get(context.Background(), req.NamespacedName, ds)
		assert.True(t, errors.IsNotFound(err))

		cm := &corev1.ConfigMap{}
		err = clientWithDeletion.Get(context.Background(), types.NamespacedName{
			Name:      "test-vector-delete-config",
			Namespace: "default",
		}, cm)
		assert.True(t, errors.IsNotFound(err))

		sa := &corev1.ServiceAccount{}
		err = clientWithDeletion.Get(context.Background(), req.NamespacedName, sa)
		assert.True(t, errors.IsNotFound(err))

		// Verify Vector instance is deleted
		vector := &vectorv1alpha1.Vector{}
		err = clientWithDeletion.Get(context.Background(), req.NamespacedName, vector)
		assert.True(t, errors.IsNotFound(err))

		// Second reconciliation should handle the not found case
		_, err = rWithDeletion.Reconcile(context.Background(), req)
		assert.NoError(t, err)
	})

	t.Run("Test Config Hash Calculation", func(t *testing.T) {
		cm := &corev1.ConfigMap{
			Data: map[string]string{
				"vector.yaml": "test: data",
			},
		}
		hash := calculateConfigHash(cm)
		assert.NotEmpty(t, hash)

		// Test hash changes with different data
		cm.Data["vector.yaml"] = "different: data"
		newHash := calculateConfigHash(cm)
		assert.NotEqual(t, hash, newHash)
	})

	t.Run("Test Labels", func(t *testing.T) {
		labels := labelsForVector("test-vector")
		assert.Equal(t, "Vector", labels["app.kubernetes.io/name"])
		assert.Equal(t, "test-vector", labels["app.kubernetes.io/instance"])
		assert.Equal(t, "vector-operator", labels["app.kubernetes.io/managed-by"])
	})
}

func TestVectorSetupWithManager(t *testing.T) {
	// Register the scheme
	s := scheme.Scheme
	vectorv1alpha1.AddToScheme(s)

	// Create a fake client
	client := fake.NewClientBuilder().
		WithScheme(s).
		Build()

	// Create the reconciler
	r := &VectorReconciler{
		Client:   client,
		Scheme:   s,
		Recorder: &record.FakeRecorder{},
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
