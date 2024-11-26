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

  # Optional: Image pull secrets for private registry authentication
  imagePullSecrets:
    - name: my-registry-secret

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

  # Optional: Additional volumes to mount
  volumes:
    - name: custom-config
      configMap:
        name: custom-vector-config
    - name: secrets
      secret:
        secretName: vector-secrets
    - name: persistent-data
      persistentVolumeClaim:
        claimName: vector-data

  # Optional: Additional volume mounts
  volumeMounts:
    - name: custom-config
      mountPath: /etc/vector/custom
    - name: secrets
      mountPath: /etc/vector/secrets
      readOnly: true
    - name: persistent-data
      mountPath: /var/lib/vector/data
```

## Field Descriptions

### Required Fields

- `image`: The Vector container image to use. This should be a valid Docker image reference.

### Optional Fields

- `imagePullSecrets`: List of references to secrets for pulling the Vector image from private registries
  - `name`: Name of the secret containing registry credentials

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

- `volumes`: Additional Kubernetes volumes to add to the pod
  - Supports all Kubernetes volume types (ConfigMap, Secret, PVC, etc.)
  - These are added alongside the default data and config volumes

- `volumeMounts`: Additional volume mounts for the Vector container
  - Specifies how volumes should be mounted in the container
  - These are added alongside the default data and config mounts

## Default Volumes

The Vector operator automatically creates and mounts the following volumes:

1. `data` volume:
   - Type: emptyDir
   - Mount path: Value of `spec.data_dir`
   - Used for temporary Vector data storage

2. `config` volume:
   - Type: ConfigMap
   - Mount path: /etc/vector
   - Contains Vector's configuration
   - Created and managed by the operator

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
  imagePullSecrets:
    - name: registry-credentials
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
  volumes:
    - name: host-logs
      hostPath:
        path: /var/log
        type: Directory
  volumeMounts:
    - name: host-logs
      mountPath: /var/log
      readOnly: true
```

This example deploys Vector agents with API enabled, defined resource limits, private registry authentication, and access to host logs through a volume mount. The Vector operator will create a DaemonSet that ensures Vector runs on every node in your cluster.