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

package imports

import (
	hvpav1alpha1 "github.com/gardener/hvpa-controller/api/v1alpha1"
	landscaperv1alpha1 "github.com/gardener/landscaper/apis/core/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiserverv1 "k8s.io/apiserver/pkg/apis/apiserver/v1"
	auditv1 "k8s.io/apiserver/pkg/apis/audit/v1"
	apiserverconfigv1 "k8s.io/apiserver/pkg/apis/config/v1"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// GardenerAPIServer contains the configuration of the Gardener Aggregated API Server
type GardenerAPIServer struct {
	// DeploymentConfiguration contains optional configurations for
	// the deployment of the Gardener API server
	DeploymentConfiguration *APIServerDeploymentConfiguration
	// ComponentConfiguration contains optional configurations for
	// the Gardener Extension API server
	ComponentConfiguration APIServerComponentConfiguration
}

// APIServerDeploymentConfiguration contains certain configurations for the deployment
// of the Gardener Extension API server
type APIServerDeploymentConfiguration struct {
	// CommonDeploymentConfiguration contains common deployment configurations
	// Defaults:
	//  Resources: Requests (CPU: 100m, memory 100Mi), Limits (CPU: 300m, memory: 256Mi)
	CommonDeploymentConfiguration
	// LivenessProbe allows to overwrite the default liveness probe
	// Defaults:
	//  initialDelaySeconds: 15
	//  periodSeconds: 10
	//  successThreshold: 1
	//  failureThreshold: 3
	//  timeoutSeconds: 15
	LivenessProbe *corev1.Probe
	// LivenessProbe allows to overwrite the default readiness probe
	// Defaults:
	//  initialDelaySeconds: 15
	//  periodSeconds: 10
	//  successThreshold: 1
	//  failureThreshold: 3
	//  timeoutSeconds: 15
	ReadinessProbe *corev1.Probe
	// MinReadySeconds allows to overwrite the default minReadySeconds field
	// Defaults to 30
	MinReadySeconds *int32
	// Hvpa contains configurations for the HVPA of the Gardener Extension API server
	// Please note that VPA has to be disabled in order to use HVPA
	Hvpa *HVPAConfiguration
}

// TODO: Add Configuration related to the authorization step in the Kubernetes API request once 'SeedAuthorizer' is ready
// Allow configuration of webhook authorization mode required for the Gardener 'SeedAuthorizer' authorization webhook
// for more information, see:
//  - https://kubernetes.io/docs/reference/access-authn-authz/authorization/
//  - https://github.com/gardener/gardener/blob/master/docs/deployment/gardenlet_api_access.md#seedauthorizer-authorization-webhook-enablement

// APIServerComponentConfiguration contains configurations for the Gardener Extension API server
type APIServerComponentConfiguration struct {
	// ClusterIdentity is a unique identity per Gardener installation.
	// Can be any string that uniquely identifies the landscape
	ClusterIdentity string
	// Encryption configures an optional encryption configuration
	// Defaults:
	// - resources (controllerregistrations.core.gardener.cloud, shootstates.core.gardener.cloud)
	//   providers: (identity: {})
	Encryption *apiserverconfigv1.EncryptionConfiguration
	// Etcd contains configuration for the etcd of the Gardener API server
	Etcd APIServerEtcdConfiguration
	// CABundle is a PEM encoded CA bundle which will be used by the Kubernetes API server
	// (either the RuntimeCluster or the VirtualGarden cluster)
	// to validate the Gardener Extension API server's TLS serving certificate.
	// It is put into the APIService resources for the Gardener resource groups
	// The TLS serving certificate of the Gardener Extension API server
	// has to be signed by this CA.
	// For more information, please see:
	// https://kubernetes.io/docs/tasks/extend-kubernetes/configure-aggregation-layer/#contacting-the-extension-apiserver
	CABundle string
	// TLS contains the TLS serving certificate & key of the Gardener Extension API server
	TLS TLSServer
	// FeatureGates are optional feature gates that shall be activated on the Gardener API server
	FeatureGates []string
	// Admission contains admission configuration for the Gardener API server
	Admission *APIServerAdmissionConfiguration
	// GoAwayChance sets the fraction of requests that will be sent a GOAWAY.
	// Clusters with single apiservers, or which don't use a load balancer,
	// should NOT enable this.
	// Min is 0 (off), Max is .02 (1/50 requests); .001 (1/1000) is a recommended starting point.
	GoAwayChance *float32
	// Http2MaxStreamsPerConnection is the limit that the server gives to clients for the maximum number of streams
	// in an HTTP/2 connection. Zero means to use golang's default
	Http2MaxStreamsPerConnection *int32
	// ShutdownDelayDuration is the time to delay the termination. During that time the server keeps serving requests normally.
	// The endpoints /healthz and /livez will return success, but /readyz immediately returns failure.
	// Graceful termination starts after this delay has elapsed.
	// This can be used to allow load balancer to stop sending traffic to this server.
	ShutdownDelayDuration *metav1.Duration
	// Requests are optional request related configuration of the Gardener API Server
	Requests *APIServerRequests
	// WatchCacheSize optionally configures the watch cache size for resources watched by the Gardener API Server
	WatchCacheSize *APIServerWatchCacheConfiguration
	// Audit contains optional audit logging configuration.
	// Can be used to override the Gardener default audit logging policy or disable audit logging altogether.
	Audit *APIServerAuditConfiguration
}

// HVPAConfiguration contains configurations for the HVPA of the Gardener Extension API server
// For more information on HVPA, please see here: https://github.com/gardener/hvpa-controller
type HVPAConfiguration struct {
	// Enabled configures whether to setup hvpa for the Gardener Extension API server or not
	// Default: false
	Enabled bool
	// MaintenanceWindow defines the time window when HVPA is allowed to act
	MaintenanceTimeWindow *hvpav1alpha1.MaintenanceTimeWindow
	// HVPAConfigurationHPA contains the HPA specific configuration for HVPA
	HVPAConfigurationHPA *HVPAConfigurationHPA
	// HVPAConfigurationVPA contains the VPA specific configuration for HVPA
	HVPAConfigurationVPA *HVPAConfigurationVPA
}

// HVPAConfigurationHPA contains HPA related configuration for the HVPA of the Gardener Extension API server
type HVPAConfigurationHPA struct {
	// MinReplicas is the minimum number of replicas.
	// Defaults to 1.
	MinReplicas *int32
	// MaxReplicas is the maximum number of replicas.
	// Defaults to 4.
	MaxReplicas *int32
	// TargetAverageUtilizationCpu is the average CPU utilization targeted by the HPA component of
	// the HVPA
	// Defaults to: 80
	TargetAverageUtilizationCpu *int32
	// TargetAverageUtilizationMemory is the average memory utilization targeted by the HPA component of
	// the HVPA
	// Defaults to: 80
	TargetAverageUtilizationMemory *int32
}

// HVPAConfigurationVPA contains VPA related configuration for the HVPA of the Gardener Extension API server
type HVPAConfigurationVPA struct {
	// ScaleUpMode controls when the VPA component of HVPA scales up
	// Possible values: "Auto", "Off", "MaintenanceWindow"
	// Defaults to: "Auto"
	ScaleUpMode *string
	// ScaleDownMode controls when the VPA component of HVPA scales down
	// Possible values: "Auto", "Off", "MaintenanceWindow"
	// Defaults to: "Auto"
	ScaleDownMode *string
	// ScaleUpStabilization defines parameters for the VPA component of HVPA for scale up
	// Defaults:
	//  stabilizationDuration: "3m"
	//    minChange:
	//      cpu:
	//        value: 300m
	//        percentage: 80
	//      memory:
	//        value: 200M
	//        percentage: 80
	ScaleUpStabilization *hvpav1alpha1.ScaleType
	// ScaleDownStabilization defines parameters for the VPA component of HVPA for scale down
	// Defaults:
	//  stabilizationDuration: "15m"
	//  minChange:
	//    cpu:
	//      value: 600m
	//      percentage: 80
	//    memory:
	//      value: 600M
	//      percentage: 80
	ScaleDownStabilization *hvpav1alpha1.ScaleType
	// LimitsRequestsGapScaleParams is the scaling thresholds for limits
	// Defaults:
	//  cpu:
	//    value: "1"
	//    percentage: 70
	//  memory:
	//    value: "1G"
	//    percentage: 70
	LimitsRequestsGapScaleParams *hvpav1alpha1.ScaleParams
}

// APIServerEtcdConfiguration contains configuration for the etcd of the Gardener API server
// etcd is a required as a prerequisite
type APIServerEtcdConfiguration struct {
	// Url is the 'url:port' of the etcd of the Gardener API server
	// If the etcd is deployed in-cluster, should be of the form 'k8s-service-name:port'
	Url string
	// CABundle is a PEM encoded CA bundle which will be used by the Gardener API server
	// to verify that the TLS certificate presented by etcd is signed by this CA
	CABundle string
	// TLSServer contains a certificate and key which will be used by the Gardener API server
	// to communicate with etcd via TLS
	TLSServer
}

// APIServerAuditConfiguration contains audit logging configuration
// For more information, please see: https://kubernetes.io/docs/tasks/debug-application-cluster/audit/
type APIServerAuditConfiguration struct {
	// DynamicConfiguration is used to enable dynamic auditing before v1.19 via API server flag --audit-dynamic-configuration.
	// This feature also requires the DynamicAuditing feature flag to be set.
	DynamicConfiguration *bool
	// Policy contains the audit policy for the Gardener API Server.
	// For more information, please see here: https://kubernetes.io/docs/reference/config-api/apiserver-audit.v1/#audit-k8s-io-v1-Policy
	// If set, overrides the default policy:
	//  rules:
	//  - level: None
	//    userGroups:
	//    - system:nodes
	//  - level: None
	//    users:
	//    - gardener
	//    - kubelet
	//    - system:kube-controller-manager
	//    - system:kube-scheduler
	//    - system:gardener-resource-manager
	//    - system:kube-aggregator
	//    - system:kube-proxy
	//    - system:apiserver
	//    - system:serviceaccount:garden:gardener-controller-manager
	//    - system:serviceaccount:garden:gardener-metrics-exporter
	//    - system:serviceaccount:kube-system:generic-garbage-collector
	//    - system:serviceaccount:kube-system:namespace-controller
	//    - garden.sapcloud.io:monitoring
	//    - garden.sapcloud.io:monitoring:prometheus
	//    - garden.sapcloud.io:monitoring:kube-state-metrics
	//    - gardener.cloud:monitoring
	//    - gardener.cloud:monitoring:prometheus
	//    - gardener.cloud:monitoring:kube-state-metrics
	//  - level: None
	//    nonResourceURLs:
	//    - /healthz*
	//    - /version
	//    - /openapi/*
	//    - /swagger* # Swagger endpoint is deprecated with https://github.com/kubernetes/kubernetes/pull/73148
	//  - level: None
	//    resources:
	//    - group: ""
	//      resources: ["events"]
	//  - level: None
	//    verbs: ["get", "list", "watch"]
	//  - level: Metadata
	Policy *auditv1.Policy
	// Log configures the Log backend for audit events
	// This is enabled with a default policy logging to the local filesystem
	// For more information, please see here: https://kubernetes.io/docs/tasks/debug-application-cluster/audit/#log-backend
	Log *APIServerAuditLogBackend
	// Webhook contains configuration for the webhook audit backend for the Gardener API server
	// For more information, please see: https://kubernetes.io/docs/tasks/debug-application-cluster/audit/#webhook-backend
	Webhook *APIServerAuditWebhookBackend
}

// APIServerAuditWebhookBackend contains configuration for the webhook
// audit backend for the Gardener API server.
// The webhook audit backend sends audit events to a remote web API, which is
// assumed to be a form of the Kubernetes API, including means of authentication.
// For more information, please see here: https://kubernetes.io/docs/tasks/debug-application-cluster/audit/#webhook-backend
type APIServerAuditWebhookBackend struct {
	APIServerAuditCommonBackendConfiguration
	// Kubeconfig is the kubeconfig for the external audit log backend
	Kubeconfig landscaperv1alpha1.Target
	// InitialBackoff specifies the amount of time to wait after the first failed request before retrying.
	// Subsequent requests are retried with exponential backoff.
	InitialBackoff *metav1.Duration
}

// APIServerAuditLogBackend are various audit-related settings for the Gardener API server.
type APIServerAuditLogBackend struct {
	APIServerAuditCommonBackendConfiguration
	// Format of saved audits.
	// "legacy" indicates 1-line text format for each event.
	// "json" indicates structured json format.
	Format *string
	// MaxAge is the maximum number of days to retain old audit log files based on the timestamp encoded in their filename.
	MaxAge *int32
	// MaxBackup is the maximum number of old audit log files to retain.
	// Default: 5
	MaxBackup *int32
	// MaxSize is the maximum size in megabytes of the audit log file before it gets rotated.
	// Default: 100
	MaxSize *int32
	// Path is the path that if set, contains the audit logs of all requests coming to the API server.
	// '-' means standard out.
	// Default: /var/lib/audit.log
	Path *string
}

// APIServerAdmissionConfiguration contains admission configuration for the Gardener API server
type APIServerAdmissionConfiguration struct {
	// EnableAdmissionPlugins is a list of names of admission plugins to be enabled in addition to default enabled ones
	EnableAdmissionPlugins []string
	// DisableAdmissionPlugins are a list of names of admission plugins that should be disabled although they are
	// in the default enabled plugins list.
	DisableAdmissionPlugins []string
	// Plugins contains the name and configuration of admission plugins of the Gardener API server
	// Mutating and Validating admission plugins must not be added.
	// For more information, see here: https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#authenticate-apiservers
	Plugins []apiserverv1.AdmissionPluginConfiguration
	// ValidatingWebhook configures client-credentials to authenticate to validating webhooks
	ValidatingWebhook *APIServerAdmissionWebhookCredentials
	// MutatingWebhook configures client-credentials to authenticate to validating webhooks
	MutatingWebhook *APIServerAdmissionWebhookCredentials
}

// APIServerAdmissionWebhookCredentials is required if your admission webhooks require authentication.
// Contains client-credentials that can be used by the Gardener API server to authenticate to registered Webhooks.
// Also see https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#authenticate-apiservers
type APIServerAdmissionWebhookCredentials struct {
	// Kubeconfig contains the kubeconfig with credentials to authenticate to an admission webhook.
	// Either use static credentials basic auth, x509 client-certificate, static token file
	// or use Service Account Volume Projection to automatically create and rotate the token
	// configured in the kubeconfig file.
	// If token projection is enabled, and this kubeconfig is not set, will default to a kubeconfig
	// with name '*' and path of the projected service account token.
	// TODO: defaulting for the token projection kubeconfig when adding the v1alpha1 version
	Kubeconfig *landscaperv1alpha1.Target
	// TokenProjection enables a projected volume with a service account for the admission webhook credentials.
	// Requires Service Account Volume Projection to be configured in the runtime cluster.
	// For more information, see here: https://kubernetes.io/docs/tasks/configure-pod-container/configure-service-account/#service-account-token-volume-projection
	// if configured, the kubeconfig must contain a 'tokenFile' with the path of the projected
	// service account token. The projected volume will be mounted at '/var/run/secrets/admission-tokens' with relative path of
	// either 'mutating-webhook-token' or 'validating-webhook-token'.
	TokenProjection *APIServerAdmissionWebhookCredentialsTokenProjection
}

// APIServerAdmissionWebhookCredentialsTokenProjection configures
// Service Account Volume Projection to be used for the APIServer Admission Webhook credentials
type APIServerAdmissionWebhookCredentialsTokenProjection struct {
	// Enabled configures if Service Account Volume Projection is used
	Enabled bool
	// Audience contains the intended audience of the token.
	// A recipient of the token must identify itself with an identifier specified in the audience of the token,
	// and otherwise should reject the token.
	// Defaults to 'validating-webhook' / 'mutating-webhook'
	Audience *string
	// ExpirationSeconds is the expected duration of validity of the service account token
	// Defaults to 3600
	ExpirationSeconds *int32
}

// APIServerRequests are request related configuration of the Gardener API Server
type APIServerRequests struct {
	// MaxNonMutatingInflight is the maximum number of non-mutating requests in flight at a given time.
	// When the server exceeds this, it rejects requests. Zero for no limit.
	MaxNonMutatingInflight *int
	// MaxMutatingInflight is the maximum number of mutating requests in flight at a given time.
	// When the server exceeds this, it rejects requests. Zero for no limit.
	MaxMutatingInflight *int
	// MinTimeout is an optional field indicating the minimum number of seconds
	// a handler must keep a request open before timing it out.
	// Currently only honored by the watch request handler, which picks a randomized
	// value above this number as the connection timeout, to spread out load.
	MinTimeout *metav1.Duration
	// Timeout is an optional field indicating the duration a handler must keep a request open before timing it out.
	// This is the default request timeout for requests but may be overridden by MinTimeout for the watch request handler.
	Timeout *metav1.Duration
}

// APIServerWatchCacheConfiguration fine tunes the watch cache size for different resources
// watched by the Gardener API Server.
// These are mostly, but not limited to, resources from Gardener resource groups e.g core.gardener.cloud.
// Some resources (replicationcontrollers, endpoints, nodes, pods, services, apiservices.apiregistration.k8s.io)
// have system defaults set by heuristics, others default to 'defaultSize'.
type APIServerWatchCacheConfiguration struct {
	// DefaultSize is the default watch cache size
	DefaultSize *int32
	// Resources contains a list of configurations of the watch cache sizes
	Resources []WatchCacheSizeResource
}

// WatchCacheSizeResource configures the watch cache of one resource
type WatchCacheSizeResource struct {
	// ApiGroup is the API Group of the resource (e.g core.gardener.cloud)
	ApiGroup string
	// Resource is the name of the resource (e.g shoots)
	Resource string
	// Size is the size of the watch cache (how many resources are cached)
	Size int32
}

// APIServerAuditCommonBackendConfiguration contains audit configuration
// applicable for several audit log backends (log, webhook)
type APIServerAuditCommonBackendConfiguration struct {
	// BatchBufferSize is the size of the buffer to store events before batching and writing.
	// Only used in batch mode.
	BatchBufferSize *int32
	// BatchMaxSize is the maximum size of a batch.
	// Only used in batch mode.
	BatchMaxSize *int32
	// BatchMaxWait is the amount of time to wait before force writing the batch that hadn't reached the max size.
	// Only used in batch mode.
	BatchMaxWait *metav1.Duration
	// BatchThrottleBurst is the maximum number of requests sent at the same moment
	// if ThrottleQPS was not utilized before.
	// Only used in batch mode.
	BatchThrottleBurst *int32
	// BatchThrottleEnable defines whether batching throttling is enabled.
	// Only used in batch mode.
	// Default: true
	BatchThrottleEnable *bool
	// BatchThrottleQPS is the maximum average number of batches per second.
	// Only used in batch mode.
	BatchThrottleQPS *float32
	// Mode is the strategy for sending audit events. Blocking indicates sending
	// events should block server responses. Batch causes the backend to buffer and write events asynchronously.
	// Known modes are batch,blocking,blocking-strict.
	Mode *string
	// TruncateEnabled configures whether event and batch truncating is enabled.
	TruncateEnabled *bool
	// TruncateMaxBatchSize is the maximum size of the batch sent to the underlying backend.
	// If a batch exceeds this limit, it is split into several batches of smaller size.
	// Actual serialized size can be several hundreds of bytes greater.
	// Only used in batch mode.
	TruncateMaxBatchSize *int32
	// TruncateMaxEventSize is the maximum size of the audit event sent to the underlying backend.
	// If the size of an event is greater than this number, first request and response are removed, and if this doesn't reduce the size enough,
	// event is discarded.
	TruncateMaxEventSize *int32
	// Version is the API group and version used for serializing audit events written to log.
	Version *string
}
