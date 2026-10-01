package api

import (
	"context"
	"errors"
	"net"
	"reagent/errdefs"
	"reagent/messenger"
)

func (ex *External) getNetworkDataHandler(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error) {
	// Every address on every interface: at least what get_ipv4_addresses
	// reveals, so the same READ.
	privileged, err := ex.Privilege.Check("READ", response.Details)
	if err != nil {
		return nil, err
	}

	if !privileged {
		return nil, errdefs.InsufficientPrivileges(errors.New("insufficient privileges to read network metadata"))
	}

	intInfo := make([]interface{}, 0)
	interfaces, _ := net.Interfaces()
	for _, interf := range interfaces {

		if addrs, err := interf.Addrs(); err == nil {
			for _, addr := range addrs {
				intInfo = append(intInfo, addr)
			}
		}
	}

	return &messenger.InvokeResult{
		Arguments: intInfo,
	}, nil
}
