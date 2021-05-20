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

package extensions

import (
	"context"
	"fmt"
	"sync"
	"time"

	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/gardener/gardener/pkg/client/kubernetes"
	"github.com/gardener/gardener/pkg/controllerutils"

	"github.com/sirupsen/logrus"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// Controller watches the extension resources and has several control loops.
type Controller struct {
	log *logrus.Entry

	waitGroup              sync.WaitGroup
	workerCh               chan int
	numberOfRunningWorkers int

	controllerArtifacts           controllerArtifacts
	controllerInstallationControl *controllerInstallationControl
	shootStateControl             *ShootStateControl
	shootExtensionStatusControl   *ShootExtensionStatusControl
}

// NewController creates new controller that syncs extensions states to ShootState
func NewController(ctx context.Context, gardenClient, seedClient kubernetes.Interface, seedName string, log *logrus.Entry, recorder record.EventRecorder) (*Controller, error) {
	controller := &Controller{
		log:      log,
		workerCh: make(chan int),

		controllerArtifacts: newControllerArtifacts(),
		controllerInstallationControl: &controllerInstallationControl{
			k8sGardenClient:             gardenClient,
			seedClient:                  seedClient,
			seedName:                    seedName,
			log:                         log,
			controllerInstallationQueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "controllerinstallation-extension-required"),
			lock:                        &sync.RWMutex{},
			kindToRequiredTypes:         make(map[string]sets.String),
		},
		shootStateControl:           NewShootStateControl(gardenClient, seedClient, log, recorder),
		shootExtensionStatusControl: NewShootExtensionStatusControl(gardenClient.Client(), seedClient.Client(), log, recorder),
	}

	if err := controller.controllerArtifacts.initialize(ctx, seedClient); err != nil {
		return nil, err
	}

	return controller, nil
}

// Run creates workers that reconciles extension resources.
func (s *Controller) Run(ctx context.Context, controllerInstallationWorkers, shootStateWorkers int) error {
	timeoutCtx, cancel := context.WithTimeout(ctx, time.Minute*2)
	defer cancel()

	if !cache.WaitForCacheSync(timeoutCtx.Done(), s.controllerArtifacts.hasSyncedFuncs...) {
		return fmt.Errorf("timeout waiting for extension informers to sync")
	}

	// Count number of running workers.
	go func() {
		for res := range s.workerCh {
			s.numberOfRunningWorkers += res
			s.log.Debugf("Current number of running extension controller workers is %d", s.numberOfRunningWorkers)
		}
	}()

	for i := 0; i < controllerInstallationWorkers; i++ {
		s.createControllerInstallationWorkers(ctx, s.controllerInstallationControl)
	}

	for i := 0; i < shootStateWorkers; i++ {
		s.createShootStateWorkers(ctx, s.shootStateControl)
	}

	shootExtensionStatusWorkers := 1
	for i := 0; i < shootExtensionStatusWorkers; i++ {
		s.createShootExtensionStatusWorkers(ctx, s.shootExtensionStatusControl)
	}

	s.log.Info("Extension controller initialized.")
	return nil
}

func (s *Controller) createControllerInstallationWorkers(ctx context.Context, control *controllerInstallationControl) {
	controllerutils.CreateWorker(ctx, s.controllerInstallationControl.controllerInstallationQueue, "ControllerInstallation-Required", reconcile.Func(control.reconcileControllerInstallationRequired), &s.waitGroup, s.workerCh)

	for kind, artifact := range s.controllerArtifacts.controllerInstallationArtifacts {
		workerName := fmt.Sprintf("ControllerInstallation-Extension-%s", kind)
		controlFn := control.createExtensionRequiredReconcileFunc(kind, artifact.newListFunc)
		// Execute control function once outside of the worker to initialize the `kindToRequiredTypes` map once.
		// This is necessary for Kinds which are registered but no extension object exists in the seed yet (e.g. disabled backups).
		// In this case no event is triggered and the control function would never be executed.
		// Eventually, the Kind would never be part of the `kindToRequiredTypes` map and no decision if the ControllerInstallation is required could be taken.
		if _, err := controlFn(ctx, reconcile.Request{}); err != nil {
			s.log.Errorf("Error during initial run of extension reconciliation: %v", err)
		}
		controllerutils.CreateWorker(ctx, artifact.queue, workerName, controlFn, &s.waitGroup, s.workerCh)
	}
}

func (s *Controller) createShootStateWorkers(ctx context.Context, control *ShootStateControl) {
	for kind, artifact := range s.controllerArtifacts.stateArtifacts {
		workerName := fmt.Sprintf("ShootState-%s", kind)
		controllerutils.CreateWorker(ctx, artifact.queue, workerName, control.CreateShootStateSyncReconcileFunc(kind, artifact.newObjFunc), &s.waitGroup, s.workerCh)
	}
}

func (s *Controller) createShootExtensionStatusWorkers(ctx context.Context, control *ShootExtensionStatusControl) {
	for kind, artifact := range s.controllerArtifacts.extensionStatusArtifacts {
		workerName := fmt.Sprintf("ShootExtensionStatus-%s", kind)
		controllerutils.CreateWorker(ctx, artifact.queue, workerName, control.CreateShootExtensionStatusSyncReconcileFunc(kind, artifact.newObjFunc), &s.waitGroup, s.workerCh)
	}
}

// Stop the controller
func (s *Controller) Stop() {
	s.controllerInstallationControl.controllerInstallationQueue.ShutDown()
	s.controllerArtifacts.shutdownQueues()
	s.waitGroup.Wait()
}

func createEnqueueFunc(queue workqueue.RateLimitingInterface) func(extensionObject interface{}) {
	return func(newObj interface{}) {
		enqueue(queue, newObj)
	}
}

// createEnqueueEmptyRequestFunc creates a func that enqueues an empty reconcile.Request into the given queue, which
// can serve as a trigger for a reconciler that actually doesn't care about which object was created/deleted, but
// only about that some object of a given kind was created/deleted.
func createEnqueueEmptyRequestFunc(queue workqueue.RateLimitingInterface) func(extensionObject interface{}) {
	return func(_ interface{}) {
		queue.Add(reconcile.Request{})
	}
}

func createEnqueueOnUpdateFunc(queue workqueue.RateLimitingInterface, predicateFunc func(new, old interface{}) bool) func(newExtensionObject, oldExtensionObject interface{}) {
	return func(newObj, oldObj interface{}) {
		if predicateFunc != nil && !predicateFunc(newObj, oldObj) {
			return
		}

		enqueue(queue, newObj)
	}
}

// createEnqueueEmptyRequestOnUpdateFunc is similar to createEnqueueEmptyRequestFunc in that it enqueues an empty
// reconcile.Request, but it only does it if an update matched the given predicateFunc.
func createEnqueueEmptyRequestOnUpdateFunc(queue workqueue.RateLimitingInterface, predicateFunc func(new, old interface{}) bool) func(newExtensionObject, oldExtensionObject interface{}) {
	return func(newObj, oldObj interface{}) {
		if predicateFunc != nil && !predicateFunc(newObj, oldObj) {
			return
		}

		queue.Add(reconcile.Request{})
	}
}

func enqueue(queue workqueue.RateLimitingInterface, obj interface{}) {
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		return
	}
	queue.Add(key)
}

func extensionStateOrResourcesChanged(newObj, oldObj interface{}) bool {
	return extensionPredicateFunc(
		func(new, old extensionsv1alpha1.Object) bool {
			return !apiequality.Semantic.DeepEqual(new.GetExtensionStatus().GetState(), old.GetExtensionStatus().GetState()) ||
				!apiequality.Semantic.DeepEqual(new.GetExtensionStatus().GetResources(), old.GetExtensionStatus().GetResources())
		},
	)(newObj, oldObj)
}

func extensionTypeChanged(newObj, oldObj interface{}) bool {
	return extensionPredicateFunc(
		func(new, old extensionsv1alpha1.Object) bool {
			return old.GetExtensionSpec().GetExtensionType() != new.GetExtensionSpec().GetExtensionType()
		},
	)(newObj, oldObj)
}

func extensionPredicateFunc(f func(extensionsv1alpha1.Object, extensionsv1alpha1.Object) bool) func(interface{}, interface{}) bool {
	return func(newObj, oldObj interface{}) bool {
		var (
			newExtensionObj, ok1 = newObj.(extensionsv1alpha1.Object)
			oldExtensionObj, ok2 = oldObj.(extensionsv1alpha1.Object)
		)
		return ok1 && ok2 && f(newExtensionObj, oldExtensionObj)
	}
}

func extensionProviderStatusChanged(newObj, oldObj interface{}) bool {
	return extensionPredicateFunc(
		func(new, old extensionsv1alpha1.Object) bool {
			return !apiequality.Semantic.DeepEqual(new.GetExtensionStatus().GetProviderStatus(), old.GetExtensionStatus().GetProviderStatus())
		},
	)(newObj, oldObj)
}
