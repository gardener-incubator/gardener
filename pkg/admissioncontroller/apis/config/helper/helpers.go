// Copyright (c) 2020 SAP SE or an SAP affiliate company. All rights reserved. This file is licensed under the Apache Software License, v. 2 except as noted otherwise in the LICENSE file
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
	"strings"

	apisconfig "github.com/gardener/gardener/pkg/admissioncontroller/apis/config"
	apisconfigv1alpha1 "github.com/gardener/gardener/pkg/admissioncontroller/apis/config/v1alpha1"
	authenticationv1 "k8s.io/api/authentication/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apiserver/pkg/authentication/serviceaccount"
)

var scheme *runtime.Scheme

func init() {
	scheme = runtime.NewScheme()
	utilruntime.Must(apisconfigv1alpha1.AddToScheme(scheme))
	utilruntime.Must(apisconfig.AddToScheme(scheme))
}

// ConvertAdmissionControllerConfiguration converts the given object to an internal AdmissionControllerConfiguration version.
func ConvertAdmissionControllerConfiguration(obj runtime.Object) (*apisconfig.AdmissionControllerConfiguration, error) {
	obj, err := scheme.ConvertToVersion(obj, apisconfig.SchemeGroupVersion)
	if err != nil {
		return nil, err
	}
	result, ok := obj.(*apisconfig.AdmissionControllerConfiguration)
	if !ok {
		return nil, fmt.Errorf("could not convert AdmissionControllerConfiguration to the internal version")
	}
	return result, nil
}

// APIGroupMatches returns `true` if the given group has a match in the given limit.
func APIGroupMatches(limit apisconfig.ResourceLimit, group string) bool {
	for _, grp := range limit.APIGroups {
		if grp == apisconfig.WildcardAll || grp == group {
			return true
		}
	}
	return false
}

// ResourceMatches returns `true` if the given resource has a match in the given limit.
func ResourceMatches(limit apisconfig.ResourceLimit, resource string) bool {
	for _, res := range limit.Resources {
		if res == apisconfig.WildcardAll || res == resource {
			return true
		}
	}
	return false
}

// VersionMatches returns `true` if the given version has a match in the given limit.
func VersionMatches(limit apisconfig.ResourceLimit, version string) bool {
	for _, ver := range limit.APIVersions {
		if ver == apisconfig.WildcardAll || ver == version {
			return true
		}
	}
	return false
}

// UserMatches returns `true` if the given user in the subject has a match in the given userConfig.
func UserMatches(subject rbacv1.Subject, userInfo authenticationv1.UserInfo) bool {
	if subject.Kind != rbacv1.UserKind {
		return false
	}

	return subject.Name == apisconfig.WildcardAll || subject.Name == userInfo.Username
}

// UserGroupMatches returns `true` if the given group in the subject has a match in the given userConfig.
// Always returns true if `apisconfig.WildcardAll` is used in subject.
func UserGroupMatches(subject rbacv1.Subject, userInfo authenticationv1.UserInfo) bool {
	if subject.Kind != rbacv1.GroupKind {
		return false
	}

	if subject.Name == apisconfig.WildcardAll {
		return true
	}

	for _, group := range userInfo.Groups {
		if group == subject.Name {
			return true
		}
	}
	return false
}

// ServiceAccountMatches returns `true` if the given service account in the subject has a match in the given userConfig.
// Supports `apisconfig.WildcardAll` in subject name.
func ServiceAccountMatches(subject rbacv1.Subject, userInfo authenticationv1.UserInfo) bool {
	if subject.Kind != rbacv1.ServiceAccountKind {
		return false
	}

	if subject.Name == apisconfig.WildcardAll {
		saPrefix := fmt.Sprintf("%s%s:", serviceaccount.ServiceAccountUsernamePrefix, subject.Namespace)
		return strings.HasPrefix(userInfo.Username, saPrefix)
	}

	return serviceaccount.MatchesUsername(subject.Namespace, subject.Name, userInfo.Username)
}
