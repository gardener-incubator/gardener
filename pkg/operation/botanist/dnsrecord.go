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

package botanist

import (
	"context"

	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/gardener/gardener/pkg/operation/botanist/component"
	extensionsdnsrecord "github.com/gardener/gardener/pkg/operation/botanist/component/extensions/dnsrecord"
	gutil "github.com/gardener/gardener/pkg/utils/gardener"
)

// DefaultExternalDNSRecord creates the default deployer for the external DNSRecord resource.
func (b *Botanist) DefaultExternalDNSRecord() extensionsdnsrecord.Interface {
	values := &extensionsdnsrecord.Values{
		Name:       b.Shoot.Info.Name + "-" + DNSExternalName,
		SecretName: b.Shoot.Info.Name + "-" + DNSExternalName,
		Namespace:  b.Shoot.SeedNamespace,
		TTL:        b.Config.Controllers.Shoot.DNSEntryTTLSeconds,
	}
	if b.NeedsExternalDNS() {
		values.Type = b.Shoot.ExternalDomain.Provider
		if b.Shoot.ExternalDomain.Zone != "" {
			values.Zone = &b.Shoot.ExternalDomain.Zone
		}
		values.SecretData = b.Shoot.ExternalDomain.SecretData
		values.DNSName = gutil.GetAPIServerDomain(*b.Shoot.ExternalClusterDomain)
	}
	return extensionsdnsrecord.New(
		b.Logger,
		b.K8sSeedClient.Client(),
		values,
		extensionsdnsrecord.DefaultInterval,
		extensionsdnsrecord.DefaultSevereThreshold,
		extensionsdnsrecord.DefaultTimeout,
	)
}

// DefaultInternalDNSRecord creates the default deployer for the internal DNSRecord resource.
func (b *Botanist) DefaultInternalDNSRecord() extensionsdnsrecord.Interface {
	values := &extensionsdnsrecord.Values{
		Name:       b.Shoot.Info.Name + "-" + DNSInternalName,
		SecretName: b.Shoot.Info.Name + "-" + DNSInternalName,
		Namespace:  b.Shoot.SeedNamespace,
		TTL:        b.Config.Controllers.Shoot.DNSEntryTTLSeconds,
	}
	if b.NeedsInternalDNS() {
		values.Type = b.Garden.InternalDomain.Provider
		if b.Garden.InternalDomain.Zone != "" {
			values.Zone = &b.Garden.InternalDomain.Zone
		}
		values.SecretData = b.Garden.InternalDomain.SecretData
		values.DNSName = gutil.GetAPIServerDomain(b.Shoot.InternalClusterDomain)
	}
	return extensionsdnsrecord.New(
		b.Logger,
		b.K8sSeedClient.Client(),
		values,
		extensionsdnsrecord.DefaultInterval,
		extensionsdnsrecord.DefaultSevereThreshold,
		extensionsdnsrecord.DefaultTimeout,
	)
}

// DefaultOwnerDNSRecord creates the default deployer for the owner DNSRecord resource.
func (b *Botanist) DefaultOwnerDNSRecord() extensionsdnsrecord.Interface {
	values := &extensionsdnsrecord.Values{
		Name:       b.Shoot.Info.Name + "-" + DNSOwnerName,
		SecretName: b.Shoot.Info.Name + "-" + DNSInternalName,
		Namespace:  b.Shoot.SeedNamespace,
		CreateOnly: true,
		TTL:        b.Config.Controllers.Shoot.DNSEntryTTLSeconds,
	}
	if b.NeedsInternalDNS() {
		values.Type = b.Garden.InternalDomain.Provider
		if b.Garden.InternalDomain.Zone != "" {
			values.Zone = &b.Garden.InternalDomain.Zone
		}
		values.SecretData = b.Garden.InternalDomain.SecretData
		values.DNSName = gutil.GetOwnerDomain(b.Shoot.InternalClusterDomain)
		values.RecordType = extensionsv1alpha1.DNSRecordTypeTXT
		values.Values = []string{*b.Seed.Info.Status.ClusterIdentity}
	}
	return extensionsdnsrecord.New(
		b.Logger,
		b.K8sSeedClient.Client(),
		values,
		extensionsdnsrecord.DefaultInterval,
		extensionsdnsrecord.DefaultSevereThreshold,
		extensionsdnsrecord.DefaultTimeout,
	)
}

// DeployOrDestroyExternalDNSRecord deploys, restores, or destroys the external DNSRecord and waits for the operation to complete.
func (b *Botanist) DeployOrDestroyExternalDNSRecord(ctx context.Context) error {
	if b.NeedsExternalDNS() {
		return b.deployExternalDNSRecord(ctx)
	}
	return b.DestroyExternalDNSRecord(ctx)
}

// DeployOrDestroyInternalDNSRecord deploys, restores, or destroys the internal DNSRecord and waits for the operation to complete.
func (b *Botanist) DeployOrDestroyInternalDNSRecord(ctx context.Context) error {
	if b.NeedsInternalDNS() {
		return b.deployInternalDNSRecord(ctx)
	}
	return b.DestroyInternalDNSRecord(ctx)
}

// DeployOrDestroyOwnerDNSRecord deploys, restores, or destroys the owner DNSRecord and waits for the operation to complete.
func (b *Botanist) DeployOrDestroyOwnerDNSRecord(ctx context.Context) error {
	if b.NeedsInternalDNS() {
		return b.DeployOwnerDNSRecord(ctx)
	}
	return b.DestroyOwnerDNSRecord(ctx)
}

// deployExternalDNSRecord deploys or restores the external DNSRecord and waits for the operation to complete.
func (b *Botanist) deployExternalDNSRecord(ctx context.Context) error {
	if err := b.deployOrRestoreDNSRecord(ctx, b.Shoot.Components.Extensions.ExternalDNSRecord); err != nil {
		return err
	}
	return b.Shoot.Components.Extensions.ExternalDNSRecord.Wait(ctx)
}

// deployInternalDNSRecord deploys or restores the internal DNSRecord and waits for the operation to complete.
func (b *Botanist) deployInternalDNSRecord(ctx context.Context) error {
	if err := b.deployOrRestoreDNSRecord(ctx, b.Shoot.Components.Extensions.InternalDNSRecord); err != nil {
		return err
	}
	return b.Shoot.Components.Extensions.InternalDNSRecord.Wait(ctx)
}

// DeployOwnerDNSRecord deploys or restores the owner DNSRecord and waits for the operation to complete.
func (b *Botanist) DeployOwnerDNSRecord(ctx context.Context) error {
	if err := b.deployOrRestoreDNSRecord(ctx, b.Shoot.Components.Extensions.OwnerDNSRecord); err != nil {
		return err
	}
	return b.Shoot.Components.Extensions.OwnerDNSRecord.Wait(ctx)
}

// DestroyExternalDNSRecord destroys the external DNSRecord and waits for the operation to complete.
func (b *Botanist) DestroyExternalDNSRecord(ctx context.Context) error {
	if err := b.Shoot.Components.Extensions.ExternalDNSRecord.Destroy(ctx); err != nil {
		return err
	}
	return b.Shoot.Components.Extensions.ExternalDNSRecord.WaitCleanup(ctx)
}

// DestroyInternalDNSRecord destroys the internal DNSRecord and waits for the operation to complete.
func (b *Botanist) DestroyInternalDNSRecord(ctx context.Context) error {
	if err := b.Shoot.Components.Extensions.InternalDNSRecord.Destroy(ctx); err != nil {
		return err
	}
	return b.Shoot.Components.Extensions.InternalDNSRecord.WaitCleanup(ctx)
}

// DestroyOwnerDNSRecord destroys the owner DNSRecord and waits for the operation to complete.
func (b *Botanist) DestroyOwnerDNSRecord(ctx context.Context) error {
	if err := b.Shoot.Components.Extensions.OwnerDNSRecord.Destroy(ctx); err != nil {
		return err
	}
	return b.Shoot.Components.Extensions.OwnerDNSRecord.WaitCleanup(ctx)
}

// MigrateExternalDNSRecord migrates the external DNSRecord and waits for the operation to complete.
func (b *Botanist) MigrateExternalDNSRecord(ctx context.Context) error {
	if err := b.Shoot.Components.Extensions.ExternalDNSRecord.Migrate(ctx); err != nil {
		return err
	}
	return b.Shoot.Components.Extensions.ExternalDNSRecord.WaitMigrate(ctx)
}

// MigrateInternalDNSRecord migrates the internal DNSRecord and waits for the operation to complete.
func (b *Botanist) MigrateInternalDNSRecord(ctx context.Context) error {
	if err := b.Shoot.Components.Extensions.InternalDNSRecord.Migrate(ctx); err != nil {
		return err
	}
	return b.Shoot.Components.Extensions.InternalDNSRecord.WaitMigrate(ctx)
}

// MigrateOwnerDNSRecord migrates the owner DNSRecord and waits for the operation to complete.
func (b *Botanist) MigrateOwnerDNSRecord(ctx context.Context) error {
	if err := b.Shoot.Components.Extensions.OwnerDNSRecord.Migrate(ctx); err != nil {
		return err
	}
	return b.Shoot.Components.Extensions.OwnerDNSRecord.WaitMigrate(ctx)
}

func (b *Botanist) deployOrRestoreDNSRecord(ctx context.Context, dnsRecord component.DeployMigrateWaiter) error {
	if b.isRestorePhase() {
		return dnsRecord.Restore(ctx, b.ShootState)
	}
	return dnsRecord.Deploy(ctx)
}
