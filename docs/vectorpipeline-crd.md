# VectorPipeline Custom Resource

The VectorPipeline custom resource defines the data processing pipeline configuration for Vector instances. It allows you to configure sources, transforms, and sinks for your Vector agents and aggregators, enabling flexible log and metric processing workflows.

## Specification

```yaml
apiVersion: vector.zcentric.com/v1alpha1
kind: VectorPipeline
metadata:
  name: vectorpipeline-example
spec:
  # Required: Reference to the Vector or VectorAggregator instance
  vectorRef: vector-example

  # Optional: Source configurations for data ingestion
  sources:
    logs:
      type: "kubernetes_logs"
    metrics:
      type: "host_metrics"
      endpoints: ["http://localhost:9100/metrics"]

  # Optional: Transform configurations for data processing
  transforms:
    addTeam:
      type: "remap"
      inputs:
        - logs
      source: |
        .team = "teamA"
    filter:
      type: "filter"
      inputs:
        - addTeam
      condition:
        type: "vrl"
        source: ".status != 200"

  # Optional: Sink configurations for data output
  sinks:
    console:
      type: "console"
      inputs:
        - filter
      encoding:
        codec: "json"
    s3:
      type: "aws_s3"
      inputs:
        - filter
      bucket: "my-log-bucket"
      compression: "gzip"
```

## Field Descriptions

### Required Fields

- `vectorRef`: Name of the Vector or VectorAggregator instance this pipeline is associated with

### Optional Fields

- `sources`: Configuration of data input sources
  - Each source has a unique name and configuration
  - Common source types: kubernetes_logs, host_metrics, file, http
  - Configuration varies by source type

- `transforms`: Configuration of data transformation steps
  - Each transform has a unique name and configuration
  - Supports various transform types: remap, filter, aggregate, etc.
  - `inputs`: List of source or transform names to process
  - Configuration varies by transform type

- `sinks`: Configuration of data output destinations
  - Each sink has a unique name and configuration
  - Common sink types: console, aws_s3, elasticsearch, prometheus
  - `inputs`: List of source or transform names to output
  - Configuration varies by sink type

## Usage

The VectorPipeline custom resource is used to define how Vector processes data. It's typically used for:

- Configuring log collection from Kubernetes pods
- Setting up metrics collection from various sources
- Defining data transformation and enrichment rules
- Specifying output destinations for processed data
- Creating complex data processing workflows

## Examples

### Basic Log Collection

```yaml
apiVersion: vector.zcentric.com/v1alpha1
kind: VectorPipeline
metadata:
  name: basic-logs
spec:
  vectorRef: vector-agent
  sources:
    logs:
      type: "kubernetes_logs"
  sinks:
    stdout:
      type: "console"
      encoding:
        codec: "json"
      inputs:
        - logs
```

### Advanced Processing Pipeline

```yaml
apiVersion: vector.zcentric.com/v1alpha1
kind: VectorPipeline
metadata:
  name: advanced-pipeline
spec:
  vectorRef: vector-aggregator
  sources:
    metrics:
      type: "prometheus_scrape"
      endpoints: ["http://app:9090/metrics"]
  transforms:
    enrich:
      type: "remap"
      inputs:
        - metrics
      source: |
        .environment = "production"
        .datacenter = "us-east"
    filter_errors:
      type: "filter"
      inputs:
        - enrich
      condition:
        type: "vrl"
        source: '.status_code != 500'
  sinks:
    elasticsearch:
      type: "elasticsearch"
      inputs:
        - filter_errors
      endpoints: ["http://elasticsearch:9200"]
      index: "vector-%F"
```

The VectorPipeline allows you to create sophisticated data processing workflows by connecting sources, transforms, and sinks. The Vector operator ensures these pipeline configurations are properly applied to the referenced Vector instances.