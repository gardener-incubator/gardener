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

	"github.com/gardener/gardener/landscaper/pkg/gardenlet/apis/imports"
	gardencore "github.com/gardener/gardener/pkg/apis/core"
	"github.com/gardener/gardener/pkg/apis/seedmanagement"
	seedmanagementvalidation "github.com/gardener/gardener/pkg/apis/seedmanagement/validation"
	gardenletconfig "github.com/gardener/gardener/pkg/gardenlet/apis/config"
	confighelper "github.com/gardener/gardener/pkg/gardenlet/apis/config/helper"
	gardenletvalidation "github.com/gardener/gardener/pkg/gardenlet/apis/config/validation"

	"k8s.io/apimachinery/pkg/util/validation/field"
)

// ValidateLandscaperImports validates an imports object.
func ValidateLandscaperImports(imports *imports.Imports) field.ErrorList {
	allErrs := field.ErrorList{}

	if imports.GardenCluster.Spec.Configuration.RawMessage == nil {
		return append(allErrs, field.Required(field.NewPath("gardenCluster"), "the garden cluster kubeconfig has to be provided."))
	}

	if imports.SeedCluster.Spec.Configuration.RawMessage == nil {
		return append(allErrs, field.Required(field.NewPath("seedCluster"), "the seed cluster kubeconfig has to be provided."))
	}

	if imports.DeploymentConfiguration != nil {
		allErrs = append(allErrs, validateGardenletDeployment(imports.DeploymentConfiguration, field.NewPath("deploymentConfiguration"))...)
	}

	componentConfigurationPath := field.NewPath("componentConfiguration")

	// Convert gardenlet config to an internal version
	gardenletConfig, err := confighelper.ConvertGardenletConfiguration(imports.ComponentConfiguration)
	if err != nil {
		allErrs = append(allErrs, field.Invalid(componentConfigurationPath, imports.ComponentConfiguration, fmt.Sprintf("could not convert to gardenlet configuration: %v", err)))
		return allErrs
	}

	allErrs = append(allErrs, validateGardenletConfiguration(gardenletConfig, componentConfigurationPath)...)

	// if only the Seed specifies a backup configuration (componentConfiguration.SeedConfig.Spec.Backup) but not imports.SeedBackupCredentials
	// then we assume that the reference backup secret already exists in the Garden cluster and does not have to
	// be deployed by the landscaper. Hence, nothing to validate.
	if imports.SeedBackupCredentials != nil {
		allErrs = validateBackup(gardenletConfig.SeedConfig.Spec.Backup, componentConfigurationPath.Child("seedConfig.spec.backup"))
	}

	return allErrs
}

// validateGardenletConfiguration validates the Gardenlet configuration using the validation from the
// Gardenlet package
// in addition, validates settings specific to the Gardenlet landscaper
func validateGardenletConfiguration(gardenletConfig *gardenletconfig.GardenletConfiguration, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	if gardenletConfig.SeedSelector != nil {
		allErrs = append(allErrs, field.Forbidden(fldPath.Child("seedSelector"), "seed selector is forbidden, provide a seedConfig instead."))
	}

	if gardenletConfig.SeedConfig == nil {
		return append(allErrs, field.Required(fldPath.Child("seedConfig"), "the seed configuration has to be provided. This is used to automatically register the seed."))
	}

	allErrs = append(allErrs, gardenletvalidation.ValidateGardenletConfiguration(gardenletConfig, fldPath, false)...)

	if gardenletConfig.GardenClientConnection != nil && len(gardenletConfig.GardenClientConnection.Kubeconfig) > 0 {
		allErrs = append(allErrs, field.Forbidden(fldPath.Child("gardenClientConnection.kubeconfig"), "directly supplying a Garden kubeconfig and therefore not using TLS bootstrapping, is not supported."))
	}

	if gardenletConfig.SeedClientConnection != nil && gardenletConfig.SeedClientConnection.Kubeconfig != "" {
		allErrs = append(allErrs, field.Forbidden(fldPath.Child("seedClientConnection.kubeconfig"), "setting a Seed cluster kubeconfig is not supported. Instead, the landscaper creates a bootstrap kubeconfig with a bootstrap token as client credential for the Gardenlet."))
	}

	return allErrs
}

// validateBackup validates the Seed Backup configuration of the gardenlet landscaper imports
func validateBackup(componentConfigSeedBackup *gardencore.SeedBackup, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	if componentConfigSeedBackup == nil {
		return append(allErrs, field.Required(fldPath, "the Seed has to to have backup enabled when the Gardenlet landscaper is configured to deploy a backup secret via \"seedBackupCredentials\""))
	}

	if len(componentConfigSeedBackup.Provider) == 0 {
		allErrs = append(allErrs, field.Required(fldPath.Child("provider"), "seed backup provider must be defined when configuring backups"))
	}

	if len(componentConfigSeedBackup.SecretRef.Name) == 0 {
		allErrs = append(allErrs, field.Required(fldPath.Child("secretRef.Name"), "seed backup secretRef must be set when configuring backups"))
	}

	return allErrs
}

// validateGardenletDeployment validates the deployment configuration of the landscaper gardenlet imports
func validateGardenletDeployment(deployment *seedmanagement.GardenletDeployment, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	if deployment.Image != nil {
		allErrs = append(allErrs, field.Forbidden(fldPath.Child("image"), "directly setting the image field is not supported, instead set the image via the component descriptor"))
	}

	allErrs = append(allErrs, seedmanagementvalidation.ValidateGardenletDeployment(deployment, fldPath)...)
	return allErrs
}
