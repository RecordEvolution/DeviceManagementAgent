package common

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildContainerName(t *testing.T) {
	tests := []struct {
		name     string
		stage    Stage
		appKey   uint64
		appName  string
		expected string
	}{
		{"prod stage lowercased", PROD, 6, "MyApp", "prod_6_myapp"},
		{"dev stage", DEV, 42, "netdata", "dev_42_netdata"},
		{"already lowercase name", PROD, 1, "app", "prod_1_app"},
		{"mixed case stage", "Pub", 3, "Foo", "pub_3_foo"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, BuildContainerName(tt.stage, tt.appKey, tt.appName))
		})
	}
}

func TestBuildComposeContainerName(t *testing.T) {
	tests := []struct {
		name     string
		stage    Stage
		appKey   uint64
		appName  string
		expected string
	}{
		{"prod compose", PROD, 6, "MyApp", "prod_6_myapp_compose"},
		{"dev compose", DEV, 42, "netdata", "dev_42_netdata_compose"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, BuildComposeContainerName(tt.stage, tt.appKey, tt.appName))
		})
	}
}

func TestBuildImageName(t *testing.T) {
	assert.Equal(t, "prod_arm64_6_myapp", BuildImageName(PROD, "arm64", 6, "MyApp"))
	assert.Equal(t, "dev_amd64_1_app", BuildImageName(DEV, "amd64", 1, "app"))
}

func TestBuildRegistryImageName(t *testing.T) {
	assert.Equal(t,
		"registry.example.com/main/dev_arm64_6_myapp",
		BuildRegistryImageName("registry.example.com/", "main/", "dev_arm64_6_myapp"),
	)
	// the whole result is lowercased
	assert.Equal(t,
		"registry.example.com/main/img",
		BuildRegistryImageName("Registry.Example.com/", "Main/", "IMG"),
	)
}

func TestBuildDockerIDs(t *testing.T) {
	assert.Equal(t, "build_6_myapp", BuildDockerBuildID(6, "myapp"))
	assert.Equal(t, "pull_6_myapp", BuildDockerPullID(6, "myapp"))
	assert.Equal(t, "push_6_myapp", BuildDockerPushID(6, "myapp"))
}

func TestBuildLogTopic(t *testing.T) {
	assert.Equal(t, "reswarm.logs.serial-1.prod_6_myapp", BuildLogTopic("serial-1", "prod_6_myapp"))
}

func TestBuildExternalApiTopic(t *testing.T) {
	assert.Equal(t, "re.mgmt.serial-1.some_topic", BuildExternalApiTopic("serial-1", "some_topic"))
}

func TestBuildTunnelStateUpdate(t *testing.T) {
	// trailing /onreload suffix on the topic
	assert.Equal(t, "re.mgmt.serial-1.tunnel_state_update/onreload", BuildTunnelStateUpdate("serial-1"))
}

func TestEscapeNewlineCharacters(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"newline", "a\nb", `a\nb`},
		{"tab", "a\tb", `a\tb`},
		{"carriage return", "a\rb", `a\rb`},
		{"all three", "a\nb\tc\rd", `a\nb\tc\rd`},
		{"no special chars", "abc", "abc"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, EscapeNewlineCharacters(tt.input))
		})
	}
}

func TestEnvironmentVarsToStringArray(t *testing.T) {
	t.Run("empty map yields empty (non-nil) slice", func(t *testing.T) {
		got := EnvironmentVarsToStringArray(map[string]interface{}{})
		require.NotNil(t, got)
		assert.Empty(t, got)
	})

	t.Run("single entry formats key=value", func(t *testing.T) {
		got := EnvironmentVarsToStringArray(map[string]interface{}{
			"FOO": map[string]interface{}{"value": "bar"},
		})
		assert.Equal(t, []string{"FOO=bar"}, got)
	})

	t.Run("non-string value is stringified", func(t *testing.T) {
		got := EnvironmentVarsToStringArray(map[string]interface{}{
			"NUM": map[string]interface{}{"value": 42},
		})
		assert.Equal(t, []string{"NUM=42"}, got)
	})

	t.Run("newlines in value are escaped", func(t *testing.T) {
		got := EnvironmentVarsToStringArray(map[string]interface{}{
			"MULTI": map[string]interface{}{"value": "a\nb"},
		})
		assert.Equal(t, []string{`MULTI=a\nb`}, got)
	})

	t.Run("nil value is stringified as <nil>", func(t *testing.T) {
		got := EnvironmentVarsToStringArray(map[string]interface{}{
			"EMPTY": map[string]interface{}{"value": nil},
		})
		assert.Equal(t, []string{"EMPTY=<nil>"}, got)
	})

	t.Run("multiple entries are all present", func(t *testing.T) {
		got := EnvironmentVarsToStringArray(map[string]interface{}{
			"A": map[string]interface{}{"value": "1"},
			"B": map[string]interface{}{"value": "2"},
		})
		assert.ElementsMatch(t, []string{"A=1", "B=2"}, got)
	})
}

func TestEnvironmentTemplateToStringArray(t *testing.T) {
	t.Run("empty map yields empty (non-nil) slice", func(t *testing.T) {
		got := EnvironmentTemplateToStringArray(map[string]interface{}{})
		require.NotNil(t, got)
		assert.Empty(t, got)
	})

	t.Run("uses defaultValue field", func(t *testing.T) {
		got := EnvironmentTemplateToStringArray(map[string]interface{}{
			"FOO": map[string]interface{}{"defaultValue": "bar"},
		})
		assert.Equal(t, []string{"FOO=bar"}, got)
	})

	t.Run("nil defaultValue is skipped entirely", func(t *testing.T) {
		got := EnvironmentTemplateToStringArray(map[string]interface{}{
			"SKIP": map[string]interface{}{"defaultValue": nil},
		})
		assert.Empty(t, got)
	})

	t.Run("missing defaultValue key is skipped", func(t *testing.T) {
		got := EnvironmentTemplateToStringArray(map[string]interface{}{
			"NOKEY": map[string]interface{}{"other": "x"},
		})
		assert.Empty(t, got)
	})

	t.Run("newlines in defaultValue are escaped", func(t *testing.T) {
		got := EnvironmentTemplateToStringArray(map[string]interface{}{
			"MULTI": map[string]interface{}{"defaultValue": "a\nb"},
		})
		assert.Equal(t, []string{`MULTI=a\nb`}, got)
	})

	t.Run("non-nil entry kept, nil entry dropped", func(t *testing.T) {
		got := EnvironmentTemplateToStringArray(map[string]interface{}{
			"KEEP": map[string]interface{}{"defaultValue": "yes"},
			"DROP": map[string]interface{}{"defaultValue": nil},
		})
		assert.Equal(t, []string{"KEEP=yes"}, got)
	})
}

func TestParseExitCodeFromContainerStatus(t *testing.T) {
	tests := []struct {
		name      string
		status    string
		expected  int64
		expectErr bool
	}{
		{"exited zero", "Exited (0) 5 seconds ago", 0, false},
		{"exited nonzero", "Exited (137) 2 minutes ago", 137, false},
		{"exited one", "Exited (1) About a minute ago", 1, false},
		{"no parentheses", "Up 3 hours", -1, true},
		{"non-numeric in parens", "Exited (oops) ago", -1, true},
		{"empty string", "", -1, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseExitCodeFromContainerStatus(tt.status)
			if tt.expectErr {
				require.Error(t, err)
				assert.Equal(t, int64(-1), got)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestOrdinal(t *testing.T) {
	tests := []struct {
		in       uint
		expected string
	}{
		{0, "0th"},
		{1, "1st"},
		{2, "2nd"},
		{3, "3rd"},
		{4, "4th"},
		{10, "10th"},
		{11, "11th"}, // special-cased: not 11st
		{12, "12th"}, // special-cased: not 12nd
		{13, "13th"}, // special-cased: not 13rd
		{21, "21st"},
		{22, "22nd"},
		{23, "23rd"},
		{100, "100th"},
		{101, "101st"},
		{111, "111th"},
		{112, "112th"},
		{113, "113th"},
	}
	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, Ordinal(tt.in))
		})
	}
}

func TestMin(t *testing.T) {
	tests := []struct {
		name     string
		a, b     int64
		expected int64
	}{
		{"a smaller", 1, 2, 1},
		{"b smaller", 5, 3, 3},
		{"equal", 4, 4, 4},
		{"negatives", -2, -1, -2},
		{"zero and positive", 0, 7, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, Min(tt.a, tt.b))
		})
	}
}

func TestParseContainerName(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantStage  Stage
		wantAppKey uint64
		wantName   string
		expectErr  bool
	}{
		{"prod simple", "prod_6_myapp", PROD, 6, "myapp", false},
		{"dev simple", "dev_42_netdata", DEV, 42, "netdata", false},
		{"pub maps to dev stage", "pub_3_foo", DEV, 3, "foo", false},
		{"leading slash stripped", "/prod_6_myapp", PROD, 6, "myapp", false},
		{"name with underscores rejoined", "dev_6_net_data", DEV, 6, "net_data", false},
		{"unknown stage becomes empty", "weird_6_app", "", 6, "app", false},
		{"empty string", "", "", 0, "", true},
		{"too few segments", "prod_6", "", 0, "", true},
		{"non-numeric app key", "prod_abc_app", "", 0, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stage, appKey, name, err := ParseContainerName(tt.input)
			if tt.expectErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantStage, stage)
			assert.Equal(t, tt.wantAppKey, appKey)
			assert.Equal(t, tt.wantName, name)
		})
	}
}

func TestParseComposeContainerName(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantStage  Stage
		wantAppKey uint64
		wantName   string
		expectErr  bool
	}{
		{"prod compose", "prod_6_myapp_compose", PROD, 6, "myapp", false},
		{"dev compose", "dev_42_netdata_compose", DEV, 42, "netdata", false},
		{"pub maps to dev stage", "pub_3_foo_compose", DEV, 3, "foo", false},
		{"leading slash stripped", "/prod_6_myapp_compose", PROD, 6, "myapp", false},
		{"unknown stage becomes empty", "weird_6_app_compose", "", 6, "app", false},
		{"missing compose marker", "prod_6_myapp_other", "", 0, "", true},
		{"empty string", "", "", 0, "", true},
		{"non-numeric app key", "prod_abc_app_compose", "", 0, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stage, appKey, name, err := ParseComposeContainerName(tt.input)
			if tt.expectErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantStage, stage)
			assert.Equal(t, tt.wantAppKey, appKey)
			assert.Equal(t, tt.wantName, name)
		})
	}
}

func TestParseContainerNameRoundTrip(t *testing.T) {
	// BuildContainerName then ParseContainerName should round-trip for
	// lowercase names without underscores.
	built := BuildContainerName(PROD, 7, "myapp")
	stage, appKey, name, err := ParseContainerName(built)
	require.NoError(t, err)
	assert.Equal(t, PROD, stage)
	assert.Equal(t, uint64(7), appKey)
	assert.Equal(t, "myapp", name)
}

func TestParseComposeContainerNameRoundTrip(t *testing.T) {
	built := BuildComposeContainerName(DEV, 9, "thing")
	stage, appKey, name, err := ParseComposeContainerName(built)
	require.NoError(t, err)
	assert.Equal(t, DEV, stage)
	assert.Equal(t, uint64(9), appKey)
	assert.Equal(t, "thing", name)
}

func TestPrettyFormat(t *testing.T) {
	t.Run("marshals struct with indentation", func(t *testing.T) {
		got, err := PrettyFormat(map[string]int{"a": 1})
		require.NoError(t, err)
		assert.Equal(t, "{\n\t\"a\": 1\n}", got)
	})

	t.Run("returns error for unmarshalable value", func(t *testing.T) {
		_, err := PrettyFormat(make(chan int))
		require.Error(t, err)
	})
}
