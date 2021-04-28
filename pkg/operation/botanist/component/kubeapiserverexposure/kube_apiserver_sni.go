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

package kubeapiserverexposure

import (
	"context"
	"fmt"

	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	"github.com/gardener/gardener/pkg/operation/botanist/component"
	kutil "github.com/gardener/gardener/pkg/utils/kubernetes"

	istioapinetworkingv1beta1 "istio.io/api/networking/v1beta1"
	istionetworkingv1beta1 "istio.io/client-go/pkg/apis/networking/v1beta1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	gatewayName = v1beta1constants.DeploymentNameKubeAPIServer
)

// SNIValues configure the kube-apiserver service SNI.
type SNIValues struct {
	Hosts                    []string
	NamespaceUID             types.UID
	ApiserverClusterIP       string
	IstioIngressGateway      IstioIngressGateway
	InternalDNSNameApiserver string
	ReversedVPN              ReversedVPN
}

// IstioIngressGateway contains the values for istio ingress gateway configuration.
type IstioIngressGateway struct {
	Namespace string
	Labels    map[string]string
}

// ReversedVPN contains whether the reversed vpn is enabled or not.
type ReversedVPN struct {
	Enabled bool
}

// NewSNI creates a new instance of DeployWaiter which deploys Istio resources for
// kube-apiserver SNI access.
func NewSNI(
	client client.Client,
	namespace string,
	values *SNIValues,
) component.DeployWaiter {
	if values == nil {
		values = &SNIValues{}
	}

	return &sni{
		client:    client,
		namespace: namespace,
		values:    values,
	}
}

type sni struct {
	client    client.Client
	namespace string
	values    *SNIValues
}

func (s *sni) Deploy(ctx context.Context) error {
	var (
		virtualService = s.emptyVirtualService()
	)

	if _, err := controllerutil.CreateOrUpdate(ctx, s.client, virtualService, func() error {
		virtualService.Labels = getLabels()
		virtualService.Spec = istioapinetworkingv1beta1.VirtualService{
			ExportTo: []string{"*"},
			Hosts:    s.values.Hosts,
			Gateways: []string{gatewayName},
			Tls: []*istioapinetworkingv1beta1.TLSRoute{{
				Match: []*istioapinetworkingv1beta1.TLSMatchAttributes{{
					Port:     443,
					SniHosts: s.values.Hosts,
				}},
				Route: []*istioapinetworkingv1beta1.RouteDestination{{
					Destination: &istioapinetworkingv1beta1.Destination{
						Host: fmt.Sprintf("%s.%s.svc.%s", v1beta1constants.DeploymentNameKubeAPIServer, s.namespace, gardencorev1beta1.DefaultDomain),
						Port: &istioapinetworkingv1beta1.PortSelector{Number: 443},
					},
				}},
			}},
		}
		return nil
	}); err != nil {
		return err
	}

	return nil
}

func (s *sni) Destroy(ctx context.Context) error {
	return kutil.DeleteObjects(
		ctx,
		s.client,
		s.emptyVirtualService(),
	)
}

func (s *sni) Wait(_ context.Context) error        { return nil }
func (s *sni) WaitCleanup(_ context.Context) error { return nil }

func (s *sni) emptyVirtualService() *istionetworkingv1beta1.VirtualService {
	return &istionetworkingv1beta1.VirtualService{ObjectMeta: metav1.ObjectMeta{Name: v1beta1constants.DeploymentNameKubeAPIServer, Namespace: s.namespace}}
}

// AnyDeployedSNI returns true if any SNI is deployed in the cluster.
func AnyDeployedSNI(ctx context.Context, c client.Client) (bool, error) {
	l := &unstructured.UnstructuredList{
		Object: map[string]interface{}{
			"apiVersion": istionetworkingv1beta1.SchemeGroupVersion.String(),
			"kind":       "VirtualServiceList",
		},
	}

	if err := c.List(ctx, l, client.MatchingFields{"metadata.name": "kube-apiserver"}, client.Limit(1)); err != nil && !meta.IsNoMatchError(err) {
		return false, err
	}

	return len(l.Items) > 0, nil
}
