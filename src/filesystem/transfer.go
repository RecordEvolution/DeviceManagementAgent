package filesystem

import (
	"archive/tar"
	"compress/gzip"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
)

type Filesystem struct {
	activeTransfers     map[string]*ActiveFileTransfer // containerName --> transferID
	activeTransfersLock sync.Mutex
}

type ActiveFileTransfer struct {
	ID       string
	Current  uint64
	Total    uint64
	Canceled bool
}

type FileChunk struct {
	ID            string
	FileName      string
	FilePath      string
	Data          string
	ContainerName string
	Total         uint64
}

func New() Filesystem {
	return Filesystem{activeTransfers: make(map[string]*ActiveFileTransfer)}
}

func (fs *Filesystem) CancelFileTransfer(containerName string) {
	fileTransfer := fs.GetActiveTransfer(containerName)
	if fileTransfer == nil {
		return
	}

	fileTransfer.Canceled = true
}

// CleanupFailedTransfer removes a failed transfer from the active transfers map.
// This should be called when a transfer error occurs to prevent stale state.
func (fs *Filesystem) CleanupFailedTransfer(containerName string) {
	fs.activeTransfersLock.Lock()
	delete(fs.activeTransfers, containerName)
	fs.activeTransfersLock.Unlock()
}

func (fs *Filesystem) GetActiveTransfer(containerName string) *ActiveFileTransfer {
	fs.activeTransfersLock.Lock()
	activeTransfer := fs.activeTransfers[containerName]
	fs.activeTransfersLock.Unlock()

	return activeTransfer
}

// Write decodes hex encoded data chunks and writes to a file.
//
// Matches implementation in file_transfer.ts (IronFlock Backend)
func (fs *Filesystem) Write(chunk FileChunk) error {

	fs.activeTransfersLock.Lock()
	activeTransfer := fs.activeTransfers[chunk.ContainerName]
	fs.activeTransfersLock.Unlock()

	f, err := os.OpenFile(chunk.FilePath+"/"+chunk.FileName, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}

	if chunk.Data == "END" {
		fs.activeTransfersLock.Lock()
		delete(fs.activeTransfers, chunk.ContainerName)
		fs.activeTransfersLock.Unlock()

		return f.Close()
	}

	if chunk.Data == "BEGIN" {
		fs.activeTransfersLock.Lock()
		fs.activeTransfers[chunk.ContainerName] = &ActiveFileTransfer{
			ID:    chunk.ID,
			Total: chunk.Total,
		}
		fs.activeTransfersLock.Unlock()

		os.Remove(chunk.FilePath + "/" + chunk.FileName)
		return nil
	}

	if activeTransfer.Canceled {
		return errors.New("canceled")
	}

	if activeTransfer == nil {
		return errors.New("We received a chunk without an active transfer")
	}

	if activeTransfer != nil && activeTransfer.ID != chunk.ID {
		log.Debug().Msg("Received a chunk from transfer that was reset")
		return nil
	}

	data, err := hex.DecodeString(chunk.Data)
	if err != nil {
		return err
	}

	n, err := f.Write(data)
	if err != nil {
		return err
	}

	activeTransfer.Current += uint64(n)

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
