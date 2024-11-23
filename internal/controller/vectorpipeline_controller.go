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
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	vectorv1alpha1 "github.com/zcentric/vector-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	vectorCount = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "vector_operator_vector_count",
			Help: "Number of Vector CRs",
		},
	)

	pipelineSuccessCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "vector_operator_pipeline_success_count",
			Help: "Number of successful pipelines per Vector CR",
		},
		[]string{"vector_ref"},
	)

	pipelineFailureCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "vector_operator_pipeline_failure_count",
			Help: "Number of failed pipelines per Vector CR",
		},
		[]string{"vector_ref"},
	)
)

func init() {
	// Register metrics with the global prometheus registry
	metrics.Registry.MustRegister(vectorCount)
	metrics.Registry.MustRegister(pipelineSuccessCount)
	metrics.Registry.MustRegister(pipelineFailureCount)
}

// VectorPipelineReconciler reconciles a VectorPipeline object
type VectorPipelineReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

const (
	VectorRefCondition = "VectorRefValid"
)

//+kubebuilder:rbac:groups=vectorpipeline.zcentric.com,resources=vectorpipelines,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=vectorpipeline.zcentric.com,resources=vectorpipelines/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=vectorpipeline.zcentric.com,resources=vectorpipelines/finalizers,verbs=update
//+kubebuilder:rbac:groups=vector.zcentric.com,resources=vectors,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch

// updateVectorMetrics updates the Vector CR count metric
func (r *VectorPipelineReconciler) updateVectorMetrics(ctx context.Context) {
	var vectorList vectorv1alpha1.VectorList
	if err := r.List(ctx, &vectorList); err != nil {
		return
	}
	vectorCount.Set(float64(len(vectorList.Items)))
}

// updatePipelineMetrics updates the pipeline success and failure metrics for a given Vector CR
func (r *VectorPipelineReconciler) updatePipelineMetrics(ctx context.Context, vectorRef string) {
	var pipelineList vectorv1alpha1.VectorPipelineList
	if err := r.List(ctx, &pipelineList); err != nil {
		return
	}

	successCount := 0
	failureCount := 0

	for _, pipeline := range pipelineList.Items {
		if pipeline.Spec.VectorRef != vectorRef {
			continue
		}

		// Check if the pipeline has the VectorRefValid condition
		condition := meta.FindStatusCondition(pipeline.Status.Conditions, VectorRefCondition)
		if condition != nil && condition.Status == metav1.ConditionTrue {
			successCount++
		} else {
			failureCount++
		}
	}

	pipelineSuccessCount.WithLabelValues(vectorRef).Set(float64(successCount))
	pipelineFailureCount.WithLabelValues(vectorRef).Set(float64(failureCount))
}

// Reconcile handles the reconciliation loop for VectorPipeline resources
func (r *VectorPipelineReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Update Vector metrics
	r.updateVectorMetrics(ctx)

	// Fetch the VectorPipeline instance
	vectorPipeline := &vectorv1alpha1.VectorPipeline{}
	err := r.Get(ctx, req.NamespacedName, vectorPipeline)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Info("VectorPipeline resource not found. Ignoring since object must be deleted", "name", req.Name)
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get VectorPipeline")
		return ctrl.Result{}, err
	}

	logger.Info("Processing VectorPipeline",
		"name", vectorPipeline.Name,
		"namespace", vectorPipeline.Namespace,
		"vectorRef", vectorPipeline.Spec.VectorRef)

	// Check if the referenced Vector exists
	vector := &vectorv1alpha1.Vector{}
	err = r.Get(ctx, types.NamespacedName{
		Name:      vectorPipeline.Spec.VectorRef,
		Namespace: req.Namespace,
	}, vector)

	// Update the status condition based on Vector existence
	condition := metav1.Condition{
		Type:               VectorRefCondition,
		Status:             metav1.ConditionUnknown,
		ObservedGeneration: vectorPipeline.Generation,
		LastTransitionTime: metav1.NewTime(time.Now()),
	}

	if err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Referenced Vector not found",
				"vectorRef", vectorPipeline.Spec.VectorRef,
				"pipeline", vectorPipeline.Name)
			condition.Status = metav1.ConditionFalse
			condition.Reason = "VectorNotFound"
			condition.Message = fmt.Sprintf("Referenced Vector '%s' not found", vectorPipeline.Spec.VectorRef)
		} else {
			condition.Status = metav1.ConditionUnknown
			condition.Reason = "ErrorCheckingVector"
			condition.Message = "Error occurred while checking Vector reference"
		}
	} else {
		logger.Info("Referenced Vector found",
			"vectorRef", vectorPipeline.Spec.VectorRef,
			"pipeline", vectorPipeline.Name)
		condition.Status = metav1.ConditionTrue
		condition.Reason = "VectorFound"
		condition.Message = "Referenced Vector exists"

		// Update Vector's pipeline components
		if err := r.updateVectorPipelineComponents(ctx, vectorPipeline); err != nil {
			if errors.IsConflict(err) {
				// If there's a conflict, requeue the request
				logger.Info("Conflict detected while updating Vector, requeueing")
				return ctrl.Result{Requeue: true}, nil
			}
			logger.Error(err, "Failed to update Vector pipeline components")
			return ctrl.Result{}, err
		}
	}

	// Update the status
	meta.SetStatusCondition(&vectorPipeline.Status.Conditions, condition)
	if err := r.Status().Update(ctx, vectorPipeline); err != nil {
		logger.Error(err, "Unable to update VectorPipeline status")
		return ctrl.Result{}, err
	}

	// Update pipeline metrics after status update
	r.updatePipelineMetrics(ctx, vectorPipeline.Spec.VectorRef)

	return ctrl.Result{}, nil
}

// findRelatedPipelines finds all VectorPipelines that reference the same Vector
func (r *VectorPipelineReconciler) findRelatedPipelines(ctx context.Context, pipeline *vectorv1alpha1.VectorPipeline) ([]vectorv1alpha1.VectorPipeline, error) {
	logger := log.FromContext(ctx)

	var pipelineList vectorv1alpha1.VectorPipelineList
	if err := r.List(ctx, &pipelineList, client.InNamespace(pipeline.Namespace)); err != nil {
		logger.Error(err, "Failed to list VectorPipelines")
		return nil, err
	}

	var relatedPipelines []vectorv1alpha1.VectorPipeline
	for _, p := range pipelineList.Items {
		if p.Spec.VectorRef == pipeline.Spec.VectorRef {
			relatedPipelines = append(relatedPipelines, p)
		}
	}

	logger.Info("Found related pipelines",
		"vectorRef", pipeline.Spec.VectorRef,
		"count", len(relatedPipelines),
		"pipelines", getPipelineNames(relatedPipelines))

	return relatedPipelines, nil
}

// getPipelineNames returns a slice of pipeline names for logging
func getPipelineNames(pipelines []vectorv1alpha1.VectorPipeline) []string {
	names := make([]string, len(pipelines))
	for i, p := range pipelines {
		names[i] = p.Name
	}
	return names
}

// updateVectorPipelineComponents updates the Vector CR with pipeline components
func (r *VectorPipelineReconciler) updateVectorPipelineComponents(ctx context.Context, pipeline *vectorv1alpha1.VectorPipeline) error {
	logger := log.FromContext(ctx)

	// Find all pipelines that reference the same Vector
	relatedPipelines, err := r.findRelatedPipelines(ctx, pipeline)
	if err != nil {
		return fmt.Errorf("failed to find related pipelines: %w", err)
	}

	// Get the referenced Vector
	vector := &vectorv1alpha1.Vector{}
	err = r.Get(ctx, types.NamespacedName{
		Name:      pipeline.Spec.VectorRef,
		Namespace: pipeline.Namespace,
	}, vector)
	if err != nil {
		return fmt.Errorf("failed to get referenced Vector: %w", err)
	}

	// Create a copy of the Vector to update
	updatedVector := vector.DeepCopy()

	// Initialize maps if they don't exist
	if updatedVector.Spec.Sources == nil {
		updatedVector.Spec.Sources = make(map[string]vectorv1alpha1.Source)
	}
	if updatedVector.Spec.Transforms == nil {
		updatedVector.Spec.Transforms = make(map[string]vectorv1alpha1.Transform)
	}
	if updatedVector.Spec.Sinks == nil {
		updatedVector.Spec.Sinks = make(map[string]vectorv1alpha1.Sink)
	}

	// Merge configurations from all related pipelines
	for _, p := range relatedPipelines {
		// Merge sources
		for name, source := range p.Spec.Sources {
			logger.Info("Adding source", "name", name, "type", source.Type)
			updatedVector.Spec.Sources[name] = source
		}

		// Merge transforms
		for name, transform := range p.Spec.Transforms {
			logger.Info("Adding transform", "name", name, "type", transform.Type)
			updatedVector.Spec.Transforms[name] = transform
		}

		// Merge sinks
		for name, sink := range p.Spec.Sinks {
			logger.Info("Adding sink", "name", name, "type", sink.Type)
			updatedVector.Spec.Sinks[name] = sink
		}
	}

	// Update the Vector CR
	if err := r.Update(ctx, updatedVector); err != nil {
		return fmt.Errorf("failed to update Vector: %w", err)
	}

	return nil
}

// enqueueRequestsForVector returns reconcile requests for VectorPipelines that reference the Vector
func (r *VectorPipelineReconciler) enqueueRequestsForVector(ctx context.Context, obj client.Object) []reconcile.Request {
	vector := obj.(*vectorv1alpha1.Vector)
	var pipelineList vectorv1alpha1.VectorPipelineList
	if err := r.List(ctx, &pipelineList, client.InNamespace(vector.Namespace)); err != nil {
		return nil
	}

	var requests []reconcile.Request
	for _, pipeline := range pipelineList.Items {
		if pipeline.Spec.VectorRef == vector.Name {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      pipeline.Name,
					Namespace: pipeline.Namespace,
				},
			})
		}
	}
	return requests
}

// SetupWithManager sets up the controller with the Manager.
func (r *VectorPipelineReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&vectorv1alpha1.VectorPipeline{}).
		Watches(&vectorv1alpha1.Vector{},
			handler.EnqueueRequestsFromMapFunc(r.enqueueRequestsForVector)).
		Complete(r)
}
