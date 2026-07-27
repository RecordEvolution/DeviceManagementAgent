package diskguard

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reagent/common"
	"reagent/container"
	"testing"
	"time"
)

func TestDecide(t *testing.T) {
	g := New(nil, Config{}) // defaults: warn 3 GiB, emergency 1 GiB

	const mb = int64(1 << 20)
	const gb = int64(1 << 30)
	cases := []struct {
		freeBytes int64
		want      action
	}{
		{50 * gb, actNone},
		{3 * gb, actNone}, // boundary: at warn, not below
		{3*gb - 1, actWarn},
		{2 * gb, actWarn},
		{1 * gb, actWarn}, // boundary: at emergency, not below
		{1*gb - 1, actEmergency},
		{512 * mb, actEmergency},
		{0, actEmergency},
	}

	for _, c := range cases {
		if got := g.decide(c.freeBytes); got != c.want {
			t.Errorf("decide(%d) = %d, want %d", c.freeBytes, got, c.want)
		}
	}
}

func TestUpdateEmergencyHysteresis(t *testing.T) {
	g := New(nil, Config{}) // warn 3 GiB, emergency 1 GiB
	defer setEmergency(false)

	const gb = int64(1 << 30)

	// drop below emergency -> on
	g.updateEmergency(512 * 1024 * 1024)
	if !IsEmergency() {
		t.Fatal("expected emergency after dropping below 1 GiB")
	}
	// recover into the hysteresis band (between 1 and 3 GiB) -> still on
	g.updateEmergency(2 * gb)
	if !IsEmergency() {
		t.Fatal("expected emergency to hold in the hysteresis band")
	}
	// recover at/above warn -> off
	g.updateEmergency(3 * gb)
	if IsEmergency() {
		t.Fatal("expected emergency cleared at/above 3 GiB")
	}
}

// fakeDocker implements the Docker interface for volume-pruning and
// image-reclaim tests.
type fakeDocker struct {
	volumes       []container.VolumeResult
	listErr       error
	removed       []string
	removeErrs    map[string]error
	images        []container.ImageResult
	imageListErr  error
	containers    []container.ContainerResult
	containerErr  error
	removedImages []string
}

func (f *fakeDocker) PruneDanglingImages(ctx context.Context) (string, error) { return "", nil }
func (f *fakeDocker) PruneBuildCache(ctx context.Context) (string, error)     { return "", nil }
func (f *fakeDocker) ListDanglingVolumes(ctx context.Context) ([]container.VolumeResult, error) {
	return f.volumes, f.listErr
}
func (f *fakeDocker) RemoveVolume(ctx context.Context, name string) error {
	if err := f.removeErrs[name]; err != nil {
		return err
	}
	f.removed = append(f.removed, name)
	return nil
}
func (f *fakeDocker) ListContainers(ctx context.Context, options common.Dict) ([]container.ContainerResult, error) {
	return f.containers, f.containerErr
}
func (f *fakeDocker) StopContainerByID(ctx context.Context, containerID string, timeout time.Duration) error {
	return nil
}
func (f *fakeDocker) ListImages(ctx context.Context, options map[string]interface{}) ([]container.ImageResult, error) {
	return f.images, f.imageListErr
}
func (f *fakeDocker) RemoveImage(ctx context.Context, imageID string, options map[string]interface{}) error {
	f.removedImages = append(f.removedImages, imageID)
	return nil
}

const anonName = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func projectLabels(project string) map[string]string {
	return map[string]string{
		composeProjectLabel:          project,
		"com.docker.compose.volume":  "data",
		"com.docker.compose.version": "2.24.0",
	}
}

func TestPruneOrphanedVolumes(t *testing.T) {
	composeDir := t.TempDir()
	// Leaked app: compose dir still present (teardown removes volumes first,
	// dir last, so an interrupted uninstall always leaves the dir).
	if err := os.Mkdir(filepath.Join(composeDir, "Leaked_App"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Installed (possibly stopped) app also has its dir.
	if err := os.Mkdir(filepath.Join(composeDir, "myapp"), 0o755); err != nil {
		t.Fatal(err)
	}

	docker := &fakeDocker{volumes: []container.VolumeResult{
		{Name: anonName, Labels: map[string]string{}},                   // anonymous -> removed
		{Name: "user-data", Labels: map[string]string{}},                // named, unlabeled -> kept
		{Name: "myapp_data", Labels: projectLabels("myapp")},            // installed app -> kept
		{Name: "leaked_app_data", Labels: projectLabels("leaked_app")},  // gone from DB, dir exists -> removed
		{Name: "foreign_data", Labels: projectLabels("customer-stack")}, // not ours (no dir) -> kept
		{Name: "appl_db", Labels: projectLabels("ironflock-appliance")}, // platform stack -> kept
	}}

	g := New(docker, Config{
		AppsComposeDir: composeDir,
		InstalledAppNames: func() ([]string, error) {
			return []string{"MyApp"}, nil // exact-case name must still protect project "myapp"
		},
	})
	g.pruneOrphanedVolumes()

	want := map[string]bool{anonName: true, "leaked_app_data": true}
	if len(docker.removed) != len(want) {
		t.Fatalf("removed %v, want exactly %v", docker.removed, want)
	}
	for _, name := range docker.removed {
		if !want[name] {
			t.Errorf("removed %q, which must be kept", name)
		}
	}
}

func TestPruneOrphanedVolumesUnknownInstalledSet(t *testing.T) {
	composeDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(composeDir, "leaked_app"), 0o755); err != nil {
		t.Fatal(err)
	}

	docker := &fakeDocker{volumes: []container.VolumeResult{
		{Name: anonName, Labels: map[string]string{}},
		{Name: "leaked_app_data", Labels: projectLabels("leaked_app")},
	}}

	g := New(docker, Config{
		AppsComposeDir: composeDir,
		InstalledAppNames: func() ([]string, error) {
			return nil, errors.New("db unavailable")
		},
	})
	g.pruneOrphanedVolumes()

	// With the installed set unknown, no compose volume may be touched — but
	// anonymous ones are still safe.
	if len(docker.removed) != 1 || docker.removed[0] != anonName {
		t.Fatalf("removed %v, want only the anonymous volume", docker.removed)
	}
}

func TestPruneOrphanedVolumesNoCallback(t *testing.T) {
	docker := &fakeDocker{volumes: []container.VolumeResult{
		{Name: "leaked_app_data", Labels: projectLabels("leaked_app")},
	}}
	g := New(docker, Config{})
	g.pruneOrphanedVolumes()
	if len(docker.removed) != 0 {
		t.Fatalf("removed %v, want nothing without an installed-apps source", docker.removed)
	}
}

func TestNormalizeProjectName(t *testing.T) {
	cases := map[string]string{
		"MyApp":       "myapp",
		"Wire.Guard!": "wireguard",
		"_leading":    "leading",
		"ok-name_2":   "ok-name_2",
		"--strip":     "strip",
	}
	for in, want := range cases {
		if got := normalizeProjectName(in); got != want {
			t.Errorf("normalizeProjectName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsAnonymousVolumeName(t *testing.T) {
	if !isAnonymousVolumeName(anonName) {
		t.Error("64-hex name should be anonymous")
	}
	for _, name := range []string{"user-data", anonName[:63], anonName[:63] + "G", anonName + "0"} {
		if isAnonymousVolumeName(name) {
			t.Errorf("%q should not be anonymous", name)
		}
	}
}

func TestRegistryHostOf(t *testing.T) {
	cases := map[string]string{
		"localhost:15001/repo/prod_arm64_6_app:1.0.0":    "localhost:15001",
		"instance-registry.ironflock.com/caddy:2":        "instance-registry.ironflock.com",
		"192.168.1.50:5000/repo/app:1":                   "192.168.1.50:5000",
		"localhost/repo/app:1":                           "localhost", // Docker special-cases bare localhost
		"postgres:15":                                    "",          // no path segment at all
		"library/postgres:15":                            "",          // implicit Docker Hub namespace, not a host
		"registry.ironflock.com/repo/prod_x_1_app:2.0.0": "registry.ironflock.com",
	}
	for ref, want := range cases {
		if got := registryHostOf(ref); got != want {
			t.Errorf("registryHostOf(%q) = %q, want %q", ref, got, want)
		}
	}
}

func TestLocalRegistryHost(t *testing.T) {
	local := map[string]string{
		"localhost:15001/":  "localhost:15001",
		"localhost/":        "localhost",
		"LocalHost:15001/":  "LocalHost:15001",
		"127.0.0.1:5000/":   "127.0.0.1:5000",
		"192.168.1.50:5000": "192.168.1.50:5000", // trailing slash is optional
		"10.0.0.7:5000/":    "10.0.0.7:5000",
		"172.16.4.2:5000/":  "172.16.4.2:5000",
		"[::1]:5000/":       "[::1]:5000",
	}
	for url, wantHost := range local {
		host, ok := localRegistryHost(url)
		if !ok || host != wantHost {
			t.Errorf("localRegistryHost(%q) = (%q, %v), want (%q, true)", url, host, ok, wantHost)
		}
	}

	// A WAN registry (cloud device) or an unresolvable name must NOT qualify —
	// removing a tagged image there could strand an offline device.
	for _, url := range []string{
		"registry.ironflock.com/",
		"instance-registry.ironflock.com/",
		"europe-docker.pkg.dev/",
		"8.8.8.8:5000/",
		"",
		"/",
	} {
		if host, ok := localRegistryHost(url); ok {
			t.Errorf("localRegistryHost(%q) = (%q, true), want not local", url, host)
		}
	}
}

func TestReclaimSupersededAppImages(t *testing.T) {
	const reg = "localhost:15001/"
	docker := &fakeDocker{
		containers: []container.ContainerResult{
			{ID: "c1", ImageID: "sha256:running"},
			{ID: "c2", ImageID: "sha256:stopped", State: "exited"},
		},
		images: []container.ImageResult{
			// pinned by a running container -> kept even though not wanted
			{ID: "sha256:running", RepoTags: []string{reg + "repo/prod_arm64_6_app:0.9.0"}},
			// pinned by a STOPPED container (app is meant to start again) -> kept
			{ID: "sha256:stopped", RepoTags: []string{reg + "repo/prod_arm64_7_other:0.1.0"}},
			// wanted present version, unreferenced -> kept
			{ID: "sha256:present", RepoTags: []string{reg + "repo/prod_arm64_6_app:1.0.0"}},
			// wanted newest version being pulled by an in-flight update -> kept
			{ID: "sha256:newest", RepoTags: []string{reg + "repo/prod_arm64_6_app:1.1.0"}},
			// superseded leftovers -> removed
			{ID: "sha256:old1", RepoTags: []string{reg + "repo/prod_arm64_6_app:0.8.0"}},
			{ID: "sha256:old2", RepoTags: []string{reg + "repo/dev_arm64_6_app:0.5.0"}},
			// multi-tag image: only the superseded tag goes, the wanted one stays
			{ID: "sha256:multi", RepoTags: []string{
				reg + "repo/prod_arm64_9_multi:2.0.0", // wanted
				reg + "repo/prod_arm64_9_multi:1.0.0", // superseded
			}},
			// another registry (stack images, Docker Hub) -> never touched
			{ID: "sha256:stack", RepoTags: []string{"instance-registry.ironflock.com/caddy:1"}},
			{ID: "sha256:hub", RepoTags: []string{"postgres:14"}},
			// dangling, no tags -> left to the dangling prune
			{ID: "sha256:dangling", RepoTags: nil},
		},
	}

	g := New(docker, Config{
		AppImageRegistry: reg,
		WantedAppImages: func() (map[string]bool, error) {
			return map[string]bool{
				reg + "repo/prod_arm64_6_app:1.0.0":   true,
				reg + "repo/prod_arm64_6_app:1.1.0":   true,
				reg + "repo/prod_arm64_9_multi:2.0.0": true,
			}, nil
		},
	})
	g.reclaimSupersededAppImages()

	want := map[string]bool{
		reg + "repo/prod_arm64_6_app:0.8.0":   true,
		reg + "repo/dev_arm64_6_app:0.5.0":    true,
		reg + "repo/prod_arm64_9_multi:1.0.0": true,
	}
	if len(docker.removedImages) != len(want) {
		t.Fatalf("removed %v, want exactly %v", docker.removedImages, want)
	}
	for _, tag := range docker.removedImages {
		if !want[tag] {
			t.Errorf("removed %q, which must be kept", tag)
		}
	}
}

func TestReclaimSupersededAppImagesSkipped(t *testing.T) {
	const reg = "localhost:15001/"
	images := []container.ImageResult{
		{ID: "sha256:old", RepoTags: []string{reg + "repo/prod_arm64_6_app:0.8.0"}},
		{ID: "sha256:cloud", RepoTags: []string{"registry.ironflock.com/repo/prod_arm64_6_app:0.8.0"}},
	}
	wantedOK := func() (map[string]bool, error) { return map[string]bool{}, nil }

	cases := []struct {
		name string
		cfg  Config
	}{
		// Cloud device: images can only be re-pulled over the WAN, so tagged
		// app images stay put no matter how low the disk is.
		{"remote registry", Config{AppImageRegistry: "registry.ironflock.com/", WantedAppImages: wantedOK}},
		{"no registry configured", Config{WantedAppImages: wantedOK}},
		{"no wanted-set source", Config{AppImageRegistry: reg}},
		{"wanted set unavailable", Config{AppImageRegistry: reg, WantedAppImages: func() (map[string]bool, error) {
			return nil, errors.New("db unavailable")
		}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			docker := &fakeDocker{images: images}
			New(docker, c.cfg).reclaimSupersededAppImages()
			if len(docker.removedImages) != 0 {
				t.Fatalf("removed %v, want nothing", docker.removedImages)
			}
		})
	}
}

func TestReclaimSupersededAppImagesContainerListFails(t *testing.T) {
	const reg = "localhost:15001/"
	// The in-use set is unknown, so nothing may be removed — an image that
	// looks unreferenced might be pinned by a container we failed to list.
	docker := &fakeDocker{
		containerErr: errors.New("daemon wedged"),
		images: []container.ImageResult{
			{ID: "sha256:old", RepoTags: []string{reg + "repo/prod_arm64_6_app:0.8.0"}},
		},
	}
	g := New(docker, Config{
		AppImageRegistry: reg,
		WantedAppImages:  func() (map[string]bool, error) { return map[string]bool{}, nil },
	})
	g.reclaimSupersededAppImages()
	if len(docker.removedImages) != 0 {
		t.Fatalf("removed %v, want nothing when the container list is unavailable", docker.removedImages)
	}
}

func TestConfigDefaults(t *testing.T) {
	g := New(nil, Config{})
	if g.cfg.WarnFreeBytes != 3<<30 || g.cfg.EmergencyFreeBytes != 1<<30 {
		t.Errorf("unexpected default thresholds: %+v", g.cfg)
	}
	if g.cfg.DataRoot != "/var/lib/docker" {
		t.Errorf("unexpected default DataRoot: %q", g.cfg.DataRoot)
	}

	g2 := New(nil, Config{WarnFreeBytes: 10 << 30, DataRoot: "/data"})
	if g2.cfg.WarnFreeBytes != 10<<30 || g2.cfg.DataRoot != "/data" {
		t.Errorf("explicit config not preserved: %+v", g2.cfg)
	}
}
