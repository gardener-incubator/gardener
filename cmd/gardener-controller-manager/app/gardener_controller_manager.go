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

package app

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"

	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	"github.com/gardener/gardener/pkg/client/kubernetes"
	clientmapbuilder "github.com/gardener/gardener/pkg/client/kubernetes/clientmap/builder"
	"github.com/gardener/gardener/pkg/controllermanager/apis/config"
	controllermanagerconfigv1alpha1 "github.com/gardener/gardener/pkg/controllermanager/apis/config/v1alpha1"
	configvalidation "github.com/gardener/gardener/pkg/controllermanager/apis/config/validation"
	"github.com/gardener/gardener/pkg/controllermanager/controller"
	controllermanagerfeatures "github.com/gardener/gardener/pkg/controllermanager/features"
	"github.com/gardener/gardener/pkg/logger"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/component-base/version/verflag"
	ctrlruntimelog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// Options has all the context and parameters needed to run a Gardener controller manager.
type Options struct {
	// ConfigFile is the location of the Gardener controller manager's configuration file.
	ConfigFile string
	config     *config.ControllerManagerConfiguration
	scheme     *runtime.Scheme
	codecs     serializer.CodecFactory
}

// AddFlags adds flags for a specific Gardener controller manager to the specified FlagSet.
func (o *Options) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&o.ConfigFile, "config", o.ConfigFile, "The path to the configuration file.")
}

// NewOptions returns a new Options object.
func NewOptions() (*Options, error) {
	o := &Options{
		config: new(config.ControllerManagerConfiguration),
	}

	o.scheme = runtime.NewScheme()
	o.codecs = serializer.NewCodecFactory(o.scheme)

	if err := config.AddToScheme(o.scheme); err != nil {
		return nil, err
	}
	if err := controllermanagerconfigv1alpha1.AddToScheme(o.scheme); err != nil {
		return nil, err
	}

	return o, nil
}

// loadConfigFromFile loads the contents of file and decodes it as a
// ControllerManagerConfiguration object.
func (o *Options) loadConfigFromFile(file string) (*config.ControllerManagerConfiguration, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	return o.decodeConfig(data)
}

// decodeConfig decodes data as a ControllerManagerConfiguration object.
func (o *Options) decodeConfig(data []byte) (*config.ControllerManagerConfiguration, error) {
	configObj, gvk, err := o.codecs.UniversalDecoder().Decode(data, nil, nil)
	if err != nil {
		return nil, err
	}
	config, ok := configObj.(*config.ControllerManagerConfiguration)
	if !ok {
		return nil, fmt.Errorf("got unexpected config type: %v", gvk)
	}
	return config, nil
}

func (o *Options) configFileSpecified() error {
	if len(o.ConfigFile) == 0 {
		return fmt.Errorf("missing Gardener controller manager config file")
	}
	return nil
}

// Validate validates all the required options.
func (o *Options) validate(args []string) error {
	if len(args) != 0 {
		return errors.New("arguments are not supported")
	}

	return nil
}

// NewCommandStartGardenerControllerManager creates a *cobra.Command object with default parameters
func NewCommandStartGardenerControllerManager() *cobra.Command {
	opts, err := NewOptions()
	if err != nil {
		panic(err)
	}

	cmd := &cobra.Command{
		Use:   "gardener-controller-manager",
		Short: "Launch the Gardener controller manager",
		Long: `In essence, the Gardener is an extension API server along with a bundle
of Kubernetes controllers which introduce new API objects in an existing Kubernetes
cluster (which is called Garden cluster) in order to use them for the management of
further Kubernetes clusters (which are called Shoot clusters).
To do that reliably and to offer a certain quality of service, it requires to control
the main components of a Kubernetes cluster (etcd, API server, controller manager, scheduler).
These so-called control plane components are hosted in Kubernetes clusters themselves
(which are called Seed clusters).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			verflag.PrintAndExitIfRequested()

			if err := opts.configFileSpecified(); err != nil {
				return err
			}
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
	config, err := opts.loadConfigFromFile(opts.ConfigFile)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	if errs := configvalidation.ValidateControllerManagerConfiguration(config); len(errs) > 0 {
		return errs.ToAggregate()
	}

	// Add feature flags
	if err := controllermanagerfeatures.FeatureGate.SetFromMap(config.FeatureGates); err != nil {
		return fmt.Errorf("failed to set feature gates: %w", err)
	}

	// Initialize logger
	// zapLogger, err := logger.NewZapLogger(config.LogLevel)
	// if err != nil {
	// 	return fmt.Errorf("failed to init logger: %w", err)
	// }

	zapLogger := zap.NewRaw()

	sugarLogger := zapLogger.Sugar()
	defer func() {
		if err := sugarLogger.Sync(); err != nil {
			fmt.Println(err)
		}
	}()

	sugarLogger.Info("Starting Gardener Controller Manager ...")
	sugarLogger.Infof("Feature Gates: %s", controllermanagerfeatures.FeatureGate.String())

	// set the logger used by sigs.k8s.io/controller-runtime
	// zapLogr := logger.NewZapLogr(zapLogger)
	zapLogr := zap.New()
	ctrlruntimelog.SetLogger(zapLogr)

	// if flag := flag.Lookup("v"); flag != nil {
	// 	if err := flag.Value.Set(fmt.Sprintf("%d", cfg.KubernetesLogLevel)); err != nil {
	// 		return nil, err
	// 	}
	// }

	// Prepare a Kubernetes client object for the Garden cluster which contains all the Clientsets
	// that can be used to access the Kubernetes API.
	if kubeconfig := os.Getenv("KUBECONFIG"); kubeconfig != "" {
		config.GardenClientConnection.Kubeconfig = kubeconfig
	}

	restCfg, err := kubernetes.RESTConfigFromClientConnectionConfiguration(&config.GardenClientConnection, nil)
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes REST configuration: %w", err)
	}

	// Setup controller-runtime manager
	mgr, err := manager.New(restCfg, manager.Options{
		MetricsBindAddress:         getAddress(config.MetricsServer),
		HealthProbeBindAddress:     getHealthAddress(config),
		LeaderElection:             config.LeaderElection.LeaderElect,
		LeaderElectionID:           "gardener-scheduler-leader-election",
		LeaderElectionNamespace:    config.LeaderElection.ResourceNamespace,
		LeaderElectionResourceLock: config.LeaderElection.ResourceLock,
		Logger:                     zapLogr,
	})
	if err != nil {
		return fmt.Errorf("failed to create controller manager: %w", err)
	}

	// Add APIs
	if err := gardencorev1beta1.AddToScheme(mgr.GetScheme()); err != nil {
		return fmt.Errorf("failed to register scheme gardencorev1beta1: %w", err)
	}

	// Setup client map; this should be refactored to use the Zap logger
	clientMapLogger := logger.NewLogger(config.LogLevel)

	clientMap, err := clientmapbuilder.NewDelegatingClientMapBuilder().
		WithGardenClientMapBuilder(clientmapbuilder.NewGardenClientMapBuilder().WithRESTConfig(restCfg)).
		WithPlantClientMapBuilder(clientmapbuilder.NewPlantClientMapBuilder()).
		WithLogger(clientMapLogger).
		Build()
	if err != nil {
		return fmt.Errorf("failed to build ClientMap: %w", err)
	}

	// Add controllers
	eventRecorder := mgr.GetEventRecorderFor("gardener-controller-manager")
	factory := controller.NewGardenControllerFactory(clientMap, config, eventRecorder, zapLogr)

	if err := factory.AddControllers(cmd.Context(), mgr); err != nil {
		return fmt.Errorf("failed to add controllers: %w", err)
	}

	// Start manager and all runnables (the command context is tied to OS signals already)
	if err := mgr.Start(cmd.Context()); err != nil {
		return fmt.Errorf("failed to start manager: %w", err)
	}

	return nil
}

func getHealthAddress(cfg *config.ControllerManagerConfiguration) string {
	address := getAddress(cfg.HealthServer)
	if address == "0" {
		address = getAddress(&cfg.Server)
	}

	return address
}

func getAddress(server *config.ServerConfiguration) string {
	if server != nil && len(server.HTTP.BindAddress) > 0 && server.HTTP.Port != 0 {
		return net.JoinHostPort(server.HTTP.BindAddress, strconv.Itoa(server.HTTP.Port))
	}

	return "0" // 0 means "disabled" in ctrl-runtime speak
}
