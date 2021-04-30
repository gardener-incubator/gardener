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

package validation

import (
	"fmt"

	"github.com/gardener/gardener/pkg/apis/core"
	"github.com/gardener/gardener/pkg/apis/core/helper"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	apivalidation "k8s.io/apimachinery/pkg/api/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

// ValidateQuota validates a Quota object.
func ValidateQuota(quota *core.Quota) field.ErrorList {
	allErrs := field.ErrorList{}

	allErrs = append(allErrs, apivalidation.ValidateObjectMeta(&quota.ObjectMeta, true, ValidateName, field.NewPath("metadata"))...)
	allErrs = append(allErrs, ValidateQuotaSpec(&quota.Spec, field.NewPath("spec"))...)

	return allErrs
}

// ValidateQuotaUpdate validates a Quota object before an update.
func ValidateQuotaUpdate(newQuota, oldQuota *core.Quota) field.ErrorList {
	allErrs := apivalidation.ValidateObjectMetaUpdate(&newQuota.ObjectMeta, &oldQuota.ObjectMeta, field.NewPath("metadata"))
	allErrs = append(allErrs, apivalidation.ValidateImmutableField(&newQuota.Spec.Scope, &oldQuota.Spec.Scope, field.NewPath("spec").Child("scope"))...)
	allErrs = append(allErrs, ValidateQuota(newQuota)...)
	return allErrs
}

// ValidateQuotaStatusUpdate validates the status field of a Quota object.
func ValidateQuotaStatusUpdate(newQuota, oldQuota *core.Quota) field.ErrorList {
	allErrs := field.ErrorList{}

	return allErrs
}

// ValidateQuotaSpec validates the specification of a Quota object.
func ValidateQuotaSpec(quotaSpec *core.QuotaSpec, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	scopeRef := quotaSpec.Scope
	if _, err := helper.QuotaScope(scopeRef); err != nil {
		allErrs = append(allErrs, field.NotSupported(fldPath.Child("scope"), scopeRef, []string{"project", "secret"}))
	}

	metricsFldPath := fldPath.Child("metrics")
	for k, v := range quotaSpec.Metrics {
		keyPath := metricsFldPath.Key(string(k))
		if !isValidQuotaMetric(corev1.ResourceName(k)) {
			allErrs = append(allErrs, field.Invalid(keyPath, v.String(), fmt.Sprintf("%s is no supported quota metric", string(k))))
		}
		allErrs = append(allErrs, ValidateResourceQuantityValue(string(k), v, keyPath)...)
	}

	return allErrs
}

func isValidQuotaMetric(metric corev1.ResourceName) bool {
	switch metric {
	case
		core.QuotaMetricCPU,
		core.QuotaMetricGPU,
		core.QuotaMetricMemory,
		core.QuotaMetricStorageStandard,
		core.QuotaMetricStoragePremium,
		core.QuotaMetricLoadbalancer:
		return true
	}
	return false
}

// ValidateResourceQuantityValue validates the value of a resource quantity.
func ValidateResourceQuantityValue(key string, value resource.Quantity, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	if value.Cmp(resource.Quantity{}) < 0 {
		allErrs = append(allErrs, field.Invalid(fldPath, value.String(), fmt.Sprintf("%s value must not be negative", key)))
	}

	return allErrs
}
