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
	gardencorevalidation "github.com/gardener/gardener/pkg/apis/core/validation"
	"github.com/gardener/gardener/pkg/utils"
	hvpav1alpha1 "github.com/gardener/hvpa-controller/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/apiserver/pkg/apis/audit"
	auditv1 "k8s.io/apiserver/pkg/apis/audit/v1"
	auditvalidation "k8s.io/apiserver/pkg/apis/audit/validation"
	apiserverconfig "k8s.io/apiserver/pkg/apis/config"
	apiserverconfigv1 "k8s.io/apiserver/pkg/apis/config/v1"
	apiserverconfigvalidation "k8s.io/apiserver/pkg/apis/config/validation"
)

// ValidateAPIServer validates the configuration of the Gardener API server.
func ValidateAPIServer(config imports.GardenerAPIServer, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	if config.DeploymentConfiguration != nil {
		allErrs = append(allErrs, ValidateAPIServerDeploymentConfiguration(config.DeploymentConfiguration, fldPath.Child("deploymentConfiguration"))...)
	}
	return append(allErrs, ValidateAPIServerComponentConfiguration(config.ComponentConfiguration, fldPath.Child("componentConfiguration"))...)
}

// ValidateAPIServerDeploymentConfiguration validates the deployment configuration of the Gardener API server.
func ValidateAPIServerDeploymentConfiguration(config *imports.APIServerDeploymentConfiguration, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	allErrs = append(allErrs, ValidateCommonDeployment(config.CommonDeploymentConfiguration, fldPath)...)

	if config.LivenessProbe != nil {
		allErrs = append(allErrs, ValidateProbe(config.LivenessProbe, fldPath.Child("livenessProbe"))...)
	}

	if config.ReadinessProbe != nil {
		allErrs = append(allErrs, ValidateProbe(config.ReadinessProbe, fldPath.Child("readinessProbe"))...)
	}
	if config.MinReadySeconds != nil && *config.MinReadySeconds < 0 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("minReadySeconds"), *config.MinReadySeconds, "value must not be negative"))
	}

	if config.Hvpa != nil {
		allErrs = append(allErrs, ValidateHVPA(config.Hvpa, fldPath.Child("hvpa"))...)
	}

	return allErrs
}

// ValidateAPIServerComponentConfiguration validates the component configuration of the Gardener API server.
func ValidateAPIServerComponentConfiguration(config imports.APIServerComponentConfiguration, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	// validation of mandatory configuration
	if len(config.ClusterIdentity) == 0 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("clusterIdentity"), config.ClusterIdentity, "cluster identity must be set"))
	}

	allErrs = append(allErrs, ValidateAPIServerETCDConfiguration(config.Etcd, fldPath.Child("etcd"))...)

	allErrs = append(allErrs, ValidateCommonTLSServer(config.TLS, fldPath.Child("tls"))...)

	if len(config.CABundle) == 0 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("caBundle"), config.CABundle, "The CA Bundle must be set"))
	}

	// validation of optional configuration
	if config.Encryption != nil {
		allErrs = append(allErrs, ValidateAPIServerEncryptionConfiguration(config.Encryption, fldPath.Child("encryption"))...)
	}

	if config.Admission != nil {
		allErrs = append(allErrs, ValidateAPIServerAdmission(config.Admission, fldPath.Child("tls"))...)
	}

	if config.GoAwayChance != nil && (*config.GoAwayChance < 0 || *config.GoAwayChance > 0.02) {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("goAwayChance"), *config.GoAwayChance, "The goAwayChance can be in the interval [0, 0.02]"))
	}

	if config.Http2MaxStreamsPerConnection != nil && *config.Http2MaxStreamsPerConnection < 0 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("http2MaxStreamsPerConnection"), *config.Http2MaxStreamsPerConnection, "The Http2MaxStreamsPerConnection cannot be negative"))
	}

	allErrs = append(allErrs, gardencorevalidation.ValidatePositiveDuration(config.ShutdownDelayDuration, fldPath.Child("shutdownDelayDuration"))...)

	if config.Requests != nil {
		allErrs = append(allErrs, ValidateAPIServerRequests(config.Requests, fldPath.Child("requests"))...)
	}

	if config.WatchCacheSize != nil {
		allErrs = append(allErrs, ValidateAPIServerWatchCache(config.WatchCacheSize, fldPath.Child("watchCacheSize"))...)
	}

	if config.Audit != nil {
		allErrs = append(allErrs, ValidateAPIServerAuditConfiguration(config.Audit, config.FeatureGates, fldPath.Child("audit"))...)
	}

	return allErrs
}

const batch = "batch"

var (
	validAuditLogFormats = sets.NewString("legacy", "json")
	validAuditLogModes   = sets.NewString(batch, "blocking", "blocking-strict")
)

// ValidateAPIServerAuditConfiguration validates the Audit configuration of the Gardener API server
func ValidateAPIServerAuditConfiguration(config *imports.APIServerAuditConfiguration, featureGates []string, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	// setting DynamicConfiguration requires feature flag DynamicAuditing=true
	if config.DynamicConfiguration != nil && *config.DynamicConfiguration {
		found := false
		for _, feature := range featureGates {
			if feature == "DynamicAuditing=true" {
				found = true
				break
			}
		}

		if !found {
			allErrs = append(allErrs, field.Invalid(fldPath.Child("dynamicConfiguration"), *config.DynamicConfiguration, "DynamicConfiguration requires the feature gate 'DynamicAuditing=true' to be set"))
		}
	}

	if config.Policy != nil {
		auditPolicyInternal := &audit.Policy{}
		err := auditv1.Convert_v1_Policy_To_audit_Policy(config.Policy, auditPolicyInternal, nil)
		if err != nil {
			allErrs = append(allErrs, field.Invalid(fldPath.Child("policy"), *config.Policy, fmt.Sprintf("Audit policy is invalid - could not be converted to internal version: %v", err)))
		} else {
			if errList := auditvalidation.ValidatePolicy(auditPolicyInternal); len(errList) > 0 {
				allErrs = append(allErrs, field.Invalid(fldPath.Child("policy"), *config.Policy, fmt.Sprintf("Audit policy is invalid - the following validation errors occured: %v", errList.ToAggregate().Error())))
			}
		}
	}

	if config.Log != nil {
		fldPathLog := fldPath.Child("log")

		allErrs = append(allErrs, ValidateAPIServerAuditCommonBackendConfiguration(config.Log.APIServerAuditCommonBackendConfiguration, fldPathLog)...)

		if config.Log.Format != nil && !validAuditLogFormats.Has(*config.Log.Format) {
			allErrs = append(allErrs, field.Invalid(fldPathLog.Child("format"), *config.Log.Format, "The log format of the Audit log must be [legacy,json]"))
		}
		if config.Log.MaxAge != nil && *config.Log.MaxAge < 0 {
			allErrs = append(allErrs, field.Invalid(fldPathLog.Child("maxAge"), *config.Log.MaxAge, "The maximum age configured for Audit logs must not be negative"))
		}
		if config.Log.MaxBackup != nil && *config.Log.MaxBackup < 0 {
			allErrs = append(allErrs, field.Invalid(fldPathLog.Child("maxBackup"), *config.Log.MaxBackup, "The maximum number of old audit log files to retain must not be negative"))
		}
		if config.Log.MaxSize != nil && *config.Log.MaxSize < 0 {
			allErrs = append(allErrs, field.Invalid(fldPathLog.Child("maxSize"), *config.Log.MaxSize, "The maximum size of audit log files must not be negative"))
		}
		if config.Log.Path != nil && len(*config.Log.Path) == 0 {
			allErrs = append(allErrs, field.Invalid(fldPathLog.Child("path"), *config.Log.Path, "The filepath to store the audit log file must not be empty"))
		}
	}

	if config.Webhook != nil {
		fldPathWebhook := fldPath.Child("webhook")

		allErrs = append(allErrs, ValidateAPIServerAuditCommonBackendConfiguration(config.Webhook.APIServerAuditCommonBackendConfiguration, fldPathWebhook)...)

		if config.Webhook.InitialBackoff != nil {
			allErrs = append(allErrs, gardencorevalidation.ValidatePositiveDuration(config.Webhook.InitialBackoff, fldPathWebhook.Child("initialBackoff"))...)
		}

		if len(config.Webhook.Kubeconfig.Spec.Configuration.RawMessage) == 0 {
			allErrs = append(allErrs, field.Invalid(fldPathWebhook.Child("kubeconfig"), "", "The kubeconfig for the external audit log backend must be set"))
		}
	}

	return allErrs
}

// ValidateAPIServerAuditCommonBackendConfiguration validates the common audit log backend configuration of the Gardener API server
func ValidateAPIServerAuditCommonBackendConfiguration(config imports.APIServerAuditCommonBackendConfiguration, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	if config.Mode != nil && !validAuditLogModes.Has(*config.Mode) {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("mode"), *config.Mode, "The mode strategy for sending audit events must be one of [batch,blocking,blocking-strict]"))
	} else if config.Mode != nil && *config.Mode == batch {
		if config.BatchBufferSize != nil && *config.BatchBufferSize < 0 {
			allErrs = append(allErrs, field.Invalid(fldPath.Child("batchBufferSize"), *config.BatchBufferSize, "The BatchBufferSize must not be negative"))
		}
		if config.BatchMaxSize != nil && *config.BatchMaxSize < 0 {
			allErrs = append(allErrs, field.Invalid(fldPath.Child("batchMaxSize"), *config.BatchMaxSize, "The BatchMaxSize must not be negative"))
		}

		allErrs = append(allErrs, gardencorevalidation.ValidatePositiveDuration(config.BatchMaxWait, fldPath.Child("batchMaxWait"))...)

		if config.BatchThrottleBurst != nil && *config.BatchThrottleBurst < 0 {
			allErrs = append(allErrs, field.Invalid(fldPath.Child("batchThrottleBurst"), *config.BatchThrottleBurst, "The BatchThrottleBurst must not be negative"))
		}
		if config.BatchThrottleQPS != nil && *config.BatchThrottleQPS < 0 {
			allErrs = append(allErrs, field.Invalid(fldPath.Child("batchThrottleQPS"), *config.BatchThrottleQPS, "The BatchThrottleQPS must not be negative"))
		}
		if config.TruncateMaxBatchSize != nil && *config.TruncateMaxBatchSize < 0 {
			allErrs = append(allErrs, field.Invalid(fldPath.Child("truncateMaxBatchSize"), *config.TruncateMaxBatchSize, "The TruncateMaxBatchSize must not be negative"))
		}
	}

	if config.TruncateMaxEventSize != nil && *config.TruncateMaxEventSize < 0 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("truncateMaxEventSize"), *config.TruncateMaxEventSize, "The TruncateMaxEventSize must not be negative"))
	}

	if config.Version != nil && len(*config.Version) == 0 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("version"), *config.Version, "The version name of the API group and version used for serializing audit events must not be empty"))
	}

	return allErrs
}

// ValidateAPIServerWatchCache validates the watch cache size configuration of the Gardener API server.
func ValidateAPIServerWatchCache(config *imports.APIServerWatchCacheConfiguration, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	if config.DefaultSize != nil && *config.DefaultSize < 0 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("defaultSize"), *config.DefaultSize, "The default watch cache size cannot be negative"))
	}

	for i, resource := range config.Resources {
		path := fldPath.Child("resources").Index(i)
		if len(resource.ApiGroup) == 0 {
			allErrs = append(allErrs, field.Invalid(path.Child("apiGroup"), resource.ApiGroup, "The API Group of the watch cache resource cannot be empty"))
		}
		if len(resource.Resource) == 0 {
			allErrs = append(allErrs, field.Invalid(path.Child("resource"), resource.Resource, "The name of the watch cache resource cannot be empty"))
		}
		if resource.Size < 0 {
			allErrs = append(allErrs, field.Invalid(path.Child("size"), resource.Size, "The size of the watch cache resource cannot be negative"))
		}
	}

	return allErrs
}

// ValidateAPIServerRequests validates the requests related configuration of the Gardener API server.
func ValidateAPIServerRequests(config *imports.APIServerRequests, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	if config.MaxNonMutatingInflight != nil && *config.MaxNonMutatingInflight < 0 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("maxNonMutatingInflight"), *config.MaxNonMutatingInflight, "The MaxNonMutatingInflight field cannot be negative"))
	}
	if config.MaxMutatingInflight != nil && *config.MaxMutatingInflight < 0 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("maxMutatingInflight"), *config.MaxNonMutatingInflight, "The MaxMutatingInflight field cannot be negative"))
	}

	allErrs = append(allErrs, gardencorevalidation.ValidatePositiveDuration(config.MinTimeout, fldPath.Child("minTimeout"))...)
	allErrs = append(allErrs, gardencorevalidation.ValidatePositiveDuration(config.Timeout, fldPath.Child("timeout"))...)

	return allErrs
}

// ValidateAPIServerAdmission validates the admission configuration of the Gardener API server.
func ValidateAPIServerAdmission(config *imports.APIServerAdmissionConfiguration, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	for i, pluginConfiguration := range config.Plugins {
		path := fldPath.Child("plugins").Index(i)
		if len(pluginConfiguration.Name) == 0 {
			allErrs = append(allErrs, field.Invalid(path.Child("name"), pluginConfiguration.Name, "Admission plugin name must be set"))
		}
		if len(pluginConfiguration.Path) > 0 {
			allErrs = append(allErrs, field.Invalid(path.Child("path"), pluginConfiguration.Path, "Admission plugin path must not be set. Instead directly supply the configuration."))
		}
		if pluginConfiguration.Configuration == nil {
			allErrs = append(allErrs, field.Invalid(path.Child("configuration"), pluginConfiguration.Configuration, "Admission plugin configuration must be set"))
		}
	}

	return allErrs
}

// ValidateAPIServerETCDConfiguration validates the etcd configuration of the Gardener API server.
func ValidateAPIServerETCDConfiguration(config imports.APIServerEtcdConfiguration, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	if len(config.Url) == 0 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("url"), config.Url, "url of etcd must be set"))
	}

	if len(config.CABundle) == 0 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("caBundle"), config.CABundle, "etcd CA bundle must be set"))
	}

	allErrs = append(allErrs, ValidateCommonTLSServer(config.TLSServer, fldPath.Child("tls"))...)

	return allErrs
}

// ValidateAPIServerEncryptionConfiguration validates the encryption configuration of the Gardener API server.
func ValidateAPIServerEncryptionConfiguration(config *apiserverconfigv1.EncryptionConfiguration, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	encryptionConfigInternal := &apiserverconfig.EncryptionConfiguration{}
	if err := apiserverconfigv1.Convert_v1_EncryptionConfiguration_To_config_EncryptionConfiguration(config, encryptionConfigInternal, nil); err != nil {
		allErrs = append(allErrs, field.Invalid(fldPath, "", fmt.Sprintf("failed to convert API server encryption config: %v", err)))
		return allErrs
	}
	allErrs = append(allErrs, apiserverconfigvalidation.ValidateEncryptionConfiguration(encryptionConfigInternal)...)
	if len(allErrs) > 0 {
		allErrs = append(field.ErrorList{}, field.Invalid(fldPath, "", fmt.Sprintf("failed to validate API server encryption config: %s", allErrs.ToAggregate().Error())))
	}

	return allErrs
}

// ValidateProbe validates probes of the Gardener API server.
func ValidateProbe(probe *corev1.Probe, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	if probe.InitialDelaySeconds < 0 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("initialDelaySeconds"), probe.InitialDelaySeconds, "value must not be negative"))
	}
	if probe.PeriodSeconds < 0 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("periodSeconds"), probe.PeriodSeconds, "value must not be negative"))
	}
	if probe.SuccessThreshold < 0 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("successThreshold"), probe.SuccessThreshold, "value must not be negative"))
	}
	if probe.FailureThreshold < 0 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("failureThreshold"), probe.FailureThreshold, "value must not be negative"))
	}
	if probe.TimeoutSeconds < 0 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("timeoutSeconds"), probe.TimeoutSeconds, "value must not be negative"))
	}

	return allErrs
}

// ValidateHVPA validates the HVPA configuration of the Gardener API server deployment configuration.
func ValidateHVPA(hvpa *imports.HVPAConfiguration, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	if !hvpa.Enabled {
		return allErrs
	}
	if hvpa.MaintenanceTimeWindow != nil {
		fldMaintenance := fldPath.Child("maintenanceTimeWindow")
		if _, err := utils.ParseMaintenanceTime(hvpa.MaintenanceTimeWindow.Begin); err != nil {
			allErrs = append(allErrs, field.Invalid(fldMaintenance.Child("begin"), hvpa.MaintenanceTimeWindow.Begin, err.Error()))
		}
		if _, err := utils.ParseMaintenanceTime(hvpa.MaintenanceTimeWindow.End); err != nil {
			allErrs = append(allErrs, field.Invalid(fldMaintenance.Child("end"), hvpa.MaintenanceTimeWindow.End, err.Error()))
		}
	}

	if hvpa.HVPAConfigurationVPA != nil {
		allErrs = append(allErrs, ValidateHVPAConfigurationVPA(hvpa.HVPAConfigurationVPA, fldPath.Child("hvpaConfigurationVPA"))...)
	}

	if hvpa.HVPAConfigurationHPA != nil {
		allErrs = append(allErrs, ValidateHVPAConfigurationHPA(hvpa.HVPAConfigurationHPA, fldPath.Child("hvpaConfigurationHPA"))...)
	}

	return allErrs
}

var allowedScaleModes = sets.NewString(
	hvpav1alpha1.UpdateModeAuto,
	hvpav1alpha1.UpdateModeMaintenanceWindow,
	hvpav1alpha1.UpdateModeOff,
)

// ValidateHVPAConfigurationVPA validates the VPA configuration of HVPA
// https://github.com/gardener/hvpa-controller does not publicly expose type validation that could be reused.
// For simplicity, skip the validation of the fields ScaleUpStabilization, ScaleDownStabilization, LimitsRequestsGapScaleParams.
func ValidateHVPAConfigurationVPA(vpa *imports.HVPAConfigurationVPA, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	if vpa.ScaleUpMode != nil && !allowedScaleModes.Has(*vpa.ScaleUpMode) {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("scaleUpMode"), *vpa.ScaleUpMode, "valid scale up modes are [Auto,Off,MaintenanceWindow]"))
	}
	if vpa.ScaleDownMode != nil && !allowedScaleModes.Has(*vpa.ScaleDownMode) {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("scaleDownMode"), *vpa.ScaleUpMode, "valid scale down modes are [Auto,Off,MaintenanceWindow]"))
	}
	return allErrs
}

// ValidateHVPAConfigurationHPA validates the HPA configuration of HVPA.
func ValidateHVPAConfigurationHPA(hpa *imports.HVPAConfigurationHPA, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	if hpa.MinReplicas != nil && *hpa.MinReplicas < 0 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("minReplicas"), *hpa.MinReplicas, "value cannot be negative"))
	}
	if hpa.MaxReplicas != nil && *hpa.MaxReplicas < 0 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("maxReplicas"), *hpa.MaxReplicas, "value cannot be negative"))
	}
	if hpa.TargetAverageUtilizationCpu != nil && (*hpa.TargetAverageUtilizationCpu < 0 || *hpa.TargetAverageUtilizationCpu > 100) {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("targetAverageUtilizationCpu"), *hpa.TargetAverageUtilizationCpu, "value is invalid"))
	}
	if hpa.TargetAverageUtilizationMemory != nil && (*hpa.TargetAverageUtilizationMemory < 0 || *hpa.TargetAverageUtilizationMemory > 100) {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("targetAverageUtilizationMemory"), *hpa.TargetAverageUtilizationMemory, "value is invalid"))
	}

	return allErrs
}
