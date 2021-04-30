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
	"github.com/gardener/gardener/pkg/apis/core"
	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	"github.com/gardener/gardener/pkg/utils"
	cidrvalidation "github.com/gardener/gardener/pkg/utils/validation/cidr"

	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apivalidation "k8s.io/apimachinery/pkg/api/validation"
	metav1validation "k8s.io/apimachinery/pkg/apis/meta/v1/validation"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

var (
	availableIngressKinds = sets.NewString(
		v1beta1constants.IngressKindNginx,
	)
)

// ValidateSeed validates a Seed object.
func ValidateSeed(seed *core.Seed) field.ErrorList {
	allErrs := field.ErrorList{}

	allErrs = append(allErrs, apivalidation.ValidateObjectMeta(&seed.ObjectMeta, false, apivalidation.NameIsDNSLabel, field.NewPath("metadata"))...)
	allErrs = append(allErrs, ValidateSeedSpec(&seed.Spec, field.NewPath("spec"), false)...)

	return allErrs
}

// ValidateSeedUpdate validates a Seed object before an update.
func ValidateSeedUpdate(newSeed, oldSeed *core.Seed) field.ErrorList {
	allErrs := field.ErrorList{}

	allErrs = append(allErrs, apivalidation.ValidateObjectMetaUpdate(&newSeed.ObjectMeta, &oldSeed.ObjectMeta, field.NewPath("metadata"))...)
	allErrs = append(allErrs, ValidateSeedSpecUpdate(&newSeed.Spec, &oldSeed.Spec, field.NewPath("spec"))...)
	allErrs = append(allErrs, ValidateSeed(newSeed)...)

	return allErrs
}

// ValidateSeedTemplate validates a SeedTemplate.
func ValidateSeedTemplate(seedTemplate *core.SeedTemplate, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	allErrs = append(allErrs, metav1validation.ValidateLabels(seedTemplate.Labels, fldPath.Child("metadata", "labels"))...)
	allErrs = append(allErrs, apivalidation.ValidateAnnotations(seedTemplate.Annotations, fldPath.Child("metadata", "annotations"))...)
	allErrs = append(allErrs, ValidateSeedSpec(&seedTemplate.Spec, fldPath.Child("spec"), true)...)

	return allErrs
}

// ValidateSeedTemplateUpdate validates a SeedTemplate before an update.
func ValidateSeedTemplateUpdate(newSeedTemplate, oldSeedTemplate *core.SeedTemplate, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	allErrs = append(allErrs, ValidateSeedSpecUpdate(&newSeedTemplate.Spec, &oldSeedTemplate.Spec, fldPath.Child("spec"))...)

	return allErrs
}

// ValidateSeedSpec validates the specification of a Seed object.
func ValidateSeedSpec(seedSpec *core.SeedSpec, fldPath *field.Path, inTemplate bool) field.ErrorList {
	allErrs := field.ErrorList{}

	providerPath := fldPath.Child("provider")
	if !inTemplate && len(seedSpec.Provider.Type) == 0 {
		allErrs = append(allErrs, field.Required(providerPath.Child("type"), "must provide a provider type"))
	}
	if !inTemplate && len(seedSpec.Provider.Region) == 0 {
		allErrs = append(allErrs, field.Required(providerPath.Child("region"), "must provide a provider region"))
	}

	if seedSpec.SecretRef != nil {
		allErrs = append(allErrs, validateSecretReference(*seedSpec.SecretRef, fldPath.Child("secretRef"))...)
	}

	networksPath := fldPath.Child("networks")

	var networks []cidrvalidation.CIDR
	if !inTemplate || len(seedSpec.Networks.Pods) > 0 {
		networks = append(networks, cidrvalidation.NewCIDR(seedSpec.Networks.Pods, networksPath.Child("pods")))
	}
	if !inTemplate || len(seedSpec.Networks.Services) > 0 {
		networks = append(networks, cidrvalidation.NewCIDR(seedSpec.Networks.Services, networksPath.Child("services")))
	}
	if seedSpec.Networks.Nodes != nil {
		networks = append(networks, cidrvalidation.NewCIDR(*seedSpec.Networks.Nodes, networksPath.Child("nodes")))
	}
	if shootDefaults := seedSpec.Networks.ShootDefaults; shootDefaults != nil {
		if shootDefaults.Pods != nil {
			networks = append(networks, cidrvalidation.NewCIDR(*shootDefaults.Pods, networksPath.Child("shootDefaults", "pods")))
		}
		if shootDefaults.Services != nil {
			networks = append(networks, cidrvalidation.NewCIDR(*shootDefaults.Services, networksPath.Child("shootDefaults", "services")))
		}
	}

	allErrs = append(allErrs, cidrvalidation.ValidateCIDRParse(networks...)...)
	allErrs = append(allErrs, cidrvalidation.ValidateCIDROverlap(networks, networks, false)...)

	vpnDefaultRanges := []cidrvalidation.CIDR{cidrvalidation.NewCIDR(v1beta1constants.DefaultVpnRange, field.NewPath(""))}
	allErrs = append(allErrs, cidrvalidation.ValidateCIDROverlap(vpnDefaultRanges, networks, false)...)
	allErrs = append(allErrs, cidrvalidation.ValidateCIDROverlap(networks, vpnDefaultRanges, false)...)

	if seedSpec.Backup != nil {
		if len(seedSpec.Backup.Provider) == 0 {
			allErrs = append(allErrs, field.Required(fldPath.Child("backup", "provider"), "must provide a backup cloud provider name"))
		}

		if seedSpec.Provider.Type != seedSpec.Backup.Provider && (seedSpec.Backup.Region == nil || len(*seedSpec.Backup.Region) == 0) {
			allErrs = append(allErrs, field.Invalid(fldPath.Child("backup", "region"), "", "region must be specified for if backup provider is different from provider used in `spec.cloud`"))
		}

		allErrs = append(allErrs, validateSecretReference(seedSpec.Backup.SecretRef, fldPath.Child("backup", "secretRef"))...)
	}

	var keyValues = sets.NewString()

	for i, taint := range seedSpec.Taints {
		idxPath := fldPath.Child("taints").Index(i)

		if len(taint.Key) == 0 {
			allErrs = append(allErrs, field.Required(idxPath.Child("key"), "cannot be empty"))
		}

		id := utils.IDForKeyWithOptionalValue(taint.Key, taint.Value)
		if keyValues.Has(id) {
			allErrs = append(allErrs, field.Duplicate(idxPath, id))
		}
		keyValues.Insert(id)
	}

	if seedSpec.Volume != nil {
		if seedSpec.Volume.MinimumSize != nil {
			allErrs = append(allErrs, ValidateResourceQuantityValue("minimumSize", *seedSpec.Volume.MinimumSize, fldPath.Child("volume", "minimumSize"))...)
		}

		volumeProviderPurposes := make(map[string]struct{}, len(seedSpec.Volume.Providers))
		for i, provider := range seedSpec.Volume.Providers {
			idxPath := fldPath.Child("volume", "providers").Index(i)
			if len(provider.Purpose) == 0 {
				allErrs = append(allErrs, field.Required(idxPath.Child("purpose"), "cannot be empty"))
			}
			if len(provider.Name) == 0 {
				allErrs = append(allErrs, field.Required(idxPath.Child("name"), "cannot be empty"))
			}
			if _, ok := volumeProviderPurposes[provider.Purpose]; ok {
				allErrs = append(allErrs, field.Duplicate(idxPath.Child("purpose"), provider.Purpose))
			}
			volumeProviderPurposes[provider.Purpose] = struct{}{}
		}
	}

	if seedSpec.Settings != nil && seedSpec.Settings.LoadBalancerServices != nil {
		allErrs = append(allErrs, apivalidation.ValidateAnnotations(seedSpec.Settings.LoadBalancerServices.Annotations, fldPath.Child("settings", "loadBalancerServices", "annotations"))...)
	}

	if seedSpec.DNS.IngressDomain != nil {
		allErrs = append(allErrs, validateDNS1123Subdomain(*seedSpec.DNS.IngressDomain, fldPath.Child("dns", "ingressDomain"))...)
	}

	if !inTemplate && seedSpec.DNS.IngressDomain == nil && (seedSpec.Ingress == nil || len(seedSpec.Ingress.Domain) == 0) {
		allErrs = append(allErrs, field.Invalid(fldPath, seedSpec, "either specify spec.ingress or spec.dns.ingressDomain"))
	}

	if seedSpec.Ingress != nil {
		if seedSpec.DNS.IngressDomain != nil {
			allErrs = append(allErrs, field.Invalid(fldPath.Child("dns", "ingressDomain"), "",
				"Either specify spec.ingress.domain or spec.dns.ingressDomain"),
			)
		} else {
			if !availableIngressKinds.Has(seedSpec.Ingress.Controller.Kind) {
				allErrs = append(allErrs, field.NotSupported(
					fldPath.Child("ingress", "controller", "kind"),
					seedSpec.Ingress.Controller.Kind,
					availableIngressKinds.UnsortedList()),
				)
			}
			if seedSpec.DNS.Provider == nil {
				allErrs = append(allErrs, field.Required(fldPath.Child("dns", "provider"),
					"ingress controller requires dns.provider to be set"))
			} else {
				if len(seedSpec.DNS.Provider.Type) == 0 {
					allErrs = append(allErrs, field.Required(fldPath.Child("dns", "provider", "type"),
						"DNS provider type must be set"))
				}
				if len(seedSpec.DNS.Provider.SecretRef.Name) == 0 {
					allErrs = append(allErrs, field.Required(fldPath.Child("dns", "provider", "secretRef", "name"),
						"secret reference name must be set"))
				}
				if len(seedSpec.DNS.Provider.SecretRef.Namespace) == 0 {
					allErrs = append(allErrs, field.Required(fldPath.Child("dns", "provider", "secretRef", "namespace"),
						"secret reference namespace must be set"))
				}
			}
			allErrs = append(allErrs, validateDNS1123Subdomain(seedSpec.Ingress.Domain, fldPath.Child("ingress", "domain"))...)
		}
	}

	return allErrs
}

// ValidateSeedSpecUpdate validates the specification updates of a Seed object.
func ValidateSeedSpecUpdate(newSeedSpec, oldSeedSpec *core.SeedSpec, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	allErrs = append(allErrs, apivalidation.ValidateImmutableField(newSeedSpec.Networks.Pods, oldSeedSpec.Networks.Pods, fldPath.Child("networks", "pods"))...)
	allErrs = append(allErrs, apivalidation.ValidateImmutableField(newSeedSpec.Networks.Services, oldSeedSpec.Networks.Services, fldPath.Child("networks", "services"))...)
	if oldSeedSpec.Networks.Nodes != nil {
		allErrs = append(allErrs, apivalidation.ValidateImmutableField(newSeedSpec.Networks.Nodes, oldSeedSpec.Networks.Nodes, fldPath.Child("networks", "nodes"))...)
	}

	if oldSeedSpec.DNS.IngressDomain != nil && newSeedSpec.DNS.IngressDomain != nil {
		allErrs = append(allErrs, apivalidation.ValidateImmutableField(*newSeedSpec.DNS.IngressDomain, *oldSeedSpec.DNS.IngressDomain, fldPath.Child("dns", "ingressDomain"))...)
	}
	if oldSeedSpec.Ingress != nil && newSeedSpec.Ingress != nil {
		allErrs = append(allErrs, apivalidation.ValidateImmutableField(newSeedSpec.Ingress.Domain, oldSeedSpec.Ingress.Domain, fldPath.Child("ingress", "domain"))...)
	}
	if oldSeedSpec.Ingress != nil && newSeedSpec.DNS.IngressDomain != nil {
		allErrs = append(allErrs, apivalidation.ValidateImmutableField(*newSeedSpec.DNS.IngressDomain, oldSeedSpec.Ingress.Domain, fldPath.Child("dns", "ingressDomain"))...)
	}
	if oldSeedSpec.DNS.IngressDomain != nil && newSeedSpec.Ingress != nil {
		allErrs = append(allErrs, apivalidation.ValidateImmutableField(newSeedSpec.Ingress.Domain, *oldSeedSpec.DNS.IngressDomain, fldPath.Child("ingress", "domain"))...)
	}

	if oldSeedSpec.Backup != nil {
		if newSeedSpec.Backup != nil {
			allErrs = append(allErrs, apivalidation.ValidateImmutableField(newSeedSpec.Backup.Provider, oldSeedSpec.Backup.Provider, fldPath.Child("backup", "provider"))...)
			allErrs = append(allErrs, apivalidation.ValidateImmutableField(newSeedSpec.Backup.Region, oldSeedSpec.Backup.Region, fldPath.Child("backup", "region"))...)
		} else {
			allErrs = append(allErrs, apivalidation.ValidateImmutableField(newSeedSpec.Backup, oldSeedSpec.Backup, fldPath.Child("backup"))...)
		}
	}
	// If oldSeedSpec doesn't have backup configured, we allow to add it; but not the vice versa.

	return allErrs
}

// ValidateSeedStatusUpdate validates the status field of a Seed object.
func ValidateSeedStatusUpdate(newSeed, oldSeed *core.Seed) field.ErrorList {
	var (
		allErrs   = field.ErrorList{}
		fldPath   = field.NewPath("status")
		oldStatus = oldSeed.Status
		newStatus = newSeed.Status
	)

	if oldStatus.ClusterIdentity != nil && !apiequality.Semantic.DeepEqual(oldStatus.ClusterIdentity, newStatus.ClusterIdentity) {
		allErrs = append(allErrs, apivalidation.ValidateImmutableField(newStatus.ClusterIdentity, oldStatus.ClusterIdentity, fldPath.Child("clusterIdentity"))...)
	}

	return allErrs
}
