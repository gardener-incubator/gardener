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

	"github.com/gardener/gardener/pkg/apis/core"
	gardencoreinstall "github.com/gardener/gardener/pkg/apis/core/install"
	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/gardener/gardener/pkg/controllerutils"
	"github.com/gardener/gardener/pkg/operation/common"
	kutil "github.com/gardener/gardener/pkg/utils/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var gardenScheme *runtime.Scheme

func init() {
	gardenScheme = runtime.NewScheme()
	gardencoreinstall.Install(gardenScheme)
}

// SyncClusterResourceToSeed creates or updates the `extensions.gardener.cloud/v1alpha1.Cluster` resource in the seed
// cluster by adding the shoot, seed, and cloudprofile specification.
func SyncClusterResourceToSeed(
	ctx context.Context,
	c client.Client,
	clusterName string,
	shoot *gardencorev1beta1.Shoot,
	cloudProfile *gardencorev1beta1.CloudProfile,
	seed *gardencorev1beta1.Seed,
) error {
	if shoot.Spec.SeedName == nil {
		return nil
	}

	var (
		cluster = &extensionsv1alpha1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterName,
			},
		}

		cloudProfileObj *gardencorev1beta1.CloudProfile
		seedObj         *gardencorev1beta1.Seed
		shootObj        *gardencorev1beta1.Shoot
	)

	if cloudProfile != nil {
		cloudProfileObj = cloudProfile.DeepCopy()
		cloudProfileObj.TypeMeta = metav1.TypeMeta{
			APIVersion: gardencorev1beta1.SchemeGroupVersion.String(),
			Kind:       "CloudProfile",
		}
		cloudProfileObj.ManagedFields = nil
	}

	if seed != nil {
		seedObj = seed.DeepCopy()
		seedObj.TypeMeta = metav1.TypeMeta{
			APIVersion: gardencorev1beta1.SchemeGroupVersion.String(),
			Kind:       "Seed",
		}
		seedObj.ManagedFields = nil
	}

	if shoot != nil {
		shootObj = shoot.DeepCopy()
		shootObj.TypeMeta = metav1.TypeMeta{
			APIVersion: gardencorev1beta1.SchemeGroupVersion.String(),
			Kind:       "Shoot",
		}
		shootObj.ManagedFields = nil
	}

	_, err := controllerutils.GetAndCreateOrMergePatch(ctx, c, cluster, func() error {
		if cloudProfileObj != nil {
			cluster.Spec.CloudProfile = runtime.RawExtension{Object: cloudProfileObj}
		}
		if seedObj != nil {
			cluster.Spec.Seed = runtime.RawExtension{Object: seedObj}
		}
		if shootObj != nil {
			cluster.Spec.Shoot = runtime.RawExtension{Object: shootObj}
		}
		return nil
	})
	return err
}

// Cluster contains the decoded resources of Gardener's extension Cluster resource.
type Cluster struct {
	ObjectMeta   metav1.ObjectMeta
	CloudProfile *gardencorev1beta1.CloudProfile
	Seed         *gardencorev1beta1.Seed
	Shoot        *gardencorev1beta1.Shoot
}

// GetCluster tries to read Gardener's Cluster extension resource in the given namespace.
func GetCluster(ctx context.Context, c client.Client, namespace string) (*Cluster, error) {
	cluster := &extensionsv1alpha1.Cluster{}
	if err := c.Get(ctx, kutil.Key(namespace), cluster); err != nil {
		return nil, err
	}

	decoder := NewGardenDecoder()

	cloudProfile, err := CloudProfileFromCluster(decoder, cluster)
	if err != nil {
		return nil, err
	}
	seed, err := SeedFromCluster(decoder, cluster)
	if err != nil {
		return nil, err
	}
	shoot, err := ShootFromCluster(decoder, cluster)
	if err != nil {
		return nil, err
	}

	return &Cluster{cluster.ObjectMeta, cloudProfile, seed, shoot}, nil
}

// CloudProfileFromCluster returns the CloudProfile resource inside the Cluster resource.
func CloudProfileFromCluster(decoder runtime.Decoder, cluster *extensionsv1alpha1.Cluster) (*gardencorev1beta1.CloudProfile, error) {
	var (
		cloudProfileInternal = &core.CloudProfile{}
		cloudProfile         = &gardencorev1beta1.CloudProfile{}
	)

	if cluster.Spec.CloudProfile.Raw == nil {
		return nil, nil
	}
	if _, _, err := decoder.Decode(cluster.Spec.CloudProfile.Raw, nil, cloudProfileInternal); err != nil {
		return nil, err
	}
	if err := gardenScheme.Convert(cloudProfileInternal, cloudProfile, nil); err != nil {
		return nil, err
	}

	return cloudProfile, nil
}

// SeedFromCluster returns the Seed resource inside the Cluster resource.
func SeedFromCluster(decoder runtime.Decoder, cluster *extensionsv1alpha1.Cluster) (*gardencorev1beta1.Seed, error) {
	var (
		seedInternal = &core.Seed{}
		seed         = &gardencorev1beta1.Seed{}
	)

	if cluster.Spec.Seed.Raw == nil {
		return nil, nil
	}
	if _, _, err := decoder.Decode(cluster.Spec.Seed.Raw, nil, seedInternal); err != nil {
		return nil, err
	}
	if err := gardenScheme.Convert(seedInternal, seed, nil); err != nil {
		return nil, err
	}

	return seed, nil
}

// ShootFromCluster returns the Shoot resource inside the Cluster resource.
func ShootFromCluster(decoder runtime.Decoder, cluster *extensionsv1alpha1.Cluster) (*gardencorev1beta1.Shoot, error) {
	var (
		shootInternal = &core.Shoot{}
		shoot         = &gardencorev1beta1.Shoot{}
	)

	if cluster.Spec.Shoot.Raw == nil {
		return nil, nil
	}
	if _, _, err := decoder.Decode(cluster.Spec.Shoot.Raw, nil, shootInternal); err != nil {
		return nil, err
	}
	if err := gardenScheme.Convert(shootInternal, shoot, nil); err != nil {
		return nil, err
	}

	return shoot, nil
}

// GetShoot tries to read Gardener's Cluster extension resource in the given namespace and return the embedded Shoot resource.
func GetShoot(ctx context.Context, c client.Client, namespace string) (*gardencorev1beta1.Shoot, error) {
	cluster := &extensionsv1alpha1.Cluster{}
	if err := c.Get(ctx, kutil.Key(namespace), cluster); err != nil {
		return nil, err
	}

	return ShootFromCluster(NewGardenDecoder(), cluster)
}

// NewGardenDecoder returns a new Garden API decoder.
func NewGardenDecoder() runtime.Decoder {
	return serializer.NewCodecFactory(gardenScheme).UniversalDecoder()
}

// ClusterFromRequest returns the cluster resource for a given reconcile.Request
func ClusterFromRequest(ctx context.Context, seedClient client.Client, req reconcile.Request) (*extensionsv1alpha1.Cluster, error) {
	var clusterName string
	if req.Namespace == "" {
		// Handling for cluster-scoped backupentry extension resources.
		clusterName, _ = common.ExtractShootDetailsFromBackupEntryName(req.Name)
	} else {
		clusterName = req.Namespace
	}

	cluster := &extensionsv1alpha1.Cluster{}
	if err := seedClient.Get(ctx, kutil.Key(clusterName), cluster); err != nil {
		return nil, err
	}

	return cluster, nil
}
