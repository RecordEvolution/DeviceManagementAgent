package filesystem

import (
	"fmt"
	"io"
	"net/http"
	"os"
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

// download from URL to local file
func DownloadURL(filepath string, url string) error {

	// retrieve the data
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// open the required file
	out, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer out.Close()

	// copy the http body into the file
	_, err = io.Copy(out, resp.Body)

	return err
}
