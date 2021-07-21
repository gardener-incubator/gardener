// Copyright (c) 2020 SAP SE or an SAP affiliate company. All rights reserved. This file is licensed under the Apache Software License, v. 2 except as noted otherwise in the LICENSE file
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package etcd

import (
	"context"
	"fmt"
	"time"

	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	"github.com/gardener/gardener/pkg/client/kubernetes"
	"github.com/gardener/gardener/pkg/operation/botanist/component"
	"github.com/gardener/gardener/pkg/utils"
	gutil "github.com/gardener/gardener/pkg/utils/gardener"
	"github.com/gardener/gardener/pkg/utils/imagevector"
	"github.com/gardener/gardener/pkg/utils/managedresources"

	"github.com/Masterminds/semver"
	druidv1alpha1 "github.com/gardener/etcd-druid/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	batchv1beta1 "k8s.io/api/batch/v1beta1"
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	autoscalingv1beta2 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1beta2"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// Druid is a constant for the name of the etcd-druid.
	Druid = "etcd-druid"

	druidRBACName                          = "gardener.cloud:system:" + Druid
	druidServiceAccountName                = Druid
	druidVPAName                           = Druid + "-vpa"
	druidConfigMapImageVectorOverwriteName = Druid + "-imagevector-overwrite"
	druidDeploymentName                    = Druid
	managedResourceControlName             = Druid

	druidConfigMapImageVectorOverwriteDataKey          = "images_overwrite.yaml"
	druidDeploymentVolumeMountPathImageVectorOverwrite = "/charts_overwrite"
	druidDeploymentVolumeNameImageVectorOverwrite      = "imagevector-overwrite"
)

// NewBootstrapper creates a new instance of DeployWaiter for the etcd bootstrapper.
func NewBootstrapper(
	client client.Client,
	namespace string,
	image string,
	kubernetesVersion *semver.Version,
	imageVectorOverwrite *string,
) component.DeployWaiter {
	return &bootstrapper{
		client:               client,
		namespace:            namespace,
		image:                image,
		kubernetesVersion:    kubernetesVersion,
		imageVectorOverwrite: imageVectorOverwrite,
	}
}

type bootstrapper struct {
	client               client.Client
	namespace            string
	image                string
	kubernetesVersion    *semver.Version
	imageVectorOverwrite *string
}

func (b *bootstrapper) Deploy(ctx context.Context) error {
	var (
		registry = managedresources.NewRegistry(kubernetes.SeedScheme, kubernetes.SeedCodec, kubernetes.SeedSerializer)
		labels   = func() map[string]string { return map[string]string{v1beta1constants.GardenRole: Druid} }

		serviceAccount = &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:      druidServiceAccountName,
				Namespace: b.namespace,
				Labels:    labels(),
			},
		}

		clusterRole = &rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{
				Name:   druidRBACName,
				Labels: labels(),
			},
			Rules: []rbacv1.PolicyRule{
				{
					APIGroups: []string{corev1.GroupName},
					Resources: []string{"pods"},
					Verbs:     []string{"list", "watch", "delete"},
				},
				{
					APIGroups: []string{corev1.GroupName},
					Resources: []string{"secrets", "endpoints"},
					Verbs:     []string{"get", "list", "patch", "update", "watch"},
				},
				{
					APIGroups: []string{corev1.GroupName},
					Resources: []string{"events"},
					Verbs:     []string{"create", "get", "list", "watch", "patch", "update"},
				},
				{
					APIGroups: []string{corev1.GroupName, appsv1.GroupName},
					Resources: []string{"services", "configmaps", "statefulsets"},
					Verbs:     []string{"get", "list", "patch", "update", "watch", "create", "delete"},
				},
				{
					APIGroups: []string{druidv1alpha1.GroupVersion.Group},
					Resources: []string{"etcds"},
					Verbs:     []string{"get", "list", "watch", "update", "patch"},
				},
				{
					APIGroups: []string{druidv1alpha1.GroupVersion.Group},
					Resources: []string{"etcds/status", "etcds/finalizers"},
					Verbs:     []string{"get", "update", "patch", "create"},
				},
				{
					APIGroups: []string{coordinationv1.GroupName},
					Resources: []string{"leases"},
					Verbs:     []string{"create"},
				},
				{
					APIGroups:     []string{coordinationv1.GroupName},
					Resources:     []string{"leases"},
					ResourceNames: []string{"druid-leader-election"},
					Verbs:         []string{"get", "update", "patch"},
				},
				{
					APIGroups: []string{corev1.GroupName},
					Resources: []string{"persistentvolumeclaims"},
					Verbs:     []string{"get", "list", "watch"},
				},
				{
					APIGroups: []string{batchv1beta1.GroupName},
					Resources: []string{"cronjobs"},
					Verbs:     []string{"get", "list", "patch", "update", "watch", "create", "delete"},
				},
			},
		}

		clusterRoleBinding = &rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:   druidRBACName,
				Labels: labels(),
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: rbacv1.GroupName,
				Kind:     "ClusterRole",
				Name:     druidRBACName,
			},
			Subjects: []rbacv1.Subject{
				{
					Kind:      rbacv1.ServiceAccountKind,
					Name:      druidServiceAccountName,
					Namespace: b.namespace,
				},
			},
		}

		configMapImageVectorOverwrite = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      druidConfigMapImageVectorOverwriteName,
				Namespace: b.namespace,
				Labels:    labels(),
			},
		}

		vpaUpdateMode = autoscalingv1beta2.UpdateModeAuto
		vpa           = &autoscalingv1beta2.VerticalPodAutoscaler{
			ObjectMeta: metav1.ObjectMeta{
				Name:      druidVPAName,
				Namespace: b.namespace,
				Labels:    labels(),
			},
			Spec: autoscalingv1beta2.VerticalPodAutoscalerSpec{
				TargetRef: &autoscalingv1.CrossVersionObjectReference{
					APIVersion: appsv1.SchemeGroupVersion.String(),
					Kind:       "Deployment",
					Name:       druidDeploymentName,
				},
				UpdatePolicy: &autoscalingv1beta2.PodUpdatePolicy{
					UpdateMode: &vpaUpdateMode,
				},
				ResourcePolicy: &autoscalingv1beta2.PodResourcePolicy{
					ContainerPolicies: []autoscalingv1beta2.ContainerResourcePolicy{{
						ContainerName: autoscalingv1beta2.DefaultContainerResourcePolicy,
						MinAllowed: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("50m"),
							corev1.ResourceMemory: resource.MustParse("100M"),
						},
					}},
				},
			},
		}

		deployment = &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      druidDeploymentName,
				Namespace: b.namespace,
				Labels:    labels(),
			},
			Spec: appsv1.DeploymentSpec{
				Replicas:             pointer.Int32(1),
				RevisionHistoryLimit: pointer.Int32(1),
				Selector: &metav1.LabelSelector{
					MatchLabels: labels(),
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: labels(),
					},
					Spec: corev1.PodSpec{
						ServiceAccountName: druidServiceAccountName,
						Containers: []corev1.Container{
							{
								Name:            Druid,
								Image:           b.image,
								ImagePullPolicy: corev1.PullIfNotPresent,
								Command: []string{
									"/bin/etcd-druid",
									"--enable-leader-election=true",
									"--ignore-operation-annotation=false",
									"--workers=50",
								},
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("50m"),
										corev1.ResourceMemory: resource.MustParse("128Mi"),
									},
									Limits: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("300m"),
										corev1.ResourceMemory: resource.MustParse("512Mi"),
									},
								},
								Ports: []corev1.ContainerPort{{
									ContainerPort: 9569,
								}},
							},
						},
					},
				},
			},
		}

		resourcesToAdd = []client.Object{
			serviceAccount,
			clusterRole,
			clusterRoleBinding,
			vpa,
		}
	)

	if b.imageVectorOverwrite != nil {
		configMapImageVectorOverwrite.Data = map[string]string{druidConfigMapImageVectorOverwriteDataKey: *b.imageVectorOverwrite}
		resourcesToAdd = append(resourcesToAdd, configMapImageVectorOverwrite)

		metav1.SetMetaDataAnnotation(&deployment.Spec.Template.ObjectMeta, "checksum/configmap-imagevector-overwrite", utils.ComputeChecksum(configMapImageVectorOverwrite.Data))
		deployment.Spec.Template.Spec.Volumes = append(deployment.Spec.Template.Spec.Volumes, corev1.Volume{
			Name: druidDeploymentVolumeNameImageVectorOverwrite,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: druidConfigMapImageVectorOverwriteName,
					},
				},
			},
		})
		deployment.Spec.Template.Spec.Containers[0].VolumeMounts = append(deployment.Spec.Template.Spec.Containers[0].VolumeMounts, corev1.VolumeMount{
			Name:      druidDeploymentVolumeNameImageVectorOverwrite,
			MountPath: druidDeploymentVolumeMountPathImageVectorOverwrite,
			ReadOnly:  true,
		})
		deployment.Spec.Template.Spec.Containers[0].Env = append(deployment.Spec.Template.Spec.Containers[0].Env, corev1.EnvVar{
			Name:  imagevector.OverrideEnv,
			Value: druidDeploymentVolumeMountPathImageVectorOverwrite + "/" + druidConfigMapImageVectorOverwriteDataKey,
		})
	}

	resources, err := registry.AddAllAndSerialize(append(resourcesToAdd, deployment)...)
	if err != nil {
		return err
	}
	resources["crd.yaml"] = []byte(crdYAML)

	return managedresources.CreateForSeed(ctx, b.client, b.namespace, managedResourceControlName, false, resources)
}

func (b *bootstrapper) Destroy(ctx context.Context) error {
	etcdList := &druidv1alpha1.EtcdList{}
	// Need to check for both error types. The DynamicRestMapper can hold a stale cache returning a path to a non-existing api-resource leading to a NotFound error.
	if err := b.client.List(ctx, etcdList); err != nil && !meta.IsNoMatchError(err) && !apierrors.IsNotFound(err) {
		return err
	}

	if len(etcdList.Items) > 0 {
		return fmt.Errorf("cannot debootstrap etcd-druid because there are still druidv1alpha1.Etcd resources left in the cluster")
	}

	if err := gutil.ConfirmDeletion(ctx, b.client, &apiextensionsv1beta1.CustomResourceDefinition{ObjectMeta: metav1.ObjectMeta{Name: crdName}}); client.IgnoreNotFound(err) != nil {
		return err
	}

	return managedresources.DeleteForSeed(ctx, b.client, b.namespace, managedResourceControlName)
}

// TimeoutWaitForManagedResource is the timeout used while waiting for the ManagedResources to become healthy
// or deleted.
var TimeoutWaitForManagedResource = 2 * time.Minute

func (b *bootstrapper) Wait(ctx context.Context) error {
	timeoutCtx, cancel := context.WithTimeout(ctx, TimeoutWaitForManagedResource)
	defer cancel()

	return managedresources.WaitUntilHealthy(timeoutCtx, b.client, b.namespace, managedResourceControlName)
}

func (b *bootstrapper) WaitCleanup(ctx context.Context) error {
	timeoutCtx, cancel := context.WithTimeout(ctx, TimeoutWaitForManagedResource)
	defer cancel()

	return managedresources.WaitUntilDeleted(timeoutCtx, b.client, b.namespace, managedResourceControlName)
}

const (
	crdName = "etcds.druid.gardener.cloud"
	crdYAML = `apiVersion: apiextensions.k8s.io/v1beta1
kind: CustomResourceDefinition
metadata:
  name: ` + crdName + `
  annotations:
    controller-gen.kubebuilder.io/version: v0.2.4
  labels:
    ` + gutil.DeletionProtected + `: "true"
spec:
  group: druid.gardener.cloud
  names:
    kind: Etcd
    listKind: EtcdList
    plural: etcds
    singular: etcd
  scope: Namespaced
  subresources:
    scale:
      labelSelectorPath: .status.labelSelector
      specReplicasPath: .spec.replicas
      statusReplicasPath: .status.replicas
    status: {}
  preserveUnknownFields: false
  validation:
    openAPIV3Schema:
      description: Etcd is the Schema for the etcds API
      properties:
        apiVersion:
          description: 'APIVersion defines the versioned schema of this representation
            of an object. Servers should convert recognized schemas to the latest
            internal value, and may reject unrecognized values. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources'
          type: string
        kind:
          description: 'Kind is a string value representing the REST resource this
            object represents. Servers may infer this from the endpoint the client
            submits requests to. Cannot be updated. In CamelCase. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds'
          type: string
        metadata:
          type: object
        spec:
          description: EtcdSpec defines the desired state of Etcd
          properties:
            annotations:
              additionalProperties:
                type: string
              type: object
            backup:
              description: BackupSpec defines parametes associated with the full and
                delta snapshots of etcd
              properties:
                compression:
                  description: SnapshotCompression defines the specification for compression of Snapshots.
                  properties:
                    enabled:
                      type: boolean
                    policy:
                      type: string
                  type: object
                deltaSnapshotMemoryLimit:
                  description: DeltaSnapshotMemoryLimit defines the memory limit after
                    which delta snapshots will be taken
                  type: string
                deltaSnapshotPeriod:
                  description: DeltaSnapshotPeriod defines the period after which
                    delta snapshots will be taken
                  type: string
                fullSnapshotSchedule:
                  description: FullSnapshotSchedule defines the cron standard schedule
                    for full snapshots.
                  type: string
                garbageCollectionPeriod:
                  description: GarbageCollectionPeriod defines the period for garbage
                    collecting old backups
                  type: string
                garbageCollectionPolicy:
                  description: GarbageCollectionPolicy defines the policy for garbage
                    collecting old backups
                  enum:
                  - Exponential
                  - LimitBased
                  type: string
                image:
                  description: Image defines the etcd container image and tag
                  type: string
                port:
                  description: Port define the port on which etcd-backup-restore server
                    will exposed.
                  type: integer
                resources:
                  description: 'Resources defines the compute Resources required by
                    backup-restore container. More info: https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/'
                  properties:
                    limits:
                      additionalProperties:
                        type: string
                      description: 'Limits describes the maximum amount of compute
                        resources allowed. More info: https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/'
                      type: object
                    requests:
                      additionalProperties:
                        type: string
                      description: 'Requests describes the minimum amount of compute
                        resources required. If Requests is omitted for a container,
                        it defaults to Limits if that is explicitly specified, otherwise
                        to an implementation-defined value. More info: https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/'
                      type: object
                  type: object
                store:
                  description: Store defines the specification of object store provider
                    for storing backups.
                  properties:
                    container:
                      type: string
                    prefix:
                      type: string
                    provider:
                      description: StorageProvider defines the type of object store
                        provider for storing backups.
                      type: string
                    secretRef:
                      description: SecretReference represents a Secret Reference.
                        It has enough information to retrieve secret in any namespace
                      properties:
                        name:
                          description: Name is unique within a namespace to reference
                            a secret resource.
                          type: string
                        namespace:
                          description: Namespace defines the space within which the
                            secret name must be unique.
                          type: string
                      type: object
                  required:
                  - prefix
                  type: object
                tls:
                  description: TLSConfig hold the TLS configuration details.
                  properties:
                    clientTLSSecretRef:
                      description: SecretReference represents a Secret Reference.
                        It has enough information to retrieve secret in any namespace
                      properties:
                        name:
                          description: Name is unique within a namespace to reference
                            a secret resource.
                          type: string
                        namespace:
                          description: Namespace defines the space within which the
                            secret name must be unique.
                          type: string
                      type: object
                    serverTLSSecretRef:
                      description: SecretReference represents a Secret Reference.
                        It has enough information to retrieve secret in any namespace
                      properties:
                        name:
                          description: Name is unique within a namespace to reference
                            a secret resource.
                          type: string
                        namespace:
                          description: Namespace defines the space within which the
                            secret name must be unique.
                          type: string
                      type: object
                    tlsCASecretRef:
                      description: SecretReference represents a Secret Reference.
                        It has enough information to retrieve secret in any namespace
                      properties:
                        name:
                          description: Name is unique within a namespace to reference
                            a secret resource.
                          type: string
                        namespace:
                          description: Namespace defines the space within which the
                            secret name must be unique.
                          type: string
                      type: object
                  required:
                  - clientTLSSecretRef
                  - serverTLSSecretRef
                  - tlsCASecretRef
                  type: object
              type: object
            etcd:
              description: EtcdConfig defines parametes associated etcd deployed
              properties:
                authSecretRef:
                  description: SecretReference represents a Secret Reference. It has
                    enough information to retrieve secret in any namespace
                  properties:
                    name:
                      description: Name is unique within a namespace to reference
                        a secret resource.
                      type: string
                    namespace:
                      description: Namespace defines the space within which the secret
                        name must be unique.
                      type: string
                  type: object
                clientPort:
                  type: integer
                defragmentationSchedule:
                  description: DefragmentationSchedule defines the cron standard schedule
                    for defragmentation of etcd.
                  type: string
                image:
                  description: Image defines the etcd container image and tag
                  type: string
                metrics:
                  description: Metrics defines the level of detail for exported metrics
                    of etcd, specify 'extensive' to include histogram metrics.
                  enum:
                  - basic
                  - extensive
                  type: string
                quota:
                  description: Quota defines the etcd DB quota.
                  type: string
                resources:
                  description: 'Resources defines the compute Resources required by
                    etcd container. More info: https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/'
                  properties:
                    limits:
                      additionalProperties:
                        type: string
                      description: 'Limits describes the maximum amount of compute
                        resources allowed. More info: https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/'
                      type: object
                    requests:
                      additionalProperties:
                        type: string
                      description: 'Requests describes the minimum amount of compute
                        resources required. If Requests is omitted for a container,
                        it defaults to Limits if that is explicitly specified, otherwise
                        to an implementation-defined value. More info: https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/'
                      type: object
                  type: object
                serverPort:
                  type: integer
                tls:
                  description: TLSConfig hold the TLS configuration details.
                  properties:
                    clientTLSSecretRef:
                      description: SecretReference represents a Secret Reference.
                        It has enough information to retrieve secret in any namespace
                      properties:
                        name:
                          description: Name is unique within a namespace to reference
                            a secret resource.
                          type: string
                        namespace:
                          description: Namespace defines the space within which the
                            secret name must be unique.
                          type: string
                      type: object
                    serverTLSSecretRef:
                      description: SecretReference represents a Secret Reference.
                        It has enough information to retrieve secret in any namespace
                      properties:
                        name:
                          description: Name is unique within a namespace to reference
                            a secret resource.
                          type: string
                        namespace:
                          description: Namespace defines the space within which the
                            secret name must be unique.
                          type: string
                      type: object
                    tlsCASecretRef:
                      description: SecretReference represents a Secret Reference.
                        It has enough information to retrieve secret in any namespace
                      properties:
                        name:
                          description: Name is unique within a namespace to reference
                            a secret resource.
                          type: string
                        namespace:
                          description: Namespace defines the space within which the
                            secret name must be unique.
                          type: string
                      type: object
                  required:
                  - clientTLSSecretRef
                  - serverTLSSecretRef
                  - tlsCASecretRef
                  type: object
              type: object
            labels:
              additionalProperties:
                type: string
              type: object
            priorityClassName:
              description: PriorityClassName is the name of a priority class that
                shall be used for the etcd pods.
              type: string
            replicas:
              type: integer
            selector:
              description: 'selector is a label query over pods that should match
                the replica count. It must match the pod template''s labels. More
                info: https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/#label-selectors'
              properties:
                matchExpressions:
                  description: matchExpressions is a list of label selector requirements.
                    The requirements are ANDed.
                  items:
                    description: A label selector requirement is a selector that contains
                      values, a key, and an operator that relates the key and values.
                    properties:
                      key:
                        description: key is the label key that the selector applies
                          to.
                        type: string
                      operator:
                        description: operator represents a key's relationship to a
                          set of values. Valid operators are In, NotIn, Exists and
                          DoesNotExist.
                        type: string
                      values:
                        description: values is an array of string values. If the operator
                          is In or NotIn, the values array must be non-empty. If the
                          operator is Exists or DoesNotExist, the values array must
                          be empty. This array is replaced during a strategic merge
                          patch.
                        items:
                          type: string
                        type: array
                    required:
                    - key
                    - operator
                    type: object
                  type: array
                matchLabels:
                  additionalProperties:
                    type: string
                  description: matchLabels is a map of {key,value} pairs. A single
                    {key,value} in the matchLabels map is equivalent to an element
                    of matchExpressions, whose key field is "key", the operator is
                    "In", and the values array contains only "value". The requirements
                    are ANDed.
                  type: object
              type: object
            storageCapacity:
              description: StorageCapacity defines the size of persistent volume.
              type: string
            storageClass:
              description: 'StorageClass defines the name of the StorageClass required
                by the claim. More info: https://kubernetes.io/docs/concepts/storage/persistent-volumes#class-1'
              type: string
            volumeClaimTemplate:
              description: VolumeClaimTemplate defines the volume claim template to
                be created
              type: string
          required:
          - backup
          - etcd
          - replicas
          - selector
          type: object
        status:
          description: EtcdStatus defines the observed state of Etcd
          properties:
            conditions:
              items:
                description: Condition holds the information about the state of a
                  resource.
                properties:
                  lastTransitionTime:
                    description: Last time the condition transitioned from one status
                      to another.
                    format: date-time
                    type: string
                  lastUpdateTime:
                    description: Last time the condition was updated.
                    format: date-time
                    type: string
                  message:
                    description: A human readable message indicating details about
                      the transition.
                    type: string
                  reason:
                    description: The reason for the condition's last transition.
                    type: string
                  status:
                    description: Status of the condition, one of True, False, Unknown.
                    type: string
                  type:
                    description: Type of the Etcd condition.
                    type: string
                type: object
              type: array
            currentReplicas:
              format: int32
              type: integer
            etcd:
              description: CrossVersionObjectReference contains enough information
                to let you identify the referred resource.
              properties:
                apiVersion:
                  description: API version of the referent
                  type: string
                kind:
                  description: Kind of the referent
                  type: string
                name:
                  description: Name of the referent
                  type: string
              type: object
            labelSelector:
              description: selector is a label query over pods that should match the
                replica count. It must match the pod template's labels.
              properties:
                matchExpressions:
                  description: matchExpressions is a list of label selector requirements.
                    The requirements are ANDed.
                  items:
                    description: A label selector requirement is a selector that contains
                      values, a key, and an operator that relates the key and values.
                    properties:
                      key:
                        description: key is the label key that the selector applies
                          to.
                        type: string
                      operator:
                        description: operator represents a key's relationship to a
                          set of values. Valid operators are In, NotIn, Exists and
                          DoesNotExist.
                        type: string
                      values:
                        description: values is an array of string values. If the operator
                          is In or NotIn, the values array must be non-empty. If the
                          operator is Exists or DoesNotExist, the values array must
                          be empty. This array is replaced during a strategic merge
                          patch.
                        items:
                          type: string
                        type: array
                    required:
                    - key
                    - operator
                    type: object
                  type: array
                matchLabels:
                  additionalProperties:
                    type: string
                  description: matchLabels is a map of {key,value} pairs. A single
                    {key,value} in the matchLabels map is equivalent to an element
                    of matchExpressions, whose key field is "key", the operator is
                    "In", and the values array contains only "value". The requirements
                    are ANDed.
                  type: object
              type: object
            lastError:
              type: string
            observedGeneration:
              description: ObservedGeneration is the most recent generation observed
                for this resource.
              format: int64
              type: integer
            ready:
              type: boolean
            readyReplicas:
              format: int32
              type: integer
            replicas:
              format: int32
              type: integer
            serviceName:
              type: string
            updatedReplicas:
              format: int32
              type: integer
          type: object
      type: object
  additionalPrinterColumns:
  - name: Ready
    type: string
    JSONPath: .status.ready
  - name: Age
    type: date
    JSONPath: .metadata.creationTimestamp
  version: v1alpha1
  versions:
  - name: v1alpha1
    served: true
    storage: true
`
)
