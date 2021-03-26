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

package gardener

import (
	"fmt"
	"strings"
)

const (
	// DNSProvider is the key for an annotation on a Kubernetes Secret object whose value must point to a valid
	// DNS provider.
	DNSProvider = "dns.gardener.cloud/provider"
	// DNSDomain is the key for an annotation on a Kubernetes Secret object whose value must point to a valid
	// domain name.
	DNSDomain = "dns.gardener.cloud/domain"
	// DNSIncludeZones is the key for an annotation on a Kubernetes Secret object whose value must point to a list
	// of zones that shall be included.
	DNSIncludeZones = "dns.gardener.cloud/include-zones"
	// DNSExcludeZones is the key for an annotation on a Kubernetes Secret object whose value must point to a list
	// of zones that shall be excluded.
	DNSExcludeZones = "dns.gardener.cloud/exclude-zones"

	// APIServerFQDNPrefix is the part of a FQDN which will be used to construct the domain name for the kube-apiserver of
	// a Shoot cluster. For example, when a Shoot specifies domain 'cluster.example.com', the apiserver domain would be
	// 'api.cluster.example.com'.
	APIServerFQDNPrefix = "api"
	// IngressPrefix is the part of a FQDN which will be used to construct the domain name for an ingress controller of
	// a Shoot cluster. For example, when a Shoot specifies domain 'cluster.example.com', the ingress domain would be
	// '*.<IngressPrefix>.cluster.example.com'.
	IngressPrefix = "ingress"
	// InternalDomainKey is a key which must be present in an internal domain constructed for a Shoot cluster. If the
	// configured internal domain already contains it, it won't be added twice. If it does not contain it, it will be
	// appended.
	InternalDomainKey = "internal"
)

// GetDomainInfoFromAnnotations returns the provider and the domain that is specified in the give annotations.
func GetDomainInfoFromAnnotations(annotations map[string]string) (provider string, domain string, includeZones, excludeZones []string, err error) {
	if annotations == nil {
		return "", "", nil, nil, fmt.Errorf("domain secret has no annotations")
	}

	if providerAnnotation, ok := annotations[DNSProvider]; ok {
		provider = providerAnnotation
	}

	if domainAnnotation, ok := annotations[DNSDomain]; ok {
		domain = domainAnnotation
	}

	if includeZonesAnnotation, ok := annotations[DNSIncludeZones]; ok {
		includeZones = strings.Split(includeZonesAnnotation, ",")
	}
	if excludeZonesAnnotation, ok := annotations[DNSExcludeZones]; ok {
		excludeZones = strings.Split(excludeZonesAnnotation, ",")
	}

	if len(domain) == 0 {
		return "", "", nil, nil, fmt.Errorf("missing dns domain annotation on domain secret")
	}
	if len(provider) == 0 {
		return "", "", nil, nil, fmt.Errorf("missing dns provider annotation on domain secret")
	}

	return
}

// GetAPIServerDomain returns the fully qualified domain name of for the api-server for the Shoot cluster. The
// end result is 'api.<domain>'.
func GetAPIServerDomain(domain string) string {
	return fmt.Sprintf("%s.%s", APIServerFQDNPrefix, domain)
}
