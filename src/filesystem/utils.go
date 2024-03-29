package filesystem

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"reagent/config"
	"reagent/errdefs"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/semaphore"
)

type DownloadProgress struct {
	FilePath      string
	Increment     uint64
	CurrentBytes  uint64
	TotalFileSize uint64
}

var DownloadLocks = make(map[string]*semaphore.Weighted)

func PathExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if os.IsNotExist(err) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	return true, nil
}

func OverwriteFile(filePath string, value string) error {
	file, err := os.OpenFile(filePath, os.O_TRUNC|os.O_WRONLY, 0)
	if err != nil {
		return err
	}

	_, err = fmt.Fprintf(file, "%s", value)
	if err != nil {
		return err
	}

	return err
}

func ReadFileInTgz(tarPath string, fileToFind string) (*tar.Reader, error) {
	f, err := os.Open(tarPath)
	if err != nil {
		return nil, err
	}

	defer f.Close()

	gzf, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}

	tarReader := tar.NewReader(gzf)

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}

		if err != nil {
			return nil, err
		}

		switch header.Typeflag {
		case tar.TypeDir:
			continue
		case tar.TypeReg:
			if path.Base(header.Name) == fileToFind {
				return tarReader, nil
			}
		}
	}

	return nil, errdefs.ErrNotFound
}

type WriteCounter struct {
	callback func(DownloadProgress)
	Size     uint64
	Total    uint64
	FilePath string
}

func (wc *WriteCounter) Write(p []byte) (int, error) {
	n := len(p)
	wc.Total += uint64(n)

	progress := DownloadProgress{
		Increment:     uint64(n),
		FilePath:      wc.FilePath,
		CurrentBytes:  wc.Total,
		TotalFileSize: wc.Size,
	}

	if wc.callback != nil {
		wc.callback(progress)
	}
	return n, nil
}

func GetTunnelBinaryPath(config *config.Config, binaryName string) string {
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}
	return filepath.Join(config.CommandLineArguments.AgentDir, binaryName)
}

func printCallBack(dp DownloadProgress) {
	fmt.Printf("\r%s", strings.Repeat(" ", 35))
	fmt.Printf("\rDownloading... %+v", dp)
}

// Downloads any data from a given URL to a given filePath. Progress is logged to CLI
func DownloadURL(filePath string, url string, callback func(DownloadProgress)) error {
	var currentLock *semaphore.Weighted
	if DownloadLocks[filePath] == nil {
		currentLock = semaphore.NewWeighted(1)
		DownloadLocks[filePath] = currentLock
	} else {
		currentLock = DownloadLocks[filePath]
	}

	log.Debug().Msgf("Trying to acquire download lock for %s", filePath)
	if !currentLock.TryAcquire(1) {
		return errdefs.InProgress(errors.New("download already in progress"))
	}

	defer func() {
		currentLock.Release(1)
		delete(DownloadLocks, filePath)
	}()

	// open the required file
	out, err := os.Create(filePath)
	if err != nil {
		return err
	}

	defer out.Close()

	client := http.Client{
		Transport: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout: 10 * time.Second,
			}).DialContext,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 10 * time.Second,
			ResponseHeaderTimeout: 10 * time.Second,
		},
	}

	resp, err := client.Get(url)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	size, err := strconv.Atoi(resp.Header.Get("Content-Length"))
	if err != nil {
		return err
	}

	// copy the http body into the file
	counter := &WriteCounter{callback: callback, Size: uint64(size), FilePath: filePath}
	if _, err = io.Copy(out, io.TeeReader(resp.Body, counter)); err != nil {
		return err
	}

	return nil
}
