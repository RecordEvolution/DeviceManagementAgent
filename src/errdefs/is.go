package errdefs

func IsBuildFailed(err error) bool {
	_, ok := err.(ErrBuildFailed)
	return ok
}

func IsContainerNameAlreadyInUse(err error) bool {
	_, ok := err.(ErrContainerNameAlreadyInUse)
	return ok
}

func IsContainerNotFound(err error) bool {
	_, ok := err.(ErrContainerNotFound)
	return ok
}

func IsImageNotFound(err error) bool {
	_, ok := err.(ErrImageNotFound)
	return ok
}

func IsContainerRemovalAlreadyInProgress(err error) bool {
	_, ok := err.(ErrContainerRemovalAlreadyInProgress)
	return ok
}

func IsRegistrationHandlerFailed(err error) bool {
	_, ok := err.(ErrContainerRemovalAlreadyInProgress)
	return ok
}

func IsDockerfileCannotBeEmpty(err error) bool {
	_, ok := err.(ErrDockerfileCannotBeEmpty)
	return ok
}

func IsDockerfileIsMissing(err error) bool {
	_, ok := err.(ErrDockerfileIsMissing)
	return ok
}

func IsDockerStreamCanceled(err error) bool {
	_, ok := err.(ErrDockerStreamCanceled)
	return ok
}

func IsDockerComposeNotSupported(err error) bool {
	_, ok := err.(ErrDockerComposeNotSupported)
	return ok
}

func IsNoActionTransition(err error) bool {
	_, ok := err.(ErrNoActionTransition)
	return ok
}

func IsDockerBuildFilesNotFound(err error) bool {
	_, ok := err.(ErrDockerBuildFilesNotFound)
	return ok
}

func IsInProgress(err error) bool {
	_, ok := err.(ErrInProgress)
	return ok
}

func IsInsufficientPrivileges(err error) bool {
	_, ok := err.(ErrInsufficientPrivileges)
	return ok
}
