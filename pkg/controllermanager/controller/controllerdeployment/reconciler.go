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

package controllerdeployment

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	"github.com/gardener/gardener/pkg/controllerutils"
	kutil "github.com/gardener/gardener/pkg/utils/kubernetes"
)

type reconciler struct {
	logger       logr.Logger
	gardenClient client.Client
}

func (r *reconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	logger := r.logger.WithValues("controllerdeployment", req)

	controllerDeployment := &gardencorev1beta1.ControllerDeployment{}
	if err := r.gardenClient.Get(ctx, kutil.Key(req.Name), controllerDeployment); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("Object is gone, stop reconciling")
			return reconcile.Result{}, nil
		}

		logger.Error(err, "Unable to retrieve object from store")
		return reconcile.Result{}, err
	}

	if controllerDeployment.DeletionTimestamp != nil {
		if !controllerutil.ContainsFinalizer(controllerDeployment, FinalizerName) {
			return reconcile.Result{}, nil
		}

		controllerRegistrationList := &gardencorev1beta1.ControllerRegistrationList{}
		if err := r.gardenClient.List(ctx, controllerRegistrationList); err != nil {
			return reconcile.Result{}, err
		}

		for _, controllerRegistration := range controllerRegistrationList.Items {
			deployment := controllerRegistration.Spec.Deployment
			if deployment == nil {
				continue
			}
			for _, deploymentRef := range deployment.DeploymentRefs {
				if deploymentRef.Name == controllerDeployment.Name {
					return reconcile.Result{}, fmt.Errorf("cannot remove finalizer of ControllerDeployment %q because still found one ControllerRegistration", controllerRegistration.Name)
				}
			}
		}

		return reconcile.Result{}, controllerutils.PatchRemoveFinalizers(ctx, r.gardenClient, controllerDeployment, FinalizerName)
	}

	return reconcile.Result{}, controllerutils.PatchAddFinalizers(ctx, r.gardenClient, controllerDeployment, FinalizerName)
}
