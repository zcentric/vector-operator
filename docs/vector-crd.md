# Vector Custom Resource

The Vector custom resource defines a Vector agent deployment that runs as a DaemonSet on your Kubernetes cluster. Vector agents are responsible for collecting, transforming, and forwarding logs and metrics from each node in your cluster.

## Specification

```yaml
apiVersion: vector.zcentric.com/v1alpha1
kind: Vector
metadata:
  name: vector-example
spec:
  # Required: Vector container image to use
  image: timberio/vector:0.34.0-debian

  # Optional: API configuration for Vector
  api:
    # Address to bind the API server to (default: "0.0.0.0:8686")
    address: "0.0.0.0:8686"
    # Enable the API server (default: false)
    enabled: true
    # Enable GraphQL playground for testing (default: false)
    playground: true

  # Optional: Data directory for Vector (default: "/tmp/vector-data-dir")
  data_dir: "/var/lib/vector"

  # Optional: How long to keep metrics before expiring them (default: 30)
  expire_metrics_secs: 30

  # Optional: ServiceAccount configuration
  serviceAccount:
    annotations:
      example.com/annotation: "value"

  # Optional: Pod tolerations
  tolerations:
    - key: "node-role.kubernetes.io/control-plane"
      operator: "Exists"
      effect: "NoSchedule"

  # Optional: Environment variables
  env:
    - name: VECTOR_LOG
      value: "debug"
    - name: VECTOR_CONFIG_YAML
      valueFrom:
        configMapKeyRef:
          name: vector-config
          key: config.yaml

  # Optional: Resource requirements
  resources:
    requests:
      cpu: "100m"
      memory: "128Mi"
    limits:
      cpu: "500m"
      memory: "512Mi"
```

## Field Descriptions

### Required Fields

- `image`: The Vector container image to use. This should be a valid Docker image reference.

### Optional Fields

- `api`: Configuration for Vector's API server
  - `address`: The address to bind the API server to
  - `enabled`: Whether to enable the API server
  - `playground`: Whether to enable the GraphQL playground

- `data_dir`: Directory where Vector stores its data
- `expire_metrics_secs`: Time in seconds before metrics are expired
- `serviceAccount`: Configuration for the Vector ServiceAccount
  - `annotations`: Key-value pairs to add as ServiceAccount annotations

- `tolerations`: Kubernetes tolerations to apply to Vector pods
- `env`: Environment variables to set in the Vector container
- `resources`: Kubernetes resource requirements for the Vector container
  - `requests`: Minimum required resources
  - `limits`: Maximum allowed resources

## Usage

The Vector custom resource is used to deploy Vector agents as a DaemonSet, ensuring one Vector instance runs on each node in your cluster. This is typically used for:

- Collecting container logs from each node
- Gathering system metrics
- Processing and forwarding logs/metrics to various destinations
- Implementing node-level monitoring and observability

## Example

```yaml
apiVersion: vector.zcentric.com/v1alpha1
kind: Vector
metadata:
  name: vector-agent
spec:
  image: timberio/vector:0.34.0-debian
  api:
    enabled: true
    address: "0.0.0.0:8686"
  data_dir: "/var/lib/vector"
  resources:
    requests:
      cpu: "100m"
      memory: "128Mi"
    limits:
      cpu: "500m"
      memory: "512Mi"
```

This example deploys Vector agents with API enabled and defined resource limits. The Vector operator will create a DaemonSet that ensures Vector runs on every node in your cluster.