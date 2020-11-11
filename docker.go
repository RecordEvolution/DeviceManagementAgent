package main

import (
  "fmt"
  "time"
  "reflect"

  "context"
  // "io"
  "os"

  "github.com/docker/docker/client"
	"github.com/docker/docker/api/types"
	// "github.com/docker/docker/api/types/container"
	// "github.com/docker/docker/pkg/stdcopy"
)

func main() {

  // create a non-nil, empty context
  ctx := context.Background()

  // initializes a new API client
  cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
  if err != nil {
    panic(err)
  }

  // list images
  images, err := cli.ImageList(ctx, types.ImageListOptions{})
  if err != nil {
    panic(err)
  }
  nowsec := time.Now().Unix()
  fmt.Println(reflect.TypeOf(images))
  for _, image := range images {
    fmt.Println(image.RepoTags,image.ID,float32(nowsec-image.Created)/3600.,float32(image.Size)/1000000)
  }

  // creater io.Reader for Dockerfile
  df, err := os.Open("/home/mariof/Downloads/test-repo.tar")
  if err != nil {
    panic(err)
  }
  defer df.Close()
  fmt.Println(df)

  // build image
  fmt.Println(reflect.TypeOf(df))
  built, err := cli.ImageBuild(ctx,df,types.ImageBuildOptions{
    Tags:   []string{"imagename"}})
  if err != nil {
    panic(err)
  }
  fmt.Println(built)

  //  list containers
  containers, err := cli.ContainerList(ctx, types.ContainerListOptions{})
	if err != nil {
		panic(err)
	}
  fmt.Println(reflect.TypeOf(containers))
	for _, container := range containers {
		fmt.Printf("%s %s\n", container.ID[:10], container.Image)
	}



    // reader, err := cli.ImagePull(ctx, "docker.io/library/alpine", types.ImagePullOptions{})
    // if err != nil {
    //     panic(err)
    // }
    // io.Copy(os.Stdout, reader)
    //
    // resp, err := cli.ContainerCreate(ctx, &container.Config{
    //     Image: "alpine",
    //     Cmd:   []string{"echo", "hello world"},
    // }, nil, nil, nil, "")
    // if err != nil {
    //     panic(err)
    // }
    //
    // if err := cli.ContainerStart(ctx, resp.ID, types.ContainerStartOptions{}); err != nil {
    //     panic(err)
    // }
    //
    // statusCh, errCh := cli.ContainerWait(ctx, resp.ID, container.WaitConditionNotRunning)
    // select {
    // case err := <-errCh:
    //     if err != nil {
    //         panic(err)
    //     }
    // case <-statusCh:
    // }
    //
    // out, err := cli.ContainerLogs(ctx, resp.ID, types.ContainerLogsOptions{ShowStdout: true})
    // if err != nil {
    //     panic(err)
    // }
    //
    // stdcopy.StdCopy(os.Stdout, os.Stderr, out)
}
