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

package bastion

import (
	"context"
	"time"

	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	operationsv1alpha1 "github.com/gardener/gardener/pkg/apis/operations/v1alpha1"
	kutil "github.com/gardener/gardener/pkg/utils/kubernetes"
	"github.com/go-logr/logr"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var _ = Describe("Controller", func() {
	var (
		namespace   = "garden-dev"
		bastionName = "bastion"
		shootName   = "myshoot"
		seedName    = "myseed"
		ctx         = context.TODO()
		maxLifetime = 12 * time.Hour

		newReconciler = func(initialObjects ...runtime.Object) *reconciler {
			scheme := runtime.NewScheme()
			utilruntime.Must(gardencorev1beta1.AddToScheme(scheme))
			utilruntime.Must(operationsv1alpha1.AddToScheme(scheme))

			c := fake.NewClientBuilder().WithRuntimeObjects(initialObjects...).WithScheme(scheme).Build()
			rec := &reconciler{
				logger:       logr.Discard(),
				gardenClient: c,
				maxLifetime:  maxLifetime,
			}

			return rec
		}

		expectBastionGone = func(c client.Client) {
			key := types.NamespacedName{Name: bastionName, Namespace: namespace}
			Expect(c.Get(ctx, key, &operationsv1alpha1.Bastion{})).NotTo(Succeed())
		}
	)

	Describe("Reconciler", func() {
		It("should return nil because object not found", func() {
			rec := newReconciler()

			result, err := rec.Reconcile(ctx, reconcile.Request{NamespacedName: kutil.Key(namespace, bastionName)})
			Expect(result).To(Equal(reconcile.Result{}))
			Expect(err).To(Succeed())
		})

		It("should requeue alive Bastions", func() {
			created := time.Now().Add(-maxLifetime / 2)
			requeueAfter := time.Until(created.Add(maxLifetime))

			rec := newReconciler(
				newShoot(namespace, shootName, &seedName),
				newBastion(namespace, bastionName, shootName, &seedName, &created, nil),
			)

			result, err := rec.Reconcile(ctx, reconcile.Request{NamespacedName: kutil.Key(namespace, bastionName)})
			Expect(result.RequeueAfter).To(BeNumerically("~", requeueAfter, 1*time.Second))
			Expect(err).To(Succeed())
		})

		It("should requeue soon-to-expire Bastions", func() {
			now := time.Now()
			remaining := 30 * time.Second
			expires := now.Add(remaining)

			rec := newReconciler(
				newShoot(namespace, shootName, &seedName),
				newBastion(namespace, bastionName, shootName, &seedName, &now, &expires),
			)

			result, err := rec.Reconcile(ctx, reconcile.Request{NamespacedName: kutil.Key(namespace, bastionName)})
			Expect(result.RequeueAfter).To(BeNumerically("~", remaining, 1*time.Second))
			Expect(err).To(Succeed())
		})

		It("should requeue soon-to-reach-max-lifetime Bastions", func() {
			now := time.Now()
			created := now.Add(-maxLifetime).Add(10 * time.Minute) // reaches end-of-life in 10 minutes
			expires := now.Add(30 * time.Minute)                   // expires in 30 minutes

			rec := newReconciler(
				newShoot(namespace, shootName, &seedName),
				newBastion(namespace, bastionName, shootName, &seedName, &created, &expires),
			)

			result, err := rec.Reconcile(ctx, reconcile.Request{NamespacedName: kutil.Key(namespace, bastionName)})
			Expect(result.RequeueAfter).To(BeNumerically("~", 10*time.Minute, 1*time.Second))
			Expect(err).To(Succeed())
		})

		It("should delete Bastions with missing shoots", func() {
			created := time.Now().Add(-maxLifetime / 2)

			rec := newReconciler(
				newBastion(namespace, bastionName, shootName, &seedName, &created, nil),
			)

			result, err := rec.Reconcile(ctx, reconcile.Request{NamespacedName: kutil.Key(namespace, bastionName)})
			Expect(result).To(Equal(reconcile.Result{}))
			Expect(err).To(Succeed())
			expectBastionGone(rec.gardenClient)
		})

		It("should delete Bastions with shoots in deletion", func() {
			now := metav1.Now()
			created := time.Now().Add(-maxLifetime / 2)

			shoot := newShoot(namespace, shootName, &seedName)
			shoot.DeletionTimestamp = &now

			rec := newReconciler(
				shoot,
				newBastion(namespace, bastionName, shootName, &seedName, &created, nil),
			)

			result, err := rec.Reconcile(ctx, reconcile.Request{NamespacedName: kutil.Key(namespace, bastionName)})
			Expect(result).To(Equal(reconcile.Result{}))
			Expect(err).To(Succeed())
			expectBastionGone(rec.gardenClient)
		})

		It("should delete expired Bastions", func() {
			now := time.Now()
			created := now.Add(-maxLifetime / 2)
			expires := now.Add(-5 * time.Second)

			rec := newReconciler(
				newShoot(namespace, shootName, &seedName),
				newBastion(namespace, bastionName, shootName, &seedName, &created, &expires),
			)

			result, err := rec.Reconcile(ctx, reconcile.Request{NamespacedName: kutil.Key(namespace, bastionName)})
			Expect(result).To(Equal(reconcile.Result{}))
			Expect(err).To(Succeed())
			expectBastionGone(rec.gardenClient)
		})

		It("should delete Bastions that have reached their TTL", func() {
			created := time.Now().Add(-maxLifetime * 2)

			rec := newReconciler(
				newShoot(namespace, shootName, &seedName),
				newBastion(namespace, bastionName, shootName, &seedName, &created, nil),
			)

			result, err := rec.Reconcile(ctx, reconcile.Request{NamespacedName: kutil.Key(namespace, bastionName)})
			Expect(result).To(Equal(reconcile.Result{}))
			Expect(err).To(Succeed())
			expectBastionGone(rec.gardenClient)
		})

		It("should delete Bastions with outdated seed information", func() {
			newSeedName := "new-seed-after-migration"
			created := time.Now().Add(-maxLifetime / 2)
			expires := time.Now().Add(1 * time.Minute)

			rec := newReconciler(
				newShoot(namespace, shootName, &newSeedName),
				newBastion(namespace, bastionName, shootName, &seedName, &created, &expires),
			)

			result, err := rec.Reconcile(ctx, reconcile.Request{NamespacedName: kutil.Key(namespace, bastionName)})
			Expect(result).To(Equal(reconcile.Result{}))
			Expect(err).To(Succeed())
			expectBastionGone(rec.gardenClient)
		})

		It("should delete Bastions with outdated seed information 2", func() {
			created := time.Now().Add(-maxLifetime / 2)
			expires := time.Now().Add(1 * time.Minute)

			rec := newReconciler(
				newShoot(namespace, shootName, nil), // shoot was removed from original seed and since then hasn't found a new seed
				newBastion(namespace, bastionName, shootName, &seedName, &created, &expires),
			)

			result, err := rec.Reconcile(ctx, reconcile.Request{NamespacedName: kutil.Key(namespace, bastionName)})
			Expect(result).To(Equal(reconcile.Result{}))
			Expect(err).To(Succeed())
			expectBastionGone(rec.gardenClient)
		})
	})
})

func newBastion(namespace string, name string, shootName string, seedName *string, createdAt *time.Time, expiresAt *time.Time) *operationsv1alpha1.Bastion {
	bastion := &operationsv1alpha1.Bastion{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: operationsv1alpha1.BastionSpec{
			ShootRef: corev1.LocalObjectReference{
				Name: shootName,
			},
			SeedName: seedName,
		},
	}

	if createdAt != nil {
		bastion.ObjectMeta.CreationTimestamp = metav1.NewTime(*createdAt)
	}

	if expiresAt != nil {
		expires := metav1.NewTime(*expiresAt)
		bastion.Status.ExpirationTimestamp = &expires
	}

	return bastion
}

func newShoot(namespace string, name string, seedName *string) *gardencorev1beta1.Shoot {
	shoot := &gardencorev1beta1.Shoot{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: gardencorev1beta1.ShootSpec{
			SeedName: seedName,
		},
		Status: gardencorev1beta1.ShootStatus{
			SeedName: seedName,
		},
	}

	return shoot
}
