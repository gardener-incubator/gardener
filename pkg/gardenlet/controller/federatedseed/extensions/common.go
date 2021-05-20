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

package extensions

import (
	"context"

	apiextensions "github.com/gardener/gardener/pkg/api/extensions"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GetExtensionObject is a helper to get the extension object for a key and objectCreator function
func GetExtensionObject(ctx context.Context, seedClient client.Client, key types.NamespacedName, objectCreator func() client.Object) (extensionsv1alpha1.Object, error) {
	obj := objectCreator()
	if err := seedClient.Get(ctx, key, obj); err != nil {
		return nil, err
	}
	return apiextensions.Accessor(obj)
}
