package controller

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	vectorv1alpha1 "github.com/zcentric/vector-operator/api/v1alpha1"
)

// MockStatusWriter is a mock implementation of the client.StatusWriter interface
type MockStatusWriter struct {
	mock.Mock
}

func (m *MockStatusWriter) Create(ctx context.Context, obj client.Object, subResource client.Object, opts ...client.SubResourceCreateOption) error {
	args := m.Called(ctx, obj, subResource, opts)
	return args.Error(0)
}

func (m *MockStatusWriter) Update(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
	args := m.Called(ctx, obj, opts)
	return args.Error(0)
}

func (m *MockStatusWriter) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
	args := m.Called(ctx, obj, patch, opts)
	return args.Error(0)
}

// MockSubResourceClient is a mock implementation of the client.SubResourceClient interface
type MockSubResourceClient struct {
	mock.Mock
}

func (m *MockSubResourceClient) Get(ctx context.Context, obj client.Object, subResource client.Object, opts ...client.SubResourceGetOption) error {
	args := m.Called(ctx, obj, subResource, opts)
	return args.Error(0)
}

func (m *MockSubResourceClient) Create(ctx context.Context, obj client.Object, subResource client.Object, opts ...client.SubResourceCreateOption) error {
	args := m.Called(ctx, obj, subResource, opts)
	return args.Error(0)
}

func (m *MockSubResourceClient) Update(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
	args := m.Called(ctx, obj, opts)
	return args.Error(0)
}

func (m *MockSubResourceClient) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
	args := m.Called(ctx, obj, patch, opts)
	return args.Error(0)
}

// MockClient is a mock implementation of the client.Client interface
type MockClient struct {
	mock.Mock
	statusWriter *MockStatusWriter
	subResClient *MockSubResourceClient
}

func (m *MockClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	args := m.Called(ctx, key, obj)
	// Simulate the behavior of Get by setting up the object
	if vp, ok := obj.(*vectorv1alpha1.VectorPipeline); ok {
		vp.Name = "example-vectorpipeline"
		vp.Namespace = "default"
		vp.Spec.VectorRef = "test-vector"
		vp.Generation = 1
		vp.Status.Conditions = []metav1.Condition{}
	}
	if v, ok := obj.(*vectorv1alpha1.Vector); ok {
		v.Name = "test-vector"
		v.Namespace = "default"
		v.Annotations = make(map[string]string)
	}
	return args.Error(0)
}

func (m *MockClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	args := m.Called(ctx, list, opts)
	return args.Error(0)
}

func (m *MockClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	args := m.Called(ctx, obj)
	return args.Error(0)
}

func (m *MockClient) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	args := m.Called(ctx, obj)
	return args.Error(0)
}

func (m *MockClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	args := m.Called(ctx, obj)
	return args.Error(0)
}

func (m *MockClient) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	args := m.Called(ctx, obj, patch)
	return args.Error(0)
}

func (m *MockClient) DeleteAllOf(ctx context.Context, obj client.Object, opts ...client.DeleteAllOfOption) error {
	args := m.Called(ctx, obj)
	return args.Error(0)
}

func (m *MockClient) Status() client.StatusWriter {
	if m.statusWriter == nil {
		m.statusWriter = &MockStatusWriter{}
	}
	return m.statusWriter
}

func (m *MockClient) SubResource(subResource string) client.SubResourceClient {
	if m.subResClient == nil {
		m.subResClient = &MockSubResourceClient{}
	}
	return m.subResClient
}

func (m *MockClient) Scheme() *runtime.Scheme {
	args := m.Called()
	return args.Get(0).(*runtime.Scheme)
}

func (m *MockClient) GroupVersionKindFor(obj runtime.Object) (schema.GroupVersionKind, error) {
	args := m.Called(obj)
	return args.Get(0).(schema.GroupVersionKind), args.Error(1)
}

func (m *MockClient) IsObjectNamespaced(obj runtime.Object) (bool, error) {
	args := m.Called(obj)
	return args.Bool(0), args.Error(1)
}

func (m *MockClient) RESTMapper() meta.RESTMapper {
	args := m.Called()
	return args.Get(0).(meta.RESTMapper)
}

func TestVectorPipelineReconcile(t *testing.T) {
	// Setup the Manager and Controller
	scheme := runtime.NewScheme()
	vectorv1alpha1.AddToScheme(scheme)

	// Create a mock client
	mockClient := &MockClient{
		statusWriter: &MockStatusWriter{},
		subResClient: &MockSubResourceClient{},
	}

	// Setup mock expectations for the main reconciliation flow
	mockClient.On("Get", mock.Anything, types.NamespacedName{Name: "example-vectorpipeline", Namespace: "default"}, &vectorv1alpha1.VectorPipeline{}).Return(nil)
	mockClient.On("List", mock.Anything, &vectorv1alpha1.VectorList{}, mock.Anything).Return(nil)
	mockClient.On("Get", mock.Anything, types.NamespacedName{Name: "test-vector", Namespace: "default"}, &vectorv1alpha1.Vector{}).Return(nil)
	mockClient.On("Update", mock.Anything, mock.AnythingOfType("*v1alpha1.Vector")).Return(nil)
	mockClient.statusWriter.On("Update", mock.Anything, mock.AnythingOfType("*v1alpha1.VectorPipeline"), mock.Anything).Return(nil)
	mockClient.On("List", mock.Anything, &vectorv1alpha1.VectorPipelineList{}, mock.Anything).Return(nil)

	// Create a reconcile request
	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "example-vectorpipeline",
			Namespace: "default",
		},
	}

	// Create a VectorPipelineReconciler
	r := &VectorPipelineReconciler{
		Client: mockClient,
		Scheme: scheme,
	}

	// Reconcile
	result, err := r.Reconcile(context.TODO(), req)

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, reconcile.Result{}, result)

	// Verify that the mock methods were called
	mockClient.AssertExpectations(t)
	mockClient.statusWriter.AssertExpectations(t)
}
