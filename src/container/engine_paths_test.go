package container

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	dockercontainer "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/system"
	"github.com/docker/docker/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCreateContainerTranslatesMountsForWSLEngine points a real Docker client
// at a stand-in engine and checks the mount source the engine is sent.
func TestCreateContainerTranslatesMountsForWSLEngine(t *testing.T) {
	const windowsSource = `C:\ProgramData\IronFlock\Reagent\apps/prod/qds`
	for name, tc := range map[string]struct {
		paths *enginePaths
		want  string
	}{
		"WSL engine":     {paths: wslEngine(), want: "/mnt/c/ProgramData/IronFlock/Reagent/apps/prod/qds"},
		"Docker Desktop": {paths: nil, want: windowsSource},
	} {
		t.Run(name, func(t *testing.T) {
			var sent dockercontainer.HostConfig
			engine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !strings.HasSuffix(r.URL.Path, "/containers/create") {
					http.NotFound(w, r)
					return
				}
				var body struct{ HostConfig dockercontainer.HostConfig }
				_ = json.NewDecoder(r.Body).Decode(&body)
				sent = body.HostConfig
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{"Id":"abc","Warnings":[]}`))
			}))
			defer engine.Close()

			cli, err := client.NewClientWithOpts(client.WithHost("tcp://"+strings.TrimPrefix(engine.URL, "http://")), client.WithVersion("1.47"))
			require.NoError(t, err)
			d := &Docker{client: cli, paths: tc.paths}
			hostConfig := dockercontainer.HostConfig{Mounts: []mount.Mount{{Type: mount.TypeBind, Source: windowsSource, Target: "/data"}}}

			id, err := d.CreateContainer(context.Background(), dockercontainer.Config{Image: "qds"}, hostConfig, network.NetworkingConfig{}, "qds")

			require.NoError(t, err)
			assert.Equal(t, "abc", id)
			require.Len(t, sent.Mounts, 1)
			assert.Equal(t, tc.want, sent.Mounts[0].Source)
			assert.Equal(t, windowsSource, hostConfig.Mounts[0].Source, "the caller's host config is not modified")
		})
	}
}

// wslEngine is an enginePaths that already knows it talks to a WSL engine.
func wslEngine() *enginePaths {
	return &enginePaths{hostOS: "windows", known: true, wslDrives: true}
}

func TestWSLDrivePath(t *testing.T) {
	cases := map[string]string{
		// the agent joins its app paths with "/" onto a Windows apps dir
		`C:\ProgramData\IronFlock\Reagent\apps/prod/qds`: "/mnt/c/ProgramData/IronFlock/Reagent/apps/prod/qds",
		`C:\ProgramData\IronFlock\Reagent\apps\certs`:    "/mnt/c/ProgramData/IronFlock/Reagent/apps/certs",
		`D:/data/x`:        "/mnt/d/data/x",
		`c:\`:              "/mnt/c/",
		`C:`:               "/mnt/c",
		"/var/lib/docker":  "/var/lib/docker",
		`\\server\share\x`: `\\server\share\x`,
		"qds_data":         "qds_data",
		"./data":           "./data",
		"":                 "",
	}
	for in, want := range cases {
		assert.Equal(t, want, wslDrivePath(in), "wslDrivePath(%q)", in)
	}
}

func TestNeedsWSLDrivePaths(t *testing.T) {
	ubuntu := system.Info{OSType: "linux", OperatingSystem: "Ubuntu 24.04.3 LTS"}
	desktop := system.Info{OSType: "linux", OperatingSystem: "Docker Desktop"}
	windowsEngine := system.Info{OSType: "windows", OperatingSystem: "Windows Server 2022"}

	assert.True(t, needsWSLDrivePaths("windows", ubuntu), "WSL distro engine from a Windows agent")
	assert.False(t, needsWSLDrivePaths("windows", desktop), "Docker Desktop translates Windows paths itself")
	assert.False(t, needsWSLDrivePaths("windows", windowsEngine), "a Windows engine opens Windows paths")
	assert.False(t, needsWSLDrivePaths("linux", ubuntu), "a Linux agent passes Linux paths")
}

func TestEnginePathsAsksOnceAndRetriesAfterError(t *testing.T) {
	calls := 0
	daemonUp := false
	e := newEnginePaths("windows", func(ctx context.Context) (system.Info, error) {
		calls++
		if !daemonUp {
			return system.Info{}, errors.New("daemon not reachable")
		}
		return system.Info{OSType: "linux", OperatingSystem: "Ubuntu 24.04.3 LTS"}, nil
	})

	assert.False(t, e.wslDriveMounts(context.Background()), "unknown engine: paths stay unchanged")
	daemonUp = true
	assert.True(t, e.wslDriveMounts(context.Background()), "asks again once the daemon answers")
	assert.True(t, e.wslDriveMounts(context.Background()))
	assert.Equal(t, 2, calls, "the answer is remembered after the first success")
}

func TestEnginePathsDockerDesktopIsRememberedToo(t *testing.T) {
	calls := 0
	e := newEnginePaths("windows", func(ctx context.Context) (system.Info, error) {
		calls++
		return system.Info{OSType: "linux", OperatingSystem: "Docker Desktop"}, nil
	})
	assert.False(t, e.wslDriveMounts(context.Background()))
	assert.False(t, e.wslDriveMounts(context.Background()))
	assert.Equal(t, 1, calls)
}

func TestEnginePathsNeverAsksOffWindows(t *testing.T) {
	e := newEnginePaths("linux", func(ctx context.Context) (system.Info, error) {
		t.Fatal("a Linux agent must not ask the engine")
		return system.Info{}, nil
	})
	assert.False(t, e.wslDriveMounts(context.Background()))

	var unset *enginePaths
	assert.False(t, unset.wslDriveMounts(context.Background()), "nil (tests, hand-built Docker) means unchanged")
}

func TestWSLDriveMountsFor(t *testing.T) {
	in := []mount.Mount{
		{Type: mount.TypeBind, Source: `C:\ProgramData\IronFlock\Reagent\apps/prod/qds`, Target: "/data"},
		{Type: mount.TypeVolume, Source: "qds_data", Target: "/var/lib/qds"},
		{Type: mount.TypeBind, Source: "/sys/bus/w1/devices", Target: "/sys/bus/w1/devices"},
	}
	out := wslDriveMountsFor(in)

	require.Len(t, out, 3)
	assert.Equal(t, "/mnt/c/ProgramData/IronFlock/Reagent/apps/prod/qds", out[0].Source)
	assert.Equal(t, "/data", out[0].Target)
	assert.Equal(t, "qds_data", out[1].Source, "named volumes are not paths")
	assert.Equal(t, "/sys/bus/w1/devices", out[2].Source, "Linux paths stay")
	assert.Equal(t, `C:\ProgramData\IronFlock\Reagent\apps/prod/qds`, in[0].Source, "the caller's mounts are not modified")
	assert.Nil(t, wslDriveMountsFor(nil))
}

const testComposeDir = `C:\ProgramData\IronFlock\Reagent\apps\compose\qds`

func composeFromJSON(t *testing.T, doc string) map[string]interface{} {
	t.Helper()
	var compose map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(doc), &compose))
	return compose
}

func TestWSLDriveCompose(t *testing.T) {
	compose := composeFromJSON(t, `{
		"services": {
			"qds": {
				"volumes": [
					"C:\\ProgramData\\IronFlock\\Reagent\\apps\\prod\\qds\\env:/data/env:ro",
					"./data:/data",
					"../shared:/shared:rw",
					"qds_data:/var/lib/qds",
					"/opt/config:/config:ro",
					"/anonymous",
					{"type": "bind", "source": "./certs", "target": "/certs"},
					{"type": "volume", "source": "qds_files", "target": "/files"}
				]
			},
			"nginx": {"image": "nginx"}
		},
		"secrets": {"api_key": {"file": "./secrets/key.txt"}, "external_one": {"external": true}},
		"configs": {"nginx": {"file": "C:\\cfg\\nginx.conf"}}
	}`)

	require.True(t, wslDriveCompose(compose, testComposeDir))

	volumes := compose["services"].(map[string]interface{})["qds"].(map[string]interface{})["volumes"].([]interface{})
	assert.Equal(t, "/mnt/c/ProgramData/IronFlock/Reagent/apps/prod/qds/env:/data/env:ro", volumes[0])
	assert.Equal(t, "/mnt/c/ProgramData/IronFlock/Reagent/apps/compose/qds/data:/data", volumes[1], "relative to the compose file's directory")
	assert.Equal(t, "/mnt/c/ProgramData/IronFlock/Reagent/apps/compose/shared:/shared:rw", volumes[2])
	assert.Equal(t, "qds_data:/var/lib/qds", volumes[3], "named volume")
	assert.Equal(t, "/opt/config:/config:ro", volumes[4], "a path inside the engine")
	assert.Equal(t, "/anonymous", volumes[5], "anonymous volume has no host side")
	assert.Equal(t, "/mnt/c/ProgramData/IronFlock/Reagent/apps/compose/qds/certs", volumes[6].(map[string]interface{})["source"])
	assert.Equal(t, "qds_files", volumes[7].(map[string]interface{})["source"])

	secrets := compose["secrets"].(map[string]interface{})
	assert.Equal(t, "/mnt/c/ProgramData/IronFlock/Reagent/apps/compose/qds/secrets/key.txt", secrets["api_key"].(map[string]interface{})["file"])
	assert.NotContains(t, secrets["external_one"].(map[string]interface{}), "file")
	assert.Equal(t, "/mnt/c/cfg/nginx.conf", compose["configs"].(map[string]interface{})["nginx"].(map[string]interface{})["file"])
}

func TestWSLDriveComposeLeavesEnginePathsAlone(t *testing.T) {
	compose := composeFromJSON(t, `{"services": {"db": {"volumes": ["db_data:/var/lib/db", "/etc/localtime:/etc/localtime:ro"]}}}`)
	assert.False(t, wslDriveCompose(compose, testComposeDir))
}

// writeCompose writes a docker-compose.json into a fresh directory.
func writeCompose(t *testing.T, doc string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "docker-compose.json")
	require.NoError(t, os.WriteFile(path, []byte(doc), 0644))
	return path
}

const windowsBindCompose = `{"name": "prod_6_qds", "services": {"qds": {"image": "qds", "volumes": ["C:\\apps\\prod\\qds\\env:/data/env:ro"]}}}`

func TestEngineComposeFileRewritesForWSLEngine(t *testing.T) {
	original := writeCompose(t, windowsBindCompose)
	c := &Compose{paths: wslEngine()}

	got := c.engineComposeFile(context.Background(), original)

	assert.Equal(t, filepath.Join(filepath.Dir(original), EngineComposeFileName), got, "a copy next to the original")
	raw, err := os.ReadFile(got)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `/mnt/c/apps/prod/qds/env:/data/env:ro`)
	assert.Contains(t, string(raw), `"name":"prod_6_qds"`, "same project name")
	unchanged, err := os.ReadFile(original)
	require.NoError(t, err)
	assert.Equal(t, windowsBindCompose, string(unchanged), "the original stays as written")
}

func TestEngineComposeFileOnlyWritesOnChange(t *testing.T) {
	original := writeCompose(t, windowsBindCompose)
	c := &Compose{paths: wslEngine()}
	enginePath := c.engineComposeFile(context.Background(), original)

	old := time.Now().Add(-time.Hour).Truncate(time.Second)
	require.NoError(t, os.Chtimes(enginePath, old, old))
	c.engineComposeFile(context.Background(), original)

	info, err := os.Stat(enginePath)
	require.NoError(t, err)
	assert.True(t, info.ModTime().Equal(old), "same content is not rewritten")
}

func TestEngineComposeFileKeepsOriginal(t *testing.T) {
	cases := map[string]struct {
		paths *enginePaths
		doc   string
	}{
		"no WSL engine":         {paths: nil, doc: windowsBindCompose},
		"Docker Desktop engine": {paths: &enginePaths{hostOS: "windows", known: true, wslDrives: false}, doc: windowsBindCompose},
		"nothing to rewrite":    {paths: wslEngine(), doc: `{"services": {"db": {"volumes": ["db_data:/var/lib/db"]}}}`},
		"unparsable file":       {paths: wslEngine(), doc: `{not json`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			original := writeCompose(t, tc.doc)
			c := &Compose{paths: tc.paths}
			assert.Equal(t, original, c.engineComposeFile(context.Background(), original))
			_, err := os.Stat(filepath.Join(filepath.Dir(original), EngineComposeFileName))
			assert.True(t, os.IsNotExist(err), "no engine copy written")
		})
	}
}

// fakeComposeCLI installs a stand-in for the docker CLI that records its
// arguments, so a test sees exactly which compose file a command was given.
func fakeComposeCLI(t *testing.T) (binary string, argsFile string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake CLI is a shell script")
	}
	dir := t.TempDir()
	argsFile = filepath.Join(dir, "args")
	binary = filepath.Join(dir, "docker")
	script := "#!/bin/sh\necho \"$@\" > '" + argsFile + "'\n"
	require.NoError(t, os.WriteFile(binary, []byte(script), 0755))
	return binary, argsFile
}

func TestComposeCommandHandsTheCLITheEngineFile(t *testing.T) {
	for name, tc := range map[string]struct {
		paths      *enginePaths
		engineFile bool
	}{
		"WSL engine":     {paths: wslEngine(), engineFile: true},
		"Docker Desktop": {paths: nil, engineFile: false},
	} {
		t.Run(name, func(t *testing.T) {
			binary, argsFile := fakeComposeCLI(t)
			original := writeCompose(t, windowsBindCompose)
			c := NewComposeWithBinary(nil, binary)
			c.paths = tc.paths

			require.NoError(t, c.run(context.Background(), original, "up", "-d"))

			raw, err := os.ReadFile(argsFile)
			require.NoError(t, err)
			args := strings.TrimSpace(string(raw))
			want := original
			if tc.engineFile {
				want = filepath.Join(filepath.Dir(original), EngineComposeFileName)
			}
			assert.Equal(t, "compose -f "+want+" up -d", args)
		})
	}
}
