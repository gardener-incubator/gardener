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

package shoot

import (
	"context"
	"fmt"
	"time"

	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	gardencorev1beta1helper "github.com/gardener/gardener/pkg/apis/core/v1beta1/helper"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/gardener/gardener/pkg/client/kubernetes"
	"github.com/gardener/gardener/pkg/client/kubernetes/clientmap/keys"
	"github.com/gardener/gardener/pkg/controllerutils"
	"github.com/gardener/gardener/pkg/operation"
	botanistpkg "github.com/gardener/gardener/pkg/operation/botanist"
	"github.com/gardener/gardener/pkg/utils/errors"
	"github.com/gardener/gardener/pkg/utils/flow"
	kutil "github.com/gardener/gardener/pkg/utils/kubernetes"
	retryutils "github.com/gardener/gardener/pkg/utils/retry"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// runDeleteShootFlow deletes a Shoot cluster entirely.
// It receives an Operation object <o> which stores the Shoot object and an ErrorContext which contains error from the previous operation.
func (c *Controller) runDeleteShootFlow(ctx context.Context, o *operation.Operation) *gardencorev1beta1helper.WrappedLastErrors {
	var (
		botanist                             *botanistpkg.Botanist
		kubeAPIServerDeploymentFound         = true
		kubeControllerManagerDeploymentFound = true
		kubeAPIServerDeploymentReplicas      int32
		infrastructure                       *extensionsv1alpha1.Infrastructure
		controlPlaneDeploymentNeeded         bool
		tasksWithErrors                      []string
		err                                  error
	)

	for _, lastError := range o.Shoot.Info.Status.LastErrors {
		if lastError.TaskID != nil {
			tasksWithErrors = append(tasksWithErrors, *lastError.TaskID)
		}
	}

	errorContext := errors.NewErrorContext("Shoot cluster deletion", tasksWithErrors)

	err = errors.HandleErrors(errorContext,
		func(errorID string) error {
			o.CleanShootTaskErrorAndUpdateStatusLabel(ctx, errorID)
			return nil
		},
		nil,
		errors.ToExecute("Create botanist", func() error {
			return retryutils.UntilTimeout(ctx, 10*time.Second, 10*time.Minute, func(context.Context) (done bool, err error) {
				botanist, err = botanistpkg.New(ctx, o)
				if err != nil {
					return retryutils.MinorError(err)
				}
				return retryutils.Ok()
			})
		}),
		errors.ToExecute("Check required extensions exist", func() error {
			return botanist.WaitUntilRequiredExtensionsReady(ctx)
		}),
		// We first check whether the namespace in the Seed cluster does exist - if it does not, then we assume that
		// all resources have already been deleted. We can delete the Shoot resource as a consequence.
		errors.ToExecute("Retrieve the Shoot namespace in the Seed cluster", func() error {
			botanist.SeedNamespaceObject = &corev1.Namespace{}
			err := botanist.K8sSeedClient.APIReader().Get(ctx, client.ObjectKey{Name: o.Shoot.SeedNamespace}, botanist.SeedNamespaceObject)
			if err != nil {
				if apierrors.IsNotFound(err) {
					o.Logger.Infof("Did not find '%s' namespace in the Seed cluster - nothing to be done", o.Shoot.SeedNamespace)
					return errors.Cancel()
				}
			}
			return err
		}),
		// We check whether the kube-apiserver deployment exists in the shoot namespace. If it does not, then we assume
		// that it has never been deployed successfully, or that we have deleted it in a previous run because we already
		// cleaned up. We follow that no (more) resources can have been deployed in the shoot cluster, thus there is nothing
		// to delete anymore.
		errors.ToExecute("Retrieve kube-apiserver deployment in the shoot namespace in the seed cluster", func() error {
			deploymentKubeAPIServer := &appsv1.Deployment{}
			if err := botanist.K8sSeedClient.APIReader().Get(ctx, kutil.Key(o.Shoot.SeedNamespace, v1beta1constants.DeploymentNameKubeAPIServer), deploymentKubeAPIServer); err != nil {
				if !apierrors.IsNotFound(err) {
					return err
				}
				kubeAPIServerDeploymentFound = false
			} else if deploymentKubeAPIServer.DeletionTimestamp != nil {
				kubeAPIServerDeploymentFound = false
			} else if deploymentKubeAPIServer.Spec.Replicas != nil {
				kubeAPIServerDeploymentReplicas = *deploymentKubeAPIServer.Spec.Replicas
			}

			return nil
		}),
		// We check whether the kube-controller-manager deployment exists in the shoot namespace. If it does not, then we assume
		// that it has never been deployed successfully, or that we have deleted it in a previous run because we already
		// cleaned up.
		errors.ToExecute("Retrieve the kube-controller-manager deployment in the shoot namespace in the seed cluster", func() error {
			deploymentKubeControllerManager := &appsv1.Deployment{}
			if err := botanist.K8sSeedClient.APIReader().Get(ctx, kutil.Key(o.Shoot.SeedNamespace, v1beta1constants.DeploymentNameKubeControllerManager), deploymentKubeControllerManager); err != nil {
				if !apierrors.IsNotFound(err) {
					return err
				}
				kubeControllerManagerDeploymentFound = false
			}
			if deploymentKubeControllerManager.DeletionTimestamp != nil {
				kubeControllerManagerDeploymentFound = false
			}
			return nil
		}),
		errors.ToExecute("Retrieve the infrastructure resource", func() error {
			obj, err := botanist.Shoot.Components.Extensions.Infrastructure.Get(ctx)
			if err != nil {
				if apierrors.IsNotFound(err) {
					return nil
				}
				return err
			}
			infrastructure = obj
			return nil
		}),
		errors.ToExecute("Check whether control plane deployment is needed", func() error {
			controlPlaneDeploymentNeeded, err = needsControlPlaneDeployment(ctx, o, kubeAPIServerDeploymentFound, infrastructure)
			return err
		}),
	)

	if err != nil {
		if errors.WasCanceled(err) {
			return nil
		}
		return gardencorev1beta1helper.NewWrappedLastErrors(gardencorev1beta1helper.FormatLastErrDescription(err), err)
	}

	var (
		nonTerminatingNamespace = botanist.SeedNamespaceObject.Status.Phase != corev1.NamespaceTerminating
		cleanupShootResources   = nonTerminatingNamespace && kubeAPIServerDeploymentFound && infrastructure != nil
		defaultInterval         = 5 * time.Second
		defaultTimeout          = 30 * time.Second
		staticNodesCIDR         = o.Shoot.Info.Spec.Networking.Nodes != nil
		useSNI                  = botanist.APIServerSNIEnabled()
		sniPhase                = botanist.Shoot.Components.ControlPlane.KubeAPIServerSNIPhase

		g = flow.NewGraph("Shoot cluster deletion")

		ensureShootStateExists = g.Add(flow.Task{
			Name: "Ensuring that ShootState exists",
			Fn:   flow.TaskFn(botanist.EnsureShootStateExists).RetryUntilTimeout(defaultInterval, defaultTimeout),
		})
		ensureShootClusterIdentity = g.Add(flow.Task{
			Name: "Ensuring Shoot cluster identity",
			Fn:   flow.TaskFn(botanist.EnsureShootClusterIdentity).RetryUntilTimeout(defaultInterval, defaultTimeout),
		})

		// We need to ensure that the deployed cloud provider secret is up-to-date. In case it has changed then we
		// need to redeploy the cloud provider config (containing the secrets for some cloud providers) as well as
		// restart the components using the secrets (cloud controller, controller manager). We also need to update all
		// existing machine class secrets.
		deployCloudProviderSecret = g.Add(flow.Task{
			Name: "Deploying cloud provider account secret",
			Fn:   flow.TaskFn(botanist.DeployCloudProviderSecret).DoIf(nonTerminatingNamespace),
		})
		deployKubeAPIServerService = g.Add(flow.Task{
			Name: "Deploying Kubernetes API server service in the Seed cluster",
			Fn: flow.TaskFn(func(ctx context.Context) error {
				return botanist.DeployKubeAPIService(ctx, sniPhase)
			}).DoIf(cleanupShootResources).RetryUntilTimeout(defaultInterval, defaultTimeout),
			Dependencies: flow.NewTaskIDs(ensureShootClusterIdentity),
		})
		_ = g.Add(flow.Task{
			Name:         "Deploying Kubernetes API server service SNI settings in the Seed cluster",
			Fn:           flow.TaskFn(botanist.DeployKubeAPIServerSNI).RetryUntilTimeout(defaultInterval, defaultTimeout).DoIf(cleanupShootResources),
			Dependencies: flow.NewTaskIDs(deployKubeAPIServerService),
		})
		waitUntilKubeAPIServerServiceIsReady = g.Add(flow.Task{
			Name:         "Waiting until Kubernetes API LoadBalancer in the Seed cluster has reported readiness",
			Fn:           flow.TaskFn(botanist.Shoot.Components.ControlPlane.KubeAPIServerService.Wait).DoIf(cleanupShootResources),
			Dependencies: flow.NewTaskIDs(deployKubeAPIServerService),
		})
		generateSecrets = g.Add(flow.Task{
			Name:         "Generating secrets and saving them into ShootState",
			Fn:           flow.TaskFn(botanist.GenerateAndSaveSecrets).DoIf(nonTerminatingNamespace),
			Dependencies: flow.NewTaskIDs(ensureShootStateExists),
		})
		deploySecrets = g.Add(flow.Task{
			Name:         "Deploying Shoot certificates / keys",
			Fn:           flow.TaskFn(botanist.DeploySecrets).DoIf(nonTerminatingNamespace),
			Dependencies: flow.NewTaskIDs(ensureShootStateExists, generateSecrets),
		})
		deployReferencedResources = g.Add(flow.Task{
			Name:         "Deploying referenced resources",
			Fn:           flow.TaskFn(botanist.DeployReferencedResources).RetryUntilTimeout(defaultInterval, defaultTimeout).DoIf(nonTerminatingNamespace),
			Dependencies: flow.NewTaskIDs(ensureShootStateExists),
		})
		deployInternalDomainDNSRecord = g.Add(flow.Task{
			Name:         "Deploying internal domain DNS record",
			Fn:           flow.TaskFn(botanist.DeployInternalDNSResources).DoIf(cleanupShootResources),
			Dependencies: flow.NewTaskIDs(deployReferencedResources, waitUntilKubeAPIServerServiceIsReady),
		})
		deployAdditionalDNSProviders = g.Add(flow.Task{
			Name:         "Deploying additional DNS providers",
			Fn:           flow.TaskFn(botanist.DeployAdditionalDNSProviders).RetryUntilTimeout(defaultInterval, defaultTimeout).DoIf(nonTerminatingNamespace),
			Dependencies: flow.NewTaskIDs(deployReferencedResources),
		})
		_ = g.Add(flow.Task{
			Name:         "Deploying network policies",
			Fn:           flow.TaskFn(botanist.Shoot.Components.NetworkPolicies.Deploy).RetryUntilTimeout(defaultInterval, defaultTimeout).DoIf(nonTerminatingNamespace),
			Dependencies: flow.NewTaskIDs(ensureShootStateExists).InsertIf(!staticNodesCIDR),
		})
		deployETCD = g.Add(flow.Task{
			Name:         "Deploying main and events etcd",
			Fn:           flow.TaskFn(botanist.DeployEtcd).RetryUntilTimeout(defaultInterval, defaultTimeout).DoIf(cleanupShootResources),
			Dependencies: flow.NewTaskIDs(deploySecrets, deployCloudProviderSecret),
		})
		_ = g.Add(flow.Task{
			Name:         "Scale up etcd main and event",
			Fn:           flow.TaskFn(botanist.ScaleETCDToOne).RetryUntilTimeout(defaultInterval, defaultTimeout).DoIf(cleanupShootResources),
			Dependencies: flow.NewTaskIDs(deployETCD),
		})
		waitUntilEtcdReady = g.Add(flow.Task{
			Name:         "Waiting until main and event etcd report readiness",
			Fn:           flow.TaskFn(botanist.WaitUntilEtcdsReady).DoIf(cleanupShootResources),
			Dependencies: flow.NewTaskIDs(deployETCD),
		})
		// Redeploy the control plane to make sure all components that depend on the cloud provider secret are restarted
		// in case it has changed. Also, it's needed for other control plane components like the kube-apiserver or kube-
		// controller-manager to be updateable due to provider config injection.
		deployControlPlane = g.Add(flow.Task{
			Name:         "Deploying Shoot control plane",
			Fn:           flow.TaskFn(botanist.DeployControlPlane).RetryUntilTimeout(defaultInterval, defaultTimeout).DoIf(cleanupShootResources && controlPlaneDeploymentNeeded),
			Dependencies: flow.NewTaskIDs(deploySecrets, deployCloudProviderSecret, ensureShootClusterIdentity),
		})
		waitUntilControlPlaneReady = g.Add(flow.Task{
			Name:         "Waiting until Shoot control plane has been reconciled",
			Fn:           flow.TaskFn(botanist.Shoot.Components.Extensions.ControlPlane.Wait).DoIf(cleanupShootResources && controlPlaneDeploymentNeeded),
			Dependencies: flow.NewTaskIDs(deployControlPlane),
		})
		generateEncryptionConfigurationMetaData = g.Add(flow.Task{
			Name:         "Generating etcd encryption configuration",
			Fn:           flow.TaskFn(botanist.GenerateEncryptionConfiguration).RetryUntilTimeout(defaultInterval, defaultTimeout).DoIf(cleanupShootResources),
			Dependencies: flow.NewTaskIDs(ensureShootStateExists),
		})
		createOrUpdateETCDEncryptionConfiguration = g.Add(flow.Task{
			Name:         "Applying etcd encryption configuration",
			Fn:           flow.TaskFn(botanist.ApplyEncryptionConfiguration).RetryUntilTimeout(defaultInterval, defaultTimeout).DoIf(cleanupShootResources),
			Dependencies: flow.NewTaskIDs(ensureShootStateExists, generateEncryptionConfigurationMetaData),
		})
		deployKubeAPIServer = g.Add(flow.Task{
			Name: "Deploying Kubernetes API server",
			Fn:   flow.TaskFn(botanist.DeployKubeAPIServer).RetryUntilTimeout(defaultInterval, defaultTimeout).DoIf(cleanupShootResources),
			Dependencies: flow.NewTaskIDs(
				deploySecrets,
				deployETCD,
				waitUntilEtcdReady,
				waitUntilKubeAPIServerServiceIsReady,
				waitUntilControlPlaneReady,
				createOrUpdateETCDEncryptionConfiguration,
			).InsertIf(!staticNodesCIDR),
		})
		_ = g.Add(flow.Task{
			Name: "Scale up Kubernetes API server",
			Fn: flow.TaskFn(botanist.ScaleKubeAPIServerToOne).
				RetryUntilTimeout(defaultInterval, defaultTimeout).
				DoIf(cleanupShootResources && kubeAPIServerDeploymentReplicas == 0),
			Dependencies: flow.NewTaskIDs(deployKubeAPIServer),
		})
		waitUntilKubeAPIServerIsReady = g.Add(flow.Task{
			Name:         "Waiting until Kubernetes API server reports readiness",
			Fn:           flow.TaskFn(botanist.Shoot.Components.ControlPlane.KubeAPIServer.Wait).DoIf(cleanupShootResources),
			Dependencies: flow.NewTaskIDs(deployKubeAPIServer),
		})
		deployControlPlaneExposure = g.Add(flow.Task{
			Name:         "Deploying shoot control plane exposure components",
			Fn:           flow.TaskFn(botanist.DeployControlPlaneExposure).RetryUntilTimeout(defaultInterval, defaultTimeout).SkipIf(useSNI).DoIf(cleanupShootResources),
			Dependencies: flow.NewTaskIDs(deployReferencedResources, waitUntilKubeAPIServerIsReady),
		})
		waitUntilControlPlaneExposureReady = g.Add(flow.Task{
			Name:         "Waiting until Shoot control plane exposure has been reconciled",
			Fn:           flow.TaskFn(botanist.Shoot.Components.Extensions.ControlPlaneExposure.Wait).SkipIf(useSNI).DoIf(cleanupShootResources),
			Dependencies: flow.NewTaskIDs(deployControlPlaneExposure),
		})
		initializeShootClients = g.Add(flow.Task{
			Name:         "Initializing connection to Shoot",
			Fn:           flow.TaskFn(botanist.InitializeShootClients).DoIf(cleanupShootResources).RetryUntilTimeout(defaultInterval, 2*time.Minute),
			Dependencies: flow.NewTaskIDs(deployCloudProviderSecret, waitUntilKubeAPIServerIsReady, deployInternalDomainDNSRecord, waitUntilControlPlaneExposureReady),
		})

		// Redeploy the worker extensions, and kube-controller-manager to make sure all components that depend on the
		// cloud provider secret are restarted in case it has changed.
		deployKubeControllerManager = g.Add(flow.Task{
			Name:         "Deploying Kubernetes controller manager",
			Fn:           flow.TaskFn(botanist.DeployKubeControllerManager).DoIf(cleanupShootResources && kubeControllerManagerDeploymentFound).RetryUntilTimeout(defaultInterval, defaultTimeout),
			Dependencies: flow.NewTaskIDs(deploySecrets, deployCloudProviderSecret, waitUntilControlPlaneReady, initializeShootClients),
		})
		_ = g.Add(flow.Task{
			Name:         "Scale up Kubernetes controller manager",
			Fn:           flow.TaskFn(botanist.ScaleKubeControllerManagerToOne).DoIf(cleanupShootResources && kubeControllerManagerDeploymentFound),
			Dependencies: flow.NewTaskIDs(deployKubeControllerManager),
		})
		deployGardenerResourceManager = g.Add(flow.Task{
			Name:         "Deploying gardener-resource-manager",
			Fn:           flow.TaskFn(botanist.DeployGardenerResourceManager).DoIf(cleanupShootResources),
			Dependencies: flow.NewTaskIDs(initializeShootClients),
		})
		_ = g.Add(flow.Task{
			Name:         "Scale up gardener-resource-manager",
			Fn:           flow.TaskFn(botanist.ScaleGardenerResourceManagerToOne).DoIf(cleanupShootResources),
			Dependencies: flow.NewTaskIDs(deployGardenerResourceManager),
		})
		deleteSeedMonitoring = g.Add(flow.Task{
			Name:         "Deleting shoot monitoring stack in Seed",
			Fn:           flow.TaskFn(botanist.DeleteSeedMonitoring).RetryUntilTimeout(defaultInterval, defaultTimeout),
			Dependencies: flow.NewTaskIDs(initializeShootClients),
		})
		deleteClusterAutoscaler = g.Add(flow.Task{
			Name:         "Deleting cluster autoscaler",
			Fn:           flow.TaskFn(botanist.Shoot.Components.ControlPlane.ClusterAutoscaler.Destroy).RetryUntilTimeout(defaultInterval, defaultTimeout),
			Dependencies: flow.NewTaskIDs(initializeShootClients),
		})

		cleanupWebhooks = g.Add(flow.Task{
			Name:         "Cleaning up webhooks",
			Fn:           flow.TaskFn(botanist.CleanWebhooks).Timeout(10 * time.Minute).DoIf(cleanupShootResources),
			Dependencies: flow.NewTaskIDs(initializeShootClients, deployGardenerResourceManager),
		})
		waitForControllersToBeActive = g.Add(flow.Task{
			Name:         "Waiting until kube-controller-manager is active",
			Fn:           flow.TaskFn(botanist.WaitForKubeControllerManagerToBeActive).DoIf(cleanupShootResources && kubeControllerManagerDeploymentFound).RetryUntilTimeout(defaultInterval, defaultTimeout),
			Dependencies: flow.NewTaskIDs(initializeShootClients, cleanupWebhooks, deployControlPlane, deployKubeControllerManager),
		})
		cleanExtendedAPIs = g.Add(flow.Task{
			Name:         "Cleaning extended API groups",
			Fn:           flow.TaskFn(botanist.CleanExtendedAPIs).Timeout(10 * time.Minute).DoIf(cleanupShootResources && !metav1.HasAnnotation(o.Shoot.Info.ObjectMeta, v1beta1constants.AnnotationShootSkipCleanup)),
			Dependencies: flow.NewTaskIDs(initializeShootClients, deleteClusterAutoscaler, waitForControllersToBeActive),
		})

		syncPointReadyForCleanup = flow.NewTaskIDs(
			initializeShootClients,
			cleanExtendedAPIs,
			deployControlPlane,
			deployKubeControllerManager,
			waitForControllersToBeActive,
			deployAdditionalDNSProviders,
		)

		cleanKubernetesResources = g.Add(flow.Task{
			Name:         "Cleaning Kubernetes resources",
			Fn:           flow.TaskFn(botanist.CleanKubernetesResources).Timeout(10 * time.Minute).DoIf(cleanupShootResources),
			Dependencies: flow.NewTaskIDs(syncPointReadyForCleanup),
		})
		cleanShootNamespaces = g.Add(flow.Task{
			Name:         "Cleaning shoot namespaces",
			Fn:           flow.TaskFn(botanist.CleanShootNamespaces).Timeout(10 * time.Minute).DoIf(cleanupShootResources),
			Dependencies: flow.NewTaskIDs(cleanKubernetesResources),
		})
		destroyNetwork = g.Add(flow.Task{
			Name:         "Destroying shoot network plugin",
			Fn:           flow.TaskFn(botanist.Shoot.Components.Extensions.Network.Destroy).RetryUntilTimeout(defaultInterval, defaultTimeout),
			Dependencies: flow.NewTaskIDs(cleanShootNamespaces),
		})
		waitUntilNetworkIsDestroyed = g.Add(flow.Task{
			Name:         "Waiting until shoot network plugin has been destroyed",
			Fn:           botanist.Shoot.Components.Extensions.Network.WaitCleanup,
			Dependencies: flow.NewTaskIDs(destroyNetwork),
		})
		destroyWorker = g.Add(flow.Task{
			Name:         "Destroying shoot workers",
			Fn:           flow.TaskFn(botanist.Shoot.Components.Extensions.Worker.Destroy).RetryUntilTimeout(defaultInterval, defaultTimeout),
			Dependencies: flow.NewTaskIDs(cleanShootNamespaces),
		})
		waitUntilWorkerDeleted = g.Add(flow.Task{
			Name:         "Waiting until shoot worker nodes have been terminated",
			Fn:           botanist.Shoot.Components.Extensions.Worker.WaitCleanup,
			Dependencies: flow.NewTaskIDs(destroyWorker),
		})
		deleteAllOperatingSystemConfigs = g.Add(flow.Task{
			Name:         "Deleting operating system config resources",
			Fn:           flow.TaskFn(botanist.Shoot.Components.Extensions.OperatingSystemConfig.Destroy).RetryUntilTimeout(defaultInterval, defaultTimeout),
			Dependencies: flow.NewTaskIDs(waitUntilWorkerDeleted),
		})
		_ = g.Add(flow.Task{
			Name:         "Waiting until all operating system config resources are deleted",
			Fn:           flow.TaskFn(botanist.Shoot.Components.Extensions.OperatingSystemConfig.WaitCleanup).SkipIf(o.Shoot.HibernationEnabled),
			Dependencies: flow.NewTaskIDs(deleteAllOperatingSystemConfigs),
		})
		deleteManagedResources = g.Add(flow.Task{
			Name:         "Deleting managed resources",
			Fn:           flow.TaskFn(botanist.DeleteManagedResources).DoIf(cleanupShootResources).RetryUntilTimeout(defaultInterval, defaultTimeout),
			Dependencies: flow.NewTaskIDs(cleanShootNamespaces, waitUntilWorkerDeleted),
		})
		waitUntilManagedResourcesDeleted = g.Add(flow.Task{
			Name:         "Waiting until managed resources have been deleted",
			Fn:           flow.TaskFn(botanist.WaitUntilManagedResourcesDeleted).DoIf(cleanupShootResources).Timeout(10 * time.Minute),
			Dependencies: flow.NewTaskIDs(deleteManagedResources),
		})
		deleteExtensionResources = g.Add(flow.Task{
			Name:         "Deleting extension resources",
			Fn:           flow.TaskFn(botanist.Shoot.Components.Extensions.Extension.Destroy).RetryUntilTimeout(defaultInterval, defaultTimeout),
			Dependencies: flow.NewTaskIDs(cleanKubernetesResources, waitUntilManagedResourcesDeleted),
		})
		waitUntilExtensionResourcesDeleted = g.Add(flow.Task{
			Name:         "Waiting until extension resources have been deleted",
			Fn:           botanist.Shoot.Components.Extensions.Extension.WaitCleanup,
			Dependencies: flow.NewTaskIDs(deleteExtensionResources),
		})
		deleteContainerRuntimeResources = g.Add(flow.Task{
			Name:         "Deleting container runtime resources",
			Fn:           flow.TaskFn(botanist.Shoot.Components.Extensions.ContainerRuntime.Destroy).RetryUntilTimeout(defaultInterval, defaultTimeout),
			Dependencies: flow.NewTaskIDs(initializeShootClients, cleanKubernetesResources, cleanShootNamespaces),
		})
		waitUntilContainerRuntimeResourcesDeleted = g.Add(flow.Task{
			Name:         "Waiting until stale container runtime resources are deleted",
			Fn:           botanist.Shoot.Components.Extensions.ContainerRuntime.WaitCleanup,
			Dependencies: flow.NewTaskIDs(deleteContainerRuntimeResources),
		})

		// Services (and other objects that have a footprint in the infrastructure) still don't have finalizers yet. There is no way to
		// determine whether all the resources have been deleted successfully yet, whether there was an error, or whether they are still
		// pending. While most providers have implemented custom clean up already (basically, duplicated the code in the CCM) not everybody
		// has, especially not for all objects.
		// Until service finalizers are enabled by default with Kubernetes 1.16 and our minimum supported seed version is raised to 1.16 we
		// can not do much more than best-effort waiting until everything has been cleaned up. That's what the following task is doing.
		timeForInfrastructureResourceCleanup = g.Add(flow.Task{
			Name: "Waiting until time for infrastructure resource cleanup has elapsed",
			Fn: flow.TaskFn(func(ctx context.Context) error {
				ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
				defer cancel()

				<-ctx.Done()
				return nil
			}).DoIf(cleanupShootResources),
			Dependencies: flow.NewTaskIDs(deleteManagedResources),
		})

		syncPointCleaned = flow.NewTaskIDs(
			cleanupWebhooks,
			cleanExtendedAPIs,
			cleanKubernetesResources,
			cleanShootNamespaces,
			deleteAllOperatingSystemConfigs,
			waitUntilWorkerDeleted,
			waitUntilManagedResourcesDeleted,
			timeForInfrastructureResourceCleanup,
			destroyNetwork,
			waitUntilNetworkIsDestroyed,
			waitUntilExtensionResourcesDeleted,
			waitUntilContainerRuntimeResourcesDeleted,
		)
		destroyControlPlane = g.Add(flow.Task{
			Name:         "Destroying shoot control plane",
			Fn:           flow.TaskFn(botanist.Shoot.Components.Extensions.ControlPlane.Destroy).RetryUntilTimeout(defaultInterval, defaultTimeout),
			Dependencies: flow.NewTaskIDs(syncPointCleaned),
		})
		waitUntilControlPlaneDeleted = g.Add(flow.Task{
			Name:         "Waiting until shoot control plane has been destroyed",
			Fn:           botanist.Shoot.Components.Extensions.ControlPlane.WaitCleanup,
			Dependencies: flow.NewTaskIDs(destroyControlPlane),
		})

		deleteKubeAPIServer = g.Add(flow.Task{
			Name:         "Deleting Kubernetes API server",
			Fn:           flow.TaskFn(botanist.DeleteKubeAPIServer).RetryUntilTimeout(defaultInterval, defaultTimeout),
			Dependencies: flow.NewTaskIDs(syncPointCleaned, waitUntilControlPlaneDeleted),
		})
		destroyKubeAPIServerSNI = g.Add(flow.Task{
			Name:         "Destroying Kubernetes API server service SNI",
			Fn:           flow.TaskFn(botanist.Shoot.Components.ControlPlane.KubeAPIServerSNI.Destroy).RetryUntilTimeout(defaultInterval, defaultTimeout),
			Dependencies: flow.NewTaskIDs(deleteKubeAPIServer),
		})
		_ = g.Add(flow.Task{
			Name:         "Destroying Kubernetes API server service",
			Fn:           flow.TaskFn(botanist.Shoot.Components.ControlPlane.KubeAPIServerService.Destroy).RetryUntilTimeout(defaultInterval, defaultTimeout),
			Dependencies: flow.NewTaskIDs(deleteKubeAPIServer, destroyKubeAPIServerSNI),
		})

		destroyControlPlaneExposure = g.Add(flow.Task{
			Name:         "Destroying shoot control plane exposure",
			Fn:           botanist.Shoot.Components.Extensions.ControlPlaneExposure.Destroy,
			Dependencies: flow.NewTaskIDs(deleteKubeAPIServer),
		})
		waitUntilControlPlaneExposureDeleted = g.Add(flow.Task{
			Name:         "Waiting until shoot control plane exposure has been destroyed",
			Fn:           botanist.Shoot.Components.Extensions.ControlPlaneExposure.WaitCleanup,
			Dependencies: flow.NewTaskIDs(destroyControlPlaneExposure),
		})

		destroyIngressDomainDNSRecord = g.Add(flow.Task{
			Name:         "Destroying nginx ingress DNS record",
			Fn:           flow.TaskFn(botanist.DestroyIngressDNSResources),
			Dependencies: flow.NewTaskIDs(syncPointCleaned),
		})
		destroyInfrastructure = g.Add(flow.Task{
			Name:         "Destroying shoot infrastructure",
			Fn:           flow.TaskFn(botanist.Shoot.Components.Extensions.Infrastructure.Destroy).RetryUntilTimeout(defaultInterval, defaultTimeout),
			Dependencies: flow.NewTaskIDs(syncPointCleaned, waitUntilControlPlaneDeleted),
		})
		waitUntilInfrastructureDeleted = g.Add(flow.Task{
			Name:         "Waiting until shoot infrastructure has been destroyed",
			Fn:           botanist.Shoot.Components.Extensions.Infrastructure.WaitCleanup,
			Dependencies: flow.NewTaskIDs(destroyInfrastructure),
		})
		destroyExternalDomainDNSRecord = g.Add(flow.Task{
			Name:         "Destroying external domain DNS record",
			Fn:           flow.TaskFn(botanist.DestroyExternalDNSResources),
			Dependencies: flow.NewTaskIDs(syncPointCleaned, deleteKubeAPIServer),
		})
		deleteGrafana = g.Add(flow.Task{
			Name:         "Deleting Grafana in Seed",
			Fn:           flow.TaskFn(botanist.DeleteGrafana).RetryUntilTimeout(defaultInterval, defaultTimeout),
			Dependencies: flow.NewTaskIDs(waitUntilInfrastructureDeleted),
		})

		syncPoint = flow.NewTaskIDs(
			deleteSeedMonitoring,
			deleteGrafana,
			deleteKubeAPIServer,
			waitUntilControlPlaneDeleted,
			waitUntilControlPlaneExposureDeleted,
			destroyIngressDomainDNSRecord,
			destroyExternalDomainDNSRecord,
			waitUntilInfrastructureDeleted,
		)

		destroyInternalDomainDNSRecord = g.Add(flow.Task{
			Name:         "Destroying internal domain DNS record",
			Fn:           flow.TaskFn(botanist.DestroyInternalDNSResources),
			Dependencies: flow.NewTaskIDs(syncPoint),
		})
		destroyOwnerDomainDNSRecord = g.Add(flow.Task{
			Name:         "Destroying owner domain DNS record",
			Fn:           flow.TaskFn(botanist.DestroyOwnerDNSResources),
			Dependencies: flow.NewTaskIDs(syncPoint),
		})
		deleteDNSProviders = g.Add(flow.Task{
			Name:         "Deleting additional DNS providers",
			Fn:           flow.TaskFn(botanist.DeleteDNSProviders),
			Dependencies: flow.NewTaskIDs(destroyInternalDomainDNSRecord, destroyOwnerDomainDNSRecord),
		})
		destroyReferencedResources = g.Add(flow.Task{
			Name:         "Deleting referenced resources",
			Fn:           flow.TaskFn(botanist.DestroyReferencedResources).RetryUntilTimeout(defaultInterval, defaultTimeout),
			Dependencies: flow.NewTaskIDs(syncPoint, deleteDNSProviders),
		})
		deleteNamespace = g.Add(flow.Task{
			Name:         "Deleting shoot namespace in Seed",
			Fn:           flow.TaskFn(botanist.DeleteSeedNamespace).RetryUntilTimeout(defaultInterval, defaultTimeout),
			Dependencies: flow.NewTaskIDs(syncPoint, deleteDNSProviders, destroyReferencedResources),
		})
		_ = g.Add(flow.Task{
			Name:         "Waiting until shoot namespace in Seed has been deleted",
			Fn:           botanist.WaitUntilSeedNamespaceDeleted,
			Dependencies: flow.NewTaskIDs(deleteNamespace),
		})
		_ = g.Add(flow.Task{
			Name:         "Deleting Shoot State",
			Fn:           botanist.DeleteShootState,
			Dependencies: flow.NewTaskIDs(deleteNamespace),
		})
		f = g.Compile()
	)

	if err := f.Run(flow.Opts{
		Logger:           o.Logger,
		ProgressReporter: c.newProgressReporter(o.ReportShootProgress),
		ErrorCleaner:     o.CleanShootTaskErrorAndUpdateStatusLabel,
		ErrorContext:     errorContext,
	}); err != nil {
		o.Logger.Errorf("Error deleting Shoot %q: %+v", o.Shoot.Info.Name, err)
		return gardencorev1beta1helper.NewWrappedLastErrors(gardencorev1beta1helper.FormatLastErrDescription(err), flow.Errors(err))
	}

	// ensure that shoot client is invalidated after it has been deleted
	if err := o.ClientMap.InvalidateClient(keys.ForShoot(o.Shoot.Info)); err != nil {
		err = fmt.Errorf("failed to invalidate shoot client: %w", err)
		return gardencorev1beta1helper.NewWrappedLastErrors(gardencorev1beta1helper.FormatLastErrDescription(err), err)
	}

	o.Logger.Infof("Successfully deleted Shoot %q", o.Shoot.Info.Name)
	return nil
}

func (c *Controller) removeFinalizerFrom(ctx context.Context, gardenClient kubernetes.Interface, shoot *gardencorev1beta1.Shoot) error {
	if err := c.patchShootStatusOperationSuccess(ctx, gardenClient.Client(), nil, shoot, "", nil, gardencorev1beta1.LastOperationTypeDelete); err != nil {
		return err
	}

	if err := controllerutils.PatchRemoveFinalizers(ctx, gardenClient.Client(), shoot, gardencorev1beta1.GardenerName); err != nil {
		return fmt.Errorf("could not remove finalizer from Shoot: %s", err.Error())
	}

	// Wait until the above modifications are reflected in the cache to prevent unwanted reconcile
	// operations (sometimes the cache is not synced fast enough).
	return retryutils.UntilTimeout(ctx, time.Second, 30*time.Second, func(context.Context) (done bool, err error) {
		// TODO(timebertt): switch to c-r cache here, once shoot controller uses c-r informers
		shoot, err := c.shootLister.Shoots(shoot.Namespace).Get(shoot.Name)
		if apierrors.IsNotFound(err) {
			return retryutils.Ok()
		}
		if err != nil {
			return retryutils.SevereError(err)
		}
		lastOperation := shoot.Status.LastOperation
		if !sets.NewString(shoot.Finalizers...).Has(gardencorev1beta1.GardenerName) && lastOperation != nil && lastOperation.Type == gardencorev1beta1.LastOperationTypeDelete && lastOperation.State == gardencorev1beta1.LastOperationStateSucceeded {
			return retryutils.Ok()
		}
		return retryutils.MinorError(fmt.Errorf("shoot still has finalizer %s", gardencorev1beta1.GardenerName))
	})
}

func needsControlPlaneDeployment(ctx context.Context, o *operation.Operation, kubeAPIServerDeploymentFound bool, infrastructure *extensionsv1alpha1.Infrastructure) (bool, error) {
	var (
		namespace = o.Shoot.SeedNamespace
		name      = o.Shoot.Info.Name
	)

	// If the `ControlPlane` resource and the kube-apiserver deployment do no longer exist then we don't want to re-deploy it.
	// The reason for the second condition is that some providers inject a cloud-provider-config into the kube-apiserver deployment
	// which is needed for it to run.
	exists, markedForDeletion, err := extensionResourceStillExists(ctx, o.K8sSeedClient.APIReader(), &extensionsv1alpha1.ControlPlane{}, namespace, name)
	if err != nil {
		return false, err
	}
	// treat `ControlPlane` in deletion as if it is already gone. If it is marked for deletion, we also shouldn't wait
	// for it to be reconciled, as it can potentially block the whole deletion flow (deletion depends on other control
	// plane components like kcm and grm) which are scaled up later in the flow
	if (!exists || markedForDeletion) && !kubeAPIServerDeploymentFound {
		return false, nil
	}

	// The infrastructure resource has not been found, no need to redeploy the control plane
	if infrastructure == nil {
		return false, nil
	}

	if providerStatus := infrastructure.Status.ProviderStatus; providerStatus != nil {
		// The infrastructure resource has been found with a non-nil provider status, so redeploy the control plane
		return true, nil
	}

	// The infrastructure resource has been found, but its provider status is nil
	// In this case the control plane could not have been created at all, so no need to redeploy it
	return false, nil
}

func extensionResourceStillExists(ctx context.Context, reader client.Reader, obj client.Object, namespace, name string) (bool, bool, error) {
	if err := reader.Get(ctx, kutil.Key(namespace, name), obj); err != nil {
		if apierrors.IsNotFound(err) {
			return false, false, nil
		}
		return false, false, err
	}
	return true, obj.GetDeletionTimestamp() != nil, nil
}
