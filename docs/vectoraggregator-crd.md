# VectorAggregator Custom Resource

The VectorAggregator custom resource defines a Vector aggregator deployment in your Kubernetes cluster. Vector aggregators are responsible for receiving, processing, and forwarding logs and metrics from Vector agents, providing centralized log aggregation and processing capabilities.

## Specification

```yaml
apiVersion: vector.zcentric.com/v1alpha1
kind: VectorAggregator
metadata:
  name: vectoraggregator-example
spec:
  # Required: Vector container image to use
  image: timberio/vector:0.38.0-distroless-libc

  # Optional: Number of replicas to run (default: 1)
  replicas: 3

  # Optional: API configuration for Vector
  api:
    # Address to bind the API server to (default: "0.0.0.0:8686")
    address: "0.0.0.0:8686"
    # Enable the API server (default: false)
    enabled: true
    # Enable GraphQL playground for testing (default: false)
    playground: false

  # Optional: Data directory for Vector (default: "/tmp/vector-data-dir")
  data_dir: "/var/lib/vector"

  # Optional: How long to keep metrics before expiring them (default: 30)
  expire_metrics_secs: 30

  # Optional: ServiceAccount configuration
  serviceAccount:
    annotations:
      eks.amazonaws.com/role-arn: "arn:aws:iam::123456789012:role/vector-role"

  # Optional: Pod tolerations
  tolerations:
    - operator: Exists

  # Optional: Environment variables
  env:
    - name: VECTOR_BUFFER_SIZE
      value: "1048576"
    - name: VECTOR_CUSTOM_ENDPOINT
      valueFrom:
        configMapKeyRef:
          name: vector-aggregator-config
          key: endpoint

  # Optional: Resource requirements
  resources:
    requests:
      cpu: "500m"
      memory: "512Mi"
    limits:
      cpu: "2"
      memory: "2Gi"

  # Optional: Topology spread constraints for high availability
  topologySpreadConstraints:
    - maxSkew: 1
      topologyKey: topology.kubernetes.io/zone
      whenUnsatisfiable: DoNotSchedule
      labelSelector:
        matchLabels:
          app.kubernetes.io/name: vector
          app.kubernetes.io/component: aggregator

  # Optional: Additional volumes to mount
  volumes:
    - name: buffer
      persistentVolumeClaim:
        claimName: vector-buffer
    - name: custom-config
      configMap:
        name: vector-custom-config
    - name: secrets
      secret:
        secretName: vector-secrets

  # Optional: Additional volume mounts
  volumeMounts:
    - name: buffer
      mountPath: /var/lib/vector/buffer
    - name: custom-config
      mountPath: /etc/vector/custom
    - name: secrets
      mountPath: /etc/vector/secrets
      readOnly: true
```

## Field Descriptions

### Required Fields

- `image`: The Vector container image to use. This should be a valid Docker image reference.

### Optional Fields

- `replicas`: Number of Vector aggregator pods to run (default: 1)

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

- `topologySpreadConstraints`: Rules for distributing pods across topology domains
  - `maxSkew`: Maximum difference in number of pods between domains
  - `topologyKey`: The key of node labels representing the topology domain
  - `whenUnsatisfiable`: What to do when constraints cannot be met
  - `labelSelector`: Labels used to identify pods for spreading

- `volumes`: Additional Kubernetes volumes to add to the pod
  - Supports all Kubernetes volume types (ConfigMap, Secret, PVC, etc.)
  - These are added alongside the default data volume

- `volumeMounts`: Additional volume mounts for the Vector container
  - Specifies how volumes should be mounted in the container
  - These are added alongside the default data mount

## Default Volumes

The Vector operator automatically creates and mounts the following volume:

1. `data` volume:
   - Type: emptyDir
   - Mount path: Value of `spec.data_dir`
   - Used for temporary Vector data storage

## Usage

The VectorAggregator custom resource is used to deploy Vector aggregators as a Deployment. This is typically used for:

- Centralizing log collection from Vector agents
- Aggregating metrics from multiple sources
- Performing complex transformations on collected data
- Load balancing and high availability for log processing
- Forwarding processed data to external systems

## Example

```yaml
apiVersion: vector.zcentric.com/v1alpha1
kind: VectorAggregator
metadata:
  name: vector-aggregator
spec:
  image: timberio/vector:0.38.0-distroless-libc
  replicas: 3
  api:
    enabled: true
    address: "0.0.0.0:8686"
  resources:
    requests:
      cpu: "500m"
      memory: "512Mi"
    limits:
      cpu: "2"
      memory: "2Gi"
  topologySpreadConstraints:
    - maxSkew: 1
      topologyKey: topology.kubernetes.io/zone
      whenUnsatisfiable: DoNotSchedule
      labelSelector:
        matchLabels:
          app.kubernetes.io/name: vector
          app.kubernetes.io/component: aggregator
  volumes:
    - name: buffer
      persistentVolumeClaim:
        claimName: vector-buffer
  volumeMounts:
    - name: buffer
      mountPath: /var/lib/vector/buffer
```

This example deploys a highly available Vector aggregator with 3 replicas, distributed across availability zones, with persistent storage for buffers. The Vector operator will create a Deployment that ensures the specified number of Vector aggregator pods are running and properly distributed across the cluster.