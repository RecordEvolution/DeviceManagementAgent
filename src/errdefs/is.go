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
