# Vector Operator

[![Go Tests](https://github.com/zcentric/vector-operator/actions/workflows/go-test.yml/badge.svg)](https://github.com/zcentric/vector-operator/actions/workflows/go-test.yml)

A Kubernetes operator that simplifies the deployment and management of [Vector](https://vector.dev/) observability pipelines in your Kubernetes cluster. This operator enables declarative configuration of Vector agents and data pipelines, making it easier to collect, transform, and forward observability data.

## Overview

The Vector Operator provides two custom resources:

- **Vector**: Manages the deployment of Vector instances in your cluster, supporting both agent (DaemonSet) and aggregator (Deployment) types
- **VectorPipeline**: Defines observability data pipelines with sources, transforms, and sinks

Key features:

- Declarative configuration of Vector instances
- Support for both agent (per-node) and aggregator (centralized) deployment types
- Pipeline management with support for multiple sources, transforms, and sinks
- Kubernetes-native deployment and management
- Automatic configuration updates and reconciliation

## Quick Start

### Prerequisites

- Kubernetes cluster v1.11.3+
- kubectl v1.11.3+
- go v1.21+ (for development)
- docker v17.03+ (for development)

### Installation

1. Install the operator and CRDs:

```sh
kubectl apply -f https://raw.githubusercontent.com/zcentric/vector-operator/main/dist/install.yaml
```

2. Create a Vector instance:

For an agent (runs on every node):

```yaml
apiVersion: vector.zcentric.com/v1alpha1
kind: Vector
metadata:
  name: vector-agent
  namespace: vector
spec:
  type: agent
  image: "timberio/vector:0.38.0-distroless-libc"
```

Or for an aggregator (centralized processing):

```yaml
apiVersion: vector.zcentric.com/v1alpha1
kind: Vector
metadata:
  name: vector-aggregator
  namespace: vector
spec:
  type: aggregator
  image: "timberio/vector:0.38.0-distroless-libc"
  replicas: 2 # optional, defaults to 1
```

3. Define a pipeline:

```yaml
apiVersion: vector.zcentric.com/v1alpha1
kind: VectorPipeline
metadata:
  name: kubernetes-logs
spec:
  vectorRef: vector-agent
  sources:
    k8s-logs:
      type: "kubernetes_logs"
  transforms:
    remap:
      type: "remap"
      inputs: ["k8s-logs"]
      source: |
        .timestamp = del(.timestamp)
        .environment = "production"
  sinks:
    console:
      type: "console"
      inputs: ["remap"]
      encoding:
        codec: "json"
```

## Usage Examples

### Vector Deployment Types

#### Agent Configuration (DaemonSet)

Use the agent type when you need to collect logs and metrics from every node in your cluster:

```yaml
apiVersion: vector.zcentric.com/v1alpha1
kind: Vector
metadata:
  name: vector-agent
spec:
  type: agent
  image: "timberio/vector:0.38.0-distroless-libc"
  api:
    enabled: true
    address: "0.0.0.0:8686"
  data_dir: "/vector-data"
  expire_metrics_secs: 30
```

#### Aggregator Configuration (Deployment)

Use the aggregator type when you need centralized log processing and aggregation:

```yaml
apiVersion: vector.zcentric.com/v1alpha1
kind: Vector
metadata:
  name: vector-aggregator
spec:
  type: aggregator
  image: "timberio/vector:0.38.0-distroless-libc"
  replicas: 2
  api:
    enabled: true
    address: "0.0.0.0:8686"
  data_dir: "/vector-data"
  expire_metrics_secs: 30
```

### Pipeline with Multiple Sources and Transforms

```yaml
apiVersion: vector.zcentric.com/v1alpha1
kind: VectorPipeline
metadata:
  name: multi-source-pipeline
spec:
  vectorRef: vector-agent
  sources:
    app-logs:
      type: "kubernetes_logs"
      extra_label_selector: "app=myapp"
    system-logs:
      type: "kubernetes_logs"
      extra_label_selector: "component=system"
  transforms:
    filter-errors:
      type: "filter"
      inputs: ["app-logs"]
      condition:
        type: "vrl"
        source: ".level == 'error'"
    add-metadata:
      type: "remap"
      inputs: ["system-logs"]
      source: |
        .metadata.cluster = "production"
  sinks:
    elasticsearch:
      type: "elasticsearch"
      inputs: ["filter-errors", "add-metadata"]
```

### Common Deployment Patterns

1. **Log Collection and Forwarding**:

   - Deploy a Vector agent (DaemonSet) to collect logs from all nodes
   - Deploy a Vector aggregator (Deployment) to receive and process logs centrally
   - Configure agents to forward to the aggregator

2. **High Availability Aggregation**:
   - Deploy multiple Vector aggregator replicas for redundancy
   - Use load balancing for even distribution of log processing

## Contributing

Contributions are welcome! Here's how you can help:

1. Fork the repository
2. Create a feature branch:

   ```sh
   git checkout -b feature/my-new-feature
   ```

3. Set up your development environment:

   ```sh
   # Install dependencies
   go mod download

   # Install CRDs
   make install

   # Run the operator locally
   make run
   ```

4. Make your changes and add tests
5. Run tests:

   ```sh
   make test
   ```

6. Submit a pull request

### Development Guidelines

- Follow Go best practices and conventions
- Add unit tests for new features
- Update documentation as needed
- Use meaningful commit messages
- Run `make lint` before submitting PRs
- All PRs are automatically tested using GitHub Actions

## License

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
