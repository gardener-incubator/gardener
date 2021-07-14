// Copyright (c) 2019 SAP SE or an SAP affiliate company. All rights reserved. This file is licensed under the Apache Software License, v. 2 except as noted otherwise in the LICENSE file
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
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	componentbaseconfig "k8s.io/component-base/config"
)

const (
	// SameRegion Strategy determines a seed candidate for a shoot only if the cloud profile and region are identical
	SameRegion CandidateDeterminationStrategy = "SameRegion"
	// MinimalDistance Strategy determines a seed candidate for a shoot if the cloud profile are identical. Then chooses the seed with the minimal distance to the shoot.
	MinimalDistance CandidateDeterminationStrategy = "MinimalDistance"
	// Default Strategy is the default strategy to use when there is no configuration provided
	Default CandidateDeterminationStrategy = SameRegion
	// SchedulerDefaultLockObjectNamespace is the default lock namespace for leader election.
	SchedulerDefaultLockObjectNamespace = "garden"
	// SchedulerDefaultLockObjectName is the default lock name for leader election.
	SchedulerDefaultLockObjectName = "gardener-scheduler-leader-election"
	// SchedulerDefaultConfigurationConfigMapNamespace is the namespace of the scheduler configuration config map
	SchedulerDefaultConfigurationConfigMapNamespace = "garden"
	// SchedulerDefaultConfigurationConfigMapName is the name of the scheduler configuration config map
	SchedulerDefaultConfigurationConfigMapName = "gardener-scheduler-configmap"
	// DefaultDiscoveryTTL is the default ttl for the cached discovery client.
	DefaultDiscoveryTTL = 10 * time.Second
)

// Strategies defines all currently implemented SeedCandidateDeterminationStrategies
var Strategies = []CandidateDeterminationStrategy{SameRegion, MinimalDistance}

// CandidateDeterminationStrategy defines how seeds for shoots, that do not specify a seed explicitly, are being determined
type CandidateDeterminationStrategy string

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// SchedulerConfiguration provides the configuration for the Gardener scheduler
type SchedulerConfiguration struct {
	metav1.TypeMeta

	// ClientConnection specifies the kubeconfig file and client connection
	// settings for the proxy server to use when communicating with the gardener-apiserver.
	ClientConnection componentbaseconfig.ClientConnectionConfiguration
	// LeaderElection defines the configuration of leader election client.
	LeaderElection LeaderElectionConfiguration
	// LogLevel is the level/severity for the logs. Must be one of [info,debug,error].
	LogLevel string
	// Server defines the configuration of the HTTP server. This is deprecated in favor of
	// HealthServer.
	Server ServerConfiguration
	// HealthServer defines the configuration of the HTTP server providing the health & alive
	// endpoints.
	HealthServer *ServerConfiguration
	// MetricsServer defines the configuration of the HTTP server that provides the /metrics
	// endpoint.
	MetricsServer *ServerConfiguration
	// Scheduler defines the configuration of the schedulers.
	Schedulers SchedulerControllerConfiguration
	// FeatureGates is a map of feature names to bools that enable or disable alpha/experimental
	// features. This field modifies piecemeal the built-in default values from
	// "github.com/gardener/gardener/pkg/scheduler/features/features.go".
	// Default: nil
	FeatureGates map[string]bool
}

// SchedulerControllerConfiguration defines the configuration of the controllers.
type SchedulerControllerConfiguration struct {
	// BackupBucket defines the configuration of the BackupBucket controller.
	// +optional
	BackupBucket *BackupBucketSchedulerConfiguration
	// Shoot defines the configuration of the Shoot controller.
	// +optional
	Shoot *ShootSchedulerConfiguration
}

// BackupBucketSchedulerConfiguration defines the configuration of the BackupBucket to Seed
// scheduler.
type BackupBucketSchedulerConfiguration struct {
	// ConcurrentSyncs is the number of workers used for the controller to work on
	// events.
	ConcurrentSyncs int
}

// BackupEntrySchedulerConfiguration defines the configuration of the BackupEntry to Seed
// scheduler.
type BackupEntrySchedulerConfiguration struct {
	// ConcurrentSyncs is the number of workers used for the controller to work on
	// events.
	ConcurrentSyncs int
	// RetrySyncPeriod is the duration how fast BackupEntries with an errornous operation are
	// re-added to the queue so that the operation can be retried. Defaults to 15s.
	// +optional
	RetrySyncPeriod metav1.Duration
}

// ShootSchedulerConfiguration defines the configuration of the Shoot to Seed
// scheduler.
type ShootSchedulerConfiguration struct {
	// ConcurrentSyncs is the number of workers used for the controller to work on
	// events.
	ConcurrentSyncs int
	// Strategy defines how seeds for shoots, that do not specify a seed explicitly, are being determined
	Strategy CandidateDeterminationStrategy
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
}

// Server contains information for HTTP(S) server configuration.
type Server struct {
	// BindAddress is the IP address on which to listen for the specified port.
	BindAddress string
	// Port is the port on which to serve unsecured, unauthenticated access.
	Port int
}
