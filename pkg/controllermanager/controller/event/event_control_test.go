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

package event

import (
	"context"
	"time"

	"github.com/gardener/gardener/pkg/controllermanager/apis/config"
	"github.com/gardener/gardener/pkg/logger"
	mockclient "github.com/gardener/gardener/pkg/mock/controller-runtime/client"
	"github.com/go-logr/logr"

	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var _ = Describe("eventReconciler", func() {
	var (
		ctx                    context.Context
		ctrl                   *gomock.Controller
		k8sGardenRuntimeClient *mockclient.MockClient
		oldTimeFunc            = nowFunc

		namespaceName = "garden-foo"
		requestName   = "request"

		ttl = &metav1.Duration{Duration: 1 * time.Hour}

		reconciler                 reconcile.Reconciler
		request                    reconcile.Request
		shootEvent                 *corev1.Event
		nonShootEvent              *corev1.Event
		nonGardenerAPIGroupEvent   *corev1.Event
		eventWithoutInvolvedObject *corev1.Event
		cfg                        *config.EventControllerConfiguration
	)

	BeforeEach(func() {
		ctx = context.TODO()
		ctrl = gomock.NewController(GinkgoT())
		k8sGardenRuntimeClient = mockclient.NewMockClient(ctrl)

		logger.Logger = logger.NewNopLogger()

		shootEvent = &corev1.Event{
			LastTimestamp:  metav1.Time{Time: time.Date(0, 0, 0, 0, 0, 0, 0, time.UTC)},
			InvolvedObject: corev1.ObjectReference{Kind: "Shoot", APIVersion: "core.gardener.cloud/v1beta1"},
		}
		nonShootEvent = &corev1.Event{
			LastTimestamp:  metav1.Time{Time: time.Date(0, 0, 0, 0, 0, 0, 0, time.UTC)},
			InvolvedObject: corev1.ObjectReference{Kind: "Project", APIVersion: "core.gardener.cloud/v1beta1"},
		}
		eventWithoutInvolvedObject = &corev1.Event{
			LastTimestamp: metav1.Time{Time: time.Date(0, 0, 0, 0, 0, 0, 0, time.UTC)},
		}
		nonGardenerAPIGroupEvent = &corev1.Event{
			LastTimestamp:  metav1.Time{Time: time.Date(0, 0, 0, 0, 0, 0, 0, time.UTC)},
			InvolvedObject: corev1.ObjectReference{Kind: "Shoot", APIVersion: "v1"},
		}
		request = reconcile.Request{
			NamespacedName: types.NamespacedName{
				Namespace: namespaceName,
				Name:      requestName,
			}}
		cfg = &config.EventControllerConfiguration{
			TTLNonShootEvents: ttl,
		}

		reconciler = NewEventReconciler(logr.Discard(), k8sGardenRuntimeClient, cfg)
	})

	AfterEach(func() {
		nowFunc = oldTimeFunc
		ctrl.Finish()
	})

	Context("Shoot Events", func() {
		It("should ignore them", func() {
			mockClientGet(k8sGardenRuntimeClient, request.NamespacedName, shootEvent)

			result, err := reconciler.Reconcile(ctx, request)
			Expect(result).To(Equal(reconcile.Result{}))
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("Non-Shoot events", func() {
		Context("ttl is not yet reached", func() {
			BeforeEach(func() {
				nowFunc = func() time.Time {
					return time.Date(0, 0, 0, 0, 0, 0, 0, time.UTC)
				}
			})

			It("should requeue non-shoot events", func() {
				mockClientGet(k8sGardenRuntimeClient, request.NamespacedName, nonShootEvent)

				result, err := reconciler.Reconcile(ctx, request)
				Expect(result).To(Equal(reconcile.Result{Requeue: false, RequeueAfter: ttl.Duration}))
				Expect(err).NotTo(HaveOccurred())
			})

			It("should requeue events with an empty involvedObject", func() {
				mockClientGet(k8sGardenRuntimeClient, request.NamespacedName, eventWithoutInvolvedObject)

				result, err := reconciler.Reconcile(ctx, request)
				Expect(result).To(Equal(reconcile.Result{Requeue: false, RequeueAfter: ttl.Duration}))
				Expect(err).NotTo(HaveOccurred())
			})

			It("should requeue events with non Gardener APIGroup", func() {
				mockClientGet(k8sGardenRuntimeClient, request.NamespacedName, nonGardenerAPIGroupEvent)

				result, err := reconciler.Reconcile(ctx, request)
				Expect(result).To(Equal(reconcile.Result{Requeue: false, RequeueAfter: ttl.Duration}))
				Expect(err).NotTo(HaveOccurred())
			})
		})

		Context("ttl is reached", func() {
			BeforeEach(func() {
				nowFunc = func() time.Time {
					return time.Date(0, 0, 0, 0, 0, 0, 0, time.UTC).
						Add(ttl.Duration)
				}
			})

			It("should delete the event", func() {
				mockClientGet(k8sGardenRuntimeClient, request.NamespacedName, nonShootEvent)
				k8sGardenRuntimeClient.
					EXPECT().
					Delete(context.TODO(), nonShootEvent).
					Return(nil).
					Times(1)

				result, err := reconciler.Reconcile(ctx, request)
				Expect(result).To(Equal(reconcile.Result{}))
				Expect(err).NotTo(HaveOccurred())
			})
		})
	})
})

func mockClientGet(k8sGardenRuntimeClient *mockclient.MockClient, key client.ObjectKey, result *corev1.Event) {
	k8sGardenRuntimeClient.
		EXPECT().
		Get(context.TODO(), key, &corev1.Event{}).
		DoAndReturn(func(_ context.Context, _ client.ObjectKey, o client.Object) error {
			event, ok := o.(*corev1.Event)
			Expect(ok).To(BeTrue())
			result.DeepCopyInto(event)
			return nil
		})
}
