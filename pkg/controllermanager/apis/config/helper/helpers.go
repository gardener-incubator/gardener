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

package helper

import (
	"fmt"

	"github.com/gardener/gardener/pkg/controllermanager/apis/config"
	"github.com/gardener/gardener/pkg/controllermanager/apis/config/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
)

var scheme *runtime.Scheme

func init() {
	scheme = runtime.NewScheme()
	utilruntime.Must(config.AddToScheme(scheme))
	utilruntime.Must(v1alpha1.AddToScheme(scheme))
}

// ConvertControllerManagerConfiguration converts the given object to an internal ControllerManagerConfiguration version.
func ConvertControllerManagerConfiguration(obj runtime.Object) (*config.ControllerManagerConfiguration, error) {
	obj, err := scheme.ConvertToVersion(obj, config.SchemeGroupVersion)
	if err != nil {
		return nil, err
	}
	result, ok := obj.(*config.ControllerManagerConfiguration)
	if !ok {
		return nil, fmt.Errorf("could not convert ControllerManagerConfiguration to the internal version")
	}
	return result, nil
}
