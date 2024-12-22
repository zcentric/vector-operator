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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
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
	Scheme     *runtime.Scheme
	KubeClient kubernetes.Interface
}

const (
	VectorRefCondition   = "VectorRefValid"
	ConfigValidCondition = "ConfigurationValid"
)

//+kubebuilder:rbac:groups=vector.zcentric.com,resources=vectorpipelines,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=vector.zcentric.com,resources=vectorpipelines/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=vector.zcentric.com,resources=vectorpipelines/finalizers,verbs=update
//+kubebuilder:rbac:groups=vector.zcentric.com,resources=vectors,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=pods/log,verbs=get
//+kubebuilder:rbac:groups=vector.zcentric.com,resources=vectors,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=vector.zcentric.com,resources=vectors/status,verbs=get;update;patch

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

// triggerVectorReconciliation triggers a reconciliation of the referenced Vector
func (r *VectorPipelineReconciler) triggerVectorReconciliation(ctx context.Context, vectorRef string, namespace string) error {
	logger := log.FromContext(ctx)

	// Get the Vector instance
	vector := &vectorv1alpha1.Vector{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      vectorRef,
		Namespace: namespace,
	}, vector)
	if err != nil {
		return err
	}

	// Update the Vector's annotation to trigger reconciliation
	if vector.Annotations == nil {
		vector.Annotations = make(map[string]string)
	}
	vector.Annotations["vectorpipeline.zcentric.com/last-update"] = time.Now().Format(time.RFC3339)

	if err := r.Update(ctx, vector); err != nil {
		logger.Error(err, "Failed to update Vector annotations")
		return err
	}

	return nil
}

// updateVectorConfigMap updates the Vector's ConfigMap with the validated configuration
func (r *VectorPipelineReconciler) updateVectorConfigMap(ctx context.Context, vector *vectorv1alpha1.Vector, configYaml string) error {
	logger := log.FromContext(ctx)

	// Get the Vector's ConfigMap
	configMap := &corev1.ConfigMap{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      fmt.Sprintf("vector-config-%s", vector.Name),
		Namespace: vector.Namespace,
	}, configMap)
	if err != nil {
		return fmt.Errorf("failed to get Vector ConfigMap: %w", err)
	}

	// Update the configuration
	configMap.Data["vector.yaml"] = configYaml

	// Update the ConfigMap
	if err := r.Update(ctx, configMap); err != nil {
		return fmt.Errorf("failed to update Vector ConfigMap: %w", err)
	}

	logger.Info("Successfully updated Vector ConfigMap with validated configuration",
		"vector", vector.Name)

	return nil
}

// Reconcile handles the reconciliation loop for VectorPipeline resources
func (r *VectorPipelineReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Clean up old successful validation jobs
	r.cleanupOldValidationJobs(ctx)

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

	// Initialize Status.Conditions if it's nil
	if vectorPipeline.Status.Conditions == nil {
		vectorPipeline.Status.Conditions = []metav1.Condition{}
	}

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
			return ctrl.Result{Requeue: true}, err
		}
	} else {
		logger.Info("Referenced Vector found",
			"vectorRef", vectorPipeline.Spec.VectorRef,
			"pipeline", vectorPipeline.Name)
		condition.Status = metav1.ConditionTrue
		condition.Reason = "VectorFound"
		condition.Message = "Referenced Vector exists"

		// Check if we need to validate the configuration
		validationCondition := meta.FindStatusCondition(vectorPipeline.Status.Conditions, ConfigValidCondition)
		// Initialize ValidatedPipelines if nil
		if vector.Status.ValidatedPipelines == nil {
			vector.Status.ValidatedPipelines = make(map[string]int64)
		}

		// Check if pipeline needs validation
		lastValidatedGeneration := vector.Status.ValidatedPipelines[vectorPipeline.Name]

		// Need validation if:
		// 1. Generation has changed from last validated generation, or
		// 2. No validation condition exists yet
		needsValidation := lastValidatedGeneration != vectorPipeline.Generation ||
			validationCondition == nil

		// Skip validation if we already have a failed validation for this generation
		if validationCondition != nil &&
			validationCondition.Status == metav1.ConditionFalse &&
			validationCondition.ObservedGeneration == vectorPipeline.Generation {
			logger.Info("Skipping validation as it already failed for this generation",
				"pipeline", vectorPipeline.Name,
				"generation", vectorPipeline.Generation)
			return ctrl.Result{}, nil
		}

		logger.Info("Checking validation status",
			"pipeline", vectorPipeline.Name,
			"currentGeneration", vectorPipeline.Generation,
			"lastValidatedGeneration", lastValidatedGeneration,
			"lastValidationStatus", getValidationStatus(validationCondition),
			"observedGeneration", getObservedGeneration(validationCondition),
			"needsValidation", needsValidation)

		if needsValidation {
			// Generate the complete Vector configuration for validation
			configYaml, err := r.generateConfigForValidation(ctx, vector, vectorPipeline)
			if err != nil {
				logger.Error(err, "Failed to generate Vector configuration")
				meta.SetStatusCondition(&vectorPipeline.Status.Conditions, metav1.Condition{
					Type:               ConfigValidCondition,
					Status:             metav1.ConditionFalse,
					Reason:             "ConfigGenerationFailed",
					Message:            fmt.Sprintf("Failed to generate configuration: %v", err),
					ObservedGeneration: vectorPipeline.Generation,
					LastTransitionTime: metav1.NewTime(time.Now()),
				})
				if err := r.Status().Update(ctx, vectorPipeline); err != nil {
					logger.Error(err, "Unable to update VectorPipeline status")
				}
				return ctrl.Result{}, err
			}

			if err := r.validateVectorConfig(ctx, req.Namespace, configYaml, vectorPipeline.Name); err != nil {
				logger.Error(err, "Vector configuration validation failed")
				// Set validation condition
				meta.SetStatusCondition(&vectorPipeline.Status.Conditions, metav1.Condition{
					Type:               ConfigValidCondition,
					Status:             metav1.ConditionFalse,
					Reason:             "ValidationFailed",
					Message:            fmt.Sprintf("Configuration validation failed: %v", err),
					ObservedGeneration: vectorPipeline.Generation,
					LastTransitionTime: metav1.NewTime(time.Now()),
				})
				// Remove from validated pipelines if validation fails
				delete(vector.Status.ValidatedPipelines, vectorPipeline.Name)
				if err := r.Status().Update(ctx, vector); err != nil {
					logger.Error(err, "Failed to update Vector validation status")
					return ctrl.Result{}, err
				}
				// Update status and return
				if err := r.Status().Update(ctx, vectorPipeline); err != nil {
					logger.Error(err, "Unable to update VectorPipeline validation status")
					return ctrl.Result{}, err
				}
				// Don't requeue - wait for user to fix the configuration
				return ctrl.Result{}, nil
			}

			// Now that validation has succeeded, update the ConfigMap
			logger.Info("Validation succeeded, updating Vector ConfigMap",
				"pipeline", vectorPipeline.Name,
				"generation", vectorPipeline.Generation)

			// Get the complete config including all validated pipelines
			completeConfig, err := r.generateConfigForValidation(ctx, vector, vectorPipeline)
			if err != nil {
				logger.Error(err, "Failed to generate complete configuration")
				return ctrl.Result{}, err
			}

			// Update the Vector's ConfigMap with the complete validated configuration
			if err := r.updateVectorConfigMap(ctx, vector, completeConfig); err != nil {
				logger.Error(err, "Failed to update Vector ConfigMap")
				return ctrl.Result{}, err
			}

			// Set successful validation condition
			meta.SetStatusCondition(&vectorPipeline.Status.Conditions, metav1.Condition{
				Type:               ConfigValidCondition,
				Status:             metav1.ConditionTrue,
				Reason:             "ValidationSucceeded",
				Message:            "Vector configuration validation succeeded",
				ObservedGeneration: vectorPipeline.Generation,
				LastTransitionTime: metav1.NewTime(time.Now()),
			})

			// Update Vector status with validated pipeline
			vector.Status.ValidatedPipelines[vectorPipeline.Name] = vectorPipeline.Generation
			if err := r.Status().Update(ctx, vector); err != nil {
				logger.Error(err, "Failed to update Vector validation status")
				return ctrl.Result{}, err
			}

			// Trigger Vector reconciliation to restart pods
			if err := r.triggerVectorReconciliation(ctx, vectorPipeline.Spec.VectorRef, req.Namespace); err != nil {
				logger.Error(err, "Failed to trigger Vector reconciliation")
				return ctrl.Result{}, err
			}

			// Update status and return
			if err := r.Status().Update(ctx, vectorPipeline); err != nil {
				logger.Error(err, "Unable to update VectorPipeline validation status")
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, nil
		}
	}

	// Check if condition has changed before updating
	currentCondition := meta.FindStatusCondition(vectorPipeline.Status.Conditions, VectorRefCondition)
	if currentCondition == nil || currentCondition.Status != condition.Status {
		meta.SetStatusCondition(&vectorPipeline.Status.Conditions, condition)
		if err := r.Status().Update(ctx, vectorPipeline); err != nil {
			logger.Error(err, "Unable to update VectorPipeline status")
			return ctrl.Result{Requeue: true}, err
		}
	}

	// Update pipeline metrics after status update
	r.updatePipelineMetrics(ctx, vectorPipeline.Spec.VectorRef)

	return ctrl.Result{}, nil
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

// getValidationStatus returns the status string from a validation condition
func getValidationStatus(condition *metav1.Condition) string {
	if condition == nil {
		return "None"
	}
	return string(condition.Status)
}

// getObservedGeneration returns the observed generation from a validation condition
func getObservedGeneration(condition *metav1.Condition) int64 {
	if condition == nil {
		return 0
	}
	return condition.ObservedGeneration
}
