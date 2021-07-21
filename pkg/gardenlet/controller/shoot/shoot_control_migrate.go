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

package shoot

import (
	"context"
	"errors"
	"fmt"
	"time"

	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	gardencorev1beta1helper "github.com/gardener/gardener/pkg/apis/core/v1beta1/helper"
	"github.com/gardener/gardener/pkg/client/kubernetes"
	"github.com/gardener/gardener/pkg/controllerutils"
	"github.com/gardener/gardener/pkg/operation"
	botanistpkg "github.com/gardener/gardener/pkg/operation/botanist"
	shootpkg "github.com/gardener/gardener/pkg/operation/shoot"
	utilerrors "github.com/gardener/gardener/pkg/utils/errors"
	"github.com/gardener/gardener/pkg/utils/flow"
	gutil "github.com/gardener/gardener/pkg/utils/gardener"
	kutil "github.com/gardener/gardener/pkg/utils/kubernetes"
	retryutils "github.com/gardener/gardener/pkg/utils/retry"

	"github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func (c *Controller) prepareShootForMigration(ctx context.Context, logger *logrus.Entry, gardenClient kubernetes.Interface, shoot *gardencorev1beta1.Shoot, project *gardencorev1beta1.Project, cloudProfile *gardencorev1beta1.CloudProfile, seed *gardencorev1beta1.Seed) (reconcile.Result, error) {
	var (
		err error

		respectSyncPeriodOverwrite = c.respectSyncPeriodOverwrite()
		failed                     = gutil.IsShootFailed(shoot)
		ignored                    = gutil.ShouldIgnoreShoot(respectSyncPeriodOverwrite, shoot)
	)

	if failed || ignored {
		return reconcile.Result{}, fmt.Errorf("shoot %s is failed or ignored, will skip migration preparation", shoot.GetName())
	}

	c.recorder.Event(shoot, corev1.EventTypeNormal, gardencorev1beta1.EventPrepareMigration, "Prepare Shoot cluster for migration")
	shootNamespace := shootpkg.ComputeTechnicalID(project.Name, shoot)
	if err = c.updateShootStatusOperationStart(ctx, gardenClient.Client(), shoot, shootNamespace, gardencorev1beta1.LastOperationTypeMigrate); err != nil {
		return reconcile.Result{}, err
	}

	o, operationErr := c.initializeOperation(ctx, logger, gardenClient, shoot, project, cloudProfile, seed)
	if operationErr != nil {
		updateErr := c.patchShootStatusOperationError(ctx, gardenClient.Client(), o, shoot, fmt.Sprintf("Could not initialize a new operation for preparation of Shoot Control Plane migration: %s", operationErr.Error()), gardencorev1beta1.LastOperationTypeMigrate, lastErrorsOperationInitializationFailure(shoot.Status.LastErrors, operationErr)...)
		return reconcile.Result{}, utilerrors.WithSuppressed(operationErr, updateErr)
	}

	if flowErr := c.runPrepareShootControlPlaneMigration(o); flowErr != nil {
		c.recorder.Event(shoot, corev1.EventTypeWarning, gardencorev1beta1.EventMigrationPreparationFailed, flowErr.Description)
		updateErr := c.patchShootStatusOperationError(ctx, gardenClient.Client(), o, o.Shoot.Info, flowErr.Description, gardencorev1beta1.LastOperationTypeMigrate, flowErr.LastErrors...)
		return reconcile.Result{}, utilerrors.WithSuppressed(errors.New(flowErr.Description), updateErr)
	}

	return c.finalizeShootPrepareForMigration(ctx, gardenClient.Client(), shoot, o)
}

func (c *Controller) runPrepareShootControlPlaneMigration(o *operation.Operation) *gardencorev1beta1helper.WrappedLastErrors {
	var (
		ctx                          = context.TODO()
		botanist                     *botanistpkg.Botanist
		err                          error
		tasksWithErrors              []string
		kubeAPIServerDeploymentFound = true
	)

	for _, lastError := range o.Shoot.Info.Status.LastErrors {
		if lastError.TaskID != nil {
			tasksWithErrors = append(tasksWithErrors, *lastError.TaskID)
		}
	}

	errorContext := utilerrors.NewErrorContext("Shoot's control plane preparation for migration", tasksWithErrors)

	err = utilerrors.HandleErrors(errorContext,
		func(errorID string) error {
			o.CleanShootTaskErrorAndUpdateStatusLabel(ctx, errorID)
			return nil
		},
		nil,
		utilerrors.ToExecute("Create botanist", func() error {
			return retryutils.UntilTimeout(ctx, 10*time.Second, 10*time.Minute, func(context.Context) (done bool, err error) {
				botanist, err = botanistpkg.New(ctx, o)
				if err != nil {
					return retryutils.MinorError(err)
				}
				return retryutils.Ok()
			})
		}),
		utilerrors.ToExecute("Retrieve kube-apiserver deployment in the shoot namespace in the seed cluster", func() error {
			deploymentKubeAPIServer := &appsv1.Deployment{}
			if err := botanist.K8sSeedClient.APIReader().Get(ctx, kutil.Key(o.Shoot.SeedNamespace, v1beta1constants.DeploymentNameKubeAPIServer), deploymentKubeAPIServer); err != nil {
				if !apierrors.IsNotFound(err) {
					return err
				}
				kubeAPIServerDeploymentFound = false
			}
			if deploymentKubeAPIServer.DeletionTimestamp != nil {
				kubeAPIServerDeploymentFound = false
			}
			return nil
		}),
		utilerrors.ToExecute("Retrieve the Shoot namespace in the Seed cluster", func() error {
			botanist.SeedNamespaceObject = &corev1.Namespace{}
			err := botanist.K8sSeedClient.APIReader().Get(ctx, client.ObjectKey{Name: o.Shoot.SeedNamespace}, botanist.SeedNamespaceObject)
			if err != nil {
				if apierrors.IsNotFound(err) {
					o.Logger.Infof("Did not find '%s' namespace in the Seed cluster - nothing to be done", o.Shoot.SeedNamespace)
					return utilerrors.Cancel()
				}
			}
			return err
		}),
	)

	if err != nil {
		if utilerrors.WasCanceled(err) {
			return nil
		}
		return gardencorev1beta1helper.NewWrappedLastErrors(gardencorev1beta1helper.FormatLastErrDescription(err), err)
	}

	var (
		nonTerminatingNamespace = botanist.SeedNamespaceObject.Status.Phase != corev1.NamespaceTerminating
		cleanupShootResources   = nonTerminatingNamespace && kubeAPIServerDeploymentFound
		wakeupRequired          = (o.Shoot.Info.Status.IsHibernated || (!o.Shoot.Info.Status.IsHibernated && o.Shoot.HibernationEnabled)) && cleanupShootResources
		defaultTimeout          = 10 * time.Minute
		defaultInterval         = 5 * time.Second

		g = flow.NewGraph("Shoot's control plane preparation for migration")

		ensureShootStateExists = g.Add(flow.Task{
			Name: "Ensuring that ShootState exists",
			Fn:   flow.TaskFn(botanist.EnsureShootStateExists).RetryUntilTimeout(defaultInterval, defaultTimeout),
		})
		generateSecrets = g.Add(flow.Task{
			Name:         "Generating secrets and saving them into ShootState",
			Fn:           flow.TaskFn(botanist.GenerateAndSaveSecrets),
			Dependencies: flow.NewTaskIDs(ensureShootStateExists),
		})
		deploySecrets = g.Add(flow.Task{
			Name:         "Deploying Shoot certificates / keys",
			Fn:           flow.TaskFn(botanist.DeploySecrets).DoIf(nonTerminatingNamespace),
			Dependencies: flow.NewTaskIDs(ensureShootStateExists, generateSecrets),
		})
		deployETCD = g.Add(flow.Task{
			Name:         "Deploying main and events etcd",
			Fn:           flow.TaskFn(botanist.DeployEtcd).RetryUntilTimeout(defaultInterval, defaultTimeout).DoIf(nonTerminatingNamespace),
			Dependencies: flow.NewTaskIDs(deploySecrets),
		})
		scaleETCDToOne = g.Add(flow.Task{
			Name:         "Scaling etcd up",
			Fn:           flow.TaskFn(botanist.ScaleETCDToOne).RetryUntilTimeout(defaultInterval, defaultTimeout).DoIf(nonTerminatingNamespace && o.Shoot.HibernationEnabled),
			Dependencies: flow.NewTaskIDs(deployETCD),
		})
		waitUntilEtcdReady = g.Add(flow.Task{
			Name:         "Waiting until main and event etcd report readiness",
			Fn:           flow.TaskFn(botanist.WaitUntilEtcdsReady).DoIf(nonTerminatingNamespace),
			Dependencies: flow.NewTaskIDs(deployETCD, scaleETCDToOne),
		})
		wakeUpKubeAPIServer = g.Add(flow.Task{
			Name:         "Scaling Kubernetes API Server up and waiting until ready",
			Fn:           flow.TaskFn(botanist.WakeUpKubeAPIServer).DoIf(wakeupRequired),
			Dependencies: flow.NewTaskIDs(deployETCD, scaleETCDToOne),
		})
		ensureResourceManagerScaledUp = g.Add(flow.Task{
			Name:         "Ensuring that the gardener resource manager is scaled to 1",
			Fn:           flow.TaskFn(botanist.ScaleGardenerResourceManagerToOne).DoIf(cleanupShootResources),
			Dependencies: flow.NewTaskIDs(wakeUpKubeAPIServer),
		})
		annotateExtensionCRsForMigration = g.Add(flow.Task{
			Name:         "Annotating Extensions CRs with operation - migration",
			Fn:           botanist.MigrateAllExtensionResources,
			Dependencies: flow.NewTaskIDs(ensureResourceManagerScaledUp),
		})
		waitForExtensionCRsOperationMigrateToSucceed = g.Add(flow.Task{
			Name:         "Waiting until all extension CRs are with lastOperation Status Migrate = Succeeded",
			Fn:           botanist.WaitUntilAllExtensionResourcesMigrated,
			Dependencies: flow.NewTaskIDs(annotateExtensionCRsForMigration),
		})
		deleteAllExtensionCRs = g.Add(flow.Task{
			Name:         "Deleting all extension CRs from the Shoot namespace",
			Dependencies: flow.NewTaskIDs(waitForExtensionCRsOperationMigrateToSucceed),
			Fn:           botanist.DestroyAllExtensionResources,
		})
		keepManagedResourcesObjectsInShoot = g.Add(flow.Task{
			Name:         "Configuring Managed Resources objects to be kept in the Shoot",
			Fn:           flow.TaskFn(botanist.KeepObjectsForAllManagedResources).DoIf(cleanupShootResources),
			Dependencies: flow.NewTaskIDs(deleteAllExtensionCRs),
		})
		deleteAllManagedResourcesFromShootNamespace = g.Add(flow.Task{
			Name:         "Deleting all Managed Resources from the Shoot's namespace",
			Fn:           flow.TaskFn(botanist.DeleteAllManagedResourcesObjects),
			Dependencies: flow.NewTaskIDs(keepManagedResourcesObjectsInShoot, ensureResourceManagerScaledUp),
		})
		waitForManagedResourcesDeletion = g.Add(flow.Task{
			Name:         "Waiting until ManagedResources are deleted",
			Fn:           flow.TaskFn(botanist.WaitUntilAllManagedResourcesDeleted).Timeout(10 * time.Minute),
			Dependencies: flow.NewTaskIDs(deleteAllManagedResourcesFromShootNamespace),
		})
		prepareKubeAPIServerForMigration = g.Add(flow.Task{
			Name:         "Preparing kube-apiserver in Shoot's namespace for migration, by deleting it and its respective hvpa",
			Fn:           flow.TaskFn(botanist.Shoot.Components.ControlPlane.KubeAPIServer.Destroy).SkipIf(!kubeAPIServerDeploymentFound),
			Dependencies: flow.NewTaskIDs(waitForManagedResourcesDeletion, waitUntilEtcdReady),
		})
		waitUntilAPIServerDeleted = g.Add(flow.Task{
			Name:         "Waiting until kube-apiserver doesn't exist",
			Fn:           flow.TaskFn(botanist.Shoot.Components.ControlPlane.KubeAPIServer.WaitCleanup),
			Dependencies: flow.NewTaskIDs(prepareKubeAPIServerForMigration),
		})
		migrateIngressDNSRecord = g.Add(flow.Task{
			Name:         "Migrating nginx ingress DNS record",
			Fn:           flow.TaskFn(botanist.MigrateIngressDNSResources),
			Dependencies: flow.NewTaskIDs(waitUntilAPIServerDeleted),
		})
		migrateExternalDNSRecord = g.Add(flow.Task{
			Name:         "Migrating external domain DNS record",
			Fn:           flow.TaskFn(botanist.MigrateExternalDNSResources),
			Dependencies: flow.NewTaskIDs(waitUntilAPIServerDeleted),
		})
		migrateInternalDNSRecord = g.Add(flow.Task{
			Name:         "Migrating internal domain DNS record",
			Fn:           flow.TaskFn(botanist.MigrateInternalDNSResources),
			Dependencies: flow.NewTaskIDs(waitUntilAPIServerDeleted),
		})
		migrateOwnerDNSRecord = g.Add(flow.Task{
			Name:         "Migrating owner domain DNS record",
			Fn:           flow.TaskFn(botanist.MigrateOwnerDNSResources),
			Dependencies: flow.NewTaskIDs(waitUntilAPIServerDeleted),
		})
		destroyDNSProviders = g.Add(flow.Task{
			Name:         "Deleting DNS providers",
			Fn:           flow.TaskFn(botanist.DeleteDNSProviders),
			Dependencies: flow.NewTaskIDs(migrateIngressDNSRecord, migrateExternalDNSRecord, migrateInternalDNSRecord, migrateOwnerDNSRecord),
		})
		createETCDSnapshot = g.Add(flow.Task{
			Name:         "Creating ETCD Snapshot",
			Fn:           flow.TaskFn(botanist.SnapshotEtcd).DoIf(nonTerminatingNamespace),
			Dependencies: flow.NewTaskIDs(waitUntilAPIServerDeleted),
		})
		scaleETCDToZero = g.Add(flow.Task{
			Name:         "Scaling ETCD to zero",
			Fn:           flow.TaskFn(botanist.ScaleETCDToZero).DoIf(nonTerminatingNamespace),
			Dependencies: flow.NewTaskIDs(createETCDSnapshot),
		})
		migrateBackupEntryInGarden = g.Add(flow.Task{
			Name:         "Migrate BackupEntry to new seed",
			Fn:           flow.TaskFn(botanist.Shoot.Components.BackupEntry.Migrate),
			Dependencies: flow.NewTaskIDs(createETCDSnapshot),
		})
		waitUntilBackupEntryInGardenMigrated = g.Add(flow.Task{
			Name:         "Waiting for BackupEntry to be migrated to new seed",
			Fn:           flow.TaskFn(botanist.Shoot.Components.BackupEntry.WaitMigrate),
			Dependencies: flow.NewTaskIDs(migrateBackupEntryInGarden),
		})
		deleteNamespace = g.Add(flow.Task{
			Name:         "Deleting shoot namespace in Seed",
			Fn:           flow.TaskFn(botanist.DeleteSeedNamespace).RetryUntilTimeout(defaultInterval, defaultTimeout),
			Dependencies: flow.NewTaskIDs(waitUntilBackupEntryInGardenMigrated, deleteAllExtensionCRs, destroyDNSProviders, waitForManagedResourcesDeletion, scaleETCDToZero),
		})
		_ = g.Add(flow.Task{
			Name:         "Waiting until shoot namespace in Seed has been deleted",
			Fn:           botanist.WaitUntilSeedNamespaceDeleted,
			Dependencies: flow.NewTaskIDs(deleteNamespace),
		})

		f = g.Compile()
	)

	if err := f.Run(flow.Opts{
		Logger:           o.Logger,
		ProgressReporter: c.newProgressReporter(o.ReportShootProgress),
		ErrorContext:     errorContext,
		ErrorCleaner:     o.CleanShootTaskErrorAndUpdateStatusLabel,
	}); err != nil {
		o.Logger.Errorf("Failed to prepare Shoot %q for migration: %+v", o.Shoot.Info.Name, err)
		return gardencorev1beta1helper.NewWrappedLastErrors(gardencorev1beta1helper.FormatLastErrDescription(err), flow.Errors(err))
	}

	o.Logger.Infof("Successfully prepared Shoot's control plane for migration %q", o.Shoot.Info.Name)
	return nil
}

func (c *Controller) finalizeShootPrepareForMigration(ctx context.Context, gardenClient client.Client, shoot *gardencorev1beta1.Shoot, o *operation.Operation) (reconcile.Result, error) {
	if len(shoot.Status.UID) > 0 {
		if err := o.DeleteClusterResourceFromSeed(ctx); err != nil {
			lastErr := gardencorev1beta1helper.LastError(fmt.Sprintf("Could not delete Cluster resource in seed: %s", err))
			c.recorder.Event(shoot, corev1.EventTypeWarning, gardencorev1beta1.EventDeleteError, lastErr.Description)
			updateErr := c.patchShootStatusOperationError(ctx, gardenClient, o, shoot, lastErr.Description, gardencorev1beta1.LastOperationTypeMigrate, *lastErr)
			return reconcile.Result{}, utilerrors.WithSuppressed(errors.New(lastErr.Description), updateErr)
		}
	}

	metaPatch := client.MergeFrom(shoot.DeepCopy())
	controllerutils.RemoveAllTasks(shoot.Annotations)
	if err := gardenClient.Patch(ctx, shoot, metaPatch); err != nil {
		return reconcile.Result{}, err
	}

	c.recorder.Event(shoot, corev1.EventTypeNormal, gardencorev1beta1.EventMigrationPrepared, "Shoot Control Plane prepared for migration, successfully")

	statusPatch := client.MergeFrom(shoot.DeepCopy())
	shoot.Status.RetryCycleStartTime = nil
	shoot.Status.SeedName = nil
	shoot.Status.LastErrors = nil
	shoot.Status.LastOperation = &gardencorev1beta1.LastOperation{
		Type:           gardencorev1beta1.LastOperationTypeRestore,
		State:          gardencorev1beta1.LastOperationStatePending,
		Progress:       0,
		Description:    "Shoot cluster state has been successfully prepared for migration.",
		LastUpdateTime: metav1.Now(),
	}

	return reconcile.Result{}, gardenClient.Status().Patch(ctx, shoot, statusPatch)
}
