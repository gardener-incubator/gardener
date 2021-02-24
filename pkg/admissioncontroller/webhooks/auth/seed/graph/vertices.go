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
	VertexTypeBackupEntry VertexType = iota
	// VertexTypeCloudProfile is a constant for a 'CloudProfile' vertex.
	VertexTypeCloudProfile VertexType = iota
	// VertexTypeConfigMap is a constant for a 'ConfigMap' vertex.
	VertexTypeConfigMap VertexType = iota
	// VertexTypeControllerInstallation is a constant for a 'ControllerInstallation' vertex.
	VertexTypeControllerInstallation VertexType = iota
	// VertexTypeControllerRegistration is a constant for a 'ControllerRegistration' vertex.
	VertexTypeControllerRegistration VertexType = iota
	// VertexTypeManagedSeed is a constant for a 'ManagedSeed' vertex.
	VertexTypeManagedSeed VertexType = iota
	// VertexTypeNamespace is a constant for a 'Namespace' vertex.
	VertexTypeNamespace VertexType = iota
	// VertexTypeProject is a constant for a 'Project' vertex.
	VertexTypeProject VertexType = iota
	// VertexTypeSecret is a constant for a 'Secret' vertex.
	VertexTypeSecret VertexType = iota
	// VertexTypeSecretBinding is a constant for a 'SecretBinding' vertex.
	VertexTypeSecretBinding VertexType = iota
	// VertexTypeSeed is a constant for a 'Seed' vertex.
	VertexTypeSeed VertexType = iota
	// VertexTypeShoot is a constant for a 'Shoot' vertex.
	VertexTypeShoot VertexType = iota
	// VertexTypeShootState is a constant for a 'ShootState' vertex.
	VertexTypeShootState VertexType = iota
)

var vertexTypes = map[VertexType]string{
	VertexTypeBackupBucket:           "BackupBucket",
	VertexTypeBackupEntry:            "BackupEntry",
	VertexTypeCloudProfile:           "CloudProfile",
	VertexTypeConfigMap:              "ConfigMap",
	VertexTypeControllerInstallation: "ControllerInstallation",
	VertexTypeControllerRegistration: "ControllerRegistration",
	VertexTypeManagedSeed:            "ManagedSeed",
	VertexTypeNamespace:              "Namespace",
	VertexTypeProject:                "Project",
	VertexTypeSecret:                 "Secret",
	VertexTypeSecretBinding:          "SecretBinding",
	VertexTypeSeed:                   "Seed",
	VertexTypeShoot:                  "Shoot",
	VertexTypeShootState:             "ShootState",
}

type vertex struct {
	vertexType VertexType
	namespace  string
	name       string
	id         int64
}

func newVertex(VertexType VertexType, namespace, name string, id int64) *vertex {
	return &vertex{
		vertexType: VertexType,
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