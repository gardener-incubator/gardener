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

package core

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ErrorCode is a string alias.
type ErrorCode string

const (
	// ErrorInfraUnauthorized indicates that the last error occurred due to invalid infrastructure credentials.
	// It is classified as a non-retryable error code.
	ErrorInfraUnauthorized ErrorCode = "ERR_INFRA_UNAUTHORIZED"
	// ErrorInfraInsufficientPrivileges indicates that the last error occurred due to insufficient infrastructure privileges.
	// It is classified as a non-retryable error code.
	ErrorInfraInsufficientPrivileges ErrorCode = "ERR_INFRA_INSUFFICIENT_PRIVILEGES"
	// ErrorInfraQuotaExceeded indicates that the last error occurred due to infrastructure quota limits.
	// It is classified as a non-retryable error code.
	ErrorInfraQuotaExceeded ErrorCode = "ERR_INFRA_QUOTA_EXCEEDED"
	// ErrorInfraRateLimitsExceeded indicates that the last error occurred due to exceeded infrastructure request rate limits.
	ErrorInfraRateLimitsExceeded ErrorCode = "ERR_INFRA_RATE_LIMITS_EXCEEDED"
	// ErrorInfraDependencies indicates that the last error occurred due to dependent objects on the infrastructure level.
	// It is classified as a non-retryable error code.
	ErrorInfraDependencies ErrorCode = "ERR_INFRA_DEPENDENCIES"
	// ErrorRetryableInfraDependencies indicates that the last error occurred due to dependent objects on the infrastructure level, but operation should be retried.
	ErrorRetryableInfraDependencies ErrorCode = "ERR_RETRYABLE_INFRA_DEPENDENCIES"
	// ErrorInfraResourcesDepleted indicates that the last error occurred due to depleted resource in the infrastructure.
	ErrorInfraResourcesDepleted ErrorCode = "ERR_INFRA_RESOURCES_DEPLETED"
	// ErrorCleanupClusterResources indicates that the last error occurred due to resources in the cluster that are stuck in deletion.
	ErrorCleanupClusterResources ErrorCode = "ERR_CLEANUP_CLUSTER_RESOURCES"
	// ErrorConfigurationProblem indicates that the last error occurred due to a configuration problem.
	// It is classified as a non-retryable error code.
	ErrorConfigurationProblem ErrorCode = "ERR_CONFIGURATION_PROBLEM"
	// ErrorRetryableConfigurationProblem indicates that the last error occurred due to a retryable configuration problem.
	ErrorRetryableConfigurationProblem ErrorCode = "ERR_RETRYABLE_CONFIGURATION_PROBLEM"
)

// LastError indicates the last occurred error for an operation on a resource.
type LastError struct {
	// A human readable message indicating details about the last error.
	Description string
	// ID of the task which caused this last error
	TaskID *string
	// Well-defined error codes of the last error(s).
	// +optional
	Codes []ErrorCode
	// Last time the error was reported
	LastUpdateTime *metav1.Time
}

// LastOperationType is a string alias.
type LastOperationType string

const (
	// LastOperationTypeReconcile indicates a 'reconcile' operation.
	LastOperationTypeReconcile LastOperationType = "Reconcile"
	// LastOperationTypeDelete indicates a 'delete' operation.
	LastOperationTypeDelete LastOperationType = "Delete"
	// LastOperationTypeRestore indicates a 'restore' operation.
	LastOperationTypeRestore LastOperationType = "Restore"
	// LastOperationTypeRestore indicates a 'migrate' operation.
	LastOperationTypeMigrate LastOperationType = "Migrate"
)

// LastOperationState is a string alias.
type LastOperationState string

const (
	// LastOperationStateProcessing indicates that an operation is ongoing.
	LastOperationStateProcessing LastOperationState = "Processing"
	// LastOperationStateSucceeded indicates that an operation has completed successfully.
	LastOperationStateSucceeded LastOperationState = "Succeeded"
	// LastOperationStateError indicates that an operation is completed with errors and will be retried.
	LastOperationStateError LastOperationState = "Error"
	// LastOperationStateFailed indicates that an operation is completed with errors and won't be retried.
	LastOperationStateFailed LastOperationState = "Failed"
	// LastOperationStatePending indicates that an operation cannot be done now, but will be tried in future.
	LastOperationStatePending LastOperationState = "Pending"
	// LastOperationStateAborted indicates that an operation has been aborted.
	LastOperationStateAborted LastOperationState = "Aborted"
)

// LastOperation indicates the type and the state of the last operation, along with a description
// message and a progress indicator.
type LastOperation struct {
	// A human readable message indicating details about the last operation.
	Description string
	// Last time the operation state transitioned from one to another.
	LastUpdateTime metav1.Time
	// The progress in percentage (0-100) of the last operation.
	Progress int32
	// Status of the last operation, one of Aborted, Processing, Succeeded, Error, Failed.
	State LastOperationState
	// Type of the last operation, one of Create, Reconcile, Delete.
	Type LastOperationType
}

// Gardener holds the information about the Gardener.
type Gardener struct {
	// ID is the Docker container id of the Gardener which last acted on a Shoot cluster.
	ID string
	// Name is the hostname (pod name) of the Gardener which last acted on a Shoot cluster.
	Name string
	// Version is the version of the Gardener which last acted on a Shoot cluster.
	Version string
}

const (
	// GardenerName is the value in a Garden resource's `.metadata.finalizers[]` array on which the Gardener will react
	// when performing a delete request on a resource.
	GardenerName = "gardener"
)
