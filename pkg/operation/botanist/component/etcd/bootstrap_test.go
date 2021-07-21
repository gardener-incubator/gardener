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

package etcd_test

import (
	"context"
	"fmt"
	"time"

	mockclient "github.com/gardener/gardener/pkg/mock/controller-runtime/client"
	"github.com/gardener/gardener/pkg/operation/botanist/component"
	. "github.com/gardener/gardener/pkg/operation/botanist/component/etcd"
	gutil "github.com/gardener/gardener/pkg/utils/gardener"
	kutil "github.com/gardener/gardener/pkg/utils/kubernetes"
	. "github.com/gardener/gardener/pkg/utils/test/matchers"

	"github.com/Masterminds/semver"
	druidv1alpha1 "github.com/gardener/etcd-druid/api/v1alpha1"
	resourcesv1alpha1 "github.com/gardener/gardener-resource-manager/api/resources/v1alpha1"
	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Etcd", func() {
	var (
		ctrl         *gomock.Controller
		c            *mockclient.MockClient
		bootstrapper component.DeployWaiter

		ctx                       = context.TODO()
		fakeErr                   = fmt.Errorf("fake error")
		namespace                 = "shoot--foo--bar"
		seedVersion               = semver.MustParse("1.17.0")
		etcdDruidImage            = "etcd/druid:1.2.3"
		imageVectorOverwriteEmpty *string
		imageVectorOverwriteFull  = pointer.String("some overwrite")

		managedResourceName       = "etcd-druid"
		managedResourceSecretName = "managedresource-" + managedResourceName
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		c = mockclient.NewMockClient(ctrl)
		bootstrapper = NewBootstrapper(c, namespace, etcdDruidImage, seedVersion, imageVectorOverwriteEmpty)
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	Describe("#Deploy", func() {
		var (
			serviceAccountYAML = `apiVersion: v1
kind: ServiceAccount
metadata:
  creationTimestamp: null
  labels:
    gardener.cloud/role: etcd-druid
  name: etcd-druid
  namespace: ` + namespace + `
`
			clusterRoleYAML = `apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  creationTimestamp: null
  labels:
    gardener.cloud/role: etcd-druid
  name: gardener.cloud:system:etcd-druid
rules:
- apiGroups:
  - ""
  resources:
  - pods
  verbs:
  - list
  - watch
  - delete
- apiGroups:
  - ""
  resources:
  - secrets
  - endpoints
  verbs:
  - get
  - list
  - patch
  - update
  - watch
- apiGroups:
  - ""
  resources:
  - events
  verbs:
  - create
  - get
  - list
  - watch
  - patch
  - update
- apiGroups:
  - ""
  - apps
  resources:
  - services
  - configmaps
  - statefulsets
  verbs:
  - get
  - list
  - patch
  - update
  - watch
  - create
  - delete
- apiGroups:
  - druid.gardener.cloud
  resources:
  - etcds
  verbs:
  - get
  - list
  - watch
  - update
  - patch
- apiGroups:
  - druid.gardener.cloud
  resources:
  - etcds/status
  - etcds/finalizers
  verbs:
  - get
  - update
  - patch
  - create
- apiGroups:
  - coordination.k8s.io
  resources:
  - leases
  verbs:
  - create
- apiGroups:
  - coordination.k8s.io
  resourceNames:
  - druid-leader-election
  resources:
  - leases
  verbs:
  - get
  - update
  - patch
- apiGroups:
  - ""
  resources:
  - persistentvolumeclaims
  verbs:
  - get
  - list
  - watch
- apiGroups:
  - batch
  resources:
  - cronjobs
  verbs:
  - get
  - list
  - patch
  - update
  - watch
  - create
  - delete
`
			clusterRoleBindingYAML = `apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  creationTimestamp: null
  labels:
    gardener.cloud/role: etcd-druid
  name: gardener.cloud:system:etcd-druid
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: gardener.cloud:system:etcd-druid
subjects:
- kind: ServiceAccount
  name: etcd-druid
  namespace: ` + namespace + `
`
			vpaYAML = `apiVersion: autoscaling.k8s.io/v1beta2
kind: VerticalPodAutoscaler
metadata:
  creationTimestamp: null
  labels:
    gardener.cloud/role: etcd-druid
  name: etcd-druid-vpa
  namespace: ` + namespace + `
spec:
  resourcePolicy:
    containerPolicies:
    - containerName: '*'
      minAllowed:
        cpu: 50m
        memory: 100M
  targetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: etcd-druid
  updatePolicy:
    updateMode: Auto
status: {}
`
			configMapImageVectorOverwriteYAML = `apiVersion: v1
data:
  images_overwrite.yaml: ` + *imageVectorOverwriteFull + `
kind: ConfigMap
metadata:
  creationTimestamp: null
  labels:
    gardener.cloud/role: etcd-druid
  name: etcd-druid-imagevector-overwrite
  namespace: ` + namespace + `
`

			deploymentWithoutImageVectorOverwriteYAML = `apiVersion: apps/v1
kind: Deployment
metadata:
  creationTimestamp: null
  labels:
    gardener.cloud/role: etcd-druid
  name: etcd-druid
  namespace: ` + namespace + `
spec:
  replicas: 1
  revisionHistoryLimit: 1
  selector:
    matchLabels:
      gardener.cloud/role: etcd-druid
  strategy: {}
  template:
    metadata:
      creationTimestamp: null
      labels:
        gardener.cloud/role: etcd-druid
    spec:
      containers:
      - command:
        - /bin/etcd-druid
        - --enable-leader-election=true
        - --ignore-operation-annotation=false
        - --workers=50
        image: ` + etcdDruidImage + `
        imagePullPolicy: IfNotPresent
        name: etcd-druid
        ports:
        - containerPort: 9569
        resources:
          limits:
            cpu: 300m
            memory: 512Mi
          requests:
            cpu: 50m
            memory: 128Mi
      serviceAccountName: etcd-druid
status: {}
`
			deploymentWithImageVectorOverwriteYAML = `apiVersion: apps/v1
kind: Deployment
metadata:
  creationTimestamp: null
  labels:
    gardener.cloud/role: etcd-druid
  name: etcd-druid
  namespace: ` + namespace + `
spec:
  replicas: 1
  revisionHistoryLimit: 1
  selector:
    matchLabels:
      gardener.cloud/role: etcd-druid
  strategy: {}
  template:
    metadata:
      annotations:
        checksum/configmap-imagevector-overwrite: a131f53775a6f537386a63d22cf474b338bbab6d55e439b884c7dea9e148933d
      creationTimestamp: null
      labels:
        gardener.cloud/role: etcd-druid
    spec:
      containers:
      - command:
        - /bin/etcd-druid
        - --enable-leader-election=true
        - --ignore-operation-annotation=false
        - --workers=50
        env:
        - name: IMAGEVECTOR_OVERWRITE
          value: /charts_overwrite/images_overwrite.yaml
        image: ` + etcdDruidImage + `
        imagePullPolicy: IfNotPresent
        name: etcd-druid
        ports:
        - containerPort: 9569
        resources:
          limits:
            cpu: 300m
            memory: 512Mi
          requests:
            cpu: 50m
            memory: 128Mi
        volumeMounts:
        - mountPath: /charts_overwrite
          name: imagevector-overwrite
          readOnly: true
      serviceAccountName: etcd-druid
      volumes:
      - configMap:
          name: etcd-druid-imagevector-overwrite
        name: imagevector-overwrite
status: {}
`

			managedResourceSecret *corev1.Secret
			managedResource       *resourcesv1alpha1.ManagedResource
		)

		BeforeEach(func() {
			managedResourceSecret = &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      managedResourceSecretName,
					Namespace: namespace,
				},
				Type: corev1.SecretTypeOpaque,
				Data: map[string][]byte{
					"serviceaccount__shoot--foo--bar__etcd-druid.yaml":            []byte(serviceAccountYAML),
					"clusterrole____gardener.cloud_system_etcd-druid.yaml":        []byte(clusterRoleYAML),
					"clusterrolebinding____gardener.cloud_system_etcd-druid.yaml": []byte(clusterRoleBindingYAML),
					"verticalpodautoscaler__shoot--foo--bar__etcd-druid-vpa.yaml": []byte(vpaYAML),
					"deployment__shoot--foo--bar__etcd-druid.yaml":                []byte(deploymentWithoutImageVectorOverwriteYAML),
					"crd.yaml": []byte(crd),
				},
			}
			managedResource = &resourcesv1alpha1.ManagedResource{
				ObjectMeta: metav1.ObjectMeta{
					Name:      managedResourceName,
					Namespace: namespace,
				},
				Spec: resourcesv1alpha1.ManagedResourceSpec{
					SecretRefs: []corev1.LocalObjectReference{
						{Name: managedResourceSecretName},
					},
					Class:       pointer.String("seed"),
					KeepObjects: pointer.Bool(false),
				},
			}
		})

		It("should fail because the managed resource secret cannot be updated", func() {
			gomock.InOrder(
				c.EXPECT().Get(ctx, kutil.Key(namespace, managedResourceSecretName), gomock.AssignableToTypeOf(&corev1.Secret{})),
				c.EXPECT().Update(ctx, gomock.AssignableToTypeOf(&corev1.Secret{})).Return(fakeErr),
			)

			Expect(bootstrapper.Deploy(ctx)).To(MatchError(fakeErr))
		})

		It("should fail because the managed resource cannot be updated", func() {
			gomock.InOrder(
				c.EXPECT().Get(ctx, kutil.Key(namespace, managedResourceSecretName), gomock.AssignableToTypeOf(&corev1.Secret{})),
				c.EXPECT().Update(ctx, gomock.AssignableToTypeOf(&corev1.Secret{})),
				c.EXPECT().Get(ctx, kutil.Key(namespace, managedResourceName), gomock.AssignableToTypeOf(&resourcesv1alpha1.ManagedResource{})),
				c.EXPECT().Update(ctx, gomock.AssignableToTypeOf(&resourcesv1alpha1.ManagedResource{})).Return(fakeErr),
			)

			Expect(bootstrapper.Deploy(ctx)).To(MatchError(fakeErr))
		})

		It("should successfully deploy all the resources (w/o image vector overwrite)", func() {
			gomock.InOrder(
				c.EXPECT().Get(ctx, kutil.Key(namespace, managedResourceSecretName), gomock.AssignableToTypeOf(&corev1.Secret{})),
				c.EXPECT().Update(ctx, gomock.AssignableToTypeOf(&corev1.Secret{})).Do(func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) {
					Expect(obj).To(DeepEqual(managedResourceSecret))
				}),
				c.EXPECT().Get(ctx, kutil.Key(namespace, managedResourceName), gomock.AssignableToTypeOf(&resourcesv1alpha1.ManagedResource{})),
				c.EXPECT().Update(ctx, gomock.AssignableToTypeOf(&resourcesv1alpha1.ManagedResource{})).Do(func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) {
					Expect(obj).To(DeepEqual(managedResource))
				}),
			)

			Expect(bootstrapper.Deploy(ctx)).To(Succeed())
		})

		It("should successfully deploy all the resources (w/ image vector overwrite)", func() {
			bootstrapper = NewBootstrapper(c, namespace, etcdDruidImage, seedVersion, imageVectorOverwriteFull)

			managedResourceSecret.Data["configmap__shoot--foo--bar__etcd-druid-imagevector-overwrite.yaml"] = []byte(configMapImageVectorOverwriteYAML)
			managedResourceSecret.Data["deployment__shoot--foo--bar__etcd-druid.yaml"] = []byte(deploymentWithImageVectorOverwriteYAML)

			gomock.InOrder(
				c.EXPECT().Get(ctx, kutil.Key(namespace, managedResourceSecretName), gomock.AssignableToTypeOf(&corev1.Secret{})),
				c.EXPECT().Update(ctx, gomock.AssignableToTypeOf(&corev1.Secret{})).Do(func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) {
					Expect(obj).To(DeepEqual(managedResourceSecret))
				}),
				c.EXPECT().Get(ctx, kutil.Key(namespace, managedResourceName), gomock.AssignableToTypeOf(&resourcesv1alpha1.ManagedResource{})),
				c.EXPECT().Update(ctx, gomock.AssignableToTypeOf(&resourcesv1alpha1.ManagedResource{})).Do(func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) {
					Expect(obj).To(DeepEqual(managedResource))
				}),
			)

			Expect(bootstrapper.Deploy(ctx)).To(Succeed())
		})
	})

	Describe("#Wait", func() {
		It("should fail because it cannot be checked if the managed resource became healthy", func() {
			oldTimeout := TimeoutWaitForManagedResource
			defer func() { TimeoutWaitForManagedResource = oldTimeout }()
			TimeoutWaitForManagedResource = time.Millisecond

			c.EXPECT().Get(gomock.Any(), kutil.Key(namespace, managedResourceName), gomock.AssignableToTypeOf(&resourcesv1alpha1.ManagedResource{})).Return(fakeErr)

			Expect(bootstrapper.Wait(ctx)).To(MatchError(fakeErr))
		})

		It("should fail because the managed resource doesn't become healthy", func() {
			oldTimeout := TimeoutWaitForManagedResource
			defer func() { TimeoutWaitForManagedResource = oldTimeout }()
			TimeoutWaitForManagedResource = time.Millisecond

			c.EXPECT().Get(gomock.Any(), kutil.Key(namespace, managedResourceName), gomock.AssignableToTypeOf(&resourcesv1alpha1.ManagedResource{})).DoAndReturn(
				func(ctx context.Context, _ client.ObjectKey, obj client.Object) error {
					(&resourcesv1alpha1.ManagedResource{
						ObjectMeta: metav1.ObjectMeta{
							Generation: 1,
						},
						Status: resourcesv1alpha1.ManagedResourceStatus{
							ObservedGeneration: 1,
							Conditions: []resourcesv1alpha1.ManagedResourceCondition{
								{
									Type:   resourcesv1alpha1.ResourcesApplied,
									Status: resourcesv1alpha1.ConditionFalse,
								},
								{
									Type:   resourcesv1alpha1.ResourcesHealthy,
									Status: resourcesv1alpha1.ConditionFalse,
								},
							},
						},
					}).DeepCopyInto(obj.(*resourcesv1alpha1.ManagedResource))
					return nil
				},
			).AnyTimes()

			Expect(bootstrapper.Wait(ctx)).To(MatchError(ContainSubstring("is not healthy")))
		})

		It("should successfully wait for the managed resource to become healthy", func() {
			oldTimeout := TimeoutWaitForManagedResource
			defer func() { TimeoutWaitForManagedResource = oldTimeout }()
			TimeoutWaitForManagedResource = time.Millisecond

			c.EXPECT().Get(gomock.Any(), kutil.Key(namespace, managedResourceName), gomock.AssignableToTypeOf(&resourcesv1alpha1.ManagedResource{})).DoAndReturn(
				func(ctx context.Context, _ client.ObjectKey, obj client.Object) error {
					(&resourcesv1alpha1.ManagedResource{
						ObjectMeta: metav1.ObjectMeta{
							Generation: 1,
						},
						Status: resourcesv1alpha1.ManagedResourceStatus{
							ObservedGeneration: 1,
							Conditions: []resourcesv1alpha1.ManagedResourceCondition{
								{
									Type:   resourcesv1alpha1.ResourcesApplied,
									Status: resourcesv1alpha1.ConditionTrue,
								},
								{
									Type:   resourcesv1alpha1.ResourcesHealthy,
									Status: resourcesv1alpha1.ConditionTrue,
								},
							},
						},
					}).DeepCopyInto(obj.(*resourcesv1alpha1.ManagedResource))
					return nil
				},
			)

			Expect(bootstrapper.Wait(ctx)).To(Succeed())
		})
	})

	Context("cleanup", func() {
		var (
			secret          = &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: managedResourceSecretName}}
			managedResource = &resourcesv1alpha1.ManagedResource{
				ObjectMeta: metav1.ObjectMeta{
					Name:      managedResourceName,
					Namespace: namespace,
				},
			}

			timeNowFunc = func() time.Time { return time.Time{} }
		)

		Describe("#Destroy", func() {
			It("should fail when the etcd listing fails", func() {
				c.EXPECT().List(ctx, gomock.AssignableToTypeOf(&druidv1alpha1.EtcdList{})).Return(fakeErr)

				Expect(bootstrapper.Destroy(ctx)).To(MatchError(fakeErr))
			})

			It("should succeed when isNoMatch error is returned", func() {
				noMatchError := &meta.NoKindMatchError{}
				c.EXPECT().List(ctx, gomock.AssignableToTypeOf(&druidv1alpha1.EtcdList{})).Return(noMatchError)

				c.EXPECT().Patch(gomock.Any(), gomock.AssignableToTypeOf(&apiextensionsv1beta1.CustomResourceDefinition{}), gomock.Any())
				c.EXPECT().Delete(gomock.Any(), gomock.Any())
				c.EXPECT().Delete(gomock.Any(), gomock.Any())

				Expect(bootstrapper.Destroy(ctx)).To(Succeed())
			})

			It("should suceed when NotFoundError is returned", func() {
				notFoundError := apierrors.NewNotFound(schema.GroupResource{}, "etcd")
				c.EXPECT().List(ctx, gomock.AssignableToTypeOf(&druidv1alpha1.EtcdList{})).Return(notFoundError)

				c.EXPECT().Patch(gomock.Any(), gomock.AssignableToTypeOf(&apiextensionsv1beta1.CustomResourceDefinition{}), gomock.Any())
				c.EXPECT().Delete(gomock.Any(), gomock.Any())
				c.EXPECT().Delete(gomock.Any(), gomock.Any())

				Expect(bootstrapper.Destroy(ctx)).To(Succeed())
			})

			It("should fail when there are etcd resources left", func() {
				c.EXPECT().List(ctx, gomock.AssignableToTypeOf(&druidv1alpha1.EtcdList{})).DoAndReturn(
					func(ctx context.Context, list client.ObjectList, _ ...client.ListOptions) error {
						(&druidv1alpha1.EtcdList{
							Items: []druidv1alpha1.Etcd{{}},
						}).DeepCopyInto(list.(*druidv1alpha1.EtcdList))
						return nil
					},
				)

				Expect(bootstrapper.Destroy(ctx)).To(MatchError(ContainSubstring("because there are still druidv1alpha1.Etcd resources left in the cluster")))
			})

			It("should fail when the deletion confirmation fails", func() {
				gomock.InOrder(
					c.EXPECT().List(ctx, gomock.AssignableToTypeOf(&druidv1alpha1.EtcdList{})),
					c.EXPECT().Patch(ctx, gomock.AssignableToTypeOf(&apiextensionsv1beta1.CustomResourceDefinition{}), gomock.Any()).Return(fakeErr),
				)

				Expect(bootstrapper.Destroy(ctx)).To(MatchError(fakeErr))
			})

			It("should fail when the managed resource deletion fails", func() {
				gomock.InOrder(
					c.EXPECT().List(ctx, gomock.AssignableToTypeOf(&druidv1alpha1.EtcdList{})),
					c.EXPECT().Patch(ctx, gomock.AssignableToTypeOf(&apiextensionsv1beta1.CustomResourceDefinition{}), gomock.Any()),
					c.EXPECT().Delete(ctx, managedResource).Return(fakeErr),
				)

				Expect(bootstrapper.Destroy(ctx)).To(MatchError(fakeErr))
			})

			It("should fail when the secret deletion fails", func() {
				gomock.InOrder(
					c.EXPECT().List(ctx, gomock.AssignableToTypeOf(&druidv1alpha1.EtcdList{})),
					c.EXPECT().Patch(ctx, gomock.AssignableToTypeOf(&apiextensionsv1beta1.CustomResourceDefinition{}), gomock.Any()),
					c.EXPECT().Delete(ctx, managedResource),
					c.EXPECT().Delete(ctx, secret).Return(fakeErr),
				)

				Expect(bootstrapper.Destroy(ctx)).To(MatchError(fakeErr))
			})

			It("should successfully delete all resources", func() {
				gomock.InOrder(
					c.EXPECT().List(ctx, gomock.AssignableToTypeOf(&druidv1alpha1.EtcdList{})),
					c.EXPECT().Patch(ctx, gomock.AssignableToTypeOf(&apiextensionsv1beta1.CustomResourceDefinition{}), gomock.Any()),
					c.EXPECT().Delete(ctx, managedResource),
					c.EXPECT().Delete(ctx, secret),
				)

				Expect(bootstrapper.Destroy(ctx)).To(Succeed())
			})
		})

		Describe("#WaitCleanup", func() {
			It("should fail when the wait for the managed resource deletion fails", func() {
				oldTimeNow := gutil.TimeNow
				defer func() { gutil.TimeNow = oldTimeNow }()
				gutil.TimeNow = timeNowFunc

				c.EXPECT().Get(gomock.Any(), kutil.Key(namespace, managedResourceName), gomock.AssignableToTypeOf(&resourcesv1alpha1.ManagedResource{})).Return(fakeErr)

				Expect(bootstrapper.WaitCleanup(ctx)).To(MatchError(fakeErr))
			})

			It("should fail when the wait for the managed resource deletion times out", func() {
				oldTimeNow := gutil.TimeNow
				defer func() { gutil.TimeNow = oldTimeNow }()
				gutil.TimeNow = timeNowFunc

				oldTimeout := TimeoutWaitForManagedResource
				defer func() { TimeoutWaitForManagedResource = oldTimeout }()
				TimeoutWaitForManagedResource = time.Millisecond

				c.EXPECT().Get(gomock.Any(), kutil.Key(namespace, managedResourceName), gomock.AssignableToTypeOf(&resourcesv1alpha1.ManagedResource{})).AnyTimes()

				Expect(bootstrapper.WaitCleanup(ctx)).To(MatchError(ContainSubstring("still exists")))
			})

			It("should successfully delete all resources", func() {
				oldTimeNow := gutil.TimeNow
				defer func() { gutil.TimeNow = oldTimeNow }()
				gutil.TimeNow = timeNowFunc

				c.EXPECT().Get(gomock.Any(), kutil.Key(namespace, managedResourceName), gomock.AssignableToTypeOf(&resourcesv1alpha1.ManagedResource{})).Return(apierrors.NewNotFound(schema.GroupResource{}, ""))

				Expect(bootstrapper.WaitCleanup(ctx)).To(Succeed())
			})
		})
	})
})

const crd = `apiVersion: apiextensions.k8s.io/v1beta1
kind: CustomResourceDefinition
metadata:
  name: etcds.druid.gardener.cloud
  annotations:
    controller-gen.kubebuilder.io/version: v0.2.4
  labels:
    gardener.cloud/deletion-protected: "true"
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
