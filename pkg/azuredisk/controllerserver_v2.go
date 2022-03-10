//go:build azurediskv2
// +build azurediskv2

/*
Copyright 2017 The Kubernetes Authors.

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

package azuredisk

import (
	"context"
	"fmt"
	"strconv"

	"github.com/container-storage-interface/spec/lib/go/csi"

	"github.com/golang/protobuf/ptypes"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/klog/v2"

	diskv1alpha2 "sigs.k8s.io/azuredisk-csi-driver/pkg/apis/azuredisk/v1alpha2"
	"sigs.k8s.io/azuredisk-csi-driver/pkg/azureutils"
	"sigs.k8s.io/cloud-provider-azure/pkg/metrics"
)

// CreateVolume provisions an azure disk
func (d *DriverV2) CreateVolume(ctx context.Context, req *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {
	if err := d.ValidateControllerServiceRequest(csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME); err != nil {
		klog.Errorf("invalid create volume req: %v", req)
		return nil, err
	}

	params := req.GetParameters()
	diskParams, err := azureutils.ParseDiskParameters(params)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "Failed parsing disk parameters: %v", err)
	}

	name := req.GetName()
	if len(name) == 0 {
		return nil, status.Error(codes.InvalidArgument, "CreateVolume Name must be provided")
	}

	if !azureutils.IsValidVolumeCapabilities(volCaps, diskParams.MaxShares) {
		return nil, status.Error(codes.InvalidArgument, "Volume capability not supported")
	}

	if acquired := d.volumeLocks.TryAcquire(name); !acquired {
		return nil, status.Errorf(codes.Aborted, volumeOperationAlreadyExistsFmt, name)
	}
	defer d.volumeLocks.Release(name)

	mc := metrics.NewMetricContext(d.cloudProvisioner.GetMetricPrefix(), "controller_create_volume", d.cloudProvisioner.GetCloud().ResourceGroup, d.cloudProvisioner.GetCloud().SubscriptionID, d.Name)
	isOperationSucceeded := false
	defer func() {
		mc.ObserveOperationWithResult(isOperationSucceeded)
	}()

	volumeCaps := req.GetVolumeCapabilities()
	if len(volumeCaps) == 0 {
		return nil, status.Error(codes.InvalidArgument, "CreateVolume Volume capabilities must be provided")
	}

	params := req.GetParameters()
	maxShares, err := azureutils.GetMaxShares(params)
	if !azureutils.IsValidVolumeCapabilities(volumeCaps, maxShares) {
		return nil, status.Error(codes.InvalidArgument, "Volume capability not supported")
	}

	capRange := &diskv1alpha2.CapacityRange{
		RequiredBytes: req.GetCapacityRange().GetRequiredBytes(),
		LimitBytes:    req.GetCapacityRange().GetLimitBytes(),
	}

	volCaps := []diskv1alpha2.VolumeCapability{}

	for _, v := range volumeCaps {
		volCap := generateAzVolumeCapability(v)
		volCaps = append(volCaps, volCap)
	}

	contentVolSource := &diskv1alpha2.ContentVolumeSource{}
	reqVolumeContentSource := req.GetVolumeContentSource()
	if reqVolumeContentSource != nil {
		if reqVolumeContentSource.GetSnapshot() != nil {
			contentVolSource.ContentSource = diskv1alpha2.ContentVolumeSourceTypeSnapshot
			contentVolSource.ContentSourceID = reqVolumeContentSource.GetSnapshot().GetSnapshotId()
		} else if reqVolumeContentSource.GetVolume() != nil {
			contentVolSource.ContentSource = diskv1alpha2.ContentVolumeSourceTypeVolume
			contentVolSource.ContentSourceID = reqVolumeContentSource.GetVolume().GetVolumeId()
		}
	}

	preferredTopology, requisiteTopology := []diskv1alpha2.Topology{}, []diskv1alpha2.Topology{}
	accessibilityReqs := req.GetAccessibilityRequirements()

	for _, requisite := range accessibilityReqs.GetRequisite() {
		reqTopology := diskv1alpha2.Topology{
			Segments: requisite.GetSegments(),
		}

		requisiteTopology = append(requisiteTopology, reqTopology)
	}

	for _, preferred := range accessibilityReqs.GetPreferred() {
		prefTopology := diskv1alpha2.Topology{
			Segments: preferred.GetSegments(),
		}

		preferredTopology = append(preferredTopology, prefTopology)
	}

	accessibilityRequirement := &diskv1alpha2.TopologyRequirement{
		Requisite: requisiteTopology,
		Preferred: preferredTopology,
	}

	response, err := d.crdProvisioner.CreateVolume(ctx, name, capRange, volCaps, params, req.GetSecrets(), contentVolSource, accessibilityRequirement)

	if err != nil {
		return nil, err
	}

	if response == nil {
		return nil, status.Error(codes.Unknown, "Error creating volume")
	}

	isOperationSucceeded = true

	responseVolumeContentSource := &csi.VolumeContentSource{}

	if response.ContentSource != nil {
		if response.ContentSource.ContentSource == diskv1alpha2.ContentVolumeSourceTypeSnapshot {
			responseVolumeContentSource.Type = &csi.VolumeContentSource_Snapshot{
				Snapshot: &csi.VolumeContentSource_SnapshotSource{
					SnapshotId: response.ContentSource.ContentSourceID,
				},
			}
		} else {
			responseVolumeContentSource.Type = &csi.VolumeContentSource_Volume{
				Volume: &csi.VolumeContentSource_VolumeSource{
					VolumeId: response.ContentSource.ContentSourceID,
				},
			}
		}
	}

<<<<<<< HEAD
	responseAccessibleTopology := []*csi.Topology{}
	for _, t := range response.AccessibleTopology {
		topology := &csi.Topology{
			Segments: t.Segments,
=======
	diskParams.VolumeContext[consts.RequestedSizeGib] = strconv.Itoa(requestGiB)
	volumeOptions := &azure.ManagedDiskOptions{
		AvailabilityZone:    selectedAvailabilityZone,
		BurstingEnabled:     diskParams.EnableBursting,
		DiskEncryptionSetID: diskParams.DiskEncryptionSetID,
		DiskIOPSReadWrite:   diskParams.DiskIOPSReadWrite,
		DiskMBpsReadWrite:   diskParams.DiskMBPSReadWrite,
		DiskName:            diskParams.DiskName,
		LogicalSectorSize:   int32(diskParams.LogicalSectorSize),
		MaxShares:           int32(diskParams.MaxShares),
		PVCName:             "",
		ResourceGroup:       diskParams.ResourceGroup,
		SizeGB:              requestGiB,
		StorageAccountType:  skuName,
		SourceResourceID:    sourceID,
		SourceType:          sourceType,
		Tags:                diskParams.Tags,
	}
	// Azure Stack Cloud does not support NetworkAccessPolicy
	if !azureutils.IsAzureStackCloud(d.cloud.Config.Cloud, d.cloud.Config.DisableAzureStackCloud) {
		volumeOptions.NetworkAccessPolicy = networkAccessPolicy
		if diskParams.DiskAccessID != "" {
			volumeOptions.DiskAccessID = &diskParams.DiskAccessID
		}
	}
	diskURI, err := d.cloud.CreateManagedDisk(ctx, volumeOptions)
	if err != nil {
		if strings.Contains(err.Error(), consts.NotFound) {
			return nil, status.Error(codes.NotFound, err.Error())
>>>>>>> chore: upgrade azure lib
		}

		responseAccessibleTopology = append(responseAccessibleTopology, topology)
	}

	return &csi.CreateVolumeResponse{
		Volume: &csi.Volume{
			VolumeId:           response.VolumeID,
			CapacityBytes:      response.CapacityBytes,
			VolumeContext:      response.VolumeContext,
			ContentSource:      responseVolumeContentSource,
			AccessibleTopology: responseAccessibleTopology,
		},
	}, nil
}

// DeleteVolume delete an azure disk
func (d *DriverV2) DeleteVolume(ctx context.Context, req *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {
	volumeID := req.GetVolumeId()
	if len(volumeID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID missing in the request")
	}

	if err := d.ValidateControllerServiceRequest(csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME); err != nil {
		return nil, fmt.Errorf("invalid delete volume req: %v", req)
	}

	if acquired := d.volumeLocks.TryAcquire(volumeID); !acquired {
		return nil, status.Errorf(codes.Aborted, volumeOperationAlreadyExistsFmt, volumeID)
	}
	defer d.volumeLocks.Release(volumeID)

	mc := metrics.NewMetricContext(d.cloudProvisioner.GetMetricPrefix(), "controller_delete_volume", d.cloudProvisioner.GetCloud().ResourceGroup, d.cloudProvisioner.GetCloud().SubscriptionID, d.Name)
	isOperationSucceeded := false
	defer func() {
		mc.ObserveOperationWithResult(isOperationSucceeded)
	}()

	klog.V(2).Infof("deleting disk(%s)", volumeID)
	err := d.crdProvisioner.DeleteVolume(ctx, volumeID, req.GetSecrets())
	klog.V(2).Infof("delete disk(%s) returned with %v", volumeID, err)
	isOperationSucceeded = (err == nil)
	return &csi.DeleteVolumeResponse{}, err
}

// ControllerGetVolume get volume
func (d *DriverV2) ControllerGetVolume(context.Context, *csi.ControllerGetVolumeRequest) (*csi.ControllerGetVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

// ControllerPublishVolume attach an azure disk to a required node
func (d *DriverV2) ControllerPublishVolume(ctx context.Context, req *csi.ControllerPublishVolumeRequest) (*csi.ControllerPublishVolumeResponse, error) {
	diskURI := req.GetVolumeId()
	if len(diskURI) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID not provided")
	}

	volCap := req.GetVolumeCapability()
	if volCap == nil {
		return nil, status.Error(codes.InvalidArgument, "Volume capability not provided")
	}

	caps := []*csi.VolumeCapability{volCap}
	maxShares, err := azureutils.GetMaxShares(req.GetVolumeContext())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("invalid value specified by maxShares parameter: %s", err.Error()))
	}

	if !azureutils.IsValidVolumeCapabilities(caps, maxShares) {
		return nil, status.Error(codes.InvalidArgument, "Volume capability not supported")
	}

	nodeID := req.GetNodeId()
	if len(nodeID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Node ID not provided")
	}

	mc := metrics.NewMetricContext(d.cloudProvisioner.GetMetricPrefix(), "controller_publish_volume", d.cloudProvisioner.GetCloud().ResourceGroup, d.cloudProvisioner.GetCloud().SubscriptionID, d.Name)
	isOperationSucceeded := false
	defer func() {
		mc.ObserveOperationWithResult(isOperationSucceeded)
	}()

	volumeCapability := generateAzVolumeCapability(volCap)

	response, err := d.crdProvisioner.PublishVolume(ctx, diskURI, nodeID, &volumeCapability, req.GetReadonly(), req.GetSecrets(), req.GetVolumeContext())

	if err != nil {
		return nil, err
	}

<<<<<<< HEAD
	if response == nil {
		return nil, status.Error(codes.Unknown, "Error publishing volume")
=======
	if err == nil {
		if vmState != nil && strings.ToLower(*vmState) == "failed" {
			klog.Warningf("VM(%q) is in failed state, update VM first", nodeName)
			if err := d.cloud.UpdateVM(ctx, nodeName); err != nil {
				return nil, fmt.Errorf("update instance %q failed with %v", nodeName, err)
			}
		}
		// Volume is already attached to node.
		klog.V(2).Infof("Attach operation is successful. volume %q is already attached to node %q at lun %d.", diskURI, nodeName, lun)
	} else {
		var cachingMode compute.CachingTypes
		if cachingMode, err = azureutils.GetCachingMode(volumeContext); err != nil {
			return nil, err
		}
		klog.V(2).Infof("Trying to attach volume %q to node %q", diskURI, nodeName)

		lun, err = d.cloud.AttachDisk(ctx, true, diskName, diskURI, nodeName, cachingMode, disk)
		if err == nil {
			klog.V(2).Infof("Attach operation successful: volume %q attached to node %q.", diskURI, nodeName)
		} else {
			if derr, ok := err.(*volerr.DanglingAttachError); ok {
				klog.Warningf("volume %q is already attached to node %q, try detach first", diskURI, derr.CurrentNode)
				if err = d.cloud.DetachDisk(ctx, diskName, diskURI, derr.CurrentNode); err != nil {
					return nil, status.Errorf(codes.Internal, "Could not detach volume %q from node %q: %v", diskURI, derr.CurrentNode, err)
				}
				klog.V(2).Infof("Trying to attach volume %q to node %q again", diskURI, nodeName)
				lun, err = d.cloud.AttachDisk(ctx, true, diskName, diskURI, nodeName, cachingMode, disk)
			}
			if err != nil {
				klog.Errorf("Attach volume %q to instance %q failed with %v", diskURI, nodeName, err)
				return nil, fmt.Errorf("Attach volume %q to instance %q failed with %v", diskURI, nodeName, err)
			}
		}
		klog.V(2).Infof("attach volume %q to node %q successfully", diskURI, nodeName)
>>>>>>> chore: upgrade azure lib
	}

	isOperationSucceeded = true
	return &csi.ControllerPublishVolumeResponse{
		PublishContext: response,
	}, nil
}

// ControllerUnpublishVolume detach an azure disk from a required node
func (d *DriverV2) ControllerUnpublishVolume(ctx context.Context, req *csi.ControllerUnpublishVolumeRequest) (*csi.ControllerUnpublishVolumeResponse, error) {
	diskURI := req.GetVolumeId()
	if len(diskURI) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID not provided")
	}

	nodeID := req.GetNodeId()
	if len(nodeID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Node ID not provided")
	}

	mc := metrics.NewMetricContext(d.cloudProvisioner.GetMetricPrefix(), "controller_unpublish_volume", d.cloudProvisioner.GetCloud().ResourceGroup, d.cloudProvisioner.GetCloud().SubscriptionID, d.Name)
	isOperationSucceeded := false
	defer func() {
		mc.ObserveOperationWithResult(isOperationSucceeded)
	}()

	err := d.crdProvisioner.UnpublishVolume(ctx, diskURI, nodeID, req.GetSecrets())

	if err != nil {
		return nil, err
	}

	isOperationSucceeded = true

	return &csi.ControllerUnpublishVolumeResponse{}, nil
}

// ValidateVolumeCapabilities return the capabilities of the volume
func (d *DriverV2) ValidateVolumeCapabilities(ctx context.Context, req *csi.ValidateVolumeCapabilitiesRequest) (*csi.ValidateVolumeCapabilitiesResponse, error) {
	diskURI := req.GetVolumeId()
	if len(diskURI) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID missing in the request")
	}
	volumeCapabilities := req.GetVolumeCapabilities()
	if volumeCapabilities == nil {
		return nil, status.Error(codes.InvalidArgument, "VolumeCapabilities missing in the request")
	}

	params := req.GetParameters()
	maxShares, err := azureutils.GetMaxShares(params)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("invalid value specified by maxShares parameter: %s", err.Error()))
	}

	if !azureutils.IsValidVolumeCapabilities(volumeCapabilities, maxShares) {
		return &csi.ValidateVolumeCapabilitiesResponse{Message: "VolumeCapabilities are invalid"}, nil
	}

	if _, err := d.cloudProvisioner.CheckDiskExists(ctx, diskURI); err != nil {
		return nil, status.Error(codes.NotFound, fmt.Sprintf("Volume not found, failed with error: %v", err))

	return &csi.ValidateVolumeCapabilitiesResponse{
		Confirmed: &csi.ValidateVolumeCapabilitiesResponse_Confirmed{
			VolumeCapabilities: volumeCapabilities,
		}}, nil
}
func (d *DriverV2) ControllerGetCapabilities(ctx context.Context, req *csi.ControllerGetCapabilitiesRequest) (*csi.ControllerGetCapabilitiesResponse, error) {
	return &csi.ControllerGetCapabilitiesResponse{
		Capabilities: d.Cap,
	}, nil
}

// GetCapacity returns the capacity of the total available storage pool
func (d *DriverV2) GetCapacity(ctx context.Context, req *csi.GetCapacityRequest) (*csi.GetCapacityResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

// ListVolumes return all available volumes
func (d *DriverV2) ListVolumes(ctx context.Context, req *csi.ListVolumesRequest) (*csi.ListVolumesResponse, error) {
	start := 0
	var err error
	startingToken := req.GetStartingToken()
	if startingToken != "" {
		start, err = strconv.Atoi(startingToken)
		if err != nil {
			return nil, status.Errorf(codes.Aborted, "ListVolumes starting token(%s) parsing with error: %v", startingToken, err)
		}
		if start < 0 {
			return nil, status.Errorf(codes.Aborted, "ListVolumes starting token(%d) can not be negative", start)
		}
	}

	mc := metrics.NewMetricContext(d.cloudProvisioner.GetMetricPrefix(), "controller_list_volumes", d.cloudProvisioner.GetCloud().ResourceGroup, d.cloudProvisioner.GetCloud().SubscriptionID, d.Name)
	isOperationSucceeded := false
	defer func() {
		mc.ObserveOperationWithResult(isOperationSucceeded)
	}()

	result, err := d.cloudProvisioner.ListVolumes(ctx, req.GetMaxEntries(), startingToken)

	if err != nil {
		return nil, err
	}

	if result == nil {
		return nil, status.Error(codes.Unknown, "Error listing volumes")
	}

	responseEntries := []*csi.ListVolumesResponse_Entry{}

	for _, resultEntry := range result.Entries {
		resultVolumeDetail := resultEntry.Details
		responseContentSource := &csi.VolumeContentSource{}

		if resultVolumeDetail.ContentSource != nil {
			if resultVolumeDetail.ContentSource.ContentSource == diskv1alpha2.ContentVolumeSourceTypeSnapshot {
				responseContentSource.Type = &csi.VolumeContentSource_Snapshot{
					Snapshot: &csi.VolumeContentSource_SnapshotSource{
						SnapshotId: resultVolumeDetail.ContentSource.ContentSourceID,
					},
				}
			} else {
				responseContentSource.Type = &csi.VolumeContentSource_Volume{
					Volume: &csi.VolumeContentSource_VolumeSource{
						VolumeId: resultVolumeDetail.ContentSource.ContentSourceID,
					},
				}
			}
		}

		responseAccessibleTopology := []*csi.Topology{}
		for _, t := range resultVolumeDetail.AccessibleTopology {
			topology := &csi.Topology{
				Segments: t.Segments,
			}

			responseAccessibleTopology = append(responseAccessibleTopology, topology)
		}

		responseVolumeStatus := &csi.ListVolumesResponse_VolumeStatus{}

		if resultEntry.Status != nil {
			responseVolumeStatus.PublishedNodeIds = resultEntry.Status.PublishedNodeIds
			if resultEntry.Status.Condition != nil {
				condition := &csi.VolumeCondition{
					Abnormal: resultEntry.Status.Condition.Abnormal,
					Message:  resultEntry.Status.Condition.Message,
				}

				responseVolumeStatus.VolumeCondition = condition
			}
		}

		responseEntries = append(responseEntries, &csi.ListVolumesResponse_Entry{
			Volume: &csi.Volume{
				VolumeId:           resultVolumeDetail.VolumeID,
				CapacityBytes:      resultVolumeDetail.CapacityBytes,
				VolumeContext:      resultVolumeDetail.VolumeContext,
				ContentSource:      responseContentSource,
				AccessibleTopology: responseAccessibleTopology,
			},
			Status: responseVolumeStatus,
		})
	}

	isOperationSucceeded = true

	response := &csi.ListVolumesResponse{
		Entries:   responseEntries,
		NextToken: result.NextToken,
	}

	return response, nil
}

// ControllerExpandVolume controller expand volume
func (d *DriverV2) ControllerExpandVolume(ctx context.Context, req *csi.ControllerExpandVolumeRequest) (*csi.ControllerExpandVolumeResponse, error) {
	diskURI := req.GetVolumeId()
	if len(diskURI) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID missing in the request")
	}
	if err := d.ValidateControllerServiceRequest(csi.ControllerServiceCapability_RPC_EXPAND_VOLUME); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid expand volume request: %v", req)
	}

	capacityBytes := req.GetCapacityRange().GetRequiredBytes()
	if capacityBytes == 0 {
		return nil, status.Error(codes.InvalidArgument, "volume capacity range missing in request")
	}

	mc := metrics.NewMetricContext(d.cloudProvisioner.GetMetricPrefix(), "controller_expand_volume", d.cloudProvisioner.GetCloud().ResourceGroup, d.cloudProvisioner.GetCloud().SubscriptionID, d.Name)
	isOperationSucceeded := false
	defer func() {
		mc.ObserveOperationWithResult(isOperationSucceeded)
	}()

	capacityRange := &diskv1alpha2.CapacityRange{
		RequiredBytes: req.GetCapacityRange().GetRequiredBytes(),
		LimitBytes:    req.GetCapacityRange().GetLimitBytes(),
	}

<<<<<<< HEAD
	response, err := d.crdProvisioner.ExpandVolume(ctx, diskURI, capacityRange, req.GetSecrets())

=======
	klog.V(2).Infof("begin to expand azure disk(%s) with new size(%d)", diskURI, requestSize.Value())
	newSize, err := d.cloud.ResizeDisk(ctx, diskURI, oldSize, requestSize, d.enableDiskOnlineResize)
>>>>>>> chore: upgrade azure lib
	if err != nil {
		return nil, err
	}

	if response == nil {
		return nil, status.Error(codes.Unknown, "Error exanding volume")
	}

	isOperationSucceeded = true
	return &csi.ControllerExpandVolumeResponse{
		CapacityBytes:         response.CapacityBytes,
		NodeExpansionRequired: response.NodeExpansionRequired,
	}, nil
}

// CreateSnapshot create a snapshot
func (d *DriverV2) CreateSnapshot(ctx context.Context, req *csi.CreateSnapshotRequest) (*csi.CreateSnapshotResponse, error) {
	sourceVolumeID := req.GetSourceVolumeId()
	if len(sourceVolumeID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "CreateSnapshot Source Volume ID must be provided")
	}
	snapshotName := req.GetName()
	if len(snapshotName) == 0 {
		return nil, status.Error(codes.InvalidArgument, "snapshot name must be provided")
	}

	mc := metrics.NewMetricContext(d.cloudProvisioner.GetMetricPrefix(), "controller_create_snapshot", d.cloudProvisioner.GetCloud().ResourceGroup, d.cloudProvisioner.GetCloud().SubscriptionID, d.Name)
	isOperationSucceeded := false
	defer func() {
		mc.ObserveOperationWithResult(isOperationSucceeded)
	}()

	snapshot, err := d.cloudProvisioner.CreateSnapshot(ctx, sourceVolumeID, snapshotName, req.GetSecrets(), req.GetParameters())

	if err != nil {
		return nil, err
	}

	if snapshot == nil {
		return nil, status.Error(codes.Unknown, "Error creating snapshot")
	}

	tp, err := ptypes.TimestampProto(snapshot.CreationTime.Time)
	if err != nil {
		return nil, fmt.Errorf("Failed to covert creation timestamp: %v", err)
	}

	isOperationSucceeded = true
	return &csi.CreateSnapshotResponse{
		Snapshot: &csi.Snapshot{
			SnapshotId:     snapshot.SnapshotID,
			SourceVolumeId: snapshot.SourceVolumeID,
			CreationTime:   tp,
			ReadyToUse:     snapshot.ReadyToUse,
			SizeBytes:      snapshot.SizeBytes,
		},
	}, nil
}

// DeleteSnapshot delete a snapshot
func (d *DriverV2) DeleteSnapshot(ctx context.Context, req *csi.DeleteSnapshotRequest) (*csi.DeleteSnapshotResponse, error) {
	snapshotID := req.GetSnapshotId()
	if len(snapshotID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Snapshot ID must be provided")
	}

	mc := metrics.NewMetricContext(d.cloudProvisioner.GetMetricPrefix(), "controller_delete_snapshot", d.cloudProvisioner.GetCloud().ResourceGroup, d.cloudProvisioner.GetCloud().SubscriptionID, d.Name)
	isOperationSucceeded := false
	defer func() {
		mc.ObserveOperationWithResult(isOperationSucceeded)
	}()

	err := d.cloudProvisioner.DeleteSnapshot(ctx, snapshotID, req.GetSecrets())

	if err != nil {
		return nil, err
	}

	isOperationSucceeded = true
	return &csi.DeleteSnapshotResponse{}, nil
}

// ListSnapshots list all snapshots
func (d *DriverV2) ListSnapshots(ctx context.Context, req *csi.ListSnapshotsRequest) (*csi.ListSnapshotsResponse, error) {
	mc := metrics.NewMetricContext(d.cloudProvisioner.GetMetricPrefix(), "controller_list_snapshots", d.cloudProvisioner.GetCloud().ResourceGroup, d.cloudProvisioner.GetCloud().SubscriptionID, d.Name)
	isOperationSucceeded := false
	defer func() {
		mc.ObserveOperationWithResult(isOperationSucceeded)
	}()

	result, err := d.cloudProvisioner.ListSnapshots(ctx, req.GetMaxEntries(), req.GetStartingToken(), req.GetSourceVolumeId(), req.GetSnapshotId(), req.GetSecrets())
	if err != nil {
		return nil, err
	}

	if result == nil {
		return nil, status.Error(codes.Unknown, "Error listing volumes")
	}

	responseEntries := []*csi.ListSnapshotsResponse_Entry{}

	for _, resultEntry := range result.Entries {
		tp, err := ptypes.TimestampProto(resultEntry.CreationTime.Time)
		if err != nil {
			return nil, fmt.Errorf("Failed to covert creation timestamp: %v", err)
		}
		responseEntries = append(responseEntries, &csi.ListSnapshotsResponse_Entry{
			Snapshot: &csi.Snapshot{
				SnapshotId:     resultEntry.SnapshotID,
				SourceVolumeId: resultEntry.SourceVolumeID,
				CreationTime:   tp,
				ReadyToUse:     resultEntry.ReadyToUse,
				SizeBytes:      resultEntry.SizeBytes,
			},
		})
	}

	isOperationSucceeded = true

	return &csi.ListSnapshotsResponse{
		Entries:   responseEntries,
		NextToken: result.NextToken,
	}, nil
}

func generateAzVolumeCapability(volumeCapability *csi.VolumeCapability) diskv1alpha2.VolumeCapability {
	volCap := diskv1alpha2.VolumeCapability{
		AccessMode: diskv1alpha2.VolumeCapabilityAccessMode(volumeCapability.GetAccessMode().GetMode()),
	}

	if volumeCapability.GetMount() != nil {
		volCap.AccessType = diskv1alpha2.VolumeCapabilityAccessMount
		volCap.FsType = volumeCapability.GetMount().GetFsType()
		volCap.MountFlags = volumeCapability.GetMount().GetMountFlags()
	} else if volumeCapability.GetBlock() != nil {
		volCap.AccessType = diskv1alpha2.VolumeCapabilityAccessBlock
	}

	return volCap
}
