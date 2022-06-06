package filesystem

import (
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

func GetRemoteFile(url string) (io.ReadCloser, error) {
	client := http.Client{
		Transport: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout: 5 * time.Second,
			}).DialContext,
			TLSHandshakeTimeout:   5 * time.Second,
			ExpectContinueTimeout: 5 * time.Second,
			ResponseHeaderTimeout: 5 * time.Second,
		},
		Timeout: 10 * time.Second, // timeout for the entire request, i.e. the download itself
	}

	resp, err := client.Get(url)
	if err != nil {
		// happens when time setup (ReswarmOS) is not setup yet
		if strings.Contains(err.Error(), "certificate has expired or is not yet valid") {
			time.Sleep(time.Second * 1)
			return GetRemoteFile(url)
		}
		return nil, err
	}

	return resp.Body, nil
}
