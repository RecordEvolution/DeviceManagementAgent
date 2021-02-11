package topics

type Topic string

const TopicBootConfig Topic = "reswarm.containers.get_boot_config"
const TopicSetActualAppOnDeviceState Topic = "reswarm.containers.update_app_on_device"
const TopicGetRequestedAppStates Topic = "reswarm.containers.get_requested_app_states"
const TopicGetRegistryToken Topic = "reswarm.containers.get_registry_token"
const TopicUpdateDeviceStatus Topic = "reswarm.devices.update_device"
