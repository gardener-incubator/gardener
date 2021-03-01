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

package v1alpha1

import (
	"k8s.io/utils/pointer"
)

// SetDefaults_ManagedSeedSet sets default values for ManagedSeed objects.
func SetDefaults_ManagedSeedSet(obj *ManagedSeedSet) {
	// Set default replicas
	if obj.Spec.Replicas == nil {
		obj.Spec.Replicas = pointer.Int32Ptr(1)
	}

	// Set update strategy defaults
	if obj.Spec.UpdateStrategy == nil {
		obj.Spec.UpdateStrategy = &ManagedSeedSetUpdateStrategy{}
	}

	// Set default revision history limit
	if obj.Spec.RevisionHistoryLimit == nil {
		obj.Spec.RevisionHistoryLimit = pointer.Int32Ptr(10)
	}
}

// SetDefaults_ManagedSeedSetUpdateStrategy sets default values for ManagedSeedSetUpdateStrategy objects.
func SetDefaults_ManagedSeedSetUpdateStrategy(obj *ManagedSeedSetUpdateStrategy) {
	// Set default type
	if obj.Type == nil {
		t := RollingUpdateManagedSeedSetUpdateStrategyType
		obj.Type = &t
	}

	if *obj.Type == RollingUpdateManagedSeedSetUpdateStrategyType {
		// Set rolling update defaults
		if obj.RollingUpdate == nil {
			obj.RollingUpdate = &RollingUpdateManagedSeedSetUpdateStrategy{}
		}
	}
}

// SetDefaults_RollingUpdateManagedSeedSetUpdateStrategye sets default values for RollingUpdateManagedSeedSetUpdateStrategy objects.
func SetDefaults_RollingUpdateManagedSeedSetUpdateStrategy(obj *RollingUpdateManagedSeedSetUpdateStrategy) {
	// Set default partition
	if obj.Partition == nil {
		obj.Partition = pointer.Int32Ptr(0)
	}
}
