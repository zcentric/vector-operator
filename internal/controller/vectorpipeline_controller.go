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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	vectorv1alpha1 "github.com/zcentric/vector-operator/api/v1alpha1"
	"github.com/zcentric/vector-operator/internal/metrics"
)

// VectorPipelineReconciler reconciles a VectorPipeline object
type VectorPipelineReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	KubeClient *kubernetes.Clientset
	// Add fields for cleanup management
	cleanupCancel context.CancelFunc
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

// Reconcile handles the reconciliation loop for VectorPipeline resources
func (r *VectorPipelineReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	startTime := time.Now()
	defer func() {
		metrics.ReconciliationDuration.WithLabelValues("pipeline", req.Namespace).Observe(time.Since(startTime).Seconds())
		metrics.ReconciliationCount.WithLabelValues("pipeline", "total", req.Namespace).Inc()
	}()

	// Fetch the VectorPipeline instance first
	vectorPipeline := &vectorv1alpha1.VectorPipeline{}
	err := r.Get(ctx, req.NamespacedName, vectorPipeline)
	if err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Check if we already have a failed validation for this generation
	validationCondition := meta.FindStatusCondition(vectorPipeline.Status.Conditions, ConfigValidCondition)
	if validationCondition != nil &&
		validationCondition.Status == metav1.ConditionFalse &&
		validationCondition.ObservedGeneration == vectorPipeline.Generation {
		logger.Info("Skipping reconciliation as validation already failed for this generation",
			"pipeline", vectorPipeline.Name,
			"generation", vectorPipeline.Generation)
		return ctrl.Result{}, nil
	}

	// Get the operator start time from an annotation on the Vector resource
	startTimeAnnotation := "vector.zcentric.com/operator-start-time"

	// Clean up old successful validation jobs
	r.cleanupOldValidationJobs(ctx)

	// Update Vector metrics
	r.updateVectorMetrics(ctx)

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

	// Check if this is the first reconciliation after operator startup
	if vector.Annotations == nil || vector.Annotations[startTimeAnnotation] == "" {
		// This is the first reconciliation after operator startup
		if vector.Annotations == nil {
			vector.Annotations = make(map[string]string)
		}
		vector.Annotations[startTimeAnnotation] = time.Now().Format(time.RFC3339)
		if err := r.Update(ctx, vector); err != nil {
			logger.Error(err, "Failed to update Vector start time annotation")
			return ctrl.Result{}, err
		}
		logger.Info("Skipping initial validation on operator startup",
			"pipeline", vectorPipeline.Name,
			"vector", vector.Name)
		// Skip validation on startup
		return ctrl.Result{}, nil
	}

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
		// 2. No validation condition exists yet, or
		// 3. Generation has changed since last validation
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
			// Update status to indicate validation is in progress
			meta.SetStatusCondition(&vectorPipeline.Status.Conditions, metav1.Condition{
				Type:               ConfigValidCondition,
				Status:             metav1.ConditionUnknown,
				Reason:             "ValidationInProgress",
				Message:            "Vector configuration validation is in progress",
				ObservedGeneration: vectorPipeline.Generation,
				LastTransitionTime: metav1.NewTime(time.Now()),
			})
			vectorPipeline.Status.ValidationStatus = "InProgress"
			vectorPipeline.Status.LastValidationTime = &metav1.Time{Time: time.Now()}
			if err := r.updateVectorPipelineStatus(ctx, vectorPipeline); err != nil {
				logger.Error(err, "Unable to update VectorPipeline status")
				return ctrl.Result{}, err
			}

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
				vectorPipeline.Status.ValidationStatus = "Failed"
				vectorPipeline.Status.LastValidationError = fmt.Sprintf("Failed to generate configuration: %v", err)
				if err := r.updateVectorPipelineStatus(ctx, vectorPipeline); err != nil {
					logger.Error(err, "Unable to update VectorPipeline status")
				}
				return ctrl.Result{}, err
			}

			// Create or update the validation ConfigMap
			validationCM := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      getValidationConfigMapName(vectorPipeline.Name),
					Namespace: req.Namespace,
				},
				Data: map[string]string{
					"vector.yaml": configYaml,
				},
			}

			// Create or update the validation ConfigMap
			if err := ctrl.SetControllerReference(vectorPipeline, validationCM, r.Scheme); err != nil {
				return ctrl.Result{}, err
			}

			err = r.Create(ctx, validationCM)
			if errors.IsAlreadyExists(err) {
				if err := r.Update(ctx, validationCM); err != nil {
					logger.Error(err, "Failed to update validation ConfigMap")
					return ctrl.Result{}, err
				}
			} else if err != nil {
				logger.Error(err, "Failed to create validation ConfigMap")
				return ctrl.Result{}, err
			}

			// Run validation first
			if err := r.validateVectorConfig(ctx, req.Namespace, validationCM.Name, vectorPipeline.Name, vectorPipeline); err != nil {
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

				// Update Vector's pipeline validation status
				if vector.Status.PipelineValidationStatus == nil {
					vector.Status.PipelineValidationStatus = make(map[string]vectorv1alpha1.PipelineValidation)
				}
				vector.Status.PipelineValidationStatus[vectorPipeline.Name] = vectorv1alpha1.PipelineValidation{
					Status:        "Failed",
					Message:       fmt.Sprintf("Validation failed: %v", err),
					LastValidated: metav1.NewTime(time.Now()),
					Generation:    vectorPipeline.Generation,
				}
				// Remove from validated pipelines if validation fails
				delete(vector.Status.ValidatedPipelines, vectorPipeline.Name)
				if err := r.Status().Update(ctx, vector); err != nil {
					logger.Error(err, "Failed to update Vector validation status")
					return ctrl.Result{}, err
				}
				// Update status and return
				if err := r.updateVectorPipelineStatus(ctx, vectorPipeline); err != nil {
					logger.Error(err, "Unable to update VectorPipeline validation status")
					return ctrl.Result{}, err
				}
				// Don't requeue - wait for user to fix the configuration
				return ctrl.Result{}, nil
			}

			// After successful validation
			// Update Vector's pipeline validation status
			if vector.Status.PipelineValidationStatus == nil {
				vector.Status.PipelineValidationStatus = make(map[string]vectorv1alpha1.PipelineValidation)
			}
			vector.Status.PipelineValidationStatus[vectorPipeline.Name] = vectorv1alpha1.PipelineValidation{
				Status:        "Validated",
				Message:       "Configuration validation succeeded",
				LastValidated: metav1.NewTime(time.Now()),
				Generation:    vectorPipeline.Generation,
			}
			vector.Status.ValidatedPipelines[vectorPipeline.Name] = vectorPipeline.Generation

			// Update Vector status before proceeding
			if err := r.Status().Update(ctx, vector); err != nil {
				logger.Error(err, "Failed to update Vector validation status")
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
			vectorPipeline.Status.ValidationStatus = "Validated"
			vectorPipeline.Status.LastValidatedGeneration = vectorPipeline.Generation
			vectorPipeline.Status.LastValidationError = ""

			// Update status and return
			if err := r.updateVectorPipelineStatus(ctx, vectorPipeline); err != nil {
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
		if err := r.updateVectorPipelineStatus(ctx, vectorPipeline); err != nil {
			logger.Error(err, "Unable to update VectorPipeline status")
			return ctrl.Result{Requeue: true}, err
		}
	}

	// Update pipeline metrics after status update
	r.updatePipelineMetrics(ctx, vectorPipeline.Spec.VectorRef)

	// Update pipeline metrics
	var pipelineList vectorv1alpha1.VectorPipelineList
	if err := r.List(ctx, &pipelineList, client.InNamespace(req.Namespace)); err == nil {
		validatedCount := 0
		failedCount := 0
		pendingCount := 0
		pipelinesByVector := make(map[string]int)

		for _, p := range pipelineList.Items {
			pipelinesByVector[p.Spec.VectorRef]++

			if condition := meta.FindStatusCondition(p.Status.Conditions, ConfigValidCondition); condition != nil {
				switch condition.Status {
				case metav1.ConditionTrue:
					validatedCount++
				case metav1.ConditionFalse:
					failedCount++
				default:
					pendingCount++
				}
			} else {
				pendingCount++
			}
		}

		metrics.PipelineCount.WithLabelValues("validated", req.Namespace).Set(float64(validatedCount))
		metrics.PipelineCount.WithLabelValues("failed", req.Namespace).Set(float64(failedCount))
		metrics.PipelineCount.WithLabelValues("pending", req.Namespace).Set(float64(pendingCount))

		for vectorName, count := range pipelinesByVector {
			metrics.PipelinesByVector.WithLabelValues(vectorName, req.Namespace).Set(float64(count))
		}
	}

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
	// Add the runnable to the manager
	if err := mgr.Add(r); err != nil {
		return fmt.Errorf("failed to add cleanup routine to manager: %w", err)
	}

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

// updateVectorMetrics updates the Vector CR count metric
func (r *VectorPipelineReconciler) updateVectorMetrics(ctx context.Context) {
	var vectorList vectorv1alpha1.VectorList
	if err := r.List(ctx, &vectorList); err != nil {
		return
	}
	metrics.VectorInstanceCount.WithLabelValues("agent").Set(float64(len(vectorList.Items)))
}

// updatePipelineMetrics updates the pipeline success and failure metrics for a given Vector CR
func (r *VectorPipelineReconciler) updatePipelineMetrics(ctx context.Context, vectorRef string) {
	var pipelineList vectorv1alpha1.VectorPipelineList
	if err := r.List(ctx, &pipelineList); err != nil {
		return
	}

	successCount := 0
	failureCount := 0
	var namespace string

	for _, pipeline := range pipelineList.Items {
		if pipeline.Spec.VectorRef != vectorRef {
			continue
		}
		namespace = pipeline.Namespace

		// Check if the pipeline has the VectorRefValid condition
		condition := meta.FindStatusCondition(pipeline.Status.Conditions, VectorRefCondition)
		if condition != nil && condition.Status == metav1.ConditionTrue {
			successCount++
		} else {
			failureCount++
		}
	}

	if namespace != "" {
		metrics.PipelineCount.WithLabelValues("success", namespace).Set(float64(successCount))
		metrics.PipelineCount.WithLabelValues("failure", namespace).Set(float64(failureCount))
	}
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
		Name:      fmt.Sprintf("%s-config", vector.Name),
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

// updateVectorPipelineStatus updates the VectorPipeline status with retries
func (r *VectorPipelineReconciler) updateVectorPipelineStatus(ctx context.Context, vectorPipeline *vectorv1alpha1.VectorPipeline) error {
	logger := log.FromContext(ctx)
	retries := 3

	for i := 0; i < retries; i++ {
		err := r.Status().Update(ctx, vectorPipeline)
		if err == nil {
			return nil
		}
		if !errors.IsConflict(err) {
			return err
		}
		// Fetch the latest version and retry
		var latest vectorv1alpha1.VectorPipeline
		if err := r.Get(ctx, types.NamespacedName{Name: vectorPipeline.Name, Namespace: vectorPipeline.Namespace}, &latest); err != nil {
			return err
		}
		// Copy status over and retry
		latest.Status = vectorPipeline.Status
		vectorPipeline = &latest
		logger.Info("Retrying status update due to conflict")
	}
	return fmt.Errorf("failed to update VectorPipeline status after %d attempts", retries)
}

// getValidationConfigMapName returns the name of the validation ConfigMap for a pipeline
func getValidationConfigMapName(pipelineName string) string {
	return fmt.Sprintf("vector-validate-config-%s", pipelineName)
}

// copyConfigMapToVector copies the validated configuration to the Vector's ConfigMap
func (r *VectorPipelineReconciler) copyConfigMapToVector(ctx context.Context, vector *vectorv1alpha1.Vector, validationConfigMap *corev1.ConfigMap) error {
	logger := log.FromContext(ctx)

	// Get the Vector's ConfigMap
	vectorConfigMap := &corev1.ConfigMap{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      fmt.Sprintf("%s-config", vector.Name),
		Namespace: vector.Namespace,
	}, vectorConfigMap)
	if err != nil {
		return fmt.Errorf("failed to get Vector ConfigMap: %w", err)
	}

	// Copy the validated configuration
	vectorConfigMap.Data = validationConfigMap.Data

	// Update the ConfigMap
	if err := r.Update(ctx, vectorConfigMap); err != nil {
		return fmt.Errorf("failed to update Vector ConfigMap: %w", err)
	}

	logger.Info("Successfully copied validated configuration to Vector ConfigMap",
		"vector", vector.Name)

	return nil
}

// Start implements manager.Runnable
func (r *VectorPipelineReconciler) Start(ctx context.Context) error {
	logger := log.FromContext(ctx)
	logger.Info("Starting validation cleanup routine")

	// Create a new context with cancel for the cleanup routine
	cleanupCtx, cancel := context.WithCancel(ctx)
	r.cleanupCancel = cancel

	// Start the cleanup routine
	ticker := time.NewTicker(1 * time.Minute)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-cleanupCtx.Done():
				logger.Info("Stopping validation cleanup routine")
				return
			case <-ticker.C:
				r.cleanupOldValidationJobs(cleanupCtx)
			}
		}
	}()

	// Block until the context is cancelled
	<-ctx.Done()
	return nil
}

// NeedLeaderElection implements manager.LeaderElectionRunnable
func (r *VectorPipelineReconciler) NeedLeaderElection() bool {
	return true // Only run cleanup on the leader
}
