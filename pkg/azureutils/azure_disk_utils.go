/*
Copyright 2021 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package azureutils

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/Azure/azure-sdk-for-go/services/compute/mgmt/2020-12-01/compute"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/pborman/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	v1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/net"
	"k8s.io/apimachinery/pkg/util/wait"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	kubeutil "k8s.io/kubernetes/pkg/volume/util"
	"k8s.io/mount-utils"
	"sigs.k8s.io/azuredisk-csi-driver/pkg/apis/azuredisk/v1alpha1"
	azDiskClientSet "sigs.k8s.io/azuredisk-csi-driver/pkg/apis/client/clientset/versioned"
	azurediskInformers "sigs.k8s.io/azuredisk-csi-driver/pkg/apis/client/informers/externalversions"
	consts "sigs.k8s.io/azuredisk-csi-driver/pkg/azureconstants"
	"sigs.k8s.io/azuredisk-csi-driver/pkg/util"
	"sigs.k8s.io/cloud-provider-azure/pkg/azureclients"
	azure "sigs.k8s.io/cloud-provider-azure/pkg/provider"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	azureStackCloud = "AZURESTACKCLOUD"

	azurePublicCloudDefaultStorageAccountType     = compute.StandardSSDLRS
	azureStackCloudDefaultStorageAccountType      = compute.StandardLRS
	defaultAzureDataDiskCachingMode               = v1.AzureDataDiskCachingReadOnly
	defaultAzureDataDiskCachingModeForSharedDisks = v1.AzureDataDiskCachingNone

	// default IOPS Caps & Throughput Cap (MBps) per https://docs.microsoft.com/en-us/azure/virtual-machines/linux/disks-ultra-ssd
	// see https://docs.microsoft.com/en-us/rest/api/compute/disks/createorupdate#uri-parameters
	diskNameMinLength = 1
	// Reseting max length to 63 since the disk name is used in the label "volume-name"
	// of the kubernetes object and a label cannot have length greater than 63.
	// https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/
	diskNameMaxLengthForLabel = 63
	diskNameMaxLength         = 80
	// maxLength = 63 - (4 for ".vhd") = 59
	diskNameGenerateMaxLengthForLabel = 59
	// maxLength = 80 - (4 for ".vhd") = 76
	diskNameGenerateMaxLength = 76
)

type ClientOperationMode int

const (
	Cached ClientOperationMode = iota
	Uncached
)

func IsAzureStackCloud(cloud string, disableAzureStackCloud bool) bool {
	return !disableAzureStackCloud && strings.EqualFold(cloud, azureStackCloud)
}

// gets the AzVolume cluster client
func GetAzDiskClient(config *rest.Config) (
	*azDiskClientSet.Clientset,
	error) {
	azDiskClient, err := azDiskClientSet.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	return azDiskClient, nil
}

// GetDiskLUN : deviceInfo could be a LUN number or a device path, e.g. /dev/disk/azure/scsi1/lun2
func GetDiskLUN(deviceInfo string) (int32, error) {
	var diskLUN string
	if len(deviceInfo) <= 2 {
		diskLUN = deviceInfo
	} else {
		// extract the LUN num from a device path
		matches := consts.LunPathRE.FindStringSubmatch(deviceInfo)
		if len(matches) == 2 {
			diskLUN = matches[1]
		} else {
			return -1, fmt.Errorf("cannot parse deviceInfo: %s", deviceInfo)
		}
	}

	lun, err := strconv.Atoi(diskLUN)
	if err != nil {
		return -1, err
	}
	return int32(lun), nil
}

func NormalizeStorageAccountType(storageAccountType, cloud string, disableAzureStackCloud bool) (compute.DiskStorageAccountTypes, error) {
	if storageAccountType == "" {
		if IsAzureStackCloud(cloud, disableAzureStackCloud) {
			return azureStackCloudDefaultStorageAccountType, nil
		}
		return azurePublicCloudDefaultStorageAccountType, nil
	}

	sku := compute.DiskStorageAccountTypes(storageAccountType)
	supportedSkuNames := compute.PossibleDiskStorageAccountTypesValues()
	if IsAzureStackCloud(cloud, disableAzureStackCloud) {
		supportedSkuNames = []compute.DiskStorageAccountTypes{compute.StandardLRS, compute.PremiumLRS}
	}
	for _, s := range supportedSkuNames {
		if sku == s {
			return sku, nil
		}
	}

	return "", fmt.Errorf("azureDisk - %s is not supported sku/storageaccounttype. Supported values are %s", storageAccountType, supportedSkuNames)
}

func NormalizeCachingMode(cachingMode v1.AzureDataDiskCachingMode, maxShares int) (v1.AzureDataDiskCachingMode, error) {
	if cachingMode == "" {
		if maxShares > 1 {
			return defaultAzureDataDiskCachingModeForSharedDisks, nil
		}
		return defaultAzureDataDiskCachingMode, nil
	}

	if !consts.SupportedCachingModes.Has(string(cachingMode)) {
		return "", fmt.Errorf("azureDisk - %s is not supported cachingmode. Supported values are %s", cachingMode, consts.SupportedCachingModes.List())
	}

	return cachingMode, nil
}

func NormalizeNetworkAccessPolicy(networkAccessPolicy string) (compute.NetworkAccessPolicy, error) {
	if networkAccessPolicy == "" {
		return compute.AllowAll, nil
	}
	policy := compute.NetworkAccessPolicy(networkAccessPolicy)
	for _, s := range compute.PossibleNetworkAccessPolicyValues() {
		if policy == s {
			return policy, nil
		}
	}
	return "", fmt.Errorf("azureDisk - %s is not supported NetworkAccessPolicy. Supported values are %s", networkAccessPolicy, compute.PossibleNetworkAccessPolicyValues())
}

// Disk name must begin with a letter or number, end with a letter, number or underscore,
// and may contain only letters, numbers, underscores, periods, or hyphens.
// See https://docs.microsoft.com/en-us/rest/api/compute/disks/createorupdate#uri-parameters
//
//
// Snapshot name must begin with a letter or number, end with a letter, number or underscore,
// and may contain only letters, numbers, underscores, periods, or hyphens.
// See https://docs.microsoft.com/en-us/rest/api/compute/snapshots/createorupdate#uri-parameters
//
// Since the naming rule of disk is same with snapshot's, here we use the same function to handle disks and snapshots.
func CreateValidDiskName(volumeName string, usedForLabel bool) string {
	var maxDiskNameLength, maxGeneratedDiskNameLength int
	diskName := volumeName
	if usedForLabel {
		maxDiskNameLength = diskNameMaxLengthForLabel
		maxGeneratedDiskNameLength = diskNameGenerateMaxLengthForLabel
	} else {
		maxDiskNameLength = diskNameMaxLength
		maxGeneratedDiskNameLength = diskNameGenerateMaxLength
	}
	if len(diskName) > maxDiskNameLength {
		diskName = diskName[0:maxDiskNameLength]
		klog.Warningf("since the maximum volume name length is %d, so it is truncated as (%q)", diskNameMaxLength, diskName)
	}
	if !checkDiskName(diskName) || len(diskName) < diskNameMinLength {
		// todo: get cluster name
		diskName = kubeutil.GenerateVolumeName("pvc-disk", uuid.NewUUID().String(), maxGeneratedDiskNameLength)
		klog.Warningf("the requested volume name (%q) is invalid, so it is regenerated as (%q)", volumeName, diskName)
	}

	return diskName
}

// GetCloudProviderFromClient get Azure Cloud Provider
func GetCloudProviderFromClient(kubeClient *clientset.Clientset, secretName string, secretNamespace string, userAgent string) (*azure.Cloud, error) {
	var config *azure.Config
	var fromSecret bool

	// Try to get the configuration from a K8s secret first...
	if kubeClient != nil {
		az := &azure.Cloud{
			InitSecretConfig: azure.InitSecretConfig{
				SecretName:      secretName,
				SecretNamespace: secretNamespace,
				CloudConfigKey:  "cloud-config",
			},
		}

		az.KubeClient = kubeClient

		var err error

		config, err = az.GetConfigFromSecret()
		if err == nil {
			fromSecret = true
		} else {
			if !errors.IsNotFound(err) {
				klog.Warningf("failed to create cloud config from secret %s/%s: %v", az.SecretNamespace, az.SecretName, err)
			}
		}
	}

	// ... and fallback to reading configuration file on disk.
	if config == nil {
		credFile, ok := os.LookupEnv(consts.DefaultAzureCredentialFileEnv)
		if ok && strings.TrimSpace(credFile) != "" {
			klog.V(2).Infof("%s env var set as %v", consts.DefaultAzureCredentialFileEnv, credFile)
		} else {
			if util.IsWindowsOS() {
				credFile = consts.DefaultCredFilePathWindows
			} else {
				credFile = consts.DefaultCredFilePathLinux
			}
			klog.V(2).Infof("use default %s env var: %v", consts.DefaultAzureCredentialFileEnv, credFile)
		}

		credFileConfig, err := os.Open(credFile)
		if err != nil {
			err = fmt.Errorf("failed to load cloud config from file %q: %v", credFile, err)
			klog.Errorf(err.Error())
			return nil, err
		}
		defer credFileConfig.Close()

		config, err = azure.ParseConfig(credFileConfig)
		if err != nil {
			err = fmt.Errorf("failed to parse cloud config file %q: %v", credFile, err)
			klog.Errorf(err.Error())
			return nil, err
		}
	}

	// Override configuration values
	config.DiskRateLimit = &azureclients.RateLimitConfig{
		CloudProviderRateLimit: false,
	}
	config.SnapshotRateLimit = &azureclients.RateLimitConfig{
		CloudProviderRateLimit: false,
	}
	config.UserAgent = userAgent

	// Create a new cloud provider
	az, err := azure.NewCloudWithoutFeatureGatesFromConfig(config, fromSecret, false)
	if err != nil {
		err = fmt.Errorf("failed to create cloud: %v", err)
		klog.Errorf(err.Error())
		return nil, err
	}

	// reassign kubeClient
	if kubeClient != nil && az.KubeClient == nil {
		az.KubeClient = kubeClient
	}

	return az, nil
}

// GetCloudProvider get Azure Cloud Provider
func GetCloudProvider(kubeConfig, secretName, secretNamespace, userAgent string) (*azure.Cloud, error) {
	kubeClient, err := GetKubeClient(kubeConfig)
	if err != nil {
		klog.Warningf("get kubeconfig(%s) failed with error: %v", kubeConfig, err)
		if !os.IsNotExist(err) && err != rest.ErrNotInCluster {
			return nil, fmt.Errorf("failed to get KubeClient: %v", err)
		}
	}
	return GetCloudProviderFromClient(kubeClient, secretName, secretNamespace, userAgent)
}

// GetKubeConfig gets config object from config file
func GetKubeConfig(kubeconfig string) (config *rest.Config, err error) {
	if kubeconfig != "" {
		if config, err = clientcmd.BuildConfigFromFlags("", kubeconfig); err != nil {
			return nil, err
		}
	} else {
		if config, err = rest.InClusterConfig(); err != nil {
			return nil, err
		}
	}
	return config, err
}

func GetKubeClient(kubeconfig string) (*clientset.Clientset, error) {
	config, err := GetKubeConfig(kubeconfig)
	if err != nil {
		return nil, err
	}

	return clientset.NewForConfig(config)
}

func IsValidDiskURI(diskURI string) error {
	if strings.Index(strings.ToLower(diskURI), "/subscriptions/") != 0 {
		return fmt.Errorf("invalid DiskURI: %v, correct format: %v", diskURI, consts.DiskURISupportedManaged)
	}
	return nil
}

func GetDiskName(diskURI string) (string, error) {
	matches := consts.ManagedDiskPathRE.FindStringSubmatch(diskURI)
	if len(matches) != 2 {
		return "", fmt.Errorf("could not get disk name from %s, correct format: %s", diskURI, consts.ManagedDiskPathRE)
	}
	return matches[1], nil
}

// GetResourceGroupFromURI returns resource groupd from URI
func GetResourceGroupFromURI(diskURI string) (string, error) {
	fields := strings.Split(diskURI, "/")
	if len(fields) != 9 || strings.ToLower(fields[3]) != "resourcegroups" {
		return "", fmt.Errorf("invalid disk URI: %s", diskURI)
	}
	return fields[4], nil
}

func GetCachingMode(attributes map[string]string) (compute.CachingTypes, error) {
	var (
		cachingMode v1.AzureDataDiskCachingMode
		maxShares   int
		err         error
	)

	for k, v := range attributes {
		if strings.EqualFold(k, consts.CachingModeField) {
			cachingMode = v1.AzureDataDiskCachingMode(v)
			break
		}
		// Check if disk is shared
		if strings.EqualFold(k, consts.MaxSharesField) {
			maxShares, err = strconv.Atoi(v)
			if err != nil || maxShares < 1 {
				maxShares = 1
			}
		}
	}

	cachingMode, err = NormalizeCachingMode(cachingMode, maxShares)
	return compute.CachingTypes(cachingMode), err
}

// isARMResourceID check whether resourceID is an ARM ResourceID
func IsARMResourceID(resourceID string) bool {
	id := strings.ToLower(resourceID)
	return strings.Contains(id, "/subscriptions/")
}

func IsCorruptedDir(dir string) bool {
	_, pathErr := mount.PathExists(dir)
	fmt.Printf("IsCorruptedDir(%s) returned with error: %v", dir, pathErr)
	return pathErr != nil && mount.IsCorruptedMnt(pathErr)
}

// isAvailabilityZone returns true if the zone is in format of <region>-<zone-id>.
func IsValidAvailabilityZone(zone, region string) bool {
	return strings.HasPrefix(zone, fmt.Sprintf("%s-", region))
}

// PickAvailabilityZone selects 1 zone given topology requirement.
// if not found or topology requirement is not zone format, empty string is returned.
func PickAvailabilityZone(requirement *csi.TopologyRequirement, region, topologyKey string) string {
	if requirement == nil {
		return ""
	}
	for _, topology := range requirement.GetPreferred() {
		if zone, exists := topology.GetSegments()[consts.WellKnownTopologyKey]; exists {
			if IsValidAvailabilityZone(zone, region) {
				return zone
			}
		}
		if zone, exists := topology.GetSegments()[topologyKey]; exists {
			if IsValidAvailabilityZone(zone, region) {
				return zone
			}
		}
	}
	for _, topology := range requirement.GetRequisite() {
		if zone, exists := topology.GetSegments()[consts.WellKnownTopologyKey]; exists {
			if IsValidAvailabilityZone(zone, region) {
				return zone
			}
		}
		if zone, exists := topology.GetSegments()[topologyKey]; exists {
			if IsValidAvailabilityZone(zone, region) {
				return zone
			}
		}
	}
	return ""
}

func IsValidVolumeCapabilities(volCaps []*csi.VolumeCapability) bool {
	hasSupport := func(cap *csi.VolumeCapability) bool {
		for _, c := range consts.VolumeCaps {
			// todo: Block volume support
			/* compile error here
			if blk := c.GetBlock(); blk != nil {
				return false
			}
			*/
			if c.GetMode() == cap.AccessMode.GetMode() {
				return true
			}
		}
		return false
	}

	foundAll := true
	for _, c := range volCaps {
		if !hasSupport(c) {
			foundAll = false
		}
	}
	return foundAll
}

func GetAzVolumeAttachmentName(volumeName string, nodeName string) string {
	return fmt.Sprintf("%s-%s-attachment", strings.ToLower(volumeName), strings.ToLower(nodeName))
}

func GetMaxSharesAndMaxMountReplicaCount(parameters map[string]string) (int, int) {
	maxShares := 1
	maxMountReplicaCount := -1
	for param, value := range parameters {
		if strings.EqualFold(param, consts.MaxSharesField) {
			parsed, err := strconv.Atoi(value)
			if err != nil {
				klog.Warningf("failed to parse maxShares value (%s) to int, defaulting to 1: %v", value, err)
			} else {
				maxShares = parsed
			}
		} else if strings.EqualFold(param, consts.MaxMountReplicaCountField) {
			parsed, err := strconv.Atoi(value)
			if err != nil {
				klog.Warningf("failed to parse maxMountReplica value (%s) to int, defaulting to 0: %v", value, err)
			} else {
				maxMountReplicaCount = parsed
			}
		}
	}

	if maxShares <= 0 {
		klog.Warningf("maxShares cannot be set smaller than 1... Defaulting current maxShares (%d) value to 1", maxShares)
		maxShares = 1
	}
	if maxShares-1 < maxMountReplicaCount {
		klog.Warningf("maxMountReplicaCount cannot be set larger than maxShares - 1... Defaulting current maxMountReplicaCount (%d) value to (%d)", maxMountReplicaCount, maxShares-1)
		maxMountReplicaCount = maxShares - 1
	} else if maxMountReplicaCount < 0 {
		maxMountReplicaCount = maxShares - 1
	}

	return maxShares, maxMountReplicaCount
}

func GetAzVolumePhase(phase v1.PersistentVolumePhase) v1alpha1.AzVolumePhase {
	return v1alpha1.AzVolumePhase(phase)
}

func GetAzVolume(ctx context.Context, cachedClient client.Client, azDiskClient azDiskClientSet.Interface, azVolumeName, namespace string, useCache bool) (*v1alpha1.AzVolume, error) {
	var azVolume *v1alpha1.AzVolume
	var err error
	if useCache {
		azVolume = &v1alpha1.AzVolume{}
		err = cachedClient.Get(ctx, types.NamespacedName{Name: azVolumeName, Namespace: namespace}, azVolume)
	} else {
		azVolume, err = azDiskClient.DiskV1alpha1().AzVolumes(namespace).Get(ctx, azVolumeName, metav1.GetOptions{})
	}
	return azVolume, err
}

func ListAzVolumes(ctx context.Context, cachedClient client.Client, azDiskClient azDiskClientSet.Interface, namespace string, useCache bool) (v1alpha1.AzVolumeList, error) {
	var azVolumeList *v1alpha1.AzVolumeList
	var err error
	if useCache {
		azVolumeList = &v1alpha1.AzVolumeList{}
		err = cachedClient.List(ctx, azVolumeList)
	} else {
		azVolumeList, err = azDiskClient.DiskV1alpha1().AzVolumes(namespace).List(ctx, metav1.ListOptions{})
	}
	return *azVolumeList, err
}

func GetAzVolumeAttachment(ctx context.Context, cachedClient client.Client, azDiskClient azDiskClientSet.Interface, azVolumeAttachmentName, namespace string, useCache bool) (*v1alpha1.AzVolumeAttachment, error) {
	var azVolumeAttachment *v1alpha1.AzVolumeAttachment
	var err error
	if useCache {
		azVolumeAttachment = &v1alpha1.AzVolumeAttachment{}
		err = cachedClient.Get(ctx, types.NamespacedName{Name: azVolumeAttachmentName, Namespace: namespace}, azVolumeAttachment)
	} else {
		azVolumeAttachment, err = azDiskClient.DiskV1alpha1().AzVolumeAttachments(namespace).Get(ctx, azVolumeAttachmentName, metav1.GetOptions{})
	}
	return azVolumeAttachment, err
}

func ListAzVolumeAttachments(ctx context.Context, cachedClient client.Client, azDiskClient azDiskClientSet.Interface, namespace string, useCache bool) (v1alpha1.AzVolumeAttachmentList, error) {
	var azVolumeAttachmentList *v1alpha1.AzVolumeAttachmentList
	var err error
	if useCache {
		azVolumeAttachmentList = &v1alpha1.AzVolumeAttachmentList{}
		err = cachedClient.List(ctx, azVolumeAttachmentList)
	} else {
		azVolumeAttachmentList, err = azDiskClient.DiskV1alpha1().AzVolumeAttachments(namespace).List(ctx, metav1.ListOptions{})
	}
	return *azVolumeAttachmentList, err
}

func GetAzVolumeAttachmentState(volumeAttachmentStatus storagev1.VolumeAttachmentStatus) v1alpha1.AzVolumeAttachmentAttachmentState {
	if volumeAttachmentStatus.Attached {
		return v1alpha1.Attached
	} else if volumeAttachmentStatus.AttachError != nil {
		return v1alpha1.AttachmentFailed
	} else if volumeAttachmentStatus.DetachError != nil {
		return v1alpha1.DetachmentFailed
	} else {
		return v1alpha1.AttachmentPending
	}
}

func UpdateCRIWithRetry(ctx context.Context, informerFactory azurediskInformers.SharedInformerFactory, cachedClient client.Client, azDiskClient azDiskClientSet.Interface, obj client.Object, updateFunc func(interface{}) error, maxNetRetry int) error {
	klog.Infof("Initiating update with retry for %v (%s)", reflect.TypeOf(obj), obj.GetName())

	conditionFunc := func() error {
		var err error
		switch target := obj.(type) {
		case *v1alpha1.AzVolume:
			if informerFactory != nil {
				target, err = informerFactory.Disk().V1alpha1().AzVolumes().Lister().AzVolumes(target.Namespace).Get(target.Name)
			} else if cachedClient != nil {
				err = cachedClient.Get(ctx, types.NamespacedName{Namespace: target.Namespace, Name: target.Name}, target)
			} else {
				target, err = azDiskClient.DiskV1alpha1().AzVolumes(target.Namespace).Get(ctx, target.Name, metav1.GetOptions{})
			}
			obj = target.DeepCopy()
		case *v1alpha1.AzVolumeAttachment:
			if informerFactory != nil {
				target, err = informerFactory.Disk().V1alpha1().AzVolumeAttachments().Lister().AzVolumeAttachments(target.Namespace).Get(target.Name)
			} else if cachedClient != nil {
				err = cachedClient.Get(ctx, types.NamespacedName{Namespace: target.Namespace, Name: target.Name}, target)
			} else {
				target, err = azDiskClient.DiskV1alpha1().AzVolumeAttachments(target.Namespace).Get(ctx, target.Name, metav1.GetOptions{})
			}
			obj = target.DeepCopy()
		default:
			return status.Errorf(codes.Internal, "object (%v) not supported.", reflect.TypeOf(target))
		}

		if err != nil {
			klog.Errorf("failed to get %v (%s): %v", reflect.TypeOf(obj), obj.GetName(), err)
			return err
		}

		if err = updateFunc(obj); err != nil {
			return err
		}

		switch target := obj.(type) {
		case *v1alpha1.AzVolume:
			if cachedClient == nil {
				_, err = azDiskClient.DiskV1alpha1().AzVolumes(target.Namespace).Update(ctx, target, metav1.UpdateOptions{})
			} else {
				err = cachedClient.Update(ctx, target)
			}
		case *v1alpha1.AzVolumeAttachment:
			if cachedClient == nil {
				_, err = azDiskClient.DiskV1alpha1().AzVolumeAttachments(target.Namespace).Update(ctx, target, metav1.UpdateOptions{})
			} else {
				err = cachedClient.Update(ctx, target)
			}
		}

		return err
	}

	curRetry := 0
	maxRetry := maxNetRetry
	isRetriable := func(err error) bool {
		if errors.IsConflict(err) {
			return true
		}
		if isNetError(err) {
			defer func() { curRetry++ }()
			return curRetry < maxRetry
		}
		return false
	}

	err := retry.OnError(
		wait.Backoff{
			Duration: consts.CRIUpdateRetryDuration,
			Factor:   consts.CRIUpdateRetryFactor,
			Steps:    consts.CRIUpdateRetryStep,
		},
		isRetriable,
		conditionFunc,
	)
	if err != nil {
		klog.Errorf("failed to update %v (%s): %v", reflect.TypeOf(obj), obj.GetName(), err)
	}

	// if encountered net error from api server unavailability, exit process
	ExitOnNetError(err)
	return err
}

func isNetError(err error) bool {
	return net.IsConnectionRefused(err) || net.IsConnectionReset(err) || net.IsTimeout(err) || net.IsProbableEOF(err)
}

func ExitOnNetError(err error) {
	if isNetError(err) {
		klog.Fatalf("encountered unrecoverable network error: %v \nexiting process...", err)
		os.Exit(1)
	}
}

// InsertDiskProperties: insert disk properties to map
func InsertDiskProperties(disk *compute.Disk, publishConext map[string]string) {
	if disk == nil || publishConext == nil {
		return
	}

	if disk.Sku != nil {
		publishConext[consts.SkuNameField] = string(disk.Sku.Name)
	}
	prop := disk.DiskProperties
	if prop != nil {
		publishConext[consts.NetworkAccessPolicyField] = string(prop.NetworkAccessPolicy)
		if prop.DiskIOPSReadWrite != nil {
			publishConext[consts.DiskIOPSReadWriteField] = strconv.Itoa(int(*prop.DiskIOPSReadWrite))
		}
		if prop.DiskMBpsReadWrite != nil {
			publishConext[consts.DiskMBPSReadWriteField] = strconv.Itoa(int(*prop.DiskMBpsReadWrite))
		}
		if prop.CreationData != nil && prop.CreationData.LogicalSectorSize != nil {
			publishConext[consts.LogicalSectorSizeField] = strconv.Itoa(int(*prop.CreationData.LogicalSectorSize))
		}
		if prop.Encryption != nil &&
			prop.Encryption.DiskEncryptionSetID != nil {
			publishConext[consts.DesIDField] = *prop.Encryption.DiskEncryptionSetID
		}
		if prop.MaxShares != nil {
			publishConext[consts.MaxSharesField] = strconv.Itoa(int(*prop.MaxShares))
		}
	}
}

func checkDiskName(diskName string) bool {
	length := len(diskName)

	for i, v := range diskName {
		if !(unicode.IsLetter(v) || unicode.IsDigit(v) || v == '_' || v == '.' || v == '-') ||
			(i == 0 && !(unicode.IsLetter(v) || unicode.IsDigit(v))) ||
			(i == length-1 && !(unicode.IsLetter(v) || unicode.IsDigit(v) || v == '_')) {
			return false
		}
	}

	return true
}

func SleepIfThrottled(err error, sleepSec int) {
	if strings.Contains(strings.ToLower(err.Error()), strings.ToLower(consts.TooManyRequests)) || strings.Contains(strings.ToLower(err.Error()), consts.ClientThrottled) {
		klog.Warningf("sleep %d more seconds, waiting for throttling complete", sleepSec)
		time.Sleep(time.Duration(sleepSec) * time.Second)
	}
}
