package errdefs

func IsBuildFailed(err error) bool {
	_, ok := err.(ErrBuildFailed)
	return ok
}
