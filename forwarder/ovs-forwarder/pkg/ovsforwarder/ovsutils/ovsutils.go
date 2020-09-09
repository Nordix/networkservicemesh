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

package ovsutils

import (
	"strconv"
	"strings"
	"time"

	"github.com/ovn-org/ovn-kubernetes/go-controller/pkg/util"
	"github.com/sirupsen/logrus"
)

// portMap contains mapping between of port name and its port no.
// This map is upto date most of the times when forwarding pod running.
var PortMap = make(map[string]int)

// Get Port number from Interface name in OVS
func GetInterfaceOfPort(interfaceName string) (int, error) {
	var portNo, count int
	count = 5
	for count > 0 {
		ofPort, stdErr, err := util.RunOVSVsctl("--if-exists", "get", "interface", interfaceName, "ofport")
		if err != nil {
			return -1, err
		}
		if stdErr != "" {
			logrus.Infof("ovsutils: error occured while retrieving of port for interface %s - %s", interfaceName, stdErr)
		}
		portNo, err = strconv.Atoi(ofPort)
		if portNo == 0 {
			logrus.Infof("ovsutils: got port number %d for interface %s, retrying", portNo, interfaceName)
			count = count - 1
			time.Sleep(500 * time.Millisecond)
			continue
		} else {
			break
		}
	}
	return portNo, nil
}

func CheckNetRepOvs(netRep string) (bool, error) {
	specialChar := []string{"name", ":", "\"", " "}
	ovsPorts, _, err := util.RunOVSVsctl("--columns=name", "list", "Interface")
	if err != nil {
		return false, err
	}
	for _, char := range specialChar {
		ovsPorts = strings.ReplaceAll(ovsPorts, char, "")
	}
	ovsPorts = strings.ReplaceAll(ovsPorts, "\n\n", ",")
	for _, attachedNetRep := range strings.Split(ovsPorts, ",") {
		if netRep == attachedNetRep {
			return false, nil
		}
	}
	return true, nil
}
