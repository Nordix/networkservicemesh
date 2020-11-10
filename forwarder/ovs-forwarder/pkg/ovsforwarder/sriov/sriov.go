package sriov

import (
	"time"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/vishvananda/netns"

	"github.com/Mellanox/sriovnet"
	"github.com/networkservicemesh/networkservicemesh/controlplane/api/connectioncontext"
	"github.com/networkservicemesh/networkservicemesh/utils/fs"
)

// VFInterfaceConfiguration represents configuration details that
// will be used to setup or close cross connection
type VFInterfaceConfiguration struct {
	PciAddress   string
	Name         string
	NetRepDevice string
	IPAddress    string
	MacAddress   string
	TargetNetns  string
	GwIPAddress  string
	Routes       []*connectioncontext.Route
}

// VfNameMap contains the mapping between pci address and its net
// device name. This is useful to reset the device name back original
// name when device is moved from container network namespace into host
// net namespace.
var VfNameMap = make(map[string]string)

// GetNetRepresentor retrieves network representor device for smartvf
func GetNetRepresentor(deviceID string) (string, error) {
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
	targetNetns, err := fs.GetNsHandleFromInode(config.TargetNetns)
	if err != nil {
		return errors.Wrap(err, "failed to setup VF: GetNsHandleFromInode")
	}
	defer targetNetns.Close()

	// get VF link representor
	link, err := GetLink(config.PciAddress, "", hostNetns)
	if err != nil {
		return errors.Wrap(err, "failed to setup VF: GetLink")
	}

	origName, err := link.GetName()
	if err != nil {
		return errors.Wrap(err, "failed to setup VF: GetName")
	}
	VfNameMap[config.PciAddress] = origName

	// move link into pod's network namespace
	err = link.MoveToNetns(targetNetns)
	if err != nil {
		return errors.Wrap(err, "failed to setup VF: MoveToNetns")
	}

	// switch to pod's network namespace to apply configuration, link is already there
	err = netns.Set(targetNetns)
	if err != nil {
		return errors.Wrap(err, "failed to setup VF: Set")
	}

	// add IP address
	err = link.AddAddress(config.IPAddress)
	if err != nil {
		return errors.Wrap(err, "failed to setup VF: AddAddress")
	}

	// set new interface name
	err = link.SetName(config.Name)
	if err != nil {
		return errors.Wrap(err, "failed to setup VF: AddAddress")
	}

	// add routes
	err = link.AddRoute(config.IPAddress, config.GwIPAddress, config.Routes)
	if err != nil {
		return errors.Wrap(err, "failed to setup VF: AddRoutes")
	}

	return nil
}

// ResetVF reset the VF on the host network namespace which was already moved
// upon deletion of the endpoint and client pod containers.
func ResetVF(config VFInterfaceConfiguration) error {
	// get the host network namespace
	hostNetns, err := netns.Get()
	if err != nil {
		return errors.Errorf("failed to get host namespace: %v", err)
	}
	defer hostNetns.Close()
	var link Link
	// Move the VF into host network namespace if its not done already and ignore the errors
	// as pod can be deleted at any time by kubelet.
	targetNetns, err := fs.GetNsHandleFromInode(config.TargetNetns)
	if err == nil {
		defer targetNetns.Close()
		// switch to pod namespace
		netns.Set(targetNetns)
		// get VF link representor
		link, err = GetLink(config.PciAddress, config.Name, targetNetns)
		if link != nil {
			// switch to pod's network namespace to apply configuration, link is already there
			err = netns.Set(targetNetns)
			if err == nil {
				// delete IP address
				link.DeleteAddress(config.IPAddress)
				// move the link into host network namespace
				link.MoveToNetns(hostNetns)
			}
		} else {
			logrus.Errorf("link is not present in container net namespace %s, %s, %v", config.PciAddress, config.Name, err)
		}
		// switch to host namespace
		netns.Set(hostNetns)
	}

	// get VF link representor on the host network namespace. Try for 10s until its available.
	count := 5
	for count > 0 {
		link, err = GetLink(config.PciAddress, "", hostNetns)
		if err != nil {
			count = count - 1
			if count == 0 {
				return errors.Wrap(err, "failed to release VF: : GetLink on hostNetns")
			}
			time.Sleep(2 * time.Second)
			continue
		}
		break
	}

	if origName, found := VfNameMap[config.PciAddress]; found {
		delete(VfNameMap, config.PciAddress)
		// set to original interface name
		err = link.SetName(origName)
		if err != nil {
			return errors.Wrap(err, "failed to release VF: SetName")
		}
	}

	return nil
}
