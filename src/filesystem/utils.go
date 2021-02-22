package filesystem

import (
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strings"
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

// WriteCounter counts the number of bytes written to it. It implements to the io.Writer interface
// and we can pass this into io.TeeReader() which will report progress on each write cycle.
type WriteCounter struct {
	Total uint64
}

func (wc *WriteCounter) Write(p []byte) (int, error) {
	n := len(p)
	wc.Total += uint64(n)
	wc.PrintProgress()
	return n, nil
}

func (wc WriteCounter) PrintProgress() {
	megabytes := float64(wc.Total) * math.Pow(10, -6)
	fmt.Printf("\r%s", strings.Repeat(" ", 35))
	fmt.Printf("\rDownloading... %.3f MB", megabytes)
}

// Downloads any data from a given URL to a given filePath. Progress is logged to CLI
func DownloadURL(filePath string, url string) error {
	// open the required file
	out, err := os.Create(filePath)
	if err != nil {
		return err
	}

	defer out.Close()

	resp, err := http.Get(url)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	// copy the http body into the file
	counter := &WriteCounter{}
	if _, err = io.Copy(out, io.TeeReader(resp.Body, counter)); err != nil {
		return err
	}

	fmt.Print(" OK!\n")

	return nil
}
