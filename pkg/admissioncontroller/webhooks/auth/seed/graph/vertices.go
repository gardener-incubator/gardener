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

package graph

// VertexType is a type for specific vertices.
type VertexType byte

const (
	// VertexTypeBackupBucket is a constant for a 'BackupBucket' vertex.
	VertexTypeBackupBucket VertexType = iota
	// VertexTypeBackupEntry is a constant for a 'BackupEntry' vertex.
	VertexTypeBackupEntry
	// VertexTypeBastion is a constant for a 'Bastion' vertex.
	VertexTypeBastion
	// VertexTypeCertificateSigningRequest is a constant for a 'CertificateSigningRequest' vertex.
	VertexTypeCertificateSigningRequest
	// VertexTypeCloudProfile is a constant for a 'CloudProfile' vertex.
	VertexTypeCloudProfile
	// VertexTypeConfigMap is a constant for a 'ConfigMap' vertex.
	VertexTypeConfigMap
	// VertexTypeControllerDeployment is a constant for a 'ControllerDeployment' vertex.
	VertexTypeControllerDeployment
	// VertexTypeControllerInstallation is a constant for a 'ControllerInstallation' vertex.
	VertexTypeControllerInstallation
	// VertexTypeControllerRegistration is a constant for a 'ControllerRegistration' vertex.
	VertexTypeControllerRegistration
	// VertexTypeLease is a constant for a 'Lease' vertex.
	VertexTypeLease
	// VertexTypeManagedSeed is a constant for a 'ManagedSeed' vertex.
	VertexTypeManagedSeed
	// VertexTypeNamespace is a constant for a 'Namespace' vertex.
	VertexTypeNamespace
	// VertexTypeProject is a constant for a 'Project' vertex.
	VertexTypeProject
	// VertexTypeSecret is a constant for a 'Secret' vertex.
	VertexTypeSecret
	// VertexTypeSecretBinding is a constant for a 'SecretBinding' vertex.
	VertexTypeSecretBinding
	// VertexTypeSeed is a constant for a 'Seed' vertex.
	VertexTypeSeed
	// VertexTypeShoot is a constant for a 'Shoot' vertex.
	VertexTypeShoot
	// VertexTypeShootState is a constant for a 'ShootState' vertex.
	VertexTypeShootState
	// VertexTypeShootExtensionStatus is a constant for a 'ShootExtensionStatus' vertex.
	VertexTypeShootExtensionStatus
)

var vertexTypes = map[VertexType]string{
	VertexTypeBackupBucket:              "BackupBucket",
	VertexTypeBackupEntry:               "BackupEntry",
	VertexTypeBastion:                   "Bastion",
	VertexTypeCertificateSigningRequest: "CertificateSigningRequest",
	VertexTypeCloudProfile:              "CloudProfile",
	VertexTypeConfigMap:                 "ConfigMap",
	VertexTypeControllerInstallation:    "ControllerInstallation",
	VertexTypeControllerRegistration:    "ControllerRegistration",
	VertexTypeLease:                     "Lease",
	VertexTypeManagedSeed:               "ManagedSeed",
	VertexTypeNamespace:                 "Namespace",
	VertexTypeProject:                   "Project",
	VertexTypeSecret:                    "Secret",
	VertexTypeSecretBinding:             "SecretBinding",
	VertexTypeSeed:                      "Seed",
	VertexTypeShoot:                     "Shoot",
	VertexTypeShootState:                "ShootState",
	VertexTypeShootExtensionStatus:      "ShootExtensionStatus",
}

type vertex struct {
	vertexType VertexType
	namespace  string
	name       string
	id         int64
}

func newVertex(vertexType VertexType, namespace, name string, id int64) *vertex {
	return &vertex{
		vertexType: vertexType,
		name:       name,
		namespace:  namespace,
		id:         id,
	}
}

func (v *vertex) ID() int64 {
	return v.id
}

func (v *vertex) String() string {
	var namespace string
	if len(v.namespace) > 0 {
		namespace = v.namespace + "/"
	}
	return vertexTypes[v.vertexType] + ":" + namespace + v.name
}

// typeVertexMapping is a map from type -> namespace -> name -> vertex.
type typeVertexMapping map[VertexType]namespaceVertexMapping

// namespaceVertexMapping is a map of namespace -> name -> vertex.
type namespaceVertexMapping map[string]nameVertexMapping

// nameVertexMapping is a map of name -> vertex.
type nameVertexMapping map[string]*vertex
