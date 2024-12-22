package utils

import (
	corev1 "k8s.io/api/core/v1"
)

// GetVectorEnvVars returns the default environment variables for Vector containers
func GetVectorEnvVars() []corev1.EnvVar {
	return []corev1.EnvVar{
		{
			Name:  "VECTOR_LOG",
			Value: "info",
		},
		{
			Name: "VECTOR_SELF_NODE_NAME",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "spec.nodeName",
				},
			},
		},
		{
			Name: "VECTOR_SELF_POD_NAME",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.name",
				},
			},
		},
		{
			Name: "VECTOR_SELF_POD_NAMESPACE",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.namespace",
				},
			},
		},
		{
			Name:  "PROCFS_ROOT",
			Value: "/host/proc",
		},
		{
			Name:  "SYSFS_ROOT",
			Value: "/host/sys",
		},
	}
}

// MergeEnvVars merges user-provided environment variables with Vector defaults
func MergeEnvVars(vectorEnv []corev1.EnvVar, userEnv []corev1.EnvVar) []corev1.EnvVar {
	userEnvMap := make(map[string]corev1.EnvVar)
	for _, env := range userEnv {
		userEnvMap[env.Name] = env
	}

	result := make([]corev1.EnvVar, 0, len(vectorEnv))
	for _, env := range vectorEnv {
		if userEnv, exists := userEnvMap[env.Name]; exists {
			result = append(result, userEnv)
			delete(userEnvMap, env.Name)
		} else {
			result = append(result, env)
		}
	}

	for _, env := range userEnv {
		if _, exists := userEnvMap[env.Name]; exists {
			result = append(result, env)
		}
	}

	return result
}
