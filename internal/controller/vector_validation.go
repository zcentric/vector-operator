package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"gopkg.in/yaml.v3"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	pointer "k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	vectorv1alpha1 "github.com/zcentric/vector-operator/api/v1alpha1"
	"github.com/zcentric/vector-operator/internal/utils"
)

func (r *VectorPipelineReconciler) generateConfigForValidation(ctx context.Context, vector *vectorv1alpha1.Vector, pipeline *vectorv1alpha1.VectorPipeline) (string, error) {
	// Build the configuration sections
	configData := make(map[string]interface{})

	// Add global options
	configData["data_dir"] = "/var/lib/vector"

	// Add API configuration
	apiConfig := map[string]interface{}{
		"enabled": false,
	}
	if vector.Spec.API != nil {
		apiConfig["enabled"] = *vector.Spec.API.Enabled
		if vector.Spec.API.Address != "" {
			apiConfig["address"] = vector.Spec.API.Address
		}
	}
	configData["api"] = apiConfig

	// Initialize sources, transforms, and sinks maps
	sources := make(map[string]interface{})
	transforms := make(map[string]interface{})
	sinks := make(map[string]interface{})

	// Get all VectorPipelines that reference this Vector
	var pipelineList vectorv1alpha1.VectorPipelineList
	if err := r.List(ctx, &pipelineList, client.InNamespace(vector.Namespace)); err != nil {
		return "", fmt.Errorf("failed to list pipelines: %w", err)
	}

	// Add sources, transforms, and sinks from each pipeline
	for _, p := range pipelineList.Items {
		if p.Spec.VectorRef == vector.Name && p.Status.Conditions != nil {
			// Only include pipelines that have been previously validated
			// except for the pipeline we're currently validating
			if p.Name != pipeline.Name {
				validationCondition := meta.FindStatusCondition(p.Status.Conditions, ConfigValidCondition)
				if validationCondition == nil || validationCondition.Status != metav1.ConditionTrue {
					continue
				}
			}

			// Process Sources
			if p.Spec.Sources.Raw != nil {
				var sourcesMap map[string]interface{}
				if err := json.Unmarshal(p.Spec.Sources.Raw, &sourcesMap); err != nil {
					return "", fmt.Errorf("failed to unmarshal Sources: %w", err)
				}
				for k, v := range sourcesMap {
					sources[k] = v
				}
			}

			// Process Transforms
			if p.Spec.Transforms.Raw != nil {
				var transformsMap map[string]interface{}
				if err := json.Unmarshal(p.Spec.Transforms.Raw, &transformsMap); err != nil {
					return "", fmt.Errorf("failed to unmarshal Transforms: %w", err)
				}
				for k, v := range transformsMap {
					transforms[k] = v
				}
			}

			// Process Sinks
			if p.Spec.Sinks.Raw != nil {
				var sinksMap map[string]interface{}
				if err := json.Unmarshal(p.Spec.Sinks.Raw, &sinksMap); err != nil {
					return "", fmt.Errorf("failed to unmarshal Sinks: %w", err)
				}
				for k, v := range sinksMap {
					sinks[k] = v
				}
			}
		}
	}

	// Add the sections if they have content
	if len(sources) > 0 {
		configData["sources"] = sources
	}
	if len(transforms) > 0 {
		configData["transforms"] = transforms
	}
	if len(sinks) > 0 {
		configData["sinks"] = sinks
	}

	// Convert to YAML
	configYAML, err := yaml.Marshal(configData)
	if err != nil {
		return "", fmt.Errorf("failed to marshal config to YAML: %w", err)
	}

	return string(configYAML), nil
}

func (r *VectorPipelineReconciler) validateVectorConfig(ctx context.Context, namespace, validationConfigMapName, pipelineName string, vectorPipeline *vectorv1alpha1.VectorPipeline) error {
	logger := log.FromContext(ctx)

	// Create a unique job name for this validation
	jobName := fmt.Sprintf("vector-validate-%s", pipelineName)

	// Check if a validation job already exists
	existingJob := &batchv1.Job{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      jobName,
		Namespace: namespace,
	}, existingJob)

	if err == nil {
		// Job exists, delete it
		logger.Info("Deleting existing validation job", "job", jobName)
		propagationPolicy := metav1.DeletePropagationForeground
		if err := r.Delete(ctx, existingJob, &client.DeleteOptions{
			PropagationPolicy: &propagationPolicy,
		}); err != nil {
			return fmt.Errorf("failed to delete existing validation job: %w", err)
		}

		// Wait for the job to be deleted
		for i := 0; i < 30; i++ { // Wait up to 30 seconds
			err := r.Get(ctx, types.NamespacedName{
				Name:      jobName,
				Namespace: namespace,
			}, existingJob)
			if errors.IsNotFound(err) {
				break
			}
			time.Sleep(time.Second)
		}
	} else if !errors.IsNotFound(err) {
		return fmt.Errorf("failed to check for existing validation job: %w", err)
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: namespace,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit: pointer.Int32(0),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app.kubernetes.io/managed-by": "vector-pipeline-controller",
						"vectorpipeline":               pipelineName,
					},
					Annotations: map[string]string{
						"vector.zcentric.com/validation-start-time": time.Now().Format(time.RFC3339),
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "vector-validate",
							Image: "timberio/vector:latest-alpine",
							Env:   utils.GetVectorEnvVars(),
							Command: []string{
								"/bin/sh",
								"-c",
								`
								cat /etc/vector/config.yaml;  # Debug: print the config
								vector validate --config-yaml /etc/vector/config.yaml > /tmp/output 2>&1;
								exit_code=$?;
								if [ $exit_code -ne 0 ]; then
									echo "Validation failed:";
									cat /tmp/output;
									exit 1;
								fi
								`,
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "config-to-validate",
									MountPath: "/etc/vector",
								},
							},
						},
					},
					RestartPolicy: corev1.RestartPolicyNever,
					Volumes: []corev1.Volume{
						{
							Name: "config-to-validate",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: validationConfigMapName,
									},
								},
							},
						},
					},
				},
			},
		},
	}

	if err := r.Create(ctx, job); err != nil {
		return fmt.Errorf("failed to create validation job: %w", err)
	}

	// Wait for validation to complete
	err = r.waitForValidationJob(ctx, job, namespace, pipelineName, vectorPipeline)

	return err
}

func (r *VectorPipelineReconciler) waitForValidationJob(ctx context.Context, job *batchv1.Job, namespace string, pipelineName string, vectorPipeline *vectorv1alpha1.VectorPipeline) error {
	logger := log.FromContext(ctx)

	timeout := time.After(2 * time.Minute)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			// Clean up the job on timeout
			if err := r.Delete(ctx, job); err != nil && !errors.IsNotFound(err) {
				logger.Error(err, "Failed to delete timed out validation job")
			}
			return fmt.Errorf("timeout waiting for validation job completion")
		case <-ticker.C:
			var completedJob batchv1.Job
			if err := r.Get(ctx, types.NamespacedName{Name: job.Name, Namespace: job.Namespace}, &completedJob); err != nil {
				if errors.IsNotFound(err) {
					return fmt.Errorf("validation job was deleted before completion")
				}
				return fmt.Errorf("failed to get job status: %w", err)
			}

			if completedJob.Status.Succeeded > 0 {
				// Get the Vector instance
				vector := &vectorv1alpha1.Vector{}
				if err := r.Get(ctx, types.NamespacedName{
					Name:      vectorPipeline.Spec.VectorRef,
					Namespace: vectorPipeline.Namespace,
				}, vector); err != nil {
					return fmt.Errorf("failed to get Vector instance: %w", err)
				}

				// Check if all pipelines are validated and none have failed
				allPipelinesValidated := true
				hasFailedPipelines := false
				for _, status := range vector.Status.PipelineValidationStatus {
					if status.Status == "Failed" {
						hasFailedPipelines = true
						allPipelinesValidated = false
						break
					} else if status.Status != "Validated" {
						allPipelinesValidated = false
						break
					}
				}

				logger.Info("Checking pipeline validation status", "allPipelinesValidated", allPipelinesValidated, "hasFailedPipelines", hasFailedPipelines)

				if allPipelinesValidated && !hasFailedPipelines {
					// Get the validation ConfigMap
					validationCM := &corev1.ConfigMap{}
					if err := r.Get(ctx, types.NamespacedName{
						Name:      getValidationConfigMapName(pipelineName),
						Namespace: namespace,
					}, validationCM); err != nil {
						return fmt.Errorf("failed to get validation ConfigMap: %w", err)
					}

					// Use the copyConfigMapToVector function to update the main ConfigMap
					if err := r.copyConfigMapToVector(ctx, vector, validationCM); err != nil {
						return fmt.Errorf("failed to copy validated configuration: %w", err)
					}
				}

				// Clean up the successful job immediately
				if err := r.Delete(ctx, &completedJob); err != nil && !errors.IsNotFound(err) {
					logger.Error(err, "Failed to delete successful validation job")
				}
				return nil // Validation successful
			}

			if completedJob.Status.Failed > 0 {
				// Get the pod logs for error details
				pods := &corev1.PodList{}
				if err := r.List(ctx, pods, client.InNamespace(job.Namespace), client.MatchingLabels(job.Labels)); err != nil {
					return fmt.Errorf("validation failed, unable to get error details: %w", err)
				}

				if len(pods.Items) > 0 {
					// Get logs from the failed pod
					podLogOpts := &corev1.PodLogOptions{}
					podInterface := r.KubeClient.CoreV1().Pods(job.Namespace)
					req := podInterface.GetLogs(pods.Items[0].Name, podLogOpts)
					logs, err := req.DoRaw(ctx)
					if err != nil {
						return fmt.Errorf("validation failed, unable to get error logs: %w", err)
					}
					return fmt.Errorf("vector configuration validation failed: %s", string(logs))
				}
				return fmt.Errorf("vector configuration validation failed")
			}
		}
	}
}

func (r *VectorPipelineReconciler) cleanupOldValidationJobs(ctx context.Context) {
	logger := log.FromContext(ctx)

	// List all validation jobs
	var jobs batchv1.JobList
	if err := r.List(ctx, &jobs, client.MatchingLabels{"app.kubernetes.io/managed-by": "vector-pipeline-controller"}); err != nil {
		logger.Error(err, "Failed to list validation jobs")
		return
	}

	for _, job := range jobs.Items {
		// Skip failed jobs
		if job.Status.Failed > 0 {
			continue
		}

		// Check if the job is successful and older than 2 minutes
		if job.Status.Succeeded > 0 {
			startTime := job.Spec.Template.ObjectMeta.Annotations["vector.zcentric.com/validation-start-time"]
			if startTime == "" {
				continue
			}

			jobStartTime, err := time.Parse(time.RFC3339, startTime)
			if err != nil {
				logger.Error(err, "Failed to parse job start time", "job", job.Name)
				continue
			}

			if time.Since(jobStartTime) > 2*time.Minute {
				// Delete the job
				if err := r.Delete(ctx, &job); err != nil && !errors.IsNotFound(err) {
					logger.Error(err, "Failed to delete old validation job", "job", job.Name)
				}

				// Delete associated pods
				var pods corev1.PodList
				if err := r.List(ctx, &pods, client.InNamespace(job.Namespace), client.MatchingLabels(job.Labels)); err != nil {
					logger.Error(err, "Failed to list validation pods for cleanup")
					continue
				}

				for _, pod := range pods.Items {
					if err := r.Delete(ctx, &pod); err != nil && !errors.IsNotFound(err) {
						logger.Error(err, "Failed to delete validation pod", "pod", pod.Name)
					}
				}
			}
		}
	}
}
