// Copyright 2020 Ericsson Software Technology.
//
// SPDX-License-Identifier: Apache-2.0
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at:
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package ovsforwarder

import (
	"runtime"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"

	"github.com/networkservicemesh/networkservicemesh/controlplane/api/connection"
	"github.com/networkservicemesh/networkservicemesh/controlplane/api/connection/mechanisms/kernel"
	"github.com/networkservicemesh/networkservicemesh/controlplane/api/crossconnect"
	"github.com/networkservicemesh/networkservicemesh/forwarder/kernel-forwarder/pkg/monitoring"
	. "github.com/networkservicemesh/networkservicemesh/forwarder/ovs-forwarder/pkg/ovsforwarder/remote"
	"github.com/networkservicemesh/networkservicemesh/forwarder/ovs-forwarder/pkg/ovsforwarder/sriov"
)

// handleRemoteConnection handles remote connect/disconnect requests for either incoming or outgoing connections
func (o *OvSForwarder) handleRemoteConnection(crossConnect *crossconnect.CrossConnect, connect bool) (map[string]monitoring.Device, error) {
	if crossConnect.GetSource().IsRemote() && !crossConnect.GetDestination().IsRemote() {
		/* 1. Incoming remote connection */
		logrus.Info("remote: connection type - remote source/local destination - incoming")
		return o.handleConnection(crossConnect.GetId(), crossConnect.GetDestination(), crossConnect.GetSource(), connect, INCOMING)
	} else if !crossConnect.GetSource().IsRemote() && crossConnect.GetDestination().IsRemote() {
		/* 2. Outgoing remote connection */
		logrus.Info("remote: connection type - local source/remote destination - outgoing")
		return o.handleConnection(crossConnect.GetId(), crossConnect.GetSource(), crossConnect.GetDestination(), connect, OUTGOING)
	}
	err := errors.Errorf("remote: invalid connection type")
	logrus.Errorf("%+v", err)
	return nil, err
}

// handleConnection process the request to either creating or deleting a connection
func (o *OvSForwarder) handleConnection(connID string, localConnection, remoteConnection *connection.Connection, connect bool, direction uint8) (map[string]monitoring.Device, error) {
	var devices map[string]monitoring.Device
	var err error
	if connect {
		/* 2. Create a connection */
		devices, err = o.createRemoteConnection(connID, localConnection, remoteConnection, direction)
		if err != nil {
			logrus.Errorf("remote: failed to create connection - %v", err)
			devices = nil
		}
	} else {
		/* 3. Delete a connection */
		devices, err = o.deleteRemoteConnection(connID, localConnection, remoteConnection, direction)
		if err != nil {
			logrus.Errorf("remote: failed to delete connection - %v", err)
			devices = nil
		}
	}
	return devices, err
}

// createRemoteConnection handler for creating a remote connection
func (o *OvSForwarder) createRemoteConnection(connID string, localConnection, remoteConnection *connection.Connection, direction uint8) (map[string]monitoring.Device, error) {
	logrus.Info("remote: creating connection...")

	var xconName string
	if direction == INCOMING {
		xconName = "DST-" + connID
	} else {
		xconName = "SRC-" + connID
	}
	var nsInode string
	var err error

	/* Lock the OS thread so we don't accidentally switch namespaces */
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	var deviceID, netRep string
	deviceIDs, ok := localConnection.GetMechanism().GetParameters()[kernel.PciAddresses]
	if ok {
		deviceID, netRep, err = PickDeviceAndNetRep(deviceIDs)
		if err != nil {
			return nil, err
		}
	}

	interfaceConfig, err := o.initLocalInterface(deviceID, netRep, connID, localConnection, direction == INCOMING)
	if err != nil {
		logrus.Errorf("local: %v", err)
		return nil, err
	}

	vni, ovsTunnelName, err := o.remoteConnect.CreateTunnelInterface(remoteConnection, direction)
	if err != nil {
		logrus.Errorf("remote: %v", err)
		return nil, err
	}

	ovsPortName := interfaceConfig.NetRepDevice
	ifaceName := interfaceConfig.Name
	nsInode = interfaceConfig.TargetNetns

	if err = o.remoteConnect.SetupOvSConnection(ovsPortName, ovsTunnelName, vni); err != nil {
		logrus.Errorf("remote: %v", err)
		return nil, err
	}

	if err = o.setupLocalInterface(interfaceConfig, localConnection, direction == INCOMING); err != nil {
		logrus.Errorf("remote: %v", err)
		return nil, err
	}

	DevIDMap["rem-"+connID] = deviceID

	logrus.Infof("remote: creation completed for device - %s", ifaceName)
	return map[string]monitoring.Device{nsInode: {Name: ifaceName, XconName: xconName}}, nil
}

// deleteRemoteConnection handler for deleting a remote connection
func (o *OvSForwarder) deleteRemoteConnection(connID string, localConnection, remoteConnection *connection.Connection, direction uint8) (map[string]monitoring.Device, error) {
	logrus.Info("remote: deleting connection...")

	var xconName string
	if direction == INCOMING {
		xconName = "DST-" + connID
	} else {
		xconName = "SRC-" + connID
	}

	/* Lock the OS thread so we don't accidentally switch namespaces */
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	vni, ovsTunnelName, err := o.remoteConnect.GetTunnelParameters(remoteConnection, direction)
	if err != nil {
		logrus.Errorf("remote: %v", err)
		return nil, err
	}

	var deviceID, netRep string
	deviceID, ok := DevIDMap["rem-"+connID]
	if ok {
		netRep, err = sriov.GetNetRepresentorWithRetries(deviceID, 5)
		if err != nil {
			logrus.Errorf("remote: error occured while retrieving netRep for %s, error %v", deviceID, err)
		}
	}

	var ovsPortName string
	if deviceID != "" {
		ovsPortName = netRep
	} else {
		ovsPortName = "tap_" + connID
	}
	o.remoteConnect.DeleteLocalOvSConnection(ovsPortName, ovsTunnelName, vni)

	interfaceConfig := o.releaseLocalInterface(deviceID, ovsPortName, localConnection, direction == INCOMING)
	ifaceName := interfaceConfig.Name
	nsInode := interfaceConfig.TargetNetns

	if err := o.remoteConnect.DeleteTunnelInterface(ovsTunnelName, remoteConnection); err != nil {
		logrus.Errorf("remote: %v", err)
	}

	delete(DevIDMap, "rem-"+connID)

	logrus.Infof("remote: deletion completed for device - %s", ifaceName)
	return map[string]monitoring.Device{nsInode: {Name: ifaceName, XconName: xconName}}, nil
}

// Create local interfaces for smartNIC or Kernel case
func (o *OvSForwarder) initLocalInterface(deviceID, deviceNetRep, connID string, localConnection *connection.Connection, direction bool) (*sriov.VFInterfaceConfiguration, error) {

	var vfInterfaceConfig sriov.VFInterfaceConfiguration
	ovsPortName := "tap_" + connID
	if deviceID != "" {
		vfInterfaceConfig = GetLocalConnectionConfig(localConnection, deviceID, deviceNetRep, direction)
	} else {
		vfInterfaceConfig = GetLocalConnectionConfig(localConnection, "", ovsPortName, direction)
		if err := CreateInterfaces(vfInterfaceConfig.Name, ovsPortName); err != nil {
			return nil, err
		}

	}
	return &vfInterfaceConfig, nil
}

// Configure and attach local interfaces for smartNIC and Kernel case
func (o *OvSForwarder) setupLocalInterface(vfInterfaceConfig *sriov.VFInterfaceConfiguration,
	localConnection *connection.Connection, direction bool) error {
	if vfInterfaceConfig.PciAddress != "" {
		if err := sriov.SetupVF(*vfInterfaceConfig); err != nil {
			return err
		}
	} else {
		SetInterfacesUp(vfInterfaceConfig.NetRepDevice)
		if _, err := SetupInterface(vfInterfaceConfig.Name, localConnection, direction); err != nil {
			return err
		}
	}

	return nil
}

// Release local interfaces for SmartNIC and Kernel case
func (o *OvSForwarder) releaseLocalInterface(device, ovsPortName string, localConnection *connection.Connection,
	direction bool) *sriov.VFInterfaceConfiguration {
	var vfInterfaceConfig sriov.VFInterfaceConfiguration

	if device != "" {
		vfInterfaceConfig = GetLocalConnectionConfig(localConnection, device, ovsPortName, direction)
		if err := sriov.ResetVF(vfInterfaceConfig); err != nil {
			logrus.Errorf("remote: %v", err)
		}
	} else {
		vfInterfaceConfig = GetLocalConnectionConfig(localConnection, "", ovsPortName, direction)
		if _, err := ClearInterfaceSetup(vfInterfaceConfig.Name, localConnection); err != nil {
			logrus.Errorf("remote: %v", err)
		}
		if err := DeleteInterface(vfInterfaceConfig.Name); err != nil {
			logrus.Errorf("local: %v", err)
		}
	}
	return &vfInterfaceConfig
}
