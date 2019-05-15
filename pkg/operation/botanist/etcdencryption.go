package botanist

import (
	"fmt"

	"github.com/gardener/gardener/pkg/logger"
	encryptionconfiguration "github.com/gardener/gardener/pkg/operation/etcdencryption"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiserverconfigv1 "k8s.io/apiserver/pkg/apis/config/v1"
)

const (
	// EtcdEncryptionSecretName is a constant for the name of the secret which contains the etcd encryption key(s).
	// Should match charts/seed-controlplane/charts/kube-apiserver/templates/kube-apiserver.yaml
	EtcdEncryptionSecretName = "etcd-encryption-secret"
	// EtcdEncryptionSecretFileName is a constant for the name of the file within the EncryptionConfiguration secret
	// Should match charts/seed-controlplane/charts/kube-apiserver/templates/kube-apiserver.yaml
	EtcdEncryptionSecretFileName = "encryption-configuration.yaml"
	// EtcdEncryptionRewriteSecretsAnnotation is a constant for the name of the annotation
	// with which to decide whether or not a rewriting of the shoot secrets is necessary.
	// This is the case e.g. in case of a changed EtcdEncryptionConfiguration.
	EtcdEncryptionRewriteSecretsAnnotation = "garden.sapcloud.io/rewrite-shoot-secrets"
)

// CreateEtcdEncryptionConfiguration creates a secret
func (b *Botanist) CreateEtcdEncryptionConfiguration() error {
	logger.Logger.Info("Starting CreateEtcdEncryptionConfiguration")

	// TODO: question do we need a switch to de-activate the encryption feature (although we all agreed to have 'secure by default')?

	needToWriteConfig := false
	exists, ec, err := b.readEncryptionConfigurationFromSeed()
	if err != nil {
		return err
	}
	if !exists {
		// create new passive EncryptionConfiguration if it does not exist yet
		// and remember to write this created configuration
		ec, err = encryptionconfiguration.CreateNewPassiveConfiguration()
		if err != nil {
			return err
		}
		needToWriteConfig = true
	} else {
		// if it exists already, it needs to be consistent
		consistent, err := b.isEncryptionConfigurationConsistent()
		if (err != nil) || !consistent {
			return err
		}
		// if it is not active already (aescbc as first provider) then set it to active
		// and remember to write this created configuration
		if !encryptionconfiguration.IsActive(ec) {
			encryptionconfiguration.SetActive(ec, true)
			needToWriteConfig = true
		}
	}
	if needToWriteConfig {
		// TODOME: calculate checksum of secret and remember in checksum map
		err = b.writeEncryptionConfiguration(ec)
		if err != nil {
			return err
		}
	}
	// enablement of etcd encryption feature done in helm chart of apiserver deployment
	// TODOME: check hybridbotanist controlplane.go DeployKubeAPIServer

	return nil
}

// RewriteShootSecrets rewrites a shoot's secrets if the EncryptionConfiguration has changed
func (b *Botanist) RewriteShootSecrets() error {
	logger.Logger.Info("Starting RewriteShootSecrets")

	// rewrite secrets only if EncryptionConfiguration is (still) consistent
	consistent, err := b.isEncryptionConfigurationConsistent()
	if (err != nil) || !consistent {
		return fmt.Errorf("EncryptionConfiguration inconsistent: %v", err)
	}

	// WARNING:
	// No explicit checking of whether EncryptionConfiguration is contained in a backup.
	// Be aware of the risk!
	//
	// TODO: Ensure this is also agreed upon by Gardener team

	// TODO: contact Amshuman Rao Karaya

	needToRewrite, err := b.needToRewriteShootSecrets()
	if err != nil {
		return err
	}
	if needToRewrite {
		err = b.rewriteShootSecrets()
		if err != nil {
			return err
		}
	}

	return nil
}

// readEncryptionConfigurationFromSeed reads the EncryptionConfiguration from the shoot namespace in the seed
func (b *Botanist) readEncryptionConfigurationFromSeed() (bool, *apiserverconfigv1.EncryptionConfiguration, error) {
	client := b.Operation.K8sSeedClient
	ecs, err := client.GetSecret(b.Operation.Shoot.SeedNamespace, EtcdEncryptionSecretName)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil, nil
		} else {
			return false, nil, err
		}
	}
	secretData, ok := ecs.Data[EtcdEncryptionSecretFileName]
	if !ok {
		return true, nil, fmt.Errorf("EncryptionConfiguration in seed cluster (%v) does not contain expected element: %v", EtcdEncryptionSecretName, EtcdEncryptionSecretFileName)
	}
	ec, err := encryptionconfiguration.CreateFromYAML(secretData)
	if err != nil {
		return true, nil, fmt.Errorf("EncryptionConfiguration in seed cluster (%v) is not consistent: %v", EtcdEncryptionSecretName, err)
	}
	return true, ec, nil
}

func (b *Botanist) calculateEtcdEncryptionSecretNameInGardenCluster() string {
	secretName := fmt.Sprintf("%s.%s", b.Shoot.Info.Name, EtcdEncryptionSecretName)
	return secretName
}

// readEncryptionConfigurationFromGarden reads the EncryptionConfiguration from the shoot namespace in the seed
func (b *Botanist) readEncryptionConfigurationFromGarden() (bool, *apiserverconfigv1.EncryptionConfiguration, error) {
	client := b.Operation.K8sGardenClient
	secretNameInGardenCluster := b.calculateEtcdEncryptionSecretNameInGardenCluster()
	ecs, err := client.GetSecret(b.Shoot.Info.Namespace, secretNameInGardenCluster)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil, nil
		} else {
			return false, nil, err
		}
	}
	secretData, ok := ecs.Data[EtcdEncryptionSecretFileName]
	if !ok {
		return true, nil, fmt.Errorf("EncryptionConfiguration in garden cluster (%v) does not contain expected element: %v", secretNameInGardenCluster, EtcdEncryptionSecretFileName)
	}
	ec, err := encryptionconfiguration.CreateFromYAML(secretData)
	if err != nil {
		return true, nil, fmt.Errorf("EncryptionConfiguration in garden cluster (%v) is not consistent: %v", secretNameInGardenCluster, err)
	}
	return true, ec, nil
}

// writeEncryptionConfiguration writes the secret which contains the EncryptionConfiguration to the
// shoot namespace in the seed cluster as well as to the garden cluster
func (b *Botanist) writeEncryptionConfiguration(ec *apiserverconfigv1.EncryptionConfiguration) error {
	ecYamlBytes, err := encryptionconfiguration.ToYAML(ec)
	if err != nil {
		return err
	}
	err = b.writeEncryptionConfigurationSecretToSeed(ecYamlBytes)
	if err != nil {
		return err
	}
	err = b.writeEncryptionConfigurationSecretToGarden(ecYamlBytes)
	if err != nil {
		return err
	}
	// if changed configuration was written successfully, remember to rewrite secrets once shoot apiserver is up and running
	err = b.setNeedToRewriteShootSecrets(true)
	if err != nil {
		return err
	}
	return nil
}

// writeEncryptionConfigurationSecretToSeed writes the secret which contains the EncryptionConfiguration
// to the shoot namespace in the seed cluster
func (b *Botanist) writeEncryptionConfigurationSecretToSeed(ecYamlBytes []byte) error {
	client := b.Operation.K8sSeedClient
	secretObj := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      EtcdEncryptionSecretName,
			Namespace: b.Operation.Shoot.SeedNamespace,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			EtcdEncryptionSecretFileName: ecYamlBytes,
		},
	}
	if _, err := client.CreateSecretObject(secretObj, true); err != nil {
		return err
	}
	// TODOME: what about checksum calculation (to trigger restart of api server if required)
	return nil
}

// writeEncryptionConfigurationSecretToGarden writes the secret which contains the EncryptionConfiguration
// to the garden cluster
func (b *Botanist) writeEncryptionConfigurationSecretToGarden(ecYamlBytes []byte) error {
	client := b.Operation.K8sGardenClient
	secretNameInGardenCluster := b.calculateEtcdEncryptionSecretNameInGardenCluster()
	secretObj := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretNameInGardenCluster,
			Namespace: b.Shoot.Info.Namespace,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			EtcdEncryptionSecretFileName: ecYamlBytes,
		},
	}
	if _, err := client.CreateSecretObject(secretObj, true); err != nil {
		return err
	}
	// TODOME: what about checksum calculation (to trigger restart of api server if required)
	return nil
}

// isEncryptionConfigurationConsistent checks whether the configuration is consistent in seed and garden
func (b *Botanist) isEncryptionConfigurationConsistent() (bool, error) {
	exists, ecSeed, err := b.readEncryptionConfigurationFromSeed()
	if (err != nil) || !exists {
		return false, err
	}
	exists, ecGarden, err := b.readEncryptionConfigurationFromGarden()
	if (err != nil) || !exists {
		return false, err
	}
	equal, err := encryptionconfiguration.Equals(ecSeed, ecGarden)
	if (err != nil) || !equal {
		return false, fmt.Errorf("EncryptionConfiguration in seed cluster and garden cluster are not equal: %v", err)
	}
	consistent, err := encryptionconfiguration.IsConsistent(ecSeed)
	if (err != nil) || !consistent {
		return false, fmt.Errorf("EncryptionConfiguration in seed cluster is not consistent: %v", err)
	}
	return true, nil
}

// needToRewriteShootSecrets checks whether the secrets in the shoot need to
// be rewritten, e.g. after a change to the EncryptionConfiguration
func (b *Botanist) needToRewriteShootSecrets() (bool, error) {
	// ****************************************************************************************************************
	// TODO: Check Pseudocode
	//
	// 1. obtain e.Operation.K8sSeedClient
	// 2. switch to shoot namespace
	// 3. check annotation (how?)
	//
	// ****************************************************************************************************************

	return false, fmt.Errorf("not implemented yet")
}

// setNeedToRewriteShootSecrets sets the annotation with which to remember
// whether the shoot secrets need to be rewritten
func (b *Botanist) setNeedToRewriteShootSecrets(rewrite bool) error {
	// ****************************************************************************************************************
	// TODO: Check Pseudocode
	//
	// 1. obtain e.Operation.K8sSeedClient
	// 2. switch to shoot namespace
	// 3. set annotation of etcdencryptionconfigurationsecret in shoot namespace of seed
	// pkg/operation/botanist/controlplane.go ==> patchDeploymentCloudProviderChecksums
	//
	// ****************************************************************************************************************
	return fmt.Errorf("not implemented yet")
}

// rewriteShootSecrets rewrites all secrets of the shoot.
// This will take into account the current EncryptionConfiguration.
func (b *Botanist) rewriteShootSecrets() error {
	// ****************************************************************************************************************
	// TODO: Check Pseudocode
	//
	// 1. obtain e.Operation.K8sShootClient
	// 2. For all secrets in all namespaces:
	//    a) read secret
	//    b) write secret
	//
	// ****************************************************************************************************************
	return fmt.Errorf("not implemented yet")
	// err = b.setNeedToRewriteShootSecrets(false)
	// if err != nil {
	// 	return err
	// }
}
