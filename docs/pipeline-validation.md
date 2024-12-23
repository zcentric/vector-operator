+# Vector Pipeline Validation
+
+The Vector Operator includes a validation system for Vector pipelines that ensures configurations are valid before they are applied to the running Vector instances.
+
+## How Validation Works
+
+1. When a VectorPipeline is created or modified, the operator:
+   - Creates a validation ConfigMap with the new configuration
+   - Runs a validation job using the Vector image to test the configuration
+   - Only updates the main Vector ConfigMap if validation succeeds
+
+2. The validation status is stored in:
+   - The VectorPipeline resource's status conditions
+   - The Vector resource's pipeline validation status
+
+## Checking Validation Status
+
+### Check VectorPipeline Status
+
+```bash
+kubectl get vectorpipeline <pipeline-name> -o yaml
+```
+
+Look for the status conditions:
+```yaml
+status:
+  conditions:
+  - lastTransitionTime: "2024-01-01T00:00:00Z"
+    message: Vector configuration validation succeeded
+    reason: ValidationSucceeded
+    status: "True"
+    type: ConfigurationValid
+```
+
+### Check Vector Status
+
+```bash
+kubectl get vector <vector-name> -o yaml
+```
+
+Look for the pipeline validation status:
+```yaml
+status:
+  pipelineValidationStatus:
+    pipeline-name:
+      status: Validated
+      message: Configuration validation succeeded
+      lastValidated: "2024-01-01T00:00:00Z"
+      generation: 1
+```
+
+## Validation Failures
+
+If validation fails:
+1. The configuration will not be applied to the running Vector instances
+2. The validation status will show the failure reason
+3. No new validations will run until the pipeline is modified
+
+### Check Validation Job Logs
+
+To see detailed validation errors:
+
+```bash
+# Find the validation job pod
+kubectl get pods | grep vector-validate
+
+# Check the logs
+kubectl logs <validation-pod-name>
+```
+
+## Best Practices
+
+1. Always check validation status after applying pipeline changes
+2. Fix validation errors before making additional changes
+3. Monitor the Vector status to ensure all pipelines are validated
+4. Use `kubectl describe` on the VectorPipeline for detailed validation history 