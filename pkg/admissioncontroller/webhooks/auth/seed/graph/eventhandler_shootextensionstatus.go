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

package graph

import (
	"time"

	gardencorev1alpha1 "github.com/gardener/gardener/pkg/apis/core/v1alpha1"
	toolscache "k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/cache"
)

func (g *graph) setupShootExtensionStatusWatch(informer cache.Informer) {
	informer.AddEventHandler(toolscache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			status, ok := obj.(*gardencorev1alpha1.ShootExtensionStatus)
			if !ok {
				return
			}
			g.handleShootExtensionStatusCreate(status)
		},

		DeleteFunc: func(obj interface{}) {
			if tombstone, ok := obj.(toolscache.DeletedFinalStateUnknown); ok {
				obj = tombstone.Obj
			}
			status, ok := obj.(*gardencorev1alpha1.ShootExtensionStatus)
			if !ok {
				return
			}
			g.handleShootExtensionStatusDelete(status)
		},
	})
}

func (g *graph) handleShootExtensionStatusCreate(status *gardencorev1alpha1.ShootExtensionStatus) {
	start := time.Now()
	defer func() {
		metricUpdateDuration.WithLabelValues("ShootExtensionStatus", "CreateOrUpdate").Observe(time.Since(start).Seconds())
	}()
	g.lock.Lock()
	defer g.lock.Unlock()

	var (
		shootExtensionStatusVertex = g.getOrCreateVertex(VertexTypeShootExtensionStatus, status.Namespace, status.Name)
		shootVertex                = g.getOrCreateVertex(VertexTypeShoot, status.Namespace, status.Name)
	)

	g.addEdge(shootExtensionStatusVertex, shootVertex)
}

func (g *graph) handleShootExtensionStatusDelete(status *gardencorev1alpha1.ShootExtensionStatus) {
	start := time.Now()
	defer func() {
		metricUpdateDuration.WithLabelValues("ShootExtensionStatus", "Delete").Observe(time.Since(start).Seconds())
	}()
	g.lock.Lock()
	defer g.lock.Unlock()

	g.deleteVertex(VertexTypeShootExtensionStatus, status.Namespace, status.Name)
}
