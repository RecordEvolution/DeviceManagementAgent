package apps

import (
	"encoding/json"
	"os"
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

func TestIsOwnStoreImageRepository(t *testing.T) {
	const appKey = uint64(1953)
	const appName = "trumpfsmartview"

	// Every service of a release, built and image-only alike.
	assert.True(t, isOwnStoreImageRepository("apps/", appKey, appName, "apps/prod_amd64_1953_trumpfsmartview_3:0.4b"))
	assert.True(t, isOwnStoreImageRepository("apps/", appKey, appName, "apps/prod_armv7_1953_trumpfsmartview_11:0.4b"))

	// Another app's store image, and another key for the same name.
	assert.False(t, isOwnStoreImageRepository("apps/", appKey, appName, "apps/prod_amd64_1954_trumpfqds_1:1.0.0"))
	assert.False(t, isOwnStoreImageRepository("apps/", appKey, appName, "apps/prod_amd64_19530_trumpfsmartview_1:0.4b"))

	// Authored references that merely look close.
	assert.False(t, isOwnStoreImageRepository("apps/", appKey, appName, "apps/1953_trumpfsmartview_1:0.4b"))
	assert.False(t, isOwnStoreImageRepository("apps/", appKey, appName, "other/prod_amd64_1953_trumpfsmartview_1:0.4b"))
	assert.False(t, isOwnStoreImageRepository("apps/", appKey, appName, "nginx:alpine"))
	assert.False(t, isOwnStoreImageRepository("", appKey, appName, "apps/prod_amd64_1953_trumpfsmartview_1:0.4b"))
	assert.False(t, isOwnStoreImageRepository("apps/", appKey, "", "apps/prod_amd64_1953__1:0.4b"))
}

// The invariant: a release names a repository, the device supplies the host.
// One release row is read by the appliance's own agent, by off-host devices
// and in the cloud, and the host baked in at publish time is right for at most
// one of them.
func TestResolveComposeStoreImagesPutsEveryReaderOnItsOwnRegistry(t *testing.T) {
	const appKey = uint64(1953)
	const appName = "trumpfsmartview"

	// Refs as the appliance app-store sync minted them, in domain mode.
	newCompose := func() map[string]interface{} {
		return map[string]interface{}{
			"services": map[string]interface{}{
				"smartview": map[string]interface{}{"image": "registry.tls-sf015.corp.trumpf.com/apps/prod_amd64_1953_trumpfsmartview_1:0.4b"},
				"collector": map[string]interface{}{"image": "registry.tls-sf015.corp.trumpf.com/apps/prod_amd64_1953_trumpfsmartview_2:0.4b"},
				// An image-only service keeps its authored ref beside the store one.
				"mosquitto": map[string]interface{}{
					"image":          "registry.tls-sf015.corp.trumpf.com/apps/prod_amd64_1953_trumpfsmartview_3:0.4b",
					"x-source-image": "eclipse-mosquitto:2",
				},
			},
		}
	}

	// Colocated appliance agent: loopback.
	compose := newCompose()
	colocated := &config.ReswarmConfig{DockerRegistryURL: "localhost:15001/", DockerMainRepository: "apps/"}
	assert.Equal(t, 3, resolveComposeStoreImages(colocated, appKey, appName, compose))
	services := compose["services"].(map[string]interface{})
	assert.Equal(t, "localhost:15001/apps/prod_amd64_1953_trumpfsmartview_1:0.4b", services["smartview"].(map[string]interface{})["image"])
	assert.Equal(t, "localhost:15001/apps/prod_amd64_1953_trumpfsmartview_3:0.4b", services["mosquitto"].(map[string]interface{})["image"])
	assert.Equal(t, "eclipse-mosquitto:2", services["mosquitto"].(map[string]interface{})["x-source-image"], "the authored external ref must not be touched")

	// Off-host device on the same appliance: the domain. Already correct, so
	// nothing changes and no container is recreated for nothing.
	compose = newCompose()
	offHost := &config.ReswarmConfig{DockerRegistryURL: "registry.tls-sf015.corp.trumpf.com/", DockerMainRepository: "apps/"}
	assert.Equal(t, 0, resolveComposeStoreImages(offHost, appKey, appName, compose))

	// A reference stranded by an IP change or a mode switch self-heals.
	compose = map[string]interface{}{"services": map[string]interface{}{
		"smartview": map[string]interface{}{"image": "136.230.111.59:15001/apps/prod_amd64_1953_trumpfsmartview_1:0.4b"},
	}}
	assert.Equal(t, 1, resolveComposeStoreImages(colocated, appKey, appName, compose))
	assert.Equal(t, "localhost:15001/apps/prod_amd64_1953_trumpfsmartview_1:0.4b",
		compose["services"].(map[string]interface{})["smartview"].(map[string]interface{})["image"])

	// A host-less reference, should the mint ever stop naming one.
	compose = map[string]interface{}{"services": map[string]interface{}{
		"smartview": map[string]interface{}{"image": "apps/prod_amd64_1953_trumpfsmartview_1:0.4b"},
	}}
	assert.Equal(t, 1, resolveComposeStoreImages(colocated, appKey, appName, compose))
	assert.Equal(t, "localhost:15001/apps/prod_amd64_1953_trumpfsmartview_1:0.4b",
		compose["services"].(map[string]interface{})["smartview"].(map[string]interface{})["image"])
}

// Authored definitions are data the developer owns: a DEV compose, a public
// base image, a private registry the app declares credentials for. None of it
// may be redirected onto the platform registry.
func TestResolveComposeStoreImagesLeavesAuthoredReferencesAlone(t *testing.T) {
	compose := map[string]interface{}{
		"services": map[string]interface{}{
			"web":    map[string]interface{}{"image": "nginx:alpine"},
			"viewer": map[string]interface{}{"image": "smartviewservices.azurecr.io/viewer:1.2"},
			"other":  map[string]interface{}{"image": "registry.tls-sf015.corp.trumpf.com/apps/prod_amd64_1954_trumpfqds_1:1.0.0"},
			"built":  map[string]interface{}{"build": map[string]interface{}{"context": "."}},
		},
	}
	cfg := &config.ReswarmConfig{DockerRegistryURL: "localhost:15001/", DockerMainRepository: "apps/"}

	assert.Equal(t, 0, resolveComposeStoreImages(cfg, 1953, "trumpfsmartview", compose))

	services := compose["services"].(map[string]interface{})
	assert.Equal(t, "nginx:alpine", services["web"].(map[string]interface{})["image"])
	assert.Equal(t, "smartviewservices.azurecr.io/viewer:1.2", services["viewer"].(map[string]interface{})["image"])
	assert.Equal(t, "registry.tls-sf015.corp.trumpf.com/apps/prod_amd64_1954_trumpfqds_1:1.0.0", services["other"].(map[string]interface{})["image"])
}

func TestResolveComposeStoreImagesToleratesMissingConfig(t *testing.T) {
	compose := map[string]interface{}{"services": map[string]interface{}{
		"smartview": map[string]interface{}{"image": "apps/prod_amd64_1953_trumpfsmartview_1:0.4b"},
	}}

	assert.Equal(t, 0, resolveComposeStoreImages(nil, 1953, "trumpfsmartview", compose))
	// An unconfigured registry must never leave a host-less ref pointing at Docker Hub.
	assert.Equal(t, 0, resolveComposeStoreImages(&config.ReswarmConfig{DockerMainRepository: "apps/"}, 1953, "trumpfsmartview", compose))
	assert.Equal(t, 0, resolveComposeStoreImages(&config.ReswarmConfig{DockerRegistryURL: "localhost:15001/", DockerMainRepository: "apps/"}, 1953, "trumpfsmartview", nil))
	assert.Equal(t, "apps/prod_amd64_1953_trumpfsmartview_1:0.4b",
		compose["services"].(map[string]interface{})["smartview"].(map[string]interface{})["image"])
}

func TestSplitRegistryHost(t *testing.T) {
	host, repository := splitRegistryHost("registry.tls-sf015.corp.trumpf.com/apps/prod_amd64_1953_x_1:0.4b")
	assert.Equal(t, "registry.tls-sf015.corp.trumpf.com", host)
	assert.Equal(t, "apps/prod_amd64_1953_x_1:0.4b", repository)

	host, repository = splitRegistryHost("localhost:15001/apps/foo:latest")
	assert.Equal(t, "localhost", host[:9])
	assert.Equal(t, "apps/foo:latest", repository)

	host, repository = splitRegistryHost("apps/prod_amd64_1953_x_1:0.4b")
	assert.Equal(t, "", host)
	assert.Equal(t, "apps/prod_amd64_1953_x_1:0.4b", repository)

	host, repository = splitRegistryHost("nginx:alpine")
	assert.Equal(t, "", host)
	assert.Equal(t, "nginx:alpine", repository)
}

// Wiring: the resolution must happen inside SetupComposeFiles, the one place
// every transition materialises the compose file from. Asserting on the file
// on disk is what proves it, since that file is what the compose CLI reads for
// pull, up, build and push alike.
func TestSetupComposeFilesResolvesStoreImagesOnDisk(t *testing.T) {
	am, mockContainer, _, st, _, cfg := amHarness(t)
	cfg.ReswarmConfig.DockerRegistryURL = "localhost:15001/"
	cfg.ReswarmConfig.DockerMainRepository = "apps/"

	mockContainer.EXPECT().GetComposePublishedPorts(mock.Anything, mock.Anything).Return(map[string]uint64{}, nil).Maybe()

	const appKey = uint64(1953)
	const appName = "trumpfsmartview"

	payload := common.TransitionPayload{
		AppKey:         appKey,
		AppName:        appName,
		Stage:          common.PROD,
		RequestedState: common.PRESENT,
		Version:        "0.4b",
		ContainerName: common.StageBasedResult{
			Dev:  common.BuildContainerName(common.DEV, appKey, appName),
			Prod: common.BuildContainerName(common.PROD, appKey, appName),
		},
		DockerCompose: map[string]interface{}{
			"services": map[string]interface{}{
				"smartview": map[string]interface{}{"image": "registry.tls-sf015.corp.trumpf.com/apps/prod_amd64_1953_trumpfsmartview_1:0.4b"},
				"mosquitto": map[string]interface{}{"image": "eclipse-mosquitto:2"},
			},
		},
	}

	app, err := st.AddApp(payload)
	require.NoError(t, err)

	composePath, err := am.StateMachine.SetupComposeFiles(payload, app, false)
	require.NoError(t, err)

	written, err := os.ReadFile(composePath)
	require.NoError(t, err)

	var onDisk map[string]interface{}
	require.NoError(t, json.Unmarshal(written, &onDisk))
	services := onDisk["services"].(map[string]interface{})

	assert.Equal(t, "localhost:15001/apps/prod_amd64_1953_trumpfsmartview_1:0.4b",
		services["smartview"].(map[string]interface{})["image"],
		"the store image must be written under THIS device's registry")
	assert.Equal(t, "eclipse-mosquitto:2",
		services["mosquitto"].(map[string]interface{})["image"],
		"an authored public image must be written unchanged")

	// The payload the cloud sent must not be mutated: it is the stored release
	// definition, and the next transition re-reads it.
	assert.Equal(t, "registry.tls-sf015.corp.trumpf.com/apps/prod_amd64_1953_trumpfsmartview_1:0.4b",
		payload.DockerCompose["services"].(map[string]interface{})["smartview"].(map[string]interface{})["image"])
}

// A store image minted with an address this appliance has since left is
// resolved at materialisation, so it must not be reported as an external
// registry the user forgot to add credentials for.
func TestWarnUncredentialedComposeRegistriesIgnoresOwnStoreImages(t *testing.T) {
	sm, mockContainer, _ := setupTestStateMachine(t)

	cfg := &config.Config{
		ReswarmConfig: &config.ReswarmConfig{
			DockerRegistryURL:    "localhost:15001/",
			DockerMainRepository: "apps/",
			ApplianceDomain:      "tls-sf015.corp.trumpf.com",
		},
	}
	mockContainer.EXPECT().GetConfig().Return(cfg)

	payload := common.TransitionPayload{
		AppKey:            1953,
		AppName:           "trumpfsmartview",
		DockerCredentials: map[string]common.DockerCredential{"smartviewservices.azurecr.io": {Username: "u", Password: "p"}},
	}

	compose := map[string]interface{}{
		"services": map[string]interface{}{
			// Minted against an address the appliance has since left.
			"smartview": map[string]interface{}{"image": "136.230.111.59:15001/apps/prod_amd64_1953_trumpfsmartview_1:0.4b"},
		},
	}

	// A nil LogManager panics on Write, so reaching the warning at all fails
	// this test loudly.
	sm.warnUncredentialedComposeRegistries(payload, compose, "some.topic")
}
