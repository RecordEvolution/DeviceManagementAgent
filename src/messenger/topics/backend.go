package topics

type Topic string

const AgentProgress Topic = "agent_update_progress"
const BootConfig Topic = "reswarm.containers.get_boot_config"
const SetActualAppOnDeviceState Topic = "reswarm.containers.update_app_on_device"
const GetRequestedAppStates Topic = "reswarm.containers.get_requested_app_states"
const GetRegistryToken Topic = "reswarm.containers.get_registry_token"
const UpdateDeviceStatus Topic = "reswarm.devices.update_device_status"
const GetDeviceMetadata Topic = "reswarm.devices.read_device_metadata"

// const UpdateDevice Topic = "reswarm.devices.update_device"
const UpdateDeviceArchitecture Topic = "reswarm.devices.update_device_architecture"
const CheckPrivilege Topic = "reswarm.devices.check_privilege"
const SetDeviceTestament Topic = "reswarm.api.testament_device"

// const UpdateAppTunnel Topic = "reswarm.devices.update_app_tunnel"
const TunnelStateUpdate = "tunnel_state_update"
const ExposePort Topic = "re.tunnel.expose_port"
const ClosePort Topic = "re.tunnel.close_port"
