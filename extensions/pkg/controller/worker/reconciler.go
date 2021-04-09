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

package worker

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/runtime/inject"

	extensionscontroller "github.com/gardener/gardener/extensions/pkg/controller"
	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	gardencorev1beta1helper "github.com/gardener/gardener/pkg/apis/core/v1beta1/helper"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/gardener/gardener/pkg/controllerutils"
)

const backoffOnInfrastructureRequestThrottling = 10 * time.Minute

type reconciler struct {
	logger   logr.Logger
	actuator Actuator

	client client.Client
	reader client.Reader
}

// NewReconciler creates a new reconcile.Reconciler that reconciles
// Worker resources of Gardener's `extensions.gardener.cloud` API group.
func NewReconciler(actuator Actuator) reconcile.Reconciler {
	return extensionscontroller.OperationAnnotationWrapper(
		func() client.Object { return &extensionsv1alpha1.Worker{} },
		&reconciler{
			logger:   log.Log.WithName(ControllerName),
			actuator: actuator,
		},
	)
}

func (r *reconciler) InjectFunc(f inject.Func) error {
	return f(r.actuator)
}

func (r *reconciler) InjectClient(client client.Client) error {
	r.client = client
	return nil
}

func (r *reconciler) InjectAPIReader(reader client.Reader) error {
	r.reader = reader
	return nil
}

func (r *reconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	worker := &extensionsv1alpha1.Worker{}
	if err := r.client.Get(ctx, request.NamespacedName, worker); err != nil {
		if errors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	cluster, err := extensionscontroller.GetCluster(ctx, r.client, worker.Namespace)
	if err != nil {
		return reconcile.Result{}, err
	}

	logger := r.logger.WithValues("worker", client.ObjectKeyFromObject(worker))
	if extensionscontroller.IsFailed(cluster) {
		logger.Info("Stop reconciling Worker of failed Shoot")
		return reconcile.Result{}, nil
	}

	operationType := gardencorev1beta1helper.ComputeOperationType(worker.ObjectMeta, worker.Status.LastOperation)

	var result reconcile.Result
	switch {
	case isWorkerMigrated(worker):
		logger.Info("Stop reconciling Worker of migrated Shoot")
		result, err = reconcile.Result{}, nil
	case operationType == gardencorev1beta1.LastOperationTypeMigrate:
		result, err = r.migrate(ctx, logger.WithValues("operation", "migrate"), worker, cluster)
	case worker.DeletionTimestamp != nil:
		result, err = r.delete(ctx, logger.WithValues("operation", "delete"), worker, cluster)
	case worker.Annotations[v1beta1constants.GardenerOperation] == v1beta1constants.GardenerOperationRestore:
		result, err = r.restore(ctx, logger.WithValues("operation", "restore"), worker, cluster)
	default:
		result, err = r.reconcile(ctx, logger.WithValues("operation", "reconcile"), worker, cluster, operationType)
	}

	if err != nil {
		errorCodes := gardencorev1beta1helper.ExtractErrorCodes(gardencorev1beta1helper.DetermineError(err, err.Error()))
		if gardencorev1beta1helper.HasErrorCode(errorCodes, gardencorev1beta1.ErrorInfraRequestThrottling) {
			logger.Info("Requeue due to cloud provider request throttling", "backoff", backoffOnInfrastructureRequestThrottling, "error", err)
			return reconcile.Result{Requeue: true, RequeueAfter: backoffOnInfrastructureRequestThrottling}, nil
		}
	}

	return result, err
}

func (r *reconciler) updateStatusProcessing(ctx context.Context, logger logr.Logger, worker *extensionsv1alpha1.Worker, lastOperationType gardencorev1beta1.LastOperationType, description string) error {
	logger.Info(description)
	return extensionscontroller.TryUpdateStatus(ctx, retry.DefaultBackoff, r.client, worker, func() error {
		worker.Status.LastOperation = extensionscontroller.LastOperation(lastOperationType, gardencorev1beta1.LastOperationStateProcessing, 1, description)
		return nil
	})
}

func (r *reconciler) updateStatusError(ctx context.Context, err error, worker *extensionsv1alpha1.Worker, lastOperationType gardencorev1beta1.LastOperationType, description string) {
	updateErr := extensionscontroller.TryUpdateStatus(ctx, retry.DefaultBackoff, r.client, worker, func() error {
		worker.Status.ObservedGeneration = worker.Generation
		worker.Status.LastOperation, worker.Status.LastError = extensionscontroller.ReconcileError(lastOperationType, gardencorev1beta1helper.FormatLastErrDescription(fmt.Errorf("%s: %v", description, extensionscontroller.ReconcileErrCauseOrErr(err))), 50, gardencorev1beta1helper.ExtractErrorCodes(gardencorev1beta1helper.DetermineError(err, err.Error()))...)
		return nil
	})
	utilruntime.HandleError(updateErr)
}

func (r *reconciler) updateStatusSuccess(ctx context.Context, logger logr.Logger, worker *extensionsv1alpha1.Worker, lastOperationType gardencorev1beta1.LastOperationType, description string) error {
	logger.Info(description)
	return extensionscontroller.TryUpdateStatus(ctx, retry.DefaultBackoff, r.client, worker, func() error {
		worker.Status.ObservedGeneration = worker.Generation
		worker.Status.LastOperation, worker.Status.LastError = extensionscontroller.ReconcileSucceeded(lastOperationType, description)
		return nil
	})
}

func (r *reconciler) removeFinalizerFromWorker(ctx context.Context, logger logr.Logger, worker *extensionsv1alpha1.Worker) error {
	logger.Info("Removing finalizer")
	if err := controllerutils.RemoveFinalizer(ctx, r.reader, r.client, worker, FinalizerName); err != nil {
		return fmt.Errorf("error removing finalizer from Worker: %+v", err)
	}
	return nil
}

func (r *reconciler) removeAnnotation(ctx context.Context, logger logr.Logger, worker *extensionsv1alpha1.Worker) error {
	logger.Info("Removing operation annotation")
	return extensionscontroller.RemoveAnnotation(ctx, r.client, worker, v1beta1constants.GardenerOperation)
}

func (r *reconciler) migrate(ctx context.Context, logger logr.Logger, worker *extensionsv1alpha1.Worker, cluster *extensionscontroller.Cluster) (reconcile.Result, error) {
	if err := r.updateStatusProcessing(ctx, logger, worker, gardencorev1beta1.LastOperationTypeMigrate, "Starting Migration of the worker"); err != nil {
		return reconcile.Result{}, err
	}

	if err := r.actuator.Migrate(ctx, worker, cluster); err != nil {
		r.updateStatusError(ctx, err, worker, gardencorev1beta1.LastOperationTypeMigrate, "Error migrating worker")
		return extensionscontroller.ReconcileErr(err)
	}

	if err := r.updateStatusSuccess(ctx, logger, worker, gardencorev1beta1.LastOperationTypeMigrate, "Successfully migrate worker"); err != nil {
		return reconcile.Result{}, err
	}

	if err := r.removeFinalizerFromWorker(ctx, logger, worker); err != nil {
		return reconcile.Result{}, err
	}

	// remove operation annotation 'migrate'
	if err := r.removeAnnotation(ctx, logger, worker); err != nil {
		return reconcile.Result{}, fmt.Errorf("error removing annotation from Worker: %+v", err)
	}

	return reconcile.Result{}, nil
}

func (r *reconciler) delete(ctx context.Context, logger logr.Logger, worker *extensionsv1alpha1.Worker, cluster *extensionscontroller.Cluster) (reconcile.Result, error) {
	if !controllerutil.ContainsFinalizer(worker, FinalizerName) {
		logger.Info("Deleting worker causes a no-op as there is no finalizer")
		return reconcile.Result{}, nil
	}

	if err := r.updateStatusProcessing(ctx, logger, worker, gardencorev1beta1.LastOperationTypeDelete, "Deleting the worker"); err != nil {
		return reconcile.Result{}, err
	}

	if err := r.actuator.Delete(ctx, worker, cluster); err != nil {
		r.updateStatusError(ctx, err, worker, gardencorev1beta1.LastOperationTypeDelete, "Error deleting worker")
		return extensionscontroller.ReconcileErr(err)
	}

	if err := r.updateStatusSuccess(ctx, logger, worker, gardencorev1beta1.LastOperationTypeDelete, "Successfully deleted worker"); err != nil {
		return reconcile.Result{}, err
	}

	err := r.removeFinalizerFromWorker(ctx, logger, worker)
	return reconcile.Result{}, err
}

func (r *reconciler) reconcile(ctx context.Context, logger logr.Logger, worker *extensionsv1alpha1.Worker, cluster *extensionscontroller.Cluster, operationType gardencorev1beta1.LastOperationType) (reconcile.Result, error) {
	logger.Info("Ensuring finalizer")
	if err := controllerutils.EnsureFinalizer(ctx, r.reader, r.client, worker, FinalizerName); err != nil {
		return reconcile.Result{}, err
	}
	if err := r.updateStatusProcessing(ctx, logger, worker, operationType, "Reconciling the worker"); err != nil {
		return reconcile.Result{}, err
	}

	if err := r.actuator.Reconcile(ctx, worker, cluster); err != nil {
		r.updateStatusError(ctx, err, worker, operationType, "Error reconciling worker")
		return extensionscontroller.ReconcileErr(err)
	}

	if err := r.updateStatusSuccess(ctx, logger, worker, operationType, "Successfully reconciled worker"); err != nil {
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

func (r *reconciler) restore(ctx context.Context, logger logr.Logger, worker *extensionsv1alpha1.Worker, cluster *extensionscontroller.Cluster) (reconcile.Result, error) {
	if err := r.updateStatusProcessing(ctx, logger, worker, gardencorev1beta1.LastOperationTypeRestore, "Restoring the worker"); err != nil {
		return reconcile.Result{}, err
	}

	if err := r.actuator.Restore(ctx, worker, cluster); err != nil {
		r.updateStatusError(ctx, err, worker, gardencorev1beta1.LastOperationTypeRestore, "Error restoring worker")
		return extensionscontroller.ReconcileErr(err)
	}

	if err := r.updateStatusSuccess(ctx, logger, worker, gardencorev1beta1.LastOperationTypeRestore, "Successfully reconciled worker"); err != nil {
		return reconcile.Result{}, err
	}

	// remove operation annotation 'restore'
	if err := r.removeAnnotation(ctx, logger, worker); err != nil {
		return reconcile.Result{}, fmt.Errorf("error removing annotation from Worker: %+v", err)
	}

	// requeue to trigger reconciliation
	return reconcile.Result{Requeue: true}, nil
}

func isWorkerMigrated(worker *extensionsv1alpha1.Worker) bool {
	return worker.Status.LastOperation != nil &&
		worker.Status.LastOperation.Type == gardencorev1beta1.LastOperationTypeMigrate &&
		worker.Status.LastOperation.State == gardencorev1beta1.LastOperationStateSucceeded
}
