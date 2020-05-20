package kubetest

import "github.com/networkservicemesh/networkservicemesh/forwarder/pkg/common"

// DefaultPlaneVariablesOvS - Default variables for OvS forwarding deployment
func DefaultPlaneVariablesOvS() map[string]string {
	return map[string]string{
		common.ForwarderMetricsEnabledKey: "false",
	}
}
