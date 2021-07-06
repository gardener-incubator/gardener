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

package logger_test

import (
	. "github.com/gardener/gardener/pkg/logger"
	"go.uber.org/zap/zapcore"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("zap", func() {
	Describe("#NewZapLogger", func() {
		It("should return a pointer to a Logger object ('debug' level)", func() {
			logger, err := NewZapLogger(DebugLevel)
			Expect(err).NotTo(HaveOccurred())
			Expect(logger.Core().Enabled(zapcore.DebugLevel)).To(BeTrue())
		})

		It("should return a pointer to a Logger object ('info' level)", func() {
			logger, err := NewZapLogger(InfoLevel)
			Expect(err).NotTo(HaveOccurred())
			Expect(logger.Core().Enabled(zapcore.DebugLevel)).To(BeFalse())
			Expect(logger.Core().Enabled(zapcore.InfoLevel)).To(BeTrue())
		})

		It("should default to 'info' level", func() {
			logger, err := NewZapLogger("")
			Expect(err).NotTo(HaveOccurred())
			Expect(logger.Core().Enabled(zapcore.DebugLevel)).To(BeFalse())
			Expect(logger.Core().Enabled(zapcore.InfoLevel)).To(BeTrue())
		})

		It("should return a pointer to a Logger object ('error' level)", func() {
			logger, err := NewZapLogger(ErrorLevel)
			Expect(err).NotTo(HaveOccurred())
			Expect(logger.Core().Enabled(zapcore.InfoLevel)).To(BeFalse())
			Expect(logger.Core().Enabled(zapcore.ErrorLevel)).To(BeTrue())
		})

		It("should reject invalid log level", func() {
			_, err := NewZapLogger("invalid")
			Expect(err).To(HaveOccurred())
		})
	})
})
