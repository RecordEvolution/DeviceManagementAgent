package wampsession

import (
	"context"
	"time"

	"github.com/gammazero/nexus/v3/client"
	"github.com/gammazero/nexus/v3/wamp"
)

// APPS
func IsRunning(ctx context.Context, inv *wamp.Invocation) client.InvokeResult {
	return client.InvokeResult{Args: wamp.List{}}
}
func WriteData(ctx context.Context, inv *wamp.Invocation) client.InvokeResult {
	return client.InvokeResult{Args: wamp.List{}}
}
func DockerRemoveImage(ctx context.Context, inv *wamp.Invocation) client.InvokeResult {
	return client.InvokeResult{Args: wamp.List{}}
}
func DockerTag(ctx context.Context, inv *wamp.Invocation) client.InvokeResult {
	return client.InvokeResult{Args: wamp.List{}}
}
func DockerRemoveContainer(ctx context.Context, inv *wamp.Invocation) client.InvokeResult {
	return client.InvokeResult{Args: wamp.List{}}
}

// CONFIG
func Readme(ctx context.Context, inv *wamp.Invocation) client.InvokeResult {
	return client.InvokeResult{Args: wamp.List{}}
}
func Updater(ctx context.Context, inv *wamp.Invocation) client.InvokeResult {
	return client.InvokeResult{Args: wamp.List{}}
}

// DEVICE START, STOP, UPDATE
func AgentUpdate(ctx context.Context, inv *wamp.Invocation) client.InvokeResult {
	return client.InvokeResult{Args: wamp.List{}}
}
func SystemReboot(ctx context.Context, inv *wamp.Invocation) client.InvokeResult {
	return client.InvokeResult{Args: wamp.List{}}
}
func AgentRestart(ctx context.Context, inv *wamp.Invocation) client.InvokeResult {
	return client.InvokeResult{Args: wamp.List{}}
}
func DeviceHandshake(ctx context.Context, inv *wamp.Invocation) client.InvokeResult {
	nowis := time.Now().String()
	deviceid := "813e9e53-fe1f-4a27-a1bc-a97e8846a5a2"
	return client.InvokeResult{Args: wamp.List{nowis, deviceid}}
}

// FIREWALL
func ApplyFirewall(ctx context.Context, inv *wamp.Invocation) client.InvokeResult {
	return client.InvokeResult{Args: wamp.List{}}
}
func UfwEnable(ctx context.Context, inv *wamp.Invocation) client.InvokeResult {
	return client.InvokeResult{Args: wamp.List{}}
}
func UfwStatus(ctx context.Context, inv *wamp.Invocation) client.InvokeResult {
	return client.InvokeResult{Args: wamp.List{}}
}
func UfwAllow(ctx context.Context, inv *wamp.Invocation) client.InvokeResult {
}
