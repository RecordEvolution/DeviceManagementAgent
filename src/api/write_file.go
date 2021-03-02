package api

import (
	"context"
	"errors"
	"reagent/messenger"
)

func (ex *External) writeToFileHandler(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error) {
	args := response.Arguments

	// Matches file_transfer.ts payload
	chunkArg := args[0]
	// appTypeArg := args[1]
	nameArg := args[2]
	// containerNameArg := args[3]
	// totalArg := args[4]

	fileName, ok := nameArg.(string)
	if !ok {
		return nil, errors.New("Failed to parse name argument")
	}

	chunk, ok := chunkArg.(string)
	if !ok {
		return nil, errors.New("Failed to parse chunk argument")
	}

	// containerName, ok := containerNameArg.(string)
	// if !ok {
	// 	return nil, errors.New("Failed to parse containerName argument")
	// }

	fileDir := ex.Messenger.GetConfig().CommandLineArguments.AppsBuildDir
	err := ex.Filesystem.Write(fileName, fileDir, chunk)
	if err != nil {
		return nil, err
	}

	return &messenger.InvokeResult{}, nil
}
