package api

import (
	"context"
	"reagent/apps"
	"reagent/common"
	"reagent/logging"
	"reagent/messenger"
	"reagent/store"
	"reagent/testutil/builders"
	"reagent/testutil/fakes"
	"reagent/testutil/mocks"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newCheckHostPortEx wires an External with a real AppManager (DB-backed store,
// strict container/tunnel mocks) — checkHostPortHandler consults the app's
// requested state for compose shape resolution.
func newCheckHostPortEx(t *testing.T) (*External, *store.AppStore) {
	t.Helper()

	db := newLogDB(t)
	msg := fakes.NewMessenger()
	cont := mocks.NewContainer(t)
	tm := mocks.NewTunnelManager(t)
	as := store.NewAppStore(db, msg)
	lm := logging.NewLogManager(cont, msg, db, as)
	observer := apps.NewObserver(cont, &as, nil)
	sm := apps.NewStateMachine(cont, &lm, &observer, nil)
	am := apps.NewAppManager(&sm, &as, &observer, tm)

	return &External{AppManager: am}, &as
}

func checkHostPortArgs(args common.Dict) messenger.Result {
	return messenger.Result{Arguments: []interface{}{map[string]interface{}(args)}}
}

func TestCheckHostPortHandler(t *testing.T) {
	t.Run("rejects missing or invalid numeric params", func(t *testing.T) {
		ex, _ := newCheckHostPortEx(t)

		_, err := ex.checkHostPortHandler(context.Background(), messenger.Result{})
		require.Error(t, err)

		_, err = ex.checkHostPortHandler(context.Background(), checkHostPortArgs(common.Dict{
			"app_key": float64(5), "stage": "PROD", "protocol": "tcp", "port": float64(1883),
		}))
		require.Error(t, err, "host_port is required")

		_, err = ex.checkHostPortHandler(context.Background(), checkHostPortArgs(common.Dict{
			"app_key": "not-a-number", "stage": "PROD", "protocol": "tcp", "port": float64(1883), "host_port": float64(15000),
		}))
		require.Error(t, err)
	})

	t.Run("host_port 0 is a capability probe and always available", func(t *testing.T) {
		ex, _ := newCheckHostPortEx(t)

		// The backend deliberately sends host_port: 0 (remote-only reservation
		// or clear on a rule without a host port) and only cares that the RPC
		// answers — it must not be rejected as an invalid number.
		result, err := ex.checkHostPortHandler(context.Background(), checkHostPortArgs(common.Dict{
			"app_key": float64(5), "stage": "PROD", "protocol": "tcp", "port": float64(1883), "host_port": float64(0),
		}))
		require.NoError(t, err)

		verdict := result.Arguments[0].(common.Dict)
		assert.Equal(t, true, verdict["available"])
		assert.Equal(t, "", verdict["reason"])

		// Strict validation stays for app_key and port even on a probe.
		_, err = ex.checkHostPortHandler(context.Background(), checkHostPortArgs(common.Dict{
			"app_key": float64(0), "stage": "PROD", "protocol": "tcp", "port": float64(1883), "host_port": float64(0),
		}))
		require.Error(t, err)

		// A present but non-numeric host_port is still malformed.
		_, err = ex.checkHostPortHandler(context.Background(), checkHostPortArgs(common.Dict{
			"app_key": float64(5), "stage": "PROD", "protocol": "tcp", "port": float64(1883), "host_port": "junk",
		}))
		require.Error(t, err)
	})

	t.Run("returns the availability verdict shape", func(t *testing.T) {
		ex, _ := newCheckHostPortEx(t)

		// Serializer-typical float64 numerics must parse.
		result, err := ex.checkHostPortHandler(context.Background(), checkHostPortArgs(common.Dict{
			"app_key": float64(5), "stage": "PROD", "protocol": "tcp", "port": float64(1883), "host_port": float64(45999),
		}))
		require.NoError(t, err)
		require.Len(t, result.Arguments, 1)

		verdict, ok := result.Arguments[0].(common.Dict)
		require.True(t, ok)
		_, ok = verdict["available"].(bool)
		assert.True(t, ok, "available must be a bool")
		_, ok = verdict["reason"].(string)
		assert.True(t, ok, "reason must be a string")
	})

	t.Run("denylisted ports are refused deterministically", func(t *testing.T) {
		ex, _ := newCheckHostPortEx(t)

		result, err := ex.checkHostPortHandler(context.Background(), checkHostPortArgs(common.Dict{
			"app_key": float64(5), "stage": "PROD", "protocol": "tcp", "port": float64(1883), "host_port": float64(7411),
		}))
		require.NoError(t, err)

		verdict := result.Arguments[0].(common.Dict)
		assert.Equal(t, false, verdict["available"])
		assert.Equal(t, "denylisted", verdict["reason"])
	})

	t.Run("ambiguous compose rules are refused", func(t *testing.T) {
		ex, appStore := newCheckHostPortEx(t)

		payload := builders.BuildTransitionPayload("ambigapp", common.RUNNING, common.PROD)
		payload.AppKey = 51
		payload.CurrentState = common.PRESENT
		payload.DockerCompose = map[string]interface{}{
			"services": map[string]interface{}{
				"web": map[string]interface{}{"ports": []interface{}{"9090:80"}},
				"api": map[string]interface{}{"ports": []interface{}{"9090:81"}},
			},
		}
		require.NoError(t, appStore.UpdateLocalRequestedState(payload))

		result, err := ex.checkHostPortHandler(context.Background(), checkHostPortArgs(common.Dict{
			"app_key": float64(51), "stage": "PROD", "protocol": "tcp", "port": float64(9090), "host_port": float64(45998),
		}))
		require.NoError(t, err)

		verdict := result.Arguments[0].(common.Dict)
		assert.Equal(t, false, verdict["available"])
		assert.Equal(t, "ambiguous_compose_rule", verdict["reason"])
	})
}
