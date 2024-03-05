package apps

import (
	"context"
	"fmt"
	"reagent/common"
	"reagent/errdefs"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/pkg/errors"
)

func (sm *StateMachine) stopApp(payload common.TransitionPayload, app *common.App) error {
	if payload.Stage == common.DEV {
		return sm.stopDevApp(payload, app)
	} else if payload.Stage == common.PROD {
		return sm.stopProdApp(payload, app)
	}
	return nil
}

func (sm *StateMachine) stopProdApp(payload common.TransitionPayload, app *common.App) error {
	if payload.DockerCompose != nil {
		return sm.stopProdComposeApp(payload, app)
	}

	ctx := context.Background()

	err := sm.setState(app, common.STOPPING)
	if err != nil {
		return err
	}

	// for now to resolve the issue regarding env variables, we should remove the container on stop
	cont, err := sm.Container.GetContainer(ctx, payload.ContainerName.Prod)
	if err != nil {
		if !errdefs.IsContainerNotFound(err) {
			return errors.Wrap(err, "failed to getContainer during stopDevApp")
		}
	}

	err = sm.LogManager.Write(payload.ContainerName.Prod, fmt.Sprintf("Received stop signal for %s", payload.AppName))
	if err != nil {
		return err
	}

	err = sm.setState(app, common.STOPPING)
	if err != nil {
		return err
	}

	err = sm.Container.StopContainerByID(ctx, cont.ID, time.Second*1)
	if err != nil {
		return errors.Wrap(err, "failed to stop container by ID during stopProdApp")
	}

	_, err = sm.Container.WaitForContainerByID(ctx, cont.ID, container.WaitConditionNotRunning)
	if err != nil {
		return err
	}

	err = sm.Container.RemoveContainerByID(ctx, cont.ID, map[string]interface{}{"force": true})
	if err != nil {
		return err
	}

	// should return 'container not found' error, this way we know it's removed successfully
	_, errC := sm.Container.PollContainerState(ctx, cont.ID, time.Second)
	select {
	case err := <-errC:
		if !errdefs.IsContainerNotFound(err) {
			return err
		}
	}

	err = sm.setState(app, common.PRESENT)
	if err != nil {
		return err
	}

	return sm.LogManager.Write(payload.ContainerName.Prod, fmt.Sprintf("Successfully stopped %s", payload.AppName))
}

func (sm *StateMachine) stopDevComposeApp(payload common.TransitionPayload, app *common.App) error {
	dockerComposePath, err := sm.WriteDockerComposeFile(payload, app, false)
	if err != nil {
		return err
	}

	err = sm.LogManager.Write(payload.ContainerName.Dev, fmt.Sprintf("Received stop signal for %s", payload.AppName))
	if err != nil {
		return err
	}

	err = sm.setState(app, common.STOPPING)
	if err != nil {
		return err
	}

	compose := sm.Container.Compose()

	_, _, cmd, err := compose.Stop(dockerComposePath)
	if err != nil {
		return err
	}

	err = cmd.Wait()
	if err != nil {
		return err
	}

	_, _, cmd, err = compose.Remove(dockerComposePath)
	if err != nil {
		return err
	}

	err = cmd.Wait()
	if err != nil {
		return err
	}

	err = sm.setState(app, common.PRESENT)
	if err != nil {
		return err
	}

	return sm.LogManager.Write(payload.ContainerName.Dev, fmt.Sprintf("Successfully stopped %s", payload.AppName))
}

func (sm *StateMachine) stopProdComposeApp(payload common.TransitionPayload, app *common.App) error {
	dockerComposePath, err := sm.WriteDockerComposeFile(payload, app, false)
	if err != nil {
		return err
	}

	err = sm.LogManager.Write(payload.ContainerName.Prod, fmt.Sprintf("Received stop signal for %s", payload.AppName))
	if err != nil {
		return err
	}

	err = sm.setState(app, common.STOPPING)
	if err != nil {
		return err
	}

	compose := sm.Container.Compose()

	_, _, cmd, err := compose.Stop(dockerComposePath)
	if err != nil {
		return err
	}

	err = cmd.Wait()
	if err != nil {
		return err
	}

	_, _, cmd, err = compose.Remove(dockerComposePath)
	if err != nil {
		return err
	}

	err = cmd.Wait()
	if err != nil {
		return err
	}

	err = sm.setState(app, common.PRESENT)
	if err != nil {
		return err
	}

	return sm.LogManager.Write(payload.ContainerName.Prod, fmt.Sprintf("Successfully stopped %s", payload.AppName))
}

func (sm *StateMachine) stopDevApp(payload common.TransitionPayload, app *common.App) error {
	if payload.DockerCompose != nil {
		return sm.stopDevComposeApp(payload, app)
	}

	ctx := context.Background()

	cont, err := sm.Container.GetContainer(ctx, payload.ContainerName.Dev)
	if err != nil {
		if !errdefs.IsContainerNotFound(err) {
			return errors.Wrap(err, "failed to getContainer during stopDevApp")
		}
	}

	err = sm.LogManager.Write(payload.ContainerName.Dev, fmt.Sprintf("Received stop signal for %s", payload.AppName))
	if err != nil {
		return err
	}

	err = sm.setState(app, common.STOPPING)
	if err != nil {
		return err
	}

	err = sm.Container.StopContainerByID(ctx, cont.ID, time.Second*1)
	if err != nil {
		return errors.Wrap(err, "failed to stop container by ID during stopDevApp")
	}

	_, err = sm.Container.WaitForContainerByID(ctx, cont.ID, container.WaitConditionNotRunning)
	if err != nil {
		return err
	}

	err = sm.Container.RemoveContainerByID(ctx, cont.ID, map[string]interface{}{"force": true})
	if err != nil {
		return err
	}

	// should return 'container not found' error, this way we know it's removed successfully
	_, errC := sm.Container.PollContainerState(ctx, cont.ID, time.Second)
	select {
	case err := <-errC:
		if !errdefs.IsContainerNotFound(err) {
			return err
		}
	}

	err = sm.setState(app, common.PRESENT)
	if err != nil {
		return err
	}

	return sm.LogManager.Write(payload.ContainerName.Dev, fmt.Sprintf("Successfully stopped %s", payload.AppName))
}
