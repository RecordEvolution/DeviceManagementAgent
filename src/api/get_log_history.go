package api

import (
	"context"
	"errors"
	"reagent/common"
	"reagent/messenger"
)

func (ex *External) getLogHistoryHandler(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error) {
	if len(response.Arguments) == 0 {
		return nil, errors.New("missing argument from payload")
	}

	argsPayload := response.Arguments[0]
	if argsPayload == nil {
		return nil, errors.New("missing argument from payload")
	}

	payloadArg, ok := argsPayload.(map[string]interface{})
	if !ok {
		return nil, errors.New("failed to parse payload, invalid type")
	}

	containerNameKw := payloadArg["container_name"]
	containerName, ok := containerNameKw.(string)
	if !ok {
		return nil, errors.New("failed to parse container_name, invalid type")
	}

	// appKeyKw := payloadArg["app_key"]
	// stageKw := payloadArg["stage"]
	// logTypeKw := payloadArg["log_type"]

	// var appName string
	// var appKey uint64
	// var stage string
	// var logType string

	// appName, ok = appNameKw.(string)
	// if !ok {
	// 	return nil, errors.New("failed to parse appName, invalid type")
	// }
	// appKey, ok = appKeyKw.(uint64)
	// if !ok {
	// 	return nil, errors.New("failed to parse appKey, invalid type")
	// }
	// stage, ok = stageKw.(string)
	// if !ok {
	// 	return nil, errors.New("failed to parse stage, invalid type")
	// }
	// logType, ok = logTypeKw.(string)
	// if !ok {
	// 	return nil, errors.New("failed to parse logType, invalid type")
	// }

	stage, appKey, appName, err := common.ParseContainerName(containerName)
	if err != nil {
		return nil, err
	}

	var logType common.LogType
	app, err := ex.AppManager.AppStore.GetApp(appKey, stage)

	app.StateLock.Lock()
	curAppState := app.CurrentState
	app.StateLock.Unlock()

	if curAppState == common.DOWNLOADING {
		logType = common.PULL
	} else if curAppState == common.PUBLISHING {
		logType = common.PUSH
	} else if curAppState == common.BUILDING {
		logType = common.BUILD
	} else {
		logType = common.APP
	}

	logHistoryArr, err := ex.LogManager.GetLogHistory(appName, appKey, stage, logType)
	if err != nil {
		return nil, err
	}

	// See https://github.com/golang/go/wiki/InterfaceSlice
	args := make([]interface{}, 0)
	for _, logEntry := range logHistoryArr {
		args = append(args, logEntry)
	}

	return &messenger.InvokeResult{
		Arguments: args,
	}, nil
}
