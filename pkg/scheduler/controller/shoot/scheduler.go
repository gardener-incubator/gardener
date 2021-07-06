// Copyright (c) 2019 SAP SE or an SAP affiliate company. All rights reserved. This file is licensed under the Apache Software License, v. 2 except as noted otherwise in the LICENSE file
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

package shoot

import (
	"context"
	"fmt"

	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	"github.com/gardener/gardener/pkg/scheduler/apis/config"

	"k8s.io/apimachinery/pkg/types"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	// ControllerName is the name of this controller.
	ControllerName = "shoot-scheduler-controller"
)

func AddToManager(
	ctx context.Context,
	mgr manager.Manager,
	config *config.ShootSchedulerConfiguration,
) error {
	logger := mgr.GetLogger()

	reconciler := &reconciler{
		config:       config,
		logger:       logger,
		gardenClient: mgr.GetClient(),
		recorder:     mgr.GetEventRecorderFor(ControllerName),
	}

	ctrlOptions := controller.Options{
		Reconciler:              reconciler,
		MaxConcurrentReconciles: config.ConcurrentSyncs,
	}
	c, err := controller.New(ControllerName, mgr, ctrlOptions)
	if err != nil {
		return err
	}

	shootHandler := handler.EnqueueRequestsFromMapFunc(func(obj ctrlruntimeclient.Object) []reconcile.Request {
		// Ignore non-shoots
		shoot, ok := obj.(*gardencorev1beta1.Shoot)
		if !ok {
			return nil
		}

		// If the Shoot manifest already specifies a desired Seed cluster, we ignore it.
		if shoot.Spec.SeedName != nil {
			return nil
		}

		if shoot.DeletionTimestamp != nil {
			logger.Info("Ignoring shoot because it has been marked for deletion", "shoot", shoot.Name)
			return nil
		}

		return []reconcile.Request{{
			NamespacedName: types.NamespacedName{
				Namespace: obj.GetNamespace(),
				Name:      obj.GetName(),
			}},
		}
	})

	shoot := &gardencorev1beta1.Shoot{}
	if err := c.Watch(&source.Kind{Type: shoot}, shootHandler); err != nil {
		return fmt.Errorf("failed to create watcher for %T: %v", shoot, err)
	}

	return nil
}
