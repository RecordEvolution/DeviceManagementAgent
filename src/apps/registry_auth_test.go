package apps

import (
	"reagent/common"
	"reagent/config"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestNormalizeRegistryHost(t *testing.T) {
	cases := map[string]string{
		"smartviewservices.azurecr.io":            "smartviewservices.azurecr.io",
		"https://smartviewservices.azurecr.io":    "smartviewservices.azurecr.io",
		"http://smartviewservices.azurecr.io/":    "smartviewservices.azurecr.io",
		"SmartViewServices.azurecr.io":            "smartviewservices.azurecr.io",
		" smartviewservices.azurecr.io ":          "smartviewservices.azurecr.io",
		"https://myregistry.azurecr.io/repo/path": "myregistry.azurecr.io",
		"registry.ironflock.com/":                 "registry.ironflock.com",
		"localhost:15001/":                        "localhost:15001",
		"136.230.111.59:15001/":                   "136.230.111.59:15001",
		"docker.io":                               "docker.io",
		"":                                        "",
		"https://":                                "",
	}

	for input, expected := range cases {
		assert.Equal(t, expected, common.NormalizeRegistryHost(input), "input %q", input)
	}
}

func TestRegistryHostOfImage(t *testing.T) {
	cases := map[string]string{
		"smartviewservices.azurecr.io/viewer:1.2":  "smartviewservices.azurecr.io",
		"registry.ironflock.com/main/prod_x:1.0.0": "registry.ironflock.com",
		"localhost:15001/apps/foo:latest":          "localhost:15001",
		"nginx:alpine":                             "docker.io",
		"library/nginx":                            "docker.io",
		"ghcr.io/org/tool:2":                       "ghcr.io",
	}

	for imageRef, expected := range cases {
		assert.Equal(t, expected, registryHostOfImage(imageRef), "image %q", imageRef)
	}
}

// The regression this feature exists for: credentials stored under any
// spelling of the registry host must match the bare lowercase host parsed
// from an image reference.
func TestCredentialForRegistryHostToleratesKeySpellings(t *testing.T) {
	cred := common.DockerCredential{Username: "svc", Password: "secret"}

	for _, storedKey := range []string{
		"smartviewservices.azurecr.io",
		"smartviewservices.azurecr.io/",
		"https://smartviewservices.azurecr.io",
		"https://smartviewservices.azurecr.io/",
		"SmartViewServices.azurecr.io",
		" smartviewservices.azurecr.io ",
	} {
		credentials := map[string]common.DockerCredential{storedKey: cred}
		authConfig, found := credentialForRegistryHost(credentials, "smartviewservices.azurecr.io")
		require.True(t, found, "stored key %q must match", storedKey)
		assert.Equal(t, "svc", authConfig.Username, "stored key %q", storedKey)
		assert.Equal(t, "secret", authConfig.Password, "stored key %q", storedKey)
	}
}

func TestCredentialForRegistryHostMissReportsNotFound(t *testing.T) {
	credentials := map[string]common.DockerCredential{
		"other.registry.io": {Username: "u", Password: "p"},
	}

	authConfig, found := credentialForRegistryHost(credentials, "smartviewservices.azurecr.io")
	assert.False(t, found)
	assert.Empty(t, authConfig.Username)
	assert.Empty(t, authConfig.Password)

	_, found = credentialForRegistryHost(nil, "smartviewservices.azurecr.io")
	assert.False(t, found)
}

func TestHandleRegistryLoginsWithDefaultNormalizesAndKeepsUserEntries(t *testing.T) {
	sm, mockContainer, _ := setupTestStateMachine(t)

	cfg := &config.Config{
		ReswarmConfig: &config.ReswarmConfig{
			DockerRegistryURL: "registry.ironflock.com/",
			Secret:            "device-secret",
		},
	}
	mockContainer.EXPECT().GetConfig().Return(cfg)

	var loggedIn map[string]common.DockerCredential
	mockContainer.EXPECT().HandleRegistryLogins(mock.Anything).Run(func(credentials map[string]common.DockerCredential) {
		loggedIn = credentials
	}).Return(nil)

	payload := common.TransitionPayload{
		RegisteryToken: "store-jwt",
		DockerCredentials: map[string]common.DockerCredential{
			"https://SmartViewServices.azurecr.io/": {Username: "svc", Password: "secret"},
		},
	}

	err := sm.HandleRegistryLoginsWithDefault(payload)
	require.NoError(t, err)

	require.NotNil(t, loggedIn)
	assert.Equal(t, common.DockerCredential{Username: "svc", Password: "secret"}, loggedIn["smartviewservices.azurecr.io"], "user credential must survive under the canonical key")
	assert.Equal(t, common.DockerCredential{Username: "store-jwt", Password: "device-secret"}, loggedIn["registry.ironflock.com"], "store credential must be injected under the bare host")

	// The payload's own map must not be mutated with the store credential.
	assert.Len(t, payload.DockerCredentials, 1)
}
