package api

// func (ex *External) getAppTunnel(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error) {
// 	privileged, err := ex.Privilege.Check("NETWORK", response.Details)
// 	if err != nil {
// 		return nil, err
// 	}

// 	if !privileged {
// 		return nil, errdefs.InsufficientPrivileges(errors.New("insufficient privileges to get app tunnels"))
// 	}

// 	tunnels, err := ex.TunnelManager.GetTunnelManager().GetAll()
// 	if err != nil {
// 		return nil, err
// 	}

// 	if tunnels == nil {
// 		tunnels = []*tunnel.Tunnel{}
// 	}

// 	return &messenger.InvokeResult{Arguments: []interface{}{tunnels}}, nil
// }

// func (ex *External) createAppTunnel(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error) {
// 	privileged, err := ex.Privilege.Check("NETWORK", response.Details)
// 	if err != nil {
// 		return nil, err
// 	}

// 	if !privileged {
// 		return nil, errdefs.InsufficientPrivileges(errors.New("insufficient privileges to create an app tunnel"))
// 	}

// 	args := response.Arguments
// 	options := common.Dict{}

// 	if args != nil || args[0] != nil {
// 		argsDict, ok := args[0].(map[string]interface{})
// 		if !ok {
// 			return nil, fmt.Errorf("first param should be a dict")
// 		}

// 		options = argsDict
// 	}

// 	port, ok := options["port"].(uint64)
// 	if !ok {
// 		return nil, errors.New("failed to parse port")
// 	}

// 	app_key, ok := options["app_key"].(uint64)
// 	if !ok {
// 		return nil, errors.New("failed to parse app_key")
// 	}

// 	app_name, ok := options["app_name"].(string)
// 	if !ok {
// 		return nil, errors.New("failed to parse app_name")
// 	}

// 	// device_name, ok := options["device_name"].(string)
// 	// if !ok {
// 	// 	return nil, errors.New("failed to parse device_name")
// 	// }

// 	// swarm_name, ok := options["swarm_name"].(string)
// 	// if !ok {
// 	// 	return nil, errors.New("failed to parse swarm_name")
// 	// }

// 	if options["protocol"] == nil || options["protocol"] == "" {
// 		options["protocol"] = tunnel.HTTP_HTTPS
// 	}

// 	protocol := fmt.Sprint(options["protocol"])
// 	switch protocol {
// 	case tunnel.TCP, tunnel.HTTPS, tunnel.HTTP, tunnel.HTTP_HTTPS:
// 		break
// 	default:
// 		return nil, errors.New("invalid protocol " + protocol)
// 	}

// 	app, err := ex.AppManager.AppStore.GetApp(app_key, common.PROD)
// 	if err != nil {
// 		return nil, err
// 	}

// 	deviceKey := ex.Config.ReswarmConfig.DeviceKey
// 	var tunnel *tunnel.AppTunnel
// 	if app.CurrentState == common.RUNNING {
// 		tunnel, err = ex.TunnelManager.CreateAppTunnel(app_key, app_name, uint64(deviceKey), port, protocol)
// 		if err != nil {
// 			return nil, err
// 		}
// 	} else {
// 		tunnel = ex.TunnelManager.RegisterAppTunnel(app_key, app_name, uint64(deviceKey), port, protocol)
// 		if err != nil {
// 			return nil, err
// 		}
// 	}

// 	return &messenger.InvokeResult{Arguments: []interface{}{tunnel}}, nil
// }

// func (ex *External) killAppTunnel(ctx context.Context, response messenger.Result) (*messenger.InvokeResult, error) {
// 	privileged, err := ex.Privilege.Check("NETWORK", response.Details)
// 	if err != nil {
// 		return nil, err
// 	}

// 	if !privileged {
// 		return nil, errdefs.InsufficientPrivileges(errors.New("insufficient privileges to kill an app tunnel"))
// 	}

// 	args := response.Arguments
// 	options := common.Dict{}

// 	if args != nil || args[0] != nil {
// 		argsDict, ok := args[0].(map[string]interface{})
// 		if !ok {
// 			return nil, fmt.Errorf("first param should be a dict")
// 		}

// 		options = argsDict
// 	}

// 	appKey, ok := options["app_key"].(uint64)
// 	if !ok {
// 		return nil, errors.New("failed to parse app_key")
// 	}

// 	port, ok := options["port"].(uint64)
// 	if !ok {
// 		return nil, errors.New("failed to parse port")
// 	}

// 	err = ex.TunnelManager.KillAppTunnel(appKey, port)
// 	if err != nil {
// 		return nil, err
// 	}

// 	return &messenger.InvokeResult{}, nil
// }
