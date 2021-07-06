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

package app

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"

	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	"github.com/gardener/gardener/pkg/client/kubernetes"
	"github.com/gardener/gardener/pkg/features"
	"github.com/gardener/gardener/pkg/scheduler/apis/config"
	configloader "github.com/gardener/gardener/pkg/scheduler/apis/config/loader"
	"github.com/gardener/gardener/pkg/scheduler/apis/config/validation"
	shootcontroller "github.com/gardener/gardener/pkg/scheduler/controller/shoot"
	schedulerfeatures "github.com/gardener/gardener/pkg/scheduler/features"
	"github.com/go-logr/zapr"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	ctrlruntimelog "sigs.k8s.io/controller-runtime/pkg/log"
	ctrlruntimelzap "sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/component-base/version/verflag"
)

// Options has all the context and parameters needed to run a GardenerScheduler.
type Options struct {
	// ConfigFile is the location of the GardenerScheduler's configuration file.
	ConfigFile string
	config     *config.SchedulerConfiguration
}

// AddFlags adds flags for a specific Scheduler to the specified FlagSet.
func (o *Options) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&o.ConfigFile, "config", o.ConfigFile, "The path to the configuration file.")
}

// Validate validates all the required options.
func (o *Options) validate(args []string) error {
	if len(o.ConfigFile) == 0 {
		return fmt.Errorf("missing GardenerScheduler config file")
	}
	if len(args) != 0 {
		return errors.New("arguments are not supported")
	}

	return nil
}

// NewCommandStartGardenerScheduler creates a *cobra.Command object with default parameters
func NewCommandStartGardenerScheduler() *cobra.Command {
	opts := &Options{
		config: new(config.SchedulerConfiguration),
	}

	cmd := &cobra.Command{
		Use:   "gardener-scheduler",
		Short: "Launch the Gardener scheduler",
		Long:  `The Gardener scheduler is a controller that tries to find the best matching seed cluster for a shoot. The scheduler takes the cloud provider and the distance between the seed (hosting the control plane) and the shoot cluster region into account.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			verflag.PrintAndExitIfRequested()

			if err := opts.validate(args); err != nil {
				return err
			}
			return runCommand(cmd, opts)
		},
	}

	flags := cmd.Flags()
	verflag.AddFlags(flags)
	opts.AddFlags(flags)

	return cmd
}

func runCommand(cmd *cobra.Command, opts *Options) error {
	// Load config file
	config, err := configloader.LoadFromFile(opts.ConfigFile)
	if err != nil {
		return err
	}

	// Validate the configuration
	if err := validation.ValidateConfiguration(config); err != nil {
		return err
	}

	// Add feature flags
	if err := schedulerfeatures.FeatureGate.SetFromMap(config.FeatureGates); err != nil {
		return err
	}
	kubernetes.UseCachedRuntimeClients = schedulerfeatures.FeatureGate.Enabled(features.CachedRuntimeClients)

	// Initialize logger
	rawLog := newLogger(config.LogLevel)
	logrLogger := zapr.NewLogger(rawLog.WithOptions(zap.AddCallerSkip(1)))
	sugarLogger := rawLog.Sugar()
	defer func() {
		if err := sugarLogger.Sync(); err != nil {
			fmt.Println(err)
		}
	}()

	sugarLogger.Info("Starting Gardener scheduler ...")
	sugarLogger.Infof("Feature Gates: %s", schedulerfeatures.FeatureGate.String())

	// set the logger used by sigs.k8s.io/controller-runtime
	ctrlruntimelog.SetLogger(logrLogger)

	// Prepare a Kubernetes client object for the Garden cluster which contains all the Clientsets
	// that can be used to access the Kubernetes API.
	if kubeconfig := os.Getenv("KUBECONFIG"); kubeconfig != "" {
		config.ClientConnection.Kubeconfig = kubeconfig
	}

	// Prepare REST config
	restCfg, err := kubernetes.RESTConfigFromClientConnectionConfiguration(&config.ClientConnection, nil)
	if err != nil {
		return err
	}

	// Setup controller-runtime manager
	mgr, err := manager.New(restCfg, manager.Options{
		// MetricsBindAddress: opt.internalAddr,
		HealthProbeBindAddress:     net.JoinHostPort(config.Server.HTTP.BindAddress, strconv.Itoa(config.Server.HTTP.Port)),
		LeaderElection:             config.LeaderElection.LeaderElect,
		LeaderElectionID:           "gardener-scheduler-leader-election",
		LeaderElectionNamespace:    config.LeaderElection.ResourceNamespace,
		LeaderElectionResourceLock: config.LeaderElection.ResourceLock,
		Logger:                     logrLogger,
	})
	if err != nil {
		return fmt.Errorf("failed to create controller manager: %w", err)
	}

	// Add APIs
	if err := gardencorev1beta1.AddToScheme(mgr.GetScheme()); err != nil {
		return fmt.Errorf("failed to register scheme gardencorev1beta1: %w", err)
	}

	// Add controllers
	if err := shootcontroller.AddToManager(cmd.Context(), mgr, config.Schedulers.Shoot); err != nil {
		return fmt.Errorf("failed to create shoot scheduler controller: %w", err)
	}

	// Start manager and all runnables (the command context is tied to OS signals already)
	if err := mgr.Start(cmd.Context()); err != nil {
		return fmt.Errorf("failed to start manager: %w", err)
	}

	return nil
}

func newLogger(level string) *zap.Logger {
	var lvl zapcore.Level
	switch level {
	case "debug":
		lvl = zap.DebugLevel
	case "error":
		lvl = zap.ErrorLevel
	default:
		lvl = zap.InfoLevel
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

	coreLog := zapcore.NewCore(&ctrlruntimelzap.KubeAwareEncoder{Encoder: zapcore.NewConsoleEncoder(encCfg)}, sink, zap.NewAtomicLevelAt(lvl))
	return zap.New(coreLog, opts...)
}
