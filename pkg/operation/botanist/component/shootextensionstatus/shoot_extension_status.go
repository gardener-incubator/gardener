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

package shootextensionstatus

import (
	"context"
	"fmt"

	gardencorev1alpha1 "github.com/gardener/gardener/pkg/apis/core/v1alpha1"
	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	"github.com/gardener/gardener/pkg/controllerutils"
	"github.com/gardener/gardener/pkg/operation/botanist/component"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ShootExtensionStatus contains functions for a ShootExtensionStatus deployer.
type ShootExtensionStatus interface {
	component.Deployer
}

// New creates a new instance of DeployWaiter for the ShootExtensionStatus.
func New(
	client client.Client,
	shoot *gardencorev1beta1.Shoot,
) ShootExtensionStatus {
	return &shootExtensionStatus{
		gardenClient: client,
		shoot:        shoot,
	}
}

type shootExtensionStatus struct {
	gardenClient client.Client
	shoot        *gardencorev1beta1.Shoot
}

func (m *shootExtensionStatus) Deploy(ctx context.Context) error {
	if m.shoot == nil {
		return fmt.Errorf("failed to deploy the ShootExtensionStatus as the Shoot is not set")
	}

	status := &gardencorev1alpha1.ShootExtensionStatus{
		ObjectMeta: metav1.ObjectMeta{
			Name:      m.shoot.Name,
			Namespace: m.shoot.Namespace,
		},
	}
	ownerReference := metav1.NewControllerRef(m.shoot, gardencorev1beta1.SchemeGroupVersion.WithKind("Shoot"))
	blockOwnerDeletion := false
	ownerReference.BlockOwnerDeletion = &blockOwnerDeletion

	_, err := controllerutils.StrategicMergePatchOrCreate(ctx, m.gardenClient, status, func() error {
		status.OwnerReferences = []metav1.OwnerReference{*ownerReference}
		return nil
	})
	if err != nil {
		return err
	}
	return nil
}

// Destroy is done automatically via OwnerReference from the ShootExtensionStatus to the Shoot resource
func (m *shootExtensionStatus) Destroy(ctx context.Context) error {
	return nil
}
