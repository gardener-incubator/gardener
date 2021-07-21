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

package botanist

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/gardener/gardener/charts"
	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	"github.com/gardener/gardener/pkg/client/kubernetes"
	"github.com/gardener/gardener/pkg/controllerutils"
	"github.com/gardener/gardener/pkg/features"
	gardenletfeatures "github.com/gardener/gardener/pkg/gardenlet/features"
	"github.com/gardener/gardener/pkg/operation/botanist/component"
	"github.com/gardener/gardener/pkg/operation/common"
	"github.com/gardener/gardener/pkg/utils"
	gutil "github.com/gardener/gardener/pkg/utils/gardener"
	kutil "github.com/gardener/gardener/pkg/utils/kubernetes"
	"github.com/gardener/gardener/pkg/utils/secrets"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	autoscalingv1beta2 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DeploySeedMonitoring will install the Helm release "seed-monitoring" in the Seed clusters. It comprises components
// to monitor the Shoot cluster whose control plane runs in the Seed cluster.
func (b *Botanist) DeploySeedMonitoring(ctx context.Context) error {
	if b.Shoot.Purpose == gardencorev1beta1.ShootPurposeTesting {
		return b.DeleteSeedMonitoring(ctx)
	}

	var (
		credentials         = b.Secrets[common.MonitoringIngressCredentials]
		credentialsUsers    = b.Secrets[common.MonitoringIngressCredentialsUsers]
		basicAuth           = utils.CreateSHA1Secret(credentials.Data[secrets.DataKeyUserName], credentials.Data[secrets.DataKeyPassword])
		basicAuthUsers      = utils.CreateSHA1Secret(credentialsUsers.Data[secrets.DataKeyUserName], credentialsUsers.Data[secrets.DataKeyPassword])
		alertingRules       = strings.Builder{}
		scrapeConfigs       = strings.Builder{}
		operatorsDashboards = strings.Builder{}
		usersDashboards     = strings.Builder{}
	)

	// Fetch component-specific monitoring configuration
	monitoringComponents := []component.MonitoringComponent{
		b.Shoot.Components.ControlPlane.EtcdMain,
		b.Shoot.Components.ControlPlane.EtcdEvents,
		b.Shoot.Components.ControlPlane.KubeScheduler,
		b.Shoot.Components.ControlPlane.KubeControllerManager,
	}

	if b.Shoot.WantsClusterAutoscaler {
		monitoringComponents = append(monitoringComponents, b.Shoot.Components.ControlPlane.ClusterAutoscaler)
	}

	for _, component := range monitoringComponents {
		componentsScrapeConfigs, err := component.ScrapeConfigs()
		if err != nil {
			return err
		}
		for _, config := range componentsScrapeConfigs {
			scrapeConfigs.WriteString(fmt.Sprintf("- %s\n", utils.Indent(config, 2)))
		}

		componentsAlertingRules, err := component.AlertingRules()
		if err != nil {
			return err
		}
		for filename, rule := range componentsAlertingRules {
			alertingRules.WriteString(fmt.Sprintf("%s: |\n  %s\n", filename, utils.Indent(rule, 2)))
		}
	}

	// Fetch extensions provider-specific monitoring configuration
	existingConfigMaps := &corev1.ConfigMapList{}
	if err := b.K8sSeedClient.Client().List(ctx, existingConfigMaps,
		client.InNamespace(b.Shoot.SeedNamespace),
		client.MatchingLabels{v1beta1constants.LabelExtensionConfiguration: v1beta1constants.LabelMonitoring}); err != nil {
		return err
	}

	// Need stable order before passing the dashboards to Grafana config to avoid unnecessary changes
	kutil.ByName().Sort(existingConfigMaps)

	// Read extension monitoring configurations
	for _, cm := range existingConfigMaps.Items {
		alertingRules.WriteString(fmt.Sprintln(cm.Data[v1beta1constants.PrometheusConfigMapAlertingRules]))
		scrapeConfigs.WriteString(fmt.Sprintln(cm.Data[v1beta1constants.PrometheusConfigMapScrapeConfig]))
		operatorsDashboards.WriteString(fmt.Sprintln(cm.Data[v1beta1constants.GrafanaConfigMapOperatorDashboard]))
		usersDashboards.WriteString(fmt.Sprintln(cm.Data[v1beta1constants.GrafanaConfigMapUserDashboard]))
	}

	alerting, err := b.getCustomAlertingConfigs(ctx, b.GetSecretKeysOfRole(v1beta1constants.GardenRoleAlerting))
	if err != nil {
		return err
	}

	prometheusTLSOverride := common.PrometheusTLS
	if b.ControlPlaneWildcardCert != nil {
		prometheusTLSOverride = b.ControlPlaneWildcardCert.GetName()
	}

	hosts := []map[string]interface{}{
		{
			"hostName":   b.ComputePrometheusHost(),
			"secretName": prometheusTLSOverride,
		},
	}

	var (
		networks = map[string]interface{}{
			"pods":     b.Shoot.Networks.Pods.String(),
			"services": b.Shoot.Networks.Services.String(),
		}
		prometheusConfig = map[string]interface{}{
			"kubernetesVersion": b.Shoot.Info.Spec.Kubernetes.Version,
			"nodeLocalDNS": map[string]interface{}{
				"enabled": b.Shoot.NodeLocalDNSEnabled,
			},
			"ingress": map[string]interface{}{
				"class":           getIngressClass(b.Seed.Info.Spec.Ingress),
				"basicAuthSecret": basicAuth,
				"hosts":           hosts,
			},
			"namespace": map[string]interface{}{
				"uid": b.SeedNamespaceObject.UID,
			},
			"replicas":           b.Shoot.GetReplicas(1),
			"apiserverServiceIP": b.Shoot.Networks.APIServer.String(),
			"seed": map[string]interface{}{
				"apiserver": b.K8sSeedClient.RESTConfig().Host,
				"region":    b.Seed.Info.Spec.Provider.Region,
				"provider":  b.Seed.Info.Spec.Provider.Type,
			},
			"rules": map[string]interface{}{
				"optional": map[string]interface{}{
					"alertmanager": map[string]interface{}{
						"enabled": b.Shoot.WantsAlertmanager,
					},
					"loki": map[string]interface{}{
						"enabled": gardenletfeatures.FeatureGate.Enabled(features.Logging),
					},
					"lokiTelegraf": map[string]interface{}{
						"enabled": b.isShootNodeLoggingEnabled(),
					},
					"hvpa": map[string]interface{}{
						"enabled": gardenletfeatures.FeatureGate.Enabled(features.HVPA),
					},
				},
			},
			"shoot": map[string]interface{}{
				"apiserver":           fmt.Sprintf("https://%s", gutil.GetAPIServerDomain(b.Shoot.InternalClusterDomain)),
				"apiserverServerName": gutil.GetAPIServerDomain(b.Shoot.InternalClusterDomain),
				"sniEnabled":          b.APIServerSNIEnabled(),
				"provider":            b.Shoot.Info.Spec.Provider.Type,
				"name":                b.Shoot.Info.Name,
				"project":             b.Garden.Project.Name,
			},
			"ignoreAlerts":            b.Shoot.IgnoreAlerts,
			"alerting":                alerting,
			"additionalRules":         alertingRules.String(),
			"additionalScrapeConfigs": scrapeConfigs.String(),
		}
		kubeStateMetricsShootConfig = map[string]interface{}{
			"replicas": b.Shoot.GetReplicas(1),
		}
	)

	if v := b.Shoot.GetNodeNetwork(); v != nil {
		networks["nodes"] = *v
	}
	prometheusConfig["networks"] = networks

	var (
		prometheusImages = []string{
			charts.ImageNamePrometheus,
			charts.ImageNameConfigmapReloader,
			charts.ImageNameBlackboxExporter,
		}
		podAnnotations = map[string]interface{}{
			"checksum/secret-prometheus": b.CheckSums["prometheus"],
		}
	)

	prometheusConfig["podAnnotations"] = podAnnotations

	prometheus, err := b.InjectSeedShootImages(prometheusConfig, prometheusImages...)
	if err != nil {
		return err
	}
	kubeStateMetricsShoot, err := b.InjectSeedShootImages(kubeStateMetricsShootConfig, charts.ImageNameKubeStateMetrics)
	if err != nil {
		return err
	}

	coreValues := map[string]interface{}{
		"global": map[string]interface{}{
			"shootKubeVersion": map[string]interface{}{
				"gitVersion": b.Shoot.Info.Spec.Kubernetes.Version,
			},
		},
		"prometheus":               prometheus,
		"kube-state-metrics-shoot": kubeStateMetricsShoot,
	}

	if err := b.K8sSeedClient.ChartApplier().Apply(ctx, filepath.Join(charts.Path, "seed-monitoring", "charts", "core"), b.Shoot.SeedNamespace, fmt.Sprintf("%s-monitoring", b.Shoot.SeedNamespace), kubernetes.Values(coreValues)); err != nil {
		return err
	}

	if err := b.deployGrafanaCharts(ctx, common.GrafanaOperatorsRole, operatorsDashboards.String(), basicAuth, common.GrafanaOperatorsPrefix); err != nil {
		return err
	}

	if err := b.deployGrafanaCharts(ctx, common.GrafanaUsersRole, usersDashboards.String(), basicAuthUsers, common.GrafanaUsersPrefix); err != nil {
		return err
	}

	// Check if we want to deploy an alertmanager into the shoot namespace.
	if b.Shoot.WantsAlertmanager {
		var (
			alertingSMTPKeys = b.GetSecretKeysOfRole(v1beta1constants.GardenRoleAlerting)
			emailConfigs     = []map[string]interface{}{}
		)

		if b.Shoot.Info.Spec.Monitoring != nil && b.Shoot.Info.Spec.Monitoring.Alerting != nil {
			for _, email := range b.Shoot.Info.Spec.Monitoring.Alerting.EmailReceivers {
				for _, key := range alertingSMTPKeys {
					secret := b.Secrets[key]

					if string(secret.Data["auth_type"]) != "smtp" {
						continue
					}
					emailConfigs = append(emailConfigs, map[string]interface{}{
						"to":            email,
						"from":          string(secret.Data["from"]),
						"smarthost":     string(secret.Data["smarthost"]),
						"auth_username": string(secret.Data["auth_username"]),
						"auth_identity": string(secret.Data["auth_identity"]),
						"auth_password": string(secret.Data["auth_password"]),
					})
				}
			}
		}

		alertManagerTLSOverride := common.AlertManagerTLS
		if b.ControlPlaneWildcardCert != nil {
			alertManagerTLSOverride = b.ControlPlaneWildcardCert.GetName()
		}

		hosts := []map[string]interface{}{
			{
				"hostName":   b.ComputeAlertManagerHost(),
				"secretName": alertManagerTLSOverride,
			},
		}

		alertManagerValues, err := b.InjectSeedShootImages(map[string]interface{}{
			"ingress": map[string]interface{}{
				"class":           getIngressClass(b.Seed.Info.Spec.Ingress),
				"basicAuthSecret": basicAuthUsers,
				"hosts":           hosts,
			},
			"replicas":     b.Shoot.GetReplicas(1),
			"storage":      b.Seed.GetValidVolumeSize("1Gi"),
			"emailConfigs": emailConfigs,
		}, charts.ImageNameAlertmanager, charts.ImageNameConfigmapReloader)
		if err != nil {
			return err
		}
		if err := b.K8sSeedClient.ChartApplier().Apply(ctx, filepath.Join(charts.Path, "seed-monitoring", "charts", "alertmanager"), b.Shoot.SeedNamespace, fmt.Sprintf("%s-monitoring", b.Shoot.SeedNamespace), kubernetes.Values(alertManagerValues)); err != nil {
			return err
		}
	} else {
		if err := common.DeleteAlertmanager(ctx, b.K8sSeedClient.Client(), b.Shoot.SeedNamespace); err != nil {
			return err
		}
	}

	return nil
}

func getIngressClass(ingress *gardencorev1beta1.Ingress) string {
	if ingress != nil && ingress.Controller.Kind == v1beta1constants.IngressKindNginx {
		return v1beta1constants.SeedNginxIngressClass
	}
	return v1beta1constants.ShootNginxIngressClass
}

func (b *Botanist) getCustomAlertingConfigs(ctx context.Context, alertingSecretKeys []string) (map[string]interface{}, error) {
	configs := map[string]interface{}{
		"auth_type": map[string]interface{}{},
	}

	for _, key := range alertingSecretKeys {
		secret := b.Secrets[key]

		if string(secret.Data["auth_type"]) == "none" {

			if url, ok := secret.Data["url"]; ok {
				configs["auth_type"] = map[string]interface{}{
					"none": map[string]interface{}{
						"url": string(url),
					},
				}
			}
			break
		}

		if string(secret.Data["auth_type"]) == "basic" {
			url, urlOk := secret.Data["url"]
			username, usernameOk := secret.Data["username"]
			password, passwordOk := secret.Data["password"]

			if urlOk && usernameOk && passwordOk {
				configs["auth_type"] = map[string]interface{}{
					"basic": map[string]interface{}{
						"url":      string(url),
						"username": string(username),
						"password": string(password),
					},
				}
			}
			break
		}

		if string(secret.Data["auth_type"]) == "certificate" {
			data := map[string][]byte{}
			url, urlOk := secret.Data["url"]
			ca, caOk := secret.Data["ca.crt"]
			cert, certOk := secret.Data["tls.crt"]
			key, keyOk := secret.Data["tls.key"]
			insecure, insecureOk := secret.Data["insecure_skip_verify"]

			if urlOk && caOk && certOk && keyOk && insecureOk {
				configs["auth_type"] = map[string]interface{}{
					"certificate": map[string]interface{}{
						"url":                  string(url),
						"insecure_skip_verify": string(insecure),
					},
				}
				data["ca.crt"] = ca
				data["tls.crt"] = cert
				data["tls.key"] = key
				amSecret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "prometheus-remote-am-tls",
						Namespace: b.Shoot.SeedNamespace,
					},
				}

				if _, err := controllerutils.GetAndCreateOrMergePatch(ctx, b.K8sSeedClient.Client(), amSecret, func() error {
					amSecret.Data = data
					amSecret.Type = corev1.SecretTypeOpaque
					return nil
				}); err != nil {
					return nil, err
				}
			}
			break
		}
	}

	return configs, nil
}

func (b *Botanist) deployGrafanaCharts(ctx context.Context, role, dashboards, basicAuth, subDomain string) error {
	grafanaTLSOverride := common.GrafanaTLS
	if b.ControlPlaneWildcardCert != nil {
		grafanaTLSOverride = b.ControlPlaneWildcardCert.GetName()
	}

	hosts := []map[string]interface{}{
		{
			"hostName":   b.ComputeIngressHost(subDomain),
			"secretName": grafanaTLSOverride,
		},
	}

	values, err := b.InjectSeedShootImages(map[string]interface{}{
		"ingress": map[string]interface{}{
			"class":           getIngressClass(b.Seed.Info.Spec.Ingress),
			"basicAuthSecret": basicAuth,
			"hosts":           hosts,
		},
		"replicas": b.Shoot.GetReplicas(1),
		"role":     role,
		"extensions": map[string]interface{}{
			"dashboards": dashboards,
		},
		"vpaEnabled": b.Shoot.WantsVerticalPodAutoscaler,
		"sni": map[string]interface{}{
			"enabled": b.APIServerSNIEnabled(),
		},
		"nodeLocalDNS": map[string]interface{}{
			"enabled": b.Shoot.NodeLocalDNSEnabled,
		},
	}, charts.ImageNameGrafana, charts.ImageNameBusybox)
	if err != nil {
		return err
	}
	return b.K8sSeedClient.ChartApplier().Apply(ctx, filepath.Join(charts.Path, "seed-monitoring", "charts", "grafana"), b.Shoot.SeedNamespace, fmt.Sprintf("%s-monitoring", b.Shoot.SeedNamespace), kubernetes.Values(values))
}

// DeleteGrafana will delete all grafana instances from the seed cluster.
func (b *Botanist) DeleteGrafana(ctx context.Context) error {
	if err := common.DeleteGrafanaByRole(ctx, b.K8sSeedClient, b.Shoot.SeedNamespace, common.GrafanaOperatorsRole); err != nil {
		return err
	}

	return common.DeleteGrafanaByRole(ctx, b.K8sSeedClient, b.Shoot.SeedNamespace, common.GrafanaUsersRole)
}

// DeleteSeedMonitoring will delete the monitoring stack from the Seed cluster to avoid phantom alerts
// during the deletion process. More precisely, the Alertmanager and Prometheus StatefulSets will be
// deleted.
func (b *Botanist) DeleteSeedMonitoring(ctx context.Context) error {
	if err := common.DeleteAlertmanager(ctx, b.K8sSeedClient.Client(), b.Shoot.SeedNamespace); err != nil {
		return err
	}

	objects := []client.Object{
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: b.Shoot.SeedNamespace,
				Name:      "kube-state-metrics",
			},
		},
		&autoscalingv1beta2.VerticalPodAutoscaler{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: b.Shoot.SeedNamespace,
				Name:      "kube-state-metrics-vpa",
			},
		},
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: b.Shoot.SeedNamespace,
				Name:      "kube-state-metrics",
			},
		},

		&networkingv1.NetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: b.Shoot.SeedNamespace,
				Name:      "allow-from-prometheus",
			},
		},
		&networkingv1.NetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: b.Shoot.SeedNamespace,
				Name:      "allow-prometheus",
			},
		},
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: b.Shoot.SeedNamespace,
				Name:      "prometheus-config",
			},
		},
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: b.Shoot.SeedNamespace,
				Name:      "prometheus-rules",
			},
		},
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: b.Shoot.SeedNamespace,
				Name:      "blackbox-exporter-config-prometheus",
			},
		},
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: b.Shoot.SeedNamespace,
				Name:      "prometheus-basic-auth",
			},
		},
		&extensionsv1beta1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: b.Shoot.SeedNamespace,
				Name:      "prometheus",
			},
		},
		&autoscalingv1beta2.VerticalPodAutoscaler{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: b.Shoot.SeedNamespace,
				Name:      "prometheus-vpa",
			},
		},
		&corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: b.Shoot.SeedNamespace,
				Name:      "prometheus",
			},
		},
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: b.Shoot.SeedNamespace,
				Name:      "prometheus-web",
			},
		},
		&appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: b.Shoot.SeedNamespace,
				Name:      "prometheus",
			},
		},
		&rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: b.Shoot.SeedNamespace,
				Name:      "prometheus-" + b.Shoot.SeedNamespace,
			},
		},
		&corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: b.Shoot.SeedNamespace,
				Name:      "prometheus-db-prometheus-0",
			},
		},
	}

	return kutil.DeleteObjects(ctx, b.K8sSeedClient.Client(), objects...)
}
