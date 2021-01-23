package apps

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"reagent/container"
	"reagent/messenger"

	"github.com/docker/docker/api/types"
)

type AppManager struct {
	container container.Container
	messenger messenger.Messenger
}

func New(c container.Container, m messenger.Messenger) AppManager {
	return AppManager{container: c, messenger: m}
}

func (am *AppManager) BuildDevApp(appName string) error {
	ctx := context.Background()
	imageName := fmt.Sprintf("reswarm.registry.io/apps/dev_%s", appName)
	reader, err := am.container.Build(ctx, "./TestApp.tar", types.ImageBuildOptions{Tags: []string{imageName}, Dockerfile: "Dockerfile"})

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
