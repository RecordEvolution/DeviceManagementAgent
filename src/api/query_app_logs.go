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

// queryAppLogsHandler serves a bounded, optionally time-windowed slice of one
// app's container logs.
//
// It exists alongside getAppLogHistoryHandler rather than replacing it: that
// procedure returns a flat string[] which the Studio log viewer iterates
// directly, so its wire contract cannot carry the window metadata a diagnosis
// needs (which store answered, whether lines were dropped, how far back the
// device can still see). Callers that want a window ask for this one; a caller
// talking to an agent too old to have it gets no_such_procedure and can fall
// back to the older topic knowing the range was not applied.
func (ex *External) queryAppLogsHandler(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error) {
	privileged, err := ex.Privilege.Check("READ", response.Details)
	if err != nil {
		// A transport failure reaching the privilege service is NOT a denial.
		// Surfacing it as one would tell an operator they lack permission during
		// exactly the cloud incident where they need the logs most.
		return nil, fmt.Errorf("could not verify privileges: %w", err)
	}

	if !privileged {
		return nil, errdefs.InsufficientPrivileges(errors.New("insufficient privileges to query app logs"))
	}

	argsDict, err := firstArgDict(response.Arguments)
	if err != nil {
		return nil, err
	}

	containerName, ok := argsDict["containerName"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid value for containerName")
	}

	now := time.Now().UTC()

	request := logging.LogQueryRequest{ContainerName: containerName}

	if request.Tail, err = optionalUint64(argsDict, "tail"); err != nil {
		return nil, err
	}
	if request.Offset, err = optionalUint64(argsDict, "offset"); err != nil {
		return nil, err
	}
	if request.Since, err = optionalLogTime(argsDict, "since", now); err != nil {
		return nil, err
	}
	if request.Until, err = optionalLogTime(argsDict, "until", now); err != nil {
		return nil, err
	}

	// Timestamps default ON: a caller diagnosing a failure needs to place the
	// lines in time. A live console passes false, because the lines it streams
	// afterwards have none and the join between backlog and tail would show.
	if raw := argsDict["timestamps"]; raw != nil {
		timestamps, ok := raw.(bool)
		if !ok {
			return nil, fmt.Errorf("the timestamps param should be a boolean")
		}
		request.NoTimestamps = !timestamps
	}

	result, err := ex.LogManager.QueryLogs(ctx, request)
	if err != nil {
		return nil, err
	}

	payload := common.Dict{
		"lines":       result.Lines,
		"source":      result.Source,
		"returned":    len(result.Lines),
		"truncated":   result.Truncated,
		"device_time": now.Format(time.RFC3339),
	}

	// Absent rather than zero: "we do not know the retention floor" and "the
	// retention floor is the epoch" are different answers, and a caller deciding
	// whether an empty window means "quiet" or "rotated away" needs to tell them
	// apart.
	if !result.OldestAvailable.IsZero() {
		payload["oldest_available"] = result.OldestAvailable.UTC().Format(time.RFC3339)
	}

	return &messenger.InvokeResult{
		Arguments: []interface{}{payload},
	}, nil
}

// firstArgDict pulls the single positional argument dict every exposed handler
// takes. It checks the length rather than only the nil-ness of the slice: the
// older handlers index args[0] after `args != nil`, which panics on an empty
// argument list.
func firstArgDict(args []interface{}) (map[string]interface{}, error) {
	if len(args) == 0 || args[0] == nil {
		return nil, fmt.Errorf("arguments are missing")
	}

	argsDict, ok := args[0].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("first param should be a dict")
	}

	return argsDict, nil
}

// optionalUint64 reads a numeric argument, absent meaning zero.
func optionalUint64(argsDict map[string]interface{}, key string) (uint64, error) {
	raw := argsDict[key]
	if raw == nil {
		return 0, nil
	}

	value, ok := common.ToUint64(raw)
	if !ok {
		return 0, fmt.Errorf("the %s param should be a non-negative whole number", key)
	}

	return value, nil
}

// optionalLogTime reads a window bound, absent meaning unbounded. Relative
// durations resolve against the device's clock, which is why every response
// echoes device_time.
func optionalLogTime(argsDict map[string]interface{}, key string, now time.Time) (time.Time, error) {
	raw := argsDict[key]
	if raw == nil {
		return time.Time{}, nil
	}

	value, ok := raw.(string)
	if !ok {
		return time.Time{}, fmt.Errorf("the %s param should be a string", key)
	}

	if value == "" {
		return time.Time{}, nil
	}

	parsed, err := common.ParseLogTime(value, now)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid %s: %w", key, err)
	}

	return parsed, nil
}
