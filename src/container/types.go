package container

import (
	"context"
	"io"
)

type Container interface {
	Build(ctx context.Context, pathToTar string, options interface{}) (io.ReadCloser, error)
	Login(ctx context.Context, username string, password string, registryURL string) (string, error)
}
