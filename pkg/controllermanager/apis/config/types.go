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

package config

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	componentbaseconfig "k8s.io/component-base/config"
	"k8s.io/klog"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ControllerManagerConfiguration defines the configuration for the Gardener controller manager.
type ControllerManagerConfiguration struct {
	metav1.TypeMeta
	// GardenClientConnection specifies the kubeconfig file and the client connection settings
	// for the proxy server to use when communicating with the garden apiserver.
	GardenClientConnection componentbaseconfig.ClientConnectionConfiguration
	// Controllers defines the configuration of the controllers.
	Controllers ControllerManagerControllerConfiguration
	// LeaderElection defines the configuration of leader election client.
	LeaderElection LeaderElectionConfiguration
	// LogLevel is the level/severity for the logs. Must be one of [info,debug,error].
	LogLevel string
	// KubernetesLogLevel is the log level used for Kubernetes' k8s.io/klog functions.
	KubernetesLogLevel klog.Level
	// Server defines the configuration of the HTTP server. This is deprecated in favor of
	// HealthServer.
	Server ServerConfiguration
	// HealthServer defines the configuration of the HTTP server providing the health & alive
	// endpoints.
	HealthServer *ServerConfiguration
	// MetricsServer defines the configuration of the HTTP server that provides the /metrics
	// endpoint.
	MetricsServer *ServerConfiguration
	// FeatureGates is a map of feature names to bools that enable or disable alpha/experimental
	// features. This field modifies piecemeal the built-in default values from
	// "github.com/gardener/gardener/pkg/controllermanager/features/features.go".
	// Default: nil
	FeatureGates map[string]bool
}

// ControllerManagerControllerConfiguration defines the configuration of the controllers.
type ControllerManagerControllerConfiguration struct {
	// Bastion defines the configuration of the Bastion controller.
	Bastion *BastionControllerConfiguration
	// CloudProfile defines the configuration of the CloudProfile controller.
	CloudProfile *CloudProfileControllerConfiguration
	// ControllerDeployment defines the configuration of the ControllerDeployment controller.
	ControllerDeployment *ControllerDeploymentControllerConfiguration
	// ControllerRegistration defines the configuration of the ControllerRegistration controller.
	ControllerRegistration *ControllerRegistrationControllerConfiguration
	// Event defines the configuration of the Event controller.  If unset, the event controller will be disabled.
	Event *EventControllerConfiguration
	// ExposureClass defines the configuration of the ExposureClass controller.
	ExposureClass *ExposureClassControllerConfiguration
	// Plant defines the configuration of the Plant controller.
	Plant *PlantControllerConfiguration
	// Project defines the configuration of the Project controller.
	Project *ProjectControllerConfiguration
	// Quota defines the configuration of the Quota controller.
	Quota *QuotaControllerConfiguration
	// SecretBinding defines the configuration of the SecretBinding controller.
	SecretBinding *SecretBindingControllerConfiguration
	// Seed defines the configuration of the Seed controller.
	Seed *SeedControllerConfiguration
	// ShootMaintenance defines the configuration of the ShootMaintenance controller.
	ShootMaintenance ShootMaintenanceControllerConfiguration
	// ShootQuota defines the configuration of the ShootQuota controller.
	ShootQuota ShootQuotaControllerConfiguration
	// ShootHibernation defines the configuration of the ShootHibernation controller.
	ShootHibernation ShootHibernationControllerConfiguration
	// ShootReference defines the configuration of the ShootReference controller. If unspecified, it is defaulted with `concurrentSyncs=5`.
	ShootReference *ShootReferenceControllerConfiguration
	// ShootRetry defines the configuration of the ShootRetry controller. If unspecified, it is defaulted with `concurrentSyncs=5`.
	ShootRetry *ShootRetryControllerConfiguration
	// ManagedSeedSet defines the configuration of the ManagedSeedSet controller.
	ManagedSeedSet *ManagedSeedSetControllerConfiguration
}

// BastionControllerConfiguration defines the configuration of the Bastion
// controller.
type BastionControllerConfiguration struct {
	// ConcurrentSyncs is the number of workers used for the controller to work on
	// events.
	ConcurrentSyncs int
	// MaxLifetime is the maximum time a Bastion resource can exist before it is
	// forcefully deleted (defaults to '24h').
	MaxLifetime *metav1.Duration
}

// CloudProfileControllerConfiguration defines the configuration of the CloudProfile
// controller.
type CloudProfileControllerConfiguration struct {
	// ConcurrentSyncs is the number of workers used for the controller to work on
	// events.
	ConcurrentSyncs int
}

// ControllerDeploymentControllerConfiguration defines the configuration of the
// ControllerDeployment controller.
type ControllerDeploymentControllerConfiguration struct {
	// ConcurrentSyncs is the number of workers used for the controller to work on
	// events.
	ConcurrentSyncs int
}

// ControllerRegistrationControllerConfiguration defines the configuration of the
// ControllerRegistration controller.
type ControllerRegistrationControllerConfiguration struct {
	// ConcurrentSyncs is the number of workers used for the controller to work on
	// events.
	ConcurrentSyncs int
}

// PlantControllerConfiguration defines the configuration of the
// PlantControllerConfiguration controller.
type PlantControllerConfiguration struct {
	// ConcurrentSyncs is the number of workers used for the controller to work on
	// events.
	ConcurrentSyncs int
	// SyncPeriod is the duration how often the existing resources are reconciled.
	SyncPeriod metav1.Duration
}

// EventControllerConfiguration defines the configuration of the Event controller.
type EventControllerConfiguration struct {
	// ConcurrentSyncs is the number of workers used for the controller to work on
	// events.
	ConcurrentSyncs int
	// TTLNonShootEvents is the time-to-live for all non-shoot related events (defaults to `1h`).
	TTLNonShootEvents *metav1.Duration
}

// ExposureClassControllerConfiguration defines the configuration of the
// ExposureClass controller.
type ExposureClassControllerConfiguration struct {
	// ConcurrentSyncs is the number of workers used for the controller to work on
	// events.
	ConcurrentSyncs int
}

// ProjectControllerConfiguration defines the configuration of the
// Project controller.
type ProjectControllerConfiguration struct {
	// ConcurrentSyncs is the number of workers used for the controller to work on
	// events.
	ConcurrentSyncs int
	// MinimumLifetimeDays is the number of days a `Project` may exist before it is being
	// checked whether it is actively used or got stale.
	MinimumLifetimeDays *int
	// Quotas is the default configuration matching projects are set up with if a quota is not already specified.
	Quotas []QuotaConfiguration
	// StaleGracePeriodDays is the number of days a `Project` may be unused/stale before a
	// timestamp for an auto deletion is computed.
	StaleGracePeriodDays *int
	// StaleExpirationTimeDays is the number of days after a `Project` that has been marked as
	// 'stale'/'unused' and passed the 'stale grace period' will be considered for auto deletion.
	StaleExpirationTimeDays *int
	// StaleSyncPeriod is the duration how often the reconciliation loop for stale Projects is executed.
	StaleSyncPeriod *metav1.Duration
}

// QuotaConfiguration defines quota configurations.
type QuotaConfiguration struct {
	// Config is the quota specification used for the project set-up.
	// Only v1.ResourceQuota resources are supported.
	Config runtime.Object
	// ProjectSelector is an optional setting to select the projects considered for quotas.
	// Defaults to empty LabelSelector, which matches all projects.
	ProjectSelector *metav1.LabelSelector
}

// QuotaControllerConfiguration defines the configuration of the Quota controller.
type QuotaControllerConfiguration struct {
	// ConcurrentSyncs is the number of workers used for the controller to work on
	// events.
	ConcurrentSyncs int
}

// SecretBindingControllerConfiguration defines the configuration of the
// SecretBinding controller.
type SecretBindingControllerConfiguration struct {
	// ConcurrentSyncs is the number of workers used for the controller to work on
	// events.
	ConcurrentSyncs int
}

// SeedControllerConfiguration defines the configuration of the
// Seed controller.
type SeedControllerConfiguration struct {
	// ConcurrentSyncs is the number of workers used for the controller to work on
	// events.
	ConcurrentSyncs int
	// MonitorPeriod is the duration after the seed controller will mark the `GardenletReady`
	// condition in `Seed` resources as `Unknown` in case the gardenlet did not send heartbeats.
	MonitorPeriod *metav1.Duration
	// ShootMonitorPeriod is the duration after the seed controller will mark Gardener's conditions
	// in `Shoot` resources as `Unknown` in case the gardenlet of the responsible seed cluster did
	// not send heartbeats.
	ShootMonitorPeriod *metav1.Duration
	// SyncPeriod is the duration how often the existing resources are reconciled.
	SyncPeriod metav1.Duration
}

// ShootMaintenanceControllerConfiguration defines the configuration of the
// ShootMaintenance controller.
type ShootMaintenanceControllerConfiguration struct {
	// ConcurrentSyncs is the number of workers used for the controller to work on
	// events.
	ConcurrentSyncs int
	// EnableShootControlPlaneRestarter configures whether adequate pods of the shoot control plane are restarted during maintenance.
	EnableShootControlPlaneRestarter *bool
	// EnableShootCoreAddonRestarter configures whether some core addons to be restarted during maintenance.
	EnableShootCoreAddonRestarter *bool
}

// ShootQuotaControllerConfiguration defines the configuration of the
// ShootQuota controller.
type ShootQuotaControllerConfiguration struct {
	// ConcurrentSyncs is the number of workers used for the controller to work on
	// events.
	ConcurrentSyncs int
	// SyncPeriod is the duration how often the existing resources are reconciled
	// (how often Shoots referenced Quota is checked).
	SyncPeriod metav1.Duration
}

// ShootHibernationControllerConfiguration defines the configuration of the
// ShootHibernation controller.
type ShootHibernationControllerConfiguration struct {
	// ConcurrentSyncs is the number of workers used for the controller to work on
	// events.
	ConcurrentSyncs int
}

// ShootReferenceControllerConfiguration defines the configuration of the
// ShootReference controller.
type ShootReferenceControllerConfiguration struct {
	// ConcurrentSyncs is the number of workers used for the controller to work on
	// shoots.
	ConcurrentSyncs int
	// ProtectAuditPolicyConfigMaps controls whether the shoot reference controller shall protect ConfigMaps containing
	// audit policies and referenced in Shoots.
	ProtectAuditPolicyConfigMaps *bool
}

// ShootRetryControllerConfiguration defines the configuration of the
// ShootRetry controller.
type ShootRetryControllerConfiguration struct {
	// ConcurrentSyncs is the number of workers used for the controller to work on
	// events.
	ConcurrentSyncs int
	// RetryPeriod is the retry period for retrying failed Shoots that match certain criterion.
	RetryPeriod *metav1.Duration
}

// ManagedSeedSetControllerConfiguration defines the configuration of the
// ManagedSeedSet controller.
type ManagedSeedSetControllerConfiguration struct {
	// ConcurrentSyncs is the number of workers used for the controller to work on
	// events.
	ConcurrentSyncs int
	// MaxShootRetries is the maximum number of times to retry failed shoots before giving up. Defaults to 3.
	MaxShootRetries *int
	// SyncPeriod is the duration how often the existing resources are reconciled.
	SyncPeriod metav1.Duration
}

// LeaderElectionConfiguration defines the configuration of leader election
// clients for components that can run with leader election enabled.
type LeaderElectionConfiguration struct {
	componentbaseconfig.LeaderElectionConfiguration
	// LockObjectNamespace defines the namespace of the lock object.
	LockObjectNamespace string
	// LockObjectName defines the lock object name.
	LockObjectName string
}

// ServerConfiguration contains details for the HTTP(S) servers.
type ServerConfiguration struct {
	// HTTP is the configuration for the HTTP server.
	HTTP Server
	// HTTPS is the configuration for the HTTPS server.
	HTTPS HTTPSServer
}

// Server contains information for HTTP(S) server configuration.
type Server struct {
	// BindAddress is the IP address on which to listen for the specified port.
	BindAddress string
	// Port is the port on which to serve requests.
	Port int
}

// HTTPSServer is the configuration for the HTTPSServer server.
type HTTPSServer struct {
	// Server is the configuration for the bind address and the port.
	Server
	// TLSServer contains information about the TLS configuration for a HTTPS server.
	TLS TLSServer
}

// TLSServer contains information about the TLS configuration for a HTTPS server.
type TLSServer struct {
	// ServerCertPath is the path to the server certificate file.
	ServerCertPath string
	// ServerKeyPath is the path to the private key file.
	ServerKeyPath string
}

const (
	// ControllerManagerDefaultLockObjectNamespace is the default lock namespace for leader election.
	ControllerManagerDefaultLockObjectNamespace = "garden"

	// ControllerManagerDefaultLockObjectName is the default lock name for leader election.
	ControllerManagerDefaultLockObjectName = "gardener-controller-manager-leader-election"
)
