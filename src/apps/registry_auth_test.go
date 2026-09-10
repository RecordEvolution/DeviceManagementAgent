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

func TestPlatformRegistryHostsAddsApplianceAliasOnce(t *testing.T) {
	// Colocated appliance agent: loopback registry, domain set → two names.
	colocated := &config.ReswarmConfig{
		DockerRegistryURL: "localhost:15001/",
		ApplianceDomain:   "tls-sf015.corp.trumpf.com",
	}
	assert.Equal(t, []string{"localhost:15001", "registry.tls-sf015.corp.trumpf.com"}, platformRegistryHosts(colocated))
	assert.True(t, isPlatformRegistryHost(colocated, "registry.tls-sf015.corp.trumpf.com"))
	assert.True(t, isPlatformRegistryHost(colocated, "Registry.TLS-SF015.corp.trumpf.com"))
	assert.True(t, isPlatformRegistryHost(colocated, "localhost:15001"))
	assert.False(t, isPlatformRegistryHost(colocated, "smartviewservices.azurecr.io"))

	// Off-host device on the same appliance: the alias IS the primary → no duplicate.
	offHost := &config.ReswarmConfig{
		DockerRegistryURL: "registry.tls-sf015.corp.trumpf.com/",
		ApplianceDomain:   "tls-sf015.corp.trumpf.com",
	}
	assert.Equal(t, []string{"registry.tls-sf015.corp.trumpf.com"}, platformRegistryHosts(offHost))

	// Cloud device: no appliance domain → only the configured registry.
	cloud := &config.ReswarmConfig{DockerRegistryURL: "registry.ironflock.com/"}
	assert.Equal(t, []string{"registry.ironflock.com"}, platformRegistryHosts(cloud))
	assert.Equal(t, "", applianceRegistryHost(cloud))
	assert.Nil(t, platformRegistryHosts(nil))
}

func TestComposeReferencesRegistryHost(t *testing.T) {
	compose := map[string]interface{}{
		"services": map[string]interface{}{
			"smartview": map[string]interface{}{"image": "registry.tls-sf015.corp.trumpf.com/apps/prod_amd64_1953_trumpfsmartview_1:0.4b"},
			"nginx":     map[string]interface{}{"image": "nginx:alpine"},
			"built":     map[string]interface{}{"build": map[string]interface{}{"context": "."}},
		},
	}

	assert.True(t, composeReferencesRegistryHost(compose, "registry.tls-sf015.corp.trumpf.com"))
	assert.True(t, composeReferencesRegistryHost(compose, "docker.io"))
	assert.False(t, composeReferencesRegistryHost(compose, "localhost:15001"))
	assert.False(t, composeReferencesRegistryHost(nil, "registry.tls-sf015.corp.trumpf.com"))
	assert.False(t, composeReferencesRegistryHost(compose, ""))
	assert.ElementsMatch(t, []string{"registry.tls-sf015.corp.trumpf.com/apps/prod_amd64_1953_trumpfsmartview_1:0.4b", "nginx:alpine"}, composeImageRefs(compose))
}

// The regression this exists for: the appliance's own agent is configured with
// localhost:15001 while a store-synced app's compose carries
// registry.<appliance_domain> — one registry, two names. Without a login under
// the domain name `docker compose pull` ran anonymously and every image failed
// with "Authentication required" (tls-sf015 ironflock-instance, 2026-09-10).
func TestHandleRegistryLoginsWithDefaultLogsInToApplianceAliasReferencedByCompose(t *testing.T) {
	sm, mockContainer, _ := setupTestStateMachine(t)

	cfg := &config.Config{
		ReswarmConfig: &config.ReswarmConfig{
			DockerRegistryURL: "localhost:15001/",
			ApplianceDomain:   "tls-sf015.corp.trumpf.com",
			Secret:            "ironflock-appliance-default-secret",
		},
	}
	mockContainer.EXPECT().GetConfig().Return(cfg)

	var loggedIn map[string]common.DockerCredential
	mockContainer.EXPECT().HandleRegistryLogins(mock.Anything).Run(func(credentials map[string]common.DockerCredential) {
		loggedIn = credentials
	}).Return(nil)

	payload := common.TransitionPayload{
		RegisteryToken: "store-jwt",
		DockerCompose: map[string]interface{}{
			"services": map[string]interface{}{
				"smartview": map[string]interface{}{"image": "registry.tls-sf015.corp.trumpf.com/apps/prod_amd64_1953_trumpfsmartview_1:0.4b"},
			},
		},
	}

	require.NoError(t, sm.HandleRegistryLoginsWithDefault(payload))

	storeCredential := common.DockerCredential{Username: "store-jwt", Password: "ironflock-appliance-default-secret"}
	require.NotNil(t, loggedIn)
	assert.Equal(t, storeCredential, loggedIn["localhost:15001"], "configured registry must still be logged in")
	assert.Equal(t, storeCredential, loggedIn["registry.tls-sf015.corp.trumpf.com"], "the domain alias the compose references must be logged in with the same store credential")
	assert.Len(t, loggedIn, 2)
}

// The alias login is gated on the compose actually referencing it: an
// appliance whose cert the host's docker does not trust must not fail every
// transition on a login none of its images need.
func TestHandleRegistryLoginsWithDefaultSkipsUnreferencedApplianceAlias(t *testing.T) {
	sm, mockContainer, _ := setupTestStateMachine(t)

	cfg := &config.Config{
		ReswarmConfig: &config.ReswarmConfig{
			DockerRegistryURL: "localhost:15001/",
			ApplianceDomain:   "tls-sf015.corp.trumpf.com",
			Secret:            "ironflock-appliance-default-secret",
		},
	}
	mockContainer.EXPECT().GetConfig().Return(cfg)

	var loggedIn map[string]common.DockerCredential
	mockContainer.EXPECT().HandleRegistryLogins(mock.Anything).Run(func(credentials map[string]common.DockerCredential) {
		loggedIn = credentials
	}).Return(nil)

	payload := common.TransitionPayload{
		RegisteryToken: "store-jwt",
		DockerCompose: map[string]interface{}{
			"services": map[string]interface{}{
				"local": map[string]interface{}{"image": "localhost:15001/apps/prod_amd64_7_local_1:1.0.0"},
			},
		},
	}

	require.NoError(t, sm.HandleRegistryLoginsWithDefault(payload))

	require.NotNil(t, loggedIn)
	assert.Contains(t, loggedIn, "localhost:15001")
	assert.NotContains(t, loggedIn, "registry.tls-sf015.corp.trumpf.com")
	assert.Len(t, loggedIn, 1)
}

// Daemon-API pulls (legacy single-image releases) take the same alias: an
// image on registry.<appliance_domain> gets the store credential, not an
// anonymous pull with a "no stored credentials" warning.
func TestAuthConfigForImageCoversApplianceAlias(t *testing.T) {
	sm, mockContainer, _ := setupTestStateMachine(t)

	cfg := &config.Config{
		ReswarmConfig: &config.ReswarmConfig{
			DockerRegistryURL: "localhost:15001/",
			ApplianceDomain:   "tls-sf015.corp.trumpf.com",
			Secret:            "ironflock-appliance-default-secret",
		},
	}
	mockContainer.EXPECT().GetConfig().Return(cfg)

	payload := common.TransitionPayload{RegisteryToken: "store-jwt"}

	// logTopic "" keeps the miss path from touching the (nil) LogManager; a
	// platform host never reaches it anyway.
	authConfig := sm.authConfigForImage(payload, "registry.tls-sf015.corp.trumpf.com/apps/prod_amd64_1953_trumpfsmartview_1:0.4b", "")
	assert.Equal(t, "store-jwt", authConfig.Username)
	assert.Equal(t, "ironflock-appliance-default-secret", authConfig.Password)

	authConfig = sm.authConfigForImage(payload, "localhost:15001/apps/prod_amd64_7_local_1:1.0.0", "")
	assert.Equal(t, "store-jwt", authConfig.Username)
}
