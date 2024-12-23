package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

const (
	// MetricsPrefix is the prefix for all vector operator metrics
	MetricsPrefix = "vector_operator"
)

var (
	// Vector metrics
	VectorInstanceCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: MetricsPrefix + "_instances_total",
			Help: "Number of Vector instances by type (agent/aggregator)",
		},
		[]string{"type"},
	)

	VectorInstanceReadyCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: MetricsPrefix + "_instances_ready",
			Help: "Number of ready Vector instances by type",
		},
		[]string{"type"},
	)

	// DaemonSet/Deployment Status Metrics
	VectorPodsDesired = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: MetricsPrefix + "_pods_desired",
			Help: "Number of desired Vector pods by type",
		},
		[]string{"type", "namespace"},
	)

	VectorPodsAvailable = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: MetricsPrefix + "_pods_available",
			Help: "Number of available Vector pods by type",
		},
		[]string{"type", "namespace"},
	)

	VectorPodsUnavailable = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: MetricsPrefix + "_pods_unavailable",
			Help: "Number of unavailable Vector pods by type",
		},
		[]string{"type", "namespace"},
	)

	// Pipeline metrics
	PipelineCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: MetricsPrefix + "_pipelines_total",
			Help: "Number of VectorPipelines by status",
		},
		[]string{"status", "namespace"}, // validated, failed, pending
	)

	PipelinesByVector = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: MetricsPrefix + "_pipelines_by_vector",
			Help: "Number of pipelines associated with each Vector instance",
		},
		[]string{"vector_name", "namespace"},
	)

	ValidationJobCount = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: MetricsPrefix + "_validation_jobs_total",
			Help: "Total number of validation jobs by result",
		},
		[]string{"result", "namespace"}, // success, failure
	)

	ValidationDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    MetricsPrefix + "_validation_duration_seconds",
			Help:    "Duration of validation jobs",
			Buckets: []float64{1, 5, 10, 30, 60, 120},
		},
		[]string{"pipeline", "namespace"},
	)

	ValidationQueueLength = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: MetricsPrefix + "_validation_queue_length",
			Help: "Number of pipelines waiting for validation",
		},
		[]string{"namespace"},
	)

	ConfigMapUpdateCount = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: MetricsPrefix + "_configmap_updates_total",
			Help: "Total number of ConfigMap updates by result",
		},
		[]string{"result", "namespace"}, // success, failure
	)

	ReconciliationCount = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: MetricsPrefix + "_reconciliations_total",
			Help: "Total number of reconciliations by controller and result",
		},
		[]string{"controller", "result", "namespace"},
	)

	// Error Metrics
	ReconciliationErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: MetricsPrefix + "_reconciliation_errors_total",
			Help: "Total number of reconciliation errors by controller and error type",
		},
		[]string{"controller", "error_type", "namespace"},
	)

	// Resource Health Metrics
	ResourceHealth = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: MetricsPrefix + "_resource_health",
			Help: "Health status of Vector resources (1 for healthy, 0 for unhealthy)",
		},
		[]string{"type", "name", "namespace"},
	)

	// Operator Performance Metrics
	ReconciliationDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    MetricsPrefix + "_reconciliation_duration_seconds",
			Help:    "Duration of reconciliation operations",
			Buckets: []float64{0.1, 0.5, 1, 2, 5, 10},
		},
		[]string{"controller", "namespace"},
	)

	// API Server Interaction Metrics
	APIServerRequestCount = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: MetricsPrefix + "_api_server_requests_total",
			Help: "Total number of API server requests by type and result",
		},
		[]string{"type", "result"},
	)

	// Memory Usage Metrics
	MemoryUsage = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: MetricsPrefix + "_memory_bytes",
			Help: "Memory usage by component",
		},
		[]string{"component"},
	)
)

func init() {
	// Register metrics with the global prometheus registry
	metrics.Registry.MustRegister(
		VectorInstanceCount,
		VectorInstanceReadyCount,
		VectorPodsDesired,
		VectorPodsAvailable,
		VectorPodsUnavailable,
		PipelineCount,
		PipelinesByVector,
		ValidationJobCount,
		ValidationDuration,
		ValidationQueueLength,
		ConfigMapUpdateCount,
		ReconciliationCount,
		ReconciliationErrors,
		ResourceHealth,
		ReconciliationDuration,
		APIServerRequestCount,
		MemoryUsage,
	)
}
