package filesystem

import (
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

func FileExists(filename string) (bool, error) {
	info, err := os.Stat(filename)
	if os.IsNotExist(err) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	return !info.IsDir(), nil
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

type WriteCounter struct {
	callback func(increment uint64, currentBytes uint64, totalFileSize uint64)
	Size     uint64
	Total    uint64
}

func (wc *WriteCounter) Write(p []byte) (int, error) {
	n := len(p)
	wc.Total += uint64(n)

	printCallBack(uint64(n), wc.Total)

	// custom download callback
	if wc.callback != nil {
		wc.callback(uint64(n), wc.Total, wc.Size)
	}
	return n, nil
}

func printCallBack(increment uint64, totalBytes uint64) {
	megabytes := float64(totalBytes) * math.Pow(10, -6)
	fmt.Printf("\r%s", strings.Repeat(" ", 35))
	fmt.Printf("\rDownloading... %.3f MB", megabytes)
}

// Downloads any data from a given URL to a given filePath. Progress is logged to CLI
func DownloadURL(filePath string, url string, callback func(increment uint64, currentBytes uint64, totalFileSize uint64)) error {
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
		Timeout: 120 * time.Second, // timeout for the entire request, i.e. the download itself
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
	counter := &WriteCounter{callback: callback, Size: uint64(size)}
	if _, err = io.Copy(out, io.TeeReader(resp.Body, counter)); err != nil {
		return err
	}

	fmt.Print(" OK!\n")

	return nil
}
