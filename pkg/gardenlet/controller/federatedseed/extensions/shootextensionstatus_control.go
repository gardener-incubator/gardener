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

package extensions

import (
	"context"
	"fmt"

	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	"github.com/sirupsen/logrus"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	gardencorev1alpha1 "github.com/gardener/gardener/pkg/apis/core/v1alpha1"
	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/gardener/gardener/pkg/extensions"
	kutil "github.com/gardener/gardener/pkg/utils/kubernetes"
)

// ShootExtensionStatusControl is used to sync the `providerStatus` field from extension
// resources in the Seed to the ShootExtensionStatus resource in the Garden cluster.
type ShootExtensionStatusControl struct {
	gardenClient client.Client
	seedClient   client.Client
	log          *logrus.Entry
	recorder     record.EventRecorder
	decoder      runtime.Decoder
}

// NewShootExtensionStatusControl creates a new instance of ShootExtensionStatusControl.
func NewShootExtensionStatusControl(k8sGardenClient, seedClient client.Client, log *logrus.Entry, recorder record.EventRecorder) *ShootExtensionStatusControl {
	return &ShootExtensionStatusControl{
		gardenClient: k8sGardenClient,
		seedClient:   seedClient,
		log:          log,
		recorder:     recorder,
		decoder:      extensions.NewGardenDecoder(),
	}
}

// CreateShootExtensionStatusSyncReconcileFunc creates a function which can be used by the reconciliation loop to sync
// the extension status to the ShootExtensionStatus resource in the Garden cluster
func (s *ShootExtensionStatusControl) CreateShootExtensionStatusSyncReconcileFunc(kind string, objectCreator func() client.Object) reconcile.Func {
	return func(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
		extensionObj, err := GetExtensionObject(ctx, s.seedClient, req.NamespacedName, objectCreator)
		if apierrors.IsNotFound(err) {
			// the extension has been deleted from the Seed cluster
			// we an be sure that there are no more cloud provider resources left.
			// Delete the provider status from the ShootExtensionStatus resource in the Garden cluster
			s.log.Debugf("Extension resource (%s/%s) of kind %q has been deleted. Synchonrizing with ShootExtensionStatus in the Garden cluster", req.NamespacedName.Namespace, req.NamespacedName.Name, kind)

			shoot, err := s.getShootForRequest(ctx, req)
			if err != nil {
				return reconcile.Result{}, fmt.Errorf("failed to delete the provider status of the %q extension (%s/%s) from the ShootExtensionStatus: %w",
					kind,
					req.NamespacedName.Namespace,
					req.NamespacedName.Name,
					err)
			}

			shootExtensionStatus := &gardencorev1alpha1.ShootExtensionStatus{}
			if err := s.gardenClient.Get(ctx, kutil.Key(shoot.Namespace, shoot.Name), shootExtensionStatus); err != nil {
				return reconcile.Result{}, fmt.Errorf("failed to delete the provider status of the %q extension (%s/%s) from the ShootExtensionStatus for Shoot %q in namespace %q: %w",
					kind,
					req.NamespacedName.Namespace,
					req.NamespacedName.Name,
					shoot.Name,
					shoot.Namespace,
					err)
			}
			shootExtensionStatusCopy := shootExtensionStatus.DeepCopy()
			shootExtensionStatus.Statuses = removeExtensionStatus(shootExtensionStatus.Statuses, kind)
			return s.patchShootExtensionStatus(ctx, shootExtensionStatus, shootExtensionStatusCopy, kind)
		}
		if err != nil {
			return reconcile.Result{}, err
		}

		if shouldSkipExtensionStatusSync(extensionObj) {
			return reconcile.Result{}, nil
		}

		extensionType := extensionObj.GetExtensionSpec().GetExtensionType()
		purpose := extensionObj.GetExtensionSpec().GetExtensionPurpose()
		currentProviderStatus := extensionObj.GetExtensionStatus().GetProviderStatus()

		shoot, err := s.getShootForRequest(ctx, req)
		if err != nil {
			return reconcile.Result{}, err
		}

		// ShootExtensionStatus resource must already exist in the project namespace
		shootExtensionStatus := &gardencorev1alpha1.ShootExtensionStatus{}
		if err := s.gardenClient.Get(ctx, kutil.Key(shoot.Namespace, shoot.Name), shootExtensionStatus); err != nil {
			return reconcile.Result{}, fmt.Errorf("shoot extension status sync failed: %w", err)
		}

		shootExtensionStatusCopy := shootExtensionStatus.DeepCopy()
		currentExtensionStatus, indexOldStatus := getExtensionStatusForKind(shootExtensionStatus.Statuses, kind)

		if currentProviderStatus == nil {
			if currentExtensionStatus == nil {
				s.log.Debugf("Skipping ShootExtensionStatus for the %q extension of Shoot %q. The resource is up-to-date.", kind, shoot.Name)
				return reconcile.Result{}, nil
			} else {
				// Usually, the provider status of an extension resource should not be removed without the extension having a deletion timestamp
				// so we should not get to this point.
				// But if we do (e.g manual status update), we also delete the status from the ShootExtensionStatus resource
				s.log.Debugf(fmt.Sprintf("Deleting the provider status for the %q extension from the ShootExtensionStatus for Shoot %q", kind, shoot.Name))
				shootExtensionStatus.Statuses = removeExtensionStatus(shootExtensionStatus.Statuses, kind)
				return s.patchShootExtensionStatus(ctx, shootExtensionStatus, shootExtensionStatusCopy, kind)
			}
		}

		newEntry := gardencorev1alpha1.ExtensionStatus{
			Kind:    kind,
			Type:    extensionType,
			Purpose: purpose,
			Status:  *currentProviderStatus,
		}

		// new provider status that has not yet been synced
		// add the new ExtensionStatus entry
		if currentExtensionStatus == nil {
			shootExtensionStatus.Statuses = append(shootExtensionStatus.Statuses, newEntry)
			return s.patchShootExtensionStatus(ctx, shootExtensionStatus, shootExtensionStatusCopy, kind)
		}

		// at this point we know that we have a provider status that also exists in the ShootExtensionStatus in the Garden cluster
		// we need to check if the status is up to date by checking for equality
		if apiequality.Semantic.DeepEqual(*currentExtensionStatus, newEntry) {
			s.log.Debugf("Skipping sync of the ShootExtensionStatus for the %q extension of Shoot %q. The resource is up-to-date", kind, shoot.Name)
			return reconcile.Result{}, nil
		}

		// patch the existing ExtensionStatus entry and patch the ShootExtensionStatus resource
		shootExtensionStatus.Statuses[indexOldStatus] = newEntry
		return s.patchShootExtensionStatus(ctx, shootExtensionStatus, shootExtensionStatusCopy, kind)
	}
}

// getShootForRequest returns the Shoot resource for a reconcile.Request parsed from the Cluster resource in the Seed
func (s *ShootExtensionStatusControl) getShootForRequest(ctx context.Context, req reconcile.Request) (*gardencorev1beta1.Shoot, error) {
	cluster, err := extensions.ClusterFromRequest(ctx, s.seedClient, req)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, err
		}
		return nil, fmt.Errorf("could not get cluster with name %s : %w", cluster.Name, err)
	}

	shoot, err := extensions.ShootFromCluster(s.decoder, cluster)
	if err != nil {
		return nil, err
	}

	if shoot == nil {
		return nil, fmt.Errorf("cluster resource %s doesn't contain shoot resource", cluster.Name)
	}
	return shoot, nil
}

func (s *ShootExtensionStatusControl) patchShootExtensionStatus(ctx context.Context, shootExtensionStatus *gardencorev1alpha1.ShootExtensionStatus, shootExtensionStatusCopy *gardencorev1alpha1.ShootExtensionStatus, kind string) (reconcile.Result, error) {
	if err := s.gardenClient.Patch(ctx, shootExtensionStatus, client.MergeFromWithOptions(shootExtensionStatusCopy, client.MergeFromWithOptimisticLock{})); err != nil {
		message := fmt.Sprintf("The %q extension status for Shoot %q was NOT successfully synced: %v", shootExtensionStatus.Name, kind, err)
		s.log.Error(message)
		return reconcile.Result{}, err
	}

	message := fmt.Sprintf("The provider status of the %q extension of Shoot %q was successfully synced to the Garden cluster", kind, shootExtensionStatus.Name)
	s.log.Debug(message)
	return reconcile.Result{}, nil
}

// getExtensionStatusForKind given an []ExtensionStatus and kind, returns the matching ExtensionStatus and index in the array
func getExtensionStatusForKind(status []gardencorev1alpha1.ExtensionStatus, kind string) (*gardencorev1alpha1.ExtensionStatus, int) {
	for i, entry := range status {
		if entry.Kind == kind {
			return &entry, i
		}
	}
	return nil, 0
}

// removeExtensionStatus given an []ExtensionStatus and kind, returns the []ExtensionStatus without the ExtensionStatus
// identified by kind
func removeExtensionStatus(status []gardencorev1alpha1.ExtensionStatus, kind string) []gardencorev1alpha1.ExtensionStatus {
	var entries []gardencorev1alpha1.ExtensionStatus
	for _, entry := range status {
		if entry.Kind == kind {
			continue
		}
		entries = append(entries, entry)
	}
	return entries
}

// shouldSkipExtensionStatusSync returns true if the ShootExtensionStatus sync should be skipped.
// Skip when
//  - a control plane migration is ongoing to avoid concurrent updates from both source
//    and destination Seed gardenlets.
//  - the extension resource already has a deletion timestamp.
//    The cloud provider resources might still exist.
func shouldSkipExtensionStatusSync(extensionObject extensionsv1alpha1.Object) bool {
	if extensionObject.GetDeletionTimestamp() != nil {
		return true
	}

	annotations := extensionObject.GetAnnotations()
	if annotations != nil {
		operationAnnotation := annotations[v1beta1constants.GardenerOperation]
		return operationAnnotation == v1beta1constants.GardenerOperationWaitForState ||
			operationAnnotation == v1beta1constants.GardenerOperationRestore ||
			operationAnnotation == v1beta1constants.GardenerOperationMigrate
	}
	return false
}
