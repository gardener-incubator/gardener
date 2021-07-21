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

package validation

import (
	"fmt"

	"github.com/gardener/gardener/pkg/logger"
	schedulerapi "github.com/gardener/gardener/pkg/scheduler/apis/config"
	"k8s.io/apimachinery/pkg/util/sets"
)

// ValidateConfiguration validates the configuration.
func ValidateConfiguration(config *schedulerapi.SchedulerConfiguration) error {
	if err := validateStrategy(config.Schedulers.Shoot.Strategy); err != nil {
		return fmt.Errorf("invalid seed determination strategy: %w", err)
	}

	if config.LogLevel != "" {
		if !sets.NewString(logger.AllLogLevels...).Has(config.LogLevel) {
			return fmt.Errorf("invalid log level %q, valid levels are %v", config.LogLevel, logger.AllLogLevels)
		}
	}

	return nil
}

func validateStrategy(strategy schedulerapi.CandidateDeterminationStrategy) error {
	for _, s := range schedulerapi.Strategies {
		if s == strategy {
			return nil
		}
	}

	return fmt.Errorf("strategy %q does not exist, valid strategies are %v", strategy, schedulerapi.Strategies)
}
