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

package seed

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/gardener/gardener/charts"
	v1alpha1constants "github.com/gardener/gardener/pkg/apis/core/v1alpha1/constants"
	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	extensionsv1alpha1helper "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1/helper"
	gardencorelisters "github.com/gardener/gardener/pkg/client/core/listers/core/v1beta1"
	"github.com/gardener/gardener/pkg/client/kubernetes"
	"github.com/gardener/gardener/pkg/controllerutils"
	"github.com/gardener/gardener/pkg/features"
	"github.com/gardener/gardener/pkg/gardenlet/apis/config"
	gardenletfeatures "github.com/gardener/gardener/pkg/gardenlet/features"
	"github.com/gardener/gardener/pkg/operation/botanist/component"
	"github.com/gardener/gardener/pkg/operation/botanist/component/clusterautoscaler"
	"github.com/gardener/gardener/pkg/operation/botanist/component/clusteridentity"
	"github.com/gardener/gardener/pkg/operation/botanist/component/dependencywatchdog"
	"github.com/gardener/gardener/pkg/operation/botanist/component/etcd"
	"github.com/gardener/gardener/pkg/operation/botanist/component/extensions/dns"
	"github.com/gardener/gardener/pkg/operation/botanist/component/extensions/dnsrecord"
	"github.com/gardener/gardener/pkg/operation/botanist/component/gardenerkubescheduler"
	"github.com/gardener/gardener/pkg/operation/botanist/component/istio"
	"github.com/gardener/gardener/pkg/operation/botanist/component/kubeapiserverexposure"
	"github.com/gardener/gardener/pkg/operation/botanist/component/kubecontrollermanager"
	"github.com/gardener/gardener/pkg/operation/botanist/component/kubescheduler"
	"github.com/gardener/gardener/pkg/operation/botanist/component/metricsserver"
	"github.com/gardener/gardener/pkg/operation/botanist/component/networkpolicies"
	"github.com/gardener/gardener/pkg/operation/botanist/component/resourcemanager"
	"github.com/gardener/gardener/pkg/operation/botanist/component/seedadmissioncontroller"
	"github.com/gardener/gardener/pkg/operation/botanist/component/vpnseedserver"
	"github.com/gardener/gardener/pkg/operation/common"
	"github.com/gardener/gardener/pkg/utils"
	"github.com/gardener/gardener/pkg/utils/flow"
	gutil "github.com/gardener/gardener/pkg/utils/gardener"
	"github.com/gardener/gardener/pkg/utils/imagevector"
	kutil "github.com/gardener/gardener/pkg/utils/kubernetes"
	secretsutils "github.com/gardener/gardener/pkg/utils/secrets"
	versionutils "github.com/gardener/gardener/pkg/utils/version"

	"github.com/Masterminds/semver"
	dnsv1alpha1 "github.com/gardener/external-dns-management/pkg/apis/dns/v1alpha1"
	"github.com/sirupsen/logrus"
	istiov1beta1 "istio.io/client-go/pkg/apis/networking/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	policyv1beta1 "k8s.io/api/policy/v1beta1"
	rbacv1 "k8s.io/api/rbac/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
	autoscalingv1beta2 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1beta2"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// NewBuilder returns a new Builder.
func NewBuilder() *Builder {
	return &Builder{
		seedObjectFunc: func() (*gardencorev1beta1.Seed, error) { return nil, fmt.Errorf("seed object is required but not set") },
	}
}

// WithSeedObject sets the seedObjectFunc attribute at the Builder.
func (b *Builder) WithSeedObject(seedObject *gardencorev1beta1.Seed) *Builder {
	b.seedObjectFunc = func() (*gardencorev1beta1.Seed, error) { return seedObject, nil }
	return b
}

// WithSeedObjectFromLister sets the seedObjectFunc attribute at the Builder after fetching it from the given lister.
func (b *Builder) WithSeedObjectFromLister(seedLister gardencorelisters.SeedLister, seedName string) *Builder {
	b.seedObjectFunc = func() (*gardencorev1beta1.Seed, error) { return seedLister.Get(seedName) }
	return b
}

// Build initializes a new Seed object.
func (b *Builder) Build() (*Seed, error) {
	seed := &Seed{}

	seedObject, err := b.seedObjectFunc()
	if err != nil {
		return nil, err
	}
	seed.Info = seedObject

	if seedObject.Spec.Settings != nil && seedObject.Spec.Settings.LoadBalancerServices != nil {
		seed.LoadBalancerServiceAnnotations = seedObject.Spec.Settings.LoadBalancerServices.Annotations
	}

	return seed, nil
}

const (
	caSeed = "ca-seed"
)

var wantedCertificateAuthorities = map[string]*secretsutils.CertificateSecretConfig{
	caSeed: {
		Name:       caSeed,
		CommonName: "kubernetes",
		CertType:   secretsutils.CACert,
	},
}

const (
	grafanaPrefix = "g-seed"
	grafanaTLS    = "grafana-tls"

	prometheusPrefix = "p-seed"
	prometheusTLS    = "aggregate-prometheus-tls"

	userExposedComponentTagPrefix = "user-exposed"
)

// generateWantedSecrets returns a list of Secret configuration objects satisfying the secret config intface,
// each containing their specific configuration for the creation of certificates (server/client), RSA key pairs, basic
// authentication credentials, etc.
func generateWantedSecrets(seed *Seed, certificateAuthorities map[string]*secretsutils.Certificate) ([]secretsutils.ConfigInterface, error) {
	if len(certificateAuthorities) != len(wantedCertificateAuthorities) {
		return nil, fmt.Errorf("missing certificate authorities")
	}

	endUserCrtValidity := common.EndUserCrtValidity

	secretList := []secretsutils.ConfigInterface{
		&secretsutils.CertificateSecretConfig{
			Name: common.VPASecretName,

			CommonName:   "vpa-webhook.garden.svc",
			Organization: nil,
			DNSNames:     []string{"vpa-webhook.garden.svc", "vpa-webhook"},
			IPAddresses:  nil,

			CertType:  secretsutils.ServerCert,
			SigningCA: certificateAuthorities[caSeed],
		},
		&secretsutils.CertificateSecretConfig{
			Name: common.GrafanaTLS,

			CommonName:   "grafana",
			Organization: []string{"gardener.cloud:monitoring:ingress"},
			DNSNames:     []string{seed.GetIngressFQDN(grafanaPrefix)},
			IPAddresses:  nil,

			CertType:  secretsutils.ServerCert,
			SigningCA: certificateAuthorities[caSeed],
			Validity:  &endUserCrtValidity,
		},
		&secretsutils.CertificateSecretConfig{
			Name: prometheusTLS,

			CommonName:   "prometheus",
			Organization: []string{"gardener.cloud:monitoring:ingress"},
			DNSNames:     []string{seed.GetIngressFQDN(prometheusPrefix)},
			IPAddresses:  nil,

			CertType:  secretsutils.ServerCert,
			SigningCA: certificateAuthorities[caSeed],
			Validity:  &endUserCrtValidity,
		},
	}

	return secretList, nil
}

// deployCertificates deploys CA and TLS certificates inside the garden namespace
// It takes a map[string]*corev1.Secret object which contains secrets that have already been deployed inside that namespace to avoid duplication errors.
func deployCertificates(ctx context.Context, seed *Seed, k8sSeedClient kubernetes.Interface, existingSecretsMap map[string]*corev1.Secret) (map[string]*corev1.Secret, error) {
	_, certificateAuthorities, err := secretsutils.GenerateCertificateAuthorities(ctx, k8sSeedClient, existingSecretsMap, wantedCertificateAuthorities, v1beta1constants.GardenNamespace)
	if err != nil {
		return nil, err
	}

	wantedSecretsList, err := generateWantedSecrets(seed, certificateAuthorities)
	if err != nil {
		return nil, err
	}

	// Only necessary to renew certificates for Grafana, Prometheus
	// TODO: (timuthy) remove in future version.
	var (
		renewedLabel = "cert.gardener.cloud/renewed-endpoint"
		browserCerts = sets.NewString(grafanaTLS, prometheusTLS)
	)
	for name, secret := range existingSecretsMap {
		_, ok := secret.Labels[renewedLabel]
		if browserCerts.Has(name) && !ok {
			if err := k8sSeedClient.Client().Delete(ctx, secret); client.IgnoreNotFound(err) != nil {
				return nil, err
			}
			delete(existingSecretsMap, name)
		}
	}

	secrets, err := secretsutils.GenerateClusterSecrets(ctx, k8sSeedClient, existingSecretsMap, wantedSecretsList, v1beta1constants.GardenNamespace)
	if err != nil {
		return nil, err
	}

	// Only necessary to renew certificates for Grafana, Prometheus
	// TODO: (timuthy) remove in future version.
	for name, secret := range secrets {
		_, ok := secret.Labels[renewedLabel]
		if browserCerts.Has(name) && !ok {
			if secret.Labels == nil {
				secret.Labels = make(map[string]string)
			}
			secret.Labels[renewedLabel] = "true"

			if err := k8sSeedClient.Client().Update(ctx, secret); err != nil {
				return nil, err
			}
		}
	}

	return secrets, nil
}

// RunReconcileSeedFlow bootstraps a Seed cluster and deploys various required manifests.
func RunReconcileSeedFlow(
	ctx context.Context,
	k8sGardenClient kubernetes.Interface,
	k8sSeedClient kubernetes.Interface,
	seed *Seed,
	secrets map[string]*corev1.Secret,
	imageVector imagevector.ImageVector,
	componentImageVectors imagevector.ComponentImageVectors,
	conf *config.GardenletConfiguration,
	seedLogger *logrus.Entry,
) error {
	vpaGK := schema.GroupKind{Group: "autoscaling.k8s.io", Kind: "VerticalPodAutoscaler"}

	vpaEnabled := seed.Info.Spec.Settings == nil || seed.Info.Spec.Settings.VerticalPodAutoscaler == nil || seed.Info.Spec.Settings.VerticalPodAutoscaler.Enabled
	if !vpaEnabled {
		// VPA is a prerequisite. If it's not enabled via the seed spec it must be provided through some other mechanism.
		if _, err := k8sSeedClient.Client().RESTMapper().RESTMapping(vpaGK); err != nil {
			return fmt.Errorf("VPA is required for seed cluster: %s", err)
		}

		if err := common.DeleteVpa(ctx, k8sSeedClient.Client(), v1beta1constants.GardenNamespace, false); client.IgnoreNotFound(err) != nil {
			return err
		}
	}

	const chartName = "seed-bootstrap"
	var (
		loggingConfig   = conf.Logging
		gardenNamespace = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: v1beta1constants.GardenNamespace,
			},
		}
	)

	// create + label garden namespace
	if _, err := controllerutils.CreateOrGetAndMergePatch(ctx, k8sSeedClient.Client(), gardenNamespace, func() error {
		kutil.SetMetaDataLabel(&gardenNamespace.ObjectMeta, "role", v1beta1constants.GardenNamespace)
		return nil
	}); err != nil {
		return err
	}

	// label kube-system namespace
	namespaceKubeSystem := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: metav1.NamespaceSystem}}
	patch := client.MergeFrom(namespaceKubeSystem.DeepCopy())
	kutil.SetMetaDataLabel(&namespaceKubeSystem.ObjectMeta, "role", metav1.NamespaceSystem)
	if err := k8sSeedClient.Client().Patch(ctx, namespaceKubeSystem, patch); err != nil {
		return err
	}

	if monitoringSecrets := common.GetSecretKeysWithPrefix(v1beta1constants.GardenRoleGlobalMonitoring, secrets); len(monitoringSecrets) > 0 {
		for _, key := range monitoringSecrets {
			secret := secrets[key]
			secretObj := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("%s-%s", "seed", secret.Name),
					Namespace: "garden",
				},
			}

			if _, err := controllerutils.GetAndCreateOrMergePatch(ctx, k8sSeedClient.Client(), secretObj, func() error {
				secretObj.Type = corev1.SecretTypeOpaque
				secretObj.Data = secret.Data
				return nil
			}); err != nil {
				return err
			}
		}
	}

	images, err := imagevector.FindImages(imageVector,
		[]string{
			charts.ImageNameAlertmanager,
			charts.ImageNameAlpine,
			charts.ImageNameConfigmapReloader,
			charts.ImageNameLoki,
			charts.ImageNameLokiCurator,
			charts.ImageNameFluentBit,
			charts.ImageNameFluentBitPluginInstaller,
			charts.ImageNameGardenerResourceManager,
			charts.ImageNameGrafana,
			charts.ImageNamePauseContainer,
			charts.ImageNamePrometheus,
			charts.ImageNameVpaAdmissionController,
			charts.ImageNameVpaExporter,
			charts.ImageNameVpaRecommender,
			charts.ImageNameVpaUpdater,
			charts.ImageNameHvpaController,
			charts.ImageNameKubeStateMetrics,
			charts.ImageNameNginxIngressControllerSeed,
			charts.ImageNameIngressDefaultBackend,
		},
		imagevector.RuntimeVersion(k8sSeedClient.Version()),
		imagevector.TargetVersion(k8sSeedClient.Version()),
	)
	if err != nil {
		return err
	}

	// HVPA feature gate
	hvpaEnabled := gardenletfeatures.FeatureGate.Enabled(features.HVPA)
	if !hvpaEnabled {
		if err := common.DeleteHvpa(ctx, k8sSeedClient, v1beta1constants.GardenNamespace); client.IgnoreNotFound(err) != nil {
			return err
		}
	} else {
		// Clean up stale vpa objects
		resources := []client.Object{
			&autoscalingv1beta2.VerticalPodAutoscaler{ObjectMeta: metav1.ObjectMeta{Name: "prometheus-vpa", Namespace: v1beta1constants.GardenNamespace}},
			&autoscalingv1beta2.VerticalPodAutoscaler{ObjectMeta: metav1.ObjectMeta{Name: "aggregate-prometheus-vpa", Namespace: v1beta1constants.GardenNamespace}},
		}

		if err := kutil.DeleteObjects(ctx, k8sSeedClient.Client(), resources...); err != nil {
			return err
		}
	}

	// Fetch component-specific central monitoring configuration
	var (
		centralScrapeConfigs                            = strings.Builder{}
		centralCAdvisorScrapeConfigMetricRelabelConfigs = strings.Builder{}
	)

	for _, componentFn := range []component.CentralMonitoringConfiguration{} {
		centralMonitoringConfig, err := componentFn()
		if err != nil {
			return err
		}

		for _, config := range centralMonitoringConfig.ScrapeConfigs {
			centralScrapeConfigs.WriteString(fmt.Sprintf("- %s\n", utils.Indent(config, 2)))
		}

		for _, config := range centralMonitoringConfig.CAdvisorScrapeConfigMetricRelabelConfigs {
			centralCAdvisorScrapeConfigMetricRelabelConfigs.WriteString(fmt.Sprintf("- %s\n", utils.Indent(config, 2)))
		}
	}

	// Logging feature gate
	var (
		loggingEnabled                    = gardenletfeatures.FeatureGate.Enabled(features.Logging)
		existingSecretsMap                = map[string]*corev1.Secret{}
		filters                           = strings.Builder{}
		parsers                           = strings.Builder{}
		fluentBitConfigurationsOverwrites = map[string]interface{}{}
		lokiValues                        = map[string]interface{}{}
	)

	lokiValues["enabled"] = loggingEnabled

	if loggingEnabled {
		lokiValues["authEnabled"] = false

		lokiVpa := &autoscalingv1beta2.VerticalPodAutoscaler{ObjectMeta: metav1.ObjectMeta{Name: "loki-vpa", Namespace: v1beta1constants.GardenNamespace}}
		if err := k8sSeedClient.Client().Delete(ctx, lokiVpa); client.IgnoreNotFound(err) != nil && !meta.IsNoMatchError(err) {
			return err
		}

		if conf.Logging != nil && conf.Logging.Loki != nil && conf.Logging.Loki.Garden != nil &&
			conf.Logging.Loki.Garden.Priority != nil {
			priority := *conf.Logging.Loki.Garden.Priority
			if err := deletePriorityClassIfValueNotTheSame(ctx, k8sSeedClient.Client(), common.GardenLokiPriorityClassName, priority); err != nil {
				return err
			}
			lokiValues["priorityClass"] = map[string]interface{}{
				"value": priority,
				"name":  common.GardenLokiPriorityClassName,
			}
		} else {
			pc := &schedulingv1.PriorityClass{ObjectMeta: metav1.ObjectMeta{Name: common.GardenLokiPriorityClassName}}
			if err := k8sSeedClient.Client().Delete(ctx, pc); client.IgnoreNotFound(err) != nil {
				return err
			}
		}

		//TODO: Remove after next release
		if err := deletePriorityClassIfValueNotTheSame(ctx, k8sSeedClient.Client(), "fluent-bit", 150); err != nil {
			return err
		}

		if hvpaEnabled {
			shootInfo := &corev1.ConfigMap{}
			maintenanceBegin := "220000-0000"
			maintenanceEnd := "230000-0000"
			if err := k8sSeedClient.Client().Get(ctx, kutil.Key(metav1.NamespaceSystem, "shoot-info"), shootInfo); err != nil {
				if !apierrors.IsNotFound(err) {
					return err
				}
			} else {
				shootMaintenanceBegin, err := utils.ParseMaintenanceTime(shootInfo.Data["maintenanceBegin"])
				if err != nil {
					return err
				}
				maintenanceBegin = shootMaintenanceBegin.Add(1, 0, 0).Formatted()

				shootMaintenanceEnd, err := utils.ParseMaintenanceTime(shootInfo.Data["maintenanceEnd"])
				if err != nil {
					return err
				}
				maintenanceEnd = shootMaintenanceEnd.Add(1, 0, 0).Formatted()
			}

			lokiValues["hvpa"] = map[string]interface{}{
				"enabled": true,
				"maintenanceTimeWindow": map[string]interface{}{
					"begin": maintenanceBegin,
					"end":   maintenanceEnd,
				},
			}

			currentResources, err := kutil.GetContainerResourcesInStatefulSet(ctx, k8sSeedClient.Client(), kutil.Key(v1beta1constants.GardenNamespace, "loki"))
			if err != nil {
				return err
			}
			if len(currentResources) != 0 && currentResources["loki"] != nil {
				lokiValues["resources"] = map[string]interface{}{
					"loki": currentResources["loki"],
				}
			}
		}

		componentsFunctions := []component.CentralLoggingConfiguration{
			// seed system components
			dependencywatchdog.CentralLoggingConfiguration,
			seedadmissioncontroller.CentralLoggingConfiguration,
			resourcemanager.CentralLoggingConfiguration,
			// shoot control plane components
			etcd.CentralLoggingConfiguration,
			clusterautoscaler.CentralLoggingConfiguration,
			kubescheduler.CentralLoggingConfiguration,
			kubecontrollermanager.CentralLoggingConfiguration,
			// shoot system components
			metricsserver.CentralLoggingConfiguration,
		}
		userAllowedComponents := []string{
			v1beta1constants.DeploymentNameKubeAPIServer,
			v1beta1constants.DeploymentNameVPAExporter, v1beta1constants.DeploymentNameVPARecommender,
			v1beta1constants.DeploymentNameVPAAdmissionController,
		}

		// Fetch component specific logging configurations
		for _, componentFn := range componentsFunctions {
			loggingConfig, err := componentFn()
			if err != nil {
				return err
			}

			filters.WriteString(fmt.Sprintln(loggingConfig.Filters))
			parsers.WriteString(fmt.Sprintln(loggingConfig.Parsers))

			if loggingConfig.UserExposed {
				userAllowedComponents = append(userAllowedComponents, loggingConfig.PodPrefix)
			}
		}

		loggingRewriteTagFilter := `[FILTER]
    Name          rewrite_tag
    Match         kubernetes.*
    Rule          $tag ^kubernetes\.var\.log\.containers\.(` + strings.Join(userAllowedComponents, "-.+?|") + `-.+?)_ ` + userExposedComponentTagPrefix + `.$TAG true
    Emitter_Name  re_emitted
`
		filters.WriteString(fmt.Sprintln(loggingRewriteTagFilter))

		// Read extension provider specific logging configuration
		existingConfigMaps := &corev1.ConfigMapList{}
		if err = k8sSeedClient.Client().List(ctx, existingConfigMaps,
			client.InNamespace(v1beta1constants.GardenNamespace),
			client.MatchingLabels{v1beta1constants.LabelExtensionConfiguration: v1beta1constants.LabelLogging}); err != nil {
			return err
		}

		// Need stable order before passing the dashboards to Grafana config to avoid unnecessary changes
		kutil.ByName().Sort(existingConfigMaps)

		// Read all filters and parsers coming from the extension provider configurations
		for _, cm := range existingConfigMaps.Items {
			filters.WriteString(fmt.Sprintln(cm.Data[v1beta1constants.FluentBitConfigMapKubernetesFilter]))
			parsers.WriteString(fmt.Sprintln(cm.Data[v1beta1constants.FluentBitConfigMapParser]))
		}

		if loggingConfig != nil && loggingConfig.FluentBit != nil {
			fbConfig := loggingConfig.FluentBit

			if fbConfig.ServiceSection != nil {
				fluentBitConfigurationsOverwrites["service"] = *fbConfig.ServiceSection
			}
			if fbConfig.InputSection != nil {
				fluentBitConfigurationsOverwrites["input"] = *fbConfig.InputSection
			}
			if fbConfig.OutputSection != nil {
				fluentBitConfigurationsOverwrites["output"] = *fbConfig.OutputSection
			}
		}
	} else {
		if err := common.DeleteSeedLoggingStack(ctx, k8sSeedClient.Client()); err != nil {
			return err
		}
	}

	// Monitoring resource values
	monitoringResources := map[string]interface{}{
		"prometheus":           map[string]interface{}{},
		"aggregate-prometheus": map[string]interface{}{},
	}

	if hvpaEnabled {
		for resource := range monitoringResources {
			currentResources, err := kutil.GetContainerResourcesInStatefulSet(ctx, k8sSeedClient.Client(), kutil.Key(v1beta1constants.GardenNamespace, resource))
			if err != nil {
				return err
			}
			if len(currentResources) != 0 && currentResources["prometheus"] != nil {
				monitoringResources[resource] = map[string]interface{}{
					"prometheus": currentResources["prometheus"],
				}
			}
		}
	}

	existingSecrets := &corev1.SecretList{}
	if err = k8sSeedClient.Client().List(ctx, existingSecrets, client.InNamespace(v1beta1constants.GardenNamespace)); err != nil {
		return err
	}

	for _, secret := range existingSecrets.Items {
		secretObj := secret
		existingSecretsMap[secret.ObjectMeta.Name] = &secretObj
	}

	deployedSecretsMap, err := deployCertificates(ctx, seed, k8sSeedClient, existingSecretsMap)
	if err != nil {
		return err
	}
	jsonString, err := json.Marshal(deployedSecretsMap[common.VPASecretName].Data)
	if err != nil {
		return err
	}

	// AlertManager configuration
	alertManagerConfig := map[string]interface{}{
		"storage": seed.GetValidVolumeSize("1Gi"),
	}

	alertingSMTPKeys := common.GetSecretKeysWithPrefix(v1beta1constants.GardenRoleAlerting, secrets)

	if seedWantsAlertmanager(alertingSMTPKeys, secrets) {
		emailConfigs := make([]map[string]interface{}, 0, len(alertingSMTPKeys))
		for _, key := range alertingSMTPKeys {
			if string(secrets[key].Data["auth_type"]) == "smtp" {
				secret := secrets[key]
				emailConfigs = append(emailConfigs, map[string]interface{}{
					"to":            string(secret.Data["to"]),
					"from":          string(secret.Data["from"]),
					"smarthost":     string(secret.Data["smarthost"]),
					"auth_username": string(secret.Data["auth_username"]),
					"auth_identity": string(secret.Data["auth_identity"]),
					"auth_password": string(secret.Data["auth_password"]),
				})
				alertManagerConfig["enabled"] = true
				alertManagerConfig["emailConfigs"] = emailConfigs
				break
			}
		}
	} else {
		alertManagerConfig["enabled"] = false
		if err := common.DeleteAlertmanager(ctx, k8sSeedClient.Client(), v1beta1constants.GardenNamespace); err != nil {
			return err
		}
	}

	if !seed.Info.Spec.Settings.ExcessCapacityReservation.Enabled {
		if err := common.DeleteReserveExcessCapacity(ctx, k8sSeedClient.Client()); client.IgnoreNotFound(err) != nil {
			return err
		}
	}

	chartApplier := k8sSeedClient.ChartApplier()

	var (
		applierOptions          = kubernetes.CopyApplierOptions(kubernetes.DefaultMergeFuncs)
		retainStatusInformation = func(new, old *unstructured.Unstructured) {
			// Apply status from old Object to retain status information
			new.Object["status"] = old.Object["status"]
		}
		hvpaGK                = schema.GroupKind{Group: "autoscaling.k8s.io", Kind: "Hvpa"}
		issuerGK              = schema.GroupKind{Group: "certmanager.k8s.io", Kind: "ClusterIssuer"}
		grafanaHost           = seed.GetIngressFQDN(grafanaPrefix)
		prometheusHost        = seed.GetIngressFQDN(prometheusPrefix)
		monitoringCredentials = existingSecretsMap["seed-monitoring-ingress-credentials"]
		monitoringBasicAuth   string
	)

	if monitoringCredentials != nil {
		monitoringBasicAuth = utils.CreateSHA1Secret(monitoringCredentials.Data[secretsutils.DataKeyUserName], monitoringCredentials.Data[secretsutils.DataKeyPassword])
	}
	applierOptions[vpaGK] = retainStatusInformation
	applierOptions[hvpaGK] = retainStatusInformation
	applierOptions[issuerGK] = retainStatusInformation

	var (
		grafanaTLSOverride    = grafanaTLS
		prometheusTLSOverride = prometheusTLS
	)

	wildcardCert, err := GetWildcardCertificate(ctx, k8sSeedClient.Client())
	if err != nil {
		return err
	}

	if wildcardCert != nil {
		grafanaTLSOverride = wildcardCert.GetName()
		prometheusTLSOverride = wildcardCert.GetName()
	}

	imageVectorOverwrites := make(map[string]string, len(componentImageVectors))
	for name, data := range componentImageVectors {
		imageVectorOverwrites[name] = data
	}

	anySNI, err := kubeapiserverexposure.AnyDeployedSNI(ctx, k8sSeedClient.Client())
	if err != nil {
		return err
	}

	if gardenletfeatures.FeatureGate.Enabled(features.ManagedIstio) {
		istiodImage, err := imageVector.FindImage(charts.ImageNameIstioIstiod)
		if err != nil {
			return err
		}

		igwImage, err := imageVector.FindImage(charts.ImageNameIstioProxy)
		if err != nil {
			return err
		}

		chartApplier := k8sSeedClient.ChartApplier()
		istioCRDs := istio.NewIstioCRD(chartApplier, charts.Path, k8sSeedClient.Client())
		istiod := istio.NewIstiod(
			&istio.IstiodValues{
				TrustDomain: gardencorev1beta1.DefaultDomain,
				Image:       istiodImage.String(),
			},
			common.IstioNamespace,
			chartApplier,
			charts.Path,
			k8sSeedClient.Client(),
		)
		istioDeployers := []component.DeployWaiter{istioCRDs, istiod}

		defaultIngressGatewayConfig := &istio.IngressValues{
			TrustDomain:     gardencorev1beta1.DefaultDomain,
			Image:           igwImage.String(),
			IstiodNamespace: common.IstioNamespace,
			Annotations:     seed.LoadBalancerServiceAnnotations,
			Ports:           []corev1.ServicePort{},
			LoadBalancerIP:  conf.SNI.Ingress.ServiceExternalIP,
			Labels:          conf.SNI.Ingress.Labels,
		}

		// even if SNI is being disabled, the existing ports must stay the same
		// until all APIServer SNI resources are removed.
		if gardenletfeatures.FeatureGate.Enabled(features.APIServerSNI) || anySNI {
			defaultIngressGatewayConfig.Ports = append(
				defaultIngressGatewayConfig.Ports,
				corev1.ServicePort{Name: "proxy", Port: 8443, TargetPort: intstr.FromInt(8443)},
				corev1.ServicePort{Name: "tcp", Port: 443, TargetPort: intstr.FromInt(9443)},
				corev1.ServicePort{Name: "tls-tunnel", Port: vpnseedserver.GatewayPort, TargetPort: intstr.FromInt(vpnseedserver.GatewayPort)},
			)
		}

		istioDeployers = append(istioDeployers, istio.NewIngressGateway(
			defaultIngressGatewayConfig,
			*conf.SNI.Ingress.Namespace,
			chartApplier,
			charts.Path,
			k8sSeedClient.Client(),
		))

		// Add for each ExposureClass handler in the config an own Ingress Gateway.
		for _, handler := range conf.ExposureClassHandlers {
			istioDeployers = append(istioDeployers, istio.NewIngressGateway(
				&istio.IngressValues{
					TrustDomain:     gardencorev1beta1.DefaultDomain,
					Image:           igwImage.String(),
					IstiodNamespace: common.IstioNamespace,
					Annotations:     utils.MergeStringMaps(seed.LoadBalancerServiceAnnotations, handler.LoadBalancerService.Annotations),
					Ports:           defaultIngressGatewayConfig.Ports,
					LoadBalancerIP:  handler.SNI.Ingress.ServiceExternalIP,
					Labels:          gutil.GetMandatoryExposureClassHandlerSNILabels(handler.SNI.Ingress.Labels, handler.Name),
				},
				*handler.SNI.Ingress.Namespace,
				chartApplier,
				charts.Path,
				k8sSeedClient.Client(),
			))
		}

		if err := component.OpWaiter(istioDeployers...).Deploy(ctx); err != nil {
			return err
		}
	}

	var proxyGatewayDeployers = []component.DeployWaiter{
		istio.NewProxyProtocolGateway(
			&istio.ProxyValues{
				Labels: conf.SNI.Ingress.Labels,
			},
			*conf.SNI.Ingress.Namespace,
			chartApplier,
			charts.Path,
		),
	}

	for _, handler := range conf.ExposureClassHandlers {
		proxyGatewayDeployers = append(proxyGatewayDeployers, istio.NewProxyProtocolGateway(
			&istio.ProxyValues{
				Labels: gutil.GetMandatoryExposureClassHandlerSNILabels(handler.SNI.Ingress.Labels, handler.Name),
			},
			*handler.SNI.Ingress.Namespace,
			chartApplier,
			charts.Path,
		))
	}

	if gardenletfeatures.FeatureGate.Enabled(features.APIServerSNI) {
		for _, proxyDeployer := range proxyGatewayDeployers {
			if err := proxyDeployer.Deploy(ctx); err != nil {
				return err
			}
		}
	} else {
		for _, proxyDeployer := range proxyGatewayDeployers {
			if err := proxyDeployer.Destroy(ctx); err != nil {
				return err
			}
		}
	}

	if err := cleanupOrphanExposureClassHandlerResources(ctx, k8sSeedClient.Client(), conf.ExposureClassHandlers, seedLogger); err != nil {
		return err
	}

	if seed.Info.Status.ClusterIdentity == nil {
		seedClusterIdentity, err := determineClusterIdentity(ctx, k8sSeedClient.Client())
		if err != nil {
			return err
		}

		seedCopy := seed.Info.DeepCopy()
		seed.Info.Status.ClusterIdentity = &seedClusterIdentity

		if err := k8sGardenClient.Client().Status().Patch(ctx, seed.Info, client.MergeFrom(seedCopy)); err != nil {
			return err
		}
	}

	values := kubernetes.Values(map[string]interface{}{
		"priorityClassName": v1beta1constants.PriorityClassNameShootControlPlane,
		"global": map[string]interface{}{
			"ingressClass": getIngressClass(managedIngress(seed)),
			"images":       imagevector.ImageMapToValues(images),
		},
		"reserveExcessCapacity": seed.Info.Spec.Settings.ExcessCapacityReservation.Enabled,
		"replicas": map[string]interface{}{
			"reserve-excess-capacity": desiredExcessCapacity(),
		},
		"prometheus": map[string]interface{}{
			"resources":               monitoringResources["prometheus"],
			"storage":                 seed.GetValidVolumeSize("10Gi"),
			"additionalScrapeConfigs": centralScrapeConfigs.String(),
			"additionalCAdvisorScrapeConfigMetricRelabelConfigs": centralCAdvisorScrapeConfigMetricRelabelConfigs.String(),
		},
		"aggregatePrometheus": map[string]interface{}{
			"resources":  monitoringResources["aggregate-prometheus"],
			"storage":    seed.GetValidVolumeSize("20Gi"),
			"seed":       seed.Info.Name,
			"hostName":   prometheusHost,
			"secretName": prometheusTLSOverride,
		},
		"grafana": map[string]interface{}{
			"hostName":   grafanaHost,
			"secretName": grafanaTLSOverride,
		},
		"fluent-bit": map[string]interface{}{
			"enabled":                           loggingEnabled,
			"additionalParsers":                 parsers.String(),
			"additionalFilters":                 filters.String(),
			"fluentBitConfigurationsOverwrites": fluentBitConfigurationsOverwrites,
			"exposedComponentsTagPrefix":        userExposedComponentTagPrefix,
		},
		"loki":         lokiValues,
		"alertmanager": alertManagerConfig,
		"vpa": map[string]interface{}{
			"enabled": vpaEnabled,
			"runtime": map[string]interface{}{
				"admissionController": map[string]interface{}{
					"podAnnotations": map[string]interface{}{
						"checksum/secret-vpa-tls-certs": utils.ComputeSHA256Hex(jsonString),
					},
				},
			},
			"application": map[string]interface{}{
				"admissionController": map[string]interface{}{
					"controlNamespace": v1beta1constants.GardenNamespace,
					"caCert":           deployedSecretsMap[common.VPASecretName].Data[secretsutils.DataKeyCertificateCA],
				},
			},
		},
		"nginx-ingress": computeNginxIngress(seed),
		"hvpa": map[string]interface{}{
			"enabled": hvpaEnabled,
		},
		"istio": map[string]interface{}{
			"enabled": gardenletfeatures.FeatureGate.Enabled(features.ManagedIstio),
		},
		"ingress": map[string]interface{}{
			"basicAuthSecret": monitoringBasicAuth,
		},
	})

	if err := chartApplier.Apply(ctx, filepath.Join(charts.Path, chartName), v1beta1constants.GardenNamespace, chartName, values, applierOptions); err != nil {
		return err
	}

	var ingressClass string
	if managedIngress(seed) {
		ingressClass = v1beta1constants.SeedNginxIngressClass
	} else {
		ingressClass = v1beta1constants.ShootNginxIngressClass
		if err := deleteIngressController(ctx, k8sSeedClient.Client()); err != nil {
			return err
		}
	}
	if err := migrateIngressClassForShootIngresses(ctx, k8sGardenClient.Client(), k8sSeedClient.Client(), seed, ingressClass); err != nil {
		return err
	}

	return runCreateSeedFlow(ctx, k8sSeedClient, k8sGardenClient, imageVector, imageVectorOverwrites, seed, seedLogger, anySNI)
}

func runCreateSeedFlow(
	ctx context.Context,
	sc, gc kubernetes.Interface,
	imageVector imagevector.ImageVector,
	imageVectorOverwrites map[string]string,
	seed *Seed,
	seedLogger *logrus.Entry,
	anySNI bool,
) error {
	kubernetesVersion, err := semver.NewVersion(sc.Version())
	if err != nil {
		return err
	}

	secretData, err := getDNSProviderSecretData(ctx, gc.Client(), seed)
	if err != nil {
		return err
	}
	if err := updateDNSProviderSecret(ctx, sc.Client(), emptyDNSProviderSecret(), secretData, seed); err != nil {
		return err
	}

	// setup for flow graph
	var ingressLoadBalancerAddress string
	if managedIngress(seed) {
		ingressLoadBalancerAddress, err = kutil.WaitUntilLoadBalancerIsReady(ctx, sc, v1beta1constants.GardenNamespace, "nginx-ingress-controller", time.Minute, seedLogger)
		if err != nil {
			return err
		}
	}

	dnsEntry := getManagedIngressDNSEntry(sc.Client(), seed.GetIngressFQDN("*"), *seed.Info.Status.ClusterIdentity, ingressLoadBalancerAddress, seedLogger)
	dnsOwner := getManagedIngressDNSOwner(sc.Client(), *seed.Info.Status.ClusterIdentity)
	dnsRecord := getManagedIngressDNSRecord(sc.Client(), seed.Info.Spec.DNS, secretData, seed.GetIngressFQDN("*"), ingressLoadBalancerAddress, seedLogger)

	networkPolicies, err := defaultNetworkPolicies(sc, seed.Info, anySNI)
	if err != nil {
		return err
	}
	gardenerResourceManager, err := defaultGardenerResourceManager(sc, imageVector)
	if err != nil {
		return err
	}
	etcdDruid, err := defaultEtcdDruid(sc, imageVector, kubernetesVersion, imageVectorOverwrites)
	if err != nil {
		return err
	}
	gardenerSeedAdmissionController, err := defaultGardenerSeedAdmissionController(sc, imageVector, kubernetesVersion)
	if err != nil {
		return err
	}
	kubeScheduler, err := defaultKubeScheduler(sc, imageVector, kubernetesVersion)
	if err != nil {
		return err
	}
	dwdEndpoint, dwdProbe, err := defaultDependencyWatchdogs(sc, imageVector)
	if err != nil {
		return err
	}

	var (
		g = flow.NewGraph("Seed cluster creation")
		_ = g.Add(flow.Task{
			Name: "Ensuring network policies",
			Fn:   networkPolicies.Deploy,
		})
		_ = g.Add(flow.Task{
			Name: "Deploying managed ingress DNS record",
			Fn: flow.TaskFn(func(ctx context.Context) error {
				return deployDNSResources(ctx, dnsEntry, dnsOwner, dnsRecord, deployDNSProviderTask(sc.Client(), seed.Info.Spec.DNS), destroyDNSProviderTask(sc.Client()))
			}).DoIf(managedIngress(seed)),
		})
		_ = g.Add(flow.Task{
			Name: "Destroying managed ingress DNS record (if existing)",
			Fn: flow.TaskFn(func(ctx context.Context) error {
				return destroyDNSResources(ctx, dnsEntry, dnsOwner, dnsRecord, destroyDNSProviderTask(sc.Client()))
			}).DoIf(!managedIngress(seed)),
		})
		deployResourceManager = g.Add(flow.Task{
			Name: "Deploying gardener-resource-manager",
			Fn:   component.OpWaiter(gardenerResourceManager).Deploy,
		})
		_ = g.Add(flow.Task{
			Name:         "Deploying cluster-identity",
			Fn:           clusteridentity.NewForSeed(sc.Client(), v1beta1constants.GardenNamespace, *seed.Info.Status.ClusterIdentity).Deploy,
			Dependencies: flow.NewTaskIDs(deployResourceManager),
		})
		_ = g.Add(flow.Task{
			Name:         "Deploying cluster-autoscaler",
			Fn:           clusterautoscaler.NewBootstrapper(sc.Client(), v1beta1constants.GardenNamespace).Deploy,
			Dependencies: flow.NewTaskIDs(deployResourceManager),
		})
		_ = g.Add(flow.Task{
			Name:         "Deploying etcd-druid",
			Fn:           etcdDruid.Deploy,
			Dependencies: flow.NewTaskIDs(deployResourceManager),
		})
		_ = g.Add(flow.Task{
			Name:         "Deploying gardener-seed-admission-controller",
			Fn:           gardenerSeedAdmissionController.Deploy,
			Dependencies: flow.NewTaskIDs(deployResourceManager),
		})
		_ = g.Add(flow.Task{
			Name:         "Deploying kube-scheduler for shoot control plane pods",
			Fn:           kubeScheduler.Deploy,
			Dependencies: flow.NewTaskIDs(deployResourceManager),
		})
		_ = g.Add(flow.Task{
			Name:         "Deploying dependency-watchdog-endpoint",
			Fn:           dwdEndpoint.Deploy,
			Dependencies: flow.NewTaskIDs(deployResourceManager),
		})
		_ = g.Add(flow.Task{
			Name:         "Deploying dependency-watchdog-probe",
			Fn:           dwdProbe.Deploy,
			Dependencies: flow.NewTaskIDs(deployResourceManager),
		})
	)

	if err := g.Compile().Run(flow.Opts{Logger: seedLogger}); err != nil {
		return flow.Errors(err)
	}

	return nil
}

// RunDeleteSeedFlow deletes certain resources from the seed cluster.
func RunDeleteSeedFlow(ctx context.Context, sc, gc kubernetes.Interface, seed *Seed, seedLogger *logrus.Entry) error {
	kubernetesVersion, err := semver.NewVersion(sc.Version())
	if err != nil {
		return err
	}

	secretData, err := getDNSProviderSecretData(ctx, gc.Client(), seed)
	if err != nil {
		return err
	}
	if err := updateDNSProviderSecret(ctx, sc.Client(), emptyDNSProviderSecret(), secretData, seed); err != nil {
		return err
	}

	// setup for flow graph
	var (
		dnsEntry        = getManagedIngressDNSEntry(sc.Client(), seed.GetIngressFQDN("*"), *seed.Info.Status.ClusterIdentity, "", seedLogger)
		dnsOwner        = getManagedIngressDNSOwner(sc.Client(), *seed.Info.Status.ClusterIdentity)
		dnsRecord       = getManagedIngressDNSRecord(sc.Client(), seed.Info.Spec.DNS, secretData, seed.GetIngressFQDN("*"), "", seedLogger)
		autoscaler      = clusterautoscaler.NewBootstrapper(sc.Client(), v1beta1constants.GardenNamespace)
		gsac            = seedadmissioncontroller.New(sc.Client(), v1beta1constants.GardenNamespace, "", kubernetesVersion)
		resourceManager = resourcemanager.New(sc.Client(), v1beta1constants.GardenNamespace, "", 0, resourcemanager.Values{})
		etcdDruid       = etcd.NewBootstrapper(sc.Client(), v1beta1constants.GardenNamespace, "", kubernetesVersion, nil)
		networkPolicies = networkpolicies.NewBootstrapper(sc.Client(), v1beta1constants.GardenNamespace, networkpolicies.GlobalValues{})
		clusterIdentity = clusteridentity.NewForSeed(sc.Client(), v1beta1constants.GardenNamespace, "")
		dwdEndpoint     = dependencywatchdog.New(sc.Client(), v1beta1constants.GardenNamespace, dependencywatchdog.Values{Role: dependencywatchdog.RoleEndpoint})
		dwdProbe        = dependencywatchdog.New(sc.Client(), v1beta1constants.GardenNamespace, dependencywatchdog.Values{Role: dependencywatchdog.RoleProbe})
	)
	scheduler, err := gardenerkubescheduler.Bootstrap(sc.Client(), v1beta1constants.GardenNamespace, nil, kubernetesVersion)
	if err != nil {
		return err
	}

	var (
		g                = flow.NewGraph("Seed cluster deletion")
		destroyDNSRecord = g.Add(flow.Task{
			Name: "Destroying managed ingress DNS record (if existing)",
			Fn: func(ctx context.Context) error {
				return destroyDNSResources(ctx, dnsEntry, dnsOwner, dnsRecord, destroyDNSProviderTask(sc.Client()))
			},
		})
		noControllerInstallations = g.Add(flow.Task{
			Name:         "Ensuring no ControllerInstallations are left",
			Fn:           ensureNoControllerInstallations(gc, seed.Info.Name),
			Dependencies: flow.NewTaskIDs(destroyDNSRecord),
		})
		destroyClusterIdentity = g.Add(flow.Task{
			Name: "Destroying cluster-identity",
			Fn:   component.OpDestroyAndWait(clusterIdentity).Destroy,
		})
		destroyClusterAutoscaler = g.Add(flow.Task{
			Name: "Destroying cluster-autoscaler",
			Fn:   component.OpDestroyAndWait(autoscaler).Destroy,
		})
		destroyEtcdDruid = g.Add(flow.Task{
			Name: "Destroying etcd druid",
			Fn:   component.OpDestroyAndWait(etcdDruid).Destroy,
		})
		destroySeedAdmissionController = g.Add(flow.Task{
			Name: "Destroying gardener-seed-admission-controller",
			Fn:   component.OpDestroyAndWait(gsac).Destroy,
		})
		destroyKubeScheduler = g.Add(flow.Task{
			Name: "Destroying kube-scheduler",
			Fn:   component.OpDestroyAndWait(scheduler).Destroy,
		})
		destroyNetworkPolicies = g.Add(flow.Task{
			Name: "Destroy network policies",
			Fn:   component.OpDestroyAndWait(networkPolicies).Destroy,
		})
		destroyDWDEndpoint = g.Add(flow.Task{
			Name: "Destroy dependency-watchdog-endpoint",
			Fn:   component.OpDestroyAndWait(dwdEndpoint).Destroy,
		})
		destroyDWDProbe = g.Add(flow.Task{
			Name: "Destroy dependency-watchdog-probe",
			Fn:   component.OpDestroyAndWait(dwdProbe).Destroy,
		})
		_ = g.Add(flow.Task{
			Name: "Destroying gardener-resource-manager",
			Fn:   resourceManager.Destroy,
			Dependencies: flow.NewTaskIDs(
				destroySeedAdmissionController,
				destroyEtcdDruid,
				destroyClusterIdentity,
				destroyClusterAutoscaler,
				destroyKubeScheduler,
				destroyNetworkPolicies,
				destroyDWDEndpoint,
				destroyDWDProbe,
				noControllerInstallations,
			),
		})
	)

	if err := g.Compile().Run(flow.Opts{Logger: seedLogger}); err != nil {
		return flow.Errors(err)
	}

	return nil
}

func deployDNSResources(ctx context.Context, dnsEntry, dnsOwner component.DeployWaiter, dnsRecord component.DeployMigrateWaiter, deployDNSProviderTask, destroyDNSProviderTask flow.TaskFn) error {
	if gardenletfeatures.FeatureGate.Enabled(features.UseDNSRecords) {
		if err := dnsOwner.Destroy(ctx); err != nil {
			return err
		}
		if err := dnsOwner.WaitCleanup(ctx); err != nil {
			return err
		}
		if err := destroyDNSProviderTask(ctx); err != nil {
			return err
		}
		if err := dnsEntry.Destroy(ctx); err != nil {
			return err
		}
		if err := dnsEntry.WaitCleanup(ctx); err != nil {
			return err
		}
		if err := dnsRecord.Deploy(ctx); err != nil {
			return err
		}
		return dnsRecord.Wait(ctx)
	} else {
		if err := dnsRecord.Migrate(ctx); err != nil {
			return err
		}
		if err := dnsRecord.WaitMigrate(ctx); err != nil {
			return err
		}
		if err := dnsRecord.Destroy(ctx); err != nil {
			return err
		}
		if err := dnsRecord.WaitCleanup(ctx); err != nil {
			return err
		}
		if err := dnsOwner.Deploy(ctx); err != nil {
			return err
		}
		if err := dnsOwner.Wait(ctx); err != nil {
			return err
		}
		if err := deployDNSProviderTask(ctx); err != nil {
			return err
		}
		if err := dnsEntry.Deploy(ctx); err != nil {
			return err
		}
		return dnsEntry.Wait(ctx)
	}
}

func destroyDNSResources(ctx context.Context, dnsEntry, dnsOwner, dnsRecord component.DeployWaiter, destroyDNSProviderTask flow.TaskFn) error {
	if err := dnsEntry.Destroy(ctx); err != nil {
		return err
	}
	if err := dnsEntry.WaitCleanup(ctx); err != nil {
		return err
	}
	if err := destroyDNSProviderTask(ctx); err != nil {
		return err
	}
	if err := dnsOwner.Destroy(ctx); err != nil {
		return err
	}
	if err := dnsOwner.WaitCleanup(ctx); err != nil {
		return err
	}
	if err := dnsRecord.Destroy(ctx); err != nil {
		return err
	}
	return dnsRecord.WaitCleanup(ctx)
}

func ensureNoControllerInstallations(gc kubernetes.Interface, seedName string) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		associatedControllerInstallations, err := controllerutils.DetermineControllerInstallationAssociations(ctx, gc.Client(), seedName)
		if err != nil {
			return err
		}
		if associatedControllerInstallations != nil {
			return fmt.Errorf("can't continue with Seed deletion, because the following objects are still referencing it: ControllerInstallations=%v", associatedControllerInstallations)
		}
		return nil
	}
}

// updateDNSProviderSecret updates the DNSProvider secret in the garden namespace of the seed. This is only needed
// if the `UseDNSRecords` feature gate is not enabled.
func updateDNSProviderSecret(ctx context.Context, seedClient client.Client, secret *corev1.Secret, secretData map[string][]byte, seed *Seed) error {
	if dnsConfig := seed.Info.Spec.DNS; dnsConfig.Provider != nil && !gardenletfeatures.FeatureGate.Enabled(features.UseDNSRecords) {
		_, err := controllerutils.GetAndCreateOrMergePatch(ctx, seedClient, secret, func() error {
			secret.Type = corev1.SecretTypeOpaque
			secret.Data = secretData
			return nil
		})
		return err
	}
	return nil
}

func deployDNSProviderTask(seedClient client.Client, dnsConfig gardencorev1beta1.SeedDNS) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		var (
			dnsProvider    = emptyDNSProvider()
			providerSecret = emptyDNSProviderSecret()
		)

		_, err := controllerutils.GetAndCreateOrMergePatch(ctx, seedClient, dnsProvider, func() error {
			dnsProvider.Spec = dnsv1alpha1.DNSProviderSpec{
				Type: dnsConfig.Provider.Type,
				SecretRef: &corev1.SecretReference{
					Namespace: providerSecret.Namespace,
					Name:      providerSecret.Name,
				},
			}

			if dnsConfig.Provider.Domains != nil {
				dnsProvider.Spec.Domains = &dnsv1alpha1.DNSSelection{
					Include: dnsConfig.Provider.Domains.Include,
					Exclude: dnsConfig.Provider.Domains.Exclude,
				}
			}

			if dnsConfig.Provider.Zones != nil {
				dnsProvider.Spec.Zones = &dnsv1alpha1.DNSSelection{
					Include: dnsConfig.Provider.Zones.Include,
					Exclude: dnsConfig.Provider.Zones.Exclude,
				}
			}

			return nil
		})
		return err
	}
}

func destroyDNSProviderTask(seedClient client.Client) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		return kutil.DeleteObjects(ctx, seedClient, emptyDNSProvider(), emptyDNSProviderSecret())
	}
}

func emptyDNSProvider() *dnsv1alpha1.DNSProvider {
	return &dnsv1alpha1.DNSProvider{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: v1beta1constants.GardenNamespace,
			Name:      "seed",
		},
	}
}

func emptyDNSProviderSecret() *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: v1beta1constants.GardenNamespace,
			Name:      "dnsprovider-seed",
		},
	}
}

func getDNSProviderSecretData(ctx context.Context, gardenClient client.Client, seed *Seed) (map[string][]byte, error) {
	if dnsConfig := seed.Info.Spec.DNS; dnsConfig.Provider != nil {
		secret, err := kutil.GetSecretByReference(ctx, gardenClient, &dnsConfig.Provider.SecretRef)
		if err != nil {
			return nil, err
		}
		return secret.Data, nil
	}
	return nil, nil
}

func managedIngress(seed *Seed) bool {
	return seed.Info.Spec.DNS.Provider != nil && seed.Info.Spec.Ingress != nil
}

func getManagedIngressDNSEntry(k8sSeedClient client.Client, seedFQDN string, seedClusterIdentity, loadBalancerAddress string, seedLogger *logrus.Entry) component.DeployWaiter {
	values := &dns.EntryValues{
		Name:    "ingress",
		DNSName: seedFQDN,
		OwnerID: seedClusterIdentity + "-ingress",
	}
	if loadBalancerAddress != "" {
		values.Targets = []string{loadBalancerAddress}
	}

	return dns.NewEntry(
		seedLogger,
		k8sSeedClient,
		v1beta1constants.GardenNamespace,
		values,
	)
}

func getManagedIngressDNSOwner(k8sSeedClient client.Client, seedClusterIdentity string) component.DeployWaiter {
	values := &dns.OwnerValues{
		Name:    "ingress",
		OwnerID: seedClusterIdentity + "-ingress",
		Active:  pointer.Bool(true),
	}

	return dns.NewOwner(
		k8sSeedClient,
		v1beta1constants.GardenNamespace,
		values,
	)
}

func getManagedIngressDNSRecord(seedClient client.Client, dnsConfig gardencorev1beta1.SeedDNS, secretData map[string][]byte, seedFQDN string, loadBalancerAddress string, seedLogger *logrus.Entry) component.DeployMigrateWaiter {
	values := &dnsrecord.Values{
		Name:       "seed-ingress",
		SecretName: "seed-ingress",
		Namespace:  v1beta1constants.GardenNamespace,
		SecretData: secretData,
		DNSName:    seedFQDN,
		RecordType: extensionsv1alpha1helper.GetDNSRecordType(loadBalancerAddress),
	}
	if dnsConfig.Provider != nil {
		values.Type = dnsConfig.Provider.Type
		if dnsConfig.Provider.Zones != nil && len(dnsConfig.Provider.Zones.Include) == 1 {
			values.Zone = &dnsConfig.Provider.Zones.Include[0]
		}
	}
	if loadBalancerAddress != "" {
		values.Values = []string{loadBalancerAddress}
	}

	return dnsrecord.New(
		seedLogger,
		seedClient,
		values,
		dnsrecord.DefaultInterval,
		dnsrecord.DefaultSevereThreshold,
		dnsrecord.DefaultTimeout,
	)
}

// desiredExcessCapacity computes the required resources (CPU and memory) required to deploy new shoot control planes
// (on the seed) in terms of reserve-excess-capacity deployment replicas. Each deployment replica currently
// corresponds to resources of (request/limits) 2 cores of CPU and 6Gi of RAM.
// This roughly corresponds to a single, moderately large control-plane.
// The logic for computation of desired excess capacity corresponds to deploying 2 such shoot control planes.
// This excess capacity can be used for hosting new control planes or newly vertically scaled old control-planes.
func desiredExcessCapacity() int {
	var (
		replicasToSupportSingleShoot = 1
		effectiveExcessCapacity      = 2
	)

	return effectiveExcessCapacity * replicasToSupportSingleShoot
}

// GetIngressFQDN returns the fully qualified domain name of ingress sub-resource for the Seed cluster. The
// end result is '<subDomain>.<shootName>.<projectName>.<seed-ingress-domain>'.
func (s *Seed) GetIngressFQDN(subDomain string) string {
	return fmt.Sprintf("%s.%s", subDomain, s.IngressDomain())
}

// IngressDomain returns the ingress domain for the seed.
func (s *Seed) IngressDomain() string {
	if s.Info.Spec.DNS.IngressDomain != nil {
		return *s.Info.Spec.DNS.IngressDomain
	} else if s.Info.Spec.Ingress != nil {
		return s.Info.Spec.Ingress.Domain
	}
	return ""
}

// CheckMinimumK8SVersion checks whether the Kubernetes version of the Seed cluster fulfills the minimal requirements.
func (s *Seed) CheckMinimumK8SVersion(seedClient kubernetes.Interface) (string, error) {
	var (
		minSeedVersion = "1.15"
		version        = seedClient.Version()
	)

	seedVersionOK, err := versionutils.CompareVersions(version, ">=", minSeedVersion)
	if err != nil {
		return "<unknown>", err
	}
	if !seedVersionOK {
		return "<unknown>", fmt.Errorf("the Kubernetes version of the Seed cluster must be at least %s", minSeedVersion)
	}
	return version, nil
}

// GetValidVolumeSize is to get a valid volume size.
// If the given size is smaller than the minimum volume size permitted by cloud provider on which seed cluster is running, it will return the minimum size.
func (s *Seed) GetValidVolumeSize(size string) string {
	if s.Info.Spec.Volume == nil || s.Info.Spec.Volume.MinimumSize == nil {
		return size
	}

	qs, err := resource.ParseQuantity(size)
	if err == nil && qs.Cmp(*s.Info.Spec.Volume.MinimumSize) < 0 {
		return s.Info.Spec.Volume.MinimumSize.String()
	}

	return size
}

func seedWantsAlertmanager(keys []string, secrets map[string]*corev1.Secret) bool {
	for _, key := range keys {
		if string(secrets[key].Data["auth_type"]) == "smtp" {
			return true
		}
	}
	return false
}

// GetWildcardCertificate gets the wildcard certificate for the seed's ingress domain.
// Nil is returned if no wildcard certificate is configured.
func GetWildcardCertificate(ctx context.Context, c client.Client) (*corev1.Secret, error) {
	wildcardCerts := &corev1.SecretList{}
	if err := c.List(
		ctx,
		wildcardCerts,
		client.InNamespace(v1beta1constants.GardenNamespace),
		client.MatchingLabels{v1beta1constants.GardenRole: v1beta1constants.GardenRoleControlPlaneWildcardCert},
	); err != nil {
		return nil, err
	}

	if len(wildcardCerts.Items) > 1 {
		return nil, fmt.Errorf("misconfigured seed cluster: not possible to provide more than one secret with annotation %s", v1beta1constants.GardenRoleControlPlaneWildcardCert)
	}

	if len(wildcardCerts.Items) == 1 {
		return &wildcardCerts.Items[0], nil
	}
	return nil, nil
}

// determineClusterIdentity determines the identity of a cluster, in cases where the identity was
// created manually or the Seed was created as Shoot, and later registered as Seed and already has
// an identity, it should not be changed.
func determineClusterIdentity(ctx context.Context, c client.Client) (string, error) {
	clusterIdentity := &corev1.ConfigMap{}
	if err := c.Get(ctx, kutil.Key(metav1.NamespaceSystem, v1beta1constants.ClusterIdentity), clusterIdentity); err != nil {
		if !apierrors.IsNotFound(err) {
			return "", err
		}

		gardenNamespace := &corev1.Namespace{}
		if err := c.Get(ctx, kutil.Key(metav1.NamespaceSystem), gardenNamespace); err != nil {
			return "", err
		}
		return string(gardenNamespace.UID), nil
	}
	return clusterIdentity.Data[v1beta1constants.ClusterIdentity], nil
}

func getIngressClass(seedIngressEnabled bool) string {
	if seedIngressEnabled {
		return v1beta1constants.SeedNginxIngressClass
	}
	return v1beta1constants.ShootNginxIngressClass
}

const annotationSeedIngressClass = "seed.gardener.cloud/ingress-class"

func migrateIngressClassForShootIngresses(ctx context.Context, gardenClient, seedClient client.Client, seed *Seed, newClass string) error {
	if oldClass, ok := seed.Info.Annotations[annotationSeedIngressClass]; ok && oldClass == newClass {
		return nil
	}

	shootNamespaces := &corev1.NamespaceList{}
	if err := seedClient.List(ctx, shootNamespaces, client.MatchingLabels{v1beta1constants.GardenRole: v1beta1constants.GardenRoleShoot}); err != nil {
		return err
	}

	for _, ns := range shootNamespaces.Items {
		if err := switchIngressClass(ctx, seedClient, kutil.Key(ns.Name, "alertmanager"), newClass); err != nil {
			return err
		}
		if err := switchIngressClass(ctx, seedClient, kutil.Key(ns.Name, "prometheus"), newClass); err != nil {
			return err
		}
		if err := switchIngressClass(ctx, seedClient, kutil.Key(ns.Name, "grafana-operators"), newClass); err != nil {
			return err
		}
		if err := switchIngressClass(ctx, seedClient, kutil.Key(ns.Name, "grafana-users"), newClass); err != nil {
			return err
		}
	}

	seedCopy := seed.Info.DeepCopy()
	metav1.SetMetaDataAnnotation(&seed.Info.ObjectMeta, annotationSeedIngressClass, newClass)

	return gardenClient.Patch(ctx, seed.Info, client.MergeFrom(seedCopy))
}

func switchIngressClass(ctx context.Context, seedClient client.Client, ingressKey types.NamespacedName, newClass string) error {
	ingress := &extensionsv1beta1.Ingress{}
	if err := seedClient.Get(ctx, ingressKey, ingress); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	ingress.Annotations["kubernetes.io/ingress.class"] = newClass
	return seedClient.Update(ctx, ingress)
}

func computeNginxIngress(seed *Seed) map[string]interface{} {
	values := map[string]interface{}{
		"enabled":      managedIngress(seed),
		"ingressClass": v1beta1constants.SeedNginxIngressClass,
	}

	if seed.Info.Spec.Ingress != nil && seed.Info.Spec.Ingress.Controller.ProviderConfig != nil {
		values["config"] = seed.Info.Spec.Ingress.Controller.ProviderConfig
	}

	return values
}

func deleteIngressController(ctx context.Context, c client.Client) error {
	return kutil.DeleteObjects(
		ctx,
		c,
		&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "gardener.cloud:seed:nginx-ingress"}},
		&rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "gardener.cloud:seed:nginx-ingress"}},
		&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "nginx-ingress", Namespace: v1beta1constants.GardenNamespace}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "nginx-ingress-controller", Namespace: v1beta1constants.GardenNamespace}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "nginx-ingress-controller", Namespace: v1beta1constants.GardenNamespace}},
		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "nginx-ingress-controller", Namespace: v1beta1constants.GardenNamespace}},
		&policyv1beta1.PodDisruptionBudget{ObjectMeta: metav1.ObjectMeta{Name: "nginx-ingress-controller", Namespace: v1beta1constants.GardenNamespace}},
		&autoscalingv1beta2.VerticalPodAutoscaler{ObjectMeta: metav1.ObjectMeta{Name: "nginx-ingress-controller", Namespace: v1beta1constants.GardenNamespace}},
		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "nginx-ingress-k8s-backend", Namespace: v1beta1constants.GardenNamespace}},
		&rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: "nginx-ingress", Namespace: v1beta1constants.GardenNamespace}},
		&rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "nginx-ingress", Namespace: v1beta1constants.GardenNamespace}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "nginx-ingress-k8s-backend", Namespace: v1beta1constants.GardenNamespace}},
	)
}

func deletePriorityClassIfValueNotTheSame(ctx context.Context, k8sClient client.Client, priorityClassName string, valueToCompare int32) error {
	pc := &schedulingv1.PriorityClass{}
	err := k8sClient.Get(ctx, kutil.Key(priorityClassName), pc)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			return err
		}
		return nil
	}
	if valueToCompare == pc.Value {
		return nil
	}

	return client.IgnoreNotFound(k8sClient.Delete(ctx, pc))
}

func cleanupOrphanExposureClassHandlerResources(ctx context.Context, c client.Client, exposureClassHandlers []config.ExposureClassHandler, logger *logrus.Entry) error {
	exposureClassHandlerNamespaces := &corev1.NamespaceList{}

	if err := c.List(ctx, exposureClassHandlerNamespaces, client.MatchingLabels{v1alpha1constants.GardenRole: v1alpha1constants.GardenRoleExposureClassHandler}); err != nil {
		return err
	}

	for _, namespace := range exposureClassHandlerNamespaces.Items {
		var exposureClassHandlerExists bool
		for _, handler := range exposureClassHandlers {
			if *handler.SNI.Ingress.Namespace == namespace.Name {
				exposureClassHandlerExists = true
				break
			}
		}
		if exposureClassHandlerExists {
			continue
		}
		logger.Infof("Namespace %q is orphan as there is no ExposureClass handler in the gardenlet configuration anymore", namespace.Name)

		// Determine the corresponding handler name to the ExposureClass handler resources.
		handlerName, ok := namespace.Labels[v1alpha1constants.LabelExposureClassHandlerName]
		if !ok {
			logger.Info("Cannot delete ExposureClass handler resources as the corresponging handler is unknown and it is not save to remove them")
			continue
		}

		gatewayList := istiov1beta1.GatewayList{}
		if err := c.List(ctx, &gatewayList); err != nil {
			return err
		}

		var exposureClassHandlerInUse bool
		for _, gateway := range gatewayList.Items {
			if gateway.Name != v1beta1constants.DeploymentNameKubeAPIServer && gateway.Name != v1beta1constants.DeploymentNameVPNSeedServer {
				continue
			}
			// Check if the gateway still selects the ExposureClass handler ingress gateway.
			if value, ok := gateway.Spec.Selector[v1alpha1constants.LabelExposureClassHandlerName]; ok && value == handlerName {
				exposureClassHandlerInUse = true
				break
			}
		}
		if exposureClassHandlerInUse {
			logger.Infof("Resources of ExposureClass handler %q in namespace %q cannot be deleted as they are still in use", handlerName, namespace.Name)
			continue
		}

		// ExposureClass handler is orphan and not used by any Shoots anymore
		// therefore it is save to clean it up.
		logger.Infof("Delete orphan ExposureClass handler namespace %q", namespace.Name)
		if err := c.Delete(ctx, &namespace); client.IgnoreNotFound(err) != nil {
			return err
		}
	}

	return nil
}
