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

package managedseedset

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	seedmanagementv1alpha1 "github.com/gardener/gardener/pkg/apis/seedmanagement/v1alpha1"
	operationshoot "github.com/gardener/gardener/pkg/operation/shoot"
)

func filterShoot(obj, controller client.Object, deleted bool) bool {
	shoot, ok := obj.(*gardencorev1beta1.Shoot)
	if !ok {
		return false
	}
	set, ok := controller.(*seedmanagementv1alpha1.ManagedSeedSet)
	if !ok {
		return false
	}

	if set.Status.PendingReplica == nil || set.Status.PendingReplica.Name != shoot.Name {
		return false
	}
	switch set.Status.PendingReplica.Reason {
	case seedmanagementv1alpha1.ShootReconcilingReason:
		return shootReconcileFailed(shoot) || shootReconcileSucceeded(shoot) || shoot.DeletionTimestamp != nil
	case seedmanagementv1alpha1.ShootDeletingReason:
		return deleted || shootDeleteFailed(shoot)
	case seedmanagementv1alpha1.ShootReconcileFailedReason:
		return !shootReconcileFailed(shoot)
	case seedmanagementv1alpha1.ShootDeleteFailedReason:
		return !shootDeleteFailed(shoot)
	case seedmanagementv1alpha1.ShootNotHealthyReason:
		return shootHealthStatus(shoot) == operationshoot.StatusHealthy
	default:
		return false
	}
}

func filterManagedSeed(obj, controller client.Object, deleted bool) bool {
	managedSeed, ok := obj.(*seedmanagementv1alpha1.ManagedSeed)
	if !ok {
		return false
	}
	set, ok := controller.(*seedmanagementv1alpha1.ManagedSeedSet)
	if !ok {
		return false
	}

	if set.Status.PendingReplica == nil || set.Status.PendingReplica.Name != managedSeed.Name {
		return false
	}
	switch set.Status.PendingReplica.Reason {
	case seedmanagementv1alpha1.ManagedSeedPreparingReason:
		return managedSeedRegistered(managedSeed) || managedSeed.DeletionTimestamp != nil
	case seedmanagementv1alpha1.ManagedSeedDeletingReason:
		return deleted
	default:
		return false
	}
}

func filterSeed(obj, controller client.Object, _ bool) bool {
	seed, ok := obj.(*gardencorev1beta1.Seed)
	if !ok {
		return false
	}
	set, ok := controller.(*seedmanagementv1alpha1.ManagedSeedSet)
	if !ok {
		return false
	}

	if set.Status.PendingReplica == nil || set.Status.PendingReplica.Name != seed.Name {
		return false
	}
	switch set.Status.PendingReplica.Reason {
	case seedmanagementv1alpha1.SeedNotReadyReason:
		return seedReady(seed)
	default:
		return false
	}
}
