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

package main

import (
	"context"

	"github.com/networkservicemesh/networkservicemesh/pkg/tools/jaeger"

	"github.com/sirupsen/logrus"

	"github.com/networkservicemesh/networkservicemesh/pkg/tools/spanhelper"

	"github.com/networkservicemesh/networkservicemesh/pkg/probes"

	"github.com/networkservicemesh/networkservicemesh/forwarder/ovs-forwarder/pkg/ovsforwarder"
	"github.com/networkservicemesh/networkservicemesh/forwarder/pkg/common"
	"github.com/networkservicemesh/networkservicemesh/pkg/tools"
)

func main() {
	// Capture signals to cleanup before exiting
	logrus.Info("Starting the OvS-based forwarding plane!")

	closer := jaeger.InitJaeger("ovs-forwarder")
	defer func() { _ = closer.Close() }()

	span := spanhelper.FromContext(context.Background(), "Start.OvSForwarder.Forwarder")
	defer span.Finish()
	c := tools.NewOSSignalChannel()
	forwarderGoals := &common.ForwarderProbeGoals{}
	forwarderProbes := probes.New("OvS-based forwarding plane liveness/readiness healthcheck", forwarderGoals)
	forwarderProbes.BeginHealthCheck()

	plane := ovsforwarder.CreateOvSForwarder()

	registration := common.CreateForwarder(span.Context(), plane, forwarderGoals)

	<-c
	logrus.Info("Closing Forwarder Registration")
	registration.Close()
}
