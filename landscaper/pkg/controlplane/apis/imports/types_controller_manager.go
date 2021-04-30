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

package imports

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// GardenerControllerManager contains configurations of the Gardener Controller Manager
type GardenerControllerManager struct {
	// DeploymentConfiguration contains optional configurations for
	// the deployment of the Gardener Controller Manager
	DeploymentConfiguration *ControllerManagerDeploymentConfiguration
	// ComponentConfiguration contains the component configuration for the Gardener Controller Manager
	ComponentConfiguration ControllerManagerComponentConfiguration
}

// ControllerManagerDeploymentConfiguration contains certain configurations for the deployment
// of the Gardener Controller Manager
type ControllerManagerDeploymentConfiguration struct {
	// CommonDeploymentConfiguration contains common deployment configurations
	// Defaults:
	//   Resources: Requests (CPU: 100m, memory 100Mi), Limits (CPU: 750m, memory: 512Mi)
	CommonDeploymentConfiguration
	// AdditionalVolumes is the list of additional volumes that should be mounted.
	AdditionalVolumes []corev1.Volume
	// AdditionalVolumeMounts is the list of additional pod volumes to mount into the Gardener Controller Manager container's filesystem.
	AdditionalVolumeMounts []corev1.VolumeMount
	// Env is the list of environment variables to set in the Gardener Controller Manager.
	Env []corev1.EnvVar
}

// ControllerManagerComponentConfiguration contains the component configuration for the Gardener controller manager
type ControllerManagerComponentConfiguration struct {
	// TLS configures the HTTPS server of the Gardener Controller Manager
	// uses http for /healthz endpoint, optionally serves HTTPS for metrics
	TLS TLSServer
	// ComponentConfiguration specifies values for the Gardener Controller Manager component configuration
	// Please see example/20-componentconfig-gardener-controller-manager.yaml for what
	// can be configured here
	ComponentConfiguration runtime.Object
}
