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

package botanist

import (
	"github.com/gardener/gardener/pkg/operation/botanist/component"
	"github.com/gardener/gardener/pkg/utils"
)

// DefaultKubeAPIServerService returns a deployer for kube-apiserver service.
func (b *Botanist) DefaultKubeAPIServerService(sniPhase component.Phase) component.DeployWaiter {
	return b.kubeAPIServiceService(sniPhase)
}

func (b *Botanist) getKubeAPIServerServiceAnnotations(sniPhase component.Phase) map[string]string {
	if b.ExposureClassHandler != nil && sniPhase != component.PhaseEnabled {
		return utils.MergeStringMaps(b.Seed.LoadBalancerServiceAnnotations, b.ExposureClassHandler.LoadBalancerService.Annotations)
	}
	return b.Seed.LoadBalancerServiceAnnotations
}
