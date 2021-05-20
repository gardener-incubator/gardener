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
	"fmt"
	"sync"

	gardencorev1alpha1 "github.com/gardener/gardener/pkg/apis/core/v1alpha1"
	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	gardenoperationsv1alpha1 "github.com/gardener/gardener/pkg/apis/operations/v1alpha1"
	seedmanagementv1alpha1 "github.com/gardener/gardener/pkg/apis/seedmanagement/v1alpha1"
	"github.com/gardener/gardener/pkg/client/kubernetes"
	gutil "github.com/gardener/gardener/pkg/utils/gardener"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/matchers"
	gomegatypes "github.com/onsi/gomega/types"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	certificatesv1beta1 "k8s.io/api/certificates/v1beta1"
	coordinationv1 "k8s.io/api/coordination/v1"
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

		fakeInformerSeed                      *controllertest.FakeInformer
		fakeInformerShoot                     *controllertest.FakeInformer
		fakeInformerProject                   *controllertest.FakeInformer
		fakeInformerBackupBucket              *controllertest.FakeInformer
		fakeInformerBackupEntry               *controllertest.FakeInformer
		fakeInformerBastion                   *controllertest.FakeInformer
		fakeInformerSecretBinding             *controllertest.FakeInformer
		fakeInformerControllerInstallation    *controllertest.FakeInformer
		fakeInformerManagedSeed               *controllertest.FakeInformer
		fakeInformerShootState                *controllertest.FakeInformer
		fakeInformerShootExtensionStatus      *controllertest.FakeInformer
		fakeInformerLease                     *controllertest.FakeInformer
		fakeInformerCertificateSigningRequest *controllertest.FakeInformer
		fakeInformers                         *informertest.FakeInformers

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

		backupBucket1                   *gardencorev1beta1.BackupBucket
		backupBucket1SecretRef          = corev1.SecretReference{Namespace: "baz", Name: "foo"}
		backupBucket1GeneratedSecretRef = corev1.SecretReference{Namespace: "generated", Name: "secret"}

		backupEntry1 *gardencorev1beta1.BackupEntry

		bastion1 *gardenoperationsv1alpha1.Bastion

		secretBinding1          *gardencorev1beta1.SecretBinding
		secretBinding1SecretRef = corev1.SecretReference{Namespace: "foobar", Name: "bazfoo"}

		controllerInstallation1 *gardencorev1beta1.ControllerInstallation

		managedSeed1 *seedmanagementv1alpha1.ManagedSeed

		shootState1 *metav1.PartialObjectMetadata

		shootExtensionStatus1 *gardencorev1alpha1.ShootExtensionStatus

		lease1 *coordinationv1.Lease

		seedNameInCSR = "myseed"
		csr1          *certificatesv1beta1.CertificateSigningRequest
	)

	BeforeEach(func() {
		scheme := kubernetes.GardenScheme
		Expect(metav1.AddMetaToScheme(scheme)).To(Succeed())

		fakeInformerSeed = &controllertest.FakeInformer{}
		fakeInformerShoot = &controllertest.FakeInformer{}
		fakeInformerProject = &controllertest.FakeInformer{}
		fakeInformerBackupBucket = &controllertest.FakeInformer{}
		fakeInformerBackupEntry = &controllertest.FakeInformer{}
		fakeInformerBastion = &controllertest.FakeInformer{}
		fakeInformerSecretBinding = &controllertest.FakeInformer{}
		fakeInformerControllerInstallation = &controllertest.FakeInformer{}
		fakeInformerManagedSeed = &controllertest.FakeInformer{}
		fakeInformerShootState = &controllertest.FakeInformer{}
		fakeInformerShootExtensionStatus = &controllertest.FakeInformer{}
		fakeInformerLease = &controllertest.FakeInformer{}
		fakeInformerCertificateSigningRequest = &controllertest.FakeInformer{}

		fakeInformers = &informertest.FakeInformers{
			Scheme: scheme,
			InformersByGVK: map[schema.GroupVersionKind]toolscache.SharedIndexInformer{
				gardencorev1beta1.SchemeGroupVersion.WithKind("Seed"):                        fakeInformerSeed,
				gardencorev1beta1.SchemeGroupVersion.WithKind("Shoot"):                       fakeInformerShoot,
				gardencorev1beta1.SchemeGroupVersion.WithKind("Project"):                     fakeInformerProject,
				gardencorev1beta1.SchemeGroupVersion.WithKind("BackupBucket"):                fakeInformerBackupBucket,
				gardencorev1beta1.SchemeGroupVersion.WithKind("BackupEntry"):                 fakeInformerBackupEntry,
				gardenoperationsv1alpha1.SchemeGroupVersion.WithKind("Bastion"):              fakeInformerBastion,
				gardencorev1beta1.SchemeGroupVersion.WithKind("SecretBinding"):               fakeInformerSecretBinding,
				gardencorev1beta1.SchemeGroupVersion.WithKind("ControllerInstallation"):      fakeInformerControllerInstallation,
				seedmanagementv1alpha1.SchemeGroupVersion.WithKind("ManagedSeed"):            fakeInformerManagedSeed,
				metav1.SchemeGroupVersion.WithKind("PartialObjectMetadata"):                  fakeInformerShootState,
				gardencorev1alpha1.SchemeGroupVersion.WithKind("ShootExtensionStatus"):       fakeInformerShootExtensionStatus,
				coordinationv1.SchemeGroupVersion.WithKind("Lease"):                          fakeInformerLease,
				certificatesv1beta1.SchemeGroupVersion.WithKind("CertificateSigningRequest"): fakeInformerCertificateSigningRequest,
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
			Status: gardencorev1beta1.BackupBucketStatus{
				GeneratedSecretRef: &backupBucket1GeneratedSecretRef,
			},
		}

		backupEntry1 = &gardencorev1beta1.BackupEntry{
			ObjectMeta: metav1.ObjectMeta{Name: "backupentry1", Namespace: "backupentry1namespace"},
			Spec: gardencorev1beta1.BackupEntrySpec{
				BucketName: backupBucket1.Name,
				SeedName:   &seed1.Name,
			},
		}

		bastion1 = &gardenoperationsv1alpha1.Bastion{
			ObjectMeta: metav1.ObjectMeta{Name: "bastion1", Namespace: "bastion1namespace"},
			Spec: gardenoperationsv1alpha1.BastionSpec{
				SeedName: &seed1.Name,
			},
		}

		secretBinding1 = &gardencorev1beta1.SecretBinding{
			ObjectMeta: metav1.ObjectMeta{Name: "secretbinding1", Namespace: "sb1namespace"},
			SecretRef:  secretBinding1SecretRef,
		}

		controllerInstallation1 = &gardencorev1beta1.ControllerInstallation{
			ObjectMeta: metav1.ObjectMeta{Name: "controllerinstallation1"},
			Spec: gardencorev1beta1.ControllerInstallationSpec{
				DeploymentRef:   &corev1.ObjectReference{Name: "controllerdeployment1"},
				RegistrationRef: corev1.ObjectReference{Name: "controllerregistration1"},
				SeedRef:         corev1.ObjectReference{Name: seed1.Name},
			},
		}

		managedSeed1 = &seedmanagementv1alpha1.ManagedSeed{
			ObjectMeta: metav1.ObjectMeta{Name: "managedseed1", Namespace: "managedseednamespace"},
			Spec: seedmanagementv1alpha1.ManagedSeedSpec{
				Shoot: &seedmanagementv1alpha1.Shoot{Name: shoot1.Name},
			},
		}

		shootState1 = &metav1.PartialObjectMetadata{
			ObjectMeta: metav1.ObjectMeta{Namespace: "shootstate1ns", Name: "shootstate1"},
		}

		shootExtensionStatus1 = &gardencorev1alpha1.ShootExtensionStatus{
			ObjectMeta: metav1.ObjectMeta{Namespace: "shootextensionstatus1ns", Name: "shootextensionstatus1"},
		}

		lease1 = &coordinationv1.Lease{
			ObjectMeta: metav1.ObjectMeta{Namespace: "gardener-system-seed-lease", Name: "lease1"},
		}

		csr1 = &certificatesv1beta1.CertificateSigningRequest{
			ObjectMeta: metav1.ObjectMeta{Name: "csr1"},
			Spec: certificatesv1beta1.CertificateSigningRequestSpec{
				Request: []byte(`-----BEGIN CERTIFICATE REQUEST-----
MIIClzCCAX8CAQAwUjEkMCIGA1UEChMbZ2FyZGVuZXIuY2xvdWQ6c3lzdGVtOnNl
ZWRzMSowKAYDVQQDEyFnYXJkZW5lci5jbG91ZDpzeXN0ZW06c2VlZDpteXNlZWQw
ggEiMA0GCSqGSIb3DQEBAQUAA4IBDwAwggEKAoIBAQCzNgJWhogJrCSzAhKKmHkJ
FuooKAbxpWRGDOe5DiB8jPdgCoRCkZYnF7D9x9cDzliljA9IeBad3P3E9oegtSV/
sXFJYqb+lRuhJQ5oo2eBC6WRg+Oxglp+n7o7xt0bO7JHS977mqNrqsJ1d1FnJHTB
MPHPxqoqkgIbdW4t219ckSA20aWzC3PU7I7+Z9OD+YfuuYgzkWG541XyBBKVSD2w
Ix2yGu6zrslqZ1eVBZ4IoxpWrQNmLSMFQVnABThyEUi0U1eVtW0vPNwSnBf0mufX
Z0PpqAIPVjr64Z4s3HHml2GSu64iOxaG5wwb9qIPcdyFaQCep/sFh7kq1KjNI1Ql
AgMBAAGgADANBgkqhkiG9w0BAQsFAAOCAQEAb+meLvm7dgHpzhu0XQ39w41FgpTv
S7p78ABFwzDNcP1NwfrEUft0T/rUwPiMlN9zve2rRicaZX5Z7Bol/newejsu8H5z
OdotvtKjE7zBCMzwnXZwO/0pA0cuUFcAy50DPcr35gdGjGlzV9ogO+HPKPTieS3n
TRVg+MWlcLqCjALr9Y4N39DOzf4/SJts8AZJJ+lyyxnY3XIPXx7SdADwNWC8BX0U
OK8CwMwN3iiBQ4redVeMK7LU1unV899q/PWB+NXFcKVr+Grm/Kom5VxuhXSzcHEp
yO57qEcJqG1cB7iSchFuCSTuDBbZlN0fXgn4YjiWZyb4l3BDp3rm4iJImA==
-----END CERTIFICATE REQUEST-----`),
				Usages: []certificatesv1beta1.KeyUsage{
					certificatesv1beta1.UsageKeyEncipherment,
					certificatesv1beta1.UsageDigitalSignature,
					certificatesv1beta1.UsageClientAuth,
				},
			},
		}
	})

	It("should behave as expected for gardencorev1beta1.Seed", func() {
		By("add")
		fakeInformerSeed.Add(seed1)
		Expect(graph.graph.Nodes().Len()).To(Equal(4))
		Expect(graph.graph.Edges().Len()).To(Equal(3))
		Expect(graph.HasPathFrom(VertexTypeSecret, seed1SecretRef.Namespace, seed1SecretRef.Name, VertexTypeSeed, "", seed1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeSecret, seed1BackupSecretRef.Namespace, seed1BackupSecretRef.Name, VertexTypeSeed, "", seed1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeNamespace, "", gutil.ComputeGardenNamespace(seed1.Name), VertexTypeSeed, "", seed1.Name)).To(BeTrue())

		By("update (irrelevant change)")
		seed1Copy := seed1.DeepCopy()
		seed1.Spec.Provider.Type = "providertype"
		fakeInformerSeed.Update(seed1Copy, seed1)
		Expect(graph.graph.Nodes().Len()).To(Equal(4))
		Expect(graph.graph.Edges().Len()).To(Equal(3))
		Expect(graph.HasPathFrom(VertexTypeSecret, seed1SecretRef.Namespace, seed1SecretRef.Name, VertexTypeSeed, "", seed1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeSecret, seed1BackupSecretRef.Namespace, seed1BackupSecretRef.Name, VertexTypeSeed, "", seed1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeNamespace, "", gutil.ComputeGardenNamespace(seed1.Name), VertexTypeSeed, "", seed1.Name)).To(BeTrue())

		By("update (remove secret ref)")
		seed1Copy = seed1.DeepCopy()
		seed1.Spec.SecretRef = nil
		fakeInformerSeed.Update(seed1Copy, seed1)
		Expect(graph.graph.Nodes().Len()).To(Equal(3))
		Expect(graph.graph.Edges().Len()).To(Equal(2))
		Expect(graph.HasPathFrom(VertexTypeSecret, seed1SecretRef.Namespace, seed1SecretRef.Name, VertexTypeSeed, "", seed1.Name)).To(BeFalse())
		Expect(graph.HasPathFrom(VertexTypeSecret, seed1BackupSecretRef.Namespace, seed1BackupSecretRef.Name, VertexTypeSeed, "", seed1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeNamespace, "", gutil.ComputeGardenNamespace(seed1.Name), VertexTypeSeed, "", seed1.Name)).To(BeTrue())

		By("update (remove backup secret ref)")
		seed1Copy = seed1.DeepCopy()
		seed1.Spec.Backup = nil
		fakeInformerSeed.Update(seed1Copy, seed1)
		Expect(graph.graph.Nodes().Len()).To(Equal(2))
		Expect(graph.graph.Edges().Len()).To(Equal(1))
		Expect(graph.HasPathFrom(VertexTypeSecret, seed1SecretRef.Namespace, seed1SecretRef.Name, VertexTypeSeed, "", seed1.Name)).To(BeFalse())
		Expect(graph.HasPathFrom(VertexTypeSecret, seed1BackupSecretRef.Namespace, seed1BackupSecretRef.Name, VertexTypeSeed, "", seed1.Name)).To(BeFalse())
		Expect(graph.HasPathFrom(VertexTypeNamespace, "", gutil.ComputeGardenNamespace(seed1.Name), VertexTypeSeed, "", seed1.Name)).To(BeTrue())

		By("update (both secret refs)")
		seed1Copy = seed1.DeepCopy()
		seed1.Spec.Backup = &gardencorev1beta1.SeedBackup{SecretRef: seed1BackupSecretRef}
		seed1.Spec.SecretRef = &seed1SecretRef
		fakeInformerSeed.Update(seed1Copy, seed1)
		Expect(graph.graph.Nodes().Len()).To(Equal(4))
		Expect(graph.graph.Edges().Len()).To(Equal(3))
		Expect(graph.HasPathFrom(VertexTypeSecret, seed1SecretRef.Namespace, seed1SecretRef.Name, VertexTypeSeed, "", seed1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeSecret, seed1BackupSecretRef.Namespace, seed1BackupSecretRef.Name, VertexTypeSeed, "", seed1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeNamespace, "", gutil.ComputeGardenNamespace(seed1.Name), VertexTypeSeed, "", seed1.Name)).To(BeTrue())

		By("delete")
		fakeInformerSeed.Delete(seed1)
		Expect(graph.graph.Nodes().Len()).To(BeZero())
		Expect(graph.graph.Edges().Len()).To(BeZero())
		Expect(graph.HasPathFrom(VertexTypeSecret, seed1SecretRef.Namespace, seed1SecretRef.Name, VertexTypeSeed, "", seed1.Name)).To(BeFalse())
		Expect(graph.HasPathFrom(VertexTypeSecret, seed1BackupSecretRef.Namespace, seed1BackupSecretRef.Name, VertexTypeSeed, "", seed1.Name)).To(BeFalse())
		Expect(graph.HasPathFrom(VertexTypeNamespace, "", gutil.ComputeGardenNamespace(seed1.Name), VertexTypeSeed, "", seed1.Name)).To(BeFalse())
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
		Expect(graph.graph.Nodes().Len()).To(Equal(4))
		Expect(graph.graph.Edges().Len()).To(Equal(3))
		Expect(graph.HasPathFrom(VertexTypeSecret, backupBucket1SecretRef.Namespace, backupBucket1SecretRef.Name, VertexTypeBackupBucket, "", backupBucket1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeSecret, backupBucket1GeneratedSecretRef.Namespace, backupBucket1GeneratedSecretRef.Name, VertexTypeBackupBucket, "", backupBucket1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeBackupBucket, "", backupBucket1.Name, VertexTypeSeed, "", *backupBucket1.Spec.SeedName)).To(BeTrue())

		By("update (irrelevant change)")
		backupBucket1Copy := backupBucket1.DeepCopy()
		backupBucket1.Spec.Provider.Type = "provider-type"
		fakeInformerBackupBucket.Update(backupBucket1Copy, backupBucket1)
		Expect(graph.graph.Nodes().Len()).To(Equal(4))
		Expect(graph.graph.Edges().Len()).To(Equal(3))
		Expect(graph.HasPathFrom(VertexTypeSecret, backupBucket1SecretRef.Namespace, backupBucket1SecretRef.Name, VertexTypeBackupBucket, "", backupBucket1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeSecret, backupBucket1GeneratedSecretRef.Namespace, backupBucket1GeneratedSecretRef.Name, VertexTypeBackupBucket, "", backupBucket1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeBackupBucket, "", backupBucket1.Name, VertexTypeSeed, "", *backupBucket1.Spec.SeedName)).To(BeTrue())

		By("update (seed name)")
		backupBucket1Copy = backupBucket1.DeepCopy()
		backupBucket1.Spec.SeedName = pointer.StringPtr("newbbseed")
		fakeInformerBackupBucket.Update(backupBucket1Copy, backupBucket1)
		Expect(graph.graph.Nodes().Len()).To(Equal(4))
		Expect(graph.graph.Edges().Len()).To(Equal(3))
		Expect(graph.HasPathFrom(VertexTypeSecret, backupBucket1SecretRef.Namespace, backupBucket1SecretRef.Name, VertexTypeBackupBucket, "", backupBucket1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeSecret, backupBucket1GeneratedSecretRef.Namespace, backupBucket1GeneratedSecretRef.Name, VertexTypeBackupBucket, "", backupBucket1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeBackupBucket, "", backupBucket1.Name, VertexTypeSeed, "", *backupBucket1Copy.Spec.SeedName)).To(BeFalse())
		Expect(graph.HasPathFrom(VertexTypeBackupBucket, "", backupBucket1.Name, VertexTypeSeed, "", *backupBucket1.Spec.SeedName)).To(BeTrue())

		By("update (secret ref)")
		backupBucket1Copy = backupBucket1.DeepCopy()
		backupBucket1.Spec.SecretRef = corev1.SecretReference{Namespace: "newsecretrefnamespace", Name: "newsecretrefname"}
		fakeInformerBackupBucket.Update(backupBucket1Copy, backupBucket1)
		Expect(graph.graph.Nodes().Len()).To(Equal(4))
		Expect(graph.graph.Edges().Len()).To(Equal(3))
		Expect(graph.HasPathFrom(VertexTypeSecret, backupBucket1SecretRef.Namespace, backupBucket1SecretRef.Name, VertexTypeBackupBucket, "", backupBucket1.Name)).To(BeFalse())
		Expect(graph.HasPathFrom(VertexTypeSecret, backupBucket1.Spec.SecretRef.Namespace, backupBucket1.Spec.SecretRef.Name, VertexTypeBackupBucket, "", backupBucket1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeSecret, backupBucket1GeneratedSecretRef.Namespace, backupBucket1GeneratedSecretRef.Name, VertexTypeBackupBucket, "", backupBucket1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeBackupBucket, "", backupBucket1.Name, VertexTypeSeed, "", *backupBucket1.Spec.SeedName)).To(BeTrue())

		By("update (generated secret ref)")
		backupBucket1Copy = backupBucket1.DeepCopy()
		backupBucket1.Status.GeneratedSecretRef = &corev1.SecretReference{Namespace: "newgeneratedsecretrefnamespace", Name: "newgeneratedsecretrefname"}
		fakeInformerBackupBucket.Update(backupBucket1Copy, backupBucket1)
		Expect(graph.graph.Nodes().Len()).To(Equal(4))
		Expect(graph.graph.Edges().Len()).To(Equal(3))
		Expect(graph.HasPathFrom(VertexTypeSecret, backupBucket1.Spec.SecretRef.Namespace, backupBucket1.Spec.SecretRef.Name, VertexTypeBackupBucket, "", backupBucket1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeSecret, backupBucket1GeneratedSecretRef.Namespace, backupBucket1GeneratedSecretRef.Name, VertexTypeBackupBucket, "", backupBucket1.Name)).To(BeFalse())
		Expect(graph.HasPathFrom(VertexTypeSecret, backupBucket1.Status.GeneratedSecretRef.Namespace, backupBucket1.Status.GeneratedSecretRef.Name, VertexTypeBackupBucket, "", backupBucket1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeBackupBucket, "", backupBucket1.Name, VertexTypeSeed, "", *backupBucket1.Spec.SeedName)).To(BeTrue())

		By("delete")
		fakeInformerBackupBucket.Delete(backupBucket1)
		Expect(graph.graph.Nodes().Len()).To(BeZero())
		Expect(graph.graph.Edges().Len()).To(BeZero())
		Expect(graph.HasPathFrom(VertexTypeSecret, backupBucket1.Spec.SecretRef.Namespace, backupBucket1.Spec.SecretRef.Name, VertexTypeBackupBucket, "", backupBucket1.Name)).To(BeFalse())
		Expect(graph.HasPathFrom(VertexTypeSecret, backupBucket1.Status.GeneratedSecretRef.Namespace, backupBucket1.Status.GeneratedSecretRef.Name, VertexTypeBackupBucket, "", backupBucket1.Name)).To(BeFalse())
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

	It("should behave as expected for gardenoperationsv1alpha1.Bastion", func() {
		By("add")
		fakeInformerBastion.Add(bastion1)
		Expect(graph.graph.Nodes().Len()).To(Equal(2))
		Expect(graph.graph.Edges().Len()).To(Equal(1))
		Expect(graph.HasPathFrom(VertexTypeBastion, bastion1.Namespace, bastion1.Name, VertexTypeSeed, "", *bastion1.Spec.SeedName)).To(BeTrue())

		By("update (irrelevant change)")
		bastion1Copy := bastion1.DeepCopy()
		bastion1.Spec.SSHPublicKey = "foobar"
		fakeInformerBastion.Update(bastion1Copy, bastion1)
		Expect(graph.graph.Nodes().Len()).To(Equal(2))
		Expect(graph.graph.Edges().Len()).To(Equal(1))
		Expect(graph.HasPathFrom(VertexTypeBastion, bastion1.Namespace, bastion1.Name, VertexTypeSeed, "", *bastion1.Spec.SeedName)).To(BeTrue())

		By("update (seed name)")
		bastion1Copy = bastion1.DeepCopy()
		bastion1.Spec.SeedName = pointer.StringPtr("newseed")
		fakeInformerBastion.Update(bastion1Copy, bastion1)
		Expect(graph.graph.Nodes().Len()).To(Equal(2))
		Expect(graph.graph.Edges().Len()).To(Equal(1))
		Expect(graph.HasPathFrom(VertexTypeBastion, bastion1.Namespace, bastion1.Name, VertexTypeSeed, "", *bastion1Copy.Spec.SeedName)).To(BeFalse())
		Expect(graph.HasPathFrom(VertexTypeBastion, bastion1.Namespace, bastion1.Name, VertexTypeSeed, "", *bastion1.Spec.SeedName)).To(BeTrue())

		By("delete")
		fakeInformerBastion.Delete(bastion1)
		Expect(graph.graph.Nodes().Len()).To(BeZero())
		Expect(graph.graph.Edges().Len()).To(BeZero())
		Expect(graph.HasPathFrom(VertexTypeBastion, bastion1.Namespace, bastion1.Name, VertexTypeSeed, "", *bastion1.Spec.SeedName)).To(BeFalse())
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

	It("should behave as expected for gardencorev1beta1.ControllerInstallation", func() {
		By("add")
		fakeInformerControllerInstallation.Add(controllerInstallation1)
		Expect(graph.graph.Nodes().Len()).To(Equal(4))
		Expect(graph.graph.Edges().Len()).To(Equal(3))
		Expect(graph.HasPathFrom(VertexTypeControllerRegistration, "", controllerInstallation1.Spec.RegistrationRef.Name, VertexTypeControllerInstallation, "", controllerInstallation1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeControllerDeployment, "", controllerInstallation1.Spec.DeploymentRef.Name, VertexTypeControllerInstallation, "", controllerInstallation1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeControllerInstallation, "", controllerInstallation1.Name, VertexTypeSeed, "", controllerInstallation1.Spec.SeedRef.Name)).To(BeTrue())

		By("update (irrelevant change)")
		controllerInstallation1Copy := controllerInstallation1.DeepCopy()
		controllerInstallation1.Spec.RegistrationRef.ResourceVersion = "123"
		fakeInformerControllerInstallation.Update(controllerInstallation1Copy, controllerInstallation1)
		Expect(graph.graph.Nodes().Len()).To(Equal(4))
		Expect(graph.graph.Edges().Len()).To(Equal(3))
		Expect(graph.HasPathFrom(VertexTypeControllerRegistration, "", controllerInstallation1.Spec.RegistrationRef.Name, VertexTypeControllerInstallation, "", controllerInstallation1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeControllerDeployment, "", controllerInstallation1.Spec.DeploymentRef.Name, VertexTypeControllerInstallation, "", controllerInstallation1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeControllerInstallation, "", controllerInstallation1.Name, VertexTypeSeed, "", controllerInstallation1.Spec.SeedRef.Name)).To(BeTrue())

		By("update (controller registration name)")
		controllerInstallation1Copy = controllerInstallation1.DeepCopy()
		controllerInstallation1.Spec.RegistrationRef.Name = "newreg"
		fakeInformerControllerInstallation.Update(controllerInstallation1Copy, controllerInstallation1)
		Expect(graph.graph.Nodes().Len()).To(Equal(4))
		Expect(graph.graph.Edges().Len()).To(Equal(3))
		Expect(graph.HasPathFrom(VertexTypeControllerRegistration, "", controllerInstallation1Copy.Spec.RegistrationRef.Name, VertexTypeControllerInstallation, "", controllerInstallation1.Name)).To(BeFalse())
		Expect(graph.HasPathFrom(VertexTypeControllerDeployment, "", controllerInstallation1.Spec.DeploymentRef.Name, VertexTypeControllerInstallation, "", controllerInstallation1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeControllerRegistration, "", controllerInstallation1.Spec.RegistrationRef.Name, VertexTypeControllerInstallation, "", controllerInstallation1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeControllerInstallation, "", controllerInstallation1.Name, VertexTypeSeed, "", controllerInstallation1.Spec.SeedRef.Name)).To(BeTrue())

		By("update (controller deployment name)")
		controllerInstallation1Copy = controllerInstallation1.DeepCopy()
		controllerInstallation1.Spec.DeploymentRef.Name = "newdeploy"
		fakeInformerControllerInstallation.Update(controllerInstallation1Copy, controllerInstallation1)
		Expect(graph.graph.Nodes().Len()).To(Equal(4))
		Expect(graph.graph.Edges().Len()).To(Equal(3))
		Expect(graph.HasPathFrom(VertexTypeControllerDeployment, "", controllerInstallation1Copy.Spec.DeploymentRef.Name, VertexTypeControllerInstallation, "", controllerInstallation1.Name)).To(BeFalse())
		Expect(graph.HasPathFrom(VertexTypeControllerRegistration, "", controllerInstallation1.Spec.RegistrationRef.Name, VertexTypeControllerInstallation, "", controllerInstallation1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeControllerDeployment, "", controllerInstallation1.Spec.DeploymentRef.Name, VertexTypeControllerInstallation, "", controllerInstallation1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeControllerInstallation, "", controllerInstallation1.Name, VertexTypeSeed, "", controllerInstallation1.Spec.SeedRef.Name)).To(BeTrue())

		By("update (seed name)")
		controllerInstallation1Copy = controllerInstallation1.DeepCopy()
		controllerInstallation1.Spec.SeedRef.Name = "newseed"
		fakeInformerControllerInstallation.Update(controllerInstallation1Copy, controllerInstallation1)
		Expect(graph.graph.Nodes().Len()).To(Equal(4))
		Expect(graph.graph.Edges().Len()).To(Equal(3))
		Expect(graph.HasPathFrom(VertexTypeControllerInstallation, "", controllerInstallation1.Name, VertexTypeSeed, "", controllerInstallation1Copy.Spec.SeedRef.Name)).To(BeFalse())
		Expect(graph.HasPathFrom(VertexTypeControllerRegistration, "", controllerInstallation1.Spec.RegistrationRef.Name, VertexTypeControllerInstallation, "", controllerInstallation1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeControllerDeployment, "", controllerInstallation1.Spec.DeploymentRef.Name, VertexTypeControllerInstallation, "", controllerInstallation1.Name)).To(BeTrue())
		Expect(graph.HasPathFrom(VertexTypeControllerInstallation, "", controllerInstallation1.Name, VertexTypeSeed, "", controllerInstallation1.Spec.SeedRef.Name)).To(BeTrue())

		By("delete")
		fakeInformerControllerInstallation.Delete(controllerInstallation1)
		Expect(graph.graph.Nodes().Len()).To(BeZero())
		Expect(graph.graph.Edges().Len()).To(BeZero())
		Expect(graph.HasPathFrom(VertexTypeControllerRegistration, "", controllerInstallation1.Spec.RegistrationRef.Name, VertexTypeControllerInstallation, "", controllerInstallation1.Name)).To(BeFalse())
		Expect(graph.HasPathFrom(VertexTypeControllerDeployment, "", controllerInstallation1.Spec.DeploymentRef.Name, VertexTypeControllerInstallation, "", controllerInstallation1.Name)).To(BeFalse())
		Expect(graph.HasPathFrom(VertexTypeControllerInstallation, "", controllerInstallation1.Name, VertexTypeSeed, "", controllerInstallation1.Spec.SeedRef.Name)).To(BeFalse())
	})

	It("should behave as expected for seedmanagementv1alpha1.ManagedSeed", func() {
		By("add")
		fakeInformerManagedSeed.Add(managedSeed1)
		Expect(graph.graph.Nodes().Len()).To(Equal(2))
		Expect(graph.graph.Edges().Len()).To(Equal(1))
		Expect(graph.HasPathFrom(VertexTypeManagedSeed, managedSeed1.Namespace, managedSeed1.Name, VertexTypeShoot, managedSeed1.Namespace, managedSeed1.Spec.Shoot.Name)).To(BeTrue())

		By("update (irrelevant change)")
		managedSeed1Copy := managedSeed1.DeepCopy()
		managedSeed1.Spec.Gardenlet = &seedmanagementv1alpha1.Gardenlet{}
		fakeInformerManagedSeed.Update(managedSeed1Copy, managedSeed1)
		Expect(graph.graph.Nodes().Len()).To(Equal(2))
		Expect(graph.graph.Edges().Len()).To(Equal(1))
		Expect(graph.HasPathFrom(VertexTypeManagedSeed, managedSeed1.Namespace, managedSeed1.Name, VertexTypeShoot, managedSeed1.Namespace, managedSeed1.Spec.Shoot.Name)).To(BeTrue())

		By("update (shoot name)")
		managedSeed1Copy = managedSeed1.DeepCopy()
		managedSeed1.Spec.Shoot.Name = "newshoot"
		fakeInformerManagedSeed.Update(managedSeed1Copy, managedSeed1)
		Expect(graph.graph.Nodes().Len()).To(Equal(2))
		Expect(graph.graph.Edges().Len()).To(Equal(1))
		Expect(graph.HasPathFrom(VertexTypeManagedSeed, managedSeed1.Namespace, managedSeed1.Name, VertexTypeShoot, managedSeed1.Namespace, managedSeed1Copy.Spec.Shoot.Name)).To(BeFalse())
		Expect(graph.HasPathFrom(VertexTypeManagedSeed, managedSeed1.Namespace, managedSeed1.Name, VertexTypeShoot, managedSeed1.Namespace, managedSeed1.Spec.Shoot.Name)).To(BeTrue())

		By("delete")
		fakeInformerManagedSeed.Delete(managedSeed1)
		Expect(graph.graph.Nodes().Len()).To(BeZero())
		Expect(graph.graph.Edges().Len()).To(BeZero())
		Expect(graph.HasPathFrom(VertexTypeManagedSeed, managedSeed1.Namespace, managedSeed1.Name, VertexTypeShoot, managedSeed1.Namespace, managedSeed1.Spec.Shoot.Name)).To(BeFalse())
	})

	It("should behave as expected for gardencorev1beta1.ShootState", func() {
		By("add")
		fakeInformerShootState.Add(shootState1)
		Expect(graph.graph.Nodes().Len()).To(Equal(2))
		Expect(graph.graph.Edges().Len()).To(Equal(1))
		Expect(graph.HasPathFrom(VertexTypeShootState, shootState1.Namespace, shootState1.Name, VertexTypeShoot, shootState1.Namespace, shootState1.Name)).To(BeTrue())

		By("delete")
		fakeInformerShootState.Delete(shootState1)
		Expect(graph.graph.Nodes().Len()).To(BeZero())
		Expect(graph.graph.Edges().Len()).To(BeZero())
		Expect(graph.HasPathFrom(VertexTypeShootState, shootState1.Namespace, shootState1.Name, VertexTypeShoot, shootState1.Namespace, shootState1.Name)).To(BeFalse())
	})

	It("should behave as expected for gardencorev1alpha1.ShootExtensionStatus", func() {
		By("add")
		fakeInformerShootExtensionStatus.Add(shootExtensionStatus1)
		Expect(graph.graph.Nodes().Len()).To(Equal(2))
		Expect(graph.graph.Edges().Len()).To(Equal(1))
		Expect(graph.HasPathFrom(VertexTypeShootExtensionStatus, shootExtensionStatus1.Namespace, shootExtensionStatus1.Name, VertexTypeShoot, shootExtensionStatus1.Namespace, shootExtensionStatus1.Name)).To(BeTrue())

		By("delete")
		fakeInformerShootExtensionStatus.Delete(shootExtensionStatus1)
		Expect(graph.graph.Nodes().Len()).To(BeZero())
		Expect(graph.graph.Edges().Len()).To(BeZero())
		Expect(graph.HasPathFrom(VertexTypeShootExtensionStatus, shootExtensionStatus1.Namespace, shootExtensionStatus1.Name, VertexTypeShoot, shootExtensionStatus1.Namespace, shootExtensionStatus1.Name)).To(BeFalse())
	})

	It("should behave as expected for coordinationv1.Lease", func() {
		By("add")
		fakeInformerLease.Add(lease1)
		Expect(graph.graph.Nodes().Len()).To(Equal(2))
		Expect(graph.graph.Edges().Len()).To(Equal(1))
		Expect(graph.HasPathFrom(VertexTypeLease, lease1.Namespace, lease1.Name, VertexTypeSeed, "", lease1.Name)).To(BeTrue())

		By("delete")
		fakeInformerLease.Delete(lease1)
		Expect(graph.graph.Nodes().Len()).To(BeZero())
		Expect(graph.graph.Edges().Len()).To(BeZero())
		Expect(graph.HasPathFrom(VertexTypeLease, lease1.Namespace, lease1.Name, VertexTypeSeed, "", lease1.Name)).To(BeFalse())

		By("add unrelated")
		lease1.Namespace = "foo"
		fakeInformerLease.Add(lease1)
		Expect(graph.graph.Nodes().Len()).To(BeZero())
		Expect(graph.graph.Edges().Len()).To(BeZero())
		Expect(graph.HasPathFrom(VertexTypeLease, lease1.Namespace, lease1.Name, VertexTypeSeed, "", lease1.Name)).To(BeFalse())
	})

	It("should behave as expected for certificatesv1beta1.CertificateSigningRequest", func() {
		By("add")
		fakeInformerCertificateSigningRequest.Add(csr1)
		Expect(graph.graph.Nodes().Len()).To(Equal(2))
		Expect(graph.graph.Edges().Len()).To(Equal(1))
		Expect(graph.HasPathFrom(VertexTypeCertificateSigningRequest, "", csr1.Name, VertexTypeSeed, "", seedNameInCSR)).To(BeTrue())

		By("delete")
		fakeInformerCertificateSigningRequest.Delete(csr1)
		Expect(graph.graph.Nodes().Len()).To(BeZero())
		Expect(graph.graph.Edges().Len()).To(BeZero())
		Expect(graph.HasPathFrom(VertexTypeCertificateSigningRequest, "", csr1.Name, VertexTypeSeed, "", seedNameInCSR)).To(BeFalse())

		By("add unrelated")
		csr1.Spec.Usages = nil
		fakeInformerCertificateSigningRequest.Add(csr1)
		Expect(graph.graph.Nodes().Len()).To(BeZero())
		Expect(graph.graph.Edges().Len()).To(BeZero())
		Expect(graph.HasPathFrom(VertexTypeCertificateSigningRequest, "", csr1.Name, VertexTypeSeed, "", seedNameInCSR)).To(BeFalse())
	})

	It("should behave as expected with more objects modified in parallel", func() {
		var (
			nodes, edges int
			paths        = make(map[VertexType][]pathExpectation)
			wg           sync.WaitGroup
			lock         sync.Mutex
		)

		By("creating objects")
		wg.Add(1)
		go func() {
			defer wg.Done()
			fakeInformerSeed.Add(seed1)
			lock.Lock()
			defer lock.Unlock()
			nodes, edges = nodes+4, edges+3
			paths[VertexTypeSeed] = append(paths[VertexTypeSeed], pathExpectation{VertexTypeSecret, seed1SecretRef.Namespace, seed1SecretRef.Name, VertexTypeSeed, "", seed1.Name, BeTrue()})
			paths[VertexTypeSeed] = append(paths[VertexTypeSeed], pathExpectation{VertexTypeSecret, seed1BackupSecretRef.Namespace, seed1BackupSecretRef.Name, VertexTypeSeed, "", seed1.Name, BeTrue()})
			paths[VertexTypeSeed] = append(paths[VertexTypeSeed], pathExpectation{VertexTypeNamespace, "", gutil.ComputeGardenNamespace(seed1.Name), VertexTypeSeed, "", seed1.Name, BeTrue()})
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			fakeInformerShoot.Add(shoot1)
			lock.Lock()
			defer lock.Unlock()
			nodes, edges = nodes+8, edges+8
			paths[VertexTypeShoot] = append(paths[VertexTypeShoot], pathExpectation{VertexTypeNamespace, "", shoot1.Namespace, VertexTypeShoot, shoot1.Namespace, shoot1.Name, BeTrue()})
			paths[VertexTypeShoot] = append(paths[VertexTypeShoot], pathExpectation{VertexTypeCloudProfile, "", shoot1.Spec.CloudProfileName, VertexTypeShoot, shoot1.Namespace, shoot1.Name, BeTrue()})
			paths[VertexTypeShoot] = append(paths[VertexTypeShoot], pathExpectation{VertexTypeSecretBinding, shoot1.Namespace, shoot1.Spec.SecretBindingName, VertexTypeShoot, shoot1.Namespace, shoot1.Name, BeTrue()})
			paths[VertexTypeShoot] = append(paths[VertexTypeShoot], pathExpectation{VertexTypeConfigMap, shoot1.Namespace, shoot1AuditPolicyConfigMapRef.Name, VertexTypeShoot, shoot1.Namespace, shoot1.Name, BeTrue()})
			paths[VertexTypeShoot] = append(paths[VertexTypeShoot], pathExpectation{VertexTypeSecret, shoot1.Namespace, *shoot1DNSProvider1.SecretName, VertexTypeShoot, shoot1.Namespace, shoot1.Name, BeTrue()})
			paths[VertexTypeShoot] = append(paths[VertexTypeShoot], pathExpectation{VertexTypeSecret, shoot1.Namespace, *shoot1DNSProvider2.SecretName, VertexTypeShoot, shoot1.Namespace, shoot1.Name, BeTrue()})
			paths[VertexTypeShoot] = append(paths[VertexTypeShoot], pathExpectation{VertexTypeSecret, shoot1.Namespace, shoot1Resource2.Name, VertexTypeShoot, shoot1.Namespace, shoot1.Name, BeTrue()})
			paths[VertexTypeShoot] = append(paths[VertexTypeShoot], pathExpectation{VertexTypeShoot, shoot1.Namespace, shoot1.Name, VertexTypeSeed, "", seed1.Name, BeTrue()})
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			fakeInformerProject.Add(project1)
			lock.Lock()
			defer lock.Unlock()
			nodes, edges = nodes+2, edges+1
			paths[VertexTypeProject] = append(paths[VertexTypeProject], pathExpectation{VertexTypeProject, "", project1.Name, VertexTypeNamespace, "", *project1.Spec.Namespace, BeTrue()})
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			fakeInformerBackupBucket.Add(backupBucket1)
			lock.Lock()
			defer lock.Unlock()
			nodes, edges = nodes+3, edges+3
			paths[VertexTypeBackupBucket] = append(paths[VertexTypeBackupBucket], pathExpectation{VertexTypeSecret, backupBucket1SecretRef.Namespace, backupBucket1SecretRef.Name, VertexTypeBackupBucket, "", backupBucket1.Name, BeTrue()})
			paths[VertexTypeBackupBucket] = append(paths[VertexTypeBackupBucket], pathExpectation{VertexTypeSecret, backupBucket1GeneratedSecretRef.Namespace, backupBucket1GeneratedSecretRef.Name, VertexTypeBackupBucket, "", backupBucket1.Name, BeTrue()})
			paths[VertexTypeBackupBucket] = append(paths[VertexTypeBackupBucket], pathExpectation{VertexTypeBackupBucket, "", backupBucket1.Name, VertexTypeSeed, "", *backupBucket1.Spec.SeedName, BeTrue()})
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			fakeInformerBackupEntry.Add(backupEntry1)
			lock.Lock()
			defer lock.Unlock()
			nodes, edges = nodes+1, edges+2
			paths[VertexTypeBackupEntry] = append(paths[VertexTypeBackupEntry], pathExpectation{VertexTypeBackupEntry, backupEntry1.Namespace, backupEntry1.Name, VertexTypeBackupBucket, "", backupEntry1.Spec.BucketName, BeTrue()})
			paths[VertexTypeBackupEntry] = append(paths[VertexTypeBackupEntry], pathExpectation{VertexTypeBackupEntry, backupEntry1.Namespace, backupEntry1.Name, VertexTypeSeed, "", *backupEntry1.Spec.SeedName, BeTrue()})
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			fakeInformerBastion.Add(bastion1)
			lock.Lock()
			defer lock.Unlock()
			nodes, edges = nodes+1, edges+1
			paths[VertexTypeBastion] = append(paths[VertexTypeBastion], pathExpectation{VertexTypeBastion, bastion1.Namespace, bastion1.Name, VertexTypeSeed, "", seed1.Name, BeTrue()})
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			fakeInformerSecretBinding.Add(secretBinding1)
			lock.Lock()
			defer lock.Unlock()
			nodes, edges = nodes+2, edges+1
			paths[VertexTypeSecretBinding] = append(paths[VertexTypeSecretBinding], pathExpectation{VertexTypeSecret, secretBinding1.SecretRef.Namespace, secretBinding1.SecretRef.Name, VertexTypeSecretBinding, secretBinding1.Namespace, secretBinding1.Name, BeTrue()})
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			fakeInformerControllerInstallation.Add(controllerInstallation1)
			lock.Lock()
			defer lock.Unlock()
			nodes, edges = nodes+3, edges+3
			paths[VertexTypeControllerInstallation] = append(paths[VertexTypeControllerInstallation], pathExpectation{VertexTypeControllerDeployment, "", controllerInstallation1.Spec.DeploymentRef.Name, VertexTypeControllerInstallation, "", controllerInstallation1.Name, BeTrue()})
			paths[VertexTypeControllerInstallation] = append(paths[VertexTypeControllerInstallation], pathExpectation{VertexTypeControllerRegistration, "", controllerInstallation1.Spec.RegistrationRef.Name, VertexTypeControllerInstallation, "", controllerInstallation1.Name, BeTrue()})
			paths[VertexTypeControllerInstallation] = append(paths[VertexTypeControllerInstallation], pathExpectation{VertexTypeControllerInstallation, "", controllerInstallation1.Name, VertexTypeSeed, "", controllerInstallation1.Spec.SeedRef.Name, BeTrue()})
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			fakeInformerManagedSeed.Add(managedSeed1)
			lock.Lock()
			defer lock.Unlock()
			nodes, edges = nodes+2, edges+1
			paths[VertexTypeManagedSeed] = append(paths[VertexTypeManagedSeed], pathExpectation{VertexTypeManagedSeed, managedSeed1.Namespace, managedSeed1.Name, VertexTypeShoot, managedSeed1.Namespace, managedSeed1.Spec.Shoot.Name, BeTrue()})
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			fakeInformerShootState.Add(shootState1)
			lock.Lock()
			defer lock.Unlock()
			nodes, edges = nodes+2, edges+1
			paths[VertexTypeShootState] = append(paths[VertexTypeShootState], pathExpectation{VertexTypeShootState, shootState1.Namespace, shootState1.Name, VertexTypeShoot, shootState1.Namespace, shootState1.Name, BeTrue()})
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			fakeInformerShootExtensionStatus.Add(shootExtensionStatus1)
			lock.Lock()
			defer lock.Unlock()
			nodes, edges = nodes+2, edges+1
			paths[VertexTypeShootExtensionStatus] = append(paths[VertexTypeShootExtensionStatus], pathExpectation{VertexTypeShootExtensionStatus, shootExtensionStatus1.Namespace, shootExtensionStatus1.Name, VertexTypeShoot, shootExtensionStatus1.Namespace, shootExtensionStatus1.Name, BeTrue()})
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			fakeInformerLease.Add(lease1)
			lock.Lock()
			defer lock.Unlock()
			nodes, edges = nodes+2, edges+1
			paths[VertexTypeLease] = append(paths[VertexTypeLease], pathExpectation{VertexTypeLease, lease1.Namespace, lease1.Name, VertexTypeSeed, "", lease1.Name, BeTrue()})
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			fakeInformerCertificateSigningRequest.Add(csr1)
			lock.Lock()
			defer lock.Unlock()
			nodes, edges = nodes+2, edges+1
			paths[VertexTypeCertificateSigningRequest] = append(paths[VertexTypeCertificateSigningRequest], pathExpectation{VertexTypeCertificateSigningRequest, "", csr1.Name, VertexTypeSeed, "", seedNameInCSR, BeTrue()})
		}()
		wg.Wait()
		Expect(graph.graph.Nodes().Len()).To(Equal(nodes))
		Expect(graph.graph.Edges().Len()).To(Equal(edges))
		expectPaths(graph, edges, paths)

		By("updating some objects (1)")
		paths = make(map[VertexType][]pathExpectation)
		wg.Add(1)
		go func() {
			defer wg.Done()
			seed1Copy := seed1.DeepCopy()
			seed1.Spec.Provider.Type = "providertype"
			fakeInformerSeed.Update(seed1Copy, seed1)
			lock.Lock()
			defer lock.Unlock()
			paths[VertexTypeSeed] = append(paths[VertexTypeSeed], pathExpectation{VertexTypeSecret, seed1SecretRef.Namespace, seed1SecretRef.Name, VertexTypeSeed, "", seed1.Name, BeTrue()})
			paths[VertexTypeSeed] = append(paths[VertexTypeSeed], pathExpectation{VertexTypeSecret, seed1BackupSecretRef.Namespace, seed1BackupSecretRef.Name, VertexTypeSeed, "", seed1.Name, BeTrue()})
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			shoot1Copy := shoot1.DeepCopy()
			shoot1.Spec.CloudProfileName = "foo"
			fakeInformerShoot.Update(shoot1Copy, shoot1)
			lock.Lock()
			defer lock.Unlock()
			paths[VertexTypeShoot] = append(paths[VertexTypeShoot], pathExpectation{VertexTypeNamespace, "", shoot1.Namespace, VertexTypeShoot, shoot1.Namespace, shoot1.Name, BeTrue()})
			paths[VertexTypeShoot] = append(paths[VertexTypeShoot], pathExpectation{VertexTypeCloudProfile, "", shoot1Copy.Spec.CloudProfileName, VertexTypeShoot, shoot1.Namespace, shoot1.Name, BeFalse()})
			paths[VertexTypeShoot] = append(paths[VertexTypeShoot], pathExpectation{VertexTypeCloudProfile, "", shoot1.Spec.CloudProfileName, VertexTypeShoot, shoot1.Namespace, shoot1.Name, BeTrue()})
			paths[VertexTypeShoot] = append(paths[VertexTypeShoot], pathExpectation{VertexTypeSecretBinding, shoot1.Namespace, shoot1.Spec.SecretBindingName, VertexTypeShoot, shoot1.Namespace, shoot1.Name, BeTrue()})
			paths[VertexTypeShoot] = append(paths[VertexTypeShoot], pathExpectation{VertexTypeConfigMap, shoot1.Namespace, shoot1AuditPolicyConfigMapRef.Name, VertexTypeShoot, shoot1.Namespace, shoot1.Name, BeTrue()})
			paths[VertexTypeShoot] = append(paths[VertexTypeShoot], pathExpectation{VertexTypeSecret, shoot1.Namespace, *shoot1DNSProvider1.SecretName, VertexTypeShoot, shoot1.Namespace, shoot1.Name, BeTrue()})
			paths[VertexTypeShoot] = append(paths[VertexTypeShoot], pathExpectation{VertexTypeSecret, shoot1.Namespace, *shoot1DNSProvider2.SecretName, VertexTypeShoot, shoot1.Namespace, shoot1.Name, BeTrue()})
			paths[VertexTypeShoot] = append(paths[VertexTypeShoot], pathExpectation{VertexTypeSecret, shoot1.Namespace, shoot1Resource2.Name, VertexTypeShoot, shoot1.Namespace, shoot1.Name, BeTrue()})
			paths[VertexTypeShoot] = append(paths[VertexTypeShoot], pathExpectation{VertexTypeShoot, shoot1.Namespace, shoot1.Name, VertexTypeSeed, "", seed1.Name, BeTrue()})
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			project1Copy := project1.DeepCopy()
			project1.Spec.Namespace = pointer.StringPtr("newnamespace")
			fakeInformerProject.Update(project1Copy, project1)
			lock.Lock()
			defer lock.Unlock()
			paths[VertexTypeProject] = append(paths[VertexTypeProject], pathExpectation{VertexTypeProject, "", project1.Name, VertexTypeNamespace, "", *project1Copy.Spec.Namespace, BeFalse()})
			paths[VertexTypeProject] = append(paths[VertexTypeProject], pathExpectation{VertexTypeProject, "", project1.Name, VertexTypeNamespace, "", *project1.Spec.Namespace, BeTrue()})
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			backupBucket1Copy := backupBucket1.DeepCopy()
			backupBucket1.Spec.SecretRef = corev1.SecretReference{Namespace: "newsecretrefnamespace", Name: "newsecretrefname"}
			fakeInformerBackupBucket.Update(backupBucket1Copy, backupBucket1)
			lock.Lock()
			defer lock.Unlock()
			paths[VertexTypeBackupBucket] = append(paths[VertexTypeBackupBucket], pathExpectation{VertexTypeSecret, backupBucket1Copy.Spec.SecretRef.Namespace, backupBucket1Copy.Spec.SecretRef.Name, VertexTypeBackupBucket, "", backupBucket1.Name, BeFalse()})
			paths[VertexTypeBackupBucket] = append(paths[VertexTypeBackupBucket], pathExpectation{VertexTypeSecret, backupBucket1GeneratedSecretRef.Namespace, backupBucket1GeneratedSecretRef.Name, VertexTypeBackupBucket, "", backupBucket1.Name, BeTrue()})
			paths[VertexTypeBackupBucket] = append(paths[VertexTypeBackupBucket], pathExpectation{VertexTypeSecret, backupBucket1.Spec.SecretRef.Namespace, backupBucket1.Spec.SecretRef.Name, VertexTypeBackupBucket, "", backupBucket1.Name, BeTrue()})
			paths[VertexTypeBackupBucket] = append(paths[VertexTypeBackupBucket], pathExpectation{VertexTypeBackupBucket, "", backupBucket1.Name, VertexTypeSeed, "", *backupBucket1.Spec.SeedName, BeTrue()})
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			backupEntry1Copy := backupEntry1.DeepCopy()
			backupEntry1.Spec.SeedName = pointer.StringPtr("newbbseed")
			fakeInformerBackupEntry.Update(backupEntry1Copy, backupEntry1)
			lock.Lock()
			defer lock.Unlock()
			nodes = nodes + 1
			paths[VertexTypeBackupEntry] = append(paths[VertexTypeBackupEntry], pathExpectation{VertexTypeBackupEntry, backupEntry1.Namespace, backupEntry1.Name, VertexTypeBackupBucket, "", backupEntry1.Spec.BucketName, BeTrue()})
			paths[VertexTypeBackupEntry] = append(paths[VertexTypeBackupEntry], pathExpectation{VertexTypeBackupEntry, backupEntry1.Namespace, backupEntry1.Name, VertexTypeSeed, "", *backupEntry1Copy.Spec.SeedName, BeTrue()})
			paths[VertexTypeBackupEntry] = append(paths[VertexTypeBackupEntry], pathExpectation{VertexTypeBackupEntry, backupEntry1.Namespace, backupEntry1.Name, VertexTypeSeed, "", *backupEntry1.Spec.SeedName, BeTrue()})
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			bastion1Copy := bastion1.DeepCopy()
			bastion1.Spec.SSHPublicKey = "new-key"
			fakeInformerBastion.Update(bastion1Copy, bastion1)
			lock.Lock()
			defer lock.Unlock()
			paths[VertexTypeBastion] = append(paths[VertexTypeBastion], pathExpectation{VertexTypeBastion, bastion1.Namespace, bastion1.Name, VertexTypeSeed, "", seed1.Name, BeTrue()})
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			secretBinding1Copy := secretBinding1.DeepCopy()
			secretBinding1.Quotas = []corev1.ObjectReference{{}, {}, {}}
			fakeInformerSecretBinding.Update(secretBinding1Copy, secretBinding1)
			lock.Lock()
			defer lock.Unlock()
			paths[VertexTypeSecretBinding] = append(paths[VertexTypeSecretBinding], pathExpectation{VertexTypeSecret, secretBinding1.SecretRef.Namespace, secretBinding1.SecretRef.Name, VertexTypeSecretBinding, secretBinding1.Namespace, secretBinding1.Name, BeTrue()})
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			controllerInstallation1Copy := controllerInstallation1.DeepCopy()
			controllerInstallation1.Spec.RegistrationRef.Name = "newreg"
			fakeInformerControllerInstallation.Update(controllerInstallation1Copy, controllerInstallation1)
			lock.Lock()
			defer lock.Unlock()
			paths[VertexTypeControllerInstallation] = append(paths[VertexTypeControllerInstallation], pathExpectation{VertexTypeControllerRegistration, "", controllerInstallation1Copy.Spec.RegistrationRef.Name, VertexTypeControllerInstallation, "", controllerInstallation1.Name, BeFalse()})
			paths[VertexTypeControllerInstallation] = append(paths[VertexTypeControllerInstallation], pathExpectation{VertexTypeControllerDeployment, "", controllerInstallation1.Spec.DeploymentRef.Name, VertexTypeControllerInstallation, "", controllerInstallation1.Name, BeTrue()})
			paths[VertexTypeControllerInstallation] = append(paths[VertexTypeControllerInstallation], pathExpectation{VertexTypeControllerRegistration, "", controllerInstallation1.Spec.RegistrationRef.Name, VertexTypeControllerInstallation, "", controllerInstallation1.Name, BeTrue()})
			paths[VertexTypeControllerInstallation] = append(paths[VertexTypeControllerInstallation], pathExpectation{VertexTypeControllerInstallation, "", controllerInstallation1.Name, VertexTypeSeed, "", controllerInstallation1.Spec.SeedRef.Name, BeTrue()})
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			managedSeed1Copy := managedSeed1.DeepCopy()
			managedSeed1.Spec.Shoot.Name = "newshoot"
			fakeInformerManagedSeed.Update(managedSeed1Copy, managedSeed1)
			lock.Lock()
			defer lock.Unlock()
			paths[VertexTypeManagedSeed] = append(paths[VertexTypeManagedSeed], pathExpectation{VertexTypeManagedSeed, managedSeed1.Namespace, managedSeed1.Name, VertexTypeShoot, managedSeed1.Namespace, managedSeed1Copy.Spec.Shoot.Name, BeFalse()})
			paths[VertexTypeManagedSeed] = append(paths[VertexTypeManagedSeed], pathExpectation{VertexTypeManagedSeed, managedSeed1.Namespace, managedSeed1.Name, VertexTypeShoot, managedSeed1.Namespace, managedSeed1.Spec.Shoot.Name, BeTrue()})
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			fakeInformerShootState.Delete(shootState1)
			lock.Lock()
			defer lock.Unlock()
			nodes, edges = nodes-2, edges-1
			paths[VertexTypeShootState] = append(paths[VertexTypeShootState], pathExpectation{VertexTypeShootState, shootState1.Namespace, shootState1.Name, VertexTypeShoot, shootState1.Namespace, shootState1.Name, BeFalse()})
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			fakeInformerShootExtensionStatus.Delete(shootExtensionStatus1)
			lock.Lock()
			defer lock.Unlock()
			nodes, edges = nodes-2, edges-1
			paths[VertexTypeShootExtensionStatus] = append(paths[VertexTypeShootExtensionStatus], pathExpectation{VertexTypeShootExtensionStatus, shootExtensionStatus1.Namespace, shootExtensionStatus1.Name, VertexTypeShoot, shootExtensionStatus1.Namespace, shootExtensionStatus1.Name, BeFalse()})
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			fakeInformerLease.Delete(lease1)
			lock.Lock()
			defer lock.Unlock()
			nodes, edges = nodes-2, edges-1
			paths[VertexTypeLease] = append(paths[VertexTypeLease], pathExpectation{VertexTypeLease, lease1.Namespace, lease1.Name, VertexTypeSeed, "", lease1.Name, BeFalse()})
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			fakeInformerCertificateSigningRequest.Delete(csr1)
			lock.Lock()
			defer lock.Unlock()
			nodes, edges = nodes-2, edges-1
			paths[VertexTypeCertificateSigningRequest] = append(paths[VertexTypeCertificateSigningRequest], pathExpectation{VertexTypeCertificateSigningRequest, csr1.Namespace, csr1.Name, VertexTypeSeed, "", seedNameInCSR, BeFalse()})
		}()
		wg.Wait()
		Expect(graph.graph.Nodes().Len()).To(Equal(nodes), "node count")
		Expect(graph.graph.Edges().Len()).To(Equal(edges), "edge count")
		expectPaths(graph, edges, paths)

		By("updating some objects (2)")
		paths = make(map[VertexType][]pathExpectation)
		wg.Add(1)
		go func() {
			defer wg.Done()
			seed1Copy := seed1.DeepCopy()
			seed1.Spec.Backup = nil
			fakeInformerSeed.Update(seed1Copy, seed1)
			lock.Lock()
			defer lock.Unlock()
			nodes, edges = nodes-1, edges-1
			paths[VertexTypeSeed] = append(paths[VertexTypeSeed], pathExpectation{VertexTypeSecret, seed1SecretRef.Namespace, seed1SecretRef.Name, VertexTypeSeed, "", seed1.Name, BeTrue()})
			paths[VertexTypeSeed] = append(paths[VertexTypeSeed], pathExpectation{VertexTypeSecret, seed1BackupSecretRef.Namespace, seed1BackupSecretRef.Name, VertexTypeSeed, "", seed1.Name, BeFalse()})
			paths[VertexTypeSeed] = append(paths[VertexTypeSeed], pathExpectation{VertexTypeNamespace, "", gutil.ComputeGardenNamespace(seed1.Name), VertexTypeSeed, "", seed1.Name, BeTrue()})
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			shoot1Copy := shoot1.DeepCopy()
			shoot1.Spec.Kubernetes.KubeAPIServer = nil
			fakeInformerShoot.Update(shoot1Copy, shoot1)
			lock.Lock()
			defer lock.Unlock()
			nodes, edges = nodes-1, edges-1
			paths[VertexTypeShoot] = append(paths[VertexTypeShoot], pathExpectation{VertexTypeNamespace, "", shoot1.Namespace, VertexTypeShoot, shoot1.Namespace, shoot1.Name, BeTrue()})
			paths[VertexTypeShoot] = append(paths[VertexTypeShoot], pathExpectation{VertexTypeCloudProfile, "", shoot1.Spec.CloudProfileName, VertexTypeShoot, shoot1.Namespace, shoot1.Name, BeTrue()})
			paths[VertexTypeShoot] = append(paths[VertexTypeShoot], pathExpectation{VertexTypeSecretBinding, shoot1.Namespace, shoot1.Spec.SecretBindingName, VertexTypeShoot, shoot1.Namespace, shoot1.Name, BeTrue()})
			paths[VertexTypeShoot] = append(paths[VertexTypeShoot], pathExpectation{VertexTypeConfigMap, shoot1.Namespace, shoot1AuditPolicyConfigMapRef.Name, VertexTypeShoot, shoot1.Namespace, shoot1.Name, BeFalse()})
			paths[VertexTypeShoot] = append(paths[VertexTypeShoot], pathExpectation{VertexTypeSecret, shoot1.Namespace, *shoot1DNSProvider1.SecretName, VertexTypeShoot, shoot1.Namespace, shoot1.Name, BeTrue()})
			paths[VertexTypeShoot] = append(paths[VertexTypeShoot], pathExpectation{VertexTypeSecret, shoot1.Namespace, *shoot1DNSProvider2.SecretName, VertexTypeShoot, shoot1.Namespace, shoot1.Name, BeTrue()})
			paths[VertexTypeShoot] = append(paths[VertexTypeShoot], pathExpectation{VertexTypeSecret, shoot1.Namespace, shoot1Resource2.Name, VertexTypeShoot, shoot1.Namespace, shoot1.Name, BeTrue()})
			paths[VertexTypeShoot] = append(paths[VertexTypeShoot], pathExpectation{VertexTypeShoot, shoot1.Namespace, shoot1.Name, VertexTypeSeed, "", seed1.Name, BeTrue()})
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			project1Copy := project1.DeepCopy()
			project1.Spec.Purpose = pointer.StringPtr("purpose")
			fakeInformerProject.Update(project1Copy, project1)
			lock.Lock()
			defer lock.Unlock()
			paths[VertexTypeProject] = append(paths[VertexTypeProject], pathExpectation{VertexTypeProject, "", project1.Name, VertexTypeNamespace, "", *project1.Spec.Namespace, BeTrue()})
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			backupBucket1Copy := backupBucket1.DeepCopy()
			backupBucket1.Spec.SeedName = pointer.StringPtr("newbbseed")
			fakeInformerBackupBucket.Update(backupBucket1Copy, backupBucket1)
			lock.Lock()
			defer lock.Unlock()
			paths[VertexTypeBackupBucket] = append(paths[VertexTypeBackupBucket], pathExpectation{VertexTypeSecret, backupBucket1.Spec.SecretRef.Namespace, backupBucket1.Spec.SecretRef.Name, VertexTypeBackupBucket, "", backupBucket1.Name, BeTrue()})
			paths[VertexTypeBackupBucket] = append(paths[VertexTypeBackupBucket], pathExpectation{VertexTypeSecret, backupBucket1GeneratedSecretRef.Namespace, backupBucket1GeneratedSecretRef.Name, VertexTypeBackupBucket, "", backupBucket1.Name, BeTrue()})
			paths[VertexTypeBackupBucket] = append(paths[VertexTypeBackupBucket], pathExpectation{VertexTypeBackupBucket, "", backupBucket1.Name, VertexTypeSeed, "", *backupBucket1Copy.Spec.SeedName, BeFalse()})
			paths[VertexTypeBackupBucket] = append(paths[VertexTypeBackupBucket], pathExpectation{VertexTypeBackupBucket, "", backupBucket1.Name, VertexTypeSeed, "", *backupBucket1.Spec.SeedName, BeTrue()})
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			backupEntry1Copy := backupEntry1.DeepCopy()
			backupEntry1.Spec.BucketName = "newbebucket"
			fakeInformerBackupEntry.Update(backupEntry1Copy, backupEntry1)
			lock.Lock()
			defer lock.Unlock()
			nodes = nodes + 1
			paths[VertexTypeBackupEntry] = append(paths[VertexTypeBackupEntry], pathExpectation{VertexTypeBackupEntry, backupEntry1.Namespace, backupEntry1.Name, VertexTypeBackupBucket, "", backupEntry1Copy.Spec.BucketName, BeFalse()})
			paths[VertexTypeBackupEntry] = append(paths[VertexTypeBackupEntry], pathExpectation{VertexTypeBackupEntry, backupEntry1.Namespace, backupEntry1.Name, VertexTypeBackupBucket, "", backupEntry1.Spec.BucketName, BeTrue()})
			paths[VertexTypeBackupEntry] = append(paths[VertexTypeBackupEntry], pathExpectation{VertexTypeBackupEntry, backupEntry1.Namespace, backupEntry1.Name, VertexTypeSeed, "", *backupEntry1.Spec.SeedName, BeTrue()})
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			bastion1Copy := bastion1.DeepCopy()
			bastion1.Spec.SSHPublicKey = "another-new-key"
			fakeInformerBastion.Update(bastion1Copy, bastion1)
			lock.Lock()
			defer lock.Unlock()
			paths[VertexTypeBastion] = append(paths[VertexTypeBastion], pathExpectation{VertexTypeBastion, bastion1.Namespace, bastion1.Name, VertexTypeSeed, "", seed1.Name, BeTrue()})
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			secretBinding1Copy := secretBinding1.DeepCopy()
			secretBinding1.SecretRef = corev1.SecretReference{Namespace: "new-sb-secret-namespace", Name: "new-sb-secret-name"}
			fakeInformerSecretBinding.Update(secretBinding1Copy, secretBinding1)
			lock.Lock()
			defer lock.Unlock()
			paths[VertexTypeSecretBinding] = append(paths[VertexTypeSecretBinding], pathExpectation{VertexTypeSecret, secretBinding1Copy.SecretRef.Namespace, secretBinding1Copy.SecretRef.Name, VertexTypeSecretBinding, secretBinding1.Namespace, secretBinding1.Name, BeFalse()})
			paths[VertexTypeSecretBinding] = append(paths[VertexTypeSecretBinding], pathExpectation{VertexTypeSecret, secretBinding1.SecretRef.Namespace, secretBinding1.SecretRef.Name, VertexTypeSecretBinding, secretBinding1.Namespace, secretBinding1.Name, BeTrue()})
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			controllerInstallation1Copy := controllerInstallation1.DeepCopy()
			controllerInstallation1.Spec.RegistrationRef.ResourceVersion = "123"
			fakeInformerControllerInstallation.Update(controllerInstallation1Copy, controllerInstallation1)
			lock.Lock()
			defer lock.Unlock()
			paths[VertexTypeControllerInstallation] = append(paths[VertexTypeControllerInstallation], pathExpectation{VertexTypeControllerDeployment, "", controllerInstallation1.Spec.DeploymentRef.Name, VertexTypeControllerInstallation, "", controllerInstallation1.Name, BeTrue()})
			paths[VertexTypeControllerInstallation] = append(paths[VertexTypeControllerInstallation], pathExpectation{VertexTypeControllerRegistration, "", controllerInstallation1.Spec.RegistrationRef.Name, VertexTypeControllerInstallation, "", controllerInstallation1.Name, BeTrue()})
			paths[VertexTypeControllerInstallation] = append(paths[VertexTypeControllerInstallation], pathExpectation{VertexTypeControllerInstallation, "", controllerInstallation1.Name, VertexTypeSeed, "", controllerInstallation1.Spec.SeedRef.Name, BeTrue()})
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			managedSeed1Copy := managedSeed1.DeepCopy()
			managedSeed1.Spec.Gardenlet = &seedmanagementv1alpha1.Gardenlet{}
			fakeInformerManagedSeed.Update(managedSeed1Copy, managedSeed1)
			lock.Lock()
			defer lock.Unlock()
			paths[VertexTypeManagedSeed] = append(paths[VertexTypeManagedSeed], pathExpectation{VertexTypeManagedSeed, managedSeed1.Namespace, managedSeed1.Name, VertexTypeShoot, managedSeed1.Namespace, managedSeed1.Spec.Shoot.Name, BeTrue()})
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			fakeInformerShootState.Add(shootState1)
			lock.Lock()
			defer lock.Unlock()
			nodes, edges = nodes+2, edges+1
			paths[VertexTypeShootState] = append(paths[VertexTypeShootState], pathExpectation{VertexTypeShootState, shootState1.Namespace, shootState1.Name, VertexTypeShoot, shootState1.Namespace, shootState1.Name, BeTrue()})
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			fakeInformerShootExtensionStatus.Add(shootExtensionStatus1)
			lock.Lock()
			defer lock.Unlock()
			nodes, edges = nodes+2, edges+1
			paths[VertexTypeShootExtensionStatus] = append(paths[VertexTypeShootExtensionStatus], pathExpectation{VertexTypeShootExtensionStatus, shootExtensionStatus1.Namespace, shootExtensionStatus1.Name, VertexTypeShootExtensionStatus, shootExtensionStatus1.Namespace, shootExtensionStatus1.Name, BeTrue()})
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			fakeInformerLease.Add(lease1)
			lock.Lock()
			defer lock.Unlock()
			nodes, edges = nodes+2, edges+1
			paths[VertexTypeLease] = append(paths[VertexTypeLease], pathExpectation{VertexTypeLease, lease1.Namespace, lease1.Name, VertexTypeSeed, "", lease1.Name, BeTrue()})
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			fakeInformerCertificateSigningRequest.Add(csr1)
			lock.Lock()
			defer lock.Unlock()
			nodes, edges = nodes+2, edges+1
			paths[VertexTypeCertificateSigningRequest] = append(paths[VertexTypeCertificateSigningRequest], pathExpectation{VertexTypeCertificateSigningRequest, "", csr1.Name, VertexTypeSeed, "", seedNameInCSR, BeTrue()})
		}()
		wg.Wait()
		Expect(graph.graph.Nodes().Len()).To(Equal(nodes), "node count")
		Expect(graph.graph.Edges().Len()).To(Equal(edges), "edge count")
		expectPaths(graph, edges, paths)

		By("deleting all objects")
		paths = make(map[VertexType][]pathExpectation)
		wg.Add(1)
		go func() {
			defer wg.Done()
			fakeInformerSeed.Delete(seed1)
			lock.Lock()
			defer lock.Unlock()
			paths[VertexTypeSeed] = append(paths[VertexTypeSeed], pathExpectation{VertexTypeSecret, seed1SecretRef.Namespace, seed1SecretRef.Name, VertexTypeSeed, "", seed1.Name, BeFalse()})
			paths[VertexTypeSeed] = append(paths[VertexTypeSeed], pathExpectation{VertexTypeSecret, seed1BackupSecretRef.Namespace, seed1BackupSecretRef.Name, VertexTypeSeed, "", seed1.Name, BeFalse()})
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			fakeInformerShoot.Delete(shoot1)
			lock.Lock()
			defer lock.Unlock()
			paths[VertexTypeShoot] = append(paths[VertexTypeShoot], pathExpectation{VertexTypeNamespace, "", shoot1.Namespace, VertexTypeShoot, shoot1.Namespace, shoot1.Name, BeFalse()})
			paths[VertexTypeShoot] = append(paths[VertexTypeShoot], pathExpectation{VertexTypeCloudProfile, "", shoot1.Spec.CloudProfileName, VertexTypeShoot, shoot1.Namespace, shoot1.Name, BeFalse()})
			paths[VertexTypeShoot] = append(paths[VertexTypeShoot], pathExpectation{VertexTypeSecretBinding, shoot1.Namespace, shoot1.Spec.SecretBindingName, VertexTypeShoot, shoot1.Namespace, shoot1.Name, BeFalse()})
			paths[VertexTypeShoot] = append(paths[VertexTypeShoot], pathExpectation{VertexTypeConfigMap, shoot1.Namespace, shoot1AuditPolicyConfigMapRef.Name, VertexTypeShoot, shoot1.Namespace, shoot1.Name, BeFalse()})
			paths[VertexTypeShoot] = append(paths[VertexTypeShoot], pathExpectation{VertexTypeSecret, shoot1.Namespace, *shoot1DNSProvider1.SecretName, VertexTypeShoot, shoot1.Namespace, shoot1.Name, BeFalse()})
			paths[VertexTypeShoot] = append(paths[VertexTypeShoot], pathExpectation{VertexTypeSecret, shoot1.Namespace, *shoot1DNSProvider2.SecretName, VertexTypeShoot, shoot1.Namespace, shoot1.Name, BeFalse()})
			paths[VertexTypeShoot] = append(paths[VertexTypeShoot], pathExpectation{VertexTypeSecret, shoot1.Namespace, shoot1Resource2.Name, VertexTypeShoot, shoot1.Namespace, shoot1.Name, BeFalse()})
			paths[VertexTypeShoot] = append(paths[VertexTypeShoot], pathExpectation{VertexTypeShoot, shoot1.Namespace, shoot1.Name, VertexTypeSeed, "", seed1.Name, BeFalse()})
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			fakeInformerProject.Delete(project1)
			lock.Lock()
			defer lock.Unlock()
			paths[VertexTypeProject] = append(paths[VertexTypeProject], pathExpectation{VertexTypeProject, "", project1.Name, VertexTypeNamespace, "", *project1.Spec.Namespace, BeFalse()})
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			fakeInformerBackupBucket.Delete(backupBucket1)
			lock.Lock()
			defer lock.Unlock()
			paths[VertexTypeBackupBucket] = append(paths[VertexTypeBackupBucket], pathExpectation{VertexTypeSecret, backupBucket1SecretRef.Namespace, backupBucket1SecretRef.Name, VertexTypeBackupBucket, "", backupBucket1.Name, BeFalse()})
			paths[VertexTypeBackupBucket] = append(paths[VertexTypeBackupBucket], pathExpectation{VertexTypeSecret, backupBucket1GeneratedSecretRef.Namespace, backupBucket1GeneratedSecretRef.Name, VertexTypeBackupBucket, "", backupBucket1.Name, BeFalse()})
			paths[VertexTypeBackupBucket] = append(paths[VertexTypeBackupBucket], pathExpectation{VertexTypeBackupBucket, "", backupBucket1.Name, VertexTypeSeed, "", *backupBucket1.Spec.SeedName, BeFalse()})
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			fakeInformerBackupEntry.Delete(backupEntry1)
			lock.Lock()
			defer lock.Unlock()
			paths[VertexTypeBackupEntry] = append(paths[VertexTypeBackupEntry], pathExpectation{VertexTypeBackupEntry, backupEntry1.Namespace, backupEntry1.Name, VertexTypeBackupBucket, "", backupEntry1.Spec.BucketName, BeFalse()})
			paths[VertexTypeBackupEntry] = append(paths[VertexTypeBackupEntry], pathExpectation{VertexTypeBackupEntry, backupEntry1.Namespace, backupEntry1.Name, VertexTypeSeed, "", *backupEntry1.Spec.SeedName, BeFalse()})
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			fakeInformerBastion.Delete(bastion1)
			lock.Lock()
			defer lock.Unlock()
			paths[VertexTypeBastion] = append(paths[VertexTypeBastion], pathExpectation{VertexTypeBastion, bastion1.Namespace, bastion1.Name, VertexTypeSeed, "", *bastion1.Spec.SeedName, BeFalse()})
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			fakeInformerSecretBinding.Delete(secretBinding1)
			lock.Lock()
			defer lock.Unlock()
			paths[VertexTypeSecretBinding] = append(paths[VertexTypeSecretBinding], pathExpectation{VertexTypeSecret, secretBinding1.SecretRef.Namespace, secretBinding1.SecretRef.Name, VertexTypeSecretBinding, secretBinding1.Namespace, secretBinding1.Name, BeFalse()})
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			fakeInformerControllerInstallation.Delete(controllerInstallation1)
			lock.Lock()
			defer lock.Unlock()
			paths[VertexTypeControllerInstallation] = append(paths[VertexTypeControllerInstallation], pathExpectation{VertexTypeControllerRegistration, "", controllerInstallation1.Spec.RegistrationRef.Name, VertexTypeControllerInstallation, "", controllerInstallation1.Name, BeFalse()})
			paths[VertexTypeControllerInstallation] = append(paths[VertexTypeControllerInstallation], pathExpectation{VertexTypeControllerInstallation, "", controllerInstallation1.Name, VertexTypeSeed, "", controllerInstallation1.Spec.SeedRef.Name, BeFalse()})
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			fakeInformerManagedSeed.Delete(managedSeed1)
			lock.Lock()
			defer lock.Unlock()
			paths[VertexTypeManagedSeed] = append(paths[VertexTypeManagedSeed], pathExpectation{VertexTypeManagedSeed, managedSeed1.Namespace, managedSeed1.Name, VertexTypeShoot, managedSeed1.Namespace, managedSeed1.Spec.Shoot.Name, BeFalse()})
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			fakeInformerShootState.Delete(shootState1)
			lock.Lock()
			defer lock.Unlock()
			paths[VertexTypeShootState] = append(paths[VertexTypeShootState], pathExpectation{VertexTypeShootState, shootState1.Namespace, shootState1.Name, VertexTypeShoot, shootState1.Namespace, shootState1.Name, BeFalse()})
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			fakeInformerShootExtensionStatus.Delete(shootExtensionStatus1)
			lock.Lock()
			defer lock.Unlock()
			paths[VertexTypeShootExtensionStatus] = append(paths[VertexTypeShootExtensionStatus], pathExpectation{VertexTypeShootExtensionStatus, shootExtensionStatus1.Namespace, shootExtensionStatus1.Name, VertexTypeShoot, shootExtensionStatus1.Namespace, shootExtensionStatus1.Name, BeFalse()})
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			fakeInformerLease.Delete(lease1)
			lock.Lock()
			defer lock.Unlock()
			paths[VertexTypeLease] = append(paths[VertexTypeLease], pathExpectation{VertexTypeLease, lease1.Namespace, lease1.Name, VertexTypeSeed, "", lease1.Name, BeFalse()})
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			fakeInformerCertificateSigningRequest.Delete(csr1)
			lock.Lock()
			defer lock.Unlock()
			nodes, edges = nodes-2, edges-1
			paths[VertexTypeCertificateSigningRequest] = append(paths[VertexTypeCertificateSigningRequest], pathExpectation{VertexTypeCertificateSigningRequest, csr1.Namespace, csr1.Name, VertexTypeSeed, "", seedNameInCSR, BeFalse()})
		}()
		wg.Wait()
		Expect(graph.graph.Nodes().Len()).To(BeZero())
		Expect(graph.graph.Edges().Len()).To(BeZero())
		expectPaths(graph, 0, paths)
	})
})

type pathExpectation struct {
	fromType      VertexType
	fromNamespace string
	fromName      string
	toType        VertexType
	toNamespace   string
	toName        string
	matcher       gomegatypes.GomegaMatcher
}

func expectPaths(graph *graph, edges int, paths map[VertexType][]pathExpectation) {
	var pathsCount int

	for vertexType, expectation := range paths {
		By("validating path expectations for " + vertexTypes[vertexType])
		for _, p := range expectation {
			switch p.matcher.(type) {
			case *matchers.BeTrueMatcher:
				pathsCount++
			}

			Expect(graph.HasPathFrom(p.fromType, p.fromNamespace, p.fromName, p.toType, p.toNamespace, p.toName)).To(p.matcher, fmt.Sprintf("path expectation from %s:%s/%s to %s:%s/%s", vertexTypes[p.fromType], p.fromNamespace, p.fromName, vertexTypes[p.toType], p.toNamespace, p.toName))
		}
	}

	Expect(pathsCount).To(BeNumerically(">=", edges), "paths equals edges")
}
