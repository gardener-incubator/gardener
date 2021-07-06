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

package logger

import (
	"fmt"
	"os"

	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	ctrlruntimelzap "sigs.k8s.io/controller-runtime/pkg/log/zap"
)

// NewZapLogger creates a new logger backed by Zap.
func NewZapLogger(logLevel string) (*zap.Logger, error) {
	var lvl zapcore.Level
	switch logLevel {
	case DebugLevel:
		lvl = zap.DebugLevel
	case ErrorLevel:
		lvl = zap.ErrorLevel
	case "", InfoLevel:
		lvl = zap.InfoLevel
	default:
		return nil, fmt.Errorf("invalid log level %q", logLevel)
	}

	encCfg := zap.NewProductionEncoderConfig()
	encCfg.TimeKey = "time"
	encCfg.EncodeTime = zapcore.ISO8601TimeEncoder
	encCfg.EncodeDuration = zapcore.StringDurationEncoder

	sink := zapcore.AddSync(os.Stderr)
	opts := []zap.Option{
		zap.AddCaller(),
		zap.ErrorOutput(sink),
	}

	encoder := &ctrlruntimelzap.KubeAwareEncoder{Encoder: zapcore.NewConsoleEncoder(encCfg)}
	coreLog := zapcore.NewCore(encoder, sink, zap.NewAtomicLevelAt(lvl))

	return zap.New(coreLog, opts...), nil
}

// NewZapLogr wraps a Zap logger into a standard logr-compatible logger.
func NewZapLogr(logger *zap.Logger) logr.Logger {
	return zapr.NewLogger(logger.WithOptions(zap.AddCallerSkip(1)))
}
