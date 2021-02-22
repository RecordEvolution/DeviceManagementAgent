package api

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reagent/messenger"
)

func (ex *External) writeToFileHandler(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error) {
	args := response.Arguments

	// Matches file_transfer.ts payload
	chunkArg := args[0]
	// appTypeArg := args[1]
	nameArg := args[2]
	// container_nameArg := args[3]
	// totalArg := args[4]

	fileName, ok := nameArg.(string)
	if !ok {
		return nil, errors.New("Failed to parse name argument")
	}

	chunk, ok := chunkArg.(string)
	if !ok {
		return nil, errors.New("Failed to parse chunk argument")
	}

	fileDir := ex.Messenger.GetConfig().CommandLineArguments.AppsBuildDir
	err := write(fileName, fileDir, chunk)
	if err != nil {
		return nil, err
	}

	return &messenger.InvokeResult{}, nil
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

func decompressTgz(sourcePath string, targetPath string, targetFileName string) error {
	file, err := os.Open(sourcePath)
	if err != nil {
		return err
	}

	defer file.Close()

	gzReader, err := gzip.NewReader(file)
	if err != nil {
		return err
	}

	defer gzReader.Close()

	tarReader := tar.NewReader(gzReader)

	targetPath = filepath.Join(targetPath, targetFileName)
	writer, err := os.Create(targetPath)
	if err != nil {
		return err
	}
	defer writer.Close()

	_, err = io.Copy(writer, tarReader)
	return err
}
