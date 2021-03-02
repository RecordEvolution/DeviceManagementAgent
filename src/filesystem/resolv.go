package filesystem

import (
	"fmt"
	"io/ioutil"
	"os"
	"strings"
)

func EnsureResolvConf() error {
	f, err := os.OpenFile("/etc/resolv.conf", os.O_CREATE|os.O_RDONLY, os.ModePerm)
	if err != nil {
		return err
	}

	byteContent, err := ioutil.ReadAll(f)
	if err != nil {
		return err
	}

	err = f.Close()
	if err != nil {
		return err
	}

	stringContent := string(byteContent)
	if !strings.Contains(stringContent, "8.8.8.8") {
		entry := fmt.Sprintf("nameserver 8.8.8.8\n")
		combinedContent := entry + stringContent
		err = OverwriteFile("/etc/resolv.conf", combinedContent)
		if err != nil {
			return err
		}
	}

	return nil
}
