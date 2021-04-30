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

package helper_test

import (
	apisconfig "github.com/gardener/gardener/pkg/admissioncontroller/apis/config"
	. "github.com/gardener/gardener/pkg/admissioncontroller/apis/config/helper"
	apisconfigv1alpha1 "github.com/gardener/gardener/pkg/admissioncontroller/apis/config/v1alpha1"
	configv1alpha1 "github.com/gardener/gardener/pkg/gardenlet/apis/config/v1alpha1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	gomegatypes "github.com/onsi/gomega/types"
	authenticationv1 "k8s.io/api/authentication/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apiserver/pkg/authentication/serviceaccount"
)

var _ = Describe("Helpers test", func() {
	limit := apisconfig.ResourceLimit{
		APIGroups:   []string{"core.gardener.cloud", "extensions.gardener.cloud"},
		APIVersions: []string{"v1beta1"},
		Resources:   []string{"shoots"},
	}

	limitWildcard := apisconfig.ResourceLimit{
		APIGroups:   []string{"core.gardener.cloud", "*"},
		APIVersions: []string{"*"},
		Resources:   []string{"*"},
	}

	invalidConfig := rbacv1.Subject{
		Kind: "invalid",
	}

	userConfig := rbacv1.Subject{
		Kind: rbacv1.UserKind,
		Name: "user",
	}

	userWildcard := rbacv1.Subject{
		Kind: rbacv1.UserKind,
		Name: "*",
	}

	groupConfig := rbacv1.Subject{
		Kind: rbacv1.GroupKind,
		Name: "system:masters",
	}

	groupWildcard := rbacv1.Subject{
		Kind: rbacv1.GroupKind,
		Name: "*",
	}

	serviceAccountConfig := rbacv1.Subject{
		Kind:      rbacv1.ServiceAccountKind,
		Name:      "foo",
		Namespace: "bar",
	}

	serviceAccountConfigWildcard := rbacv1.Subject{
		Kind:      rbacv1.ServiceAccountKind,
		Name:      "*",
		Namespace: "bar",
	}

	Describe("#ConvertAdmissionControllerConfiguration", func() {
		externalAdmissionConfiguration := apisconfigv1alpha1.AdmissionControllerConfiguration{
			TypeMeta: metav1.TypeMeta{
				APIVersion: configv1alpha1.SchemeGroupVersion.String(),
				Kind:       "AdmissionControllerConfiguration",
			},
		}

		It("should convert the external AdmissionControllerConfiguration to an internal one", func() {
			result, err := ConvertAdmissionControllerConfiguration(&externalAdmissionConfiguration)

			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(&apisconfig.AdmissionControllerConfiguration{}))
		})
	})

	DescribeTable("#APIGroupMatches",
		func(limit apisconfig.ResourceLimit, apiGroup string, matcher gomegatypes.GomegaMatcher) {
			Expect(APIGroupMatches(limit, apiGroup)).To(matcher)
		},
		Entry("no match because request is empty", limit, "", BeFalse()),
		Entry("core.gardener.cloud group is found", limit, "core.gardener.cloud", BeTrue()),
		Entry("extensions.gardener.cloud group is found", limit, "core.gardener.cloud", BeTrue()),
		Entry("settings.gardener.cloud apiGroup is not found", limit, "settings.gardener.cloud", BeFalse()),
		Entry("settings.gardener.cloud apiGroup is found because of wildcard", limitWildcard, "settings.gardener.cloud", BeTrue()),
	)

	DescribeTable("#VersionMatches",
		func(limit apisconfig.ResourceLimit, version string, matcher gomegatypes.GomegaMatcher) {
			Expect(VersionMatches(limit, version)).To(matcher)
		},
		Entry("no match because request is empty", limit, "", BeFalse()),
		Entry("version is found", limit, "v1beta1", BeTrue()),
		Entry("version is not found", limit, "settings.gardener.cloud", BeFalse()),
		Entry("version is found because of wildcard", limitWildcard, "settings.gardener.cloud", BeTrue()),
	)

	DescribeTable("#ResourceMatches",
		func(limit apisconfig.ResourceLimit, resource string, matcher gomegatypes.GomegaMatcher) {
			Expect(ResourceMatches(limit, resource)).To(matcher)
		},
		Entry("no match because request is empty", limit, "", BeFalse()),
		Entry("resource is found", limit, "shoots", BeTrue()),
		Entry("resource is not found", limit, "seeds", BeFalse()),
		Entry("resource is found because of wildcard", limitWildcard, "seeds", BeTrue()),
	)

	DescribeTable("#UserMatches",
		func(subject rbacv1.Subject, userName string, matcher gomegatypes.GomegaMatcher) {
			Expect(UserMatches(subject, authenticationv1.UserInfo{Username: userName})).To(matcher)
		},
		Entry("no match because request is empty", userConfig, "", BeFalse()),
		Entry("no match because of invalid config", invalidConfig, "", BeFalse()),
		Entry("user name is found", userConfig, "user", BeTrue()),
		Entry("user name is not found", userConfig, "user2", BeFalse()),
		Entry("user name is found because of wildcard", userWildcard, "user2", BeTrue()),
	)

	DescribeTable("#UserGroupMatches",
		func(subject rbacv1.Subject, groupName string, matcher gomegatypes.GomegaMatcher) {
			Expect(UserGroupMatches(subject, authenticationv1.UserInfo{Groups: []string{groupName}})).To(matcher)
		},
		Entry("no match because request is empty", groupConfig, "", BeFalse()),
		Entry("no match because of invalid config", invalidConfig, "", BeFalse()),
		Entry("group name is found", groupConfig, "system:masters", BeTrue()),
		Entry("group name is not found", groupConfig, "users", BeFalse()),
		Entry("group name is found because of wildcard", groupWildcard, "users", BeTrue()),
	)

	DescribeTable("#ServiceAccountMatches",
		func(subject rbacv1.Subject, namespace, name string, matcher gomegatypes.GomegaMatcher) {
			Expect(ServiceAccountMatches(subject, authenticationv1.UserInfo{
				Username: serviceaccount.MakeUsername(namespace, name),
			})).To(matcher)
		},
		Entry("no match because request is empty", serviceAccountConfig, "", "", BeFalse()),
		Entry("no match because of invalid config", invalidConfig, "", "", BeFalse()),
		Entry("service account name is found", serviceAccountConfig, "bar", "foo", BeTrue()),
		Entry("service account name is not found", serviceAccountConfig, "bar", "bar", BeFalse()),
		Entry("service account name is found because of wildcard", serviceAccountConfigWildcard, "bar", "users", BeTrue()),
		Entry("service account name is found because of different namespace", serviceAccountConfigWildcard, "foo", "foo", BeFalse()),
	)
})
