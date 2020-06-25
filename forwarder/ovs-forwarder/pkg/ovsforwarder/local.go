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

	"github.com/sirupsen/logrus"

	"github.com/networkservicemesh/networkservicemesh/controlplane/api/connection"
	"github.com/networkservicemesh/networkservicemesh/controlplane/api/connection/mechanisms/kernel"
	"github.com/networkservicemesh/networkservicemesh/controlplane/api/crossconnect"
	"github.com/networkservicemesh/networkservicemesh/forwarder/kernel-forwarder/pkg/monitoring"
	"github.com/networkservicemesh/networkservicemesh/forwarder/ovs-forwarder/pkg/ovsforwarder/sriov"
)

const (
	srcPrefix = "tapsrc"
	dstPrefix = "tapdst"
)

// handleLocalConnection either creates or deletes a local connection - same host
func (o *OvSForwarder) handleLocalConnection(crossConnect *crossconnect.CrossConnect, connect bool) (map[string]monitoring.Device, error) {
	logrus.Info("local: connection type - local source/local destination")
	var devices map[string]monitoring.Device
	var err error
	if connect {
		/* 2. Create a connection */
		devices, err = o.createLocalConnection(crossConnect)
		if err != nil {
			logrus.Errorf("local: failed to create connection - %v", err)
			devices = nil
		}
	} else {
		/* 3. Delete a connection */
		devices, err = o.deleteLocalConnection(crossConnect)
		if err != nil {
			logrus.Errorf("local: failed to delete connection - %v", err)
			devices = nil
		}
	}
	return devices, err
}

func (o *OvSForwarder) initInterface(deviceID, deviceNetRep string, crossConnect *crossconnect.CrossConnect,
	isDst bool) (*sriov.VFInterfaceConfiguration, error) {
	var ovsPortName string
	var vfInterfaceConfig sriov.VFInterfaceConfiguration
	var conn *connection.Connection
	if isDst {
		conn = crossConnect.GetDestination()
		ovsPortName = dstPrefix + crossConnect.GetId()
	} else {
		conn = crossConnect.GetSource()
		ovsPortName = srcPrefix + crossConnect.GetId()
	}
	if deviceID != "" {
		vfInterfaceConfig = GetLocalConnectionConfig(conn, deviceNetRep, isDst)
		if err := sriov.SetupVF(vfInterfaceConfig); err != nil {
			return nil, err
		}
	} else {
		vfInterfaceConfig = GetLocalConnectionConfig(conn, ovsPortName, isDst)
		if err := CreateInterfaces(vfInterfaceConfig.Name, ovsPortName); err != nil {
			return nil, err
		}
		SetInterfacesUp(ovsPortName)
		if _, err := SetupInterface(vfInterfaceConfig.Name, conn, isDst); err != nil {
			return nil, err
		}
	}
	return &vfInterfaceConfig, nil
}

func (o *OvSForwarder) releaseInterface(device, ovsPortName string, crossConnect *crossconnect.CrossConnect,
	isDst bool) *sriov.VFInterfaceConfiguration {
	var vfInterfaceConfig sriov.VFInterfaceConfiguration
	var conn *connection.Connection
	if isDst {
		conn = crossConnect.GetDestination()
	} else {
		conn = crossConnect.GetSource()
	}
	if device != "" {
		vfInterfaceConfig = GetLocalConnectionConfig(conn, ovsPortName, isDst)
		if err := sriov.ResetVF(vfInterfaceConfig); err != nil {
			logrus.Errorf("local: %v", err)
		}
	} else {
		vfInterfaceConfig = GetLocalConnectionConfig(conn, ovsPortName, isDst)
		if _, err := ClearInterfaceSetup(vfInterfaceConfig.Name, conn); err != nil {
			logrus.Errorf("local: %v", err)
		}
		if err := DeleteInterface(ovsPortName); err != nil {
			logrus.Errorf("local: %v", err)
		}
	}
	return &vfInterfaceConfig
}

// createLocalConnection handles creating a local connection
func (o *OvSForwarder) createLocalConnection(crossConnect *crossconnect.CrossConnect) (map[string]monitoring.Device, error) {
	logrus.Info("local: creating connection...")
	/* Lock the OS thread so we don't accidentally switch namespaces */
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	var srcNetRep, dstNetRep string
	var err error
	srcDeviceID, isPresent := crossConnect.GetSource().GetMechanism().GetParameters()[kernel.PciAddress]
	if isPresent {
		if srcNetRep, err = sriov.GetNetRepresentor(srcDeviceID); err != nil {
			return nil, err
		}
	}
	dstDeviceID, isPresent := crossConnect.GetDestination().GetMechanism().GetParameters()[kernel.PciAddress]
	if isPresent {
		if dstNetRep, err = sriov.GetNetRepresentor(dstDeviceID); err != nil {
			return nil, err
		}
	}

	interfaceConfig, err := o.initInterface(srcDeviceID, srcNetRep, crossConnect, false)
	if err != nil {
		logrus.Errorf("local: %v", err)
		return nil, err

	}
	srcName := interfaceConfig.Name
	srcOvSPortName := interfaceConfig.NetRepDevice
	srcNetNsInode := interfaceConfig.TargetNetns

	interfaceConfig, err = o.initInterface(dstDeviceID, dstNetRep, crossConnect, true)
	if err != nil {
		logrus.Errorf("local: %v", err)
		return nil, err

	}
	dstName := interfaceConfig.Name
	dstOvSPortName := interfaceConfig.NetRepDevice
	dstNetNsInode := interfaceConfig.TargetNetns

	if err = o.localConnect.SetupLocalOvSConnection(srcOvSPortName, dstOvSPortName); err != nil {
		logrus.Errorf("local: %v", err)
		return nil, err
	}

	logrus.Infof("local: creation completed for devices - source: %s, destination: %s", srcName, dstName)

	srcDevice := monitoring.Device{Name: srcName, XconName: "SRC-" + crossConnect.GetId()}
	dstDevice := monitoring.Device{Name: dstName, XconName: "DST-" + crossConnect.GetId()}
	return map[string]monitoring.Device{srcNetNsInode: srcDevice, dstNetNsInode: dstDevice}, nil
}

// deleteLocalConnection handles deleting a local connection
func (o *OvSForwarder) deleteLocalConnection(crossConnect *crossconnect.CrossConnect) (map[string]monitoring.Device, error) {
	logrus.Info("local: deleting connection...")
	/* Lock the OS thread so we don't accidentally switch namespaces */
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	var srcNetRep, dstNetRep string
	srcDeviceID, isPresent := crossConnect.GetSource().GetMechanism().GetParameters()[kernel.PciAddress]
	if isPresent {
		srcNetRep, _ = sriov.GetNetRepresentor(srcDeviceID)
	}
	dstDeviceID, isPresent := crossConnect.GetDestination().GetMechanism().GetParameters()[kernel.PciAddress]
	if isPresent {
		dstNetRep, _ = sriov.GetNetRepresentor(dstDeviceID)
	}

	var srcOvSPortName, dstOvSPortName string
	if srcDeviceID != "" {
		srcOvSPortName = srcNetRep
	} else {
		srcOvSPortName = srcPrefix + crossConnect.GetId()
	}
	if dstDeviceID != "" {
		dstOvSPortName = dstNetRep
	} else {
		dstOvSPortName = dstPrefix + crossConnect.GetId()
	}

	o.localConnect.DeleteLocalOvSConnection(srcOvSPortName, dstOvSPortName)

	interfaceConfig := o.releaseInterface(srcDeviceID, srcOvSPortName, crossConnect, false)
	srcName := interfaceConfig.Name
	srcNetNsInode := interfaceConfig.TargetNetns

	interfaceConfig = o.releaseInterface(dstDeviceID, dstOvSPortName, crossConnect, true)
	dstName := interfaceConfig.Name
	dstNetNsInode := interfaceConfig.TargetNetns

	logrus.Infof("local: deletion completed for devices - source: %s, destination: %s", srcName, dstName)
	srcDevice := monitoring.Device{Name: srcName, XconName: "SRC-" + crossConnect.GetId()}
	dstDevice := monitoring.Device{Name: dstName, XconName: "DST-" + crossConnect.GetId()}
	return map[string]monitoring.Device{srcNetNsInode: srcDevice, dstNetNsInode: dstDevice}, nil
}
