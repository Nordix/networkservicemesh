// Copyright 2020 Ericsson Software Technology.
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
	"context"

	"github.com/golang/protobuf/ptypes/empty"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc/status"

	"github.com/networkservicemesh/networkservicemesh/controlplane/api/connection"
	"github.com/networkservicemesh/networkservicemesh/controlplane/api/connection/mechanisms/kernel"
	"github.com/networkservicemesh/networkservicemesh/controlplane/api/connection/mechanisms/vxlan"
	"github.com/networkservicemesh/networkservicemesh/controlplane/api/crossconnect"
	"github.com/networkservicemesh/networkservicemesh/forwarder/api/forwarder"
	"github.com/networkservicemesh/networkservicemesh/forwarder/kernel-forwarder/pkg/monitoring"
	"github.com/networkservicemesh/networkservicemesh/forwarder/ovs-forwarder/pkg/ovsforwarder/local"
	"github.com/networkservicemesh/networkservicemesh/forwarder/ovs-forwarder/pkg/ovsforwarder/remote"
	"github.com/networkservicemesh/networkservicemesh/forwarder/pkg/common"

	"github.com/ovn-org/ovn-kubernetes/go-controller/pkg/util"
	kexec "k8s.io/utils/exec"
)

// OvSForwarder instance
type OvSForwarder struct {
	common        *common.ForwarderConfig
	monitoring    *monitoring.Metrics
	localConnect  *local.Connect
	remoteConnect *remote.Connect
}

// CreateOvSForwarder creates an instance of the OvSForwarder
func CreateOvSForwarder() *OvSForwarder {
	return &OvSForwarder{
		localConnect:  local.NewConnect(),
		remoteConnect: remote.NewConnect(),
	}
}

// Init initializes the OvS forwarding plane
func (o *OvSForwarder) Init(common *common.ForwarderConfig) error {
	o.common = common
	o.common.Name = "ovs-forwarder"
	o.configureOvSForwarder()
	return nil
}

// CreateForwarderServer creates an instance of ForwarderServer
func (o *OvSForwarder) CreateForwarderServer(config *common.ForwarderConfig) forwarder.ForwarderServer {
	return o
}

// Request handler for connections
func (o *OvSForwarder) Request(ctx context.Context, crossConnect *crossconnect.CrossConnect) (*crossconnect.CrossConnect, error) {
	logrus.Infof("Request() called with %v", crossConnect)

	if err := crossConnect.IsValid(); err != nil {
		logrus.Errorf("Close: %v is not valid, reason: %v", crossConnect, err)
		return crossConnect, err
	}

	err := o.connectOrDisconnect(crossConnect, true)
	if err != nil {
		logrus.Warn("error while handling Request() connection:", err)
		return nil, err
	}
	o.common.Monitor.Update(ctx, crossConnect)
	return crossConnect, err
}

// Close handler for connections
func (o *OvSForwarder) Close(ctx context.Context, crossConnect *crossconnect.CrossConnect) (*empty.Empty, error) {
	logrus.Infof("Close() called with %#v", crossConnect)
	err := o.connectOrDisconnect(crossConnect, false)
	if err != nil {
		logrus.Warn("error while handling Close() connection:", err)
	}
	o.common.Monitor.Delete(ctx, crossConnect)
	return &empty.Empty{}, nil
}

func (o *OvSForwarder) connectOrDisconnect(crossConnect *crossconnect.CrossConnect, connect bool) error {
	var err error
	var devices map[string]monitoring.Device

	if o.common.MetricsEnabled {
		o.monitoring.GetDevices().Lock()
		defer o.monitoring.GetDevices().Unlock()
	}

	/* 0. Sanity check whether the forwarding plane supports the connection type in the request */
	if err = common.SanityCheckConnectionType(o.common.Mechanisms, crossConnect); err != nil {
		return err
	}

	/* 1. Handle local connection */
	if crossConnect.GetSource().GetMechanism().GetType() == kernel.MECHANISM && crossConnect.GetDestination().GetMechanism().GetType() == kernel.MECHANISM {
		devices, err = o.handleLocalConnection(crossConnect, connect)
	} else {
		/* 2. Handle remote connection */
		devices, err = o.handleRemoteConnection(crossConnect, connect)
	}
	if devices != nil && err == nil {
		if connect {
			logrus.Info("ovs-forwarder: created devices: ", devices)
		} else {
			logrus.Info("ovs-forwarder: deleted devices: ", devices)
		}
		// Metrics monitoring
		if o.common.MetricsEnabled {
			o.monitoring.GetDevices().UpdateDeviceList(devices, connect)
		}
	}
	return err
}

// configureOvSForwarder setups the OvS forwarding plane
func (o *OvSForwarder) configureOvSForwarder() {
	o.common.MechanismsUpdateChannel = make(chan *common.Mechanisms, 1)
	o.common.Mechanisms = &common.Mechanisms{
		LocalMechanisms: []*connection.Mechanism{
			{
				Type: kernel.MECHANISM,
			},
		},
		RemoteMechanisms: []*connection.Mechanism{
			{
				Type: vxlan.MECHANISM,
				Parameters: map[string]string{
					vxlan.SrcIP: o.common.EgressInterface.SrcIPNet().IP.String(),
				},
			},
		},
	}

	// Initialize the ovs utility wrapper.
	exec := kexec.New()
	if err := util.SetExec(exec); err != nil {
		logrus.Errorf("failed to initialize ovs exec helper: %v", err)
	}

	// Create ovs bridge for client and endpoint connections
	stdout, stderr, err := util.RunOVSVsctl("--", "--may-exist", "add-br", kernel.BridgeName)
	if err != nil {
		logrus.Errorf("Failed to add bridge %s, stdout: %q, stderr: %q, error: %v", kernel.BridgeName, stdout, stderr, err)
	}

	// Clean the flows from the above created ovs bridge
	stdout, stderr, err = util.RunOVSOfctl("del-flows", kernel.BridgeName)
	if err != nil {
		logrus.Errorf("Failed to cleanup flows on %s "+
			"stdout: %q, stderr: %q, error: %v", kernel.BridgeName, stdout, stderr, err)
	}

	// Metrics monitoring
	if o.common.MetricsEnabled {
		o.monitoring = monitoring.CreateMetricsMonitor(o.common.MetricsPeriod)
		o.monitoring.Start(o.common.Monitor)
	}
	// Network Service monitoring
	common.CreateNSMonitor(o.common.Monitor, nsmonitorCallback)
}

// MonitorMechanisms handler
func (o *OvSForwarder) MonitorMechanisms(empty *empty.Empty, updateSrv forwarder.MechanismsMonitor_MonitorMechanismsServer) error {
	initialUpdate := &forwarder.MechanismUpdate{
		RemoteMechanisms: o.common.Mechanisms.RemoteMechanisms,
		LocalMechanisms:  o.common.Mechanisms.LocalMechanisms,
	}

	logrus.Infof("ovs-forwarder: sending MonitorMechanisms update: %v", initialUpdate)
	if err := updateSrv.Send(initialUpdate); err != nil {
		logrus.Errorf("ovs-forwarder: detected server error %s, gRPC code: %+v on gRPC channel", err.Error(), status.Convert(err).Code())
		return nil
	}
	// Waiting for any updates which might occur during a life of forwarder module and communicating
	// them back to NSM.
	for update := range o.common.MechanismsUpdateChannel {
		o.common.Mechanisms = update
		logrus.Infof("ovs-forwarder: sending MonitorMechanisms update: %v", update)

		updateMsg := &forwarder.MechanismUpdate{
			RemoteMechanisms: update.RemoteMechanisms,
			LocalMechanisms:  update.LocalMechanisms,
		}
		if err := updateSrv.Send(updateMsg); err != nil {
			logrus.Errorf("ovs-forwarder: detected server error %s, gRPC code: %+v on gRPC channel", err.Error(), status.Convert(err).Code())
			return nil
		}
	}
	return nil
}

// nsmonitorCallback is called to notify the forwarder that the connection is down. If needed, may be used as a trigger to some specific handling
func nsmonitorCallback() {
	logrus.Infof("ovs-forwarder: NSMonitor callback called")
}
