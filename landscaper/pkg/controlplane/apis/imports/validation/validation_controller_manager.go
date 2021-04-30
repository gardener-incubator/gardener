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

package validation

import (
	"fmt"

	"github.com/gardener/gardener/landscaper/pkg/controlplane/apis/imports"
	apisconfig "github.com/gardener/gardener/pkg/controllermanager/apis/config"
	confighelper "github.com/gardener/gardener/pkg/controllermanager/apis/config/helper"
	configvalidation "github.com/gardener/gardener/pkg/controllermanager/apis/config/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

// ValidateControllerManager validates the configuration of the Gardener Controller Manager
func ValidateControllerManager(config imports.GardenerControllerManager, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	if config.DeploymentConfiguration != nil {
		allErrs = append(allErrs, ValidateCommonDeployment(config.DeploymentConfiguration.CommonDeploymentConfiguration, fldPath.Child("deploymentConfiguration"))...)
	}
	return append(allErrs, ValidateControllerManagerComponentConfiguration(config.ComponentConfiguration, fldPath.Child("componentConfiguration"))...)
}

// ValidateControllerManagerComponentConfiguration validates the component configuration of the Gardener Controller Manager
func ValidateControllerManagerComponentConfiguration(config imports.ControllerManagerComponentConfiguration, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	allErrs = append(allErrs, ValidateCommonTLSServer(config.TLS, fldPath.Child("tls"))...)

	// Convert the Gardener controller config to an internal version
	componentConfig, err := confighelper.ConvertControllerManagerConfiguration(config.ComponentConfiguration)
	if err != nil {
		allErrs = append(allErrs, field.Invalid(fldPath, config.ComponentConfiguration, fmt.Sprintf("could not convert to Gardener controller manager configuration: %v", err)))
		return allErrs
	}

	allErrs = append(allErrs, ValidateControllerManagerConfiguration(componentConfig, fldPath)...)

	return allErrs
}

// ValidateControllerManagerConfiguration validates the Gardener Controller Manager configuration
func ValidateControllerManagerConfiguration(config *apisconfig.ControllerManagerConfiguration, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	if len(config.GardenClientConnection.Kubeconfig) > 0 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("gardenClientConnection").Child("kubeconfig"), config.GardenClientConnection.Kubeconfig, "The path to the kubeconfig for the Garden cluster in the Gardener Admission Controller must not be set. Instead the provided runtime cluster or virtual garden cluster kubeconfig will be used."))
	}

	if len(config.Server.HTTPS.TLS.ServerCertPath) > 0 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("server").Child("https").Child("tls").Child("serverCertPath"), config.Server.HTTPS.TLS.ServerCertPath, "The path to the TLS serving certificate of the Gardener Controller Manager must not be set. Instead, directly provide the certificates via the landscaper imports field gardenerControllerManager.componentConfiguration.tls.certificate."))
	}

	if len(config.Server.HTTPS.TLS.ServerKeyPath) > 0 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("server").Child("https").Child("tls").Child("serverKeyPath"), config.Server.HTTPS.TLS.ServerKeyPath, "The path to the TLS serving certificate of the Gardener Controller Manager must not be set. Instead, directly provide the certificates via the landscaper imports field gardenerControllerManager.componentConfiguration.tls.key."))
	}

	allErrs = append(allErrs, configvalidation.ValidateControllerManagerConfiguration(config)...)

	return allErrs
}
