package sriov

import (
	"fmt"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/vishvananda/netns"

	"github.com/Mellanox/sriovnet"
	"github.com/networkservicemesh/networkservicemesh/utils/fs"
)

// VFInterfaceConfiguration represents configuration details that
// will be used to setup or close cross connection
type VFInterfaceConfiguration struct {
	pciAddress  string
	name        string
	ipAddress   string
	macAddress  string
	targetNetns string
}

// GetNetRepresentor retrieves network representor device for smartvf
func GetNetRepresentor(deviceID string) (string, error) {
	// get smart VF netdevice from PCI
	vfNetdevices, err := sriovnet.GetNetDevicesFromPci(deviceID)
	if err != nil {
		return "", err
	}

	// Make sure we have 1 netdevice per pci address
	if len(vfNetdevices) != 1 {
		return "", fmt.Errorf("failed to get one netdevice interface per %s", deviceID)
	}
	// get Uplink netdevice.  The uplink is basically the PF name of the deviceID (smart VF).
	// The uplink is later used to retrieve the representor for the smart VF.
	uplink, err := sriovnet.GetUplinkRepresentor(deviceID)
	if err != nil {
		return "", err
	}

	// get smart VF index from PCI
	vfIndex, err := sriovnet.GetVfIndexByPciAddress(deviceID)
	if err != nil {
		return "", err
	}

	// get smart VF representor interface. This is a host net device which represents
	// smart VF attached inside the container by device plugin. It can be considered
	// as one end of veth pair whereas other end is smartVF. The VF representor would
	// get added into ovs bridge for the control plane configuration.
	rep, err := sriovnet.GetVfRepresentor(uplink, vfIndex)
	if err != nil {
		return "", err
	}

	return rep, nil

}

// SetupVF sets up the VF into taget container network namespace
func SetupVF(config VFInterfaceConfiguration) error {
	// host network namespace to switch back to after finishing link setup
	hostNetns, err := netns.Get()
	if err != nil {
		return errors.Errorf("failed to get host namespace: %v", err)
	}
	defer hostNetns.Close()

	// always switch back to the host namespace at the end of link setup
	defer func() {
		if err = netns.Set(hostNetns); err != nil {
			logrus.Errorf("failed to switch back to host namespace: %v", err)
		}
	}()

	// get network namespace handle
	targetNetns, err := fs.GetNsHandleFromInode(config.targetNetns)
	if err != nil {
		return errors.Wrap(err, "failed to setup VF")
	}
	defer targetNetns.Close()

	// get VF link representor
	link, err := GetLink(config.pciAddress, config.name, hostNetns, targetNetns)
	if err != nil {
		return errors.Wrap(err, "failed to setup VF")
	}

	// move link into pod's network namespace
	err = link.MoveToNetns(targetNetns)
	if err != nil {
		return errors.Wrap(err, "failed to setup VF")
	}

	// switch to pod's network namespace to apply configuration, link is already there
	err = netns.Set(targetNetns)
	if err != nil {
		return errors.Wrap(err, "failed to setup VF")
	}

	// add IP address
	err = link.AddAddress(config.ipAddress)
	if err != nil {
		return errors.Wrap(err, "failed to setup VF")
	}

	// set new interface name
	err = link.SetName(config.name)
	if err != nil {
		return err
	}

	// bring up the link
	err = link.SetAdminState(UP)
	if err != nil {
		return err
	}

	// TODO: set MAC address, routes, neighbours, vlan and other properties etc.

	return nil
}

// ReleaseVF releases the VF from target container network namespace into host network namespace
func ReleaseVF(config VFInterfaceConfiguration) error {
	// host network namespace to switch back to after finishing link setup
	hostNetns, err := netns.Get()
	if err != nil {
		return errors.Errorf("failed to get host namespace: %v", err)
	}
	defer hostNetns.Close()

	// always switch back to the host namespace at the end of link setup
	defer func() {
		if err = netns.Set(hostNetns); err != nil {
			logrus.Errorf("failed to switch back to host namespace: %v", err)
		}
	}()

	// get network namespace handle
	targetNetns, err := fs.GetNsHandleFromInode(config.targetNetns)
	if err != nil {
		return errors.Wrap(err, "failed to release VF")
	}
	defer targetNetns.Close()

	// get VF link representor
	link, err := GetLink(config.pciAddress, config.name, targetNetns)
	if err != nil {
		return errors.Wrap(err, "failed to release VF")
	}

	// switch to pod's network namespace to apply configuration, link is already there
	err = netns.Set(targetNetns)
	if err != nil {
		return errors.Wrap(err, "failed to release VF")
	}

	// delete IP address
	err = link.DeleteAddress(config.ipAddress)
	if err != nil {
		return errors.Wrapf(err, "failed to release VF")
	}

	err = link.MoveToNetns(hostNetns)
	if err != nil {
		return errors.Wrap(err, "failed to release VF")
	}

	// switch to host namespace
	err = netns.Set(hostNetns)
	if err != nil {
		return errors.Wrap(err, "failed to release VF")
	}

	// TODO(optional): restore original link name

	return nil
}
