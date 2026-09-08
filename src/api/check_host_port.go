package api

import (
	"context"
	"fmt"
	"reagent/common"
	"reagent/messenger"
)

// checkHostPortHandler serves re.mgmt.<serial>.check_host_port: whether the
// given app port rule could take host_port as its reserved device host port
// right now. Result: {available bool, reason string} with the contract's
// reason tokens ("taken_by_app", "taken_by_process", "ambiguous_compose_rule",
// "denylisted", "" when available).
func (ex *External) checkHostPortHandler(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error) {
	argsDict, err := firstArgDict(response.Arguments)
	if err != nil {
		return nil, err
	}

	appKey, err := requiredUint64(argsDict, "app_key")
	if err != nil {
		return nil, err
	}
	port, err := requiredUint64(argsDict, "port")
	if err != nil {
		return nil, err
	}

	// host_port 0 is a deliberate capability probe: the backend always calls
	// check_host_port before saving a reservation and sends 0 when the rule has
	// no host port to validate (a remote-only reservation, or a clear). An old
	// agent errors on the unknown topic; answering available proves the
	// capability without touching the registry.
	hostPortValue, hostPortPresent := argsDict["host_port"]
	hostPort, hostPortNumeric := common.ToUint64(hostPortValue)
	if !hostPortPresent || !hostPortNumeric {
		return nil, fmt.Errorf("the host_port param should be a non-negative whole number")
	}
	if hostPort == 0 {
		return &messenger.InvokeResult{
			Arguments: []interface{}{common.Dict{
				"available": true,
				"reason":    "",
			}},
		}, nil
	}

	stage, _ := argsDict["stage"].(string)
	if stage == "" {
		stage = string(common.PROD)
	}
	protocol, _ := argsDict["protocol"].(string)

	available, reason := ex.AppManager.CheckHostPortAvailability(appKey, common.Stage(stage), protocol, port, hostPort)

	return &messenger.InvokeResult{
		Arguments: []interface{}{common.Dict{
			"available": available,
			"reason":    reason,
		}},
	}, nil
}

// requiredUint64 reads a numeric argument that must be present and positive.
// Coerced rather than type-asserted: which numeric type a value arrives as
// depends on the negotiated serializer.
func requiredUint64(argsDict map[string]interface{}, key string) (uint64, error) {
	value, ok := common.ToUint64(argsDict[key])
	if !ok || value == 0 {
		return 0, fmt.Errorf("the %s param should be a positive whole number", key)
	}
	return value, nil
}
