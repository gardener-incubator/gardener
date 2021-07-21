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

package logger_test

import (
	"fmt"
	"os"

	. "github.com/gardener/gardener/pkg/logger"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/sirupsen/logrus"
)

var _ = Describe("logrus", func() {
	AfterEach(func() {
		Logger = nil
	})

	Describe("#NewLogger", func() {
		It("should return a pointer to a Logger object ('info' level)", func() {
			logger := NewLogger(InfoLevel)

			Expect(logger.Out).To(Equal(os.Stderr))
			Expect(logger.Level).To(Equal(logrus.InfoLevel))
			Expect(Logger).To(Equal(logger))
		})

		It("should return a pointer to a Logger object ('debug' level)", func() {
			logger := NewLogger(DebugLevel)

			Expect(logger.Out).To(Equal(os.Stderr))
			Expect(logger.Level).To(Equal(logrus.DebugLevel))
			Expect(Logger).To(Equal(logger))
		})

		It("should return a pointer to a Logger object ('error' level)", func() {
			logger := NewLogger(ErrorLevel)

			Expect(logger.Out).To(Equal(os.Stderr))
			Expect(logger.Level).To(Equal(logrus.ErrorLevel))
			Expect(Logger).To(Equal(logger))
		})
	})

	Describe("#NewShootLogger", func() {
		It("should return an Entry object with additional fields (w/o operationID)", func() {
			logger := NewLogger(InfoLevel)
			namespace := "core"
			name := "shoot01"

			shootLogger := NewShootLogger(logger, name, namespace)

			Expect(shootLogger.Data).To(HaveKeyWithValue("shoot", fmt.Sprintf("%s/%s", namespace, name)))
		})
	})

	Describe("#NewFieldLogger", func() {
		It("should return an Entry object with additional fields", func() {
			logger := NewLogger(InfoLevel)
			key := "foo"
			value := "bar"

			fieldLogger := NewFieldLogger(logger, key, value)

			Expect(fieldLogger.Data).To(HaveKeyWithValue(key, value))
		})
	})

	Describe("#NewIDLogger", func() {
		It("should return an Entry object an ID field", func() {
			logger := NewLogger(InfoLevel)

			fieldLogger := NewIDLogger(logger)

			entry, _ := fieldLogger.(*logrus.Entry)
			Expect(entry.Data).To(HaveKeyWithValue(IDFieldName, Not(BeEmpty())))
		})
	})

	Describe("#AddWriter", func() {
		It("should return a pointer to a Test Logger object ('info' level)", func() {
			logger := AddWriter(NewLogger(""), GinkgoWriter)
			Expect(logger.Out).To(Equal(GinkgoWriter))
			Expect(logger.Level).To(Equal(logrus.InfoLevel))
			Expect(Logger).To(Equal(logger))
		})
	})
})
