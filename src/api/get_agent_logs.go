package api

import (
	"context"
	"io/ioutil"
	"os"
	"reagent/common"
	"reagent/messenger"
)

func (ex *External) getAgentLogs(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error) {
	logFile := ex.Config.CommandLineArguments.LogFileLocation
	fileContents, err := ioutil.ReadFile(logFile)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		fileContents = []byte("log file was not found")
	}

	dict := common.Dict{
		"reagent.log": string(fileContents),
	}

	return &messenger.InvokeResult{
		Arguments: []interface{}{dict},
	}, nil
}
