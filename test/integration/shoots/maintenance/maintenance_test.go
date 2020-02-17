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

/**
	Overview
		- Tests the machine image & Kubernetes version maintenance operations for a shoot

	BeforeSuite
		- Prepare valid Shoot from example folder using InfrastructureProvider config
		- If running in TestMachinery mode: scale down the Gardener-Scheduler
		- Create Shoot
		- Update CloudProfile to include a test machine image and a test Kubernetes version

	AfterSuite
		- Delete Shoot and cleanup CloudProfile

	Test: Machine Image Maintenance test
		1) Shoot.Spec.AutoUpdate.MachineImageVersion == false && expirationDate does not apply
		Expected Output
			- shoot machineImage must not be updated in maintenance time
		2) Shoot.Spec.AutoUpdate.MachineImageVersion == true && expirationDate does not apply
		Expected Output
			- shoot machineImage must be updated in maintenance time
		3) Shoot.Spec.AutoUpdate.KubernetesVersion == false && expirationDate does not apply
		Expected Output
			- shoot machineImage must not be updated in maintenance time
		4) Shoot.Spec.AutoUpdate.MachineImageVersion == false && expirationDate applies
		Expected Output
			- shoot machineImage must be updated in maintenance time

	Test: Kubernetes Version Maintenance test
		1) Shoot.Spec.AutoUpdate.KubernetesVersion == false && expirationDate does not apply
		Expected Output
			- shoot Kubernetes version must not be updated in maintenance time
		2) AutoUpdate.KubernetesVersion == true && expirationDate does not apply
		Expected Output
			- shoot Kubernetes version must not be updated in maintenance time
		3) Patch Version update: AutoUpdate.KubernetesVersion == false && expirationDate applies
		Expected Output
			- shoot Kubernetes version must be updated in maintenance time to highest patch version of its minor version
		4) Minor Version update: AutoUpdate.KubernetesVersion == false && expirationDate applies
		Expected Output
			- shoot Kubernetes version must be updated in maintenance time to highest patch version of next minor version
 **/

package maintenance

import (
	"context"
	"flag"
	"fmt"
	"time"

	"k8s.io/utils/pointer"

	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	"github.com/gardener/gardener/pkg/apis/core/v1beta1/helper"
	"github.com/gardener/gardener/test/framework"
	. "github.com/gardener/gardener/test/integration/framework"
	. "github.com/gardener/gardener/test/integration/shoots"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	testShootsPrefix              = flag.String("prefix", "", "prefix to use for test shoots")
	shootMaintenanceTestNamespace = flag.String("shoot-test-namespace", "", "the namespace where the shoot will be created")
	shootMachineImageName         = flag.String("machine-image-name", "", "the Machine Image Name of the test shoot. Defaults to first machine image in the CloudProfile.")
	shootMachineType              = flag.String("machine-type", "", "the Machine type of the first worker of the test shoot. Needs to match the machine types for that Provider available in the CloudProfile")
	testMachineryRun              = flag.Bool("test-machinery-run", false, "indicates whether the test is being executed by the test machinery or locally")
	cloudProfile                  = flag.String("cloud-profile", "", "cloudProfile to use for the shoot")
	shootRegion                   = flag.String("region", "", "region to use for the shoot. Must be compatible with the infrastructureProvider.Zone.")
	secretBinding                 = flag.String("secret-binding", "", "the secretBinding for the provider account of the shoot")
	shootProviderType             = flag.String("provider-type", "", "the type of the cloud provider where the shoot is deployed to. e.g gcp, aws,azure,alicloud")
	shootK8sVersion               = flag.String("k8s-version", "", "kubernetes version to use for the shoot")
	workerZone                    = flag.String("worker-zone", "", "zone to use for every worker of the shoot.")

	// ProviderConfigs
	infrastructureProviderConfig = flag.String("infrastructure-provider-config-filepath", "", "filepath to the provider specific infrastructure config")

	setupContextTimeout           = time.Minute * 2
	restoreCtxTimeout             = time.Minute * 2
	gardenerSchedulerReplicaCount *int32
	shootMaintenanceTest          *ShootMaintenanceTest

	intialShootForCreation                     gardencorev1beta1.Shoot
	shootCleanupNeeded                         bool
	cloudProfileCleanupNeeded                  bool
	testMachineImageVersion                    = "0.0.1-beta"
	testKubernetesVersionLowMinor              = gardencorev1beta1.ExpirableVersion{Version: "0.0.1", Classification: &deprecatedClassification}
	testHighestPatchKubernetesVersionLowMinor  = gardencorev1beta1.ExpirableVersion{Version: "0.0.5", Classification: &deprecatedClassification}
	testKubernetesVersionHighMinor             = gardencorev1beta1.ExpirableVersion{Version: "0.1.1", Classification: &deprecatedClassification}
	testHighestPatchKubernetesVersionHighMinor = gardencorev1beta1.ExpirableVersion{Version: "0.1.5", Classification: &deprecatedClassification}
	expirationDateInTheFuture                  = metav1.Time{Time: time.Now().UTC().Add(time.Second * 20)}
	expirationDateInThePast                    = metav1.Time{Time: time.Now().UTC().AddDate(0, 0, -1)}
	testMachineImage                           = gardencorev1beta1.ShootMachineImage{
		Version: testMachineImageVersion,
	}

	trueVar = true
	err     error

	deprecatedClassification = gardencorev1beta1.ClassificationDeprecated

	shootYamlPath = "/example/90-shoot.yaml"
)

const (
	WaitForCreateDeleteTimeout = 7200 * time.Second
	InitializationTimeout      = 600 * time.Second
)

func init() {
	framework.RegisterGardenerFrameworkFlags(nil)
}

func validateFlags() {
	if !framework.StringSet(*shootProviderType) {
		Fail("you need to specify provider type of the shoot")
	}

	if !framework.StringSet(*infrastructureProviderConfig) {
		Fail(fmt.Sprintf("you need to specify the filepath to the infrastructureProviderConfig for the provider '%s'", *shootProviderType))
	}

	if !framework.FileExists(*infrastructureProviderConfig) {
		Fail("path to the infrastructureProviderConfig of the Shoot is invalid")
	}
}

var _ = Describe("Shoot Maintenance testing", func() {
	f := framework.NewGardenerFramework(&framework.GardenerConfig{
		CommonConfig: &framework.CommonConfig{
			ResourceDir: "../../framework/resources",
		},
	})

	framework.CIt("Prepare Shoot and CloudProfile", func(ctx context.Context) {
		validateFlags()

		shootObject := prepareShoot(f)
		intialShootForCreation = *shootObject.DeepCopy()

		cloudProfile, err := f.GetCloudProfile(ctx, shootObject.Spec.CloudProfileName)
		Expect(err).ToNot(HaveOccurred())

		shootMaintenanceTest, err = NewShootMaintenanceTest(ctx, f.GardenClient, cloudProfile, shootObject, shootMachineImageName, f.Logger)
		Expect(err).To(BeNil())
		testMachineImage.Name = shootMaintenanceTest.ShootMachineImage.Name

		framework.SetupShootWorker(shootMaintenanceTest.Shoot, shootMaintenanceTest.CloudProfile, workerZone)
		Expect(err).To(BeNil())
		Expect(len(shootMaintenanceTest.Shoot.Spec.Provider.Workers)).Should(BeNumerically("==", 1))

		// set machine type & if set, the machineImage name on the first worker image
		if shootMachineType != nil && len(*shootMachineType) > 0 {
			shootMaintenanceTest.Shoot.Spec.Provider.Workers[0].Machine.Type = *shootMachineType
		}

		if shootMachineImageName != nil && len(*shootMachineImageName) > 0 {
			shootMaintenanceTest.Shoot.Spec.Provider.Workers[0].Machine.Image.Name = *shootMachineImageName
		}

		if testMachineryRun != nil && *testMachineryRun {
			f.Logger.Info("Running in test Machinery")
			// setup the integration test environment by manipulation the Gardener Components (namespace garden) in the garden cluster
			// scale down the gardener-scheduler to 0 replicas
			replicas, err := ScaleGardenerScheduler(setupContextTimeout, f.GardenClient.Client(), pointer.Int32Ptr(0))
			gardenerSchedulerReplicaCount = replicas
			Expect(err).To(BeNil())
			f.Logger.Info("Environment for test-machinery run is prepared")
		}

		// the test machine version is being added to
		prepareCloudProfile(ctx, f)
		cloudProfileCleanupNeeded = true
	}, InitializationTimeout)

	CAfterSuite(func(ctx context.Context) {
		if cloudProfileCleanupNeeded {
			err := shootMaintenanceTest.CleanupCloudProfile(ctx, testMachineImage, []gardencorev1beta1.ExpirableVersion{testKubernetesVersionLowMinor, testHighestPatchKubernetesVersionLowMinor, testKubernetesVersionHighMinor, testHighestPatchKubernetesVersionHighMinor})
			Expect(err).NotTo(HaveOccurred())
			f.Logger.Infof("Cleaned Cloud Profile '%s'", shootMaintenanceTest.CloudProfile.Name)
		}
		if testMachineryRun != nil && *testMachineryRun {
			_, err := ScaleGardenerScheduler(restoreCtxTimeout, f.GardenClient.Client(), gardenerSchedulerReplicaCount)
			Expect(err).NotTo(HaveOccurred())
			f.Logger.Infof("Environment is restored")
		}
	}, InitializationTimeout)

	CAfterEach(func(ctx context.Context) {
		if shootCleanupNeeded {
			// Finally we delete the shoot again
			f.Logger.Infof("Delete shoot %s", shootMaintenanceTest.Shoot.Name)
			err := f.DeleteShootAndWaitForDeletion(ctx, shootMaintenanceTest.Shoot)
			Expect(err).NotTo(HaveOccurred())
			shootCleanupNeeded = false
		}
	}, WaitForCreateDeleteTimeout)

	CBeforeEach(func(ctx context.Context) {
		if shootMaintenanceTest != nil && !shootCleanupNeeded {
			// set dummy kubernetes version to shoot
			intialShootForCreation.Spec.Kubernetes.Version = testKubernetesVersionLowMinor.Version
			// set integration test machineImage to shoot
			intialShootForCreation.Spec.Provider.Workers[0].Machine.Image = &testMachineImage

			shootMaintenanceTest.Shoot = intialShootForCreation.DeepCopy()

			err := f.GetShoot(ctx, shootMaintenanceTest.Shoot)
			Expect(apierrors.IsNotFound(err)).To(Equal(true))
			shoot, err := f.CreateShootResource(ctx, shootMaintenanceTest.Shoot)
			Expect(err).NotTo(HaveOccurred())
			shootMaintenanceTest.Shoot = shoot
			shootCleanupNeeded = true
		}
	}, WaitForCreateDeleteTimeout)

	// CIt("Machine Image Maintenance test", func(ctx context.Context) {
	// 	By("AutoUpdate.MachineImageVersion == false && expirationDate does not apply -> shoot machineImage must not be updated in maintenance time")
	// 	err := f.GetShoot(ctx, shootMaintenanceTest.Shoot)
	// 	Expect(err).To(BeNil())
	//
	// 	// set test specific shoot settings
	// 	shootMaintenanceTest.Shoot.Spec.Maintenance.AutoUpdate.MachineImageVersion = false
	//
	// 	// update integration test shoot
	// 	err = shootMaintenanceTest.TryUpdateShootForMachineImageMaintenance(ctx, shootMaintenanceTest.Shoot, true, nil)
	// 	Expect(err).To(BeNil())
	//
	// 	err = shootMaintenanceTest.WaitForExpectedMachineImageMaintenance(ctx, testMachineImage, false, time.Now().Add(time.Second*10))
	// 	Expect(err).To(BeNil())
	//
	// 	By("AutoUpdate.MachineImageVersion == true && expirationDate does not apply -> shoot machineImage must be updated in maintenance time")
	// 	// set test specific shoot settings
	// 	shootMaintenanceTest.Shoot.Spec.Maintenance.AutoUpdate.MachineImageVersion = trueVar
	//
	// 	// update integration test shoot - set maintain now annotation & autoupdate == true
	// 	err = shootMaintenanceTest.TryUpdateShootForMachineImageMaintenance(ctx, shootMaintenanceTest.Shoot, true, nil)
	// 	Expect(err).To(BeNil())
	//
	// 	err = shootMaintenanceTest.WaitForExpectedMachineImageMaintenance(ctx, shootMaintenanceTest.ShootMachineImage, true, time.Now().Add(time.Second*20))
	// 	Expect(err).To(BeNil())
	//
	// 	By("AutoUpdate.MachineImageVersion == default && expirationDate does not apply -> shoot machineImage must be updated in maintenance time")
	// 	// set test specific shoot settings
	// 	shootMaintenanceTest.Shoot.Spec.Maintenance.AutoUpdate = nil
	//
	// 	// reset machine image from latest version to dummy version
	// 	intialShootForCreation.Spec.Provider.Workers[0].Machine.Image = &testMachineImage
	//
	// 	// update integration test shoot - downgrade image again & set maintain now  annotation & autoupdate == nil (default)
	// 	err = shootMaintenanceTest.TryUpdateShootForMachineImageMaintenance(ctx, shootMaintenanceTest.Shoot, true, &intialShootForCreation.Spec.Provider.Workers)
	// 	Expect(err).To(BeNil())
	//
	// 	err = shootMaintenanceTest.WaitForExpectedMachineImageMaintenance(ctx, shootMaintenanceTest.ShootMachineImage, true, time.Now().Add(time.Second*20))
	// 	Expect(err).To(BeNil())
	//
	// 	By("AutoUpdate.MachineImageVersion == false && expirationDate applies -> shoot machineImage must be updated in maintenance time")
	// 	defer func() {
	// 		// make sure to remove expiration date from cloud profile after test
	// 		err = shootMaintenanceTest.TryUpdateCloudProfileForMachineImageMaintenance(ctx, shootMaintenanceTest.Shoot, testMachineImage, nil, &deprecatedClassification)
	// 		Expect(err).To(BeNil())
	// 		f.Logger.Infof("Cleaned expiration date on machine image from Cloud Profile '%s'", shootMaintenanceTest.CloudProfile.Name)
	// 	}()
	//
	// 	// set test specific shoot settings
	// 	shootMaintenanceTest.Shoot.Spec.Maintenance.AutoUpdate = &gardencorev1beta1.MaintenanceAutoUpdate{MachineImageVersion: false}
	//
	// 	// reset machine image from latest version to dummy version
	// 	intialShootForCreation.Spec.Provider.Workers[0].Machine.Image = &testMachineImage
	//
	// 	// update integration test shoot - downgrade image again & set maintain now annotation & autoupdate == nil (default)
	// 	err = shootMaintenanceTest.TryUpdateShootForMachineImageMaintenance(ctx, shootMaintenanceTest.Shoot, false, &intialShootForCreation.Spec.Provider.Workers)
	// 	Expect(err).To(BeNil())
	//
	// 	// modify cloud profile for test
	// 	err = shootMaintenanceTest.TryUpdateCloudProfileForMachineImageMaintenance(ctx, shootMaintenanceTest.Shoot, testMachineImage, &expirationDateInTheFuture, &deprecatedClassification)
	// 	Expect(err).To(BeNil())
	//
	// 	// sleep so that expiration date is in the past - forceUpdate is required
	// 	time.Sleep(30 * time.Second)
	//
	// 	// update integration test shoot - set maintain now  annotation
	// 	err = shootMaintenanceTest.TryUpdateShootForMachineImageMaintenance(ctx, shootMaintenanceTest.Shoot, true, nil)
	// 	Expect(err).To(BeNil())
	//
	// 	err = shootMaintenanceTest.WaitForExpectedMachineImageMaintenance(ctx, shootMaintenanceTest.ShootMachineImage, true, time.Now().Add(time.Minute*1))
	// 	Expect(err).To(BeNil())
	//
	// }, WaitForCreateDeleteTimeout)
	//
	// CIt("Kubernetes Version update opt-out of - should not be updated", func(ctx context.Context) {
	// 	By("AutoUpdate.KubernetesVersion == false && expirationDate does not apply -> shoot Kubernetes version must not be updated in maintenance time")
	// 	err := f.GetShoot(ctx, shootMaintenanceTest.Shoot)
	// 	Expect(err).To(BeNil())
	//
	// 	// set test specific shoot settings
	// 	shootMaintenanceTest.Shoot.Spec.Maintenance.AutoUpdate.KubernetesVersion = false
	//
	// 	// update integration test shoot
	// 	err = shootMaintenanceTest.TryUpdateShootForKubernetesMaintenance(ctx, shootMaintenanceTest.Shoot, true, nil)
	// 	Expect(err).To(BeNil())
	//
	// 	err = shootMaintenanceTest.WaitForExpectedKubernetesVersionMaintenance(ctx, testKubernetesVersionLowMinor.Version, false, time.Now().Add(time.Second*10))
	// 	Expect(err).To(BeNil())
	//
	// }, WaitForCreateDeleteTimeout)

	CIt("Kubernetes Version update opt-in - should be updated", func(ctx context.Context) {
		By("Kubernetes Version update opt-in - should be updated: AutoUpdate.KubernetesVersion == true && expirationDate does not apply")
		err := f.GetShoot(ctx, shootMaintenanceTest.Shoot)
		Expect(err).To(BeNil())

		// set test specific shoot settings
		shootMaintenanceTest.Shoot.Spec.Maintenance.AutoUpdate.KubernetesVersion = trueVar

		// update integration test shoot
		err = shootMaintenanceTest.TryUpdateShootForKubernetesMaintenance(ctx, shootMaintenanceTest.Shoot, true, nil)
		Expect(err).To(BeNil())

		err = shootMaintenanceTest.WaitForExpectedKubernetesVersionMaintenance(ctx, testHighestPatchKubernetesVersionLowMinor.Version, true, time.Now().Add(time.Second*20))
		Expect(err).To(BeNil())
	}, WaitForCreateDeleteTimeout)

	CIt("Kubernetes Version force update - Patch version", func(ctx context.Context) {
		By("Kubernetes Version force update - Patch version: AutoUpdate.KubernetesVersion == false && expirationDate applies")
		err := f.GetShoot(ctx, shootMaintenanceTest.Shoot)
		Expect(err).To(BeNil())
		defer func() {
			// make sure to remove expiration date from cloud profile after test
			err = shootMaintenanceTest.TryUpdateCloudProfileForKubernetesVersionMaintenance(ctx, shootMaintenanceTest.Shoot, testKubernetesVersionLowMinor.Version, nil, &deprecatedClassification)
			Expect(err).To(BeNil())
			f.Logger.Infof("Cleaned expiration date on kubernetes version from Cloud Profile '%s'", shootMaintenanceTest.CloudProfile.Name)
		}()

		// modify cloud profile for test
		err = shootMaintenanceTest.TryUpdateCloudProfileForKubernetesVersionMaintenance(ctx, shootMaintenanceTest.Shoot, testKubernetesVersionLowMinor.Version, &expirationDateInTheFuture, &deprecatedClassification)
		Expect(err).To(BeNil())

		// set test specific shoot settings
		shootMaintenanceTest.Shoot.Spec.Maintenance.AutoUpdate.KubernetesVersion = false

		// update integration test shoot - autoupdate == false
		err = shootMaintenanceTest.TryUpdateShootForKubernetesMaintenance(ctx, shootMaintenanceTest.Shoot, false, nil)
		Expect(err).To(BeNil())

		// sleep so that expiration date is in the past - forceUpdate is required
		time.Sleep(30 * time.Second)

		// update integration test shoot - set maintain now  annotation
		err = shootMaintenanceTest.TryUpdateShootForKubernetesMaintenance(ctx, shootMaintenanceTest.Shoot, true, nil)
		Expect(err).To(BeNil())

		err = shootMaintenanceTest.WaitForExpectedKubernetesVersionMaintenance(ctx, testHighestPatchKubernetesVersionLowMinor.Version, true, time.Now().Add(time.Second*20))
		Expect(err).To(BeNil())
	}, WaitForCreateDeleteTimeout)

	CIt("Kubernetes Version force update - Minor version", func(ctx context.Context) {
		By("Kubernetes Version force update - latest patch of next Minor version: AutoUpdate.KubernetesVersion == false && expirationDate does apply && is highest patch version")
		err := f.GetShoot(ctx, shootMaintenanceTest.Shoot)
		Expect(err).To(BeNil())

		defer func() {
			// make sure to remove expiration date from cloud profile after test
			err = shootMaintenanceTest.TryUpdateCloudProfileForKubernetesVersionMaintenance(ctx, shootMaintenanceTest.Shoot, testHighestPatchKubernetesVersionLowMinor.Version, &expirationDateInThePast, &deprecatedClassification)
			Expect(err).To(BeNil())
			f.Logger.Infof("Cleaned expiration date on kubernetes version from Cloud Profile '%s'", shootMaintenanceTest.CloudProfile.Name)
		}()

		// set the shoots Kubernetes version to be the highest patch version of its minor version & autoupdate == false
		shootMaintenanceTest.Shoot.Spec.Maintenance.AutoUpdate.KubernetesVersion = false
		err = shootMaintenanceTest.TryUpdateShootForKubernetesMaintenance(ctx, shootMaintenanceTest.Shoot, false, &testHighestPatchKubernetesVersionLowMinor.Version)
		Expect(err).To(BeNil())

		// let Shoot's Kubernetes version expire
		err = shootMaintenanceTest.TryUpdateCloudProfileForKubernetesVersionMaintenance(ctx, shootMaintenanceTest.Shoot, testHighestPatchKubernetesVersionLowMinor.Version, &expirationDateInTheFuture, &deprecatedClassification)
		Expect(err).To(BeNil())

		// sleep so that expiration date is in the past - forceUpdate is required
		time.Sleep(30 * time.Second)

		// update integration test shoot - set maintain now  annotation
		err = shootMaintenanceTest.TryUpdateShootForKubernetesMaintenance(ctx, shootMaintenanceTest.Shoot, true, nil)
		Expect(err).To(BeNil())

		// expect shoot to have updated to latest patch version of next minor version
		err = shootMaintenanceTest.WaitForExpectedKubernetesVersionMaintenance(ctx, testHighestPatchKubernetesVersionHighMinor.Version, true, time.Now().Add(time.Second*20))
		Expect(err).To(BeNil())
	}, WaitForCreateDeleteTimeout)
})

func prepareCloudProfile(ctx context.Context, f *framework.GardenerFramework) {
	// setup cloud profile for integration test
	profile := shootMaintenanceTest.CloudProfile

	found, image, err := helper.DetermineMachineImageForName(profile, shootMaintenanceTest.ShootMachineImage.Name)
	Expect(err).To(BeNil())
	Expect(found).To(Equal(true))

	imageVersions := append(image.Versions, gardencorev1beta1.ExpirableVersion{Version: testMachineImageVersion, Classification: &deprecatedClassification})
	updatedCloudProfileImages, err := helper.SetMachineImageVersionsToMachineImage(profile.Spec.MachineImages, shootMaintenanceTest.ShootMachineImage.Name, imageVersions)
	Expect(err).To(BeNil())
	// need one image in Cloud Profile to be updated with one additional version
	profile.Spec.MachineImages = updatedCloudProfileImages

	// add  test kubernetes versions (one low patch version, one high patch version)
	profile.Spec.Kubernetes.Versions = append(profile.Spec.Kubernetes.Versions, testKubernetesVersionLowMinor, testHighestPatchKubernetesVersionLowMinor, testKubernetesVersionHighMinor, testHighestPatchKubernetesVersionHighMinor)
	err = f.GardenClient.Client().Update(ctx, profile)
	Expect(err).To(BeNil())
}

func prepareShoot(f *framework.GardenerFramework) *gardencorev1beta1.Shoot {
	// if running in test machinery, test will be executed from root of the project
	if !FileExists(fmt.Sprintf(".%s", shootYamlPath)) {
		// locally, we need find the example shoot
		shootYamlPath = GetProjectRootPath() + shootYamlPath
		Expect(FileExists(shootYamlPath)).To(Equal(true))
	}
	// parse shoot yaml into shoot object and generate random test names for shoots
	_, shootObject, err := framework.CreateShootTestArtifacts(shootYamlPath, testShootsPrefix, &f.ProjectNamespace, shootRegion, cloudProfile, secretBinding, shootProviderType, shootK8sVersion, nil, true, true)
	Expect(err).To(BeNil())

	shootObject.Spec.Extensions = nil

	// set ProviderConfigs
	err = framework.SetProviderConfigsFromFilepath(shootObject, infrastructureProviderConfig, nil, nil, nil)
	Expect(err).To(BeNil())
	// set other provider configs to nil as we do not need them for shoot creation
	shootObject.Spec.Provider.ControlPlaneConfig = nil
	return shootObject
}
