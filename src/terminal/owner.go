package terminal

import (
	"errors"
	"fmt"
	"math"
	"strconv"

	"reagent/common"
	"reagent/errdefs"
	"reagent/messenger"
)

// A terminal belongs to the caller that opened it: the account id the router
// disclosed, or the one a 'system' caller named for it, or 'system' itself.
// Its session topics are guessable enough to be subscribed or published by
// wildcard, so the agent keeps the output to the owner, and takes keystrokes
// and session calls (start, stop, resize) only from the owner.

// systemAuthID is the platform backend's authid on every router the agent
// connects to. On an appliance it is also the cloudbridge's: the bridge
// re-publishes a device's output to the cloud and a cloud user's keystrokes to
// the device.
const systemAuthID = "system"

// bridgeEligibleKey hands a publish's audience to the appliance cloudbridge.
// The router consumes eligible_authid, so the bridge reads the publisher's own
// list from this kwarg (RESWARM CrossbarConnection.publish sets it the same
// way), maps each local account to its cloud account, applies the result to
// the publish it forwards, and strips the key. When no account maps, it
// forwards nothing.
const bridgeEligibleKey = "__bridge_eligible_authid__"

// outputAudience returns the publish options and kwargs for a terminal's
// output. Only the owner and 'system' receive it. 'system' has to stay
// eligible: the appliance cloudbridge subscribes to re.mgmt.* as 'system', and
// cutting it off would cut every cloud user off their terminal. The kwarg makes
// the bridge restrict the forwarded output to the owner's cloud account.
//
// A terminal 'system' opened for itself is kept to 'system' and gets no kwarg:
// the bridge would find no cloud account for 'system' and drop the output. It
// forwards it unrestricted, as it does for older bridges that open every cloud
// user's terminal as 'system'.
func outputAudience(owner string) (options common.Dict, kwargs common.Dict) {
	if owner == systemAuthID {
		return common.Dict{"acknowledge": true, "eligible_authid": []string{systemAuthID}}, nil
	}

	return common.Dict{"acknowledge": true, "eligible_authid": []string{owner, systemAuthID}},
		common.Dict{bridgeEligibleKey: []string{owner}}
}

// fromOwner reports whether a keystroke publish may reach the owner's shell:
// it must come from the owner, or from 'system' relaying for it (the appliance
// cloudbridge re-publishes a cloud user's keystrokes as 'system'). Every role
// that may publish keystrokes has its publisher disclosed by the router, so a
// publish that does not say who sent it is let through, as before.
func fromOwner(owner string, details common.Dict) bool {
	publisher, disclosed := details["publisher_authid"]
	return !disclosed || actsForOwner(owner, fmt.Sprint(publisher))
}

// calledByOwner reports whether a call on a terminal session (a resize) comes
// from its owner, or from 'system' relaying for it: the appliance cloudbridge
// forwards a cloud user's resize as 'system'. Every role discloses its caller
// on re.mgmt., so a call that does not say who made it is refused.
//
// A 'system' call that names an account acts for that account, as every call
// through api.wrapDetails does. These procedures are registered here, past
// wrapDetails, so they apply its rule themselves: otherwise a 'system' call
// naming another account would pass as the owner's.
func calledByOwner(owner string, invocation messenger.Result) bool {
	caller, disclosed := invocation.Details["caller_authid"]
	if !disclosed {
		return false
	}

	id := fmt.Sprint(caller)
	if id == systemAuthID {
		if account, named := namedAccount(invocation); named {
			id = account
		}
	}

	return actsForOwner(owner, id)
}

// unnamedAccount is api.unnamedAccount: the caller of a 'system' call whose
// requestor_account_key names no account. No terminal belongs to it.
const unnamedAccount = "unnamed-account"

// namedAccount returns the account a call names in requestor_account_key, read
// and normalized as api.wrapDetails does: the kwargs decide when they carry the
// key at all, the first argument's dict otherwise, and a key that is not an
// account, null included, is unnamedAccount. named is false when neither
// carries the key. api's TestTerminalResizeNamesTheAccountAsWrapDetailsDoes
// keeps the two in step.
func namedAccount(invocation messenger.Result) (account string, named bool) {
	key, present := invocation.ArgumentsKw["requestor_account_key"]
	if !present && len(invocation.Arguments) > 0 {
		switch first := invocation.Arguments[0].(type) {
		case map[string]interface{}:
			key, present = first["requestor_account_key"]
		case common.Dict:
			key, present = first["requestor_account_key"]
		}
	}

	if !present {
		return "", false
	}

	if id, ok := accountKey(key); ok {
		return strconv.FormatUint(id, 10), true
	}

	return unnamedAccount, true
}

// accountKey is api.accountKey: an account id off the wire, whichever number
// type the serializer gave it, or a string.
func accountKey(value interface{}) (uint64, bool) {
	switch number := value.(type) {
	case float64:
		if number < 1 || number > 1<<53 || number != math.Trunc(number) {
			return 0, false
		}
		return uint64(number), true
	case float32:
		return accountKey(float64(number))
	}

	key, err := strconv.ParseUint(fmt.Sprint(value), 10, 64)
	return key, err == nil && key > 0
}

// actsForOwner reports whether id, the router-disclosed authid of a caller or
// publisher, may use a terminal that belongs to owner.
func actsForOwner(owner string, id string) bool {
	return id == owner || id == systemAuthID
}

// errNotOwner refuses a call on someone else's terminal session.
var errNotOwner = errdefs.InsufficientPrivileges(errors.New("insufficient privileges to use this terminal session"))
