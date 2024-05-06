package filesystem

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"

	"github.com/google/uuid"
)

const DEFAULT_INTERPRETER = "/bin/bash"

func writeScript(command string) (string, error) {
	var buffer bytes.Buffer

	buffer.WriteString("#!")
	buffer.WriteString(DEFAULT_INTERPRETER)
	buffer.WriteString("\n")
	buffer.WriteString(command)
	buffer.WriteString("\n")
	buffer.WriteString("exit 0")

	script := buffer.String()

	id := uuid.New()
	scriptPath := fmt.Sprintf("%s/%s.sh", os.TempDir(), id)
	err := os.WriteFile(scriptPath, []byte(script), os.ModePerm)
	if err != nil {
		return "", err
	}

	return scriptPath, nil
}

func ExecuteAsScript(command string) ([]byte, error) {
	scriptPath, err := writeScript(command)
	if err != nil {
		return nil, err
	}

	cmd := exec.Command(DEFAULT_INTERPRETER, scriptPath)
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	err = os.Remove(scriptPath)
	if err != nil {
		return nil, err
	}

	return output, nil
}
