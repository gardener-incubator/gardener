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
	"encoding/json"
	"fmt"

	gardencorev1alpha1 "github.com/gardener/gardener/pkg/apis/core/v1alpha1"
	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	fakeclientset "github.com/gardener/gardener/pkg/client/kubernetes/fake"
	"github.com/gardener/gardener/pkg/gardenlet/controller/federatedseed/extensions"
	"github.com/gardener/gardener/pkg/logger"
	mockclient "github.com/gardener/gardener/pkg/mock/controller-runtime/client"

	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/autoscaling/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var _ = Describe("ShootState Control", func() {
	var (
		extensionObjCreator = func() client.Object { return &extensionsv1alpha1.Extension{} }

		fakeGardenClient  *fakeclientset.ClientSet
		fakeSeedClient    *fakeclientset.ClientSet
		shootStateControl *extensions.ShootStateControl

		ctx = context.TODO()
		log = logger.NewNopLogger()

		shootName             = "my-shoot"
		projectName           = "my-project"
		secretName            = "my-secret"
		clusterName           = fmt.Sprintf("shoot--%s--%s", projectName, shootName)
		controlPlaneNamespace = clusterName
		projectNamespace      = fmt.Sprintf("garden-%s", projectName)
		secretDataJSON        = []byte(fmt.Sprintf(`{"apiVersion":"v1","data":{"data":"c29tZSBzZWNyZXQgZGF0YQ=="},"kind":"Secret","metadata":{"name":"%s","namespace":"%s"}}`, secretName, controlPlaneNamespace))

		reconcileRequest = reconcile.Request{
			NamespacedName: types.NamespacedName{
				Namespace: controlPlaneNamespace,
				Name:      shootName,
			},
		}

		shootState                  *gardencorev1alpha1.ShootState
		shootStateWithExtensionData *gardencorev1alpha1.ShootState
		cluster                     *extensionsv1alpha1.Cluster
		extension                   *extensionsv1alpha1.Extension
		extensionState              *runtime.RawExtension
		extensionResources          []gardencorev1beta1.NamedResourceReference
		stateResources              []gardencorev1alpha1.ResourceData
		secretObj                   *corev1.Secret
	)

	BeforeEach(func() {
		gardenScheme := runtime.NewScheme()
		Expect(gardencorev1alpha1.AddToScheme(gardenScheme)).NotTo(HaveOccurred())
		Expect(gardencorev1beta1.AddToScheme(gardenScheme)).NotTo(HaveOccurred())
		gardenClient := fake.NewFakeClientWithScheme(gardenScheme)
		fakeGardenClient = fakeclientset.NewClientSetBuilder().WithClient(gardenClient).Build()

		seedScheme := runtime.NewScheme()
		Expect(corev1.AddToScheme(seedScheme)).NotTo(HaveOccurred())
		Expect(extensionsv1alpha1.AddToScheme(seedScheme)).NotTo(HaveOccurred())
		seedClient := fake.NewFakeClientWithScheme(seedScheme)
		fakeSeedClient = fakeclientset.NewClientSetBuilder().WithClient(seedClient).Build()

		recorder := record.NewFakeRecorder(64)
		shootStateControl = extensions.NewShootStateControl(fakeGardenClient, fakeSeedClient, log.WithField("seed", "test-seed"), recorder)

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
							Namespace: projectNamespace,
							Name:      shootName,
						},
					}),
				},
			},
		}

		shootState = &gardencorev1alpha1.ShootState{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: projectNamespace,
				Name:      shootName,
			},
		}

		secretObj = &corev1.Secret{}
		Expect(json.Unmarshal(secretDataJSON, secretObj))

		stateResources = []gardencorev1alpha1.ResourceData{
			{
				CrossVersionObjectReference: v1.CrossVersionObjectReference{
					Name:       secretName,
					Kind:       "Secret",
					APIVersion: "v1",
				},
				Data: runtime.RawExtension{
					Raw: secretDataJSON,
				},
			},
		}
		extensionState = &runtime.RawExtension{
			Raw: []byte(`{"data":"data1"}`),
		}

		extensionResources = make([]gardencorev1beta1.NamedResourceReference, 0, len(stateResources))
		for _, resource := range stateResources {
			extensionResources = append(extensionResources, gardencorev1beta1.NamedResourceReference{
				Name:        resource.Name,
				ResourceRef: resource.CrossVersionObjectReference,
			})
		}

		extension = &extensionsv1alpha1.Extension{
			ObjectMeta: metav1.ObjectMeta{
				Name:      shootName,
				Namespace: controlPlaneNamespace,
			},
			Status: extensionsv1alpha1.ExtensionStatus{
				DefaultStatus: extensionsv1alpha1.DefaultStatus{
					State: extensionState.DeepCopy(),
				},
			},
		}

		shootStateWithExtensionData = &gardencorev1alpha1.ShootState{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: projectNamespace,
				Name:      shootName,
			},
			Spec: gardencorev1alpha1.ShootStateSpec{
				Extensions: []gardencorev1alpha1.ExtensionResourceState{
					{
						Kind:  extensionsv1alpha1.ExtensionResource,
						Name:  &shootName,
						State: extensionState.DeepCopy(),
					},
				},
			},
		}
	})

	Describe("#CreateShootStateSyncReconcileFunc", func() {
		It("should properly update ShootState with extension state", func() {
			Expect(fakeGardenClient.Client().Create(ctx, shootState)).To(Succeed())
			Expect(fakeSeedClient.Client().Create(ctx, extension)).To(Succeed())
			Expect(fakeSeedClient.Client().Create(ctx, cluster)).To(Succeed())

			reconcilerFunc := shootStateControl.CreateShootStateSyncReconcileFunc(extensionsv1alpha1.ExtensionResource, extensionObjCreator)
			_, err := reconcilerFunc.Reconcile(ctx, reconcileRequest)
			Expect(err).NotTo(HaveOccurred())

			Expect(fakeGardenClient.Client().Get(ctx, client.ObjectKeyFromObject(shootState), shootState)).To(Succeed())
			Expect(shootState.Spec).To(Equal(shootStateWithExtensionData.Spec))
		})

		It("should properly update ShootState with extension state and resources", func() {
			extension.Status.Resources = extensionResources
			shootStateWithExtensionData.Spec.Resources = stateResources
			shootStateWithExtensionData.Spec.Extensions[0].Resources = extensionResources

			Expect(fakeGardenClient.Client().Create(ctx, shootState)).To(Succeed())
			Expect(fakeSeedClient.Client().Create(ctx, extension)).To(Succeed())
			Expect(fakeSeedClient.Client().Create(ctx, secretObj)).To(Succeed())
			Expect(fakeSeedClient.Client().Create(ctx, cluster)).To(Succeed())

			reconcilerFunc := shootStateControl.CreateShootStateSyncReconcileFunc(extensionsv1alpha1.ExtensionResource, extensionObjCreator)
			_, err := reconcilerFunc.Reconcile(ctx, reconcileRequest)
			Expect(err).NotTo(HaveOccurred())

			Expect(fakeGardenClient.Client().Get(ctx, client.ObjectKeyFromObject(shootState), shootState)).To(Succeed())
			Expect(shootState.Spec).To(Equal(shootStateWithExtensionData.Spec))
		})

		It("should properly update ShootState with extension state if it was changed", func() {
			extension.Status.State = &runtime.RawExtension{Raw: []byte(`{"data":"newdata"}`)}
			expectedShootState := shootStateWithExtensionData.DeepCopy()
			expectedShootState.Spec.Extensions[0].State = extension.Status.State

			Expect(fakeGardenClient.Client().Create(ctx, shootStateWithExtensionData)).To(Succeed())
			Expect(fakeSeedClient.Client().Create(ctx, extension)).To(Succeed())
			Expect(fakeSeedClient.Client().Create(ctx, cluster)).To(Succeed())

			reconcilerFunc := shootStateControl.CreateShootStateSyncReconcileFunc(extensionsv1alpha1.ExtensionResource, extensionObjCreator)
			_, err := reconcilerFunc.Reconcile(ctx, reconcileRequest)
			Expect(err).NotTo(HaveOccurred())

			Expect(fakeGardenClient.Client().Get(ctx, client.ObjectKeyFromObject(shootState), shootState)).To(Succeed())
			Expect(shootState.Spec).To(Equal(expectedShootState.Spec))
		})

		It("should update ShootState with extension resources if they changed", func() {
			extension.Status.Resources = extensionResources
			shootStateWithExtensionData.Spec.Resources = stateResources
			shootStateWithExtensionData.Spec.Extensions[0].Resources = extensionResources

			newSecretJSON := []byte(fmt.Sprintf(`{"apiVersion":"v1","data":{"data":"bmV3IHNlY3JldCBkYXRh"},"kind":"Secret","metadata":{"name":"%s","namespace":"%s"}}`, secretName, controlPlaneNamespace))
			newSecretObj := &corev1.Secret{}
			Expect(json.Unmarshal(newSecretJSON, newSecretObj)).To(Succeed())
			expectedShootState := shootStateWithExtensionData.DeepCopy()
			expectedShootState.Spec.Resources[0].Data.Raw = newSecretJSON

			Expect(fakeGardenClient.Client().Create(ctx, shootStateWithExtensionData)).To(Succeed())
			Expect(fakeSeedClient.Client().Create(ctx, extension)).To(Succeed())
			Expect(fakeSeedClient.Client().Create(ctx, newSecretObj)).To(Succeed())
			Expect(fakeSeedClient.Client().Create(ctx, cluster)).To(Succeed())

			reconcilerFunc := shootStateControl.CreateShootStateSyncReconcileFunc(extensionsv1alpha1.ExtensionResource, extensionObjCreator)
			_, err := reconcilerFunc.Reconcile(ctx, reconcileRequest)
			Expect(err).NotTo(HaveOccurred())

			Expect(fakeGardenClient.Client().Get(ctx, client.ObjectKeyFromObject(shootState), shootState)).To(Succeed())
			Expect(shootState.Spec).To(Equal(expectedShootState.Spec))
		})

		It("should remove the extension state from the ShootState if its new value is null", func() {
			expectedShootState := shootState.DeepCopy()
			extension.Status.State = nil
			Expect(fakeGardenClient.Client().Create(ctx, shootStateWithExtensionData)).To(Succeed())
			Expect(fakeSeedClient.Client().Create(ctx, extension)).To(Succeed())
			Expect(fakeSeedClient.Client().Create(ctx, cluster)).To(Succeed())

			reconcilerFunc := shootStateControl.CreateShootStateSyncReconcileFunc(extensionsv1alpha1.ExtensionResource, extensionObjCreator)
			_, err := reconcilerFunc.Reconcile(ctx, reconcileRequest)
			Expect(err).NotTo(HaveOccurred())

			Expect(fakeGardenClient.Client().Get(ctx, client.ObjectKeyFromObject(shootState), shootState)).To(Succeed())
			Expect(shootState.Spec).To(Equal(expectedShootState.Spec))
		})

		It("should delete resources which do not exist in the new extension state", func() {
			expectedShootState := shootStateWithExtensionData.DeepCopy()

			shootStateWithExtensionData.Spec.Resources = stateResources
			shootStateWithExtensionData.Spec.Extensions[0].Resources = extensionResources

			Expect(fakeGardenClient.Client().Create(ctx, shootStateWithExtensionData)).To(Succeed())
			Expect(fakeSeedClient.Client().Create(ctx, extension)).To(Succeed())
			Expect(fakeSeedClient.Client().Create(ctx, cluster)).To(Succeed())

			reconcilerFunc := shootStateControl.CreateShootStateSyncReconcileFunc(extensionsv1alpha1.ExtensionResource, extensionObjCreator)
			_, err := reconcilerFunc.Reconcile(ctx, reconcileRequest)
			Expect(err).NotTo(HaveOccurred())

			Expect(fakeGardenClient.Client().Get(ctx, client.ObjectKeyFromObject(shootState), shootState)).To(Succeed())
			Expect(shootState.Spec).To(Equal(expectedShootState.Spec))
		})

		It("should not try to patch the ShootState if there are no changes to the extension state", func() {
			ctrl := gomock.NewController(GinkgoT())
			mc := mockclient.NewMockClient(ctrl)
			fakeClientSet := fakeclientset.NewClientSetBuilder().WithDirectClient(mc).WithClient(mc).Build()
			recorder := record.NewFakeRecorder(64)
			shootStateControl = extensions.NewShootStateControl(fakeClientSet, fakeClientSet, log.WithField("seed", "test-seed"), recorder)
			gomock.InOrder(
				mc.EXPECT().Get(ctx, client.ObjectKeyFromObject(extension), gomock.AssignableToTypeOf(&extensionsv1alpha1.Extension{})),
				mc.EXPECT().Get(ctx, client.ObjectKeyFromObject(cluster), gomock.AssignableToTypeOf(&extensionsv1alpha1.Cluster{})).SetArg(2, *cluster),
				mc.EXPECT().Get(ctx, client.ObjectKeyFromObject(shootState), gomock.AssignableToTypeOf(&gardencorev1alpha1.ShootState{})),
			)
			reconcilerFunc := shootStateControl.CreateShootStateSyncReconcileFunc(extensionsv1alpha1.ExtensionResource, extensionObjCreator)
			_, err := reconcilerFunc.Reconcile(ctx, reconcileRequest)
			Expect(err).NotTo(HaveOccurred())

			ctrl.Finish()
		})

		It("should not throw any errors if Cluster resource does not exists", func() {
			Expect(fakeSeedClient.Client().Create(ctx, extension)).To(Succeed())

			reconcilerFunc := shootStateControl.CreateShootStateSyncReconcileFunc(extensionsv1alpha1.ExtensionResource, extensionObjCreator)
			_, err := reconcilerFunc.Reconcile(ctx, reconcileRequest)
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
