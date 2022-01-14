package privilege

import (
	"context"
	"errors"
	"fmt"
	"reagent/common"
	"reagent/config"
	"reagent/messenger"
	"reagent/messenger/topics"
	"strconv"
	"time"
)

type Privilege struct {
	messenger messenger.Messenger
	config    *config.Config
}

func NewPrivilege(messenger messenger.Messenger, config *config.Config) Privilege {
	return Privilege{messenger: messenger, config: config}
}

func (p *Privilege) Check(privilege string, details common.Dict) (bool, error) {
	deviceKey := uint64(p.config.ReswarmConfig.DeviceKey)
	swarmKey := uint64(p.config.ReswarmConfig.SwarmKey)
	callerAuthId := fmt.Sprint(details["caller_authid"])

	if callerAuthId == "system" {
		return true, nil
	}

	requestorAccountKey, err := strconv.Atoi(callerAuthId)
	if err != nil {
		return false, err
	}

	payload := common.Dict{
		"privilege":             privilege,
		"entity":                "DEVICE",
		"entity_key":            deviceKey,
		"requestor_account_key": requestorAccountKey,
		"swarm_key":             swarmKey,
	}

	ctx, cancelFunc := context.WithTimeout(context.Background(), time.Second*2)
	defer cancelFunc()

	res, err := p.messenger.Call(ctx, topics.CheckPrivilege, []interface{}{payload}, nil, nil, nil)
	if err != nil {
		return false, err
	}

	isPrivilegedArgs, ok := res.Arguments[0].(bool)
	if !ok {
		return false, errors.New("type of argument is not bool")
	}

	return isPrivilegedArgs, nil
}
