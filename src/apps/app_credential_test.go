package apps

import (
	"reagent/common"
	"reagent/config"
	"testing"

	"github.com/stretchr/testify/assert"
)

// The derivation MUST stay byte-identical to REaccounting's
// device.f_authenticate_userapp, which recomputes this secret to verify the
// WAMP-CRA signature. A drift here does not fail loudly — it makes every app
// on every device fail authentication with "invalid signature". This vector
// was cross-checked against:
//
//	SELECT encode(public.hmac(
//	  'ironflock/app-cred/v1|06a0bf96-a539-4d6a-8471-ac7adc67616e|33|PROD|1',
//	  'dGVzdC1rZXktZm9yLXZlY3Rvcg==', 'sha256'), 'base64');
func TestAppCredentialSecretMatchesSQLVector(t *testing.T) {
	assert.Equal(t,
		"Pr7pphQvvuN4WW0EKmbTnngTGFpxqFXuH4mYCVrV6+A=",
		appCredentialSecret(
			"dGVzdC1rZXktZm9yLXZlY3Rvcg==",
			"06a0bf96-a539-4d6a-8471-ac7adc67616e",
			33, common.PROD, 1,
		))
}

// The authid is parsed server-side by regex
// '^app-(\d+)-(dev|prod)-e(\d+)@(.+)$' — stage lowercase, epoch after the 'e',
// serial after the '@'.
func TestAppCredentialAuthIDShape(t *testing.T) {
	assert.Equal(t, "app-33-prod-e1@06a0bf96-a539-4d6a-8471-ac7adc67616e",
		appCredentialAuthID("06a0bf96-a539-4d6a-8471-ac7adc67616e", 33, common.PROD, 1))
	assert.Equal(t, "app-7-dev-e12@serial-x",
		appCredentialAuthID("serial-x", 7, common.DEV, 12))
}

// Never emit a half-formed credential: a missing per-device key (backend not
// reachable yet) must yield empty strings so callers leave existing files
// alone and the SDK falls back to the device credential.
func TestAppCredentialRequiresKeyAndSerial(t *testing.T) {
	cfgWith := testConfigWithSerial("serial-abc")
	payload := common.TransitionPayload{AppKey: 7, Stage: common.PROD, AppCredEpoch: 2}

	id, secret := appCredential(cfgWith, "", payload)
	assert.Empty(t, id)
	assert.Empty(t, secret)

	id, secret = appCredential(testConfigWithSerial(""), "key", payload)
	assert.Empty(t, id)
	assert.Empty(t, secret)

	id, secret = appCredential(cfgWith, "key", common.TransitionPayload{Stage: common.PROD})
	assert.Empty(t, id, "no app_key, no credential")
	assert.Empty(t, secret)

	id, secret = appCredential(cfgWith, "key", payload)
	assert.Equal(t, "app-7-prod-e2@serial-abc", id)
	assert.NotEmpty(t, secret)
}

func testConfigWithSerial(serial string) *config.Config {
	return &config.Config{ReswarmConfig: &config.ReswarmConfig{SerialNumber: serial}}
}
