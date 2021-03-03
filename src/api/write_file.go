package api

import (
	"context"
	"errors"
	"fmt"
	"reagent/filesystem"
	"reagent/messenger"
	"reagent/safe"
)

func (ex *External) writeToFileHandler(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error) {
	args := response.Arguments

	// Matches file_transfer.ts payload
	chunkArg := args[0]
	fileNameArg := args[1]
	containerNameArg := args[2]
	totalArg := args[3]
	idArg := args[4]

	fileName, ok := fileNameArg.(string)
	if !ok {
		return nil, errors.New("Failed to parse name argument")
	}

	chunk, ok := chunkArg.(string)
	if !ok {
		return nil, errors.New("Failed to parse chunk argument")
	}

	containerName, ok := containerNameArg.(string)
	if !ok {
		return nil, errors.New("Failed to parse containerName argument")
	}

	total, ok := totalArg.(uint64)
	if !ok {
		return nil, errors.New("Failed to parse total argument")
	}

	id, ok := idArg.(string)
	if !ok {
		return nil, errors.New("Failed to parse id argument")
	}

	fileDir := ex.Messenger.GetConfig().CommandLineArguments.AppsBuildDir

	fileChunk := filesystem.FileChunk{
		ID:            id,
		FileName:      fileName,
		FilePath:      fileDir,
		Data:          chunk,
		ContainerName: containerName,
		Total:         total,
	}

	err := ex.Filesystem.Write(fileChunk)
	if err != nil {
		return nil, err
	}

	var message string
	if fileChunk.Data == "BEGIN" {
		safe.Go(func() {
			ex.LogManager.ClearLogHistory(containerName)
		})

		message = "Received first package on device!"
	} else if fileChunk.Data == "END" {
		message = "File transfer has finished, starting build..."
	}

	if fileChunk.Data == "BEGIN" || fileChunk.Data == "END" {
		safe.Go(func() {
			ex.LogManager.Write(containerName, message)
		})

		return &messenger.InvokeResult{}, nil
	}

	activeTransfer := ex.Filesystem.GetActiveTransfer(containerName)
	if activeTransfer == nil {
		// received a chunk but no active stream was found
		return &messenger.InvokeResult{}, nil
	}

	safe.Go(func() {
		percentage := float64(activeTransfer.Current) / float64(activeTransfer.Total) * 100
		percentageString := fmt.Sprintf("%.3f%%", percentage)
		ex.LogManager.PublishProgress(containerName, fileName, "Transfer Progress:", percentageString)
	})

	return &messenger.InvokeResult{}, nil
}
