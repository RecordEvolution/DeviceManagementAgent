package errdefs

import "errors"

type ErrBuildFailed struct{ error }

func (e ErrBuildFailed) Cause() error {
	return e.error
}

func (e ErrBuildFailed) Unwrap() error {
	return e.error
}

func BuildFailed(err error) error {
	if err == nil || IsBuildFailed(err) {
		return err
	}
	return ErrBuildFailed{err}
}

/*------------*/

type ErrContainerNameAlreadyInUse struct{ error }

func (e ErrContainerNameAlreadyInUse) Cause() error {
	return e.error
}

func (e ErrContainerNameAlreadyInUse) Unwrap() error {
	return e.error
}

func ContainerNameAlreadyInUse(err error) error {
	if err == nil || IsContainerNameAlreadyInUse(err) {
		return err
	}

	return ErrContainerNameAlreadyInUse{err}
}

/*------------*/

type ErrContainerNotFound struct{ error }

func (e ErrContainerNotFound) Cause() error {
	return e.error
}

func (e ErrContainerNotFound) Unwrap() error {
	return e.error
}

func ContainerNotFound(err error) error {
	if err == nil || IsContainerNotFound(err) {
		return err
	}

	return ErrContainerNotFound{err}
}

/*------------*/

type ErrImageNotFound struct{ error }

func (e ErrImageNotFound) Cause() error {
	return e.error
}

func (e ErrImageNotFound) Unwrap() error {
	return e.error
}

func ImageNotFound(err error) error {
	if err == nil || IsImageNotFound(err) {
		return err
	}

	return ErrImageNotFound{err}
}

/*------------*/

type ErrContainerRemovalAlreadyInProgress struct{ error }

func (e ErrContainerRemovalAlreadyInProgress) Cause() error {
	return e.error
}

func (e ErrContainerRemovalAlreadyInProgress) Unwrap() error {
	return e.error
}

func ContainerRemovalAlreadyInProgress(err error) error {
	if err == nil || IsContainerRemovalAlreadyInProgress(err) {
		return err
	}

	return ErrContainerRemovalAlreadyInProgress{err}
}

/*-----------*/

type ErrRegistrationHandlerFailed struct {
	err error
	URI string
}

func (e ErrRegistrationHandlerFailed) Error() string {
	return e.err.Error()
}

func (e ErrRegistrationHandlerFailed) Cause() error {
	return e.err
}

func (e ErrRegistrationHandlerFailed) Unwrap() error {
	return e.err
}

func RegistrationHandlerFailed(err error, URI string) error {
	if err == nil || IsRegistrationHandlerFailed(err) {
		return err
	}

	return ErrRegistrationHandlerFailed{err, URI}
}

/*-----------*/

type ErrDockerfileCannotBeEmpty struct {
	error
}

func (e ErrDockerfileCannotBeEmpty) Cause() error {
	return e.error
}

func (e ErrDockerfileCannotBeEmpty) Unwrap() error {
	return e.error
}

func DockerfileCannotBeEmpty(err error) error {
	if err == nil || IsDockerfileCannotBeEmpty(err) {
		return err
	}

	return ErrDockerfileCannotBeEmpty{err}
}

/*-----------*/

type ErrDockerfileIsMissing struct {
	error
}

func (e ErrDockerfileIsMissing) Cause() error {
	return e.error
}

func (e ErrDockerfileIsMissing) Unwrap() error {
	return e.error
}

func DockerfileIsMissing(err error) error {
	if err == nil || IsDockerfileIsMissing(err) {
		return err
	}

	return ErrDockerfileIsMissing{err}
}

/*-----------*/

type ErrDockerBuildFilesNotFound struct {
	error
}

func (e ErrDockerBuildFilesNotFound) Cause() error {
	return e.error
}

func (e ErrDockerBuildFilesNotFound) Unwrap() error {
	return e.error
}

func DockerBuildFilesNotFound(err error) error {
	if err == nil || IsDockerBuildFilesNotFound(err) {
		return err
	}

	return ErrDockerBuildFilesNotFound{err}
}

/*-----------*/

type ErrDockerStreamCanceled struct {
	error
}

func (e ErrDockerStreamCanceled) Cause() error {
	return e.error
}

func (e ErrDockerStreamCanceled) Unwrap() error {
	return e.error
}

func DockerStreamCanceled(err error) error {
	if err == nil || IsDockerStreamCanceled(err) {
		return err
	}

	return ErrDockerStreamCanceled{err}
}

/*-----------*/

type ErrNoActionTransition struct {
	error
}

func (e ErrNoActionTransition) Cause() error {
	return e.error
}

func (e ErrNoActionTransition) Unwrap() error {
	return e.error
}

func NoActionTransition() error {
	return ErrNoActionTransition{errors.New("no action")}
}

/*-----------*/

type ErrInProgress struct {
	error
}

func (e ErrInProgress) Cause() error {
	return e.error
}

func (e ErrInProgress) Unwrap() error {
	return e.error
}

func InProgress(err error) error {
	return ErrInProgress{err}
}
