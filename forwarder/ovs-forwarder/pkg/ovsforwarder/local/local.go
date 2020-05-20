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

// Package local - controlling local mechanisms interfaces
package local

import (
	"fmt"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/vishvananda/netlink"
	"strconv"

	"github.com/networkservicemesh/networkservicemesh/controlplane/api/connection/mechanisms/kernel"
	"github.com/ovn-org/ovn-kubernetes/go-controller/pkg/util"
)

const (
	/* VETH pairs are used only for local connections(same node), so we can use a larger MTU size as there's no multi-node connection */
	cVETHMTU = 16000
)

// portMap contains mapping between of port name and its port no.
// This map is upto date most of the times when forwarding pod running.
// TODO: Do we still need to populate it while startup ? is that needed ?
var portMap = make(map[string]int)

// Connect - struct with local mechanism interfaces creation and deletion methods
type Connect struct{}

// NewConnect - creates instance of local Connect
func NewConnect() *Connect {
	return &Connect{}
}

// CreateInterfaces - creates local interfaces pair
func (c *Connect) CreateInterfaces(srcName, srcOvSPortName string) error {
	/* Create the VETH pair - host namespace */
	if err := netlink.LinkAdd(newVETH(srcName, srcOvSPortName)); err != nil {
		return errors.Errorf("failed to create VETH pair - %v", err)
	}
	return nil
}

// DeleteInterface - deletes interface
func (c *Connect) DeleteInterface(ifaceName string) error {
	/* Get a link object for the interface */
	ifaceLink, err := netlink.LinkByName(ifaceName)
	if err != nil {
		return errors.Errorf("failed to get link for %q - %v", ifaceName, err)
	}

	/* Delete the VETH pair - host namespace */
	if err := netlink.LinkDel(ifaceLink); err != nil {
		return errors.Errorf("local: failed to delete the VETH pair - %v", err)
	}
	return nil
}

// SetupLocalOvSConnection - set up the ports and flows in openvswitch for local connection
func (c *Connect) SetupLocalOvSConnection(srcOvsPort, dstOvsPort string) {
	stdout, stderr, err := util.RunOVSVsctl("--", "--may-exist", "add-port", kernel.BridgeName, srcOvsPort)
	if err != nil {
		logrus.Errorf("Failed to add port %s to %s, stdout: %q, stderr: %q,"+
			" error: %v", srcOvsPort, kernel.BridgeName, stdout, stderr, err)
	}

	stdout, stderr, err = util.RunOVSVsctl("--", "--may-exist", "add-port", kernel.BridgeName, dstOvsPort)
	if err != nil {
		logrus.Errorf("Failed to add port %s to %s, stdout: %q, stderr: %q,"+
			" error: %v", dstOvsPort, kernel.BridgeName, stdout, stderr, err)
	}

	srcPort := getInterfaceOfPort(srcOvsPort)
	dstPort := getInterfaceOfPort(dstOvsPort)
	stdout, stderr, err = util.RunOVSOfctl("add-flow", kernel.BridgeName, fmt.Sprintf("priority=100, in_port=%d,"+
		" actions=output:%d", srcPort, dstPort))
	if err != nil {
		logrus.Errorf("Failed to add flow on %s for port %s stdout: %q"+
			" stderr: %q, error: %v", kernel.BridgeName, srcOvsPort, stdout, stderr, err)
	} else {
		portMap[srcOvsPort] = srcPort
	}

	stdout, stderr, err = util.RunOVSOfctl("add-flow", kernel.BridgeName, fmt.Sprintf("priority=100, in_port=%d,"+
		" actions=output:%d", dstPort, srcPort))
	if err != nil {
		logrus.Errorf("Failed to add flow on %s for port %s stdout: %q"+
			" stderr: %q, error: %v", kernel.BridgeName, dstOvsPort, stdout, stderr, err)
	} else {
		portMap[dstOvsPort] = dstPort
	}
}

func getInterfaceOfPort(interfaceName string) int {
	ofPort, stderr, err := util.RunOVSVsctl("--if-exists", "get", "interface", interfaceName, "ofport")
	if err != nil {
		logrus.Errorf("Failed to get ofport of %s, stderr: %q, error: %v",
			interfaceName, stderr, err)
		return -1
	}
	portNo, err := strconv.Atoi(ofPort)
	return portNo
}

// DeleteLocalOvSConnection - delete the ports and flows in openvswitch created for local connection
func (c *Connect) DeleteLocalOvSConnection(srcOvsPort, dstOvsPort string) {
	srcPort := portMap[srcOvsPort]
	defer delete(portMap, srcOvsPort)
	dstPort := portMap[dstOvsPort]
	defer delete(portMap, dstOvsPort)

	stdout, stderr, err := util.RunOVSOfctl("del-flows", kernel.BridgeName, fmt.Sprintf("in_port=%d", srcPort))
	if err != nil {
		logrus.Errorf("Failed to delete flow on %s for port "+
			"%s, stdout: %q, stderr: %q, error: %v", kernel.BridgeName, srcOvsPort, stdout, stderr, err)
	}

	stdout, stderr, err = util.RunOVSOfctl("del-flows", kernel.BridgeName, fmt.Sprintf("in_port=%d", dstPort))
	if err != nil {
		logrus.Errorf("Failed to delete flow on %s for port "+
			"%s, stdout: %q, stderr: %q, error: %v", kernel.BridgeName, dstOvsPort, stdout, stderr, err)
	}

	stdout, stderr, err = util.RunOVSVsctl("del-port", kernel.BridgeName, srcOvsPort)
	if err != nil {
		logrus.Errorf("Failed to delete port %s from %s, stdout: %q, stderr: %q,"+
			" error: %v", srcOvsPort, kernel.BridgeName, stdout, stderr, err)
	}

	stdout, stderr, err = util.RunOVSVsctl("del-port", kernel.BridgeName, dstOvsPort)
	if err != nil {
		logrus.Errorf("Failed to delete port %s from %s, stdout: %q, stderr: %q,"+
			" error: %v", dstOvsPort, kernel.BridgeName, stdout, stderr, err)
	}
}

func newVETH(srcName, dstName string) *netlink.Veth {
	/* Populate the VETH interface configuration */
	return &netlink.Veth{
		LinkAttrs: netlink.LinkAttrs{
			Name: srcName,
			MTU:  cVETHMTU,
		},
		PeerName: dstName,
	}
}
