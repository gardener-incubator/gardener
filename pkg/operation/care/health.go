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

package care

import (
	"context"
	"fmt"
	"time"

	"github.com/gardener/gardener/pkg/api/extensions"
	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	gardencorev1beta1helper "github.com/gardener/gardener/pkg/apis/core/v1beta1/helper"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/gardener/gardener/pkg/client/kubernetes"
	"github.com/gardener/gardener/pkg/features"
	gardenletfeatures "github.com/gardener/gardener/pkg/gardenlet/features"
	"github.com/gardener/gardener/pkg/logger"
	"github.com/gardener/gardener/pkg/operation"
	"github.com/gardener/gardener/pkg/operation/botanist/systemcomponents/metricsserver"
	"github.com/gardener/gardener/pkg/operation/common"
	"github.com/gardener/gardener/pkg/operation/shoot"
	"github.com/gardener/gardener/pkg/utils/flow"
	kutil "github.com/gardener/gardener/pkg/utils/kubernetes"
	"github.com/gardener/gardener/pkg/utils/kubernetes/health"
	kuberneteshealth "github.com/gardener/gardener/pkg/utils/kubernetes/health"

	resourcesv1alpha1 "github.com/gardener/gardener-resource-manager/pkg/apis/resources/v1alpha1"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Health contains information needed to execute shoot health checks.
type Health struct {
	shoot      *shoot.Shoot
	seedClient kubernetes.Interface

	initializeShootClients ShootClientInit
	shootClient            kubernetes.Interface

	logger logrus.FieldLogger
}

type ShootClientInit func() (kubernetes.Interface, bool, error)

// NewHealth creates a new Health instance with the given parameters.
func NewHealth(op *operation.Operation, shootClientInit ShootClientInit) *Health {
	return &Health{
		shoot:                  op.Shoot,
		seedClient:             op.K8sSeedClient,
		initializeShootClients: shootClientInit,
		shootClient:            op.K8sShootClient,
		logger:                 op.Logger,
	}
}

// Check conducts the health checks on all the given conditions.
func (h *Health) Check(
	ctx context.Context,
	thresholdMappings map[gardencorev1beta1.ConditionType]time.Duration,
	healthCheckOutdatedThreshold *metav1.Duration,
	conditions []gardencorev1beta1.Condition,
) []gardencorev1beta1.Condition {
	updatedConditions := h.healthChecks(ctx, thresholdMappings, healthCheckOutdatedThreshold, conditions)
	lastOp := h.shoot.Info.Status.LastOperation
	lastErrors := h.shoot.Info.Status.LastErrors
	return PardonConditions(updatedConditions, lastOp, lastErrors)
}

// ExtensionCondition contains information about the extension type, name, namespace and the respective condition object.
type ExtensionCondition struct {
	Condition          gardencorev1beta1.Condition
	ExtensionType      string
	ExtensionName      string
	ExtensionNamespace string
}

func (h *Health) getAllExtensionConditions(ctx context.Context) ([]ExtensionCondition, []ExtensionCondition, []ExtensionCondition, error) {
	var (
		conditionsControlPlaneHealthy     []ExtensionCondition
		conditionsEveryNodeReady          []ExtensionCondition
		conditionsSystemComponentsHealthy []ExtensionCondition
	)

	for _, listObj := range []client.ObjectList{
		&extensionsv1alpha1.BackupEntryList{},
		&extensionsv1alpha1.ContainerRuntimeList{},
		&extensionsv1alpha1.ControlPlaneList{},
		&extensionsv1alpha1.ExtensionList{},
		&extensionsv1alpha1.InfrastructureList{},
		&extensionsv1alpha1.NetworkList{},
		&extensionsv1alpha1.OperatingSystemConfigList{},
		&extensionsv1alpha1.WorkerList{},
	} {
		listKind := listObj.GetObjectKind().GroupVersionKind().Kind
		if err := h.seedClient.Client().List(ctx, listObj, client.InNamespace(h.shoot.SeedNamespace)); err != nil {
			return nil, nil, nil, err
		}

		if err := meta.EachListItem(listObj, func(obj runtime.Object) error {
			acc, err := extensions.Accessor(obj)
			if err != nil {
				return err
			}

			kind := obj.GetObjectKind().GroupVersionKind().Kind
			name := acc.GetName()
			namespace := acc.GetNamespace()

			for _, condition := range acc.GetExtensionStatus().GetConditions() {
				switch condition.Type {
				case gardencorev1beta1.ShootControlPlaneHealthy:
					conditionsControlPlaneHealthy = append(conditionsControlPlaneHealthy, ExtensionCondition{condition, kind, name, namespace})
				case gardencorev1beta1.ShootEveryNodeReady:
					conditionsEveryNodeReady = append(conditionsEveryNodeReady, ExtensionCondition{condition, kind, name, namespace})
				case gardencorev1beta1.ShootSystemComponentsHealthy:
					conditionsSystemComponentsHealthy = append(conditionsSystemComponentsHealthy, ExtensionCondition{condition, kind, name, namespace})
				}
			}

			return nil
		}); err != nil {
			h.logger.Errorf("Error during evaluation of kind %q for extensions health check: %+v", listKind, err)
			return nil, nil, nil, err
		}
	}

	return conditionsControlPlaneHealthy, conditionsEveryNodeReady, conditionsSystemComponentsHealthy, nil
}

func (h *Health) healthChecks(
	ctx context.Context,
	thresholdMappings map[gardencorev1beta1.ConditionType]time.Duration,
	healthCheckOutdatedThreshold *metav1.Duration,
	conditions []gardencorev1beta1.Condition,
) []gardencorev1beta1.Condition {
	if h.shoot.HibernationEnabled || h.shoot.Info.Status.IsHibernated {
		return shootHibernatedConditions(conditions)
	}

	var apiserverAvailability, controlPlane, nodes, systemComponents gardencorev1beta1.Condition
	for _, cond := range conditions {
		switch cond.Type {
		case gardencorev1beta1.ShootAPIServerAvailable:
			apiserverAvailability = cond
		case gardencorev1beta1.ShootControlPlaneHealthy:
			controlPlane = cond
		case gardencorev1beta1.ShootEveryNodeReady:
			nodes = cond
		case gardencorev1beta1.ShootSystemComponentsHealthy:
			systemComponents = cond
		}
	}

	extensionConditionsControlPlaneHealthy, extensionConditionsEveryNodeReady, extensionConditionsSystemComponentsHealthy, err := h.getAllExtensionConditions(ctx)
	if err != nil {
		h.logger.Errorf("error getting extension conditions: %+v", err)
	}

	var (
		checker               = NewHealthChecker(thresholdMappings, healthCheckOutdatedThreshold, h.shoot.Info.Status.LastOperation, h.shoot.KubernetesVersion, h.shoot.GardenerVersion)
		seedDeploymentLister  = makeDeploymentLister(ctx, h.seedClient.Client(), h.shoot.SeedNamespace, controlPlaneMonitoringLoggingSelector)
		seedStatefulSetLister = makeStatefulSetLister(ctx, h.seedClient.Client(), h.shoot.SeedNamespace, controlPlaneMonitoringLoggingSelector)
		seedEtcdLister        = makeEtcdLister(ctx, h.seedClient.Client(), h.shoot.SeedNamespace)
		seedWorkerLister      = makeWorkerLister(ctx, h.seedClient.Client(), h.shoot.SeedNamespace)
	)

	shootClient, apiServerRunning, err := h.initializeShootClients()
	if err != nil || !apiServerRunning {
		// don't execute health checks if API server has already been deleted or has not been created yet
		message := shootControlPlaneNotRunningMessage(h.shoot.Info.Status.LastOperation)
		if err != nil {
			message = fmt.Sprintf("Could not initialize Shoot client for health check: %+v", err)
			h.logger.Error(message)
		}

		apiserverAvailability = checker.FailedCondition(apiserverAvailability, "APIServerDown", "Could not reach API server during client initialization.")
		nodes = gardencorev1beta1helper.UpdatedConditionUnknownErrorMessage(nodes, message)
		systemComponents = gardencorev1beta1helper.UpdatedConditionUnknownErrorMessage(systemComponents, message)

		newControlPlane, err := h.checkControlPlane(ctx, checker, controlPlane, seedDeploymentLister, seedStatefulSetLister, seedEtcdLister, seedWorkerLister, extensionConditionsControlPlaneHealthy)
		controlPlane = NewConditionOrError(controlPlane, newControlPlane, err)
		return []gardencorev1beta1.Condition{apiserverAvailability, controlPlane, nodes, systemComponents}
	}

	h.shootClient = shootClient

	_ = flow.Parallel(func(ctx context.Context) error {
		apiserverAvailability = h.checkAPIServerAvailability(ctx, checker, apiserverAvailability)
		return nil
	}, func(ctx context.Context) error {
		newControlPlane, err := h.checkControlPlane(ctx, checker, controlPlane, seedDeploymentLister, seedStatefulSetLister, seedEtcdLister, seedWorkerLister, extensionConditionsControlPlaneHealthy)
		controlPlane = NewConditionOrError(controlPlane, newControlPlane, err)
		return nil
	}, func(ctx context.Context) error {
		newNodes, err := h.checkClusterNodes(ctx, h.shootClient.Client(), checker, nodes, extensionConditionsEveryNodeReady)
		nodes = NewConditionOrError(nodes, newNodes, err)
		return nil
	}, func(ctx context.Context) error {
		newSystemComponents, err := h.checkSystemComponents(ctx, checker, systemComponents, extensionConditionsSystemComponentsHealthy)
		systemComponents = NewConditionOrError(systemComponents, newSystemComponents, err)
		return nil
	})(ctx)

	return []gardencorev1beta1.Condition{apiserverAvailability, controlPlane, nodes, systemComponents}
}

// checkAPIServerAvailability checks if the API server of a Shoot cluster is reachable and measure the response time.
func (h *Health) checkAPIServerAvailability(ctx context.Context, checker *HealthChecker, condition gardencorev1beta1.Condition) gardencorev1beta1.Condition {
	return health.CheckAPIServerAvailability(ctx, condition, h.shootClient.RESTClient(), func(conditionType, message string) gardencorev1beta1.Condition {
		return checker.FailedCondition(condition, conditionType, message)
	}, h.logger)
}

// checkControlPlane checks whether the control plane of the Shoot cluster is healthy.
func (h *Health) checkControlPlane(
	_ context.Context,
	checker *HealthChecker,
	condition gardencorev1beta1.Condition,
	seedDeploymentLister kutil.DeploymentLister,
	seedStatefulSetLister kutil.StatefulSetLister,
	seedEtcdLister kutil.EtcdLister,
	seedWorkerLister kutil.WorkerLister,
	extensionConditions []ExtensionCondition,
) (*gardencorev1beta1.Condition, error) {
	if exitCondition, err := checker.CheckControlPlane(h.shoot.Info, h.shoot.SeedNamespace, condition, seedDeploymentLister, seedEtcdLister, seedWorkerLister); err != nil || exitCondition != nil {
		return exitCondition, err
	}

	wantsAlertmanager := h.shoot.WantsAlertmanager
	if exitCondition, err := checker.CheckMonitoringControlPlane(h.shoot.SeedNamespace, h.shoot.Purpose == gardencorev1beta1.ShootPurposeTesting, wantsAlertmanager, condition, seedDeploymentLister, seedStatefulSetLister); err != nil || exitCondition != nil {
		return exitCondition, err
	}
	if gardenletfeatures.FeatureGate.Enabled(features.Logging) {
		if exitCondition, err := checker.CheckLoggingControlPlane(h.shoot.SeedNamespace, h.shoot.Purpose == gardencorev1beta1.ShootPurposeTesting, condition, seedStatefulSetLister); err != nil || exitCondition != nil {
			return exitCondition, err
		}
	}
	if exitCondition := checker.CheckExtensionCondition(condition, extensionConditions); exitCondition != nil {
		return exitCondition, nil
	}

	c := gardencorev1beta1helper.UpdatedCondition(condition, gardencorev1beta1.ConditionTrue, "ControlPlaneRunning", "All control plane components are healthy.")
	return &c, nil
}

// checkSystemComponents checks whether the system components of a Shoot are running.
func (h *Health) checkSystemComponents(
	ctx context.Context,
	checker *HealthChecker,
	condition gardencorev1beta1.Condition,
	extensionConditions []ExtensionCondition,
) (*gardencorev1beta1.Condition, error) {
	managedResources := managedResourcesShoot.List()
	if versionConstraintGreaterEqual113.Check(checker.gardenerVersion) {
		managedResources = append(managedResources, metricsserver.ManagedResourceName)
	}

	for _, name := range managedResources {
		mr := &resourcesv1alpha1.ManagedResource{}
		if err := h.seedClient.Client().Get(ctx, kutil.Key(h.shoot.SeedNamespace, name), mr); err != nil {
			return nil, err
		}

		if exitCondition := checker.CheckManagedResource(condition, mr); exitCondition != nil {
			return exitCondition, nil
		}
	}

	if exitCondition := checker.CheckExtensionCondition(condition, extensionConditions); exitCondition != nil {
		return exitCondition, nil
	}

	podsList := &corev1.PodList{}
	if err := h.shootClient.Client().List(ctx, podsList, client.InNamespace(metav1.NamespaceSystem), client.MatchingLabels{"type": "tunnel"}); err != nil {
		return nil, err
	}
	if len(podsList.Items) == 0 {
		c := checker.FailedCondition(condition, "NoTunnelDeployed", "no tunnels are currently deployed to perform health-check on")
		return &c, nil
	}

	tunnelName := common.VPNTunnel
	if podsList.Items[0].Labels["app"] == common.KonnectivityTunnel {
		tunnelName = common.KonnectivityTunnel
	}

	if established, err := kuberneteshealth.CheckTunnelConnection(ctx, h.shootClient, logrus.NewEntry(logger.NewNopLogger()), tunnelName); err != nil || !established {
		msg := "Tunnel connection has not been established"
		if err != nil {
			msg += fmt.Sprintf(" (%+v)", err)
		}
		c := checker.FailedCondition(condition, "TunnelConnectionBroken", msg)
		return &c, nil
	}

	c := gardencorev1beta1helper.UpdatedCondition(condition, gardencorev1beta1.ConditionTrue, "SystemComponentsRunning", "All system components are healthy.")
	return &c, nil
}

// checkClusterNodes checks whether every node registered at the Shoot cluster is in "Ready" state, that
// as many nodes are registered as desired, and that every machine is running.
func (h *Health) checkClusterNodes(
	ctx context.Context,
	shootClient client.Client,
	checker *HealthChecker,
	condition gardencorev1beta1.Condition,
	extensionConditions []ExtensionCondition,
) (*gardencorev1beta1.Condition, error) {
	if exitCondition, err := checker.CheckClusterNodes(ctx, shootClient, h.shoot.Info.Spec.Provider.Workers, condition); err != nil || exitCondition != nil {
		return exitCondition, err
	}
	if exitCondition := checker.CheckExtensionCondition(condition, extensionConditions); exitCondition != nil {
		return exitCondition, nil
	}

	c := gardencorev1beta1helper.UpdatedCondition(condition, gardencorev1beta1.ConditionTrue, "EveryNodeReady", "Every node registered to the cluster is ready.")
	return &c, nil
}
