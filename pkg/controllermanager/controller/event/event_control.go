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

	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	"github.com/gardener/gardener/pkg/controllermanager/apis/config"
	"github.com/go-logr/logr"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var nowFunc = time.Now

type eventReconciler struct {
	logger       logr.Logger
	gardenClient client.Client
	cfg          *config.EventControllerConfiguration
}

// NewEventReconciler creates a new instance of a reconciler which reconciles Events.
func NewEventReconciler(logger logr.Logger, gardenClient client.Client, cfg *config.EventControllerConfiguration) reconcile.Reconciler {
	return &eventReconciler{
		logger:       logger,
		gardenClient: gardenClient,
		cfg:          cfg,
	}
}

func (r *eventReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	logger := r.logger.WithValues("event", req)

	event := &corev1.Event{}
	if err := r.gardenClient.Get(ctx, req.NamespacedName, event); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("Object is gone, stop reconciling")
			return reconcile.Result{}, nil
		}

		logger.Error(err, "Unable to retrieve object from store")
		return reconcile.Result{}, err
	}

	if isShootEvent(event) {
		return reconcile.Result{}, nil
	}

	deleteAt := event.LastTimestamp.Add(r.cfg.TTLNonShootEvents.Duration)
	timeUntilDeletion := deleteAt.Sub(nowFunc())
	if timeUntilDeletion > 0 {
		return reconcile.Result{RequeueAfter: timeUntilDeletion}, nil
	}

	return reconcile.Result{}, r.gardenClient.Delete(ctx, event)
}

func isShootEvent(event *corev1.Event) bool {
	gv, err := schema.ParseGroupVersion(event.InvolvedObject.APIVersion)

	return err == nil && gv.Group == gardencorev1beta1.GroupName && event.InvolvedObject.Kind == "Shoot"
}
