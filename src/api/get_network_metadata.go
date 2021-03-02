package api

import (
	"context"
	"net"
	"reagent/messenger"
)

func (ex *External) getNetworkDataHandler(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error) {
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
