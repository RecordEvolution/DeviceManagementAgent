package apps

import (
	"fmt"
	"reagent/common"
	"reagent/container"
	"strings"
)

// registryHostOfImage returns the registry host of an image reference,
// following docker's own convention: the first path component is a registry
// host only when it contains '.' or ':' or is exactly "localhost"; every
// other reference lives on Docker Hub.
func registryHostOfImage(imageRef string) string {
	firstComponent, _, found := strings.Cut(imageRef, "/")
	if found && (strings.ContainsAny(firstComponent, ".:") || firstComponent == "localhost") {
		return firstComponent
	}
	return "docker.io"
}

// credentialForRegistryHost resolves the credential entry for a registry
// host. Both sides go through common.NormalizeRegistryHost, so a stored key
// like `https://MyRegistry.azurecr.io/` still matches the bare lowercase host
// parsed out of an image reference. The second return value reports whether
// any entry matched — a miss means the pull will run anonymously, and callers
// should say so in the app log instead of failing silently.
func credentialForRegistryHost(credentials map[string]common.DockerCredential, host string) (container.AuthConfig, bool) {
	normalizedHost := common.NormalizeRegistryHost(host)
	if normalizedHost == "" {
		return container.AuthConfig{}, false
	}

	cred, found := credentials[normalizedHost]
	if !found {
		for key, candidate := range credentials {
			if common.NormalizeRegistryHost(key) == normalizedHost {
				cred, found = candidate, true
				break
			}
		}
	}

	return container.AuthConfig{Username: cred.Username, Password: cred.Password}, found
}

// authConfigForImage picks pull credentials by the image's registry host: the
// platform's own registry gets the device's store token, any other host the
// app's docker_credentials entry. A miss on an external host degrades to an
// anonymous pull — logged to the app's log topic so a missing or mistyped
// credential host is visible instead of surfacing later as a bare
// "unauthorized" from the daemon.
func (sm *StateMachine) authConfigForImage(payload common.TransitionPayload, imageRef string, logTopic string) container.AuthConfig {
	config := sm.Container.GetConfig()
	host := registryHostOfImage(imageRef)

	if common.NormalizeRegistryHost(host) == common.NormalizeRegistryHost(config.ReswarmConfig.DockerRegistryURL) {
		return container.AuthConfig{
			Username: payload.RegisteryToken,
			Password: config.ReswarmConfig.Secret,
		}
	}

	authConfig, found := credentialForRegistryHost(payload.DockerCredentials, host)
	if !found && logTopic != "" {
		sm.LogManager.Write(logTopic, registryCredentialMissMessage(host))
	}

	return authConfig
}

// warnUncredentialedComposeRegistries logs, best-effort, every external
// registry referenced by a compose definition's `image:` fields for which no
// credential entry matches. It only speaks up when the app has credentials
// configured at all: anonymous pulls of public images are the norm, and
// warning on every public image would drown the log. Called on the compose
// pull/start paths (never on build paths, whose service image names are local
// build targets that would misread as Docker Hub references).
func (sm *StateMachine) warnUncredentialedComposeRegistries(payload common.TransitionPayload, compose map[string]interface{}, logTopic string) {
	if len(payload.DockerCredentials) == 0 || compose == nil {
		return
	}

	services, ok := compose["services"].(map[string]interface{})
	if !ok {
		return
	}

	config := sm.Container.GetConfig()
	storeHost := common.NormalizeRegistryHost(config.ReswarmConfig.DockerRegistryURL)

	warned := make(map[string]bool)
	for _, serviceInterface := range services {
		service, ok := serviceInterface.(map[string]interface{})
		if !ok {
			continue
		}

		imageRef, _ := service["image"].(string)
		if imageRef == "" {
			continue
		}

		host := registryHostOfImage(imageRef)
		normalizedHost := common.NormalizeRegistryHost(host)
		if normalizedHost == "" || normalizedHost == storeHost || warned[normalizedHost] {
			continue
		}

		if _, found := credentialForRegistryHost(payload.DockerCredentials, host); !found {
			warned[normalizedHost] = true
			sm.LogManager.Write(logTopic, registryCredentialMissMessage(host))
		}
	}
}

// registryCredentialMissMessage names an anonymous pull against an external
// registry and where to fix it, for the app's log stream.
func registryCredentialMissMessage(host string) string {
	return fmt.Sprintf("No stored credentials match registry %s — pulling anonymously. If this registry is private, add credentials for host %q under App Settings → Docker Registry Credentials.", host, common.NormalizeRegistryHost(host))
}

// isRegistryAuthError reports whether a docker daemon error is a registry
// auth rejection (anonymous pull of a private image, or wrong credentials).
func isRegistryAuthError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unauthorized") ||
		strings.Contains(message, "authentication required") ||
		strings.Contains(message, "access denied") ||
		strings.Contains(message, "denied: requested access")
}
