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

package remote

import (
	"net"
	"strings"
	"strconv"

	"github.com/pkg/errors"

	"github.com/networkservicemesh/networkservicemesh/controlplane/api/connection"
	"github.com/networkservicemesh/networkservicemesh/controlplane/api/connection/mechanisms/vxlan"
	"github.com/ovn-org/ovn-kubernetes/go-controller/pkg/util"
	"github.com/networkservicemesh/networkservicemesh/controlplane/api/connection/mechanisms/kernel"
	. "github.com/networkservicemesh/networkservicemesh/forwarder/ovs-forwarder/pkg/ovsforwarder/ovsutils"
)

// CreateVXLANInterface creates a VXLAN interface
func (c *Connect) createVXLANInterface(remoteConnection *connection.Connection, direction uint8) (int, string, error) {
	/* Create interface - host namespace */
	srcIP := net.ParseIP(remoteConnection.GetMechanism().GetParameters()[vxlan.SrcIP])
	dstIP := net.ParseIP(remoteConnection.GetMechanism().GetParameters()[vxlan.DstIP])
	vni, _ := strconv.Atoi(remoteConnection.GetMechanism().GetParameters()[vxlan.VNI])

	var localIP net.IP
	var remoteIP net.IP
	if direction == INCOMING {
		localIP = dstIP
		remoteIP = srcIP
	} else {
		localIP = srcIP
		remoteIP = dstIP
	}
	ovsTunnelName :="v"+strings.ReplaceAll(remoteIP.String(), ".", "")
	c.vxlanInterfacesMutex.Lock()
	defer c.vxlanInterfacesMutex.Unlock()
	if _, exists := c.vxlanInterfaces[ovsTunnelName]; !exists{
		if err := newVXLAN(ovsTunnelName, localIP, remoteIP); err != nil {
			return 0, "", errors.Wrapf(err, "failed to create VXLAN interface")
		}
	}
	c.vxlanInterfaces[ovsTunnelName] += 1
	return vni, ovsTunnelName, nil
}

func (c *Connect) getVXLANParameters(remoteConnection *connection.Connection, direction uint8) (int, string) {
	srcIP := net.ParseIP(remoteConnection.GetMechanism().GetParameters()[vxlan.SrcIP])
	dstIP := net.ParseIP(remoteConnection.GetMechanism().GetParameters()[vxlan.DstIP])
	vni, _ := strconv.Atoi(remoteConnection.GetMechanism().GetParameters()[vxlan.VNI])

	var remoteIP net.IP
	if direction == INCOMING {
		remoteIP = srcIP
	} else {
		remoteIP = dstIP
	}
	ovsTunnelName :="v"+strings.ReplaceAll(remoteIP.String(), ".", "")
	
	return vni, ovsTunnelName
}

func (c *Connect) deleteVXLANInterface(ovsTunnelName string) error {
	c.vxlanInterfacesMutex.Lock()
	defer c.vxlanInterfacesMutex.Unlock()
	if counter := c.vxlanInterfaces[ovsTunnelName]; counter == 1 {
		if err := deleteVXLAN(ovsTunnelName); err != nil {
			return errors.Wrapf(err, "failed to delete VXLAN interface")
		}
		delete(PortMap, ovsTunnelName)
		delete(c.vxlanInterfaces, ovsTunnelName)
	} else {
		if exists := c.vxlanInterfaces[ovsTunnelName]; exists != 0 {
			c.vxlanInterfaces[ovsTunnelName] -= 1
		}
	}

	return nil
}

// newVXLAN creates a VXLAN interface instance in OVS
func newVXLAN(ovsTunnelName string, egressIP, remoteIP net.IP) error {
	/* Populate the VXLAN interface configuration */
	localOptions := "options:local_ip="+egressIP.String()
    remoteOptions :="options:remote_ip="+remoteIP.String()
	stdout, stderr, err := util.RunOVSVsctl("--", "--may-exist", "add-port", kernel.BridgeName, ovsTunnelName,
											"--", "set", "interface", ovsTunnelName, "type=vxlan",localOptions,
											remoteOptions, "options:key=flow")
	if err != nil {
		return errors.Errorf("Failed to add port %s to %s, stdout: %q, stderr: %q,"+
								" error: %v", ovsTunnelName, kernel.BridgeName, stdout, stderr, err)
	}

	return nil

}

func deleteVXLAN(ovsTunnelPort string) error {
	/* Populate the VXLAN interface configuration */
	stdout, stderr, err := util.RunOVSVsctl("del-port", kernel.BridgeName, ovsTunnelPort)
	if err != nil {
		return errors.Errorf("Failed to delete port %s to %s, stdout: %q, stderr: %q,"+
								" error: %v", ovsTunnelPort, kernel.BridgeName, stdout, stderr, err)
	}

	return nil

}
