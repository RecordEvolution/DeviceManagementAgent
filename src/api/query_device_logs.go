package api

import (
	"context"
	"errors"
	"fmt"
	"reagent/common"
	"reagent/errdefs"
	"reagent/logging"
	"reagent/messenger"
	"time"
)

// queryDeviceLogsHandler serves a bounded, severity-filtered slice of the
// agent's own log — what the DEVICE did, across every app.
//
// This is deliberately a different procedure from the app-log query rather than
// a mode of it. The agent log is device-scoped: app installs and image pulls,
// disk pressure, network and tunnel state, agent updates. Most of it relates to
// no app at all, and folding it into an app-addressed call would force a
// container name onto a question that has none.
//
// It reads a bounded tail rather than the whole file, and checks the READ
// privilege first. Both matter: reagent.log is capped at 100 MB with two
// backups (logging.SetupLogger), and nothing between the device and a browser
// streams — a large result is fully buffered at every hop — so returning the
// file whole was never safe.
func (ex *External) queryDeviceLogsHandler(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error) {
	privileged, err := ex.Privilege.Check("READ", response.Details)
	if err != nil {
		// Not a denial — see queryAppLogsHandler.
		return nil, fmt.Errorf("could not verify privileges: %w", err)
	}

	if !privileged {
		return nil, errdefs.InsufficientPrivileges(errors.New("insufficient privileges to query device logs"))
	}

	// Every argument is optional: "show me what this device has been doing" is a
	// complete question, so an empty argument list is valid.
	argsDict := map[string]interface{}{}
	if len(response.Arguments) > 0 && response.Arguments[0] != nil {
		if parsed, ok := response.Arguments[0].(map[string]interface{}); ok {
			argsDict = parsed
		} else {
			return nil, fmt.Errorf("first param should be a dict")
		}
	}

	now := time.Now().UTC()

	query := logging.AgentLogQuery{
		Path: ex.Config.CommandLineArguments.LogFileLocation,
	}

	if query.Tail, err = optionalUint64(argsDict, "tail"); err != nil {
		return nil, err
	}
	if query.Since, err = optionalLogTime(argsDict, "since", now); err != nil {
		return nil, err
	}
	if query.Until, err = optionalLogTime(argsDict, "until", now); err != nil {
		return nil, err
	}

	if raw := argsDict["level"]; raw != nil {
		level, ok := raw.(string)
		if !ok {
			return nil, fmt.Errorf("the level param should be a string")
		}
		query.MinLevel = level
	}

	result, err := logging.QueryAgentLog(query)
	if err != nil {
		return nil, err
	}

	payload := common.Dict{
		"lines":         result.Lines,
		"returned":      len(result.Lines),
		"truncated":     result.Truncated,
		"files_scanned": result.FilesScanned,
		"device_time":   now.Format(time.RFC3339),
	}

	if !result.OldestAvailable.IsZero() {
		payload["oldest_available"] = result.OldestAvailable.UTC().Format(time.RFC3339)
	}

	return &messenger.InvokeResult{
		Arguments: []interface{}{payload},
	}, nil
}
