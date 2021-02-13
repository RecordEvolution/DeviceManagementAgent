package apps

import (
	"context"
	"reagent/common"
	"reagent/errdefs"

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

	err = sm.setState(app, common.STOPPING)
	if err != nil {
		return err
	}

	err = sm.Container.RemoveContainerByID(ctx, cont.ID, map[string]interface{}{"force": true})
	if err != nil {
		// if !errdefs.IsContainerRemovalAlreadyInProgress(err) && !errdefs.IsContainerNotFound(err) {
		return errors.Wrap(err, "failed to remove container by ID during stopProdApp")
		// }
	}

	_, err = sm.Container.WaitForContainerByID(ctx, cont.ID, container.WaitConditionRemoved)
	if err != nil {
		// expected behaviour, see: https://github.com/docker/docker-py/issues/2270
		// still useful, and will wait if it's still not removed
		if !errdefs.IsContainerNotFound(err) {
			return errors.Wrap(err, "failed to wait for container during stopProdApp")
		}
	}

	err = sm.setState(app, common.PRESENT)
	if err != nil {
		return err
	}

	return nil
}

func (sm *StateMachine) stopDevApp(payload common.TransitionPayload, app *common.App) error {
	ctx := context.Background()

	cont, err := sm.Container.GetContainer(ctx, payload.ContainerName.Dev)
	if err != nil {
		if !errdefs.IsContainerNotFound(err) {
			return errors.Wrap(err, "failed to getContainer during stopDevApp")
		}
	}

	err = sm.setState(app, common.STOPPING)
	if err != nil {
		return err
	}

	err = sm.Container.RemoveContainerByID(ctx, cont.ID, map[string]interface{}{"force": true})
	if err != nil {
		// It's possible we're trying to remove the container when it's already being removed
		// RUNNING -> STOPPED -> RUNNING
		if !errdefs.IsContainerRemovalAlreadyInProgress(err) {
			return errors.Wrap(err, "failed to remove container by ID during stopDevApp")
		}
	}

	_, err = sm.Container.WaitForContainerByID(ctx, cont.ID, container.WaitConditionRemoved)
	if err != nil {
		// expected behaviour, see: https://github.com/docker/docker-py/issues/2270
		// still useful, and will wait if it's still not removed
		if !errdefs.IsContainerNotFound(err) {
			return errors.Wrap(err, "failed to wait for container during stopDevApp")
		}
	}

	err = sm.setState(app, common.PRESENT)
	if err != nil {
		return err
	}

	return nil
}
