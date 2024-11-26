# Vector Pipeline Guide

This guide demonstrates how to use the Vector operator to collect and process logs in your Kubernetes cluster using Vector CRs (Custom Resources), VectorPipeline CRs, and VectorAggregator CRs.

## Overview

The Vector operator uses three main Custom Resources:

1. `Vector` - Defines the Vector agent configuration for collecting logs on nodes
2. `VectorAggregator` - Defines a centralized Vector instance for aggregating logs from multiple sources
3. `VectorPipeline` - Defines log collection pipelines, including sources, transforms, and sinks

Multiple VectorPipeline CRs can reference either a Vector CR or VectorAggregator CR, allowing you to create different pipelines for different use cases.

## Architecture Options

### 1. Agent-Only Setup

Use this when you want to process logs directly on each node:

- Deploy `Vector` CR for node-level log collection
- Create `VectorPipeline` CRs that reference the Vector agent

### 2. Aggregator Setup

Use this when you want centralized log aggregation:

- Deploy `VectorAggregator` CR for centralized log processing
- Create `VectorPipeline` CRs that reference the aggregator

### 3. Agent-Aggregator Setup

Use this for a complete logging pipeline:

- Deploy `Vector` CR for node-level collection
- Deploy `VectorAggregator` CR for centralization
- Create `VectorPipeline` CRs for both agent and aggregator

## Minimal Examples

### Minimal Agent Setup

```yaml
# Minimal Vector agent configuration
apiVersion: vector.zcentric.com/v1alpha1
kind: Vector
metadata:
  name: vector-agent-minimal
spec:
  image: timberio/vector:0.34.0-debian

---
# Minimal pipeline configuration
apiVersion: vector.zcentric.com/v1alpha1
kind: VectorPipeline
metadata:
  name: vector-agent-minimal-base
spec:
  vectorRef: vector-agent-minimal
  sources:
    logs:
      type: "kubernetes_logs"
```

### Minimal Aggregator Setup

```yaml
# Minimal Vector aggregator configuration
apiVersion: vector.zcentric.com/v1alpha1
kind: VectorAggregator
metadata:
  name: vector-aggregator-minimal
spec:
  image: "timberio/vector:0.38.0-distroless-libc"

---
# Minimal aggregator pipeline
apiVersion: vector.zcentric.com/v1alpha1
kind: VectorPipeline
metadata:
  name: vector-aggregator-minimal-base
spec:
  vectorRef: vector-aggregator-minimal
  sources:
    logs:
      type: "kubernetes_logs"
```

## Complete Examples

### 1. Deploy Vector Agent

For node-level log collection:

```yaml
apiVersion: vector.zcentric.com/v1alpha1
kind: Vector
metadata:
  name: vector-basic
spec:
  # Specify the Vector image to use
  image: timberio/vector:0.34.0-debian
  # Enable the Vector API for monitoring and troubleshooting
  api:
    enabled: true
    address: "0.0.0.0:8686"
  # Directory for Vector's internal data
  data_dir: "/var/lib/vector"
```

### 2. Deploy Vector Aggregator

For centralized log aggregation:

```yaml
apiVersion: vector.zcentric.com/v1alpha1
kind: VectorAggregator
metadata:
  name: vector-aggregator
spec:
  image: "timberio/vector:0.38.0-distroless-libc"
  api:
    enabled: true
    address: "0.0.0.0:8686"
  data_dir: "/var/lib/vector"
```

### 3. Create Log Collection Pipelines

#### Example 1: Agent Pipeline (Node-Level Collection)

```yaml
apiVersion: vector.zcentric.com/v1alpha1
kind: VectorPipeline
metadata:
  name: collect-node-logs
spec:
  # Reference the Vector agent CR
  vectorRef: vector-basic
  sources:
    kubernetes_logs:
      type: "kubernetes_logs"

  transforms:
    add_node_metadata:
      type: "remap"
      inputs: ["kubernetes_logs"]
      source: |
        # Add node metadata
        .node_name = get_env_var!("VECTOR_SELF_NODE_NAME")
        .environment = "production"

  sinks:
    aggregator_forward:
      type: "vector"
      inputs: ["add_node_metadata"]
      address: "vector-aggregator:9000"
      version: "2"
```

#### Example 2: Aggregator Pipeline (Centralized Processing)

```yaml
apiVersion: vector.zcentric.com/v1alpha1
kind: VectorPipeline
metadata:
  name: process-aggregated-logs
spec:
  # Reference the Vector aggregator CR
  vectorRef: vector-aggregator
  sources:
    vector_incoming:
      type: "vector"
      address: "0.0.0.0:9000"
      version: "2"

  transforms:
    error_filter:
      type: "filter"
      inputs: ["vector_incoming"]
      condition:
        type: "vrl"
        source: |
          lowercase(string!(.level)) == "error"

    enrich_logs:
      type: "remap"
      inputs: ["error_filter"]
      source: |
        # Add processing metadata
        .processed_at = now()
        .datacenter = "us-west"

  sinks:
    elasticsearch_out:
      type: "elasticsearch"
      inputs: ["enrich_logs"]
      endpoints: ["http://elasticsearch:9200"]
      index: "vector-logs-%F"
```

## Usage

1. Apply the Vector CRs:

```bash
# For agent setup
kubectl apply -f vector-agent.yaml

# For aggregator setup
kubectl apply -f vector-aggregator.yaml
```

2. Apply VectorPipeline CRs:

```bash
kubectl apply -f vector-pipeline.yaml
```

3. Monitor the Vector instances:

```bash
# Check agent status
kubectl get vector

# Check aggregator status
kubectl get vectoraggregator

# Check pipeline status
kubectl get vectorpipeline
```

## Key Concepts

1. **Vector Agent**: Runs on each node, collecting local logs
2. **Vector Aggregator**: Centralized instance for log aggregation
3. **Sources**: Define where logs come from
4. **Transforms**: Process logs (filter, modify, parse)
5. **Sinks**: Define where logs go
6. **VectorRef**: Links VectorPipeline to Vector agent or aggregator
7. **VRL**: Vector Remap Language used in transforms

## Best Practices

1. Start with the minimal configuration and add features as needed
2. Use agents for node-level collection and aggregators for centralization
3. Create separate pipelines for different log types
4. Add appropriate metadata at both agent and aggregator levels
5. Use filters early in the pipeline to reduce processing load
6. Monitor Vector API for health and performance
7. Consider resource requirements for both agents and aggregators

## Troubleshooting

1. Check component status:

```bash
# Check agent
kubectl describe vector vector-basic

# Check aggregator
kubectl describe vectoraggregator vector-aggregator

# Check pipeline
kubectl describe vectorpipeline pipeline-name
```

2. View logs:

```bash
# Agent logs
kubectl logs -l app=vector-basic

# Aggregator logs
kubectl logs -l app=vector-aggregator
```

3. Access Vector API (if enabled):

```bash
# For agent
kubectl port-forward service/vector-basic 8686:8686

# For aggregator
kubectl port-forward service/vector-aggregator 8686:8686
```

## Common Issues and Solutions

1. **Pipeline not processing logs**

   - Verify the VectorRef matches your Vector/VectorAggregator CR name
   - Check if the source configuration is correct
   - Ensure transforms and sinks have correct input references

2. **Agent/Aggregator not starting**

   - Check if the specified image exists and is accessible
   - Verify RBAC permissions are correct
   - Check resource constraints

3. **Missing logs**

   - Verify source configuration matches your log sources
   - Check filter conditions in transforms
   - Ensure sink configuration is correct
   - Verify network connectivity between agents and aggregator

4. **Performance issues**
   - Use specific filters early in the pipeline
   - Monitor resource usage through the Vector API
   - Consider scaling aggregator resources
   - Split large pipelines into smaller, focused ones
