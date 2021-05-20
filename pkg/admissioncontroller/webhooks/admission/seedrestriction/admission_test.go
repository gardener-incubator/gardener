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

package seedrestriction_test

import (
	"context"
	"fmt"
	"net/http"

	. "github.com/gardener/gardener/pkg/admissioncontroller/webhooks/admission/seedrestriction"
	gardencorev1alpha1 "github.com/gardener/gardener/pkg/apis/core/v1alpha1"
	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	gardenoperationsv1alpha1 "github.com/gardener/gardener/pkg/apis/operations/v1alpha1"
	seedmanagementv1alpha1 "github.com/gardener/gardener/pkg/apis/seedmanagement/v1alpha1"
	"github.com/gardener/gardener/pkg/client/kubernetes"
	mockcache "github.com/gardener/gardener/pkg/mock/controller-runtime/cache"
	kutil "github.com/gardener/gardener/pkg/utils/kubernetes"

	"github.com/go-logr/logr"
	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	certificatesv1beta1 "k8s.io/api/certificates/v1beta1"
	coordinationv1 "k8s.io/api/coordination/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logzap "sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

var _ = Describe("handler", func() {
	var (
		ctx     = context.TODO()
		fakeErr = fmt.Errorf("fake")
		err     error

		ctrl      *gomock.Controller
		mockCache *mockcache.MockCache
		decoder   *admission.Decoder

		logger  logr.Logger
		handler admission.Handler
		request admission.Request
		encoder runtime.Encoder

		seedName                string
		seedUser, ambiguousUser authenticationv1.UserInfo

		responseAllowed = admission.Response{
			AdmissionResponse: admissionv1.AdmissionResponse{
				Allowed: true,
				Result: &metav1.Status{
					Code: int32(http.StatusOK),
				},
			},
		}
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockCache = mockcache.NewMockCache(ctrl)
		decoder, err = admission.NewDecoder(kubernetes.GardenScheme)
		Expect(err).NotTo(HaveOccurred())

		logger = logzap.New(logzap.WriteTo(GinkgoWriter))
		request = admission.Request{}
		encoder = &json.Serializer{}

		mockCache.EXPECT().GetInformer(ctx, gomock.AssignableToTypeOf(&gardencorev1beta1.BackupBucket{}))
		mockCache.EXPECT().GetInformer(ctx, gomock.AssignableToTypeOf(&seedmanagementv1alpha1.ManagedSeed{}))
		mockCache.EXPECT().GetInformer(ctx, gomock.AssignableToTypeOf(&gardencorev1beta1.Shoot{}))

		handler, err = New(ctx, logger, mockCache)
		Expect(err).NotTo(HaveOccurred())
		Expect(admission.InjectDecoderInto(decoder, handler)).To(BeTrue())

		seedName = "seed"
		seedUser = authenticationv1.UserInfo{
			Username: fmt.Sprintf("%s%s", v1beta1constants.SeedUserNamePrefix, seedName),
			Groups:   []string{v1beta1constants.SeedsGroup},
		}
		ambiguousUser = authenticationv1.UserInfo{
			Username: fmt.Sprintf("%s%s", v1beta1constants.SeedUserNamePrefix, v1beta1constants.SeedUserNameSuffixAmbiguous),
			Groups:   []string{v1beta1constants.SeedsGroup},
		}
	})

	Describe("#Handle", func() {
		Context("when resource is unhandled", func() {
			It("should have no opinion because no seed", func() {
				request.UserInfo = authenticationv1.UserInfo{Username: "foo"}

				Expect(handler.Handle(ctx, request)).To(Equal(responseAllowed))
			})

			It("should have no opinion because no resource request", func() {
				request.UserInfo = seedUser

				Expect(handler.Handle(ctx, request)).To(Equal(responseAllowed))
			})

			It("should have no opinion because resource is irrelevant", func() {
				request.UserInfo = seedUser
				request.Resource = metav1.GroupVersionResource{}

				Expect(handler.Handle(ctx, request)).To(Equal(responseAllowed))
			})
		})

		Context("when requested for ShootStates", func() {
			var name, namespace string

			BeforeEach(func() {
				name, namespace = "foo", "bar"

				request.Name = name
				request.Namespace = namespace
				request.UserInfo = seedUser
				request.Resource = metav1.GroupVersionResource{
					Group:    gardencorev1alpha1.SchemeGroupVersion.Group,
					Resource: "shootstates",
				}
			})

			DescribeTable("should forbid because no allowed verb",
				func(operation admissionv1.Operation) {
					request.Operation = operation

					Expect(handler.Handle(ctx, request)).To(Equal(admission.Response{
						AdmissionResponse: admissionv1.AdmissionResponse{
							Allowed: false,
							Result: &metav1.Status{
								Code:    int32(http.StatusBadRequest),
								Message: fmt.Sprintf("unexpected operation: %q", operation),
							},
						},
					}))
				},

				Entry("update", admissionv1.Update),
				Entry("delete", admissionv1.Delete),
				Entry("connect", admissionv1.Connect),
			)

			Context("when operation is create", func() {
				BeforeEach(func() {
					request.Operation = admissionv1.Create
				})

				It("should return an error because fetching the related shoot failed", func() {
					mockCache.EXPECT().Get(ctx, kutil.Key(namespace, name), gomock.AssignableToTypeOf(&gardencorev1beta1.Shoot{})).Return(fakeErr)

					Expect(handler.Handle(ctx, request)).To(Equal(admission.Response{
						AdmissionResponse: admissionv1.AdmissionResponse{
							Allowed: false,
							Result: &metav1.Status{
								Code:    int32(http.StatusInternalServerError),
								Message: fakeErr.Error(),
							},
						},
					}))
				})

				DescribeTable("should forbid the request because the seed name of the related shoot does not match",
					func(seedNameInShoot *string) {
						mockCache.EXPECT().Get(ctx, kutil.Key(namespace, name), gomock.AssignableToTypeOf(&gardencorev1beta1.Shoot{})).DoAndReturn(func(_ context.Context, _ client.ObjectKey, obj *gardencorev1beta1.Shoot) error {
							(&gardencorev1beta1.Shoot{Spec: gardencorev1beta1.ShootSpec{SeedName: seedNameInShoot}}).DeepCopyInto(obj)
							return nil
						})

						Expect(handler.Handle(ctx, request)).To(Equal(admission.Response{
							AdmissionResponse: admissionv1.AdmissionResponse{
								Allowed: false,
								Result: &metav1.Status{
									Code:    int32(http.StatusForbidden),
									Message: fmt.Sprintf("object does not belong to seed %q", seedName),
								},
							},
						}))
					},

					Entry("seed name is nil", nil),
					Entry("seed name is different", pointer.StringPtr("some-different-seed")),
				)

				It("should allow the request because seed name matches", func() {
					mockCache.EXPECT().Get(ctx, kutil.Key(namespace, name), gomock.AssignableToTypeOf(&gardencorev1beta1.Shoot{})).DoAndReturn(func(_ context.Context, _ client.ObjectKey, obj *gardencorev1beta1.Shoot) error {
						(&gardencorev1beta1.Shoot{Spec: gardencorev1beta1.ShootSpec{SeedName: &seedName}}).DeepCopyInto(obj)
						return nil
					})

					Expect(handler.Handle(ctx, request)).To(Equal(responseAllowed))
				})

				It("should allow the request because seed name is ambiguous", func() {
					request.UserInfo = ambiguousUser

					mockCache.EXPECT().Get(ctx, kutil.Key(namespace, name), gomock.AssignableToTypeOf(&gardencorev1beta1.Shoot{})).DoAndReturn(func(_ context.Context, _ client.ObjectKey, obj *gardencorev1beta1.Shoot) error {
						(&gardencorev1beta1.Shoot{Spec: gardencorev1beta1.ShootSpec{SeedName: pointer.StringPtr("some-different-seed")}}).DeepCopyInto(obj)
						return nil
					})

					Expect(handler.Handle(ctx, request)).To(Equal(responseAllowed))
				})
			})
		})

		Context("when requested for ShootExtensionStatuses", func() {
			var name, namespace string

			BeforeEach(func() {
				name, namespace = "foo", "bar"

				request.Name = name
				request.Namespace = namespace
				request.UserInfo = seedUser
				request.Resource = metav1.GroupVersionResource{
					Group:    gardencorev1alpha1.SchemeGroupVersion.Group,
					Resource: "shootextensionstatuses",
				}
			})

			DescribeTable("should forbid because no allowed verb",
				func(operation admissionv1.Operation) {
					request.Operation = operation

					Expect(handler.Handle(ctx, request)).To(Equal(admission.Response{
						AdmissionResponse: admissionv1.AdmissionResponse{
							Allowed: false,
							Result: &metav1.Status{
								Code:    int32(http.StatusBadRequest),
								Message: fmt.Sprintf("unexpected operation: %q", operation),
							},
						},
					}))
				},

				Entry("update", admissionv1.Update),
				Entry("delete", admissionv1.Delete),
				Entry("connect", admissionv1.Connect),
			)

			Context("when operation is create", func() {
				BeforeEach(func() {
					request.Operation = admissionv1.Create
				})

				It("should return an error because fetching the related shoot failed", func() {
					mockCache.EXPECT().Get(ctx, kutil.Key(namespace, name), gomock.AssignableToTypeOf(&gardencorev1beta1.Shoot{})).Return(fakeErr)

					Expect(handler.Handle(ctx, request)).To(Equal(admission.Response{
						AdmissionResponse: admissionv1.AdmissionResponse{
							Allowed: false,
							Result: &metav1.Status{
								Code:    int32(http.StatusInternalServerError),
								Message: fakeErr.Error(),
							},
						},
					}))
				})

				DescribeTable("should forbid the request because the seed name of the related shoot does not match",
					func(seedNameInShoot *string) {
						mockCache.EXPECT().Get(ctx, kutil.Key(namespace, name), gomock.AssignableToTypeOf(&gardencorev1beta1.Shoot{})).DoAndReturn(func(_ context.Context, _ client.ObjectKey, obj *gardencorev1beta1.Shoot) error {
							(&gardencorev1beta1.Shoot{Spec: gardencorev1beta1.ShootSpec{SeedName: seedNameInShoot}}).DeepCopyInto(obj)
							return nil
						})

						Expect(handler.Handle(ctx, request)).To(Equal(admission.Response{
							AdmissionResponse: admissionv1.AdmissionResponse{
								Allowed: false,
								Result: &metav1.Status{
									Code:    int32(http.StatusForbidden),
									Message: fmt.Sprintf("object does not belong to seed %q", seedName),
								},
							},
						}))
					},

					Entry("seed name is nil", nil),
					Entry("seed name is different", pointer.StringPtr("some-different-seed")),
				)

				It("should allow the request because seed name matches", func() {
					mockCache.EXPECT().Get(ctx, kutil.Key(namespace, name), gomock.AssignableToTypeOf(&gardencorev1beta1.Shoot{})).DoAndReturn(func(_ context.Context, _ client.ObjectKey, obj *gardencorev1beta1.Shoot) error {
						(&gardencorev1beta1.Shoot{Spec: gardencorev1beta1.ShootSpec{SeedName: &seedName}}).DeepCopyInto(obj)
						return nil
					})

					Expect(handler.Handle(ctx, request)).To(Equal(responseAllowed))
				})

				It("should allow the request because seed name is ambiguous", func() {
					request.UserInfo = ambiguousUser

					mockCache.EXPECT().Get(ctx, kutil.Key(namespace, name), gomock.AssignableToTypeOf(&gardencorev1beta1.Shoot{})).DoAndReturn(func(_ context.Context, _ client.ObjectKey, obj *gardencorev1beta1.Shoot) error {
						(&gardencorev1beta1.Shoot{Spec: gardencorev1beta1.ShootSpec{SeedName: pointer.StringPtr("some-different-seed")}}).DeepCopyInto(obj)
						return nil
					})

					Expect(handler.Handle(ctx, request)).To(Equal(responseAllowed))
				})
			})
		})

		Context("when requested for BackupBuckets", func() {
			var name string

			BeforeEach(func() {
				name = "foo"

				request.Name = name
				request.UserInfo = seedUser
				request.Resource = metav1.GroupVersionResource{
					Group:    gardencorev1beta1.SchemeGroupVersion.Group,
					Resource: "backupbuckets",
				}
			})

			DescribeTable("should not allow the request because no allowed verb",
				func(operation admissionv1.Operation) {
					request.Operation = operation

					Expect(handler.Handle(ctx, request)).To(Equal(admission.Response{
						AdmissionResponse: admissionv1.AdmissionResponse{
							Allowed: false,
							Result: &metav1.Status{
								Code:    int32(http.StatusBadRequest),
								Message: fmt.Sprintf("unexpected operation: %q", operation),
							},
						},
					}))
				},

				Entry("update", admissionv1.Update),
				Entry("delete", admissionv1.Delete),
				Entry("connect", admissionv1.Connect),
			)

			Context("when operation is create", func() {
				BeforeEach(func() {
					request.Operation = admissionv1.Create
				})

				It("should return an error because decoding the object failed", func() {
					request.Object.Raw = []byte(`{]`)

					Expect(handler.Handle(ctx, request)).To(Equal(admission.Response{
						AdmissionResponse: admissionv1.AdmissionResponse{
							Allowed: false,
							Result: &metav1.Status{
								Code:    int32(http.StatusBadRequest),
								Message: "couldn't get version/kind; json parse error: invalid character ']' looking for beginning of object key string",
							},
						},
					}))
				})

				DescribeTable("should forbid the request because the seed name of the related bucket does not match",
					func(seedNameInBackupBucket *string) {
						objData, err := runtime.Encode(encoder, &gardencorev1beta1.BackupBucket{
							Spec: gardencorev1beta1.BackupBucketSpec{
								SeedName: seedNameInBackupBucket,
							},
						})
						Expect(err).NotTo(HaveOccurred())
						request.Object.Raw = objData

						Expect(handler.Handle(ctx, request)).To(Equal(admission.Response{
							AdmissionResponse: admissionv1.AdmissionResponse{
								Allowed: false,
								Result: &metav1.Status{
									Code:    int32(http.StatusForbidden),
									Message: fmt.Sprintf("object does not belong to seed %q", seedName),
								},
							},
						}))
					},

					Entry("seed name is nil", nil),
					Entry("seed name is different", pointer.StringPtr("some-different-seed")),
				)

				It("should allow the request because seed name matches", func() {
					objData, err := runtime.Encode(encoder, &gardencorev1beta1.BackupBucket{
						Spec: gardencorev1beta1.BackupBucketSpec{
							SeedName: &seedName,
						},
					})
					Expect(err).NotTo(HaveOccurred())
					request.Object.Raw = objData

					Expect(handler.Handle(ctx, request)).To(Equal(responseAllowed))
				})

				It("should allow the request because seed name is ambiguous", func() {
					request.UserInfo = ambiguousUser

					objData, err := runtime.Encode(encoder, &gardencorev1beta1.BackupBucket{
						Spec: gardencorev1beta1.BackupBucketSpec{
							SeedName: pointer.StringPtr("some-different-seed"),
						},
					})
					Expect(err).NotTo(HaveOccurred())
					request.Object.Raw = objData

					Expect(handler.Handle(ctx, request)).To(Equal(responseAllowed))
				})
			})
		})

		Context("when requested for BackupEntrys", func() {
			var name, namespace, bucketName string

			BeforeEach(func() {
				name = "foo"
				namespace = "bar"
				bucketName = "bucket"

				request.Name = name
				request.Namespace = namespace
				request.UserInfo = seedUser
				request.Resource = metav1.GroupVersionResource{
					Group:    gardencorev1beta1.SchemeGroupVersion.Group,
					Resource: "backupentries",
				}
			})

			DescribeTable("should not allow the request because no allowed verb",
				func(operation admissionv1.Operation) {
					request.Operation = operation

					Expect(handler.Handle(ctx, request)).To(Equal(admission.Response{
						AdmissionResponse: admissionv1.AdmissionResponse{
							Allowed: false,
							Result: &metav1.Status{
								Code:    int32(http.StatusBadRequest),
								Message: fmt.Sprintf("unexpected operation: %q", operation),
							},
						},
					}))
				},

				Entry("update", admissionv1.Update),
				Entry("delete", admissionv1.Delete),
				Entry("connect", admissionv1.Connect),
			)

			Context("when operation is create", func() {
				BeforeEach(func() {
					request.Operation = admissionv1.Create
				})

				It("should return an error because decoding the object failed", func() {
					request.Object.Raw = []byte(`{]`)

					Expect(handler.Handle(ctx, request)).To(Equal(admission.Response{
						AdmissionResponse: admissionv1.AdmissionResponse{
							Allowed: false,
							Result: &metav1.Status{
								Code:    int32(http.StatusBadRequest),
								Message: "couldn't get version/kind; json parse error: invalid character ']' looking for beginning of object key string",
							},
						},
					}))
				})

				DescribeTable("should forbid the request because the seed name of the related entry does not match",
					func(seedNameInBackupEntry, seedNameInBackupBucket *string) {
						objData, err := runtime.Encode(encoder, &gardencorev1beta1.BackupEntry{
							Spec: gardencorev1beta1.BackupEntrySpec{
								BucketName: bucketName,
								SeedName:   seedNameInBackupEntry,
							},
						})
						Expect(err).NotTo(HaveOccurred())
						request.Object.Raw = objData

						if seedNameInBackupEntry != nil && *seedNameInBackupEntry == seedName {
							mockCache.EXPECT().Get(ctx, kutil.Key(bucketName), gomock.AssignableToTypeOf(&gardencorev1beta1.BackupBucket{})).DoAndReturn(func(_ context.Context, _ client.ObjectKey, obj *gardencorev1beta1.BackupBucket) error {
								(&gardencorev1beta1.BackupBucket{Spec: gardencorev1beta1.BackupBucketSpec{SeedName: seedNameInBackupBucket}}).DeepCopyInto(obj)
								return nil
							})
						}

						Expect(handler.Handle(ctx, request)).To(Equal(admission.Response{
							AdmissionResponse: admissionv1.AdmissionResponse{
								Allowed: false,
								Result: &metav1.Status{
									Code:    int32(http.StatusForbidden),
									Message: fmt.Sprintf("object does not belong to seed %q", seedName),
								},
							},
						}))
					},

					Entry("seed name is nil", nil, nil),
					Entry("seed name is different", pointer.StringPtr("some-different-seed"), nil),
					Entry("seed name is equal but bucket's seed name is nil", &seedName, nil),
					Entry("seed name is equal but bucket's seed name is different", &seedName, pointer.StringPtr("some-different-seed")),
				)

				It("should allow the request because seed name matches for both entry and bucket", func() {
					objData, err := runtime.Encode(encoder, &gardencorev1beta1.BackupEntry{
						Spec: gardencorev1beta1.BackupEntrySpec{
							BucketName: bucketName,
							SeedName:   &seedName,
						},
					})
					Expect(err).NotTo(HaveOccurred())
					request.Object.Raw = objData

					mockCache.EXPECT().Get(ctx, kutil.Key(bucketName), gomock.AssignableToTypeOf(&gardencorev1beta1.BackupBucket{})).DoAndReturn(func(_ context.Context, _ client.ObjectKey, obj *gardencorev1beta1.BackupBucket) error {
						(&gardencorev1beta1.BackupBucket{Spec: gardencorev1beta1.BackupBucketSpec{SeedName: &seedName}}).DeepCopyInto(obj)
						return nil
					})

					Expect(handler.Handle(ctx, request)).To(Equal(responseAllowed))
				})

				DescribeTable("should allow the request because seed name is ambiguous",
					func(seedNameInBackupEntry, seedNameInBackupBucket *string) {
						request.UserInfo = ambiguousUser

						objData, err := runtime.Encode(encoder, &gardencorev1beta1.BackupEntry{
							Spec: gardencorev1beta1.BackupEntrySpec{
								BucketName: bucketName,
								SeedName:   seedNameInBackupEntry,
							},
						})
						Expect(err).NotTo(HaveOccurred())
						request.Object.Raw = objData

						mockCache.EXPECT().Get(ctx, kutil.Key(bucketName), gomock.AssignableToTypeOf(&gardencorev1beta1.BackupBucket{})).DoAndReturn(func(_ context.Context, _ client.ObjectKey, obj *gardencorev1beta1.BackupBucket) error {
							(&gardencorev1beta1.BackupBucket{Spec: gardencorev1beta1.BackupBucketSpec{SeedName: seedNameInBackupBucket}}).DeepCopyInto(obj)
							return nil
						})

						Expect(handler.Handle(ctx, request)).To(Equal(responseAllowed))
					},

					Entry("seed name nil", nil, nil),
					Entry("seed name is different", pointer.StringPtr("some-different-seed"), nil),
					Entry("seed name is equal but bucket's seed name is nil", &seedName, nil),
					Entry("seed name is equal but bucket's seed name is different", &seedName, pointer.StringPtr("some-different-seed")),
				)
			})
		})

		Context("when requested for Bastions", func() {
			var name string

			BeforeEach(func() {
				name = "foo"

				request.Name = name
				request.UserInfo = seedUser
				request.Resource = metav1.GroupVersionResource{
					Group:    gardenoperationsv1alpha1.SchemeGroupVersion.Group,
					Resource: "bastions",
				}
			})

			DescribeTable("should have no opinion because no allowed verb",
				func(operation admissionv1.Operation) {
					request.Operation = operation

					Expect(handler.Handle(ctx, request)).To(Equal(admission.Response{
						AdmissionResponse: admissionv1.AdmissionResponse{
							Allowed: false,
							Result: &metav1.Status{
								Code:    int32(http.StatusBadRequest),
								Message: fmt.Sprintf("unexpected operation: %q", operation),
							},
						},
					}))
				},

				Entry("update", admissionv1.Update),
				Entry("delete", admissionv1.Delete),
				Entry("connect", admissionv1.Connect),
			)

			Context("when operation is create", func() {
				BeforeEach(func() {
					request.Operation = admissionv1.Create
				})

				It("should return an error because decoding the object failed", func() {
					request.Object.Raw = []byte(`{]`)

					Expect(handler.Handle(ctx, request)).To(Equal(admission.Response{
						AdmissionResponse: admissionv1.AdmissionResponse{
							Allowed: false,
							Result: &metav1.Status{
								Code:    int32(http.StatusBadRequest),
								Message: "couldn't get version/kind; json parse error: invalid character ']' looking for beginning of object key string",
							},
						},
					}))
				})

				It("should allow the request because seed name matches", func() {
					objData, err := runtime.Encode(encoder, &gardenoperationsv1alpha1.Bastion{
						Spec: gardenoperationsv1alpha1.BastionSpec{
							SeedName: &seedName,
						},
					})
					Expect(err).NotTo(HaveOccurred())
					request.Object.Raw = objData

					Expect(handler.Handle(ctx, request)).To(Equal(responseAllowed))
				})

				It("should allow the request because seed name is ambiguous", func() {
					request.UserInfo = ambiguousUser

					objData, err := runtime.Encode(encoder, &gardenoperationsv1alpha1.Bastion{
						Spec: gardenoperationsv1alpha1.BastionSpec{
							SeedName: pointer.StringPtr("some-different-seed"),
						},
					})
					Expect(err).NotTo(HaveOccurred())
					request.Object.Raw = objData

					Expect(handler.Handle(ctx, request)).To(Equal(responseAllowed))
				})
			})
		})

		Context("when requested for Leases", func() {
			var name, namespace string

			BeforeEach(func() {
				name, namespace = "foo", "bar"

				request.Name = name
				request.Namespace = namespace
				request.UserInfo = seedUser
				request.Resource = metav1.GroupVersionResource{
					Group:    coordinationv1.SchemeGroupVersion.Group,
					Resource: "leases",
				}
			})

			DescribeTable("should not allow the request because no allowed verb",
				func(operation admissionv1.Operation) {
					request.Operation = operation

					Expect(handler.Handle(ctx, request)).To(Equal(admission.Response{
						AdmissionResponse: admissionv1.AdmissionResponse{
							Allowed: false,
							Result: &metav1.Status{
								Code:    int32(http.StatusBadRequest),
								Message: fmt.Sprintf("unexpected operation: %q", operation),
							},
						},
					}))
				},

				Entry("update", admissionv1.Update),
				Entry("delete", admissionv1.Delete),
				Entry("connect", admissionv1.Connect),
			)

			Context("when operation is create", func() {
				BeforeEach(func() {
					request.Operation = admissionv1.Create
				})

				DescribeTable("should forbid the request because the seed name of the lease does not match",
					func(seedNameInLease string) {
						request.Name = seedNameInLease

						Expect(handler.Handle(ctx, request)).To(Equal(admission.Response{
							AdmissionResponse: admissionv1.AdmissionResponse{
								Allowed: false,
								Result: &metav1.Status{
									Code:    int32(http.StatusForbidden),
									Message: fmt.Sprintf("object does not belong to seed %q", seedName),
								},
							},
						}))
					},

					Entry("seed name is different", "some-different-seed"),
				)

				It("should allow the request because lease is used for leader-election", func() {
					request.Name = "gardenlet-leader-election"

					Expect(handler.Handle(ctx, request)).To(Equal(responseAllowed))
				})

				It("should allow the request because seed name matches", func() {
					request.Name = seedName

					Expect(handler.Handle(ctx, request)).To(Equal(responseAllowed))
				})

				It("should allow the request because seed name is ambiguous", func() {
					request.Name = "some-different-seed"
					request.UserInfo = ambiguousUser

					Expect(handler.Handle(ctx, request)).To(Equal(responseAllowed))
				})
			})
		})

		Context("when requested for Seeds", func() {
			var name string

			BeforeEach(func() {
				name = "foo"

				request.Name = name
				request.UserInfo = seedUser
				request.Resource = metav1.GroupVersionResource{
					Group:    gardencorev1beta1.SchemeGroupVersion.Group,
					Resource: "seeds",
				}
			})

			DescribeTable("should not allow the request because no allowed verb",
				func(operation admissionv1.Operation) {
					request.Operation = operation

					Expect(handler.Handle(ctx, request)).To(Equal(admission.Response{
						AdmissionResponse: admissionv1.AdmissionResponse{
							Allowed: false,
							Result: &metav1.Status{
								Code:    int32(http.StatusBadRequest),
								Message: fmt.Sprintf("unexpected operation: %q", operation),
							},
						},
					}))
				},

				Entry("connect", admissionv1.Connect),
			)

			generateTestsForOperation := func(operation admissionv1.Operation) func() {
				return func() {
					BeforeEach(func() {
						request.Operation = operation
					})

					It("should allow the request because seed name matches", func() {
						request.Name = seedName

						Expect(handler.Handle(ctx, request)).To(Equal(responseAllowed))
					})

					It("should allow the request because seed name is ambiguous", func() {
						request.UserInfo = ambiguousUser
						request.Name = "some-different-seed"

						Expect(handler.Handle(ctx, request)).To(Equal(responseAllowed))
					})

					Context("requiring information from managedseed", func() {
						var (
							differentSeedName    = "some-different-seed"
							managedSeedNamespace = "garden"
							shootName            = "shoot"
						)

						BeforeEach(func() {
							request.Name = differentSeedName
						})

						It("should forbid the request because seed does not belong to a managedseed", func() {
							mockCache.EXPECT().Get(ctx, kutil.Key(managedSeedNamespace, differentSeedName), gomock.AssignableToTypeOf(&seedmanagementv1alpha1.ManagedSeed{})).Return(apierrors.NewNotFound(schema.GroupResource{}, ""))

							Expect(handler.Handle(ctx, request)).To(Equal(admission.Response{
								AdmissionResponse: admissionv1.AdmissionResponse{
									Allowed: false,
									Result: &metav1.Status{
										Code:    int32(http.StatusForbidden),
										Message: fmt.Sprintf("object does not belong to seed %q", seedName),
									},
								},
							}))
						})

						It("should forbid the request because an error occurred while fetching the managedseed", func() {
							mockCache.EXPECT().Get(ctx, kutil.Key(managedSeedNamespace, differentSeedName), gomock.AssignableToTypeOf(&seedmanagementv1alpha1.ManagedSeed{})).Return(fakeErr)

							Expect(handler.Handle(ctx, request)).To(Equal(admission.Response{
								AdmissionResponse: admissionv1.AdmissionResponse{
									Allowed: false,
									Result: &metav1.Status{
										Code:    int32(http.StatusInternalServerError),
										Message: fakeErr.Error(),
									},
								},
							}))
						})

						if operation == admissionv1.Create || operation == admissionv1.Update {
							It("should forbid the request because managedseed's `.spec.seedTemplate` is nil", func() {
								mockCache.EXPECT().Get(ctx, kutil.Key(managedSeedNamespace, differentSeedName), gomock.AssignableToTypeOf(&seedmanagementv1alpha1.ManagedSeed{})).DoAndReturn(func(_ context.Context, _ client.ObjectKey, obj *seedmanagementv1alpha1.ManagedSeed) error {
									(&seedmanagementv1alpha1.ManagedSeed{}).DeepCopyInto(obj)
									return nil
								})

								Expect(handler.Handle(ctx, request)).To(Equal(admission.Response{
									AdmissionResponse: admissionv1.AdmissionResponse{
										Allowed: false,
										Result: &metav1.Status{
											Code:    int32(http.StatusForbidden),
											Message: fmt.Sprintf("object does not belong to seed %q", seedName),
										},
									},
								}))
							})
						}

						if operation == admissionv1.Delete {
							It("should forbid the request because managedseed's `.metadata.deletionTimestamp` is nil", func() {
								mockCache.EXPECT().Get(ctx, kutil.Key(managedSeedNamespace, differentSeedName), gomock.AssignableToTypeOf(&seedmanagementv1alpha1.ManagedSeed{})).DoAndReturn(func(_ context.Context, _ client.ObjectKey, obj *seedmanagementv1alpha1.ManagedSeed) error {
									(&seedmanagementv1alpha1.ManagedSeed{}).DeepCopyInto(obj)
									return nil
								})

								Expect(handler.Handle(ctx, request)).To(Equal(admission.Response{
									AdmissionResponse: admissionv1.AdmissionResponse{
										Allowed: false,
										Result: &metav1.Status{
											Code:    int32(http.StatusForbidden),
											Message: "object can only be deleted if corresponding ManagedSeed has a deletion timestamp",
										},
									},
								}))
							})
						}

						Context("requiring information from shoot", func() {
							var deletionTimestamp *metav1.Time

							if operation == admissionv1.Delete {
								deletionTimestamp = &metav1.Time{}
							}

							It("should forbid the request because managedseed's `.spec.shoot` is nil", func() {
								mockCache.EXPECT().Get(ctx, kutil.Key(managedSeedNamespace, differentSeedName), gomock.AssignableToTypeOf(&seedmanagementv1alpha1.ManagedSeed{})).DoAndReturn(func(_ context.Context, _ client.ObjectKey, obj *seedmanagementv1alpha1.ManagedSeed) error {
									(&seedmanagementv1alpha1.ManagedSeed{
										ObjectMeta: metav1.ObjectMeta{DeletionTimestamp: deletionTimestamp},
										Spec: seedmanagementv1alpha1.ManagedSeedSpec{
											SeedTemplate: &gardencorev1beta1.SeedTemplate{},
										},
									}).DeepCopyInto(obj)
									return nil
								})

								Expect(handler.Handle(ctx, request)).To(Equal(admission.Response{
									AdmissionResponse: admissionv1.AdmissionResponse{
										Allowed: false,
										Result: &metav1.Status{
											Code:    int32(http.StatusForbidden),
											Message: fmt.Sprintf("object does not belong to seed %q", seedName),
										},
									},
								}))
							})

							It("should forbid the request because reading the shoot referenced by the managedseed failed", func() {
								mockCache.EXPECT().Get(ctx, kutil.Key(managedSeedNamespace, differentSeedName), gomock.AssignableToTypeOf(&seedmanagementv1alpha1.ManagedSeed{})).DoAndReturn(func(_ context.Context, _ client.ObjectKey, obj *seedmanagementv1alpha1.ManagedSeed) error {
									(&seedmanagementv1alpha1.ManagedSeed{
										ObjectMeta: metav1.ObjectMeta{
											Namespace:         managedSeedNamespace,
											DeletionTimestamp: deletionTimestamp,
										},
										Spec: seedmanagementv1alpha1.ManagedSeedSpec{
											SeedTemplate: &gardencorev1beta1.SeedTemplate{},
											Shoot:        &seedmanagementv1alpha1.Shoot{Name: shootName},
										},
									}).DeepCopyInto(obj)
									return nil
								})
								mockCache.EXPECT().Get(ctx, kutil.Key(managedSeedNamespace, shootName), gomock.AssignableToTypeOf(&gardencorev1beta1.Shoot{})).Return(fakeErr)

								Expect(handler.Handle(ctx, request)).To(Equal(admission.Response{
									AdmissionResponse: admissionv1.AdmissionResponse{
										Allowed: false,
										Result: &metav1.Status{
											Code:    int32(http.StatusInternalServerError),
											Message: fakeErr.Error(),
										},
									},
								}))
							})

							DescribeTable("should forbid the request because the seed name of the shoot referenced by the managedseed does not match",
								func(seedNameInShoot *string) {
									mockCache.EXPECT().Get(ctx, kutil.Key(managedSeedNamespace, differentSeedName), gomock.AssignableToTypeOf(&seedmanagementv1alpha1.ManagedSeed{})).DoAndReturn(func(_ context.Context, _ client.ObjectKey, obj *seedmanagementv1alpha1.ManagedSeed) error {
										(&seedmanagementv1alpha1.ManagedSeed{
											ObjectMeta: metav1.ObjectMeta{
												Namespace:         managedSeedNamespace,
												DeletionTimestamp: deletionTimestamp,
											},
											Spec: seedmanagementv1alpha1.ManagedSeedSpec{
												SeedTemplate: &gardencorev1beta1.SeedTemplate{},
												Shoot:        &seedmanagementv1alpha1.Shoot{Name: shootName},
											},
										}).DeepCopyInto(obj)
										return nil
									})
									mockCache.EXPECT().Get(ctx, kutil.Key(managedSeedNamespace, shootName), gomock.AssignableToTypeOf(&gardencorev1beta1.Shoot{})).DoAndReturn(func(_ context.Context, _ client.ObjectKey, obj *gardencorev1beta1.Shoot) error {
										(&gardencorev1beta1.Shoot{Spec: gardencorev1beta1.ShootSpec{SeedName: seedNameInShoot}}).DeepCopyInto(obj)
										return nil
									})

									Expect(handler.Handle(ctx, request)).To(Equal(admission.Response{
										AdmissionResponse: admissionv1.AdmissionResponse{
											Allowed: false,
											Result: &metav1.Status{
												Code:    int32(http.StatusForbidden),
												Message: fmt.Sprintf("object does not belong to seed %q", seedName),
											},
										},
									}))
								},

								Entry("seed name is nil", nil),
								Entry("seed name is different", pointer.StringPtr("some-different-seed")),
							)

							It("should allow the request because the seed name of the shoot referenced by the managedseed matches", func() {
								mockCache.EXPECT().Get(ctx, kutil.Key(managedSeedNamespace, differentSeedName), gomock.AssignableToTypeOf(&seedmanagementv1alpha1.ManagedSeed{})).DoAndReturn(func(_ context.Context, _ client.ObjectKey, obj *seedmanagementv1alpha1.ManagedSeed) error {
									(&seedmanagementv1alpha1.ManagedSeed{
										ObjectMeta: metav1.ObjectMeta{
											Namespace:         managedSeedNamespace,
											DeletionTimestamp: deletionTimestamp,
										},
										Spec: seedmanagementv1alpha1.ManagedSeedSpec{
											SeedTemplate: &gardencorev1beta1.SeedTemplate{},
											Shoot:        &seedmanagementv1alpha1.Shoot{Name: shootName},
										},
									}).DeepCopyInto(obj)
									return nil
								})
								mockCache.EXPECT().Get(ctx, kutil.Key(managedSeedNamespace, shootName), gomock.AssignableToTypeOf(&gardencorev1beta1.Shoot{})).DoAndReturn(func(_ context.Context, _ client.ObjectKey, obj *gardencorev1beta1.Shoot) error {
									(&gardencorev1beta1.Shoot{Spec: gardencorev1beta1.ShootSpec{SeedName: &seedName}}).DeepCopyInto(obj)
									return nil
								})

								Expect(handler.Handle(ctx, request)).To(Equal(responseAllowed))
							})
						})
					})
				}
			}

			Context("when operation is create", generateTestsForOperation(admissionv1.Create))
			Context("when operation is update", generateTestsForOperation(admissionv1.Update))
			Context("when operation is delete", generateTestsForOperation(admissionv1.Delete))
		})

		Context("when requested for CertificateSigningRequests", func() {
			var name string

			BeforeEach(func() {
				name = "foo"

				request.Name = name
				request.UserInfo = seedUser
				request.Resource = metav1.GroupVersionResource{
					Group:    certificatesv1beta1.SchemeGroupVersion.Group,
					Resource: "certificatesigningrequests",
				}
			})

			DescribeTable("should not allow the request because no allowed verb",
				func(operation admissionv1.Operation) {
					request.Operation = operation

					Expect(handler.Handle(ctx, request)).To(Equal(admission.Response{
						AdmissionResponse: admissionv1.AdmissionResponse{
							Allowed: false,
							Result: &metav1.Status{
								Code:    int32(http.StatusBadRequest),
								Message: fmt.Sprintf("unexpected operation: %q", operation),
							},
						},
					}))
				},

				Entry("update", admissionv1.Update),
				Entry("delete", admissionv1.Delete),
				Entry("connect", admissionv1.Connect),
			)

			Context("when operation is create", func() {
				BeforeEach(func() {
					request.Operation = admissionv1.Create
				})

				It("should return an error because decoding the object failed", func() {
					request.Object.Raw = []byte(`{]`)

					Expect(handler.Handle(ctx, request)).To(Equal(admission.Response{
						AdmissionResponse: admissionv1.AdmissionResponse{
							Allowed: false,
							Result: &metav1.Status{
								Code:    int32(http.StatusBadRequest),
								Message: "couldn't get version/kind; json parse error: invalid character ']' looking for beginning of object key string",
							},
						},
					}))
				})

				It("should forbid the request because the CSR is not a valid seed-related CSR", func() {
					objData, err := runtime.Encode(encoder, &certificatesv1beta1.CertificateSigningRequest{
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
						},
					})
					Expect(err).NotTo(HaveOccurred())
					request.Object.Raw = objData

					Expect(handler.Handle(ctx, request)).To(Equal(admission.Response{
						AdmissionResponse: admissionv1.AdmissionResponse{
							Allowed: false,
							Result: &metav1.Status{
								Code:    int32(http.StatusForbidden),
								Message: "can only create CSRs for seed clusters",
							},
						},
					}))
				})

				It("should forbid the request because the seed name of the csr does not match", func() {
					objData, err := runtime.Encode(encoder, &certificatesv1beta1.CertificateSigningRequest{
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
					})
					Expect(err).NotTo(HaveOccurred())
					request.Object.Raw = objData

					Expect(handler.Handle(ctx, request)).To(Equal(admission.Response{
						AdmissionResponse: admissionv1.AdmissionResponse{
							Allowed: false,
							Result: &metav1.Status{
								Code:    int32(http.StatusForbidden),
								Message: fmt.Sprintf("object does not belong to seed %q", seedName),
							},
						},
					}))
				})

				It("should allow the request because seed name matches", func() {
					objData, err := runtime.Encode(encoder, &certificatesv1beta1.CertificateSigningRequest{
						Spec: certificatesv1beta1.CertificateSigningRequestSpec{
							Request: []byte(`-----BEGIN CERTIFICATE REQUEST-----
MIIClTCCAX0CAQAwUDEkMCIGA1UEChMbZ2FyZGVuZXIuY2xvdWQ6c3lzdGVtOnNl
ZWRzMSgwJgYDVQQDEx9nYXJkZW5lci5jbG91ZDpzeXN0ZW06c2VlZDpzZWVkMIIB
IjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAsDqibMtE5PXULTT12u0TYW1U
EI2f2MFImNPdEdmyTO8kjy61JzBQxUz6NLLmZWks7dnhZOrhfXqJjVzLWi7gAAIH
hkoxnu8spKTV53l6eY5RrivVsNFRuPF763bKd6JvsF1p9QD9y8uk6bY4NbLAjgMJ
MH64Sj398AnvLlIL+8XIFKtT/SjvOp99oGkKxWHBvokcz9MLUJc/2/JcOdsZ62ue
ZAsqimh0F085+BoG2YtLa4kLNAAiNsijgJ5QCXc7/F8uqkj4uy436LGgGmDfcQxC
9W2snEqriv1dsjF5R/kjh+UbTd+ZdHoAaNaiE7lfZcwe/ap6SNeZaszcDoR//wID
AQABoAAwDQYJKoZIhvcNAQELBQADggEBAKGWWWDHGHdUkOvE1L+tR/v3sDvLfmO7
jWtF/Sq7kRCrr6xEHLKmVA4wRovpzOML0ntrDCu3npKAWqN+U56L1ZeZSsxyOhvN
dXjk2wPg0+IXPscd33hq0wGZRtBc5MHNWwYLv3ERKnHNbPE2ifkYy6FQ/h/2Kx55
tHu5PlIwWS6CP+03s3/gjbHX7VL+V3RF5BIHDWcp9QfjN0zEx0R2WVXKIbhC8RTR
BkEao/FEz4eQuV5atSD0S78+aF4BriEtWKKjXECTCxMuqcA24vGOgHIrEbKd7zSC
2L4LgmHdCmMFOtPkykwLK6wV1YW7Ce8AxU3j+q4kgZQ+51HJDQDdB74=
-----END CERTIFICATE REQUEST-----`),
							Usages: []certificatesv1beta1.KeyUsage{
								certificatesv1beta1.UsageKeyEncipherment,
								certificatesv1beta1.UsageDigitalSignature,
								certificatesv1beta1.UsageClientAuth,
							},
						},
					})
					Expect(err).NotTo(HaveOccurred())
					request.Object.Raw = objData

					Expect(handler.Handle(ctx, request)).To(Equal(responseAllowed))
				})

				It("should allow the request because seed name is ambiguous", func() {
					objData, err := runtime.Encode(encoder, &certificatesv1beta1.CertificateSigningRequest{
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
					})
					Expect(err).NotTo(HaveOccurred())
					request.Object.Raw = objData

					request.UserInfo = ambiguousUser

					Expect(handler.Handle(ctx, request)).To(Equal(responseAllowed))
				})
			})
		})
	})
})
