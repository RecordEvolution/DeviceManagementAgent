package api

import (
	"context"
	"reagent/common"
	"reagent/messenger"
	"reagent/terminal"
)

func (ex *External) initDeviceTerm(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error) {
	var err error
	pseudoTerminal := terminal.GetPseudoTerminal()

	if pseudoTerminal == nil {

		pseudoTerminal, err = terminal.NewPseudoTerminal()
		if err != nil {
			return nil, err
		}

		res := pseudoTerminal.Setup(ex.Config, ex.Messenger)

		return &messenger.InvokeResult{Arguments: []interface{}{res}}, nil
	}

	res := common.Dict{
		"sessionID":   pseudoTerminal.SessionID,
		"writeTopic":  pseudoTerminal.WriteTopic,
		"dataTopic":   pseudoTerminal.DataTopic,
		"resizeTopic": pseudoTerminal.ResizeTopic,
	}

	return &messenger.InvokeResult{Arguments: []interface{}{res}}, nil
}
