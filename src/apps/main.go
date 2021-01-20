package apps

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"reagent/container"

	"github.com/docker/docker/api/types"
)

func BuildDevApp(appName string, appKey int, tarPath string) {
	ctx := context.Background()
	client := container.GetClientInstance()

	imageName := fmt.Sprintf("reswarm.registry.io/apps/dev_%d_%s", appKey, appName)
	reader, err := client.Build(ctx, tarPath, types.ImageBuildOptions{Tags: []string{imageName}, Dockerfile: "Dockerfile"})

	if err != nil {
		log.Fatalln("Failed to build app", appName, tarPath, err)
	}

	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		// TODO: send build logs via WAMP
		log.Println(scanner.Text())
	}

	// TODO: store build logs in database
}
