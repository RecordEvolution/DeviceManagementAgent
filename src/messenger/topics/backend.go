package topics

type Topic string

const AgentProgress Topic = "agent_update_progress"
const BootConfig Topic = "reswarm.containers.get_boot_config"
const SetActualAppOnDeviceState Topic = "reswarm.containers.update_app_on_device"
const GetRequestedAppStates Topic = "reswarm.containers.get_requested_app_states"
const GetRegistryToken Topic = "reswarm.containers.get_registry_token"
const UpdateDeviceStatus Topic = "reswarm.devices.update_device_status"
const UpdateDevice Topic = "reswarm.devices.update_device"
const UpdateDeviceArchitecture Topic = "reswarm.devices.update_device_architecture"
const CheckPrivilege Topic = "reswarm.devices.check_privilege"
const SetDeviceTestament Topic = "reswarm.api.testament_device"
