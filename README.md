
## Docker SDK for Go

In order to implement a _daemon_ an IoT device that is able to manage, create,
stop and remove _Docker containers_ we use the officially supported _Go_ SDK.
Please remember that on a default docker setup on a Linux platform we always
have to launch the daemon with root permissions, since the docker daemon is
accessed via the socket `///var/run/docker.sock` which is only readable with
root permissions by default.

### References

- https://docs.docker.com/engine/api/sdk/
- https://docs.docker.com/engine/api/sdk/examples/

- https://godoc.org/github.com/docker/docker/client
- https://github.com/docker/go-docker
- https://docs.docker.com/engine/api/


- https://github.com/moby/moby
- https://github.com/moby/moby/tree/master/client

#### Definition of types used in API

- https://godoc.org/github.com/docker/docker/api/types#Container

#### Deprecated API

- https://github.com/docker/engine-api/

#### Python

- https://docker-py.readthedocs.io/en/stable/
