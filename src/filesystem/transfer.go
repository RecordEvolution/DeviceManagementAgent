package filesystem

import (
	"archive/tar"
	"compress/gzip"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"reagent/common"
)

type Filesystem struct {
	activeTransfers common.Dict
}

func New() Filesystem {
	return Filesystem{activeTransfers: make(common.Dict)}
}

// Write decodes hex encoded data chunks and writes to a file.
//
// Matches implementation in file_transfer.ts (Reswarm Backend)
func (fs *Filesystem) Write(fileName string, filePath string, chunk string) error {
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
