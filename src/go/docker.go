package main

import (
  "fmt"
  "time"
  "reflect"
  // "strings"

  "context"
  "os"
  "io"

  "github.com/docker/docker/client"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
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
  df, err := os.Open("/home/mariof/Downloads/test-repo.tar.gz")
  if err != nil {
    panic(err)
  }
  defer df.Close()
  fmt.Println(df)

  // build image
  fmt.Println(reflect.TypeOf(df))
  imagebuilt, err := cli.ImageBuild(ctx,df,types.ImageBuildOptions{
    Tags: []string{"test-repo:latest"},
    Remove: true })
  if err != nil {
    panic(err)
  }
  defer imagebuilt.Body.Close()
  // buffrep := new(strings.Builder)
  // buffstr, err := io.Copy(buffrep,imagebuilt.Body)
  // fmt.Println(buffstr)

  // buf := new(bytes.Buffer)
    // buf.ReadFrom(response.Body)
    // newStr := buf.String()

  _, err = io.Copy(os.Stdout, imagebuilt.Body)
  if err != nil {
    panic(err)
  }

  // create a container
  contcreate, err := cli.ContainerCreate(ctx, &container.Config{ Image: "test-repo:latest" },
                           &container.HostConfig{ AutoRemove: true }, //hostConfig *container.HostConfig,
                           nil, //networkingConfig *network.NetworkingConfig,
                           nil, //platform *specs.Platform,
                           "test-repo-cont")
  if err != nil {
    panic(err)
  }

  fmt.Println(contcreate.ID)
  fmt.Println(contcreate.Warnings)

  // start container
  errstr := cli.ContainerStart(ctx, contcreate.ID, types.ContainerStartOptions{})
  if errstr != nil {
    panic(errstr)
  }

  // retrieve container logs
  logs, err := cli.ContainerLogs(ctx, contcreate.ID, types.ContainerLogsOptions{ ShowStdout: true, ShowStderr: true})
  if err != nil {
    panic(err)
  }
  numbytes, err := io.Copy(os.Stdout,logs)
  if err != nil {
    panic(err)
  } else {
    fmt.Println("number of bytes in log",numbytes)
  }

  //  list containers
  containers, err := cli.ContainerList(ctx, types.ContainerListOptions{ All: true })
	if err != nil {
		panic(err)
	}
  fmt.Println(reflect.TypeOf(containers))
	for _, container := range containers {
		fmt.Printf("%s %s\n", container.ID[:10], container.Image)
	}


}
