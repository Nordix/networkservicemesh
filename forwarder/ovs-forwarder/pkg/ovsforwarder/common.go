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
	"net"
	"strings"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"

	"github.com/networkservicemesh/networkservicemesh/controlplane/api/connection"
	"github.com/networkservicemesh/networkservicemesh/controlplane/api/connection/mechanisms/common"
	"github.com/networkservicemesh/networkservicemesh/controlplane/api/connection/mechanisms/kernel"
	"github.com/networkservicemesh/networkservicemesh/controlplane/api/connectioncontext"
	"github.com/networkservicemesh/networkservicemesh/forwarder/ovs-forwarder/pkg/ovsforwarder/sriov"
	"github.com/networkservicemesh/networkservicemesh/utils/fs"
	. "github.com/networkservicemesh/networkservicemesh/forwarder/ovs-forwarder/pkg/ovsforwarder/ovsutils"
)

const (
	cVETHMTU = 16000
)

var DevIDMap = make(map[string]string)

// SetupInterface - setup interface to namespace
func SetupInterface(ifaceName string, conn *connection.Connection, isDst bool) (string, error) {
	netNsInode := conn.GetMechanism().GetParameters()[common.NetNsInodeKey]
	neighbors := conn.GetContext().GetIpContext().GetIpNeighbors()
	var ifaceIP string
	var routes []*connectioncontext.Route
	if isDst {
		ifaceIP = conn.GetContext().GetIpContext().GetDstIpAddr()
		routes = conn.GetContext().GetIpContext().GetSrcRoutes()
	} else {
		ifaceIP = conn.GetContext().GetIpContext().GetSrcIpAddr()
		routes = conn.GetContext().GetIpContext().GetDstRoutes()
	}

	/* Get namespace handler - source */
	nsHandle, err := fs.GetNsHandleFromInode(netNsInode)
	if err != nil {
		logrus.Errorf("local: failed to get source namespace handle - %v", err)
		return netNsInode, err
	}
	/* If successful, don't forget to close the handler upon exit */
	defer func() {
		if err = nsHandle.Close(); err != nil {
			logrus.Error("local: error when closing source handle: ", err)
		}
		logrus.Debug("local: closed source handle: ", nsHandle, netNsInode)
	}()
	logrus.Debug("local: opened source handle: ", nsHandle, netNsInode)

	/* Setup interface - source namespace */
	if err = setupLinkInNs(nsHandle, ifaceName, ifaceIP, routes, neighbors, true); err != nil {
		logrus.Errorf("local: failed to setup interface - source - %q: %v", ifaceName, err)
		return netNsInode, err
	}

	return netNsInode, nil
}

// ClearInterfaceSetup - deletes interface setup
func ClearInterfaceSetup(ifaceName string, conn *connection.Connection) (string, error) {
	netNsInode := conn.GetMechanism().GetParameters()[common.NetNsInodeKey]
	ifaceIP := conn.GetContext().GetIpContext().GetSrcIpAddr()

	/* Get namespace handler - source */
	nsHandle, err := fs.GetNsHandleFromInode(netNsInode)
	if err != nil {
		return "", errors.Errorf("failed to get source namespace handle - %v", err)
	}
	/* If successful, don't forget to close the handler upon exit */
	defer func() {
		if err = nsHandle.Close(); err != nil {
			logrus.Error("local: error when closing source handle: ", err)
		}
		logrus.Debug("local: closed source handle: ", nsHandle, netNsInode)
	}()
	logrus.Debug("local: opened source handle: ", nsHandle, netNsInode)

	/* Extract interface - source namespace */
	if err = setupLinkInNs(nsHandle, ifaceName, ifaceIP, nil, nil, false); err != nil {
		return "", errors.Errorf("failed to extract interface - source - %q: %v", ifaceName, err)
	}

	return netNsInode, nil
}

// SetInterfacesUp - make the interfaces state to up
func SetInterfacesUp(ifaceNames ...string) error {
	for _, ifaceName := range ifaceNames {
		/* Get a link for the interface name */
		link, err := netlink.LinkByName(ifaceName)
		if err != nil {
			logrus.Errorf("local: failed to lookup %q, %v", ifaceName, err)
			return err
		}
		/* Bring the interface Up */
		if err = netlink.LinkSetUp(link); err != nil {
			logrus.Errorf("local: failed to bring %q up: %v", ifaceName, err)
			return err
		}
	}
	return nil
}

// setupLinkInNs is responsible for configuring an interface inside a given namespace - assigns IP address, routes, etc.
func setupLinkInNs(containerNs netns.NsHandle, ifaceName, ifaceIP string, routes []*connectioncontext.Route, neighbors []*connectioncontext.IpNeighbor, inject bool) error {
	if inject {
		/* Get a link object for the interface */
		ifaceLink, err := netlink.LinkByName(ifaceName)
		if err != nil {
			logrus.Errorf("common: failed to get link for %q - %v", ifaceName, err)
			return err
		}
		/* Inject the interface into the desired namespace */
		if err = netlink.LinkSetNsFd(ifaceLink, int(containerNs)); err != nil {
			logrus.Errorf("common: failed to inject %q in namespace - %v", ifaceName, err)
			return err
		}
	}
	/* Save current network namespace */
	hostNs, err := netns.Get()
	if err != nil {
		logrus.Errorf("common: failed getting host namespace: %v", err)
		return err
	}
	logrus.Debug("common: host namespace: ", hostNs)
	defer func() {
		if err = hostNs.Close(); err != nil {
			logrus.Error("common: failed closing host namespace handle: ", err)
		}
		logrus.Debug("common: closed host namespace handle: ", hostNs)
	}()

	/* Switch to the desired namespace */
	if err = netns.Set(containerNs); err != nil {
		logrus.Errorf("common: failed switching to desired namespace: %v", err)
		return err
	}
	logrus.Debug("common: switched to desired namespace: ", containerNs)

	/* Don't forget to switch back to the host namespace */
	defer func() {
		if err = netns.Set(hostNs); err != nil {
			logrus.Errorf("common: failed switching back to host namespace: %v", err)
		}
		logrus.Debug("common: switched back to host namespace: ", hostNs)
	}()

	/* Get a link for the interface name */
	link, err := netlink.LinkByName(ifaceName)
	if err != nil {
		logrus.Errorf("common: failed to lookup %q, %v", ifaceName, err)
		return err
	}
	if inject {
		var addr *netlink.Addr
		/* Parse the IP address */
		addr, err = netlink.ParseAddr(ifaceIP)
		if err != nil {
			logrus.Errorf("common: failed to parse IP %q: %v", ifaceIP, err)
			return err
		}
		/* Set IP address */
		if err = netlink.AddrAdd(link, addr); err != nil {
			logrus.Errorf("common: failed to set IP %q: %v", ifaceIP, err)
			return err
		}
		/* Bring the interface UP */
		if err = netlink.LinkSetUp(link); err != nil {
			logrus.Errorf("common: failed to bring %q up: %v", ifaceName, err)
			return err
		}
		/* Add routes */
		if err = addRoutes(link, addr, routes); err != nil {
			logrus.Error("common: failed adding routes:", err)
			return err
		}
		/* Add neighbors - applicable only for source side */
		if err = addNeighbors(link, neighbors); err != nil {
			logrus.Error("common: failed adding neighbors:", err)
			return err
		}
	} else {
		/* Bring the interface DOWN */
		if err = netlink.LinkSetDown(link); err != nil {
			logrus.Errorf("common: failed to bring %q down: %v", ifaceName, err)
			return err
		}
		/* Inject the interface back to current namespace */
		if err = netlink.LinkSetNsFd(link, int(hostNs)); err != nil {
			logrus.Errorf("common: failed to inject %q back to host namespace - %v", ifaceName, err)
			return err
		}
	}
	return nil
}

// addRoutes adds routes
func addRoutes(link netlink.Link, addr *netlink.Addr, routes []*connectioncontext.Route) error {
	for _, route := range routes {
		_, routeNet, err := net.ParseCIDR(route.GetPrefix())
		if err != nil {
			logrus.Error("common: failed parsing route CIDR:", err)
			return err
		}
		route := netlink.Route{
			LinkIndex: link.Attrs().Index,
			Dst: &net.IPNet{
				IP:   routeNet.IP,
				Mask: routeNet.Mask,
			},
			Src: addr.IP,
		}
		if err = netlink.RouteAdd(&route); err != nil {
			logrus.Error("common: failed adding routes:", err)
			return err
		}
	}
	return nil
}

// addNeighbors adds neighbors
func addNeighbors(link netlink.Link, neighbors []*connectioncontext.IpNeighbor) error {
	for _, neighbor := range neighbors {
		mac, err := net.ParseMAC(neighbor.GetHardwareAddress())
		if err != nil {
			logrus.Error("common: failed parsing the MAC address for IP neighbors:", err)
			return err
		}
		neigh := netlink.Neigh{
			LinkIndex:    link.Attrs().Index,
			State:        0x02, // netlink.NUD_REACHABLE, // the constant is somehow not being found in the package in case of using a darwin based machine
			IP:           net.ParseIP(neighbor.GetIp()),
			HardwareAddr: mac,
		}
		if err = netlink.NeighAdd(&neigh); err != nil {
			logrus.Error("common: failed adding neighbor:", err)
			return err
		}
	}
	return nil
}

// CreateInterfaces - creates local interfaces pair
func CreateInterfaces(srcName, srcOvSPortName string) error {
	/* Create the VETH pair - host namespace */
	if err := netlink.LinkAdd(newVETH(srcName, srcOvSPortName)); err != nil {
		return errors.Errorf("failed to create VETH pair - %v", err)
	}
	return nil
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

// DeleteInterface - deletes interface
func DeleteInterface(ifaceName string) error {
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

// GetLocalConnectionConfig returns VF Interface configuration
func GetLocalConnectionConfig(c *connection.Connection, ovsPortName string, isDst bool) sriov.VFInterfaceConfiguration {
	name, ok := c.GetMechanism().GetParameters()[common.InterfaceNameKey]
	if !ok {
		name = c.GetMechanism().GetParameters()[common.Workspace]
	}

	var ipAddress string
	if isDst {
		ipAddress = c.GetContext().GetIpContext().GetDstIpAddr()
	} else {
		ipAddress = c.GetContext().GetIpContext().GetSrcIpAddr()
	}

	return sriov.VFInterfaceConfiguration{
		PciAddress:   c.GetMechanism().GetParameters()[kernel.PciAddress],
		TargetNetns:  c.GetMechanism().GetParameters()[common.NetNsInodeKey],
		Name:         name,
		NetRepDevice: ovsPortName,
		IPAddress:    ipAddress,
	}
}

func CheckNetRepAvailability(netRep string) (bool, error) {
	availNetRep, err := CheckNetRepOvs(netRep)
	if err !=nil {
		return false, err
	}

	return availNetRep, nil
}

func PickDeviceAndNetRep(DeviceIDs string) (DeviceID, NetRep, error){
	var availNetRep = false
	for _, devID := range strings.Split(DeviceIDs, ",") {
		netRep, err := sriov.GetNetRepresentor(devID)
		if err != nil {
			return "", "", err
		}
		availNetRep, err = CheckNetRepAvailability(netRep)
		if err !=nil{
			return "", "", err
		}
		if availNetRep {
			return devID, netRep, nil
		}	
	}		
	if !availNetRep {
		err = errors.New("local: Could not find available Net Rep")
		return "","", err
	}

}
