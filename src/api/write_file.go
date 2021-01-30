package api

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"reagent/common"
	"reagent/messenger"

	"github.com/gammazero/nexus/v3/wamp"
)

func writeToFileHandler(ex *External) func(ctx context.Context, response messenger.Result) messenger.InvokeResult {
	return func(ctx context.Context, response messenger.Result) messenger.InvokeResult {
		args := response.Arguments

		// Matches file_transfer.ts payload
		chunkArg := args[0]
		// appTypeArg := args[1]
		nameArg := args[2]
		// container_nameArg := args[3]
		// totalArg := args[4]

		name, ok := nameArg.(string)
		if !ok {
			return messenger.InvokeResult{Err: fmt.Sprintf("Failed to parse name argument %s", nameArg)}
		}

		chunk, ok := chunkArg.(string)
		if !ok {
			return messenger.InvokeResult{Err: fmt.Sprintf("Failed to parse chunk argument %s", chunkArg)}
		}

		filePath := ex.Messenger.GetConfig().CommandLineArguments.AppBuildsDirectory
		err := write(name, filePath, chunk)

		if err != nil {
			return messenger.InvokeResult{
				ArgumentsKw: common.Dict{"cause": err.Error()},
				// TODO: show different URI error based on error that was returned upwards
				Err: string(wamp.ErrInvalidArgument),
			}
		}

		return messenger.InvokeResult{}
	}
}

// Write decodes hex encoded data chunks and writes to a file.
//
// Matches implementation in file_transfer.ts (Reswarm Backend)
func write(fileName string, filePath string, chunk string) error {
	f, err := os.OpenFile(filePath+"/"+fileName, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}

	if chunk == "END" {
		return f.Close()
	}

	if chunk == "BEGIN" {
		return os.Remove(filePath + "/" + fileName)
	}

	data, err := hex.DecodeString(chunk)
	if err != nil {
		return err
	}

	_, err = f.Write(data)
	if err != nil {
		return err
	}

	return nil
}
