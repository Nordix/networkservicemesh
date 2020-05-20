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

// Package remote - controlling remote mechanisms interfaces
package remote

import (
	"sync"
	"fmt"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"

	"github.com/networkservicemesh/networkservicemesh/controlplane/api/connection"
	"github.com/networkservicemesh/networkservicemesh/controlplane/api/connection/mechanisms/vxlan"
	"github.com/ovn-org/ovn-kubernetes/go-controller/pkg/util"
	"github.com/networkservicemesh/networkservicemesh/controlplane/api/connection/mechanisms/kernel"
	. "github.com/networkservicemesh/networkservicemesh/forwarder/ovs-forwarder/pkg/ovsforwarder/ovsutils"
)

// INCOMING, OUTGOING - packet direction constants
const (
	INCOMING = iota
	OUTGOING = iota
)

// Connect - struct with remote mechanism interfaces creation and deletion methods
type Connect struct {
	vxlanInterfacesMutex  sync.Mutex
	vxlanInterfaces 	  map[string]int
}

// NewConnect - creates instance of remote Connect
func NewConnect() *Connect {
	return &Connect{
		vxlanInterfaces:   make(map[string]int),
	}
}

//CreateTunnelInterface - creates tunnel interface to the OVS switch
func (c *Connect) CreateTunnelInterface(remoteConnection *connection.Connection, direction uint8) (int, string, error) {
	switch remoteConnection.GetMechanism().GetType() {
	case vxlan.MECHANISM:
		return c.createVXLANInterface(remoteConnection, direction)
	}
	return 0, "", errors.Errorf("unknown remote mechanism - %v", remoteConnection.GetMechanism().GetType())
}

func (c *Connect) GetTunnelParameters(remoteConnection *connection.Connection, direction uint8) (int, string, error) {
	switch remoteConnection.GetMechanism().GetType() {
	case vxlan.MECHANISM:
		vni, ovsTunnelName := c.getVXLANParameters(remoteConnection, direction)
		return vni, ovsTunnelName, nil
	}
	return 0, "", errors.Errorf("unknown remote mechanism - %v", remoteConnection.GetMechanism().GetType())
}

// SetupLocalOvSConnection - set up the ports and flows in openvswitch for local connection
func (c *Connect) SetupOvSConnection(ovsLocalPort, ovsTunnelPort string, vni int) error {
	stdout, stderr, err := util.RunOVSVsctl("--", "--may-exist", "add-port", kernel.BridgeName, ovsLocalPort)
	if err != nil {
		fmt.Printf("Failed to add port %s to %s, stdout: %q, stderr: %q,"+
			" error: %v", ovsLocalPort, kernel.BridgeName, stdout, stderr, err)
		return err
	}
	ovsLocalPortNum, err := GetInterfaceOfPort(ovsLocalPort)
	if err != nil {
		logrus.Errorf("Failed to get OVS port number for %s interface,"+ 
					  " error: %v", ovsLocalPort, err)
		return err
	}
	ovsTunnelPortNum, err := GetInterfaceOfPort(ovsTunnelPort)
	if err != nil {
		logrus.Errorf("Failed to get OVS port number for %s interface,"+ 
					  " error: %v", ovsTunnelPort, err)
		return err
	}

	stdout, stderr, err = util.RunOVSOfctl("add-flow", kernel.BridgeName, fmt.Sprintf("priority=100, in_port=%d, actions=set_field:%d->tun_id,output:%d",
											ovsLocalPortNum,vni, ovsTunnelPortNum))
	if err != nil {
		fmt.Printf("Failed to add flow on %s for port %s stdout: %q"+
			" stderr: %q, error: %v", kernel.BridgeName, ovsLocalPort, stdout, stderr, err)
		return err
	} else {
		PortMap[ovsLocalPort] = ovsLocalPortNum
	}

	stdout, stderr, err = util.RunOVSOfctl("add-flow", kernel.BridgeName, fmt.Sprintf("priority=100, in_port=%d, "+
	"tun_id=%d,actions=output:%d", ovsTunnelPortNum,vni, ovsLocalPortNum))
	if err != nil {
		fmt.Printf("Failed to add flow on %s for port %s stdout: %q"+
			" stderr: %q, error: %v", kernel.BridgeName, ovsTunnelPort, stdout, stderr, err)
		return err
	} else {
		PortMap[ovsTunnelPort] = ovsTunnelPortNum
	}
	return nil
}

// DeleteLocalOvSConnection - delete the ports and flows in openvswitch created for local connection
func (c *Connect) DeleteLocalOvSConnection(ovsLocalPort, ovsTunnelPort string, vni int) {
	defer delete(PortMap, ovsLocalPort)

	ovsLocalPortNum := PortMap[ovsLocalPort]

	stdout, stderr, err := util.RunOVSOfctl("del-flows", kernel.BridgeName, fmt.Sprintf("in_port=%d", ovsLocalPortNum))
	if err != nil {
		logrus.Errorf("Failed to delete flow on %s for port "+
			"%s, stdout: %q, stderr: %q, error: %v", kernel.BridgeName, ovsLocalPort, stdout, stderr, err)
	}
	if exists := PortMap[ovsTunnelPort]; exists != 0{
		ovsTunnelPortNum := PortMap[ovsTunnelPort]
		stdout, stderr, err = util.RunOVSOfctl("del-flows", kernel.BridgeName, fmt.Sprintf("in_port=%d,tun_id=%d", ovsTunnelPortNum, vni))
		if err != nil {
			logrus.Errorf("Failed to delete flow on %s for port "+
				"%s on VNI %d, stdout: %q, stderr: %q, error: %v", kernel.BridgeName, ovsTunnelPort,vni, stdout, stderr, err)
		}
	}

	stdout, stderr, err = util.RunOVSVsctl("del-port", kernel.BridgeName, ovsLocalPort)
	if err != nil {
		logrus.Errorf("Failed to delete port %s from %s, stdout: %q, stderr: %q,"+
			" error: %v", ovsLocalPort, kernel.BridgeName, stdout, stderr, err)
	}
}

func (c *Connect) DeleteTunnelInterface(ovsTunnelName string, remoteConnection *connection.Connection) error {
	switch remoteConnection.GetMechanism().GetType() {
	case vxlan.MECHANISM:
		return c.deleteVXLANInterface(ovsTunnelName)
	}
	return errors.Errorf("unknown remote mechanism - %v", remoteConnection.GetMechanism().GetType())
}