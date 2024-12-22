package controller

import (
	"context"
	"fmt"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	pointer "k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

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
