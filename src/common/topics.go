package common

// Topics the reagent exposes
const TopicRequestAppState = "request_app_state"
const TopicWriteToFile = "write_data"
const TopicHandshake = "device_handshake"
const TopicContainerImages = "get_images"

// Topics used for backend communication
const TopicBootConfig = "reswarm.containers.get_boot_config"
const TopicSetActualAppOnDeviceState = "reswarm.containers.update_app_on_device"
const TopicGetRequestedAppStates = "reswarm.containers.get_requested_app_states"
const TopicGetRegistryToken = "reswarm.containers.get_registry_token"
const TopicUpdateDeviceStatus = "reswarm.devices.update_device"

// Meta events
const TopicMetaEventSubOnCreate = "wamp.subscription.on_create"
const TopicMetaEventSubOnDelete = "wamp.subscription.on_delete"
const TopicMetaProcLookupSubscription = "wamp.subscription.lookup"
