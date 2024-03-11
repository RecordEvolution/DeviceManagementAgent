package api

import (
	"context"
	"errors"
	"reagent/errdefs"
	"reagent/messenger"
	"reagent/safe"
	"time"

	"github.com/rs/zerolog/log"
)

func (ex *External) systemRebootHandler(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error) {
	privileged, err := ex.Privilege.Check("MAINTAIN", response.Details)
	if err != nil {
		return nil, err
	}

	if !privileged {
		return nil, errdefs.InsufficientPrivileges(errors.New("insufficient privileges to reboot device"))
	}

	safe.Go(func() {
		time.Sleep(time.Second * 2)

		err = ex.System.Reboot()
		if err != nil {
			log.Error().Err(err).Msg("Failed to trigger reboot")
		}
	})

	return &messenger.InvokeResult{}, nil
}

func (ex *External) systemShutdownHandler(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error) {
	privileged, err := ex.Privilege.Check("MAINTAIN", response.Details)
	if err != nil {
		return nil, err
	}

	if !privileged {
		return nil, errdefs.InsufficientPrivileges(errors.New("insufficient privileges to power off device"))
	}

	safe.Go(func() {
		time.Sleep(time.Second * 2)

		err = ex.System.Poweroff()
		if err != nil {
			log.Error().Err(err).Msg("Failed to trigger poweroff")
		}
	})

	return &messenger.InvokeResult{}, nil
}

func (ex *External) systemRestartAgentHandler(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error) {
	privileged, err := ex.Privilege.Check("MAINTAIN", response.Details)
	if err != nil {
		return nil, err
	}

	if !privileged {
		return nil, errdefs.InsufficientPrivileges(errors.New("insufficient privileges to restart agent"))
	}

	safe.Go(func() {
		time.Sleep(time.Second * 2)

		err = ex.System.RestartAgent()
		if err != nil {
			log.Error().Err(err).Msg("Failed to trigger restart agent")
		}
	})

	return &messenger.InvokeResult{}, nil
}
