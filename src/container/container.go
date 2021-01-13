package container

//  "github.com/docker/docker/client"
//  "github.com/docker/docker/api/types"
//  "github.com/docker/docker/api/types/container"

//type Image interface {
//  pull(name string) bool
//  tag(tag string)
//  remove(hash string) bool
//  push() bool
//}

// TODO separate interface to different file
type Container interface {
	Pull(image string) bool
	Push(image string) bool
	Remove(imageid string) bool
	Tag(name string)
	ListImage() []string
	Build(image string) bool
	Run(image string, name string) bool
	// ...
}

type Docker struct {
	// ...
}

func (d Docker) Pull(image string) bool {
	return true
}

func (d Docker) Push(image string) bool {
	return true
}

func (d Docker) Build(image string) bool {
	return true
}

// ....
