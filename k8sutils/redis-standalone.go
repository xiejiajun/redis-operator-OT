package k8sutils

import (
	redisv1beta1 "redis-operator/api/v1beta1"
)

var (
	enableMetrics bool
)

// CreateStandAloneService method will create standalone service for Redis
func CreateStandAloneService(cr *redisv1beta1.Redis) error {
	logger := serviceLogger(cr.Namespace, cr.ObjectMeta.Name)
	// TODO 构造标签
	labels := getRedisLabels(cr.ObjectMeta.Name, "standalone", "standalone")
	if cr.Spec.RedisExporter != nil && cr.Spec.RedisExporter.Enabled {
		enableMetrics = true
	}
	// TODO 生成K8s 资源Meta信息
	objectMetaInfo := generateObjectMetaInformation(cr.ObjectMeta.Name, cr.Namespace, labels, generateServiceAnots())
	// TODO 生成headless service
	headlessObjectMetaInfo := generateObjectMetaInformation(cr.ObjectMeta.Name+"-headless", cr.Namespace, labels, generateServiceAnots())
	// TODO 创建无头服务，用于STS
	err := CreateOrUpdateHeadlessService(cr.Namespace, headlessObjectMetaInfo, labels, redisAsOwner(cr))
	if err != nil {
		logger.Error(err, "Cannot create standalone headless service for Redis")
		return err
	}
	// TODO 创建用于将Redis服务暴露给外部的ClusterIP类型的service
	err = CreateOrUpdateService(cr.Namespace, objectMetaInfo, labels, redisAsOwner(cr), enableMetrics)
	if err != nil {
		logger.Error(err, "Cannot create standalone service for Redis")
		return err
	}
	return nil
}

// CreateStandAloneRedis will create a standalone redis setup
func CreateStandAloneRedis(cr *redisv1beta1.Redis) error {
	logger := stateFulSetLogger(cr.Namespace, cr.ObjectMeta.Name)
	// TODO 打上standalone标签
	labels := getRedisLabels(cr.ObjectMeta.Name, "standalone", "standalone")
	// TODO 生成K8s 资源Meta信息
	objectMetaInfo := generateObjectMetaInformation(cr.ObjectMeta.Name, cr.Namespace, labels, generateStatefulSetsAnots())
	// TODO 创建或者更新standalone节点
	err := CreateOrUpdateStateFul(cr.Namespace,
		objectMetaInfo,
		labels,
		generateRedisStandaloneParams(cr),
		redisAsOwner(cr),
		// TODO 创建生成Container相关的参数
		generateRedisStandaloneContainerParams(cr),
	)
	if err != nil {
		logger.Error(err, "Cannot create standalone statefulset for Redis")
		return err
	}
	return nil
}

// generateRedisStandalone generates Redis standalone information
func generateRedisStandaloneParams(cr *redisv1beta1.Redis) statefulSetParameters {
	replicas := int32(1)
	// TODO 构建用于构建StatefulSet的参数
	res := statefulSetParameters{
		Replicas:          &replicas,
		NodeSelector:      cr.Spec.NodeSelector,
		SecurityContext:   cr.Spec.SecurityContext,
		PriorityClassName: cr.Spec.PriorityClassName,
		Affinity:          cr.Spec.Affinity,
		Tolerations:       cr.Spec.Tolerations,
		EnableMetrics:     cr.Spec.RedisExporter.Enabled,
	}
	if cr.Spec.KubernetesConfig.ImagePullSecrets != nil {
		res.ImagePullSecrets = cr.Spec.KubernetesConfig.ImagePullSecrets
	}
	if cr.Spec.Storage != nil {
		res.PersistentVolumeClaim = cr.Spec.Storage.VolumeClaimTemplate
	}
	if cr.Spec.RedisConfig != nil {
		res.ExternalConfig = cr.Spec.RedisConfig.AdditionalRedisConfig
	}
	return res
}

// generateRedisStandaloneContainerParams generates Redis container information
func generateRedisStandaloneContainerParams(cr *redisv1beta1.Redis) containerParameters {
	trueProperty := true
	falseProperty := false
	containerProp := containerParameters{
		Role:                         "standalone",
		Image:                        cr.Spec.KubernetesConfig.Image,
		ImagePullPolicy:              cr.Spec.KubernetesConfig.ImagePullPolicy,
		Resources:                    cr.Spec.KubernetesConfig.Resources,
		RedisExporterImage:           cr.Spec.RedisExporter.Image,
		RedisExporterImagePullPolicy: cr.Spec.RedisExporter.ImagePullPolicy,
		RedisExporterResources:       cr.Spec.RedisExporter.Resources,
	}
	if cr.Spec.KubernetesConfig.ExistingPasswordSecret != nil {
		containerProp.EnabledPassword = &trueProperty
		containerProp.SecretName = cr.Spec.KubernetesConfig.ExistingPasswordSecret.Name
		containerProp.SecretKey = cr.Spec.KubernetesConfig.ExistingPasswordSecret.Key
	} else {
		containerProp.EnabledPassword = &falseProperty
	}
	if cr.Spec.RedisExporter.EnvVars != nil {
		containerProp.RedisExporterEnv = cr.Spec.RedisExporter.EnvVars
	}
	if cr.Spec.Storage != nil {
		containerProp.PersistenceEnabled = &trueProperty
	}
	return containerProp
}
