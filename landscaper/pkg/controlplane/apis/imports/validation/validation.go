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
	"github.com/gardener/gardener/landscaper/pkg/controlplane/apis/imports"
	corevalidation "github.com/gardener/gardener/pkg/apis/core/validation"
	apivalidation "k8s.io/apimachinery/pkg/api/validation"
	metav1validation "k8s.io/apimachinery/pkg/apis/meta/v1/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

// ValidateLandscaperImports validates an imports object.
func ValidateLandscaperImports(imports *imports.Imports) field.ErrorList {
	allErrs := field.ErrorList{}

	if len(imports.RuntimeCluster.Spec.Configuration.RawMessage) == 0 {
		allErrs = append(allErrs, field.Required(field.NewPath("gardenCluster"), "the runtime cluster kubeconfig has to be provided."))
	}

	if imports.VirtualGarden.Enabled && len(imports.VirtualGarden.Kubeconfig.Spec.Configuration.RawMessage) == 0 {
		allErrs = append(allErrs, field.Required(field.NewPath("seedCluster"), "the virtual Garden cluster kubeconfig has to be provided when the virtual Garden setup option is enabled."))
	}

	allErrs = append(allErrs, validateDNS(imports.InternalDomain, field.NewPath("internalDomain"))...)

	for i, dns := range imports.DefaultDomains {
		allErrs = append(allErrs, validateDNS(dns, field.NewPath("defaultDomain").Index(i))...)
	}

	for i, alerting := range imports.Alerting {
		allErrs = append(allErrs, validateAlerting(alerting, field.NewPath("alerting").Index(i))...)
	}

	allErrs = append(allErrs, ValidateAPIServer(imports.GardenerAPIServer, field.NewPath("gardenerApiserver"))...)
	allErrs = append(allErrs, ValidateControllerManager(imports.GardenerControllerManager, field.NewPath("gardenerControllerManager"))...)
	allErrs = append(allErrs, ValidateScheduler(imports.GardenerScheduler, field.NewPath("gardenerScheduler"))...)

	if imports.GardenerAdmissionController != nil {
		allErrs = append(allErrs, ValidateAdmissionController(*imports.GardenerAdmissionController, field.NewPath("gardenerAdmissionController"))...)
	}

	return allErrs
}

func validateDNS(dns imports.DNS, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	if len(dns.Domain) == 0 {
		allErrs = append(allErrs, field.Required(fldPath.Child("domain"), "DNS domain must not be empty."))
	}
	if len(dns.Provider) == 0 {
		allErrs = append(allErrs, field.Required(fldPath.Child("provider"), "DNS provider must not be empty."))
	}
	if len(dns.Credentials) == 0 {
		allErrs = append(allErrs, field.Required(fldPath.Child("credentials"), "DNS credentials must not be empty."))
	}
	return allErrs
}

func validateAlerting(alerting imports.Alerting, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	switch alerting.AuthType {
	case "none":
		if alerting.Url == nil || len(*alerting.Url) == 0 {
			allErrs = append(allErrs, field.Required(fldPath.Child("url"), "The alerting url is required for alerting with basic authentication"))
		}
	case "smtp":
		if alerting.ToEmailAddress == nil || len(*alerting.ToEmailAddress) == 0 {
			allErrs = append(allErrs, field.Required(fldPath.Child("toEmailAddress"), "Using smtp for Alerting requires to set the email address for alerting"))
		}
		if alerting.FromEmailAddress == nil || len(*alerting.FromEmailAddress) == 0 {
			allErrs = append(allErrs, field.Required(fldPath.Child("fromEmailAddress"), "Using smtp for Alerting requires to set the email address for alerting"))
		}
		if alerting.Smarthost == nil || len(*alerting.Smarthost) == 0 {
			allErrs = append(allErrs, field.Required(fldPath.Child("smarthost"), "Using smtp for Alerting requires to set the smarthost"))
		}
		if alerting.AuthUsername == nil || len(*alerting.AuthUsername) == 0 {
			allErrs = append(allErrs, field.Required(fldPath.Child("authUsername"), "Using smtp for Alerting requires to set the username used for authentication"))
		}
		if alerting.AuthIdentity == nil || len(*alerting.AuthIdentity) == 0 {
			allErrs = append(allErrs, field.Required(fldPath.Child("authIdentity"), "Using smtp for Alerting requires to set the identity used for authentication"))
		}
		if alerting.AuthPassword == nil || len(*alerting.AuthPassword) == 0 {
			allErrs = append(allErrs, field.Required(fldPath.Child("authPassword"), "Using smtp for Alerting requires to set the password for authentication"))
		}
	case "basic":
		if alerting.Url == nil || len(*alerting.Url) == 0 {
			allErrs = append(allErrs, field.Required(fldPath.Child("url"), "Using basic authentication for Alerting requires to set the url"))
		}
		if alerting.Username == nil || len(*alerting.Username) == 0 {
			allErrs = append(allErrs, field.Required(fldPath.Child("username"), "Using basic authentication for Alerting requires to set the username"))
		}
		if alerting.Password == nil || len(*alerting.Password) == 0 {
			allErrs = append(allErrs, field.Required(fldPath.Child("password"), "Using basic authentication for Alerting requires to set the password"))
		}
	case "certificate":
		if alerting.Url == nil || len(*alerting.Url) == 0 {
			allErrs = append(allErrs, field.Required(fldPath.Child("url"), "Using certificate authentication for Alerting requires to set the url"))
		}
		if alerting.CaCert == nil || len(*alerting.CaCert) == 0 {
			allErrs = append(allErrs, field.Required(fldPath.Child("caCert"), "Using certificate authentication for Alerting requires to set the CA certificate"))
		}
		if alerting.TlsCert == nil || len(*alerting.TlsCert) == 0 {
			allErrs = append(allErrs, field.Required(fldPath.Child("tlsCert"), "Using certificate authentication for Alerting requires to set the Tls certificate"))
		}
		if alerting.TlsKey == nil || len(*alerting.TlsKey) == 0 {
			allErrs = append(allErrs, field.Required(fldPath.Child("tlsKey"), "Using certificate authentication for Alerting requires to set the Tls key"))
		}
	default:
		allErrs = append(allErrs, field.Invalid(fldPath.Child("AuthType"), alerting.AuthType, "The authentication type for alerting has to be one of [smtp, none, basic, certificate]"))
	}
	return allErrs
}

// ValidateCommonDeployment validates the deployment configuration
func ValidateCommonDeployment(deployment imports.CommonDeploymentConfiguration, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	if deployment.ReplicaCount != nil {
		allErrs = append(allErrs, apivalidation.ValidateNonnegativeField(int64(*deployment.ReplicaCount), fldPath.Child("replicaCount"))...)
	}
	if deployment.ServiceAccountName != nil {
		for _, msg := range apivalidation.ValidateServiceAccountName(*deployment.ServiceAccountName, false) {
			allErrs = append(allErrs, field.Invalid(fldPath.Child("serviceAccountName"), *deployment.ServiceAccountName, msg))
		}
	}

	if deployment.Resources != nil {
		fldPathResources := fldPath.Child("resources")
		for key, requests := range deployment.Resources.Requests {
			allErrs = append(allErrs, corevalidation.ValidateResourceQuantityValue(string(key), requests, fldPathResources.Child("requests"))...)
		}
		for key, limits := range deployment.Resources.Limits {
			allErrs = append(allErrs, corevalidation.ValidateResourceQuantityValue(string(key), limits, fldPathResources.Child("limits"))...)
		}
	}

	allErrs = append(allErrs, metav1validation.ValidateLabels(deployment.PodLabels, fldPath.Child("podLabels"))...)
	allErrs = append(allErrs, apivalidation.ValidateAnnotations(deployment.PodAnnotations, fldPath.Child("podAnnotations"))...)

	return allErrs
}

// ValidateCommonTLSServer validates TLS server configuration
func ValidateCommonTLSServer(config imports.TLSServer, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	if len(config.Certificate) == 0 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("crt"), config.Certificate, "the TLS certificate must be set"))
	}

	if len(config.Key) == 0 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("key"), config.Key, "the TLS key must be set"))
	}

	return allErrs
}
