package diskguard

import "testing"

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
