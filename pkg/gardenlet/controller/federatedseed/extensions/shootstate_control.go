// Copyright (c) 2018 SAP SE or an SAP affiliate company. All rights reserved. This file is licensed under the Apache Software License, v. 2 except as noted otherwise in the LICENSE file
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

	"github.com/sirupsen/logrus"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	gardencorev1alpha1 "github.com/gardener/gardener/pkg/apis/core/v1alpha1"
	gardencorev1alpha1helper "github.com/gardener/gardener/pkg/apis/core/v1alpha1/helper"
	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	gardencorev1beta1helper "github.com/gardener/gardener/pkg/apis/core/v1beta1/helper"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/gardener/gardener/pkg/client/kubernetes"
	"github.com/gardener/gardener/pkg/extensions"
	"github.com/gardener/gardener/pkg/utils"
	kutil "github.com/gardener/gardener/pkg/utils/kubernetes"
)

// ShootStateControl is used to update data about extensions and any resources required by them in the ShootState.
type ShootStateControl struct {
	k8sGardenClient kubernetes.Interface
	seedClient      kubernetes.Interface
	log             *logrus.Entry
	recorder        record.EventRecorder
	decoder         runtime.Decoder
}

// NewShootStateControl creates a new instance of ShootStateControl.
func NewShootStateControl(k8sGardenClient, seedClient kubernetes.Interface, log *logrus.Entry, recorder record.EventRecorder) *ShootStateControl {
	return &ShootStateControl{
		k8sGardenClient: k8sGardenClient,
		seedClient:      seedClient,
		log:             log,
		recorder:        recorder,
		decoder:         extensions.NewGardenDecoder(),
	}
}

// CreateShootStateSyncReconcileFunc creates a function which can be used by the reconciliation loop to sync the extension state and its resources to the ShootState
func (s *ShootStateControl) CreateShootStateSyncReconcileFunc(kind string, objectCreator func() client.Object) reconcile.Func {
	return func(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
		extensionObj, err := GetExtensionObject(ctx, s.seedClient.Client(), req.NamespacedName, objectCreator)
		if apierrors.IsNotFound(err) {
			s.log.Debugf("Skipping ShootState sync because resource with kind %s is missing in namespace %s", kind, req.NamespacedName)
			return reconcile.Result{}, nil
		}
		if err != nil {
			return reconcile.Result{}, err
		}

		if shouldSkipExtensionObjectSync(extensionObj) {
			return reconcile.Result{}, nil
		}

		name := extensionObj.GetName()
		purpose := extensionObj.GetExtensionSpec().GetExtensionPurpose()
		newState := extensionObj.GetExtensionStatus().GetState()
		newResources := extensionObj.GetExtensionStatus().GetResources()

		cluster, err := extensions.ClusterFromRequest(ctx, s.seedClient.Client(), req)
		if err != nil {
			if apierrors.IsNotFound(err) {
				return reconcile.Result{}, nil
			}
			return reconcile.Result{}, fmt.Errorf("could not get cluster with name %s : %v", cluster.Name, err)
		}

		shootState, err := s.getShootStateFromCluster(ctx, cluster)
		if err != nil {
			return reconcile.Result{}, err
		}
		shootStateCopy := shootState.DeepCopy()

		currentState, currentResources := getShootStateExtensionStateAndResources(shootState, kind, &name, purpose)

		// Delete resources which are no longer present in the extension state from the ShootState.Spec.Resources list
		for _, resource := range currentResources {
			if gardencorev1beta1helper.GetResourceByName(newResources, resource.Name) == nil {
				updateShootStateResourceData(shootState, &resource.ResourceRef, nil)
			}
		}

		resourcesToUpdate, err := s.getResourcesToUpdate(ctx, shootState, extensionObj.GetNamespace(), newResources)
		if err != nil {
			return reconcile.Result{}, err
		}
		for _, resourceToUpdate := range resourcesToUpdate {
			updateShootStateResourceData(shootState, &resourceToUpdate.CrossVersionObjectReference, &resourceToUpdate.Data)
		}

		shouldUpdateExtensionData := !apiequality.Semantic.DeepEqual(newState, currentState) || !apiequality.Semantic.DeepEqual(newResources, currentResources)
		if shouldUpdateExtensionData {
			updateShootStateExtensionStateAndResources(shootState, kind, &name, purpose, newState, newResources)
		}

		if !shouldUpdateExtensionData && len(resourcesToUpdate) == 0 {
			message := fmt.Sprintf("Skipping sync of Shoot %s's %s extension state with name %s and purpose %s: state already up to date", shootState.Name, kind, name, purposeToString(purpose))
			s.log.Info(message)
			return reconcile.Result{}, nil
		}

		if err := s.k8sGardenClient.Client().Patch(ctx, shootState, client.MergeFromWithOptions(shootStateCopy, client.MergeFromWithOptimisticLock{})); err != nil {
			message := fmt.Sprintf("Shoot %s's %s extension state with name %s and purpose %s was NOT successfully synced: %v", shootState.Name, kind, name, purposeToString(purpose), err)
			s.log.Error(message)
			return reconcile.Result{}, err
		}

		message := fmt.Sprintf("Shoot %s's %s extension state with name %s and purpose %s was successfully synced", shootState.Name, kind, name, purposeToString(purpose))
		s.log.Info(message)
		return reconcile.Result{}, nil
	}
}

func (s *ShootStateControl) getResourcesToUpdate(ctx context.Context, shootState *gardencorev1alpha1.ShootState, namespace string, newResources []gardencorev1beta1.NamedResourceReference) (gardencorev1alpha1helper.ResourceDataList, error) {
	var resourcesToAddUpdate gardencorev1alpha1helper.ResourceDataList

	for _, newResource := range newResources {
		obj, err := utils.GetObjectByRef(ctx, s.seedClient.Client(), &newResource.ResourceRef, namespace)
		if err != nil {
			return nil, err
		}
		if obj == nil {
			return nil, fmt.Errorf("object not found %v", newResource.ResourceRef)
		}

		raw := &runtime.RawExtension{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj, raw); err != nil {
			return nil, err
		}

		resourceList := gardencorev1alpha1helper.ResourceDataList(shootState.Spec.Resources)
		currentResource := resourceList.Get(&newResource.ResourceRef)

		if currentResource == nil || !apiequality.Semantic.DeepEqual(currentResource.Data, newResource) {
			resourcesToAddUpdate = append(resourcesToAddUpdate, gardencorev1alpha1.ResourceData{
				CrossVersionObjectReference: newResource.ResourceRef,
				Data:                        *raw,
			})
		}
	}

	return resourcesToAddUpdate, nil
}

func shouldSkipExtensionObjectSync(extensionObject extensionsv1alpha1.Object) bool {
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

func getShootStateExtensionStateAndResources(shootState *gardencorev1alpha1.ShootState, kind string, name, purpose *string) (*runtime.RawExtension, []gardencorev1beta1.NamedResourceReference) {
	list := gardencorev1alpha1helper.ExtensionResourceStateList(shootState.Spec.Extensions)
	s := list.Get(kind, name, purpose)
	if s != nil {
		return s.State, s.Resources
	}
	return nil, nil
}

func updateShootStateExtensionStateAndResources(shootState *gardencorev1alpha1.ShootState, kind string, name, purpose *string, state *runtime.RawExtension, resources []gardencorev1beta1.NamedResourceReference) {
	list := gardencorev1alpha1helper.ExtensionResourceStateList(shootState.Spec.Extensions)
	if state != nil || len(resources) > 0 {
		list.Upsert(&gardencorev1alpha1.ExtensionResourceState{
			Kind:      kind,
			Name:      name,
			Purpose:   purpose,
			State:     state,
			Resources: resources,
		})
	} else {
		list.Delete(kind, name, purpose)
	}
	shootState.Spec.Extensions = list
}

func updateShootStateResourceData(shootState *gardencorev1alpha1.ShootState, ref *autoscalingv1.CrossVersionObjectReference, data *runtime.RawExtension) {
	list := gardencorev1alpha1helper.ResourceDataList(shootState.Spec.Resources)
	if data != nil {
		list.Upsert(&gardencorev1alpha1.ResourceData{
			CrossVersionObjectReference: *ref,
			Data:                        *data,
		})
	} else {
		list.Delete(ref)
	}
	shootState.Spec.Resources = list
}

func purposeToString(purpose *string) string {
	if purpose == nil {
		return "<nil>"
	}
	return *purpose
}

func (s *ShootStateControl) getShootStateFromCluster(ctx context.Context, cluster *extensionsv1alpha1.Cluster) (*gardencorev1alpha1.ShootState, error) {
	shoot, err := extensions.ShootFromCluster(s.decoder, cluster)
	if err != nil {
		return nil, err
	}

	if shoot == nil {
		return nil, fmt.Errorf("cluster resource %s doesn't contain shoot resource in raw format", cluster.Name)
	}

	shootState := &gardencorev1alpha1.ShootState{}
	if err := s.k8sGardenClient.Client().Get(ctx, kutil.Key(shoot.Namespace, shoot.Name), shootState); err != nil {
		return nil, err
	}

	return shootState, nil
}
