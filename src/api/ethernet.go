package api

import (
	"context"
	"errors"
	"reagent/common"
	"reagent/errdefs"
	"reagent/messenger"
)

func (ex *External) listEthernetDevices(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error) {
	privileged, err := ex.Privilege.Check("READ", response.Details)
	if err != nil {
		return nil, err
	}

	if !privileged {
		return nil, errdefs.InsufficientPrivileges(errors.New("insufficient privileges to list ethernet devices"))
	}

	ethernetDevices, err := ex.Network.ListEthernetDevices()
	if err != nil {
		return nil, err
	}

	ethernetDeviceList := make([]interface{}, len(ethernetDevices))
	for i, ethernetDevice := range ethernetDevices {
		ethernetDeviceList[i] = common.Dict{
			"interfaceName": ethernetDevice.InterfaceName,
			"mac":           ethernetDevice.MAC,
			"ipv4":          ethernetDevice.IPv4AddressData,
			"ipv6":          ethernetDevice.IPv6AddressData,
			"method":        ethernetDevice.Method,
		}
	}

	return &messenger.InvokeResult{Arguments: ethernetDeviceList}, nil
}

func (ex *External) updateIPConfigHandler(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error) {
	privileged, err := ex.Privilege.Check("NETWORK", response.Details)
	if err != nil {
		return nil, err
	}

	if !privileged {
		return nil, errdefs.InsufficientPrivileges(errors.New("insufficient privileges to update ip config"))
	}

	if response.Arguments == nil {
		return nil, errors.New("failed to parse args, payload is missing")
	}

	payloadArg := response.Arguments[0]
	payload, ok := payloadArg.(map[string]interface{})

	if !ok {
		return nil, errors.New("failed to parse payload")
	}

	methodKw := payload["method"]
	method, ok := methodKw.(string)
	if !ok {
		return nil, errors.New("failed to parse method parameter")
	}

	macKw := payload["mac"]
	mac, ok := macKw.(string)
	if !ok {
		return nil, errors.New("failed to parse mac parameter")
	}

	interfaceKw := payload["interfaceName"]
	interfaceName, ok := interfaceKw.(string)
	if !ok {
		return nil, errors.New("failed to parse interface parameter")
	}

	if methodKw != nil && method == "auto" {
		err := ex.Network.EnableDHCP(mac, interfaceName)
		if err != nil {
			return nil, err
		}

		return &messenger.InvokeResult{}, nil
	}

	ipv4Kw := payload["ipv4"]
	ipv4, ok := ipv4Kw.(string)
	if !ok {
		return nil, errors.New("failed to parse ipv4 parameter")
	}

	prefixKw := payload["prefix"]
	prefix, ok := prefixKw.(uint64)
	if !ok {
		return nil, errors.New("failed to parse prefix parameter")
	}

	return &messenger.InvokeResult{}, ex.Network.SetIPv4Address(mac, interfaceName, ipv4, uint32(prefix))
}
