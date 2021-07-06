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
	"io"
	"os"

	"github.com/gardener/gardener/pkg/utils"

	"github.com/sirupsen/logrus"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
)

// Logger is the standard logger for the Gardener which is used for all messages which are not Shoot
// cluster specific.
var Logger *logrus.Logger

// NewLogger creates a new logrus logger.
// It uses STDERR as output channel and evaluates the value of the --log-level command line argument in order
// to set the log level.
// Example output: time="2017-06-08T13:00:28+02:00" level=info msg="gardener started successfully".
func NewLogger(logLevel string) *logrus.Logger {
	var level logrus.Level

	switch logLevel {
	case DebugLevel:
		level = logrus.DebugLevel
	case "", InfoLevel:
		level = logrus.InfoLevel
	case ErrorLevel:
		level = logrus.ErrorLevel
	default:
		panic("The specified log level is not supported.")
	}

	logger := &logrus.Logger{
		Out:   os.Stderr,
		Level: level,
		Formatter: &logrus.TextFormatter{
			DisableColors: true,
		},
	}
	Logger = logger
	return logger
}

// NewNopLogger instantiates a new logger that logs to ioutil.Discard.
func NewNopLogger() *logrus.Logger {
	logger := logrus.New()
	logger.Out = io.Discard
	return logger
}

// AddWriter returns a logger that uses the tests writer (e.g., GingkoWriter) as output channel
func AddWriter(logger *logrus.Logger, writer io.Writer) *logrus.Logger {
	logger.Out = writer
	return logger
}

// NewShootLogger extends an existing logrus logger and adds an additional field containing the Shoot cluster name
// and the project in the Garden cluster to the output. If an <operationID> is provided it will be printed for every
// log message.
// Example output: time="2017-06-08T13:00:49+02:00" level=info msg="Creating namespace in seed cluster" shoot=core/crazy-botany.
func NewShootLogger(logger *logrus.Logger, shoot, project string) *logrus.Entry {
	return logger.WithField("shoot", fmt.Sprintf("%s/%s", project, shoot))
}

// NewFieldLogger extends an existing logrus logger and adds the provided additional field.
// Example output: time="2017-06-08T13:00:49+02:00" level=info msg="something" <fieldKey>=<fieldValue>.
func NewFieldLogger(logger logrus.FieldLogger, fieldKey, fieldValue string) *logrus.Entry {
	return logger.WithField(fieldKey, fieldValue)
}

// IDFieldName is the name of the id field for a logger.
const IDFieldName = "process_id"

// NewIDLogger extends an existing logrus logger with a randomly generated id field.
// Example output: time="2017-06-08T13:00:49+02:00" level=info msg="something" id=123abcde.
func NewIDLogger(logger logrus.FieldLogger) logrus.FieldLogger {
	id, err := utils.GenerateRandomString(8)
	utilruntime.Must(err)
	return NewFieldLogger(logger, IDFieldName, id)
}
