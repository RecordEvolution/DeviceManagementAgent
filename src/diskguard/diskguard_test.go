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

// fakeDocker implements the Docker interface for volume-pruning tests.
type fakeDocker struct {
	volumes    []container.VolumeResult
	listErr    error
	removed    []string
	removeErrs map[string]error
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
	return nil, nil
}
func (f *fakeDocker) StopContainerByID(ctx context.Context, containerID string, timeout time.Duration) error {
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
