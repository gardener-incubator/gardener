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

package extensions_test

import (
	"context"
	"path/filepath"

	"github.com/gardener/gardener/charts"
	gardencoreinstall "github.com/gardener/gardener/pkg/apis/core/install"
	gardencorev1alpha1 "github.com/gardener/gardener/pkg/apis/core/v1alpha1"
	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/gardener/gardener/pkg/client/kubernetes"
	gardenerenvtest "github.com/gardener/gardener/pkg/envtest"
	extensionsutil "github.com/gardener/gardener/pkg/extensions"
	"github.com/gardener/gardener/pkg/gardenlet/controller/federatedseed/extensions"
	"github.com/gardener/gardener/pkg/logger"
	kutil "github.com/gardener/gardener/pkg/utils/kubernetes"
	. "github.com/gardener/gardener/pkg/utils/test/matchers"
	"github.com/gardener/gardener/test/framework"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	logzap "sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var gardenScheme *runtime.Scheme

func init() {
	gardenScheme = runtime.NewScheme()
	gardencoreinstall.Install(gardenScheme)
}

var (
	ctx        = context.Background()
	err        error
	restConfig *rest.Config
	decoder    = extensionsutil.NewGardenDecoder()

	gardenEnv    *gardenerenvtest.GardenerTestEnvironment
	gardenClient client.Client

	seedClient client.Client
	seedEnv    *envtest.Environment

	shootName           = "aws-clone"
	workerName          = "worker-1"
	shootControlPlaneNs = "shoot--dev--aws-clone"
	shootProjectNs      = "garden-dev"
	clusterName         = shootControlPlaneNs

	recorder         = record.NewFakeRecorder(64)
	log              = logger.NewNopLogger().WithField("seed", "test")
	reconcileRequest = reconcile.Request{
		NamespacedName: types.NamespacedName{
			Namespace: shootControlPlaneNs,
			Name:      workerName,
		},
	}

	cluster              *extensionsv1alpha1.Cluster
	worker               *extensionsv1alpha1.Worker
	shootExtensionStatus *gardencorev1alpha1.ShootExtensionStatus
	workerProviderStatus = &gardencorev1beta1.Shoot{
		TypeMeta: metav1.TypeMeta{
			APIVersion: gardencorev1beta1.SchemeGroupVersion.String(),
			Kind:       "Shoot",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: shootProjectNs,
			Name:      shootName,
		},
		Spec: gardencorev1beta1.ShootSpec{
			CloudProfileName: "differentiating-attribute",
		},
	}
)

var _ = Describe("Shoot extension status tests", func() {
	BeforeSuite(func() {
		logf.SetLogger(logzap.New(logzap.UseDevMode(true), logzap.WriteTo(GinkgoWriter)))

		Context("starting Seed cluster", func() {
			seedEnv = &envtest.Environment{}

			pathExtensionCRDs := filepath.Join("..", "..", "..", "..", "..", charts.Path, "seed-bootstrap", "charts", "extensions", "templates")
			seedEnv.CRDDirectoryPaths = []string{
				pathExtensionCRDs,
			}
			seedEnv.ErrorIfCRDPathMissing = true

			restConfig, err = seedEnv.Start()
			Expect(err).ToNot(HaveOccurred())

			seedClient, err = client.New(restConfig, client.Options{Scheme: kubernetes.SeedScheme})
			Expect(err).NotTo(HaveOccurred())
		})

		Context("starting Garden cluster", func() {
			gardenEnv = &gardenerenvtest.GardenerTestEnvironment{}
			restConfig, err = gardenEnv.Start()
			Expect(err).ToNot(HaveOccurred())

			gardenClient, err = client.New(restConfig, client.Options{Scheme: kubernetes.GardenScheme})
			Expect(err).NotTo(HaveOccurred())
		})

		Expect(seedClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: shootControlPlaneNs}})).
			To(Or(Succeed(), BeAlreadyExistsError()))

		Expect(gardenClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: shootProjectNs}})).
			To(Or(Succeed(), BeAlreadyExistsError()))
	})

	BeforeEach(func() {
		cluster = &extensionsv1alpha1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterName,
			},
			Spec: extensionsv1alpha1.ClusterSpec{
				Shoot: runtime.RawExtension{
					Raw: encode(&gardencorev1beta1.Shoot{
						TypeMeta: metav1.TypeMeta{
							APIVersion: gardencorev1beta1.SchemeGroupVersion.String(),
							Kind:       "Shoot",
						},
						ObjectMeta: metav1.ObjectMeta{
							Namespace: shootProjectNs,
							Name:      shootName,
						},
					}),
				},
				CloudProfile: runtime.RawExtension{
					Raw: encode(&gardencorev1beta1.CloudProfile{
						TypeMeta: metav1.TypeMeta{
							APIVersion: gardencorev1beta1.SchemeGroupVersion.String(),
							Kind:       "CloudProfile",
						},
						ObjectMeta: metav1.ObjectMeta{
							Name: "aws",
						},
					}),
				},
				Seed: runtime.RawExtension{
					Raw: encode(&gardencorev1beta1.Seed{
						TypeMeta: metav1.TypeMeta{
							APIVersion: gardencorev1beta1.SchemeGroupVersion.String(),
							Kind:       "Seed",
						},
						ObjectMeta: metav1.ObjectMeta{
							Name: "my-seed",
						},
					}),
				},
			},
		}
		Expect(seedClient.Create(ctx, cluster)).ToNot(HaveOccurred())

		worker = &extensionsv1alpha1.Worker{
			TypeMeta: metav1.TypeMeta{},
			ObjectMeta: metav1.ObjectMeta{
				Name:      workerName,
				Namespace: shootControlPlaneNs,
			},
			Spec: extensionsv1alpha1.WorkerSpec{
				DefaultSpec: extensionsv1alpha1.DefaultSpec{
					Type: "aws",
				},
				Pools: []extensionsv1alpha1.WorkerPool{
					{
						Name:        "superworker",
						MachineType: "m5.large",
						MachineImage: extensionsv1alpha1.MachineImage{
							Name:    "gardenlinux",
							Version: "184.0.0",
						},
						UserData: []byte("test"),
					},
				},
			},
			Status: extensionsv1alpha1.WorkerStatus{
				DefaultStatus: extensionsv1alpha1.DefaultStatus{
					ProviderStatus: &runtime.RawExtension{
						Raw: encode(workerProviderStatus),
					},
				},
			},
		}

		Expect(seedClient.Create(ctx, worker)).ToNot(HaveOccurred())
		Expect(seedClient.Status().Update(ctx, worker)).ToNot(HaveOccurred())

		shootExtensionStatus = &gardencorev1alpha1.ShootExtensionStatus{
			ObjectMeta: metav1.ObjectMeta{
				Name:      shootName,
				Namespace: shootProjectNs,
			},
		}
		Expect(gardenClient.Create(ctx, shootExtensionStatus)).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		Expect(seedClient.Delete(ctx, worker)).To(Or(Succeed(), BeNotFoundError()))
		Expect(seedClient.Delete(ctx, cluster)).ToNot(HaveOccurred())
		Expect(gardenClient.Delete(ctx, shootExtensionStatus)).ToNot(HaveOccurred())
	})

	Describe("#CreateShootExtensionStatusSyncReconcileFunc", func() {
		It("should create the entry in the ShootExtensionStatus", func() {
			executeStatusSyncReconciler()

			shootExtensionStatus := &gardencorev1alpha1.ShootExtensionStatus{
				ObjectMeta: metav1.ObjectMeta{
					Name:      shootName,
					Namespace: shootProjectNs,
				},
			}
			Expect(gardenClient.Get(ctx, kutil.Key(shootExtensionStatus.Namespace, shootExtensionStatus.Name), shootExtensionStatus)).ToNot(HaveOccurred())
			Expect(shootExtensionStatus.Statuses).To(HaveLen(1))

			syncedWorkerStatus := &gardencorev1beta1.Shoot{}
			_, _, err = decoder.Decode(shootExtensionStatus.Statuses[0].Status.Raw, nil, syncedWorkerStatus)
			Expect(err).ToNot(HaveOccurred())
			// pick any differentiating attribute to check if the content has actually been synced
			Expect(syncedWorkerStatus.Spec.CloudProfileName).To(Equal(workerProviderStatus.Spec.CloudProfileName))
		})

		It("should update the entry in the ShootExtensionStatus", func() {
			// simulate existing entry in the ShootExtensionStatus
			shootExtensionStatus.Statuses = append(shootExtensionStatus.Statuses, gardencorev1alpha1.ExtensionStatus{
				Kind:    "Worker",
				Type:    "aws",
				Purpose: nil,
				Status:  runtime.RawExtension{},
			})
			Expect(gardenClient.Update(ctx, shootExtensionStatus)).ToNot(HaveOccurred())

			executeStatusSyncReconciler()

			shootExtensionStatus := &gardencorev1alpha1.ShootExtensionStatus{
				ObjectMeta: metav1.ObjectMeta{
					Name:      shootName,
					Namespace: shootProjectNs,
				},
			}
			Expect(gardenClient.Get(ctx, kutil.Key(shootExtensionStatus.Namespace, shootExtensionStatus.Name), shootExtensionStatus)).ToNot(HaveOccurred())
			Expect(shootExtensionStatus.Statuses).To(HaveLen(1))

			syncedWorkerStatus := &gardencorev1beta1.Shoot{}
			_, _, err = decoder.Decode(shootExtensionStatus.Statuses[0].Status.Raw, nil, syncedWorkerStatus)
			Expect(err).ToNot(HaveOccurred())
			Expect(syncedWorkerStatus.Spec.CloudProfileName).To(Equal(workerProviderStatus.Spec.CloudProfileName))
		})

		It("should delete existing provider status from the ShootExtensionStatus when an extension resource has been deleted", func() {
			// simulate existing entry in the ShootExtensionStatus
			shootExtensionStatus.Statuses = append(shootExtensionStatus.Statuses, gardencorev1alpha1.ExtensionStatus{
				Kind:    "Worker",
				Type:    "aws",
				Purpose: nil,
				Status:  runtime.RawExtension{},
			})
			Expect(gardenClient.Update(ctx, shootExtensionStatus)).ToNot(HaveOccurred())

			// simulate the worker resource has been deleted
			// the reconcile function below is triggered after the resource has already been deleted
			Expect(seedClient.Delete(ctx, worker)).ToNot(HaveOccurred())

			executeStatusSyncReconciler()

			shootExtensionStatus := &gardencorev1alpha1.ShootExtensionStatus{
				ObjectMeta: metav1.ObjectMeta{
					Name:      shootName,
					Namespace: shootProjectNs,
				},
			}
			Expect(gardenClient.Get(ctx, kutil.Key(shootExtensionStatus.Namespace, shootExtensionStatus.Name), shootExtensionStatus)).ToNot(HaveOccurred())
			Expect(shootExtensionStatus.Statuses).To(HaveLen(0))
		})
	})

	AfterSuite(func() {
		By("running cleanup actions")
		framework.RunCleanupActions()

		By("stopping Seed test environment")
		Expect(seedEnv.Stop()).To(Succeed())
		Expect(gardenEnv.Stop()).To(Succeed())
	})
})

func executeStatusSyncReconciler() {
	control := extensions.NewShootExtensionStatusControl(
		gardenClient,
		seedClient,
		log,
		recorder)

	reconcileFunc := control.CreateShootExtensionStatusSyncReconcileFunc(extensionsv1alpha1.WorkerResource, func() client.Object { return &extensionsv1alpha1.Worker{} })
	_, err := reconcileFunc.Reconcile(ctx, reconcileRequest)
	Expect(err).ToNot(HaveOccurred())
}
