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

package seedmanagement

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	gardencore "github.com/gardener/gardener/pkg/apis/core"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ManagedSeedSet represents a set of identical ManagedSeeds.
type ManagedSeedSet struct {
	metav1.TypeMeta
	// Standard object metadata.
	metav1.ObjectMeta
	// Spec defines the desired identities of ManagedSeeds and Shoots in this set.
	Spec ManagedSeedSetSpec
	// Status is the current status of ManagedSeeds and Shoots in this ManagedSeedSet.
	Status ManagedSeedSetStatus
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ManagedSeedSetList is a list of ManagedSeed objects.
type ManagedSeedSetList struct {
	metav1.TypeMeta
	// Standard list object metadata.
	metav1.ListMeta
	// Items is the list of ManagedSeeds.
	Items []ManagedSeedSet
}

// ManagedSeedSetSpec is the specification of a ManagedSeedSet.
type ManagedSeedSetSpec struct {
	// Replicas is the desired number of replicas of the given Template. Defaults to 1.
	Replicas *int32
	// Selector is a label query over ManagedSeeds and Shoots that should match the replica count.
	// It must match the ManagedSeeds and Shoots template's labels.
	Selector *metav1.LabelSelector
	// Template describes the ManagedSeed that will be created if insufficient replicas are detected.
	// Each ManagedSeed created / updated by the ManagedSeedSet will fulfill this template.
	Template ManagedSeedTemplate
	// ShootTemplate describes the Shoot that will be created if insufficient replicas are detected for hosting the corresponding ManagedSeed.
	// Each Shoot created / updated by the ManagedSeedSet will fulfill this template.
	ShootTemplate gardencore.ShootTemplate
	// UpdateStrategy specifies the ManagedSeedSetUpdateStrategy that will be
	// employed to update ManagedSeeds / Shoots in the ManagedSeedSet when a revision is made to
	// Template / ShootTemplate.
	UpdateStrategy *ManagedSeedSetUpdateStrategy
	// RevisionHistoryLimit is the maximum number of revisions that will
	// be maintained in the ManagedSeedSet's revision history. Defaults to 10.
	RevisionHistoryLimit *int32
}

// ManagedSeedSetUpdateStrategy specifies the strategy that the ManagedSeedSet
// controller will use to perform updates. It includes any additional parameters
// necessary to perform the update for the indicated strategy.
type ManagedSeedSetUpdateStrategy struct {
	// Type indicates the type of the ManagedSeedSetUpdateStrategy. Defaults to ManagedSeedSetUpdateStrategyType.
	Type *ManagedSeedSetUpdateStrategyType
	// RollingUpdate is used to communicate parameters when Type is ManagedSeedSetUpdateStrategyType.
	RollingUpdate *RollingUpdateManagedSeedSetUpdateStrategy
}

// ManagedSeedSetUpdateStrategyType is a string enumeration type that enumerates
// all possible update strategies for the ManagedSeedSet controller.
type ManagedSeedSetUpdateStrategyType string

const (
	// RollingUpdateManagedSeedSetUpdateStrategyType indicates that update will be
	// applied to all ManagedSeeds / Shoots in the ManagedSeedSet with respect to the ManagedSeedSet
	// ordering constraints.
	RollingUpdateManagedSeedSetUpdateStrategyType ManagedSeedSetUpdateStrategyType = "RollingUpdate"
)

// RollingUpdateManagedSeedSetStrategy is used to communicate parameter for RollingUpdateManagedSeedSetUpdateStrategyType.
type RollingUpdateManagedSeedSetUpdateStrategy struct {
	// Partition indicates the ordinal at which the ManagedSeedSet should be partitioned. Defaults to 0.
	Partition *int32
}

// ManagedSeedSetStatus represents the current state of a ManagedSeedSet.
type ManagedSeedSetStatus struct {
	// ObservedGeneration is the most recent generation observed for this ManagedSeedSet. It corresponds to the
	// ManagedSeedSet's generation, which is updated on mutation by the API Server.
	ObservedGeneration int64
	// Replicas is the number of replicas (ManagedSeeds and their corresponding Shoots) created by the ManagedSeedSet controller.
	Replicas int32
	// ReadyReplicas is the number of ManagedSeeds created by the ManagedSeedSet controller that have a Ready Condition.
	ReadyReplicas int32
	// NextReplicaNumber is the ordinal number that will be assigned to the next replica of the ManagedSeedSet.
	NextReplicaNumber int32
	// CurrentReplicas is the number of ManagedSeeds created by the ManagedSeedSet controller from the ManagedSeedSet version
	// indicated by CurrentRevision.
	CurrentReplicas int32
	// UpdatedReplicas is the number of ManagedSeeds created by the ManagedSeedSet controller from the ManagedSeedSet version
	// indicated by UpdateRevision.
	UpdatedReplicas int32
	// CurrentRevision, if not empty, indicates the version of the ManagedSeedSet used to generate ManagedSeeds with smaller
	// ordinal numbers during updates.
	CurrentRevision string
	// UpdateRevision, if not empty, indicates the version of the ManagedSeedSet used to generate ManagedSeeds with larger
	// ordinal numbers during updates
	UpdateRevision string
	// CollisionCount is the count of hash collisions for the ManagedSeedSet. The ManagedSeedSet controller
	// uses this field as a collision avoidance mechanism when it needs to create the name for the
	// newest ControllerRevision.
	CollisionCount *int32
	// Conditions represents the latest available observations of a ManagedSeedSet's current state.
	Conditions []gardencore.Condition
}

// TODO Condition constants
