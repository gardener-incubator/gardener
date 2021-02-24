// Copyright (c) 2021 SAP SE or an SAP affiliate company. All rights reserved. This file is licensed under the Apache Software License, v. 2 except as noted otherwise in the LICENSE file
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

package graph

import (
	"context"

	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	"github.com/gardener/gardener/pkg/client/kubernetes"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	toolscache "k8s.io/client-go/tools/cache"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/cache/informertest"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllertest"
	logzap "sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var _ = Describe("graph", func() {
	var (
		ctx = context.TODO()

		fakeInformerSeed          *controllertest.FakeInformer
		fakeInformerShoot         *controllertest.FakeInformer
		fakeInformerProject       *controllertest.FakeInformer
		fakeInformerBackupBucket  *controllertest.FakeInformer
		fakeInformerBackupEntry   *controllertest.FakeInformer
		fakeInformerSecretBinding *controllertest.FakeInformer
		fakeInformers             *informertest.FakeInformers

		logger logr.Logger
		graph  *graph

		seed1                *gardencorev1beta1.Seed
		seed1SecretRef       = corev1.SecretReference{Namespace: "foo", Name: "bar"}
		seed1BackupSecretRef = corev1.SecretReference{Namespace: "bar", Name: "baz"}

		shoot1                        *gardencorev1beta1.Shoot
		shoot1DNSProvider1            = gardencorev1beta1.DNSProvider{SecretName: pointer.StringPtr("dnssecret1")}
		shoot1DNSProvider2            = gardencorev1beta1.DNSProvider{SecretName: pointer.StringPtr("dnssecret2")}
		shoot1AuditPolicyConfigMapRef = corev1.ObjectReference{Name: "auditpolicy1"}
		shoot1Resource1               = autoscalingv1.CrossVersionObjectReference{APIVersion: "foo", Kind: "bar", Name: "resource1"}
		shoot1Resource2               = autoscalingv1.CrossVersionObjectReference{APIVersion: "v1", Kind: "Secret", Name: "resource2"}

		project1 *gardencorev1beta1.Project

		backupBucket1          *gardencorev1beta1.BackupBucket
		backupBucket1SecretRef = corev1.SecretReference{Namespace: "baz", Name: "foo"}

		backupEntry1 *gardencorev1beta1.BackupEntry

		secretBinding1          *gardencorev1beta1.SecretBinding
		secretBinding1SecretRef = corev1.SecretReference{Namespace: "foobar", Name: "bazfoo"}
	)

	BeforeEach(func() {
		scheme := kubernetes.GardenScheme
		Expect(metav1.AddMetaToScheme(scheme)).To(Succeed())

		fakeInformerSeed = &controllertest.FakeInformer{}
		fakeInformerShoot = &controllertest.FakeInformer{}
		fakeInformerProject = &controllertest.FakeInformer{}
		fakeInformerBackupBucket = &controllertest.FakeInformer{}
		fakeInformerBackupEntry = &controllertest.FakeInformer{}
		fakeInformerSecretBinding = &controllertest.FakeInformer{}

		fakeInformers = &informertest.FakeInformers{
			Scheme: scheme,
			InformersByGVK: map[schema.GroupVersionKind]toolscache.SharedIndexInformer{
				gardencorev1beta1.SchemeGroupVersion.WithKind("Seed"):          fakeInformerSeed,
				gardencorev1beta1.SchemeGroupVersion.WithKind("Shoot"):         fakeInformerShoot,
				gardencorev1beta1.SchemeGroupVersion.WithKind("Project"):       fakeInformerProject,
				gardencorev1beta1.SchemeGroupVersion.WithKind("BackupBucket"):  fakeInformerBackupBucket,
				gardencorev1beta1.SchemeGroupVersion.WithKind("BackupEntry"):   fakeInformerBackupEntry,
				gardencorev1beta1.SchemeGroupVersion.WithKind("SecretBinding"): fakeInformerSecretBinding,
			},
		}

		logger = logzap.New(logzap.WriteTo(GinkgoWriter))
		graph = New(logger)
		Expect(graph.Setup(ctx, fakeInformers)).To(Succeed())

		seed1 = &gardencorev1beta1.Seed{
			ObjectMeta: metav1.ObjectMeta{Name: "seed1"},
			Spec: gardencorev1beta1.SeedSpec{
				SecretRef: &seed1SecretRef,
				Backup:    &gardencorev1beta1.SeedBackup{SecretRef: seed1BackupSecretRef},
			},
		}

		shoot1 = &gardencorev1beta1.Shoot{
			ObjectMeta: metav1.ObjectMeta{Name: "shoot1", Namespace: "namespace1"},
			Spec: gardencorev1beta1.ShootSpec{
				CloudProfileName: "cloudprofile1",
				DNS: &gardencorev1beta1.DNS{
					Providers: []gardencorev1beta1.DNSProvider{shoot1DNSProvider1, shoot1DNSProvider2},
				},
				Kubernetes: gardencorev1beta1.Kubernetes{
					KubeAPIServer: &gardencorev1beta1.KubeAPIServerConfig{
						AuditConfig: &gardencorev1beta1.AuditConfig{
							AuditPolicy: &gardencorev1beta1.AuditPolicy{
								ConfigMapRef: &shoot1AuditPolicyConfigMapRef,
							},
						},
					},
				},
				Resources:         []gardencorev1beta1.NamedResourceReference{{ResourceRef: shoot1Resource1}, {ResourceRef: shoot1Resource2}},
				SecretBindingName: "secretbinding1",
				SeedName:          &seed1.Name,
			},
		}

		project1 = &gardencorev1beta1.Project{
			ObjectMeta: metav1.ObjectMeta{Name: "project1"},
			Spec: gardencorev1beta1.ProjectSpec{
				Namespace: pointer.StringPtr("projectnamespace1"),
			},
		}

		backupBucket1 = &gardencorev1beta1.BackupBucket{
			ObjectMeta: metav1.ObjectMeta{Name: "backupbucket1"},
			Spec: gardencorev1beta1.BackupBucketSpec{
				SecretRef: backupBucket1SecretRef,
				SeedName:  &seed1.Name,
			},
		}

		backupEntry1 = &gardencorev1beta1.BackupEntry{
			ObjectMeta: metav1.ObjectMeta{Name: "backupentry1", Namespace: "backupentry1namespace"},
			Spec: gardencorev1beta1.BackupEntrySpec{
				BucketName: backupBucket1.Name,
				SeedName:   &seed1.Name,
			},
		}

		secretBinding1 = &gardencorev1beta1.SecretBinding{
			ObjectMeta: metav1.ObjectMeta{Name: "secretbinding1", Namespace: "sb1namespace"},
			SecretRef:  secretBinding1SecretRef,
		}
	})

	It("should behave as expected for gardencorev1beta1.Seed", func() {
		By("add")
		fakeInformerSeed.Add(seed1)
		Expect(graph.graph.Nodes().Len()).To(Equal(3))
		Expect(graph.graph.Edges().Len()).To(Equal(2))
		Expect(graph.HasPathFrom(VertexTypeSecret, seed1SecretRef.Namespace, seed1SecretRef.Name, VertexTypeSeed, "", seed1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeSecret, seed1BackupSecretRef.Namespace, seed1BackupSecretRef.Name, VertexTypeSeed, "", seed1.Name)).To(BeTrue())

		By("update (irrelevant change)")
		seed1Copy := seed1.DeepCopy()
		seed1.Spec.Provider.Type = "providertype"
		fakeInformerSeed.Update(seed1Copy, seed1)
		Expect(graph.graph.Nodes().Len()).To(Equal(3))
		Expect(graph.graph.Edges().Len()).To(Equal(2))
		Expect(graph.HasPathFrom(VertexTypeSecret, seed1SecretRef.Namespace, seed1SecretRef.Name, VertexTypeSeed, "", seed1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeSecret, seed1BackupSecretRef.Namespace, seed1BackupSecretRef.Name, VertexTypeSeed, "", seed1.Name)).To(BeTrue())

		By("update (remove secret ref)")
		seed1Copy = seed1.DeepCopy()
		seed1.Spec.SecretRef = nil
		fakeInformerSeed.Update(seed1Copy, seed1)
		Expect(graph.graph.Nodes().Len()).To(Equal(2))
		Expect(graph.graph.Edges().Len()).To(Equal(1))
		Expect(graph.HasPathFrom(VertexTypeSecret, seed1SecretRef.Namespace, seed1SecretRef.Name, VertexTypeSeed, "", seed1.Name)).To(BeFalse())
		Expect(graph.HasPathFrom(VertexTypeSecret, seed1BackupSecretRef.Namespace, seed1BackupSecretRef.Name, VertexTypeSeed, "", seed1.Name)).To(BeTrue())

		By("update (remove backup secret ref)")
		seed1Copy = seed1.DeepCopy()
		seed1.Spec.Backup = nil
		fakeInformerSeed.Update(seed1Copy, seed1)
		Expect(graph.graph.Nodes().Len()).To(BeZero())
		Expect(graph.graph.Edges().Len()).To(BeZero())
		Expect(graph.HasPathFrom(VertexTypeSecret, seed1SecretRef.Namespace, seed1SecretRef.Name, VertexTypeSeed, "", seed1.Name)).To(BeFalse())
		Expect(graph.HasPathFrom(VertexTypeSecret, seed1BackupSecretRef.Namespace, seed1BackupSecretRef.Name, VertexTypeSeed, "", seed1.Name)).To(BeFalse())

		By("update (both secret refs)")
		seed1Copy = seed1.DeepCopy()
		seed1.Spec.Backup = &gardencorev1beta1.SeedBackup{SecretRef: seed1BackupSecretRef}
		seed1.Spec.SecretRef = &seed1SecretRef
		fakeInformerSeed.Update(seed1Copy, seed1)
		Expect(graph.graph.Nodes().Len()).To(Equal(3))
		Expect(graph.graph.Edges().Len()).To(Equal(2))
		Expect(graph.HasPathFrom(VertexTypeSecret, seed1SecretRef.Namespace, seed1SecretRef.Name, VertexTypeSeed, "", seed1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeSecret, seed1BackupSecretRef.Namespace, seed1BackupSecretRef.Name, VertexTypeSeed, "", seed1.Name)).To(BeTrue())

		By("delete")
		fakeInformerSeed.Delete(seed1)
		Expect(graph.graph.Nodes().Len()).To(BeZero())
		Expect(graph.graph.Edges().Len()).To(BeZero())
		Expect(graph.HasPathFrom(VertexTypeSecret, seed1SecretRef.Namespace, seed1SecretRef.Name, VertexTypeSeed, "", seed1.Name)).To(BeFalse())
		Expect(graph.HasPathFrom(VertexTypeSecret, seed1BackupSecretRef.Namespace, seed1BackupSecretRef.Name, VertexTypeSeed, "", seed1.Name)).To(BeFalse())
	})

	It("should behave as expected for gardencorev1beta1.Shoot", func() {
		By("add")
		fakeInformerShoot.Add(shoot1)
		Expect(graph.graph.Nodes().Len()).To(Equal(9))
		Expect(graph.graph.Edges().Len()).To(Equal(8))
		Expect(graph.HasPathFrom(VertexTypeNamespace, "", shoot1.Namespace, VertexTypeShoot, shoot1.Namespace, shoot1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeCloudProfile, "", shoot1.Spec.CloudProfileName, VertexTypeShoot, shoot1.Namespace, shoot1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeSecretBinding, shoot1.Namespace, shoot1.Spec.SecretBindingName, VertexTypeShoot, shoot1.Namespace, shoot1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeConfigMap, shoot1.Namespace, shoot1AuditPolicyConfigMapRef.Name, VertexTypeShoot, shoot1.Namespace, shoot1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeSecret, shoot1.Namespace, *shoot1DNSProvider1.SecretName, VertexTypeShoot, shoot1.Namespace, shoot1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeSecret, shoot1.Namespace, *shoot1DNSProvider2.SecretName, VertexTypeShoot, shoot1.Namespace, shoot1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeSecret, shoot1.Namespace, shoot1Resource2.Name, VertexTypeShoot, shoot1.Namespace, shoot1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeShoot, shoot1.Namespace, shoot1.Name, VertexTypeSeed, "", seed1.Name)).To(BeTrue())

		By("update (cloud profile name)")
		shoot1Copy := shoot1.DeepCopy()
		shoot1.Spec.CloudProfileName = "foo"
		fakeInformerShoot.Update(shoot1Copy, shoot1)
		Expect(graph.graph.Nodes().Len()).To(Equal(9))
		Expect(graph.graph.Edges().Len()).To(Equal(8))
		Expect(graph.HasPathFrom(VertexTypeNamespace, "", shoot1.Namespace, VertexTypeShoot, shoot1.Namespace, shoot1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeCloudProfile, "", shoot1Copy.Spec.CloudProfileName, VertexTypeShoot, shoot1.Namespace, shoot1.Name)).To(BeFalse())
		Expect(graph.HasPathFrom(VertexTypeCloudProfile, "", shoot1.Spec.CloudProfileName, VertexTypeShoot, shoot1.Namespace, shoot1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeSecretBinding, shoot1.Namespace, shoot1.Spec.SecretBindingName, VertexTypeShoot, shoot1.Namespace, shoot1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeConfigMap, shoot1.Namespace, shoot1AuditPolicyConfigMapRef.Name, VertexTypeShoot, shoot1.Namespace, shoot1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeSecret, shoot1.Namespace, *shoot1DNSProvider1.SecretName, VertexTypeShoot, shoot1.Namespace, shoot1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeSecret, shoot1.Namespace, *shoot1DNSProvider2.SecretName, VertexTypeShoot, shoot1.Namespace, shoot1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeSecret, shoot1.Namespace, shoot1Resource2.Name, VertexTypeShoot, shoot1.Namespace, shoot1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeShoot, shoot1.Namespace, shoot1.Name, VertexTypeSeed, "", seed1.Name)).To(BeTrue())

		By("update (secret binding name)")
		shoot1Copy = shoot1.DeepCopy()
		shoot1.Spec.SecretBindingName = "bar"
		fakeInformerShoot.Update(shoot1Copy, shoot1)
		Expect(graph.graph.Nodes().Len()).To(Equal(9))
		Expect(graph.graph.Edges().Len()).To(Equal(8))
		Expect(graph.HasPathFrom(VertexTypeNamespace, "", shoot1.Namespace, VertexTypeShoot, shoot1.Namespace, shoot1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeCloudProfile, "", shoot1.Spec.CloudProfileName, VertexTypeShoot, shoot1.Namespace, shoot1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeSecretBinding, shoot1.Namespace, shoot1Copy.Spec.SecretBindingName, VertexTypeShoot, shoot1.Namespace, shoot1.Name)).To(BeFalse())
		Expect(graph.HasPathFrom(VertexTypeSecretBinding, shoot1.Namespace, shoot1.Spec.SecretBindingName, VertexTypeShoot, shoot1.Namespace, shoot1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeConfigMap, shoot1.Namespace, shoot1AuditPolicyConfigMapRef.Name, VertexTypeShoot, shoot1.Namespace, shoot1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeSecret, shoot1.Namespace, *shoot1DNSProvider1.SecretName, VertexTypeShoot, shoot1.Namespace, shoot1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeSecret, shoot1.Namespace, *shoot1DNSProvider2.SecretName, VertexTypeShoot, shoot1.Namespace, shoot1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeSecret, shoot1.Namespace, shoot1Resource2.Name, VertexTypeShoot, shoot1.Namespace, shoot1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeShoot, shoot1.Namespace, shoot1.Name, VertexTypeSeed, "", seed1.Name)).To(BeTrue())

		By("update (audit policy config map name)")
		shoot1Copy = shoot1.DeepCopy()
		shoot1.Spec.Kubernetes.KubeAPIServer = nil
		fakeInformerShoot.Update(shoot1Copy, shoot1)
		Expect(graph.graph.Nodes().Len()).To(Equal(8))
		Expect(graph.graph.Edges().Len()).To(Equal(7))
		Expect(graph.HasPathFrom(VertexTypeNamespace, "", shoot1.Namespace, VertexTypeShoot, shoot1.Namespace, shoot1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeCloudProfile, "", shoot1.Spec.CloudProfileName, VertexTypeShoot, shoot1.Namespace, shoot1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeSecretBinding, shoot1.Namespace, shoot1.Spec.SecretBindingName, VertexTypeShoot, shoot1.Namespace, shoot1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeConfigMap, shoot1.Namespace, shoot1AuditPolicyConfigMapRef.Name, VertexTypeShoot, shoot1.Namespace, shoot1.Name)).To(BeFalse())
		Expect(graph.HasPathFrom(VertexTypeSecret, shoot1.Namespace, *shoot1DNSProvider1.SecretName, VertexTypeShoot, shoot1.Namespace, shoot1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeSecret, shoot1.Namespace, *shoot1DNSProvider2.SecretName, VertexTypeShoot, shoot1.Namespace, shoot1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeSecret, shoot1.Namespace, shoot1Resource2.Name, VertexTypeShoot, shoot1.Namespace, shoot1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeShoot, shoot1.Namespace, shoot1.Name, VertexTypeSeed, "", seed1.Name)).To(BeTrue())

		By("update (dns provider secrets)")
		shoot1Copy = shoot1.DeepCopy()
		shoot1.Spec.DNS = nil
		fakeInformerShoot.Update(shoot1Copy, shoot1)
		Expect(graph.graph.Nodes().Len()).To(Equal(6))
		Expect(graph.graph.Edges().Len()).To(Equal(5))
		Expect(graph.HasPathFrom(VertexTypeNamespace, "", shoot1.Namespace, VertexTypeShoot, shoot1.Namespace, shoot1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeCloudProfile, "", shoot1.Spec.CloudProfileName, VertexTypeShoot, shoot1.Namespace, shoot1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeSecretBinding, shoot1.Namespace, shoot1.Spec.SecretBindingName, VertexTypeShoot, shoot1.Namespace, shoot1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeConfigMap, shoot1.Namespace, shoot1AuditPolicyConfigMapRef.Name, VertexTypeShoot, shoot1.Namespace, shoot1.Name)).To(BeFalse())
		Expect(graph.HasPathFrom(VertexTypeSecret, shoot1.Namespace, *shoot1DNSProvider1.SecretName, VertexTypeShoot, shoot1.Namespace, shoot1.Name)).To(BeFalse())
		Expect(graph.HasPathFrom(VertexTypeSecret, shoot1.Namespace, *shoot1DNSProvider2.SecretName, VertexTypeShoot, shoot1.Namespace, shoot1.Name)).To(BeFalse())
		Expect(graph.HasPathFrom(VertexTypeSecret, shoot1.Namespace, shoot1Resource2.Name, VertexTypeShoot, shoot1.Namespace, shoot1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeShoot, shoot1.Namespace, shoot1.Name, VertexTypeSeed, "", seed1.Name)).To(BeTrue())

		By("update (resources)")
		shoot1Copy = shoot1.DeepCopy()
		shoot1.Spec.Resources = nil
		fakeInformerShoot.Update(shoot1Copy, shoot1)
		Expect(graph.graph.Nodes().Len()).To(Equal(5))
		Expect(graph.graph.Edges().Len()).To(Equal(4))
		Expect(graph.HasPathFrom(VertexTypeNamespace, "", shoot1.Namespace, VertexTypeShoot, shoot1.Namespace, shoot1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeCloudProfile, "", shoot1.Spec.CloudProfileName, VertexTypeShoot, shoot1.Namespace, shoot1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeSecretBinding, shoot1.Namespace, shoot1.Spec.SecretBindingName, VertexTypeShoot, shoot1.Namespace, shoot1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeConfigMap, shoot1.Namespace, shoot1AuditPolicyConfigMapRef.Name, VertexTypeShoot, shoot1.Namespace, shoot1.Name)).To(BeFalse())
		Expect(graph.HasPathFrom(VertexTypeSecret, shoot1.Namespace, *shoot1DNSProvider1.SecretName, VertexTypeShoot, shoot1.Namespace, shoot1.Name)).To(BeFalse())
		Expect(graph.HasPathFrom(VertexTypeSecret, shoot1.Namespace, *shoot1DNSProvider2.SecretName, VertexTypeShoot, shoot1.Namespace, shoot1.Name)).To(BeFalse())
		Expect(graph.HasPathFrom(VertexTypeSecret, shoot1.Namespace, shoot1Resource2.Name, VertexTypeShoot, shoot1.Namespace, shoot1.Name)).To(BeFalse())
		Expect(graph.HasPathFrom(VertexTypeShoot, shoot1.Namespace, shoot1.Name, VertexTypeSeed, "", seed1.Name)).To(BeTrue())

		By("update (no seed name)")
		shoot1Copy = shoot1.DeepCopy()
		shoot1.Spec.SeedName = nil
		fakeInformerShoot.Update(shoot1Copy, shoot1)
		Expect(graph.graph.Nodes().Len()).To(Equal(4))
		Expect(graph.graph.Edges().Len()).To(Equal(3))
		Expect(graph.HasPathFrom(VertexTypeNamespace, "", shoot1.Namespace, VertexTypeShoot, shoot1.Namespace, shoot1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeCloudProfile, "", shoot1.Spec.CloudProfileName, VertexTypeShoot, shoot1.Namespace, shoot1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeSecretBinding, shoot1.Namespace, shoot1.Spec.SecretBindingName, VertexTypeShoot, shoot1.Namespace, shoot1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeConfigMap, shoot1.Namespace, shoot1AuditPolicyConfigMapRef.Name, VertexTypeShoot, shoot1.Namespace, shoot1.Name)).To(BeFalse())
		Expect(graph.HasPathFrom(VertexTypeSecret, shoot1.Namespace, *shoot1DNSProvider1.SecretName, VertexTypeShoot, shoot1.Namespace, shoot1.Name)).To(BeFalse())
		Expect(graph.HasPathFrom(VertexTypeSecret, shoot1.Namespace, *shoot1DNSProvider2.SecretName, VertexTypeShoot, shoot1.Namespace, shoot1.Name)).To(BeFalse())
		Expect(graph.HasPathFrom(VertexTypeSecret, shoot1.Namespace, shoot1Resource2.Name, VertexTypeShoot, shoot1.Namespace, shoot1.Name)).To(BeFalse())
		Expect(graph.HasPathFrom(VertexTypeShoot, shoot1.Namespace, shoot1.Name, VertexTypeSeed, "", seed1.Name)).To(BeFalse())

		By("update (new seed name)")
		shoot1Copy = shoot1.DeepCopy()
		shoot1.Spec.SeedName = pointer.StringPtr("newseed")
		fakeInformerShoot.Update(shoot1Copy, shoot1)
		Expect(graph.graph.Nodes().Len()).To(Equal(5))
		Expect(graph.graph.Edges().Len()).To(Equal(4))
		Expect(graph.HasPathFrom(VertexTypeNamespace, "", shoot1.Namespace, VertexTypeShoot, shoot1.Namespace, shoot1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeCloudProfile, "", shoot1.Spec.CloudProfileName, VertexTypeShoot, shoot1.Namespace, shoot1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeSecretBinding, shoot1.Namespace, shoot1.Spec.SecretBindingName, VertexTypeShoot, shoot1.Namespace, shoot1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeConfigMap, shoot1.Namespace, shoot1AuditPolicyConfigMapRef.Name, VertexTypeShoot, shoot1.Namespace, shoot1.Name)).To(BeFalse())
		Expect(graph.HasPathFrom(VertexTypeSecret, shoot1.Namespace, *shoot1DNSProvider1.SecretName, VertexTypeShoot, shoot1.Namespace, shoot1.Name)).To(BeFalse())
		Expect(graph.HasPathFrom(VertexTypeSecret, shoot1.Namespace, *shoot1DNSProvider2.SecretName, VertexTypeShoot, shoot1.Namespace, shoot1.Name)).To(BeFalse())
		Expect(graph.HasPathFrom(VertexTypeSecret, shoot1.Namespace, shoot1Resource2.Name, VertexTypeShoot, shoot1.Namespace, shoot1.Name)).To(BeFalse())
		Expect(graph.HasPathFrom(VertexTypeShoot, shoot1.Namespace, shoot1.Name, VertexTypeSeed, "", seed1.Name)).To(BeFalse())
		Expect(graph.HasPathFrom(VertexTypeShoot, shoot1.Namespace, shoot1.Name, VertexTypeSeed, "", "newseed")).To(BeTrue())

		By("update (new seed name in status)")
		shoot1Copy = shoot1.DeepCopy()
		shoot1.Status.SeedName = pointer.StringPtr("seed-in-status")
		fakeInformerShoot.Update(shoot1Copy, shoot1)
		Expect(graph.graph.Nodes().Len()).To(Equal(6))
		Expect(graph.graph.Edges().Len()).To(Equal(5))
		Expect(graph.HasPathFrom(VertexTypeNamespace, "", shoot1.Namespace, VertexTypeShoot, shoot1.Namespace, shoot1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeCloudProfile, "", shoot1.Spec.CloudProfileName, VertexTypeShoot, shoot1.Namespace, shoot1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeSecretBinding, shoot1.Namespace, shoot1.Spec.SecretBindingName, VertexTypeShoot, shoot1.Namespace, shoot1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeConfigMap, shoot1.Namespace, shoot1AuditPolicyConfigMapRef.Name, VertexTypeShoot, shoot1.Namespace, shoot1.Name)).To(BeFalse())
		Expect(graph.HasPathFrom(VertexTypeSecret, shoot1.Namespace, *shoot1DNSProvider1.SecretName, VertexTypeShoot, shoot1.Namespace, shoot1.Name)).To(BeFalse())
		Expect(graph.HasPathFrom(VertexTypeSecret, shoot1.Namespace, *shoot1DNSProvider2.SecretName, VertexTypeShoot, shoot1.Namespace, shoot1.Name)).To(BeFalse())
		Expect(graph.HasPathFrom(VertexTypeSecret, shoot1.Namespace, shoot1Resource2.Name, VertexTypeShoot, shoot1.Namespace, shoot1.Name)).To(BeFalse())
		Expect(graph.HasPathFrom(VertexTypeShoot, shoot1.Namespace, shoot1.Name, VertexTypeSeed, "", seed1.Name)).To(BeFalse())
		Expect(graph.HasPathFrom(VertexTypeShoot, shoot1.Namespace, shoot1.Name, VertexTypeSeed, "", "newseed")).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeShoot, shoot1.Namespace, shoot1.Name, VertexTypeSeed, "", "seed-in-status")).To(BeTrue())

		By("delete")
		fakeInformerShoot.Delete(shoot1)
		Expect(graph.graph.Nodes().Len()).To(BeZero())
		Expect(graph.graph.Edges().Len()).To(BeZero())
		Expect(graph.HasPathFrom(VertexTypeNamespace, "", shoot1.Namespace, VertexTypeShoot, shoot1.Namespace, shoot1.Name)).To(BeFalse())
		Expect(graph.HasPathFrom(VertexTypeCloudProfile, "", shoot1.Spec.CloudProfileName, VertexTypeShoot, shoot1.Namespace, shoot1.Name)).To(BeFalse())
		Expect(graph.HasPathFrom(VertexTypeSecretBinding, shoot1.Namespace, shoot1.Spec.SecretBindingName, VertexTypeShoot, shoot1.Namespace, shoot1.Name)).To(BeFalse())
		Expect(graph.HasPathFrom(VertexTypeConfigMap, shoot1.Namespace, shoot1AuditPolicyConfigMapRef.Name, VertexTypeShoot, shoot1.Namespace, shoot1.Name)).To(BeFalse())
		Expect(graph.HasPathFrom(VertexTypeSecret, shoot1.Namespace, *shoot1DNSProvider1.SecretName, VertexTypeShoot, shoot1.Namespace, shoot1.Name)).To(BeFalse())
		Expect(graph.HasPathFrom(VertexTypeSecret, shoot1.Namespace, *shoot1DNSProvider2.SecretName, VertexTypeShoot, shoot1.Namespace, shoot1.Name)).To(BeFalse())
		Expect(graph.HasPathFrom(VertexTypeSecret, shoot1.Namespace, shoot1Resource2.Name, VertexTypeShoot, shoot1.Namespace, shoot1.Name)).To(BeFalse())
		Expect(graph.HasPathFrom(VertexTypeShoot, shoot1.Namespace, shoot1.Name, VertexTypeSeed, "", seed1.Name)).To(BeFalse())
		Expect(graph.HasPathFrom(VertexTypeShoot, shoot1.Namespace, shoot1.Name, VertexTypeSeed, "", "newseed")).To(BeFalse())
	})

	It("should behave as expected for gardencorev1beta1.Project", func() {
		By("add")
		fakeInformerProject.Add(project1)
		Expect(graph.graph.Nodes().Len()).To(Equal(2))
		Expect(graph.graph.Edges().Len()).To(Equal(1))
		Expect(graph.HasPathFrom(VertexTypeProject, "", project1.Name, VertexTypeNamespace, "", *project1.Spec.Namespace)).To(BeTrue())

		By("update (irrelevant change)")
		project1Copy := project1.DeepCopy()
		project1.Spec.Purpose = pointer.StringPtr("purpose")
		fakeInformerProject.Update(project1Copy, project1)
		Expect(graph.graph.Nodes().Len()).To(Equal(2))
		Expect(graph.graph.Edges().Len()).To(Equal(1))
		Expect(graph.HasPathFrom(VertexTypeProject, "", project1.Name, VertexTypeNamespace, "", *project1.Spec.Namespace)).To(BeTrue())

		By("update (namespace)")
		project1Copy = project1.DeepCopy()
		project1.Spec.Namespace = pointer.StringPtr("newnamespace")
		fakeInformerProject.Update(project1Copy, project1)
		Expect(graph.graph.Nodes().Len()).To(Equal(2))
		Expect(graph.graph.Edges().Len()).To(Equal(1))
		Expect(graph.HasPathFrom(VertexTypeProject, "", project1.Name, VertexTypeNamespace, "", *project1Copy.Spec.Namespace)).To(BeFalse())
		Expect(graph.HasPathFrom(VertexTypeProject, "", project1.Name, VertexTypeNamespace, "", *project1.Spec.Namespace)).To(BeTrue())

		By("delete")
		fakeInformerProject.Delete(project1)
		Expect(graph.graph.Nodes().Len()).To(BeZero())
		Expect(graph.graph.Edges().Len()).To(BeZero())
		Expect(graph.HasPathFrom(VertexTypeProject, "", project1.Name, VertexTypeNamespace, "", *project1Copy.Spec.Namespace)).To(BeFalse())
		Expect(graph.HasPathFrom(VertexTypeProject, "", project1.Name, VertexTypeNamespace, "", *project1.Spec.Namespace)).To(BeFalse())
	})

	It("should behave as expected for gardencorev1beta1.BackupBucket", func() {
		By("add")
		fakeInformerBackupBucket.Add(backupBucket1)
		Expect(graph.graph.Nodes().Len()).To(Equal(3))
		Expect(graph.graph.Edges().Len()).To(Equal(2))
		Expect(graph.HasPathFrom(VertexTypeSecret, backupBucket1SecretRef.Namespace, backupBucket1SecretRef.Name, VertexTypeBackupBucket, "", backupBucket1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeBackupBucket, "", backupBucket1.Name, VertexTypeSeed, "", *backupBucket1.Spec.SeedName)).To(BeTrue())

		By("update (irrelevant change)")
		backupBucket1Copy := backupBucket1.DeepCopy()
		backupBucket1.Spec.Provider.Type = "provider-type"
		fakeInformerBackupBucket.Update(backupBucket1Copy, backupBucket1)
		Expect(graph.graph.Nodes().Len()).To(Equal(3))
		Expect(graph.graph.Edges().Len()).To(Equal(2))
		Expect(graph.HasPathFrom(VertexTypeSecret, backupBucket1SecretRef.Namespace, backupBucket1SecretRef.Name, VertexTypeBackupBucket, "", backupBucket1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeBackupBucket, "", backupBucket1.Name, VertexTypeSeed, "", *backupBucket1.Spec.SeedName)).To(BeTrue())

		By("update (seed name)")
		backupBucket1Copy = backupBucket1.DeepCopy()
		backupBucket1.Spec.SeedName = pointer.StringPtr("newbbseed")
		fakeInformerBackupBucket.Update(backupBucket1Copy, backupBucket1)
		Expect(graph.graph.Nodes().Len()).To(Equal(3))
		Expect(graph.graph.Edges().Len()).To(Equal(2))
		Expect(graph.HasPathFrom(VertexTypeSecret, backupBucket1SecretRef.Namespace, backupBucket1SecretRef.Name, VertexTypeBackupBucket, "", backupBucket1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeBackupBucket, "", backupBucket1.Name, VertexTypeSeed, "", *backupBucket1Copy.Spec.SeedName)).To(BeFalse())
		Expect(graph.HasPathFrom(VertexTypeBackupBucket, "", backupBucket1.Name, VertexTypeSeed, "", *backupBucket1.Spec.SeedName)).To(BeTrue())

		By("update (secret ref)")
		backupBucket1Copy = backupBucket1.DeepCopy()
		backupBucket1.Spec.SecretRef = corev1.SecretReference{Namespace: "newsecretrefnamespace", Name: "newsecretrefname"}
		fakeInformerBackupBucket.Update(backupBucket1Copy, backupBucket1)
		Expect(graph.graph.Nodes().Len()).To(Equal(3))
		Expect(graph.graph.Edges().Len()).To(Equal(2))
		Expect(graph.HasPathFrom(VertexTypeSecret, backupBucket1SecretRef.Namespace, backupBucket1SecretRef.Name, VertexTypeBackupBucket, "", backupBucket1.Name)).To(BeFalse())
		Expect(graph.HasPathFrom(VertexTypeSecret, backupBucket1.Spec.SecretRef.Namespace, backupBucket1.Spec.SecretRef.Name, VertexTypeBackupBucket, "", backupBucket1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeBackupBucket, "", backupBucket1.Name, VertexTypeSeed, "", *backupBucket1.Spec.SeedName)).To(BeTrue())

		By("delete")
		fakeInformerBackupBucket.Delete(backupBucket1)
		Expect(graph.graph.Nodes().Len()).To(BeZero())
		Expect(graph.graph.Edges().Len()).To(BeZero())
		Expect(graph.HasPathFrom(VertexTypeSecret, backupBucket1.Spec.SecretRef.Namespace, backupBucket1.Spec.SecretRef.Name, VertexTypeBackupBucket, "", backupBucket1.Name)).To(BeFalse())
		Expect(graph.HasPathFrom(VertexTypeBackupBucket, "", backupBucket1.Name, VertexTypeSeed, "", *backupBucket1.Spec.SeedName)).To(BeFalse())
	})

	It("should behave as expected for gardencorev1beta1.BackupEntry", func() {
		By("add")
		fakeInformerBackupEntry.Add(backupEntry1)
		Expect(graph.graph.Nodes().Len()).To(Equal(3))
		Expect(graph.graph.Edges().Len()).To(Equal(2))
		Expect(graph.HasPathFrom(VertexTypeBackupEntry, backupEntry1.Namespace, backupEntry1.Name, VertexTypeBackupBucket, "", backupEntry1.Spec.BucketName)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeBackupEntry, backupEntry1.Namespace, backupEntry1.Name, VertexTypeSeed, "", *backupEntry1.Spec.SeedName)).To(BeTrue())

		By("update (irrelevant change)")
		backupEntry1Copy := backupEntry1.DeepCopy()
		backupEntry1.Labels = map[string]string{"foo": "bar"}
		fakeInformerBackupEntry.Update(backupEntry1Copy, backupEntry1)
		Expect(graph.graph.Nodes().Len()).To(Equal(3))
		Expect(graph.graph.Edges().Len()).To(Equal(2))
		Expect(graph.HasPathFrom(VertexTypeBackupEntry, backupEntry1.Namespace, backupEntry1.Name, VertexTypeBackupBucket, "", backupEntry1.Spec.BucketName)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeBackupEntry, backupEntry1.Namespace, backupEntry1.Name, VertexTypeSeed, "", *backupEntry1.Spec.SeedName)).To(BeTrue())

		By("update (seed name)")
		backupEntry1Copy = backupEntry1.DeepCopy()
		backupEntry1.Spec.SeedName = pointer.StringPtr("newbbseed")
		fakeInformerBackupEntry.Update(backupEntry1Copy, backupEntry1)
		Expect(graph.graph.Nodes().Len()).To(Equal(3))
		Expect(graph.graph.Edges().Len()).To(Equal(2))
		Expect(graph.HasPathFrom(VertexTypeBackupEntry, backupEntry1.Namespace, backupEntry1.Name, VertexTypeBackupBucket, "", backupEntry1.Spec.BucketName)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeBackupEntry, backupEntry1.Namespace, backupEntry1.Name, VertexTypeSeed, "", *backupEntry1Copy.Spec.SeedName)).To(BeFalse())
		Expect(graph.HasPathFrom(VertexTypeBackupEntry, backupEntry1.Namespace, backupEntry1.Name, VertexTypeSeed, "", *backupEntry1.Spec.SeedName)).To(BeTrue())

		By("update (bucket name")
		backupEntry1Copy = backupEntry1.DeepCopy()
		backupEntry1.Spec.BucketName = "newbebucket"
		fakeInformerBackupEntry.Update(backupEntry1Copy, backupEntry1)
		Expect(graph.graph.Nodes().Len()).To(Equal(3))
		Expect(graph.graph.Edges().Len()).To(Equal(2))
		Expect(graph.HasPathFrom(VertexTypeBackupEntry, backupEntry1.Namespace, backupEntry1.Name, VertexTypeBackupBucket, "", backupEntry1Copy.Spec.BucketName)).To(BeFalse())
		Expect(graph.HasPathFrom(VertexTypeBackupEntry, backupEntry1.Namespace, backupEntry1.Name, VertexTypeBackupBucket, "", backupEntry1.Spec.BucketName)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeBackupEntry, backupEntry1.Namespace, backupEntry1.Name, VertexTypeSeed, "", *backupEntry1.Spec.SeedName)).To(BeTrue())

		By("delete")
		fakeInformerBackupEntry.Delete(backupEntry1)
		Expect(graph.graph.Nodes().Len()).To(BeZero())
		Expect(graph.graph.Edges().Len()).To(BeZero())
		Expect(graph.HasPathFrom(VertexTypeBackupEntry, backupEntry1.Namespace, backupEntry1.Name, VertexTypeBackupBucket, "", backupEntry1.Spec.BucketName)).To(BeFalse())
		Expect(graph.HasPathFrom(VertexTypeBackupEntry, backupEntry1.Namespace, backupEntry1.Name, VertexTypeSeed, "", *backupEntry1.Spec.SeedName)).To(BeFalse())
	})

	It("should behave as expected for gardencorev1beta1.SecretBinding", func() {
		By("add")
		fakeInformerSecretBinding.Add(secretBinding1)
		Expect(graph.graph.Nodes().Len()).To(Equal(2))
		Expect(graph.graph.Edges().Len()).To(Equal(1))
		Expect(graph.HasPathFrom(VertexTypeSecret, secretBinding1.SecretRef.Namespace, secretBinding1.SecretRef.Name, VertexTypeSecretBinding, secretBinding1.Namespace, secretBinding1.Name)).To(BeTrue())

		By("update (irrelevant change)")
		secretBinding1Copy := secretBinding1.DeepCopy()
		secretBinding1.Quotas = []corev1.ObjectReference{{}, {}, {}}
		fakeInformerSecretBinding.Update(secretBinding1Copy, secretBinding1)
		Expect(graph.graph.Nodes().Len()).To(Equal(2))
		Expect(graph.graph.Edges().Len()).To(Equal(1))
		Expect(graph.HasPathFrom(VertexTypeSecret, secretBinding1.SecretRef.Namespace, secretBinding1.SecretRef.Name, VertexTypeSecretBinding, secretBinding1.Namespace, secretBinding1.Name)).To(BeTrue())

		By("update (secretref)")
		secretBinding1Copy = secretBinding1.DeepCopy()
		secretBinding1.SecretRef = corev1.SecretReference{Namespace: "new-sb-secret-namespace", Name: "new-sb-secret-name"}
		fakeInformerSecretBinding.Update(secretBinding1Copy, secretBinding1)
		Expect(graph.graph.Nodes().Len()).To(Equal(2))
		Expect(graph.graph.Edges().Len()).To(Equal(1))
		Expect(graph.HasPathFrom(VertexTypeSecret, secretBinding1Copy.SecretRef.Namespace, secretBinding1Copy.SecretRef.Name, VertexTypeSecretBinding, secretBinding1.Namespace, secretBinding1.Name)).To(BeFalse())
		Expect(graph.HasPathFrom(VertexTypeSecret, secretBinding1.SecretRef.Namespace, secretBinding1.SecretRef.Name, VertexTypeSecretBinding, secretBinding1.Namespace, secretBinding1.Name)).To(BeTrue())

		By("delete")
		fakeInformerSecretBinding.Delete(secretBinding1)
		Expect(graph.graph.Nodes().Len()).To(BeZero())
		Expect(graph.graph.Edges().Len()).To(BeZero())
		Expect(graph.HasPathFrom(VertexTypeSecret, secretBinding1.SecretRef.Namespace, secretBinding1.SecretRef.Name, VertexTypeSecretBinding, secretBinding1.Namespace, secretBinding1.Name)).To(BeFalse())
	})
})
