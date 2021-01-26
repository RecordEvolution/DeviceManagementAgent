package filesystem

import (
	"encoding/hex"
	"os"
)

// Write decodes hex encoded data chunks and writes to a file
// Matches implementation in file_transfer.ts (Reswarm Backend)
func Write(fileName string, filePath string, chunk string) error {
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
