package filesystem

import (
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

func remoteFileClient() http.Client {
	return http.Client{
		Transport: &http.Transport{
			// A custom Transport defaults to no proxy; without this the agent
			// ignores HTTP(S)_PROXY/NO_PROXY and can't reach the update server
			// from a host behind a corporate proxy (it stays frozen on its
			// first-installed version). DefaultTransport sets the same.
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout: 5 * time.Second,
			}).DialContext,
			TLSHandshakeTimeout:   5 * time.Second,
			ExpectContinueTimeout: 5 * time.Second,
			ResponseHeaderTimeout: 5 * time.Second,
		},
		Timeout: 10 * time.Second, // timeout for the entire request, i.e. the download itself
	}
}

func GetRemoteFile(url string) (io.ReadCloser, error) {
	body, _, err := GetRemoteFileWithStatus(url)
	return body, err
}

// GetRemoteFileWithStatus is GetRemoteFile surfacing the HTTP status code, so
// callers can distinguish an absent file (404) from other failures.
func GetRemoteFileWithStatus(url string) (io.ReadCloser, int, error) {
	client := remoteFileClient()

	resp, err := client.Get(url)
	if err != nil {
		// happens when time setup (ReswarmOS) is not setup yet
		if strings.Contains(err.Error(), "certificate has expired or is not yet valid") {
			time.Sleep(time.Second * 1)
			return GetRemoteFileWithStatus(url)
		}
		return nil, 0, err
	}

	return resp.Body, resp.StatusCode, nil
}
