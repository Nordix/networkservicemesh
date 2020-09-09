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

	"github.com/sirupsen/logrus"

	"github.com/networkservicemesh/networkservicemesh/controlplane/api/connection/mechanisms/kernel"
	. "github.com/networkservicemesh/networkservicemesh/forwarder/ovs-forwarder/pkg/ovsforwarder/ovsutils"
	"github.com/ovn-org/ovn-kubernetes/go-controller/pkg/util"
)

// Connect - struct with local mechanism interfaces creation and deletion methods
type Connect struct{}

// NewConnect - creates instance of local Connect
func NewConnect() *Connect {
	return &Connect{}
}

// SetupLocalOvSConnection - set up the ports and flows in openvswitch for local connection
func (c *Connect) SetupLocalOvSConnection(srcOvsPort, dstOvsPort string) error {
	stdout, stderr, err := util.RunOVSVsctl("--", "--may-exist", "add-port", kernel.BridgeName, srcOvsPort)
	if err != nil {
		logrus.Errorf("Failed to add port %s to %s, stdout: %q, stderr: %q,"+
			" error: %v", srcOvsPort, kernel.BridgeName, stdout, stderr, err)
		return err
	}

	stdout, stderr, err = util.RunOVSVsctl("--", "--may-exist", "add-port", kernel.BridgeName, dstOvsPort)
	if err != nil {
		logrus.Errorf("Failed to add port %s to %s, stdout: %q, stderr: %q,"+
			" error: %v", dstOvsPort, kernel.BridgeName, stdout, stderr, err)
		return err
	}

	srcPort, err := GetInterfaceOfPort(srcOvsPort)
	if err != nil {
		logrus.Errorf("Failed to get OVS port number for %s interface,"+
			" error: %v", srcOvsPort, err)
		return err
	}
	dstPort, err := GetInterfaceOfPort(dstOvsPort)
	if err != nil {
		logrus.Errorf("Failed to get OVS port number for %s interface,"+
			" error: %v", dstOvsPort, err)
		return err
	}

	stdout, stderr, err = util.RunOVSOfctl("add-flow", kernel.BridgeName, fmt.Sprintf("priority=100, in_port=%d,"+
		" actions=output:%d", srcPort, dstPort))
	if err != nil {
		logrus.Errorf("Failed to add flow on %s for port %s stdout: %s"+
			" stderr: %s, error: %v", kernel.BridgeName, srcOvsPort, stdout, stderr, err)
		return err
	} else {
		PortMap[srcOvsPort] = srcPort
	}

	if stderr != "" {
		logrus.Errorf("Failed to add flow on %s for port %s stdout: %s"+
			" stderr: %s", kernel.BridgeName, srcOvsPort, stdout, stderr)
	}

	stdout, stderr, err = util.RunOVSOfctl("add-flow", kernel.BridgeName, fmt.Sprintf("priority=100, in_port=%d,"+
		" actions=output:%d", dstPort, srcPort))
	if err != nil {
		logrus.Errorf("Failed to add flow on %s for port %s stdout: %s"+
			" stderr: %s, error: %v", kernel.BridgeName, dstOvsPort, stdout, stderr, err)
		return err
	} else {
		PortMap[dstOvsPort] = dstPort
	}

	if stderr != "" {
		logrus.Errorf("Failed to add flow on %s for port %s stdout: %s"+
			" stderr: %s", kernel.BridgeName, dstOvsPort, stdout, stderr)
	}

	return nil
}

// DeleteLocalOvSConnection - delete the ports and flows in openvswitch created for local connection
func (c *Connect) DeleteLocalOvSConnection(srcOvsPort, dstOvsPort string) {
	srcPort := PortMap[srcOvsPort]
	defer delete(PortMap, srcOvsPort)
	dstPort := PortMap[dstOvsPort]
	defer delete(PortMap, dstOvsPort)

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
