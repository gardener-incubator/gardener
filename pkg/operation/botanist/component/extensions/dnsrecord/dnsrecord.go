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

package dnsrecord

import (
	"context"
	"time"

	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gardencorev1alpha1 "github.com/gardener/gardener/pkg/apis/core/v1alpha1"
	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/gardener/gardener/pkg/controllerutils"
	"github.com/gardener/gardener/pkg/extensions"
	"github.com/gardener/gardener/pkg/operation/botanist/component"
)

const (
	// DefaultInterval is the default interval for retry operations.
	DefaultInterval = 5 * time.Second
	// DefaultSevereThreshold is the default threshold until an error reported by another component is treated as
	// 'severe'.
	DefaultSevereThreshold = 15 * time.Second
	// DefaultTimeout is the default timeout and defines how long Gardener should wait for a successful reconciliation
	// of a DNSRecord resource.
	DefaultTimeout = 2 * time.Minute
)

// TimeNow returns the current time. Exposed for testing.
var TimeNow = time.Now

// Interface is an interface for managing DNSRecords
type Interface interface {
	component.DeployMigrateWaiter
	SetRecordType(extensionsv1alpha1.DNSRecordType)
	SetValues([]string)
}

// Values contains the values used to create DNSRecord resources.
type Values struct {
	// Namespace is the Shoot namespace in the seed.
	Namespace string
	// Name is the name of the DNSRecord resource. Commonly the Shoot's name + the purpose of the DNS record.
	Name string
	// SecretName is the name of the secret referenced by the DNSRecord resource.
	SecretName string
	// CreateOnly specifies that the DNSRecord resource should only be created, never updated.
	// This mode is used for owner DNS records.
	CreateOnly bool
	// Type is the type of the DNSRecord provider.
	Type string
	// SecretData is the secret data of the DNSRecord (containing provider credentials, etc.)
	SecretData map[string][]byte
	// Zone is the DNS hosted zone of the DNSRecord.
	Zone *string
	// DNSName is the fully qualified domain name of the DNSRecord.
	DNSName string
	// RecordType is the record type of the DNSRecord.
	RecordType extensionsv1alpha1.DNSRecordType
	// Values is the list of values of the DNSRecord.
	Values []string
	// TTL is the time to live in seconds of the DNSRecord.
	TTL *int64
}

// New creates a new instance that implements component.DeployMigrateWaiter.
func New(
	logger logrus.FieldLogger,
	client client.Client,
	values *Values,
	waitInterval time.Duration,
	waitSevereThreshold time.Duration,
	waitTimeout time.Duration,
) Interface {
	return &dnsRecord{
		logger:              logger,
		client:              client,
		values:              values,
		waitInterval:        waitInterval,
		waitSevereThreshold: waitSevereThreshold,
		waitTimeout:         waitTimeout,

		dnsRecord: &extensionsv1alpha1.DNSRecord{
			ObjectMeta: metav1.ObjectMeta{
				Name:      values.Name,
				Namespace: values.Namespace,
			},
		},
		secret: &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      values.SecretName,
				Namespace: values.Namespace,
			},
		},
	}
}

type dnsRecord struct {
	logger              logrus.FieldLogger
	client              client.Client
	values              *Values
	waitInterval        time.Duration
	waitSevereThreshold time.Duration
	waitTimeout         time.Duration

	dnsRecord *extensionsv1alpha1.DNSRecord
	secret    *corev1.Secret
}

// Deploy uses the seed client to create or update the DNSRecord resource.
func (c *dnsRecord) Deploy(ctx context.Context) error {
	_, err := c.deploy(ctx, v1beta1constants.GardenerOperationReconcile)
	return err
}

func (c *dnsRecord) deploy(ctx context.Context, operation string) (extensionsv1alpha1.Object, error) {
	if _, err := controllerutils.GetAndCreateOrMergePatch(ctx, c.client, c.secret, func() error {
		c.secret.Type = corev1.SecretTypeOpaque
		c.secret.Data = c.values.SecretData
		return nil
	}); err != nil {
		return nil, err
	}

	mutateFn := func() error {
		metav1.SetMetaDataAnnotation(&c.dnsRecord.ObjectMeta, v1beta1constants.GardenerOperation, operation)
		metav1.SetMetaDataAnnotation(&c.dnsRecord.ObjectMeta, v1beta1constants.GardenerTimestamp, TimeNow().UTC().String())

		c.dnsRecord.Spec = extensionsv1alpha1.DNSRecordSpec{
			DefaultSpec: extensionsv1alpha1.DefaultSpec{
				Type: c.values.Type,
			},
			SecretRef: corev1.SecretReference{
				Name:      c.secret.Name,
				Namespace: c.secret.Namespace,
			},
			Zone:       c.values.Zone,
			Name:       c.values.DNSName,
			RecordType: c.values.RecordType,
			Values:     c.values.Values,
			TTL:        c.values.TTL,
		}

		return nil
	}

	if c.values.CreateOnly {
		if err := c.client.Get(ctx, client.ObjectKeyFromObject(c.dnsRecord), c.dnsRecord); err != nil {
			if !apierrors.IsNotFound(err) {
				return nil, err
			}
			_ = mutateFn()
			if err := c.client.Create(ctx, c.dnsRecord); err != nil {
				return nil, err
			}
		}
	} else {
		if _, err := controllerutils.GetAndCreateOrMergePatch(ctx, c.client, c.dnsRecord, mutateFn); err != nil {
			return nil, err
		}
	}

	return c.dnsRecord, nil
}

// Restore uses the seed client and the ShootState to create the DNSRecord resource and restore its state.
func (c *dnsRecord) Restore(ctx context.Context, shootState *gardencorev1alpha1.ShootState) error {
	return extensions.RestoreExtensionWithDeployFunction(
		ctx,
		c.client,
		shootState,
		extensionsv1alpha1.DNSRecordResource,
		c.deploy,
	)
}

// Migrate migrates the DNSRecord resource.
func (c *dnsRecord) Migrate(ctx context.Context) error {
	return extensions.MigrateExtensionObject(
		ctx,
		c.client,
		c.dnsRecord,
	)
}

// Destroy deletes the DNSRecord resource.
func (c *dnsRecord) Destroy(ctx context.Context) error {
	return extensions.DeleteExtensionObject(
		ctx,
		c.client,
		c.dnsRecord,
	)
}

// Wait waits until the DNSRecord resource is ready.
func (c *dnsRecord) Wait(ctx context.Context) error {
	return extensions.WaitUntilExtensionObjectReady(
		ctx,
		c.client,
		c.logger,
		c.dnsRecord,
		extensionsv1alpha1.DNSRecordResource,
		c.waitInterval,
		c.waitSevereThreshold,
		c.waitTimeout,
		nil,
	)
}

// WaitMigrate waits until the DNSRecord resource is migrated successfully.
func (c *dnsRecord) WaitMigrate(ctx context.Context) error {
	return extensions.WaitUntilExtensionObjectMigrated(
		ctx,
		c.client,
		c.dnsRecord,
		c.waitInterval,
		c.waitTimeout,
	)
}

// WaitCleanup waits until the DNSRecord resource is deleted.
func (c *dnsRecord) WaitCleanup(ctx context.Context) error {
	return extensions.WaitUntilExtensionObjectDeleted(
		ctx,
		c.client,
		c.logger,
		c.dnsRecord,
		extensionsv1alpha1.DNSRecordResource,
		c.waitInterval,
		c.waitTimeout,
	)
}

// SetRecordType sets the record type in the values.
func (c *dnsRecord) SetRecordType(recordType extensionsv1alpha1.DNSRecordType) {
	c.values.RecordType = recordType
}

// SetValues sets the values in the values.
func (c *dnsRecord) SetValues(values []string) {
	c.values.Values = values
}
