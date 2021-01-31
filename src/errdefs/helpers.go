package errdefs

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
