package filesystem

import (
	"os"
	"io"
	"net/http"
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
	_, err = io.Copy(out,resp.Body)

	return err
}
