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

package controllerdeployment

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	mockclient "github.com/gardener/gardener/pkg/mock/controller-runtime/client"
	kutil "github.com/gardener/gardener/pkg/utils/kubernetes"
)

var _ = Describe("Controller", func() {
	var (
		ctx  = context.TODO()
		ctrl *gomock.Controller
		c    *mockclient.MockClient
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		c = mockclient.NewMockClient(ctrl)
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	Describe("controllerDeploymentReconciler", func() {
		const finalizerName = "core.gardener.cloud/controllerdeployment"

		var (
			controllerDeploymentName string
			fakeErr                  error
			rec                      reconcile.Reconciler
			controllerDeployment     *gardencorev1beta1.ControllerDeployment
		)

		BeforeEach(func() {
			controllerDeploymentName = "controllerDeployment"
			fakeErr = fmt.Errorf("fake err")
			rec = &reconciler{
				logger:       logr.Discard(),
				gardenClient: c,
			}
			controllerDeployment = &gardencorev1beta1.ControllerDeployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:            controllerDeploymentName,
					ResourceVersion: "42",
				},
			}
		})

		It("should return nil because object not found", func() {
			c.EXPECT().Get(ctx, kutil.Key(controllerDeploymentName), gomock.AssignableToTypeOf(&gardencorev1beta1.ControllerDeployment{})).Return(apierrors.NewNotFound(schema.GroupResource{}, ""))

			result, err := rec.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: controllerDeploymentName}})
			Expect(result).To(Equal(reconcile.Result{}))
			Expect(err).NotTo(HaveOccurred())
		})

		It("should return err because object reading failed", func() {
			c.EXPECT().Get(ctx, kutil.Key(controllerDeploymentName), gomock.AssignableToTypeOf(&gardencorev1beta1.ControllerDeployment{})).Return(fakeErr)

			result, err := rec.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: controllerDeploymentName}})
			Expect(result).To(Equal(reconcile.Result{}))
			Expect(err).To(MatchError(fakeErr))
		})

		Context("when deletion timestamp not set", func() {
			BeforeEach(func() {
				c.EXPECT().Get(ctx, kutil.Key(controllerDeploymentName), gomock.AssignableToTypeOf(&gardencorev1beta1.ControllerDeployment{})).DoAndReturn(func(_ context.Context, _ client.ObjectKey, obj *gardencorev1beta1.ControllerDeployment) error {
					*obj = *controllerDeployment
					return nil
				})
			})

			It("should ensure the finalizer (error)", func() {
				errToReturn := apierrors.NewNotFound(schema.GroupResource{}, controllerDeploymentName)

				c.EXPECT().Patch(ctx, gomock.AssignableToTypeOf(&gardencorev1beta1.ControllerDeployment{}), gomock.Any()).DoAndReturn(func(_ context.Context, o client.Object, patch client.Patch, opts ...client.PatchOption) error {
					Expect(patch.Data(o)).To(BeEquivalentTo(fmt.Sprintf(`{"metadata":{"finalizers":["%s"],"resourceVersion":"42"}}`, finalizerName)))
					return errToReturn
				})

				result, err := rec.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: controllerDeploymentName}})
				Expect(result).To(Equal(reconcile.Result{}))
				Expect(err).To(MatchError(err))
			})

			It("should ensure the finalizer (no error)", func() {
				c.EXPECT().Patch(ctx, gomock.AssignableToTypeOf(&gardencorev1beta1.ControllerDeployment{}), gomock.Any()).DoAndReturn(func(_ context.Context, o client.Object, patch client.Patch, opts ...client.PatchOption) error {
					Expect(patch.Data(o)).To(BeEquivalentTo(fmt.Sprintf(`{"metadata":{"finalizers":["%s"],"resourceVersion":"42"}}`, finalizerName)))
					return nil
				})

				result, err := rec.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: controllerDeploymentName}})
				Expect(result).To(Equal(reconcile.Result{}))
				Expect(err).NotTo(HaveOccurred())
			})
		})

		Context("when deletion timestamp set", func() {
			BeforeEach(func() {
				now := metav1.Now()
				controllerDeployment.DeletionTimestamp = &now
				controllerDeployment.Finalizers = []string{FinalizerName}

				c.EXPECT().Get(ctx, kutil.Key(controllerDeploymentName), gomock.AssignableToTypeOf(&gardencorev1beta1.ControllerDeployment{})).DoAndReturn(func(_ context.Context, _ client.ObjectKey, obj *gardencorev1beta1.ControllerDeployment) error {
					*obj = *controllerDeployment
					return nil
				})
			})

			It("should do nothing because finalizer is not present", func() {
				controllerDeployment.Finalizers = nil

				result, err := rec.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: controllerDeploymentName}})
				Expect(result).To(Equal(reconcile.Result{}))
				Expect(err).NotTo(HaveOccurred())
			})

			It("should return an error because ControllerRegistration list failed", func() {
				c.EXPECT().List(ctx, gomock.AssignableToTypeOf(&gardencorev1beta1.ControllerRegistrationList{})).Return(fakeErr)

				result, err := rec.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: controllerDeploymentName}})
				Expect(result).To(Equal(reconcile.Result{}))
				Expect(err).To(MatchError(fakeErr))
			})

			It("should return an error because ControllerRegistration referencing ControllerDeployment exists", func() {
				c.EXPECT().List(ctx, gomock.AssignableToTypeOf(&gardencorev1beta1.ControllerRegistrationList{})).DoAndReturn(func(_ context.Context, obj *gardencorev1beta1.ControllerRegistrationList, _ ...client.ListOption) error {
					(&gardencorev1beta1.ControllerRegistrationList{Items: []gardencorev1beta1.ControllerRegistration{
						{
							Spec: gardencorev1beta1.ControllerRegistrationSpec{
								Deployment: &gardencorev1beta1.ControllerRegistrationDeployment{
									DeploymentRefs: []gardencorev1beta1.DeploymentRef{
										{Name: controllerDeploymentName},
									},
								},
							},
						},
					}}).DeepCopyInto(obj)
					return nil
				})

				result, err := rec.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: controllerDeploymentName}})
				Expect(result).To(Equal(reconcile.Result{}))
				Expect(err).To(MatchError(ContainSubstring("cannot remove finalizer")))
			})

			It("should remove the finalizer (error)", func() {
				c.EXPECT().List(ctx, gomock.AssignableToTypeOf(&gardencorev1beta1.ControllerRegistrationList{})).DoAndReturn(func(_ context.Context, obj *gardencorev1beta1.ControllerRegistrationList, _ ...client.ListOption) error {
					(&gardencorev1beta1.ControllerRegistrationList{}).DeepCopyInto(obj)
					return nil
				})

				c.EXPECT().Patch(ctx, gomock.AssignableToTypeOf(&gardencorev1beta1.ControllerDeployment{}), gomock.Any()).DoAndReturn(func(_ context.Context, o client.Object, patch client.Patch, opts ...client.PatchOption) error {
					Expect(patch.Data(o)).To(BeEquivalentTo(`{"metadata":{"finalizers":null,"resourceVersion":"42"}}`))
					return fakeErr
				})

				result, err := rec.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: controllerDeploymentName}})
				Expect(result).To(Equal(reconcile.Result{}))
				Expect(err).To(MatchError(fakeErr))
			})

			It("should remove the finalizer (no error)", func() {
				c.EXPECT().List(ctx, gomock.AssignableToTypeOf(&gardencorev1beta1.ControllerRegistrationList{})).DoAndReturn(func(_ context.Context, obj *gardencorev1beta1.ControllerRegistrationList, _ ...client.ListOption) error {
					(&gardencorev1beta1.ControllerRegistrationList{}).DeepCopyInto(obj)
					return nil
				})

				c.EXPECT().Patch(ctx, gomock.AssignableToTypeOf(&gardencorev1beta1.ControllerDeployment{}), gomock.Any()).DoAndReturn(func(_ context.Context, o client.Object, patch client.Patch, opts ...client.PatchOption) error {
					Expect(patch.Data(o)).To(BeEquivalentTo(`{"metadata":{"finalizers":null,"resourceVersion":"42"}}`))
					return nil
				})

				result, err := rec.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: controllerDeploymentName}})
				Expect(result).To(Equal(reconcile.Result{}))
				Expect(err).NotTo(HaveOccurred())
			})
		})
	})
})
