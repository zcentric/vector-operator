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

func (r *VectorPipelineReconciler) validateVectorConfig(ctx context.Context, namespace string, configYaml string, pipelineName string) error {
	// Create temporary ConfigMap with unique name
	validationCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("vector-validate-config-%s", pipelineName),
			Namespace: namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "vector-pipeline-controller",
				"vectorpipeline":               pipelineName,
			},
		},
		Data: map[string]string{
			"config.yaml": configYaml,
		},
	}

	// First try to delete any existing validation ConfigMap
	if err := r.Delete(ctx, validationCM); err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("failed to cleanup old validation ConfigMap: %w", err)
		}
	}

	// Create new validation ConfigMap
	if err := r.Create(ctx, validationCM); err != nil {
		return fmt.Errorf("failed to create validation ConfigMap: %w", err)
	}
	defer func() {
		// Always try to cleanup the ConfigMap
		if err := r.Delete(context.Background(), validationCM); err != nil {
			if !errors.IsNotFound(err) {
				log.FromContext(ctx).Error(err, "Failed to delete validation ConfigMap")
			}
		}
	}()

	// Create validation job
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("vector-validate-%s", pipelineName),
			Namespace: namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "vector-pipeline-controller",
				"vectorpipeline":               pipelineName,
			},
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
										Name: validationCM.Name,
									},
								},
							},
						},
					},
				},
			},
		},
	}

	// First try to delete any existing validation job
	if err := r.Delete(ctx, job); err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("failed to cleanup old validation job: %w", err)
		}
	}

	if err := r.Create(ctx, job); err != nil {
		return fmt.Errorf("failed to create validation job: %w", err)
	}

	// Wait for validation to complete
	err := r.waitForValidationJob(ctx, job)

	return err
}

func (r *VectorPipelineReconciler) waitForValidationJob(ctx context.Context, job *batchv1.Job) error {
	timeout := time.After(2 * time.Minute)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			return fmt.Errorf("timeout waiting for validation job completion")
		case <-ticker.C:
			var completedJob batchv1.Job
			if err := r.Get(ctx, types.NamespacedName{Name: job.Name, Namespace: job.Namespace}, &completedJob); err != nil {
				return fmt.Errorf("failed to get job status: %w", err)
			}

			if completedJob.Status.Succeeded > 0 {
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
