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

package shoot

import (
	"context"

	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	mockclient "github.com/gardener/gardener/pkg/mock/controller-runtime/client"
	"github.com/gardener/gardener/pkg/scheduler/apis/config"
	kutil "github.com/gardener/gardener/pkg/utils/kubernetes"

	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Scheduler_Control", func() {
	var (
		ctx    = context.TODO()
		ctrl   *gomock.Controller
		reader *mockclient.MockReader

		cloudProfile gardencorev1beta1.CloudProfile
		seed         gardencorev1beta1.Seed
		shoot        gardencorev1beta1.Shoot

		schedulerConfiguration config.SchedulerConfiguration

		providerType     = "foo"
		cloudProfileName = "cloudprofile-1"
		seedName         = "seed-1"
		region           = "europe"

		cloudProfileBase = gardencorev1beta1.CloudProfile{
			ObjectMeta: metav1.ObjectMeta{
				Name: cloudProfileName,
			},
		}
		seedBase = gardencorev1beta1.Seed{
			ObjectMeta: metav1.ObjectMeta{
				Name: seedName,
			},
			Spec: gardencorev1beta1.SeedSpec{
				Provider: gardencorev1beta1.SeedProvider{
					Type:   providerType,
					Region: region,
				},
				Networks: gardencorev1beta1.SeedNetworks{
					Nodes:    pointer.String("10.10.0.0/16"),
					Pods:     "10.20.0.0/16",
					Services: "10.30.0.0/16",
				},
				Settings: &gardencorev1beta1.SeedSettings{
					Scheduling: &gardencorev1beta1.SeedSettingScheduling{
						Visible: true,
					},
					ShootDNS: &gardencorev1beta1.SeedSettingShootDNS{
						Enabled: true,
					},
				},
			},
			Status: gardencorev1beta1.SeedStatus{
				Conditions: []gardencorev1beta1.Condition{
					{
						Type:   gardencorev1beta1.SeedGardenletReady,
						Status: gardencorev1beta1.ConditionTrue,
					},
					{
						Type:   gardencorev1beta1.SeedBootstrapped,
						Status: gardencorev1beta1.ConditionTrue,
					},
				},
			},
		}
		shootBase = gardencorev1beta1.Shoot{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "shoot",
				Namespace: "my-namespace",
			},
			Spec: gardencorev1beta1.ShootSpec{
				CloudProfileName: cloudProfileName,
				Region:           region,
				Provider: gardencorev1beta1.Provider{
					Type: providerType,
				},
				Networking: gardencorev1beta1.Networking{
					Nodes:    pointer.String("10.40.0.0/16"),
					Pods:     pointer.String("10.50.0.0/16"),
					Services: pointer.String("10.60.0.0/16"),
				},
			},
		}

		schedulerConfigurationBase = config.SchedulerConfiguration{
			TypeMeta: metav1.TypeMeta{
				APIVersion: config.SchemeGroupVersion.String(),
				Kind:       "SchedulerConfiguration",
			},
			Schedulers: config.SchedulerControllerConfiguration{
				Shoot: &config.ShootSchedulerConfiguration{
					Strategy: config.SameRegion,
				},
			},
		}
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		reader = mockclient.NewMockReader(ctrl)
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	Context("SEED DETERMINATION - Shoot does not reference a Seed - find an adequate one using 'Same Region' seed determination strategy", func() {
		BeforeEach(func() {
			cloudProfile = *cloudProfileBase.DeepCopy()
			seed = *seedBase.DeepCopy()
			shoot = *shootBase.DeepCopy()
			schedulerConfiguration = *schedulerConfigurationBase.DeepCopy()
			// no seed referenced
			shoot.Spec.SeedName = nil
		})

		// PASS

		It("should find a seed cluster 1) 'Same Region' seed determination strategy 2) referencing the same profile 3) same region 4) indicating availability", func() {
			reader.EXPECT().Get(ctx, kutil.Key(cloudProfile.Name), gomock.AssignableToTypeOf(&gardencorev1beta1.CloudProfile{})).DoAndReturn(func(_ context.Context, _ client.ObjectKey, actual *gardencorev1beta1.CloudProfile) error {
				*actual = cloudProfile
				return nil
			})
			reader.EXPECT().List(ctx, gomock.AssignableToTypeOf(&gardencorev1beta1.SeedList{})).DoAndReturn(func(_ context.Context, actual *gardencorev1beta1.SeedList, _ ...client.ListOption) error {
				*actual = gardencorev1beta1.SeedList{Items: []gardencorev1beta1.Seed{seed}}
				return nil
			})
			reader.EXPECT().List(ctx, gomock.AssignableToTypeOf(&gardencorev1beta1.ShootList{}))

			bestSeed, err := determineSeed(ctx, reader, &shoot, schedulerConfiguration.Schedulers.Shoot.Strategy)
			Expect(err).NotTo(HaveOccurred())
			Expect(bestSeed.Name).To(Equal(seed.Name))
		})

		It("should find the best seed cluster 1) 'Same Region' seed determination strategy 2) referencing the same profile 3) same region 4) indicating availability", func() {
			secondSeed := seedBase
			secondSeed.Name = "seed-2"

			secondShoot := shootBase
			secondShoot.Name = "shoot-2"
			// first seed references more shoots then seed-2 -> expect seed-2 to be selected
			secondShoot.Spec.SeedName = &seed.Name

			reader.EXPECT().Get(ctx, kutil.Key(cloudProfile.Name), gomock.AssignableToTypeOf(&gardencorev1beta1.CloudProfile{})).DoAndReturn(func(_ context.Context, _ client.ObjectKey, actual *gardencorev1beta1.CloudProfile) error {
				*actual = cloudProfile
				return nil
			})
			reader.EXPECT().List(ctx, gomock.AssignableToTypeOf(&gardencorev1beta1.SeedList{})).DoAndReturn(func(_ context.Context, actual *gardencorev1beta1.SeedList, _ ...client.ListOption) error {
				*actual = gardencorev1beta1.SeedList{Items: []gardencorev1beta1.Seed{seed, secondSeed}}
				return nil
			})
			reader.EXPECT().List(ctx, gomock.AssignableToTypeOf(&gardencorev1beta1.ShootList{})).DoAndReturn(func(_ context.Context, actual *gardencorev1beta1.ShootList, _ ...client.ListOption) error {
				*actual = gardencorev1beta1.ShootList{Items: []gardencorev1beta1.Shoot{shoot, secondShoot}}
				return nil
			})

			bestSeed, err := determineSeed(ctx, reader, &shoot, schedulerConfiguration.Schedulers.Shoot.Strategy)
			Expect(err).NotTo(HaveOccurred())
			Expect(bestSeed.Name).To(Equal(secondSeed.Name))
		})

		// FAIL

		It("should fail because it cannot find a seed cluster 1) 'Same Region' seed determination strategy 2) region that no seed supports", func() {
			shoot.Spec.Region = "another-region"

			reader.EXPECT().Get(ctx, kutil.Key(cloudProfile.Name), gomock.AssignableToTypeOf(&gardencorev1beta1.CloudProfile{})).DoAndReturn(func(_ context.Context, _ client.ObjectKey, actual *gardencorev1beta1.CloudProfile) error {
				*actual = cloudProfile
				return nil
			})
			reader.EXPECT().List(ctx, gomock.AssignableToTypeOf(&gardencorev1beta1.SeedList{})).DoAndReturn(func(_ context.Context, actual *gardencorev1beta1.SeedList, _ ...client.ListOption) error {
				*actual = gardencorev1beta1.SeedList{Items: []gardencorev1beta1.Seed{seed}}
				return nil
			})
			reader.EXPECT().List(ctx, gomock.AssignableToTypeOf(&gardencorev1beta1.ShootList{}))

			bestSeed, err := determineSeed(ctx, reader, &shoot, schedulerConfiguration.Schedulers.Shoot.Strategy)
			Expect(err).To(HaveOccurred())
			Expect(bestSeed).To(BeNil())
		})
	})

	Context("SEED DETERMINATION - Shoot does not reference a Seed - find an adequate one using 'MinimalDistance' seed determination strategy", func() {
		var anotherType = "another-type"
		var anotherRegion = "another-region"

		BeforeEach(func() {
			seed = *seedBase.DeepCopy()
			shoot = *shootBase.DeepCopy()
			shoot.Spec.Provider.Type = anotherType
			cloudProfile = *cloudProfileBase.DeepCopy()
			cloudProfile.Spec.Type = anotherType
			schedulerConfiguration = *schedulerConfigurationBase.DeepCopy()
			schedulerConfiguration.Schedulers.Shoot.Strategy = config.MinimalDistance
			// no seed referenced
			shoot.Spec.SeedName = nil
		})

		It("should succeed because it cannot find a seed cluster 1) 'MinimalDistance' seed determination strategy 2) default match", func() {
			seed.Spec.Provider.Type = anotherType
			shoot.Spec.Region = anotherRegion

			reader.EXPECT().Get(ctx, kutil.Key(cloudProfile.Name), gomock.AssignableToTypeOf(&gardencorev1beta1.CloudProfile{})).DoAndReturn(func(_ context.Context, _ client.ObjectKey, actual *gardencorev1beta1.CloudProfile) error {
				*actual = cloudProfile
				return nil
			})
			reader.EXPECT().List(ctx, gomock.AssignableToTypeOf(&gardencorev1beta1.SeedList{})).DoAndReturn(func(_ context.Context, actual *gardencorev1beta1.SeedList, _ ...client.ListOption) error {
				*actual = gardencorev1beta1.SeedList{Items: []gardencorev1beta1.Seed{seed}}
				return nil
			})
			reader.EXPECT().List(ctx, gomock.AssignableToTypeOf(&gardencorev1beta1.ShootList{}))

			bestSeed, err := determineSeed(ctx, reader, &shoot, schedulerConfiguration.Schedulers.Shoot.Strategy)
			Expect(err).NotTo(HaveOccurred())
			Expect(bestSeed).NotTo(BeNil())
		})

		It("should succeed because it cannot find a seed cluster 1) 'MinimalDistance' seed determination strategy 2) cross provider", func() {
			cloudProfile.Spec.SeedSelector = &gardencorev1beta1.SeedSelector{
				ProviderTypes: []string{providerType},
			}
			shoot.Spec.Region = anotherRegion

			reader.EXPECT().Get(ctx, kutil.Key(cloudProfile.Name), gomock.AssignableToTypeOf(&gardencorev1beta1.CloudProfile{})).DoAndReturn(func(_ context.Context, _ client.ObjectKey, actual *gardencorev1beta1.CloudProfile) error {
				*actual = cloudProfile
				return nil
			})
			reader.EXPECT().List(ctx, gomock.AssignableToTypeOf(&gardencorev1beta1.SeedList{})).DoAndReturn(func(_ context.Context, actual *gardencorev1beta1.SeedList, _ ...client.ListOption) error {
				*actual = gardencorev1beta1.SeedList{Items: []gardencorev1beta1.Seed{seed}}
				return nil
			})
			reader.EXPECT().List(ctx, gomock.AssignableToTypeOf(&gardencorev1beta1.ShootList{}))

			bestSeed, err := determineSeed(ctx, reader, &shoot, schedulerConfiguration.Schedulers.Shoot.Strategy)
			Expect(err).NotTo(HaveOccurred())
			Expect(bestSeed).NotTo(BeNil())
		})

		It("should succeed because it cannot find a seed cluster 1) 'MinimalDistance' seed determination strategy 2) any provider", func() {
			cloudProfile.Spec.SeedSelector = &gardencorev1beta1.SeedSelector{
				ProviderTypes: []string{"*"},
			}
			shoot.Spec.Region = anotherRegion

			reader.EXPECT().Get(ctx, kutil.Key(cloudProfile.Name), gomock.AssignableToTypeOf(&gardencorev1beta1.CloudProfile{})).DoAndReturn(func(_ context.Context, _ client.ObjectKey, actual *gardencorev1beta1.CloudProfile) error {
				*actual = cloudProfile
				return nil
			})
			reader.EXPECT().List(ctx, gomock.AssignableToTypeOf(&gardencorev1beta1.SeedList{})).DoAndReturn(func(_ context.Context, actual *gardencorev1beta1.SeedList, _ ...client.ListOption) error {
				*actual = gardencorev1beta1.SeedList{Items: []gardencorev1beta1.Seed{seed}}
				return nil
			})
			reader.EXPECT().List(ctx, gomock.AssignableToTypeOf(&gardencorev1beta1.ShootList{}))

			bestSeed, err := determineSeed(ctx, reader, &shoot, schedulerConfiguration.Schedulers.Shoot.Strategy)
			Expect(err).NotTo(HaveOccurred())
			Expect(bestSeed).NotTo(BeNil())
		})

		It("should succeed because it cannot find a seed cluster 1) 'MinimalDistance' seed determination strategy 2) matching labels", func() {
			cloudProfile.Spec.SeedSelector = &gardencorev1beta1.SeedSelector{
				LabelSelector: metav1.LabelSelector{
					MatchLabels: map[string]string{
						"select": "true",
					},
				},
				ProviderTypes: []string{"*"},
			}
			seed.Labels = map[string]string{"select": "true"}
			shoot.Spec.Region = anotherRegion

			reader.EXPECT().Get(ctx, kutil.Key(cloudProfile.Name), gomock.AssignableToTypeOf(&gardencorev1beta1.CloudProfile{})).DoAndReturn(func(_ context.Context, _ client.ObjectKey, actual *gardencorev1beta1.CloudProfile) error {
				*actual = cloudProfile
				return nil
			})
			reader.EXPECT().List(ctx, gomock.AssignableToTypeOf(&gardencorev1beta1.SeedList{})).DoAndReturn(func(_ context.Context, actual *gardencorev1beta1.SeedList, _ ...client.ListOption) error {
				*actual = gardencorev1beta1.SeedList{Items: []gardencorev1beta1.Seed{seed}}
				return nil
			})
			reader.EXPECT().List(ctx, gomock.AssignableToTypeOf(&gardencorev1beta1.ShootList{}))

			bestSeed, err := determineSeed(ctx, reader, &shoot, schedulerConfiguration.Schedulers.Shoot.Strategy)
			Expect(err).NotTo(HaveOccurred())
			Expect(bestSeed).NotTo(BeNil())
		})

		// FAIL

		It("should fail because it cannot find a seed cluster 1) 'MinimalDistance' seed determination strategy 2) no matching provider", func() {
			reader.EXPECT().Get(ctx, kutil.Key(cloudProfile.Name), gomock.AssignableToTypeOf(&gardencorev1beta1.CloudProfile{})).DoAndReturn(func(_ context.Context, _ client.ObjectKey, actual *gardencorev1beta1.CloudProfile) error {
				*actual = cloudProfile
				return nil
			})
			reader.EXPECT().List(ctx, gomock.AssignableToTypeOf(&gardencorev1beta1.SeedList{})).DoAndReturn(func(_ context.Context, actual *gardencorev1beta1.SeedList, _ ...client.ListOption) error {
				*actual = gardencorev1beta1.SeedList{Items: []gardencorev1beta1.Seed{seed}}
				return nil
			})
			reader.EXPECT().List(ctx, gomock.AssignableToTypeOf(&gardencorev1beta1.ShootList{}))

			bestSeed, err := determineSeed(ctx, reader, &shoot, schedulerConfiguration.Schedulers.Shoot.Strategy)
			Expect(err).To(HaveOccurred())
			Expect(bestSeed).To(BeNil())
		})

		It("should fail because it cannot find a seed cluster 1) 'MinimalDistance' seed determination strategy 2) no matching labels", func() {
			cloudProfile.Spec.SeedSelector = &gardencorev1beta1.SeedSelector{
				LabelSelector: metav1.LabelSelector{
					MatchLabels: map[string]string{
						"select": "true",
					},
				},
				ProviderTypes: []string{providerType},
			}
			reader.EXPECT().Get(ctx, kutil.Key(cloudProfile.Name), gomock.AssignableToTypeOf(&gardencorev1beta1.CloudProfile{})).DoAndReturn(func(_ context.Context, _ client.ObjectKey, actual *gardencorev1beta1.CloudProfile) error {
				*actual = cloudProfile
				return nil
			})
			reader.EXPECT().List(ctx, gomock.AssignableToTypeOf(&gardencorev1beta1.SeedList{})).DoAndReturn(func(_ context.Context, actual *gardencorev1beta1.SeedList, _ ...client.ListOption) error {
				*actual = gardencorev1beta1.SeedList{Items: []gardencorev1beta1.Seed{seed}}
				return nil
			})
			reader.EXPECT().List(ctx, gomock.AssignableToTypeOf(&gardencorev1beta1.ShootList{}))

			bestSeed, err := determineSeed(ctx, reader, &shoot, schedulerConfiguration.Schedulers.Shoot.Strategy)
			Expect(err).To(HaveOccurred())
			Expect(bestSeed).To(BeNil())
		})

		It("should fail because it cannot find a seed cluster 1) 'MinimalDistance' seed determination strategy 2) matching labels but not type", func() {
			cloudProfile.Spec.SeedSelector = &gardencorev1beta1.SeedSelector{
				LabelSelector: metav1.LabelSelector{
					MatchLabels: map[string]string{
						"select": "true",
					},
				},
			}
			seed.Labels = map[string]string{"select": "true"}

			reader.EXPECT().Get(ctx, kutil.Key(cloudProfile.Name), gomock.AssignableToTypeOf(&gardencorev1beta1.CloudProfile{})).DoAndReturn(func(_ context.Context, _ client.ObjectKey, actual *gardencorev1beta1.CloudProfile) error {
				*actual = cloudProfile
				return nil
			})
			reader.EXPECT().List(ctx, gomock.AssignableToTypeOf(&gardencorev1beta1.SeedList{})).DoAndReturn(func(_ context.Context, actual *gardencorev1beta1.SeedList, _ ...client.ListOption) error {
				*actual = gardencorev1beta1.SeedList{Items: []gardencorev1beta1.Seed{seed}}
				return nil
			})
			reader.EXPECT().List(ctx, gomock.AssignableToTypeOf(&gardencorev1beta1.ShootList{}))

			bestSeed, err := determineSeed(ctx, reader, &shoot, schedulerConfiguration.Schedulers.Shoot.Strategy)
			Expect(err).To(HaveOccurred())
			Expect(bestSeed).To(BeNil())
		})
	})

	Context("SEED DETERMINATION - Shoot does not reference a Seed - find an adequate one using 'Minimal Distance' seed determination strategy", func() {
		BeforeEach(func() {
			cloudProfile = *cloudProfileBase.DeepCopy()
			seed = *seedBase.DeepCopy()
			shoot = *shootBase.DeepCopy()
			schedulerConfiguration = *schedulerConfigurationBase.DeepCopy()
			// no seed referenced
			shoot.Spec.SeedName = nil
			schedulerConfiguration.Schedulers.Shoot.Strategy = config.MinimalDistance
		})

		It("should find a seed cluster 1) referencing the same profile 2) same region 3) indicating availability", func() {
			reader.EXPECT().Get(ctx, kutil.Key(cloudProfile.Name), gomock.AssignableToTypeOf(&gardencorev1beta1.CloudProfile{})).DoAndReturn(func(_ context.Context, _ client.ObjectKey, actual *gardencorev1beta1.CloudProfile) error {
				*actual = cloudProfile
				return nil
			})
			reader.EXPECT().List(ctx, gomock.AssignableToTypeOf(&gardencorev1beta1.SeedList{})).DoAndReturn(func(_ context.Context, actual *gardencorev1beta1.SeedList, _ ...client.ListOption) error {
				*actual = gardencorev1beta1.SeedList{Items: []gardencorev1beta1.Seed{seed}}
				return nil
			})
			reader.EXPECT().List(ctx, gomock.AssignableToTypeOf(&gardencorev1beta1.ShootList{}))

			bestSeed, err := determineSeed(ctx, reader, &shoot, schedulerConfiguration.Schedulers.Shoot.Strategy)
			Expect(err).NotTo(HaveOccurred())
			Expect(bestSeed.Name).To(Equal(seedName))
		})

		It("should find a seed cluster from other region: shoot in non-existing region, only one seed existing", func() {
			anotherRegion := "another-region"
			shoot.Spec.Region = anotherRegion

			reader.EXPECT().Get(ctx, kutil.Key(cloudProfile.Name), gomock.AssignableToTypeOf(&gardencorev1beta1.CloudProfile{})).DoAndReturn(func(_ context.Context, _ client.ObjectKey, actual *gardencorev1beta1.CloudProfile) error {
				*actual = cloudProfile
				return nil
			})
			reader.EXPECT().List(ctx, gomock.AssignableToTypeOf(&gardencorev1beta1.SeedList{})).DoAndReturn(func(_ context.Context, actual *gardencorev1beta1.SeedList, _ ...client.ListOption) error {
				*actual = gardencorev1beta1.SeedList{Items: []gardencorev1beta1.Seed{seed}}
				return nil
			})
			reader.EXPECT().List(ctx, gomock.AssignableToTypeOf(&gardencorev1beta1.ShootList{}))

			bestSeed, err := determineSeed(ctx, reader, &shoot, schedulerConfiguration.Schedulers.Shoot.Strategy)
			Expect(err).NotTo(HaveOccurred())
			Expect(bestSeed.Name).To(Equal(seedName))
			// verify that shoot is in another region than the seed
			Expect(shoot.Spec.Region).NotTo(Equal(bestSeed.Spec.Provider.Region))
		})

		It("should find a seed cluster from other region: shoot in non-existing region, multiple seeds existing", func() {
			// add 3 seeds with different names and regions
			seed.Spec.Provider.Region = "europe-north1"

			secondSeed := seedBase
			secondSeed.Name = "seed-2"
			secondSeed.Spec.Provider.Region = "europe-west1"

			thirdSeed := seedBase
			thirdSeed.Name = "seed-3"
			thirdSeed.Spec.Provider.Region = "asia-south1"

			// define shoot to be lexicographically 'closer' to the second seed
			anotherRegion := "europe-west3"
			shoot.Spec.Region = anotherRegion

			reader.EXPECT().Get(ctx, kutil.Key(cloudProfile.Name), gomock.AssignableToTypeOf(&gardencorev1beta1.CloudProfile{})).DoAndReturn(func(_ context.Context, _ client.ObjectKey, actual *gardencorev1beta1.CloudProfile) error {
				*actual = cloudProfile
				return nil
			})
			reader.EXPECT().List(ctx, gomock.AssignableToTypeOf(&gardencorev1beta1.SeedList{})).DoAndReturn(func(_ context.Context, actual *gardencorev1beta1.SeedList, _ ...client.ListOption) error {
				*actual = gardencorev1beta1.SeedList{Items: []gardencorev1beta1.Seed{seed, secondSeed, thirdSeed}}
				return nil
			})
			reader.EXPECT().List(ctx, gomock.AssignableToTypeOf(&gardencorev1beta1.ShootList{}))

			bestSeed, err := determineSeed(ctx, reader, &shoot, schedulerConfiguration.Schedulers.Shoot.Strategy)
			Expect(err).NotTo(HaveOccurred())
			Expect(bestSeed.Name).To(Equal(secondSeed.Name))
			// verify that shoot is in another region than the chosen seed
			Expect(shoot.Spec.Region).NotTo(Equal(bestSeed.Spec.Provider.Region))
		})

		It("should pick candidate with least shoots deployed", func() {
			secondSeed := seedBase
			secondSeed.Name = "seed-2"

			secondShoot := shootBase
			secondShoot.Name = "shoot-2"
			// first seed references more shoots then seed-2 -> expect seed-2 to be selected
			secondShoot.Spec.SeedName = &seed.Name

			reader.EXPECT().Get(ctx, kutil.Key(cloudProfile.Name), gomock.AssignableToTypeOf(&gardencorev1beta1.CloudProfile{})).DoAndReturn(func(_ context.Context, _ client.ObjectKey, actual *gardencorev1beta1.CloudProfile) error {
				*actual = cloudProfile
				return nil
			})
			reader.EXPECT().List(ctx, gomock.AssignableToTypeOf(&gardencorev1beta1.SeedList{})).DoAndReturn(func(_ context.Context, actual *gardencorev1beta1.SeedList, _ ...client.ListOption) error {
				*actual = gardencorev1beta1.SeedList{Items: []gardencorev1beta1.Seed{seed, secondSeed}}
				return nil
			})
			reader.EXPECT().List(ctx, gomock.AssignableToTypeOf(&gardencorev1beta1.ShootList{})).DoAndReturn(func(_ context.Context, actual *gardencorev1beta1.ShootList, _ ...client.ListOption) error {
				*actual = gardencorev1beta1.ShootList{Items: []gardencorev1beta1.Shoot{shoot, secondShoot}}
				return nil
			})

			bestSeed, err := determineSeed(ctx, reader, &shoot, schedulerConfiguration.Schedulers.Shoot.Strategy)
			Expect(err).NotTo(HaveOccurred())
			Expect(bestSeed.Name).To(Equal(secondSeed.Name))
		})

		It("should find seed cluster that matches the seed selector of the CloudProfile and is from another region", func() {
			newCloudProfile := cloudProfile
			newCloudProfile.Name = "cloudprofile2"
			newCloudProfile.Spec.SeedSelector = &gardencorev1beta1.SeedSelector{
				LabelSelector: metav1.LabelSelector{
					MatchLabels: map[string]string{
						"environment": "two",
					},
				},
			}
			newCloudProfile.Spec.Regions = []gardencorev1beta1.Region{{Name: "name: eu-nl-1"}}

			// seeds
			oldSeedEnvironment1 := seed
			oldSeedEnvironment1.Spec.Provider.Type = "some-type"
			oldSeedEnvironment1.Spec.Provider.Region = "eu-de-200"
			oldSeedEnvironment1.Name = "seed1"
			oldSeedEnvironment1.Labels = map[string]string{"environment": "one"}

			newSeedEnvironment2 := seed
			newSeedEnvironment2.Spec.Provider.Type = "some-type"
			newSeedEnvironment2.Spec.Provider.Region = "eu-nl-1"
			newSeedEnvironment2.Name = "seed2"
			newSeedEnvironment2.Labels = map[string]string{"environment": "two"}

			// shoot
			testShoot := shoot
			testShoot.Spec.Region = "eu-de-2"
			testShoot.Spec.CloudProfileName = "cloudprofile2"
			testShoot.Spec.Provider.Type = "some-type"

			reader.EXPECT().Get(ctx, kutil.Key(newCloudProfile.Name), gomock.AssignableToTypeOf(&gardencorev1beta1.CloudProfile{})).DoAndReturn(func(_ context.Context, _ client.ObjectKey, actual *gardencorev1beta1.CloudProfile) error {
				*actual = newCloudProfile
				return nil
			})
			reader.EXPECT().List(ctx, gomock.AssignableToTypeOf(&gardencorev1beta1.SeedList{})).DoAndReturn(func(_ context.Context, actual *gardencorev1beta1.SeedList, _ ...client.ListOption) error {
				*actual = gardencorev1beta1.SeedList{Items: []gardencorev1beta1.Seed{oldSeedEnvironment1, newSeedEnvironment2}}
				return nil
			})
			reader.EXPECT().List(ctx, gomock.AssignableToTypeOf(&gardencorev1beta1.ShootList{}))

			bestSeed, err := determineSeed(ctx, reader, &testShoot, schedulerConfiguration.Schedulers.Shoot.Strategy)
			Expect(err).NotTo(HaveOccurred())
			Expect(bestSeed.Name).To(Equal(newSeedEnvironment2.Name))
		})

		It("should find seed cluster that matches the seed selector of the Shoot and is from another region", func() {
			newCloudProfile := cloudProfile
			newCloudProfile.Name = "cloudprofile2"
			newCloudProfile.Spec.SeedSelector = &gardencorev1beta1.SeedSelector{
				LabelSelector: metav1.LabelSelector{
					MatchLabels: map[string]string{
						"environment": "two",
					},
				},
			}
			newCloudProfile.Spec.Regions = []gardencorev1beta1.Region{{Name: "name: eu-nl-1"}}

			// seeds
			oldSeedEnvironment1 := seed
			oldSeedEnvironment1.Spec.Provider.Type = "some-type"
			oldSeedEnvironment1.Spec.Provider.Region = "eu-de-200"
			oldSeedEnvironment1.Name = "seed1"
			oldSeedEnvironment1.Labels = map[string]string{"environment": "one"}

			newSeedEnvironment2 := seed
			newSeedEnvironment2.Spec.Provider.Type = "some-type"
			newSeedEnvironment2.Spec.Provider.Region = "eu-nl-1"
			newSeedEnvironment2.Name = "seed2"
			newSeedEnvironment2.Labels = map[string]string{"environment": "two"}

			newSeedEnvironment3 := seed
			newSeedEnvironment3.Spec.Provider.Type = "some-type"
			newSeedEnvironment3.Spec.Provider.Region = "eu-nl-4"
			newSeedEnvironment3.Name = "seed3"
			newSeedEnvironment3.Labels = map[string]string{"environment": "two", "my-preferred": "seed"}

			// shoot
			testShoot := shoot
			testShoot.Spec.Region = "eu-de-2"
			testShoot.Spec.CloudProfileName = "cloudprofile2"
			testShoot.Spec.Provider.Type = "some-type"
			testShoot.Spec.SeedSelector = &gardencorev1beta1.SeedSelector{
				LabelSelector: metav1.LabelSelector{MatchLabels: map[string]string{"my-preferred": "seed"}},
			}

			reader.EXPECT().Get(ctx, kutil.Key(newCloudProfile.Name), gomock.AssignableToTypeOf(&gardencorev1beta1.CloudProfile{})).DoAndReturn(func(_ context.Context, _ client.ObjectKey, actual *gardencorev1beta1.CloudProfile) error {
				*actual = newCloudProfile
				return nil
			})
			reader.EXPECT().List(ctx, gomock.AssignableToTypeOf(&gardencorev1beta1.SeedList{})).DoAndReturn(func(_ context.Context, actual *gardencorev1beta1.SeedList, _ ...client.ListOption) error {
				*actual = gardencorev1beta1.SeedList{Items: []gardencorev1beta1.Seed{oldSeedEnvironment1, newSeedEnvironment2, newSeedEnvironment3}}
				return nil
			})
			reader.EXPECT().List(ctx, gomock.AssignableToTypeOf(&gardencorev1beta1.ShootList{}))

			bestSeed, err := determineSeed(ctx, reader, &testShoot, schedulerConfiguration.Schedulers.Shoot.Strategy)
			Expect(err).NotTo(HaveOccurred())
			Expect(bestSeed.Name).To(Equal(newSeedEnvironment3.Name))
		})

		It("should find seed cluster with enough available capacity for shoots", func() {
			seed.Status.Allocatable = corev1.ResourceList{
				gardencorev1beta1.ResourceShoots: resource.MustParse("1"),
			}

			secondSeed := seedBase
			secondSeed.Name = "seed-2"
			secondSeed.Status.Allocatable = corev1.ResourceList{
				gardencorev1beta1.ResourceShoots: resource.MustParse("2"),
			}

			secondShoot := shootBase
			secondShoot.Name = "shoot-2"
			secondShoot.Spec.SeedName = &seed.Name

			thirdShoot := shootBase
			thirdShoot.Name = "shoot-3"
			thirdShoot.Spec.SeedName = &secondSeed.Name

			reader.EXPECT().Get(ctx, kutil.Key(cloudProfile.Name), gomock.AssignableToTypeOf(&gardencorev1beta1.CloudProfile{})).DoAndReturn(func(_ context.Context, _ client.ObjectKey, actual *gardencorev1beta1.CloudProfile) error {
				*actual = cloudProfile
				return nil
			})
			reader.EXPECT().List(ctx, gomock.AssignableToTypeOf(&gardencorev1beta1.SeedList{})).DoAndReturn(func(_ context.Context, actual *gardencorev1beta1.SeedList, _ ...client.ListOption) error {
				*actual = gardencorev1beta1.SeedList{Items: []gardencorev1beta1.Seed{seed, secondSeed}}
				return nil
			})
			reader.EXPECT().List(ctx, gomock.AssignableToTypeOf(&gardencorev1beta1.ShootList{})).DoAndReturn(func(_ context.Context, actual *gardencorev1beta1.ShootList, _ ...client.ListOption) error {
				*actual = gardencorev1beta1.ShootList{Items: []gardencorev1beta1.Shoot{shoot, secondShoot, thirdShoot}}
				return nil
			})

			bestSeed, err := determineSeed(ctx, reader, &shoot, schedulerConfiguration.Schedulers.Shoot.Strategy)
			Expect(err).NotTo(HaveOccurred())
			Expect(bestSeed.Name).To(Equal(secondSeed.Name))
		})
	})

	Context("SEED DETERMINATION - Shoot does not reference a Seed - find an adequate one using default seed determination strategy", func() {
		BeforeEach(func() {
			cloudProfile = *cloudProfileBase.DeepCopy()
			seed = *seedBase.DeepCopy()
			shoot = *shootBase.DeepCopy()
			schedulerConfiguration = *schedulerConfigurationBase.DeepCopy()
			// no seed referenced
			shoot.Spec.SeedName = nil
			schedulerConfiguration.Schedulers.Shoot.Strategy = config.Default
		})

		It("should find a seed cluster 1) referencing the same profile 2) same region 3) indicating availability", func() {
			reader.EXPECT().Get(ctx, kutil.Key(cloudProfile.Name), gomock.AssignableToTypeOf(&gardencorev1beta1.CloudProfile{})).DoAndReturn(func(_ context.Context, _ client.ObjectKey, actual *gardencorev1beta1.CloudProfile) error {
				*actual = cloudProfile
				return nil
			})
			reader.EXPECT().List(ctx, gomock.AssignableToTypeOf(&gardencorev1beta1.SeedList{})).DoAndReturn(func(_ context.Context, actual *gardencorev1beta1.SeedList, _ ...client.ListOption) error {
				*actual = gardencorev1beta1.SeedList{Items: []gardencorev1beta1.Seed{seed}}
				return nil
			})
			reader.EXPECT().List(ctx, gomock.AssignableToTypeOf(&gardencorev1beta1.ShootList{}))

			bestSeed, err := determineSeed(ctx, reader, &shoot, schedulerConfiguration.Schedulers.Shoot.Strategy)
			Expect(err).NotTo(HaveOccurred())
			Expect(bestSeed.Name).To(Equal(seedName))
		})

		It("should find a seed cluster 1) referencing the same profile 2) same region 3) indicating availability 4) using shoot default networks", func() {
			seed.Spec.Networks.ShootDefaults = &gardencorev1beta1.ShootNetworks{
				Pods:     pointer.String("10.50.0.0/16"),
				Services: pointer.String("10.60.0.0/16"),
			}
			shoot.Spec.Networking.Pods = nil
			shoot.Spec.Networking.Services = nil

			reader.EXPECT().Get(ctx, kutil.Key(cloudProfile.Name), gomock.AssignableToTypeOf(&gardencorev1beta1.CloudProfile{})).DoAndReturn(func(_ context.Context, _ client.ObjectKey, actual *gardencorev1beta1.CloudProfile) error {
				*actual = cloudProfile
				return nil
			})
			reader.EXPECT().List(ctx, gomock.AssignableToTypeOf(&gardencorev1beta1.SeedList{})).DoAndReturn(func(_ context.Context, actual *gardencorev1beta1.SeedList, _ ...client.ListOption) error {
				*actual = gardencorev1beta1.SeedList{Items: []gardencorev1beta1.Seed{seed}}
				return nil
			})
			reader.EXPECT().List(ctx, gomock.AssignableToTypeOf(&gardencorev1beta1.ShootList{}))

			bestSeed, err := determineSeed(ctx, reader, &shoot, schedulerConfiguration.Schedulers.Shoot.Strategy)
			Expect(err).NotTo(HaveOccurred())
			Expect(bestSeed.Name).To(Equal(seedName))
		})

		It("should find the best seed cluster 1) referencing the same profile 2) same region 3) indicating availability", func() {
			secondSeed := seedBase
			secondSeed.Name = "seed-2"

			secondShoot := shootBase
			secondShoot.Name = "shoot-2"
			secondShoot.Spec.SeedName = &seed.Name

			reader.EXPECT().Get(ctx, kutil.Key(cloudProfile.Name), gomock.AssignableToTypeOf(&gardencorev1beta1.CloudProfile{})).DoAndReturn(func(_ context.Context, _ client.ObjectKey, actual *gardencorev1beta1.CloudProfile) error {
				*actual = cloudProfile
				return nil
			})
			reader.EXPECT().List(ctx, gomock.AssignableToTypeOf(&gardencorev1beta1.SeedList{})).DoAndReturn(func(_ context.Context, actual *gardencorev1beta1.SeedList, _ ...client.ListOption) error {
				*actual = gardencorev1beta1.SeedList{Items: []gardencorev1beta1.Seed{seed, secondSeed}}
				return nil
			})
			reader.EXPECT().List(ctx, gomock.AssignableToTypeOf(&gardencorev1beta1.ShootList{})).DoAndReturn(func(_ context.Context, actual *gardencorev1beta1.ShootList, _ ...client.ListOption) error {
				*actual = gardencorev1beta1.ShootList{Items: []gardencorev1beta1.Shoot{shoot, secondShoot}}
				return nil
			})

			bestSeed, err := determineSeed(ctx, reader, &shoot, schedulerConfiguration.Schedulers.Shoot.Strategy)
			Expect(err).NotTo(HaveOccurred())
			Expect(bestSeed.Name).To(Equal(secondSeed.Name))
		})

		// FAIL

		It("should fail because it cannot find a seed cluster due to network disjointedness", func() {
			shoot.Spec.Networking = gardencorev1beta1.Networking{
				Pods:     &seed.Spec.Networks.Pods,
				Services: &seed.Spec.Networks.Services,
				Nodes:    seed.Spec.Networks.Nodes,
			}

			reader.EXPECT().Get(ctx, kutil.Key(cloudProfile.Name), gomock.AssignableToTypeOf(&gardencorev1beta1.CloudProfile{})).DoAndReturn(func(_ context.Context, _ client.ObjectKey, actual *gardencorev1beta1.CloudProfile) error {
				*actual = cloudProfile
				return nil
			})
			reader.EXPECT().List(ctx, gomock.AssignableToTypeOf(&gardencorev1beta1.SeedList{})).DoAndReturn(func(_ context.Context, actual *gardencorev1beta1.SeedList, _ ...client.ListOption) error {
				*actual = gardencorev1beta1.SeedList{Items: []gardencorev1beta1.Seed{seed}}
				return nil
			})
			reader.EXPECT().List(ctx, gomock.AssignableToTypeOf(&gardencorev1beta1.ShootList{}))

			bestSeed, err := determineSeed(ctx, reader, &shoot, schedulerConfiguration.Schedulers.Shoot.Strategy)
			Expect(err).To(HaveOccurred())
			Expect(bestSeed).To(BeNil())
		})

		It("should fail because it cannot find a seed cluster due to non-tolerated taints", func() {
			seed.Spec.Taints = []gardencorev1beta1.SeedTaint{{Key: "foo"}}
			shoot.Spec.Tolerations = nil

			reader.EXPECT().Get(ctx, kutil.Key(cloudProfile.Name), gomock.AssignableToTypeOf(&gardencorev1beta1.CloudProfile{})).DoAndReturn(func(_ context.Context, _ client.ObjectKey, actual *gardencorev1beta1.CloudProfile) error {
				*actual = cloudProfile
				return nil
			})
			reader.EXPECT().List(ctx, gomock.AssignableToTypeOf(&gardencorev1beta1.SeedList{})).DoAndReturn(func(_ context.Context, actual *gardencorev1beta1.SeedList, _ ...client.ListOption) error {
				*actual = gardencorev1beta1.SeedList{Items: []gardencorev1beta1.Seed{seed}}
				return nil
			})
			reader.EXPECT().List(ctx, gomock.AssignableToTypeOf(&gardencorev1beta1.ShootList{}))

			bestSeed, err := determineSeed(ctx, reader, &shoot, schedulerConfiguration.Schedulers.Shoot.Strategy)
			Expect(err).To(HaveOccurred())
			Expect(bestSeed).To(BeNil())
		})

		It("should fail because it cannot find a seed cluster due to no available capacity for shoots", func() {
			seed.Status.Allocatable = corev1.ResourceList{
				gardencorev1beta1.ResourceShoots: resource.MustParse("1"),
			}
			secondShoot := shootBase
			secondShoot.Name = "shoot-2"
			secondShoot.Spec.SeedName = &seed.Name

			reader.EXPECT().Get(ctx, kutil.Key(cloudProfile.Name), gomock.AssignableToTypeOf(&gardencorev1beta1.CloudProfile{})).DoAndReturn(func(_ context.Context, _ client.ObjectKey, actual *gardencorev1beta1.CloudProfile) error {
				*actual = cloudProfile
				return nil
			})
			reader.EXPECT().List(ctx, gomock.AssignableToTypeOf(&gardencorev1beta1.SeedList{})).DoAndReturn(func(_ context.Context, actual *gardencorev1beta1.SeedList, _ ...client.ListOption) error {
				*actual = gardencorev1beta1.SeedList{Items: []gardencorev1beta1.Seed{seed}}
				return nil
			})
			reader.EXPECT().List(ctx, gomock.AssignableToTypeOf(&gardencorev1beta1.ShootList{})).DoAndReturn(func(_ context.Context, actual *gardencorev1beta1.ShootList, _ ...client.ListOption) error {
				*actual = gardencorev1beta1.ShootList{Items: []gardencorev1beta1.Shoot{shoot, secondShoot}}
				return nil
			})

			bestSeed, err := determineSeed(ctx, reader, &shoot, schedulerConfiguration.Schedulers.Shoot.Strategy)
			Expect(err).To(HaveOccurred())
			Expect(bestSeed).To(BeNil())
		})

		It("should fail because it cannot find a seed cluster due to no shoot networks specified and no defaults", func() {
			seed.Spec.Networks.ShootDefaults = nil
			shoot.Spec.Networking = gardencorev1beta1.Networking{}

			reader.EXPECT().Get(ctx, kutil.Key(cloudProfile.Name), gomock.AssignableToTypeOf(&gardencorev1beta1.CloudProfile{})).DoAndReturn(func(_ context.Context, _ client.ObjectKey, actual *gardencorev1beta1.CloudProfile) error {
				*actual = cloudProfile
				return nil
			})
			reader.EXPECT().List(ctx, gomock.AssignableToTypeOf(&gardencorev1beta1.SeedList{})).DoAndReturn(func(_ context.Context, actual *gardencorev1beta1.SeedList, _ ...client.ListOption) error {
				*actual = gardencorev1beta1.SeedList{Items: []gardencorev1beta1.Seed{seed}}
				return nil
			})
			reader.EXPECT().List(ctx, gomock.AssignableToTypeOf(&gardencorev1beta1.ShootList{}))

			bestSeed, err := determineSeed(ctx, reader, &shoot, schedulerConfiguration.Schedulers.Shoot.Strategy)
			Expect(err).To(HaveOccurred())
			Expect(bestSeed).To(BeNil())
		})

		It("should fail because it cannot find a seed cluster due to region that no seed supports", func() {
			shoot.Spec.Region = "another-region"

			reader.EXPECT().Get(ctx, kutil.Key(cloudProfile.Name), gomock.AssignableToTypeOf(&gardencorev1beta1.CloudProfile{})).DoAndReturn(func(_ context.Context, _ client.ObjectKey, actual *gardencorev1beta1.CloudProfile) error {
				*actual = cloudProfile
				return nil
			})
			reader.EXPECT().List(ctx, gomock.AssignableToTypeOf(&gardencorev1beta1.SeedList{})).DoAndReturn(func(_ context.Context, actual *gardencorev1beta1.SeedList, _ ...client.ListOption) error {
				*actual = gardencorev1beta1.SeedList{Items: []gardencorev1beta1.Seed{seed}}
				return nil
			})
			reader.EXPECT().List(ctx, gomock.AssignableToTypeOf(&gardencorev1beta1.ShootList{}))

			bestSeed, err := determineSeed(ctx, reader, &shoot, schedulerConfiguration.Schedulers.Shoot.Strategy)
			Expect(err).To(HaveOccurred())
			Expect(bestSeed).To(BeNil())
		})

		It("should fail because the cloudprofile used by the shoot doesn't select any seed candidate", func() {
			cloudProfile.Spec.SeedSelector = &gardencorev1beta1.SeedSelector{
				LabelSelector: metav1.LabelSelector{
					MatchLabels: map[string]string{
						"foo": "bar",
					},
				},
			}
			reader.EXPECT().Get(ctx, kutil.Key(cloudProfile.Name), gomock.AssignableToTypeOf(&gardencorev1beta1.CloudProfile{})).DoAndReturn(func(_ context.Context, _ client.ObjectKey, actual *gardencorev1beta1.CloudProfile) error {
				*actual = cloudProfile
				return nil
			})
			reader.EXPECT().List(ctx, gomock.AssignableToTypeOf(&gardencorev1beta1.SeedList{})).DoAndReturn(func(_ context.Context, actual *gardencorev1beta1.SeedList, _ ...client.ListOption) error {
				*actual = gardencorev1beta1.SeedList{Items: []gardencorev1beta1.Seed{seed}}
				return nil
			})
			reader.EXPECT().List(ctx, gomock.AssignableToTypeOf(&gardencorev1beta1.ShootList{}))

			bestSeed, err := determineSeed(ctx, reader, &shoot, schedulerConfiguration.Schedulers.Shoot.Strategy)
			Expect(err).To(HaveOccurred())
			Expect(bestSeed).To(BeNil())
		})

		It("should fail because the shoot doesn't select any seed candidate", func() {
			shoot.Spec.SeedSelector = &gardencorev1beta1.SeedSelector{
				LabelSelector: metav1.LabelSelector{
					MatchLabels: map[string]string{
						"foo": "bar",
					},
				},
			}

			reader.EXPECT().Get(ctx, kutil.Key(cloudProfile.Name), gomock.AssignableToTypeOf(&gardencorev1beta1.CloudProfile{})).DoAndReturn(func(_ context.Context, _ client.ObjectKey, actual *gardencorev1beta1.CloudProfile) error {
				*actual = cloudProfile
				return nil
			})
			reader.EXPECT().List(ctx, gomock.AssignableToTypeOf(&gardencorev1beta1.SeedList{})).DoAndReturn(func(_ context.Context, actual *gardencorev1beta1.SeedList, _ ...client.ListOption) error {
				*actual = gardencorev1beta1.SeedList{Items: []gardencorev1beta1.Seed{seed}}
				return nil
			})
			reader.EXPECT().List(ctx, gomock.AssignableToTypeOf(&gardencorev1beta1.ShootList{}))

			bestSeed, err := determineSeed(ctx, reader, &shoot, schedulerConfiguration.Schedulers.Shoot.Strategy)
			Expect(err).To(HaveOccurred())
			Expect(bestSeed).To(BeNil())
		})

		It("should fail because it cannot find a seed cluster due to invalid profile", func() {
			shoot.Spec.CloudProfileName = "another-profile"

			reader.EXPECT().Get(ctx, kutil.Key("another-profile"), gomock.AssignableToTypeOf(&gardencorev1beta1.CloudProfile{})).Return(apierrors.NewNotFound(schema.GroupResource{}, ""))
			reader.EXPECT().List(ctx, gomock.AssignableToTypeOf(&gardencorev1beta1.SeedList{})).DoAndReturn(func(_ context.Context, actual *gardencorev1beta1.SeedList, _ ...client.ListOption) error {
				*actual = gardencorev1beta1.SeedList{Items: []gardencorev1beta1.Seed{seed}}
				return nil
			})
			reader.EXPECT().List(ctx, gomock.AssignableToTypeOf(&gardencorev1beta1.ShootList{}))

			bestSeed, err := determineSeed(ctx, reader, &shoot, schedulerConfiguration.Schedulers.Shoot.Strategy)
			Expect(err).To(HaveOccurred())
			Expect(bestSeed).To(BeNil())
		})

		It("should fail because it cannot find a seed cluster due to gardenlet not ready", func() {
			seed.Status.Conditions = []gardencorev1beta1.Condition{
				{
					Type:   gardencorev1beta1.SeedGardenletReady,
					Status: gardencorev1beta1.ConditionFalse,
				},
				{
					Type:   gardencorev1beta1.SeedBootstrapped,
					Status: gardencorev1beta1.ConditionTrue,
				},
			}

			reader.EXPECT().Get(ctx, kutil.Key(cloudProfile.Name), gomock.AssignableToTypeOf(&gardencorev1beta1.CloudProfile{})).DoAndReturn(func(_ context.Context, _ client.ObjectKey, actual *gardencorev1beta1.CloudProfile) error {
				*actual = cloudProfile
				return nil
			})
			reader.EXPECT().List(ctx, gomock.AssignableToTypeOf(&gardencorev1beta1.SeedList{})).DoAndReturn(func(_ context.Context, actual *gardencorev1beta1.SeedList, _ ...client.ListOption) error {
				*actual = gardencorev1beta1.SeedList{Items: []gardencorev1beta1.Seed{seed}}
				return nil
			})
			reader.EXPECT().List(ctx, gomock.AssignableToTypeOf(&gardencorev1beta1.ShootList{}))

			bestSeed, err := determineSeed(ctx, reader, &shoot, schedulerConfiguration.Schedulers.Shoot.Strategy)
			Expect(err).To(HaveOccurred())
			Expect(bestSeed).To(BeNil())
		})

		It("should fail because it cannot find a seed cluster due to not bootstrapped", func() {
			seed.Status.Conditions = []gardencorev1beta1.Condition{
				{
					Type:   gardencorev1beta1.SeedGardenletReady,
					Status: gardencorev1beta1.ConditionTrue,
				},
				{
					Type:   gardencorev1beta1.SeedBootstrapped,
					Status: gardencorev1beta1.ConditionFalse,
				},
			}

			reader.EXPECT().Get(ctx, kutil.Key(cloudProfile.Name), gomock.AssignableToTypeOf(&gardencorev1beta1.CloudProfile{})).DoAndReturn(func(_ context.Context, _ client.ObjectKey, actual *gardencorev1beta1.CloudProfile) error {
				*actual = cloudProfile
				return nil
			})
			reader.EXPECT().List(ctx, gomock.AssignableToTypeOf(&gardencorev1beta1.SeedList{})).DoAndReturn(func(_ context.Context, actual *gardencorev1beta1.SeedList, _ ...client.ListOption) error {
				*actual = gardencorev1beta1.SeedList{Items: []gardencorev1beta1.Seed{seed}}
				return nil
			})
			reader.EXPECT().List(ctx, gomock.AssignableToTypeOf(&gardencorev1beta1.ShootList{}))

			bestSeed, err := determineSeed(ctx, reader, &shoot, schedulerConfiguration.Schedulers.Shoot.Strategy)
			Expect(err).To(HaveOccurred())
			Expect(bestSeed).To(BeNil())
		})

		It("should fail because it cannot find a seed cluster due to invisibility", func() {
			seed.Spec.Settings = &gardencorev1beta1.SeedSettings{
				Scheduling: &gardencorev1beta1.SeedSettingScheduling{
					Visible: false,
				},
			}

			reader.EXPECT().Get(ctx, kutil.Key(cloudProfile.Name), gomock.AssignableToTypeOf(&gardencorev1beta1.CloudProfile{})).DoAndReturn(func(_ context.Context, _ client.ObjectKey, actual *gardencorev1beta1.CloudProfile) error {
				*actual = cloudProfile
				return nil
			})
			reader.EXPECT().List(ctx, gomock.AssignableToTypeOf(&gardencorev1beta1.SeedList{})).DoAndReturn(func(_ context.Context, actual *gardencorev1beta1.SeedList, _ ...client.ListOption) error {
				*actual = gardencorev1beta1.SeedList{Items: []gardencorev1beta1.Seed{seed}}
				return nil
			})
			reader.EXPECT().List(ctx, gomock.AssignableToTypeOf(&gardencorev1beta1.ShootList{}))

			bestSeed, err := determineSeed(ctx, reader, &shoot, schedulerConfiguration.Schedulers.Shoot.Strategy)
			Expect(err).To(HaveOccurred())
			Expect(bestSeed).To(BeNil())
		})
	})

	Context("#DetermineBestSeedCandidate", func() {
		BeforeEach(func() {
			seed = *seedBase.DeepCopy()
			shoot = *shootBase.DeepCopy()
			schedulerConfiguration = *schedulerConfigurationBase.DeepCopy()
			// no seed referenced
			shoot.Spec.SeedName = nil
			schedulerConfiguration.Schedulers.Shoot.Strategy = config.MinimalDistance
		})

		It("should find two seeds candidates having the same amount of matching characters", func() {
			oldSeedEnvironment1 := seed
			oldSeedEnvironment1.Spec.Provider.Type = "some-type"
			oldSeedEnvironment1.Spec.Provider.Region = "eu-de-200"
			oldSeedEnvironment1.Name = "seed1"

			newSeedEnvironment2 := seed
			newSeedEnvironment2.Spec.Provider.Type = "some-type"
			newSeedEnvironment2.Spec.Provider.Region = "eu-de-2111"
			newSeedEnvironment2.Name = "seed2"

			otherSeedEnvironment2 := seed
			otherSeedEnvironment2.Spec.Provider.Type = "some-type"
			otherSeedEnvironment2.Spec.Provider.Region = "eu-nl-1"
			otherSeedEnvironment2.Name = "xyz"

			// shoot
			testShoot := shoot
			testShoot.Spec.Region = "eu-de-2xzxzzx"
			testShoot.Spec.CloudProfileName = "cloudprofile2"
			testShoot.Spec.Provider.Type = "some-type"

			candidates, err := applyStrategy(&testShoot, []gardencorev1beta1.Seed{newSeedEnvironment2, oldSeedEnvironment1, otherSeedEnvironment2}, schedulerConfiguration.Schedulers.Shoot.Strategy)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(candidates)).To(Equal(2))
			Expect(candidates[0].Name).To(Equal(newSeedEnvironment2.Name))
			Expect(candidates[1].Name).To(Equal(oldSeedEnvironment1.Name))
		})

		It("should find single seed candidate", func() {
			oldSeedEnvironment1 := seed
			oldSeedEnvironment1.Spec.Provider.Type = "some-type"
			oldSeedEnvironment1.Spec.Provider.Region = "eu-de-200"
			oldSeedEnvironment1.Name = "seed1"

			newSeedEnvironment2 := seed
			newSeedEnvironment2.Spec.Provider.Type = "some-type"
			newSeedEnvironment2.Spec.Provider.Region = "eu-de-2111"
			newSeedEnvironment2.Name = "seed2"

			otherSeedEnvironment2 := seed
			otherSeedEnvironment2.Spec.Provider.Type = "some-type"
			otherSeedEnvironment2.Spec.Provider.Region = "eu-nl-1"
			otherSeedEnvironment2.Name = "xyz"

			// shoot
			testShoot := shoot
			testShoot.Spec.Region = "eu-de-20"
			testShoot.Spec.CloudProfileName = "cloudprofile2"
			testShoot.Spec.Provider.Type = "some-type"

			candidates, err := applyStrategy(&testShoot, []gardencorev1beta1.Seed{newSeedEnvironment2, oldSeedEnvironment1, otherSeedEnvironment2}, schedulerConfiguration.Schedulers.Shoot.Strategy)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(candidates)).To(Equal(1))
			Expect(candidates[0].Name).To(Equal(oldSeedEnvironment1.Name))
		})
	})
})
