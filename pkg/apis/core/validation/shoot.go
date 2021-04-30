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

package validation

import (
	"fmt"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gardener/gardener/pkg/apis/core"
	"github.com/gardener/gardener/pkg/apis/core/helper"
	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	"github.com/gardener/gardener/pkg/features"
	"github.com/gardener/gardener/pkg/utils"
	gutil "github.com/gardener/gardener/pkg/utils/gardener"
	cidrvalidation "github.com/gardener/gardener/pkg/utils/validation/cidr"
	versionutils "github.com/gardener/gardener/pkg/utils/version"

	"github.com/Masterminds/semver"
	"github.com/robfig/cron"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/resource"
	apivalidation "k8s.io/apimachinery/pkg/api/validation"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	metav1validation "k8s.io/apimachinery/pkg/apis/meta/v1/validation"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
)

var (
	availableProxyMode = sets.NewString(
		string(core.ProxyModeIPTables),
		string(core.ProxyModeIPVS),
	)
	availableKubernetesDashboardAuthenticationModes = sets.NewString(
		core.KubernetesDashboardAuthModeBasic,
		core.KubernetesDashboardAuthModeToken,
	)
	availableNginxIngressExternalTrafficPolicies = sets.NewString(
		string(corev1.ServiceExternalTrafficPolicyTypeCluster),
		string(corev1.ServiceExternalTrafficPolicyTypeLocal),
	)
	availableShootPurposes = sets.NewString(
		string(core.ShootPurposeEvaluation),
		string(core.ShootPurposeTesting),
		string(core.ShootPurposeDevelopment),
		string(core.ShootPurposeProduction),
	)
	avaliableWorkerCRINames = sets.NewString(
		string(core.CRINameContainerD),
	)
)

// ValidateShoot validates a Shoot object.
func ValidateShoot(shoot *core.Shoot) field.ErrorList {
	allErrs := field.ErrorList{}

	allErrs = append(allErrs, apivalidation.ValidateObjectMeta(&shoot.ObjectMeta, true, apivalidation.NameIsDNSLabel, field.NewPath("metadata"))...)
	allErrs = append(allErrs, validateNameConsecutiveHyphens(shoot.Name, field.NewPath("metadata", "name"))...)
	allErrs = append(allErrs, ValidateShootSpec(shoot.ObjectMeta, &shoot.Spec, field.NewPath("spec"), false)...)

	return allErrs
}

// ValidateShootUpdate validates a Shoot object before an update.
func ValidateShootUpdate(newShoot, oldShoot *core.Shoot) field.ErrorList {
	allErrs := field.ErrorList{}

	allErrs = append(allErrs, apivalidation.ValidateObjectMetaUpdate(&newShoot.ObjectMeta, &oldShoot.ObjectMeta, field.NewPath("metadata"))...)
	allErrs = append(allErrs, ValidateShootSpecUpdate(&newShoot.Spec, &oldShoot.Spec, newShoot.ObjectMeta, field.NewPath("spec"))...)
	allErrs = append(allErrs, ValidateShoot(newShoot)...)

	return allErrs
}

// ValidateShootTemplate validates a ShootTemplate.
func ValidateShootTemplate(shootTemplate *core.ShootTemplate, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	allErrs = append(allErrs, metav1validation.ValidateLabels(shootTemplate.Labels, fldPath.Child("metadata", "labels"))...)
	allErrs = append(allErrs, apivalidation.ValidateAnnotations(shootTemplate.Annotations, fldPath.Child("metadata", "annotations"))...)
	allErrs = append(allErrs, ValidateShootSpec(shootTemplate.ObjectMeta, &shootTemplate.Spec, fldPath.Child("spec"), true)...)

	return allErrs
}

// ValidateShootTemplateUpdate validates a ShootTemplate before an update.
func ValidateShootTemplateUpdate(newShootTemplate, oldShootTemplate *core.ShootTemplate, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	allErrs = append(allErrs, ValidateShootSpecUpdate(&newShootTemplate.Spec, &oldShootTemplate.Spec, newShootTemplate.ObjectMeta, fldPath.Child("spec"))...)

	return allErrs
}

// ValidateShootSpec validates the specification of a Shoot object.
func ValidateShootSpec(meta metav1.ObjectMeta, spec *core.ShootSpec, fldPath *field.Path, inTemplate bool) field.ErrorList {
	allErrs := field.ErrorList{}

	allErrs = append(allErrs, validateAddons(spec.Addons, spec.Kubernetes.KubeAPIServer, fldPath.Child("addons"))...)
	allErrs = append(allErrs, validateDNS(spec.DNS, fldPath.Child("dns"))...)
	allErrs = append(allErrs, validateExtensions(spec.Extensions, fldPath.Child("extensions"))...)
	allErrs = append(allErrs, validateResources(spec.Resources, fldPath.Child("resources"))...)
	allErrs = append(allErrs, validateKubernetes(spec.Kubernetes, fldPath.Child("kubernetes"))...)
	allErrs = append(allErrs, validateNetworking(spec.Networking, fldPath.Child("networking"))...)
	allErrs = append(allErrs, validateMaintenance(spec.Maintenance, fldPath.Child("maintenance"))...)
	allErrs = append(allErrs, validateMonitoring(spec.Monitoring, fldPath.Child("monitoring"))...)
	allErrs = append(allErrs, ValidateHibernation(spec.Hibernation, fldPath.Child("hibernation"))...)
	allErrs = append(allErrs, validateProvider(spec.Provider, spec.Kubernetes, fldPath.Child("provider"), inTemplate)...)

	if len(spec.Region) == 0 {
		allErrs = append(allErrs, field.Required(fldPath.Child("region"), "must specify a region"))
	}
	if len(spec.CloudProfileName) == 0 {
		allErrs = append(allErrs, field.Required(fldPath.Child("cloudProfileName"), "must specify a cloud profile"))
	}
	if len(spec.SecretBindingName) == 0 {
		allErrs = append(allErrs, field.Required(fldPath.Child("secretBindingName"), "must specify a name"))
	}
	if spec.SeedName != nil && len(*spec.SeedName) == 0 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("seedName"), spec.SeedName, "seed name must not be empty when providing the key"))
	}
	if spec.SeedSelector != nil {
		allErrs = append(allErrs, metav1validation.ValidateLabelSelector(spec.SeedSelector.LabelSelector, fldPath.Child("seedSelector"))...)
	}
	if purpose := spec.Purpose; purpose != nil {
		allowedShootPurposes := availableShootPurposes
		if meta.Namespace == v1beta1constants.GardenNamespace || inTemplate {
			allowedShootPurposes = sets.NewString(append(availableShootPurposes.List(), string(core.ShootPurposeInfrastructure))...)
		}

		if !allowedShootPurposes.Has(string(*purpose)) {
			allErrs = append(allErrs, field.NotSupported(fldPath.Child("purpose"), *purpose, allowedShootPurposes.List()))
		}
	}
	allErrs = append(allErrs, ValidateTolerations(spec.Tolerations, fldPath.Child("tolerations"))...)

	return allErrs
}

// ValidateShootSpecUpdate validates the specification of a Shoot object.
func ValidateShootSpecUpdate(newSpec, oldSpec *core.ShootSpec, newObjectMeta metav1.ObjectMeta, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	if newObjectMeta.DeletionTimestamp != nil && !apiequality.Semantic.DeepEqual(newSpec, oldSpec) {
		allErrs = append(allErrs, apivalidation.ValidateImmutableField(newSpec, oldSpec, fldPath)...)
		return allErrs
	}

	allErrs = append(allErrs, apivalidation.ValidateImmutableField(newSpec.Region, oldSpec.Region, fldPath.Child("region"))...)
	allErrs = append(allErrs, apivalidation.ValidateImmutableField(newSpec.CloudProfileName, oldSpec.CloudProfileName, fldPath.Child("cloudProfileName"))...)
	allErrs = append(allErrs, apivalidation.ValidateImmutableField(newSpec.SecretBindingName, oldSpec.SecretBindingName, fldPath.Child("secretBindingName"))...)
	if oldSpec.SeedName != nil && !utilfeature.DefaultFeatureGate.Enabled(features.SeedChange) {
		// allow initial seed assignment
		allErrs = append(allErrs, apivalidation.ValidateImmutableField(newSpec.SeedName, oldSpec.SeedName, fldPath.Child("seedName"))...)
	}

	seedGotAssigned := oldSpec.SeedName == nil && newSpec.SeedName != nil

	allErrs = append(allErrs, validateAddonsUpdate(newSpec.Addons, oldSpec.Addons, metav1.HasAnnotation(newObjectMeta, v1beta1constants.AnnotationShootUseAsSeed), fldPath.Child("addons"))...)
	allErrs = append(allErrs, validateDNSUpdate(newSpec.DNS, oldSpec.DNS, seedGotAssigned, fldPath.Child("dns"))...)
	allErrs = append(allErrs, validateKubernetesVersionUpdate(newSpec.Kubernetes.Version, oldSpec.Kubernetes.Version, fldPath.Child("kubernetes", "version"))...)
	allErrs = append(allErrs, validateKubeProxyModeUpdate(newSpec.Kubernetes.KubeProxy, oldSpec.Kubernetes.KubeProxy, newSpec.Kubernetes.Version, fldPath.Child("kubernetes", "kubeProxy"))...)
	allErrs = append(allErrs, validateKubeControllerManagerUpdate(newSpec.Kubernetes.KubeControllerManager, oldSpec.Kubernetes.KubeControllerManager, fldPath.Child("kubernetes", "kubeControllerManager"))...)
	allErrs = append(allErrs, ValidateProviderUpdate(&newSpec.Provider, &oldSpec.Provider, fldPath.Child("provider"))...)

	allErrs = append(allErrs, apivalidation.ValidateImmutableField(newSpec.Networking.Type, oldSpec.Networking.Type, fldPath.Child("networking", "type"))...)
	if oldSpec.Networking.Pods != nil {
		allErrs = append(allErrs, apivalidation.ValidateImmutableField(newSpec.Networking.Pods, oldSpec.Networking.Pods, fldPath.Child("networking", "pods"))...)
	}
	if oldSpec.Networking.Services != nil {
		allErrs = append(allErrs, apivalidation.ValidateImmutableField(newSpec.Networking.Services, oldSpec.Networking.Services, fldPath.Child("networking", "services"))...)
	}
	if oldSpec.Networking.Nodes != nil {
		allErrs = append(allErrs, apivalidation.ValidateImmutableField(newSpec.Networking.Nodes, oldSpec.Networking.Nodes, fldPath.Child("networking", "nodes"))...)
	}

	return allErrs
}

// ValidateProviderUpdate validates the specification of a Provider object.
func ValidateProviderUpdate(newProvider, oldProvider *core.Provider, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	allErrs = append(allErrs, apivalidation.ValidateImmutableField(newProvider.Type, oldProvider.Type, fldPath.Child("type"))...)
	allErrs = append(allErrs, ValidateWorkersUpdate(newProvider.Workers, oldProvider.Workers, fldPath.Child("workers"))...)

	return allErrs
}

// ValidateWorkersUpdate validates the specification of a Provider object.
func ValidateWorkersUpdate(newWorkers, oldWorkers []core.Worker, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	oldWorkersMap := make(map[string]core.Worker)
	for _, w := range oldWorkers {
		oldWorkersMap[w.Name] = w
	}
	for i, w := range newWorkers {
		if _, ok := oldWorkersMap[w.Name]; ok {
			oldWorker := oldWorkersMap[w.Name]
			allErrs = append(allErrs, ValidateWorkerUpdate(&w, &oldWorker, fldPath.Index(i))...)
		}
	}
	return allErrs
}

// ValidateWorkerUpdate validates the specification of a Provider object.
func ValidateWorkerUpdate(newWorker, oldWorker *core.Worker, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	allErrs = append(allErrs, validateCRIUpdate(newWorker.CRI, oldWorker.CRI, fldPath.Child("cri"))...)

	return allErrs
}

func validateCRIUpdate(newCri *core.CRI, oldCri *core.CRI, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	if (newCri == nil && oldCri != nil) || (newCri != nil && oldCri == nil) || (newCri != nil && oldCri != nil && newCri.Name != oldCri.Name) {
		allErrs = append(allErrs, field.Invalid(fldPath, newCri, "can't update cri configurations"))
	}
	return allErrs
}

// ValidateShootStatusUpdate validates the status field of a Shoot object.
func ValidateShootStatusUpdate(newStatus, oldStatus core.ShootStatus) field.ErrorList {
	var (
		allErrs = field.ErrorList{}
		fldPath = field.NewPath("status")
	)

	if len(oldStatus.UID) > 0 {
		allErrs = append(allErrs, apivalidation.ValidateImmutableField(newStatus.UID, oldStatus.UID, fldPath.Child("uid"))...)
	}
	if len(oldStatus.TechnicalID) > 0 {
		allErrs = append(allErrs, apivalidation.ValidateImmutableField(newStatus.TechnicalID, oldStatus.TechnicalID, fldPath.Child("technicalID"))...)
	}

	if oldStatus.ClusterIdentity != nil && !apiequality.Semantic.DeepEqual(oldStatus.ClusterIdentity, newStatus.ClusterIdentity) {
		allErrs = append(allErrs, apivalidation.ValidateImmutableField(newStatus.ClusterIdentity, oldStatus.ClusterIdentity, fldPath.Child("clusterIdentity"))...)
	}
	if len(newStatus.AdvertisedAddresses) > 0 {
		allErrs = append(allErrs, validateAdvertiseAddresses(newStatus.AdvertisedAddresses, fldPath.Child("advertisedAddresses"))...)
	}

	return allErrs
}

// validateAdvertiseAddresses validates kube-apiserver addresses.
func validateAdvertiseAddresses(addresses []core.ShootAdvertisedAddress, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	names := sets.NewString()
	for i, address := range addresses {
		if address.Name == "" {
			allErrs = append(allErrs, field.Required(fldPath.Index(i).Child("name"), "field must not be empty"))
		} else if names.Has(address.Name) {
			allErrs = append(allErrs, field.Duplicate(fldPath.Index(i).Child("name"), address.Name))
		} else {
			names.Insert(address.Name)
			allErrs = append(allErrs, validateAdvertisedURL(address.URL, fldPath.Index(i).Child("url"))...)
		}
	}
	return allErrs
}

// validateAdvertisedURL validates kube-apiserver's URL.
func validateAdvertisedURL(URL string, fldPath *field.Path) field.ErrorList {
	var allErrors field.ErrorList
	const form = "; desired format: https://host[:port]"
	if u, err := url.Parse(URL); err != nil {
		allErrors = append(allErrors, field.Required(fldPath, "url must be a valid URL: "+err.Error()+form))
	} else {
		if u.Scheme != "https" {
			allErrors = append(allErrors, field.Invalid(fldPath, u.Scheme, "'https' is the only allowed URL scheme"+form))
		}
		if len(u.Host) == 0 {
			allErrors = append(allErrors, field.Invalid(fldPath, u.Host, "host must be provided"+form))
		}
		if len(u.Path) > 0 {
			allErrors = append(allErrors, field.Invalid(fldPath, u.Path, "path is not permitted in the URL"+form))
		}
		if u.User != nil {
			allErrors = append(allErrors, field.Invalid(fldPath, u.User.String(), "user information is not permitted in the URL"+form))
		}
		if len(u.Fragment) != 0 {
			allErrors = append(allErrors, field.Invalid(fldPath, u.Fragment, "fragments are not permitted in the URL"+form))
		}
		if len(u.RawQuery) != 0 {
			allErrors = append(allErrors, field.Invalid(fldPath, u.RawQuery, "query parameters are not permitted in the URL"+form))
		}
	}
	return allErrors
}

func validateAddons(addons *core.Addons, kubeAPIServerConfig *core.KubeAPIServerConfig, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	if helper.NginxIngressEnabled(addons) {
		if policy := addons.NginxIngress.ExternalTrafficPolicy; policy != nil {
			if !availableNginxIngressExternalTrafficPolicies.Has(string(*policy)) {
				allErrs = append(allErrs, field.NotSupported(fldPath.Child("nginxIngress", "externalTrafficPolicy"), *policy, availableNginxIngressExternalTrafficPolicies.List()))
			}
		}
	}

	if helper.KubernetesDashboardEnabled(addons) {
		if authMode := addons.KubernetesDashboard.AuthenticationMode; authMode != nil {
			if !availableKubernetesDashboardAuthenticationModes.Has(*authMode) {
				allErrs = append(allErrs, field.NotSupported(fldPath.Child("kubernetesDashboard", "authenticationMode"), *authMode, availableKubernetesDashboardAuthenticationModes.List()))
			}

			if *authMode == core.KubernetesDashboardAuthModeBasic && !helper.ShootWantsBasicAuthentication(kubeAPIServerConfig) {
				allErrs = append(allErrs, field.Invalid(fldPath.Child("kubernetesDashboard", "authenticationMode"), *authMode, "cannot use basic auth mode when basic auth is not enabled in kube-apiserver configuration"))
			}
		}
	}

	return allErrs
}

// ValidateNodeCIDRMaskWithMaxPod validates if the Pod Network has enough ip addresses (configured via the NodeCIDRMask on the kube controller manager) to support the highest max pod setting on the shoot
func ValidateNodeCIDRMaskWithMaxPod(maxPod int32, nodeCIDRMaskSize int32) field.ErrorList {
	allErrs := field.ErrorList{}

	free := float64(32 - nodeCIDRMaskSize)
	// first and last ips are reserved
	ipAdressesAvailable := int32(math.Pow(2, free) - 2)

	if ipAdressesAvailable < maxPod {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec").Child("kubernetes").Child("kubeControllerManager").Child("nodeCIDRMaskSize"), nodeCIDRMaskSize, fmt.Sprintf("kubelet or kube-controller configuration incorrect. Please adjust the NodeCIDRMaskSize of the kube-controller to support the highest maxPod on any worker pool. The NodeCIDRMaskSize of '%d (default: 24)' of the kube-controller only supports '%d' ip adresses. Highest maxPod setting on kubelet is '%d (default: 110)'. Please choose a NodeCIDRMaskSize that at least supports %d ip adresses", nodeCIDRMaskSize, ipAdressesAvailable, maxPod, maxPod)))
	}

	return allErrs
}

func validateKubeControllerManagerUpdate(newConfig, oldConfig *core.KubeControllerManagerConfig, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	var (
		nodeCIDRMaskNew *int32
		nodeCIDRMaskOld *int32
	)

	if newConfig != nil {
		nodeCIDRMaskNew = newConfig.NodeCIDRMaskSize
	}
	if oldConfig != nil {
		nodeCIDRMaskOld = oldConfig.NodeCIDRMaskSize
	}

	allErrs = append(allErrs, apivalidation.ValidateImmutableField(nodeCIDRMaskNew, nodeCIDRMaskOld, fldPath.Child("nodeCIDRMaskSize"))...)

	return allErrs
}

func validateKubeProxyModeUpdate(newConfig, oldConfig *core.KubeProxyConfig, version string, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	newMode := core.ProxyModeIPTables
	oldMode := core.ProxyModeIPTables
	if newConfig != nil {
		newMode = *newConfig.Mode
	}
	if oldConfig != nil {
		oldMode = *oldConfig.Mode
	}
	if ok, _ := versionutils.CheckVersionMeetsConstraint(version, "< 1.16"); ok {
		allErrs = append(allErrs, apivalidation.ValidateImmutableField(newMode, oldMode, fldPath.Child("mode"))...)
	}
	return allErrs
}

func validateAddonsUpdate(new, old *core.Addons, shootUseAsSeed bool, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	if !shootUseAsSeed {
		return allErrs
	}

	if !helper.NginxIngressEnabled(old) && helper.NginxIngressEnabled(new) {
		allErrs = append(allErrs, field.Forbidden(fldPath.Child("nginxIngress", "enabled"),
			"shoot ingress addon is not supported for shooted seeds - please use managed seed ingress controller"))
	}

	return allErrs
}

func validateDNSUpdate(new, old *core.DNS, seedGotAssigned bool, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	if old != nil && new == nil {
		allErrs = append(allErrs, apivalidation.ValidateImmutableField(new, old, fldPath)...)
	}

	if new != nil && old != nil {
		if old.Domain != nil && new.Domain != old.Domain {
			allErrs = append(allErrs, apivalidation.ValidateImmutableField(new.Domain, old.Domain, fldPath.Child("domain"))...)
		}

		// allow to finalize DNS configuration during seed assignment. this is required because
		// some decisions about the DNS setup can only be taken once the target seed is clarified.
		if !seedGotAssigned {
			var (
				primaryOld = helper.FindPrimaryDNSProvider(old.Providers)
				primaryNew = helper.FindPrimaryDNSProvider(new.Providers)
			)

			if primaryOld != nil && primaryNew == nil {
				allErrs = append(allErrs, field.Forbidden(fldPath.Child("providers"), "removing a primary provider is not allowed"))
			}

			if primaryOld != nil && primaryOld.Type != nil && primaryNew != nil && primaryNew.Type == nil {
				allErrs = append(allErrs, field.Forbidden(fldPath.Child("providers"), "removing the primary provider type is not allowed"))
			}

			if primaryOld != nil && primaryOld.Type != nil && primaryNew != nil && primaryNew.Type != nil && *primaryOld.Type != *primaryNew.Type {
				allErrs = append(allErrs, field.Forbidden(fldPath.Child("providers"), "changing primary provider type is not allowed"))
			}
		}
	}

	return allErrs
}

func validateKubernetesVersionUpdate(new, old string, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	if len(new) == 0 {
		allErrs = append(allErrs, field.Invalid(fldPath, new, "cannot validate kubernetes version upgrade because it is unset"))
		return allErrs
	}

	// Forbid Kubernetes version downgrade
	downgrade, err := versionutils.CompareVersions(new, "<", old)
	if err != nil {
		allErrs = append(allErrs, field.Invalid(fldPath, new, err.Error()))
	}
	if downgrade {
		allErrs = append(allErrs, field.Forbidden(fldPath, "kubernetes version downgrade is not supported"))
	}

	// Forbid Kubernetes version upgrade which skips a minor version
	oldVersion, err := semver.NewVersion(old)
	if err != nil {
		allErrs = append(allErrs, field.Invalid(fldPath, old, err.Error()))
	}
	nextMinorVersion := oldVersion.IncMinor().IncMinor()

	skippingMinorVersion, err := versionutils.CompareVersions(new, ">=", nextMinorVersion.String())
	if err != nil {
		allErrs = append(allErrs, field.Invalid(fldPath, new, err.Error()))
	}
	if skippingMinorVersion {
		allErrs = append(allErrs, field.Forbidden(fldPath, "kubernetes version upgrade cannot skip a minor version"))
	}

	return allErrs
}

func validateDNS(dns *core.DNS, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	if dns == nil {
		return allErrs
	}

	if dns.Domain != nil {
		allErrs = append(allErrs, validateDNS1123Subdomain(*dns.Domain, fldPath.Child("domain"))...)
	}

	primaryDNSProvider := helper.FindPrimaryDNSProvider(dns.Providers)
	if primaryDNSProvider != nil && primaryDNSProvider.Type != nil {
		if *primaryDNSProvider.Type != core.DNSUnmanaged && dns.Domain == nil {
			allErrs = append(allErrs, field.Required(fldPath.Child("domain"), fmt.Sprintf("domain must be set when primary provider type is not set to %q", core.DNSUnmanaged)))
		}
	}

	var (
		names        = sets.NewString()
		primaryFound bool
	)
	for i, provider := range dns.Providers {
		idxPath := fldPath.Child("providers").Index(i)

		if provider.SecretName != nil && provider.Type != nil {
			providerName := gutil.GenerateDNSProviderName(*provider.SecretName, *provider.Type)
			if names.Has(providerName) {
				allErrs = append(allErrs, field.Invalid(idxPath, providerName, "combination of .secretName and .type must be unique across dns providers"))
				continue
			}
			for _, err := range validation.IsDNS1123Subdomain(providerName) {
				allErrs = append(allErrs, field.Invalid(idxPath, providerName, fmt.Sprintf("combination of .secretName and .type is invalid: %q", err)))
			}
			names.Insert(providerName)
		}

		if provider.Primary != nil && *provider.Primary {
			if primaryFound {
				allErrs = append(allErrs, field.Forbidden(idxPath.Child("primary"), "multiple primary DNS providers are not supported"))
				continue
			}
			primaryFound = true
		}

		if providerType := provider.Type; providerType != nil {
			if *providerType == core.DNSUnmanaged && provider.SecretName != nil {
				allErrs = append(allErrs, field.Invalid(idxPath.Child("secretName"), provider.SecretName, fmt.Sprintf("secretName must not be set when type is %q", core.DNSUnmanaged)))
				continue
			}
		}

		if provider.SecretName != nil && provider.Type == nil {
			allErrs = append(allErrs, field.Required(idxPath.Child("type"), "type must be set when secretName is set"))
		}
	}

	return allErrs
}

func validateExtensions(extensions []core.Extension, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	for i, extension := range extensions {
		if extension.Type == "" {
			allErrs = append(allErrs, field.Required(fldPath.Index(i).Child("type"), "field must not be empty"))
		}
	}
	return allErrs
}

func validateResources(resources []core.NamedResourceReference, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	names := make(map[string]bool)
	for i, resource := range resources {
		if resource.Name == "" {
			allErrs = append(allErrs, field.Required(fldPath.Index(i).Child("name"), "field must not be empty"))
		} else if names[resource.Name] {
			allErrs = append(allErrs, field.Duplicate(fldPath.Index(i).Child("name"), resource.Name))
		} else {
			names[resource.Name] = true
		}
		allErrs = append(allErrs, validateCrossVersionObjectReference(resource.ResourceRef, fldPath.Index(i).Child("resourceRef"))...)
	}
	return allErrs
}

func validateKubernetes(kubernetes core.Kubernetes, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	if len(kubernetes.Version) == 0 {
		allErrs = append(allErrs, field.Required(fldPath.Child("version"), "kubernetes version must not be empty"))
		return allErrs
	}

	if kubeAPIServer := kubernetes.KubeAPIServer; kubeAPIServer != nil {
		geqKubernetes119, _ := versionutils.CheckVersionMeetsConstraint(kubernetes.Version, ">= 1.19")
		// Errors are ignored here because we cannot do anything meaningful with them - variables will default to `false`.

		if geqKubernetes119 && helper.ShootWantsBasicAuthentication(kubeAPIServer) {
			allErrs = append(allErrs, field.Forbidden(fldPath.Child("kubeAPIServer", "enableBasicAuthentication"), "basic authentication has been removed in Kubernetes v1.19+"))
		}

		if oidc := kubeAPIServer.OIDCConfig; oidc != nil {
			oidcPath := fldPath.Child("kubeAPIServer", "oidcConfig")

			if fieldNilOrEmptyString(oidc.ClientID) {
				if oidc.ClientID != nil {
					allErrs = append(allErrs, field.Invalid(oidcPath.Child("clientID"), oidc.ClientID, "clientID cannot be empty when key is provided"))
				}
				if !fieldNilOrEmptyString(oidc.IssuerURL) {
					allErrs = append(allErrs, field.Invalid(oidcPath.Child("clientID"), oidc.ClientID, "clientID must be set when issuerURL is provided"))
				}
			}

			if fieldNilOrEmptyString(oidc.IssuerURL) {
				if oidc.IssuerURL != nil {
					allErrs = append(allErrs, field.Invalid(oidcPath.Child("issuerURL"), oidc.IssuerURL, "issuerURL cannot be empty when key is provided"))
				}
				if !fieldNilOrEmptyString(oidc.ClientID) {
					allErrs = append(allErrs, field.Invalid(oidcPath.Child("issuerURL"), oidc.IssuerURL, "issuerURL must be set when clientID is provided"))
				}
			} else {
				issuer, err := url.Parse(*oidc.IssuerURL)
				if err != nil || (issuer != nil && len(issuer.Host) == 0) {
					allErrs = append(allErrs, field.Invalid(oidcPath.Child("issuerURL"), oidc.IssuerURL, "must be a valid URL and have https scheme"))
				}
				if issuer != nil && issuer.Scheme != "https" {
					allErrs = append(allErrs, field.Invalid(oidcPath.Child("issuerURL"), oidc.IssuerURL, "must have https scheme"))
				}
			}

			if oidc.CABundle != nil {
				if _, err := utils.DecodeCertificate([]byte(*oidc.CABundle)); err != nil {
					allErrs = append(allErrs, field.Invalid(oidcPath.Child("caBundle"), *oidc.CABundle, "caBundle is not a valid PEM-encoded certificate"))
				}
			}
			if oidc.GroupsClaim != nil && len(*oidc.GroupsClaim) == 0 {
				allErrs = append(allErrs, field.Invalid(oidcPath.Child("groupsClaim"), *oidc.GroupsClaim, "groupsClaim cannot be empty when key is provided"))
			}
			if oidc.GroupsPrefix != nil && len(*oidc.GroupsPrefix) == 0 {
				allErrs = append(allErrs, field.Invalid(oidcPath.Child("groupsPrefix"), *oidc.GroupsPrefix, "groupsPrefix cannot be empty when key is provided"))
			}
			if oidc.SigningAlgs != nil && len(oidc.SigningAlgs) == 0 {
				allErrs = append(allErrs, field.Invalid(oidcPath.Child("signingAlgs"), oidc.SigningAlgs, "signingAlgs cannot be empty when key is provided"))
			}
			if oidc.UsernameClaim != nil && len(*oidc.UsernameClaim) == 0 {
				allErrs = append(allErrs, field.Invalid(oidcPath.Child("usernameClaim"), *oidc.UsernameClaim, "usernameClaim cannot be empty when key is provided"))
			}
			if oidc.UsernamePrefix != nil && len(*oidc.UsernamePrefix) == 0 {
				allErrs = append(allErrs, field.Invalid(oidcPath.Child("usernamePrefix"), *oidc.UsernamePrefix, "usernamePrefix cannot be empty when key is provided"))
			}
		}

		forbiddenAdmissionPlugins := sets.NewString("SecurityContextDeny")
		admissionPluginsPath := fldPath.Child("kubeAPIServer", "admissionPlugins")
		for i, plugin := range kubeAPIServer.AdmissionPlugins {
			idxPath := admissionPluginsPath.Index(i)

			if len(plugin.Name) == 0 {
				allErrs = append(allErrs, field.Required(idxPath.Child("name"), "must provide a name"))
			}

			if forbiddenAdmissionPlugins.Has(plugin.Name) {
				allErrs = append(allErrs, field.Forbidden(idxPath.Child("name"), fmt.Sprintf("forbidden admission plugin was specified - do not use %+v", forbiddenAdmissionPlugins.UnsortedList())))
			}
		}

		if auditConfig := kubeAPIServer.AuditConfig; auditConfig != nil {
			auditPath := fldPath.Child("kubeAPIServer", "auditConfig")
			if auditPolicy := auditConfig.AuditPolicy; auditPolicy != nil && auditConfig.AuditPolicy.ConfigMapRef != nil {
				allErrs = append(allErrs, validateAuditPolicyConfigMapReference(auditPolicy.ConfigMapRef, auditPath.Child("auditPolicy", "configMapRef"))...)
			}
		}

		allErrs = append(allErrs, ValidateWatchCacheSizes(kubeAPIServer.WatchCacheSizes, fldPath.Child("kubeAPIServer", "watchCacheSizes"))...)

		if kubeAPIServer.Requests != nil {
			const maxMaxNonMutatingRequestsInflight = 800
			if v := kubeAPIServer.Requests.MaxNonMutatingInflight; v != nil {
				path := fldPath.Child("kubeAPIServer", "requests", "maxNonMutatingInflight")

				allErrs = append(allErrs, apivalidation.ValidateNonnegativeField(int64(*v), path)...)
				if *v > maxMaxNonMutatingRequestsInflight {
					allErrs = append(allErrs, field.Invalid(path, *v, fmt.Sprintf("cannot set higher than %d", maxMaxNonMutatingRequestsInflight)))
				}
			}

			const maxMaxMutatingRequestsInflight = 400
			if v := kubeAPIServer.Requests.MaxMutatingInflight; v != nil {
				path := fldPath.Child("kubeAPIServer", "requests", "maxMutatingInflight")

				allErrs = append(allErrs, apivalidation.ValidateNonnegativeField(int64(*v), path)...)
				if *v > maxMaxMutatingRequestsInflight {
					allErrs = append(allErrs, field.Invalid(path, *v, fmt.Sprintf("cannot set higher than %d", maxMaxMutatingRequestsInflight)))
				}
			}
		}
	}

	allErrs = append(allErrs, validateKubeControllerManager(kubernetes.Version, kubernetes.KubeControllerManager, fldPath.Child("kubeControllerManager"))...)
	allErrs = append(allErrs, validateKubeProxy(kubernetes.KubeProxy, fldPath.Child("kubeProxy"))...)
	if clusterAutoscaler := kubernetes.ClusterAutoscaler; clusterAutoscaler != nil {
		allErrs = append(allErrs, ValidateClusterAutoscaler(*clusterAutoscaler, fldPath.Child("clusterAutoscaler"))...)
	}
	if verticalPodAutoscaler := kubernetes.VerticalPodAutoscaler; verticalPodAutoscaler != nil {
		allErrs = append(allErrs, ValidateVerticalPodAutoscaler(*verticalPodAutoscaler, fldPath.Child("verticalPodAutoscaler"))...)
	}

	return allErrs
}

func fieldNilOrEmptyString(field *string) bool {
	return field == nil || len(*field) == 0
}

func validateNetworking(networking core.Networking, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	if len(networking.Type) == 0 {
		allErrs = append(allErrs, field.Required(fldPath.Child("type"), "networking type must be provided"))
	}

	if networking.Nodes != nil {
		path := fldPath.Child("nodes")
		cidr := cidrvalidation.NewCIDR(*networking.Nodes, path)

		allErrs = append(allErrs, cidr.ValidateParse()...)
		allErrs = append(allErrs, cidrvalidation.ValidateCIDRIsCanonical(path, cidr.GetCIDR())...)
	}

	if networking.Pods != nil {
		path := fldPath.Child("pods")
		cidr := cidrvalidation.NewCIDR(*networking.Pods, path)

		allErrs = append(allErrs, cidr.ValidateParse()...)
		allErrs = append(allErrs, cidrvalidation.ValidateCIDRIsCanonical(path, cidr.GetCIDR())...)
	}

	if networking.Services != nil {
		path := fldPath.Child("services")
		cidr := cidrvalidation.NewCIDR(*networking.Services, path)

		allErrs = append(allErrs, cidr.ValidateParse()...)
		allErrs = append(allErrs, cidrvalidation.ValidateCIDRIsCanonical(path, cidr.GetCIDR())...)
	}

	return allErrs
}

// ValidateWatchCacheSizes validates the given WatchCacheSizes fields.
func ValidateWatchCacheSizes(sizes *core.WatchCacheSizes, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	if sizes != nil {
		if defaultSize := sizes.Default; defaultSize != nil {
			allErrs = append(allErrs, apivalidation.ValidateNonnegativeField(int64(*defaultSize), fldPath.Child("default"))...)
		}

		for idx, resourceWatchCacheSize := range sizes.Resources {
			idxPath := fldPath.Child("resources").Index(idx)
			if len(resourceWatchCacheSize.Resource) == 0 {
				allErrs = append(allErrs, field.Required(idxPath.Child("resource"), "must not be empty"))
			}
			allErrs = append(allErrs, apivalidation.ValidateNonnegativeField(int64(resourceWatchCacheSize.CacheSize), idxPath.Child("size"))...)
		}
	}
	return allErrs
}

// ValidateClusterAutoscaler validates the given ClusterAutoscaler fields.
func ValidateClusterAutoscaler(autoScaler core.ClusterAutoscaler, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	if threshold := autoScaler.ScaleDownUtilizationThreshold; threshold != nil {
		if *threshold < 0.0 {
			allErrs = append(allErrs, field.Invalid(fldPath.Child("scaleDownUtilizationThreshold"), *threshold, "can not be negative"))
		}
		if *threshold > 1.0 {
			allErrs = append(allErrs, field.Invalid(fldPath.Child("scaleDownUtilizationThreshold"), *threshold, "can not be greater than 1.0"))
		}
	}
	return allErrs
}

// ValidateVerticalPodAutoscaler validates the given VerticalPodAutoscaler fields.
func ValidateVerticalPodAutoscaler(autoScaler core.VerticalPodAutoscaler, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	if threshold := autoScaler.EvictAfterOOMThreshold; threshold != nil && threshold.Duration < 0 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("evictAfterOOMThreshold"), *threshold, "can not be negative"))
	}
	if interval := autoScaler.UpdaterInterval; interval != nil && interval.Duration < 0 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("updaterInterval"), *interval, "can not be negative"))
	}
	if interval := autoScaler.RecommenderInterval; interval != nil && interval.Duration < 0 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("recommenderInterval"), *interval, "can not be negative"))
	}

	return allErrs
}

func validateKubeControllerManager(kubernetesVersion string, kcm *core.KubeControllerManagerConfig, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	if kcm != nil {
		if maskSize := kcm.NodeCIDRMaskSize; maskSize != nil {
			if *maskSize < 16 || *maskSize > 28 {
				allErrs = append(allErrs, field.Invalid(fldPath.Child("nodeCIDRMaskSize"), *maskSize, "nodeCIDRMaskSize must be between 16 and 28"))
			}
		}

		if podEvictionTimeout := kcm.PodEvictionTimeout; podEvictionTimeout != nil && podEvictionTimeout.Duration <= 0 {
			allErrs = append(allErrs, field.Invalid(fldPath.Child("podEvictionTimeout"), podEvictionTimeout.Duration, "podEvictionTimeout must be larger than 0"))
		}

		if nodeMonitorGracePeriod := kcm.NodeMonitorGracePeriod; nodeMonitorGracePeriod != nil && nodeMonitorGracePeriod.Duration <= 0 {
			allErrs = append(allErrs, field.Invalid(fldPath.Child("nodeMonitorGracePeriod"), nodeMonitorGracePeriod.Duration, "nodeMonitorGracePeriod must be larger than 0"))
		}

		if hpa := kcm.HorizontalPodAutoscalerConfig; hpa != nil {
			fldPath = fldPath.Child("horizontalPodAutoscaler")

			if hpa.SyncPeriod != nil && hpa.SyncPeriod.Duration < 1*time.Second {
				allErrs = append(allErrs, field.Invalid(fldPath.Child("syncPeriod"), *hpa.SyncPeriod, "syncPeriod must not be less than a second"))
			}
			if hpa.Tolerance != nil && *hpa.Tolerance <= 0 {
				allErrs = append(allErrs, field.Invalid(fldPath.Child("tolerance"), *hpa.Tolerance, "tolerance of must be greater than 0"))
			}
			if hpa.DownscaleStabilization != nil && hpa.DownscaleStabilization.Duration < 1*time.Second {
				allErrs = append(allErrs, field.Invalid(fldPath.Child("downscaleStabilization"), *hpa.DownscaleStabilization, "downScale stabilization must not be less than a second"))
			}
			if hpa.InitialReadinessDelay != nil && hpa.InitialReadinessDelay.Duration <= 0 {
				allErrs = append(allErrs, field.Invalid(fldPath.Child("initialReadinessDelay"), *hpa.InitialReadinessDelay, "initial readiness delay must be greater than 0"))
			}
			if hpa.CPUInitializationPeriod != nil && hpa.CPUInitializationPeriod.Duration < 1*time.Second {
				allErrs = append(allErrs, field.Invalid(fldPath.Child("cpuInitializationPeriod"), *hpa.CPUInitializationPeriod, "cpu initialization period must not be less than a second"))
			}
		}
	}

	return allErrs
}

func validateKubeProxy(kp *core.KubeProxyConfig, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	if kp != nil {
		if kp.Mode == nil {
			allErrs = append(allErrs, field.Required(fldPath.Child("mode"), "must be set when .spec.kubernetes.kubeProxy is set"))
		} else if mode := *kp.Mode; !availableProxyMode.Has(string(mode)) {
			allErrs = append(allErrs, field.NotSupported(fldPath.Child("mode"), mode, availableProxyMode.List()))
		}
	}
	return allErrs
}

func validateMonitoring(monitoring *core.Monitoring, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	if monitoring != nil && monitoring.Alerting != nil {
		allErrs = append(allErrs, validateAlerting(monitoring.Alerting, fldPath.Child("alerting"))...)
	}
	return allErrs
}

func validateAlerting(alerting *core.Alerting, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	emails := make(map[string]struct{})
	for i, email := range alerting.EmailReceivers {
		if !utils.TestEmail(email) {
			allErrs = append(allErrs, field.Invalid(fldPath.Child("emailReceivers").Index(i), email, "must provide a valid email"))
		}

		if _, duplicate := emails[email]; duplicate {
			allErrs = append(allErrs, field.Duplicate(fldPath.Child("emailReceivers").Index(i), email))
		} else {
			emails[email] = struct{}{}
		}
	}
	return allErrs
}

func validateMaintenance(maintenance *core.Maintenance, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	if maintenance == nil {
		return allErrs
	}

	if maintenance.TimeWindow != nil {
		maintenanceTimeWindow, err := utils.ParseMaintenanceTimeWindow(maintenance.TimeWindow.Begin, maintenance.TimeWindow.End)
		if err != nil {
			allErrs = append(allErrs, field.Invalid(fldPath.Child("timeWindow", "begin/end"), maintenance.TimeWindow, err.Error()))
		} else {
			duration := maintenanceTimeWindow.Duration()
			if duration > core.MaintenanceTimeWindowDurationMaximum {
				allErrs = append(allErrs, field.Forbidden(fldPath.Child("timeWindow"), fmt.Sprintf("time window must not be greater than %s", core.MaintenanceTimeWindowDurationMaximum)))
				return allErrs
			}
			if duration < core.MaintenanceTimeWindowDurationMinimum {
				allErrs = append(allErrs, field.Forbidden(fldPath.Child("timeWindow"), fmt.Sprintf("time window must not be smaller than %s", core.MaintenanceTimeWindowDurationMinimum)))
				return allErrs
			}
		}
	}

	return allErrs
}

func validateProvider(provider core.Provider, kubernetes core.Kubernetes, fldPath *field.Path, inTemplate bool) field.ErrorList {
	allErrs := field.ErrorList{}

	if len(provider.Type) == 0 {
		allErrs = append(allErrs, field.Required(fldPath.Child("type"), "must specify a provider type"))
	}

	var maxPod int32
	if kubernetes.Kubelet != nil && kubernetes.Kubelet.MaxPods != nil {
		maxPod = *kubernetes.Kubelet.MaxPods
	}

	for i, worker := range provider.Workers {
		allErrs = append(allErrs, ValidateWorker(worker, fldPath.Child("workers").Index(i), inTemplate)...)

		if worker.Kubernetes != nil && worker.Kubernetes.Kubelet != nil && worker.Kubernetes.Kubelet.MaxPods != nil && *worker.Kubernetes.Kubelet.MaxPods > maxPod {
			maxPod = *worker.Kubernetes.Kubelet.MaxPods
		}
	}

	allErrs = append(allErrs, ValidateWorkers(provider.Workers, fldPath.Child("workers"))...)

	if kubernetes.KubeControllerManager != nil && kubernetes.KubeControllerManager.NodeCIDRMaskSize != nil {
		if maxPod == 0 {
			// default maxPod setting on kubelet
			maxPod = 110
		}
		allErrs = append(allErrs, ValidateNodeCIDRMaskWithMaxPod(maxPod, *kubernetes.KubeControllerManager.NodeCIDRMaskSize)...)
	}

	return allErrs
}

const (
	// maxWorkerNameLength is a constant for the maximum length for worker name.
	maxWorkerNameLength = 15

	// maxVolumeNameLength is a constant for the maximum length for data volume name.
	maxVolumeNameLength = 15
)

// ValidateWorker validates the worker object.
func ValidateWorker(worker core.Worker, fldPath *field.Path, inTemplate bool) field.ErrorList {
	allErrs := field.ErrorList{}

	allErrs = append(allErrs, validateDNS1123Label(worker.Name, fldPath.Child("name"))...)
	if len(worker.Name) > maxWorkerNameLength {
		allErrs = append(allErrs, field.TooLong(fldPath.Child("name"), worker.Name, maxWorkerNameLength))
	}
	if len(worker.Machine.Type) == 0 {
		allErrs = append(allErrs, field.Required(fldPath.Child("machine", "type"), "must specify a machine type"))
	}
	if worker.Machine.Image != nil {
		if len(worker.Machine.Image.Name) == 0 {
			allErrs = append(allErrs, field.Required(fldPath.Child("machine", "image", "name"), "must specify a machine image name"))
		}
		if !inTemplate && len(worker.Machine.Image.Version) == 0 {
			allErrs = append(allErrs, field.Required(fldPath.Child("machine", "image", "version"), "must specify a machine image version"))
		}
	}
	if worker.Minimum < 0 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("minimum"), worker.Minimum, "minimum value must not be negative"))
	}
	if worker.Maximum < 0 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("maximum"), worker.Maximum, "maximum value must not be negative"))
	}
	if worker.Maximum < worker.Minimum {
		allErrs = append(allErrs, field.Forbidden(fldPath.Child("maximum"), "maximum value must not be less or equal than minimum value"))
	}

	allErrs = append(allErrs, ValidatePositiveIntOrPercent(worker.MaxSurge, fldPath.Child("maxSurge"))...)
	allErrs = append(allErrs, ValidatePositiveIntOrPercent(worker.MaxUnavailable, fldPath.Child("maxUnavailable"))...)
	allErrs = append(allErrs, IsNotMoreThan100Percent(worker.MaxUnavailable, fldPath.Child("maxUnavailable"))...)

	if (worker.MaxUnavailable == nil || getIntOrPercentValue(*worker.MaxUnavailable) == 0) && (worker.MaxSurge != nil && getIntOrPercentValue(*worker.MaxSurge) == 0) {
		// Both MaxSurge and MaxUnavailable cannot be zero.
		allErrs = append(allErrs, field.Invalid(fldPath.Child("maxUnavailable"), worker.MaxUnavailable, "may not be 0 when `maxSurge` is 0"))
	}

	allErrs = append(allErrs, metav1validation.ValidateLabels(worker.Labels, fldPath.Child("labels"))...)
	allErrs = append(allErrs, apivalidation.ValidateAnnotations(worker.Annotations, fldPath.Child("annotations"))...)
	if len(worker.Taints) > 0 {
		allErrs = append(allErrs, validateTaints(worker.Taints, fldPath.Child("taints"))...)
	}
	if worker.Kubernetes != nil && worker.Kubernetes.Kubelet != nil {
		allErrs = append(allErrs, ValidateKubeletConfig(*worker.Kubernetes.Kubelet, fldPath.Child("kubernetes", "kubelet"))...)
	}

	if worker.CABundle != nil {
		if _, err := utils.DecodeCertificate([]byte(*worker.CABundle)); err != nil {
			allErrs = append(allErrs, field.Invalid(fldPath.Child("caBundle"), *(worker.CABundle), "caBundle is not a valid PEM-encoded certificate"))
		}
	}

	volumeSizeRegex, _ := regexp.Compile(`^(\d)+Gi$`)

	if worker.Volume != nil {
		if !volumeSizeRegex.MatchString(worker.Volume.VolumeSize) {
			allErrs = append(allErrs, field.Invalid(fldPath.Child("volume", "size"), worker.Volume.VolumeSize, fmt.Sprintf("volume size must match the regex %s", volumeSizeRegex)))
		}
	}

	if worker.DataVolumes != nil {
		volumeNames := make(map[string]int)
		if len(worker.DataVolumes) > 0 && worker.Volume == nil {
			allErrs = append(allErrs, field.Required(fldPath.Child("volume"), "a worker volume must be defined if data volumes are defined"))
		}
		for idx, volume := range worker.DataVolumes {
			idxPath := fldPath.Child("dataVolumes").Index(idx)
			if len(volume.Name) == 0 {
				allErrs = append(allErrs, field.Required(idxPath.Child("name"), "must specify a name"))
			} else {
				allErrs = append(allErrs, validateDNS1123Label(volume.Name, idxPath.Child("name"))...)
			}
			if len(volume.Name) > maxVolumeNameLength {
				allErrs = append(allErrs, field.TooLong(idxPath.Child("name"), volume.Name, maxVolumeNameLength))
			}
			if _, keyExist := volumeNames[volume.Name]; keyExist {
				volumeNames[volume.Name]++
				allErrs = append(allErrs, field.Duplicate(idxPath.Child("name"), volume.Name))
			} else {
				volumeNames[volume.Name] = 1
			}
			if !volumeSizeRegex.MatchString(volume.VolumeSize) {
				allErrs = append(allErrs, field.Invalid(idxPath.Child("size"), volume.VolumeSize, fmt.Sprintf("data volume size must match the regex %s", volumeSizeRegex)))
			}
		}
	}

	if worker.KubeletDataVolumeName != nil {
		found := false
		for _, volume := range worker.DataVolumes {
			if volume.Name == *worker.KubeletDataVolumeName {
				found = true
			}
		}
		if !found {
			allErrs = append(allErrs, field.Invalid(fldPath.Child("kubeletDataVolumeName"), worker.KubeletDataVolumeName, fmt.Sprintf("KubeletDataVolumeName refers to unrecognized data volume %s", *worker.KubeletDataVolumeName)))
		}
	}

	if worker.CRI != nil {
		allErrs = append(allErrs, ValidateCRI(worker.CRI, fldPath.Child("cri"))...)
	}

	return allErrs
}

// PodPIDsLimitMinimum is a constant for the minimum value for the podPIDsLimit field.
const PodPIDsLimitMinimum int64 = 100

// ValidateKubeletConfig validates the KubeletConfig object.
func ValidateKubeletConfig(kubeletConfig core.KubeletConfig, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	if kubeletConfig.MaxPods != nil {
		allErrs = append(allErrs, apivalidation.ValidateNonnegativeField(int64(*kubeletConfig.MaxPods), fldPath.Child("maxPods"))...)
	}
	if value := kubeletConfig.PodPIDsLimit; value != nil {
		if *value < PodPIDsLimitMinimum {
			allErrs = append(allErrs, field.Invalid(fldPath.Child("podPIDsLimit"), *value, fmt.Sprintf("podPIDsLimit value must be at least %d", PodPIDsLimitMinimum)))
		}
	}
	if kubeletConfig.ImagePullProgressDeadline != nil {
		allErrs = append(allErrs, ValidatePositiveDuration(kubeletConfig.ImagePullProgressDeadline, fldPath.Child("imagePullProgressDeadline"))...)
	}
	if kubeletConfig.EvictionPressureTransitionPeriod != nil {
		allErrs = append(allErrs, ValidatePositiveDuration(kubeletConfig.EvictionPressureTransitionPeriod, fldPath.Child("evictionPressureTransitionPeriod"))...)
	}
	if kubeletConfig.EvictionMaxPodGracePeriod != nil {
		allErrs = append(allErrs, apivalidation.ValidateNonnegativeField(int64(*kubeletConfig.EvictionMaxPodGracePeriod), fldPath.Child("evictionMaxPodGracePeriod"))...)
	}
	if kubeletConfig.EvictionHard != nil {
		allErrs = append(allErrs, validateKubeletConfigEviction(kubeletConfig.EvictionHard, fldPath.Child("evictionHard"))...)
	}
	if kubeletConfig.EvictionSoft != nil {
		allErrs = append(allErrs, validateKubeletConfigEviction(kubeletConfig.EvictionSoft, fldPath.Child("evictionSoft"))...)
	}
	if kubeletConfig.EvictionMinimumReclaim != nil {
		allErrs = append(allErrs, validateKubeletConfigEvictionMinimumReclaim(kubeletConfig.EvictionMinimumReclaim, fldPath.Child("evictionMinimumReclaim"))...)
	}
	if kubeletConfig.EvictionSoftGracePeriod != nil {
		allErrs = append(allErrs, validateKubeletConfigEvictionSoftGracePeriod(kubeletConfig.EvictionSoftGracePeriod, fldPath.Child("evictionSoftGracePeriod"))...)
	}
	if kubeletConfig.KubeReserved != nil {
		allErrs = append(allErrs, validateKubeletConfigReserved(kubeletConfig.KubeReserved, fldPath.Child("kubeReserved"))...)
	}
	if kubeletConfig.SystemReserved != nil {
		allErrs = append(allErrs, validateKubeletConfigReserved(kubeletConfig.SystemReserved, fldPath.Child("systemReserved"))...)
	}
	return allErrs
}

func validateKubeletConfigEviction(eviction *core.KubeletConfigEviction, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	allErrs = append(allErrs, ValidateResourceQuantityOrPercent(eviction.MemoryAvailable, fldPath, "memoryAvailable")...)
	allErrs = append(allErrs, ValidateResourceQuantityOrPercent(eviction.ImageFSAvailable, fldPath, "imagefsAvailable")...)
	allErrs = append(allErrs, ValidateResourceQuantityOrPercent(eviction.ImageFSInodesFree, fldPath, "imagefsInodesFree")...)
	allErrs = append(allErrs, ValidateResourceQuantityOrPercent(eviction.NodeFSAvailable, fldPath, "nodefsAvailable")...)
	allErrs = append(allErrs, ValidateResourceQuantityOrPercent(eviction.ImageFSInodesFree, fldPath, "imagefsInodesFree")...)
	return allErrs
}

func validateKubeletConfigEvictionMinimumReclaim(eviction *core.KubeletConfigEvictionMinimumReclaim, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	if eviction.MemoryAvailable != nil {
		allErrs = append(allErrs, ValidateResourceQuantityValue("memoryAvailable", *eviction.MemoryAvailable, fldPath.Child("memoryAvailable"))...)
	}
	if eviction.ImageFSAvailable != nil {
		allErrs = append(allErrs, ValidateResourceQuantityValue("imagefsAvailable", *eviction.ImageFSAvailable, fldPath.Child("imagefsAvailable"))...)
	}
	if eviction.ImageFSInodesFree != nil {
		allErrs = append(allErrs, ValidateResourceQuantityValue("imagefsInodesFree", *eviction.ImageFSInodesFree, fldPath.Child("imagefsInodesFree"))...)
	}
	if eviction.NodeFSAvailable != nil {
		allErrs = append(allErrs, ValidateResourceQuantityValue("nodefsAvailable", *eviction.NodeFSAvailable, fldPath.Child("nodefsAvailable"))...)
	}
	if eviction.ImageFSInodesFree != nil {
		allErrs = append(allErrs, ValidateResourceQuantityValue("imagefsInodesFree", *eviction.ImageFSInodesFree, fldPath.Child("imagefsInodesFree"))...)
	}
	return allErrs
}

func validateKubeletConfigEvictionSoftGracePeriod(eviction *core.KubeletConfigEvictionSoftGracePeriod, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	allErrs = append(allErrs, ValidatePositiveDuration(eviction.MemoryAvailable, fldPath.Child("memoryAvailable"))...)
	allErrs = append(allErrs, ValidatePositiveDuration(eviction.ImageFSAvailable, fldPath.Child("imagefsAvailable"))...)
	allErrs = append(allErrs, ValidatePositiveDuration(eviction.ImageFSInodesFree, fldPath.Child("imagefsInodesFree"))...)
	allErrs = append(allErrs, ValidatePositiveDuration(eviction.NodeFSAvailable, fldPath.Child("nodefsAvailable"))...)
	allErrs = append(allErrs, ValidatePositiveDuration(eviction.ImageFSInodesFree, fldPath.Child("imagefsInodesFree"))...)
	return allErrs
}

func validateKubeletConfigReserved(reserved *core.KubeletConfigReserved, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	if reserved.CPU != nil {
		allErrs = append(allErrs, ValidateResourceQuantityValue("cpu", *reserved.CPU, fldPath.Child("cpu"))...)
	}
	if reserved.Memory != nil {
		allErrs = append(allErrs, ValidateResourceQuantityValue("memory", *reserved.Memory, fldPath.Child("memory"))...)
	}
	if reserved.EphemeralStorage != nil {
		allErrs = append(allErrs, ValidateResourceQuantityValue("ephemeralStorage", *reserved.EphemeralStorage, fldPath.Child("ephemeralStorage"))...)
	}
	if reserved.PID != nil {
		allErrs = append(allErrs, ValidateResourceQuantityValue("pid", *reserved.PID, fldPath.Child("pid"))...)
	}
	return allErrs
}

// https://github.com/kubernetes/kubernetes/blob/ee9079f8ec39914ff8975b5390749771b9303ea4/pkg/apis/core/validation/validation.go#L4057-L4089
func validateTaints(taints []corev1.Taint, fldPath *field.Path) field.ErrorList {
	allErrors := field.ErrorList{}

	uniqueTaints := map[corev1.TaintEffect]sets.String{}

	for i, taint := range taints {
		idxPath := fldPath.Index(i)
		// validate the taint key
		allErrors = append(allErrors, metav1validation.ValidateLabelName(taint.Key, idxPath.Child("key"))...)
		// validate the taint value
		if errs := validation.IsValidLabelValue(taint.Value); len(errs) != 0 {
			allErrors = append(allErrors, field.Invalid(idxPath.Child("value"), taint.Value, strings.Join(errs, ";")))
		}
		// validate the taint effect
		allErrors = append(allErrors, validateTaintEffect(&taint.Effect, false, idxPath.Child("effect"))...)

		// validate if taint is unique by <key, effect>
		if len(uniqueTaints[taint.Effect]) > 0 && uniqueTaints[taint.Effect].Has(taint.Key) {
			duplicatedError := field.Duplicate(idxPath, taint)
			duplicatedError.Detail = "taints must be unique by key and effect pair"
			allErrors = append(allErrors, duplicatedError)
			continue
		}

		// add taint to existingTaints for uniqueness check
		if len(uniqueTaints[taint.Effect]) == 0 {
			uniqueTaints[taint.Effect] = sets.String{}
		}
		uniqueTaints[taint.Effect].Insert(taint.Key)
	}
	return allErrors
}

// https://github.com/kubernetes/kubernetes/blob/ee9079f8ec39914ff8975b5390749771b9303ea4/pkg/apis/core/validation/validation.go#L2774-L2795
func validateTaintEffect(effect *corev1.TaintEffect, allowEmpty bool, fldPath *field.Path) field.ErrorList {
	if !allowEmpty && len(*effect) == 0 {
		return field.ErrorList{field.Required(fldPath, "")}
	}

	allErrors := field.ErrorList{}
	switch *effect {
	case corev1.TaintEffectNoSchedule, corev1.TaintEffectPreferNoSchedule, corev1.TaintEffectNoExecute:
	default:
		validValues := []string{
			string(corev1.TaintEffectNoSchedule),
			string(corev1.TaintEffectPreferNoSchedule),
			string(corev1.TaintEffectNoExecute),
		}
		allErrors = append(allErrors, field.NotSupported(fldPath, *effect, validValues))
	}
	return allErrors
}

// ValidateWorkers validates worker objects.
func ValidateWorkers(workers []core.Worker, fldPath *field.Path) field.ErrorList {
	var (
		allErrs = field.ErrorList{}

		workerNames                               = make(map[string]bool)
		atLeastOneActivePool                      = false
		atLeastOnePoolWithCompatibleTaints        = len(workers) == 0
		atLeastOnePoolWithAllowedSystemComponents = false
	)

	for i, worker := range workers {
		var (
			poolIsActive            = false
			poolHasCompatibleTaints = false
		)

		if worker.Minimum != 0 && worker.Maximum != 0 {
			poolIsActive = true
		}

		if workerNames[worker.Name] {
			allErrs = append(allErrs, field.Duplicate(fldPath.Index(i).Child("name"), worker.Name))
		}
		workerNames[worker.Name] = true

		switch {
		case len(worker.Taints) == 0:
			poolHasCompatibleTaints = true
		case !atLeastOnePoolWithCompatibleTaints:
			onlyPreferNoScheduleEffectTaints := true
			for _, taint := range worker.Taints {
				if taint.Effect != corev1.TaintEffectPreferNoSchedule {
					onlyPreferNoScheduleEffectTaints = false
					break
				}
			}
			if onlyPreferNoScheduleEffectTaints {
				poolHasCompatibleTaints = true
			}
		}

		if poolIsActive && poolHasCompatibleTaints && helper.SystemComponentsAllowed(&worker) {
			atLeastOnePoolWithAllowedSystemComponents = true
		}

		if !atLeastOneActivePool {
			atLeastOneActivePool = poolIsActive
		}

		if !atLeastOnePoolWithCompatibleTaints {
			atLeastOnePoolWithCompatibleTaints = poolHasCompatibleTaints
		}
	}

	if !atLeastOneActivePool {
		allErrs = append(allErrs, field.Forbidden(fldPath, "at least one worker pool with min>0 and max> 0 needed"))
	}

	if !atLeastOnePoolWithCompatibleTaints {
		allErrs = append(allErrs, field.Forbidden(fldPath, fmt.Sprintf("at least one worker pool must exist having either no taints or only the %q taint", corev1.TaintEffectPreferNoSchedule)))
	}

	if !atLeastOnePoolWithAllowedSystemComponents {
		allErrs = append(allErrs, field.Forbidden(fldPath, "at least one active worker pool with allowSystemComponents=true needed"))
	}

	return allErrs
}

// ValidateHibernation validates a Hibernation object.
func ValidateHibernation(hibernation *core.Hibernation, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	if hibernation == nil {
		return allErrs
	}

	allErrs = append(allErrs, ValidateHibernationSchedules(hibernation.Schedules, fldPath.Child("schedules"))...)

	return allErrs
}

// ValidateHibernationSchedules validates a list of hibernation schedules.
func ValidateHibernationSchedules(schedules []core.HibernationSchedule, fldPath *field.Path) field.ErrorList {
	var (
		allErrs = field.ErrorList{}
		seen    = sets.NewString()
	)

	for i, schedule := range schedules {
		allErrs = append(allErrs, ValidateHibernationSchedule(seen, &schedule, fldPath.Index(i))...)
	}

	return allErrs
}

// ValidateHibernationCronSpec validates a cron specification of a hibernation schedule.
func ValidateHibernationCronSpec(seenSpecs sets.String, spec string, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	_, err := cron.ParseStandard(spec)
	switch {
	case err != nil:
		allErrs = append(allErrs, field.Invalid(fldPath, spec, fmt.Sprintf("not a valid cron spec: %v", err)))
	case seenSpecs.Has(spec):
		allErrs = append(allErrs, field.Duplicate(fldPath, spec))
	default:
		seenSpecs.Insert(spec)
	}

	return allErrs
}

// ValidateHibernationScheduleLocation validates that the location of a HibernationSchedule is correct.
func ValidateHibernationScheduleLocation(location string, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	if _, err := time.LoadLocation(location); err != nil {
		allErrs = append(allErrs, field.Invalid(fldPath, location, fmt.Sprintf("not a valid location: %v", err)))
	}

	return allErrs
}

// ValidateHibernationSchedule validates the correctness of a HibernationSchedule.
// It checks whether the set start and end time are valid cron specs.
func ValidateHibernationSchedule(seenSpecs sets.String, schedule *core.HibernationSchedule, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	if schedule.Start == nil && schedule.End == nil {
		allErrs = append(allErrs, field.Required(fldPath.Child("start/end"), "either start or end has to be provided"))
	}
	if schedule.Start != nil {
		allErrs = append(allErrs, ValidateHibernationCronSpec(seenSpecs, *schedule.Start, fldPath.Child("start"))...)
	}
	if schedule.End != nil {
		allErrs = append(allErrs, ValidateHibernationCronSpec(seenSpecs, *schedule.End, fldPath.Child("end"))...)
	}
	if schedule.Location != nil {
		allErrs = append(allErrs, ValidateHibernationScheduleLocation(*schedule.Location, fldPath.Child("location"))...)
	}

	return allErrs
}

// ValidatePositiveDuration validates that a duration is positive.
func ValidatePositiveDuration(duration *metav1.Duration, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	if duration == nil {
		return allErrs
	}
	if duration.Seconds() < 0 {
		allErrs = append(allErrs, field.Invalid(fldPath, duration.Duration.String(), "must be non-negative"))
	}
	return allErrs
}

// ValidateResourceQuantityOrPercent checks if a value can be parsed to either a resource.quantity, a positive int or percent.
func ValidateResourceQuantityOrPercent(valuePtr *string, fldPath *field.Path, key string) field.ErrorList {
	allErrs := field.ErrorList{}

	if valuePtr == nil {
		return allErrs
	}
	value := *valuePtr
	// check for resource quantity
	if quantity, err := resource.ParseQuantity(value); err == nil {
		if len(ValidateResourceQuantityValue(key, quantity, fldPath)) == 0 {
			return allErrs
		}
	}

	if validation.IsValidPercent(value) != nil {
		allErrs = append(allErrs, field.Invalid(fldPath.Child(key), value, "field must be either a valid resource quantity (e.g 200Mi) or a percentage (e.g '5%')"))
		return allErrs
	}

	percentValue, _ := strconv.Atoi(value[:len(value)-1])
	if percentValue > 100 || percentValue < 0 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child(key), value, "must not be greater than 100% and not smaller than 0%"))
	}
	return allErrs
}

// ValidatePositiveIntOrPercent validates a int or string object and ensures it is positive.
func ValidatePositiveIntOrPercent(intOrPercent *intstr.IntOrString, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	if intOrPercent == nil {
		return allErrs
	}

	if intOrPercent.Type == intstr.String {
		if validation.IsValidPercent(intOrPercent.StrVal) != nil {
			allErrs = append(allErrs, field.Invalid(fldPath, intOrPercent, "must be an integer or percentage (e.g '5%')"))
		}
	} else if intOrPercent.Type == intstr.Int {
		allErrs = append(allErrs, apivalidation.ValidateNonnegativeField(int64(intOrPercent.IntValue()), fldPath)...)
	}

	return allErrs
}

// IsNotMoreThan100Percent validates an int or string object and ensures it is not more than 100%.
func IsNotMoreThan100Percent(intOrStringValue *intstr.IntOrString, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	if intOrStringValue == nil {
		return allErrs
	}

	value, isPercent := getPercentValue(*intOrStringValue)
	if !isPercent || value <= 100 {
		return nil
	}
	allErrs = append(allErrs, field.Invalid(fldPath, intOrStringValue, "must not be greater than 100%"))

	return allErrs
}

// ValidateCRI validates container runtime interface name and its container runtimes
func ValidateCRI(CRI *core.CRI, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	if !avaliableWorkerCRINames.Has(string(CRI.Name)) {
		allErrs = append(allErrs, field.NotSupported(fldPath.Child("name"), CRI.Name, avaliableWorkerCRINames.List()))
	}

	if CRI.ContainerRuntimes != nil {
		allErrs = append(allErrs, ValidateContainerRuntimes(CRI.ContainerRuntimes, fldPath.Child("containerruntimes"))...)
	}

	return allErrs
}

// ValidateContainerRuntimes validates the given container runtimes
func ValidateContainerRuntimes(containerRuntime []core.ContainerRuntime, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	crSet := make(map[string]bool)

	for i, cr := range containerRuntime {
		if len(cr.Type) == 0 {
			allErrs = append(allErrs, field.Required(fldPath.Index(i).Child("type"), "must specify a container runtime type"))
		}
		if crSet[cr.Type] {
			allErrs = append(allErrs, field.Duplicate(fldPath.Index(i).Child("type"), fmt.Sprintf("must specify different type, %s already exist", cr.Type)))
		}
		crSet[cr.Type] = true
	}

	return allErrs
}
