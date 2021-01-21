package apps

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"reagent/container"

	"github.com/docker/docker/api/types"
)

func BuildDevApp(appName string, tarPath string) error {
	ctx := context.Background()
	client, err := container.GetClientInstance()
	if err != nil {
		return err
	}

	imageName := fmt.Sprintf("reswarm.registry.io/apps/dev_%s", appName)
	reader, err := client.Build(ctx, tarPath, types.ImageBuildOptions{Tags: []string{imageName}, Dockerfile: "Dockerfile"})

	if err != nil {
		return err
	}

	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		// TODO: send build logs via WAMP
		log.Println(scanner.Text())
	}

	// TODO: store build logs in database
	return nil
}

func RemoveDevApp(appName string) {
	// Remove docker image
	// Remove local files
}
