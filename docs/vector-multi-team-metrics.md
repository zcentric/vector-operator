# Multi-Team Vector Metrics Collection Guide

This guide demonstrates how multiple teams can use a shared Vector deployment to collect and transform logs into metrics, enabling team autonomy while maintaining operational efficiency.

## Overview

In this example, we'll show how different teams within an organization can create their own VectorPipeline CRs to transform logs into metrics using a shared Vector deployment. This setup enables:

- Team autonomy: Each team manages their own pipeline configuration
- Resource efficiency: Single Vector deployment shared across teams
- Consistent monitoring: Standardized metric collection and export

## Architecture

```
                                  ┌──────────────────────┐
                                  │    Shared Vector     │
                                  │      Deployment      │
                                  └──────────────────────┘
                                           ▲
                                           │
                    ┌────────────────┬─────┴─────┬────────────────┐
                    │                │           │                │
         ┌──────────┴──────────┐     │    ┌──────┴───────────┐    │
         │   App Team Pipeline │     │    │  DB Team Pipeline│    │
         └──────────┬──────────┘     │    └──────┬───────────┘    │
                    │                │           │                │
            Application Logs         │      Database Logs    Infrastructure
                                     │                             Logs
                            Security Logs

```

## Example Implementation

### 1. Shared Vector Deployment

```yaml
apiVersion: vector.zcentric.com/v1alpha1
kind: Vector
metadata:
  name: shared-vector
spec:
  image: timberio/vector:0.34.0-debian
  api:
    enabled: true
    address: "0.0.0.0:8686"
  data_dir: "/var/lib/vector"
```

### 2. Application Team Pipeline

The application team wants to convert their application logs into metrics tracking API response times and error rates.

```yaml
apiVersion: vector.zcentric.com/v1alpha1
kind: VectorPipeline
metadata:
  name: app-team-metrics
  labels:
    team: application
spec:
  vectorRef: shared-vector
  sources:
    app_logs:
      type: "kubernetes_logs"
      extra_label_selector: "app=frontend"

  transforms:
    parse_logs:
      type: "remap"
      inputs: ["app_logs"]
      source: |
        . = parse_json!(string!(.message))

    extract_metrics:
      type: "log_to_metric"
      inputs: ["parse_logs"]
      metrics:
        - type: "histogram"
          field: ".response_time"
          name: "http_response_time"
          tags:
            endpoint: ".path"
            method: ".http_method"
        - type: "counter"
          name: "http_errors_total"
          tags:
            status_code: ".status"
            endpoint: ".path"
          increment_by_value: true
          conditions:
            "status_code >= 400": true

  sinks:
    prometheus_out:
      type: "prometheus_exporter"
      inputs: ["extract_metrics"]
      address: "0.0.0.0:9090"
```

### 3. Database Team Pipeline

The database team monitors database performance metrics derived from logs.

```yaml
apiVersion: vector.zcentric.com/v1alpha1
kind: VectorPipeline
metadata:
  name: db-team-metrics
  labels:
    team: database
spec:
  vectorRef: shared-vector
  sources:
    db_logs:
      type: "kubernetes_logs"
      extra_label_selector: "app=postgres"

  transforms:
    parse_db_logs:
      type: "remap"
      inputs: ["db_logs"]
      source: |
        . = parse_regex!(.message, r'^(?P<timestamp>\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}) \[(?P<level>\w+)\] (?P<message>.*)$')

    db_metrics:
      type: "log_to_metric"
      inputs: ["parse_db_logs"]
      metrics:
        - type: "gauge"
          name: "db_active_connections"
          field: ".active_connections"
          tags:
            database: ".db_name"
        - type: "histogram"
          field: ".query_time"
          name: "db_query_duration"
          tags:
            query_type: ".operation"
            database: ".db_name"
        - type: "counter"
          name: "db_deadlocks_total"
          tags:
            database: ".db_name"
          conditions:
            "contains(.message, 'deadlock detected')": true

  sinks:
    prometheus_out:
      type: "prometheus_exporter"
      inputs: ["db_metrics"]
      address: "0.0.0.0:9091"
```

### 4. Infrastructure Team Pipeline

The infrastructure team collects system-level metrics from node logs.

```yaml
apiVersion: vector.zcentric.com/v1alpha1
kind: VectorPipeline
metadata:
  name: infra-team-metrics
  labels:
    team: infrastructure
spec:
  vectorRef: shared-vector
  sources:
    system_logs:
      type: "kubernetes_logs"
      extra_label_selector: "app=node-exporter"

  transforms:
    parse_system_logs:
      type: "remap"
      inputs: ["system_logs"]
      source: |
        . = parse_json!(string!(.message))

    system_metrics:
      type: "log_to_metric"
      inputs: ["parse_system_logs"]
      metrics:
        - type: "gauge"
          name: "node_memory_usage"
          field: ".memory.used_bytes"
          tags:
            node: ".node_name"
        - type: "gauge"
          name: "node_disk_usage"
          field: ".disk.used_bytes"
          tags:
            node: ".node_name"
            device: ".disk.device"
        - type: "counter"
          name: "node_oom_events"
          tags:
            node: ".node_name"
          conditions:
            "contains(.message, 'Out of memory')": true

  sinks:
    prometheus_out:
      type: "prometheus_exporter"
      inputs: ["system_metrics"]
      address: "0.0.0.0:9092"
```

## Benefits of This Approach

1. **Team Autonomy**

   - Each team manages their own VectorPipeline CR
   - Teams can modify their pipelines without affecting others
   - Independent metric collection and transformation logic

2. **Operational Efficiency**

   - Single Vector deployment reduces resource overhead
   - Centralized logging infrastructure
   - Simplified maintenance and updates

3. **Metric Standardization**

   - Consistent metric naming conventions
   - Standardized label/tag structure
   - Unified Prometheus export format

4. **Resource Isolation**
   - Each pipeline runs independently
   - Separate Prometheus exporters per team
   - Individual resource quotas possible

## Best Practices

1. **Naming Conventions**

   - Use team-specific prefixes for metric names
   - Consistent label/tag naming across teams
   - Clear pipeline naming for easy identification

2. **Resource Management**

   - Monitor pipeline performance
   - Set appropriate scrape intervals
   - Use efficient regex patterns

3. **Security**

   - Implement RBAC for pipeline management
   - Restrict metric endpoint access
   - Validate log sources

4. **Monitoring**
   - Monitor Vector performance
   - Track pipeline throughput
   - Alert on pipeline failures

## Implementation Steps

1. Deploy the shared Vector instance:

```bash
kubectl apply -f shared-vector.yaml
```

2. Each team applies their pipeline:

```bash
# Application team
kubectl apply -f app-team-pipeline.yaml

# Database team
kubectl apply -f db-team-pipeline.yaml

# Infrastructure team
kubectl apply -f infra-team-pipeline.yaml
```

3. Configure Prometheus to scrape team-specific endpoints:

```yaml
scrape_configs:
  - job_name: "app-team-metrics"
    static_configs:
      - targets: ["shared-vector:9090"]
  - job_name: "db-team-metrics"
    static_configs:
      - targets: ["shared-vector:9091"]
  - job_name: "infra-team-metrics"
    static_configs:
      - targets: ["shared-vector:9092"]
```

## Troubleshooting

1. **Check Pipeline Status**

```bash
kubectl get vectorpipeline
kubectl describe vectorpipeline app-team-metrics
```

2. **Vector Logs**

```bash
kubectl logs -l app=shared-vector
```

3. **Metric Verification**

```bash
# Check if metrics are being exported
curl http://shared-vector:9090/metrics
curl http://shared-vector:9091/metrics
curl http://shared-vector:9092/metrics
```

4. **Common Issues**
   - Incorrect log parsing patterns
   - Missing or malformed log fields
   - Network connectivity issues
   - Resource constraints
