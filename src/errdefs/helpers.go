package errdefs

type ErrBuildFailed struct{ error }

func (e ErrBuildFailed) Cause() error {
	return e.error
}

func BuildFailed(err error) error {
	if err == nil || IsBuildFailed(err) {
		return err
	}
	return ErrBuildFailed{err}
}
