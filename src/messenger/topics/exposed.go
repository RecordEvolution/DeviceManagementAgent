package topics

const RequestAppState Topic = "request_app_state"
const WriteToFile Topic = "write_data"
const Handshake Topic = "device_handshake"
const GetImages Topic = "get_images"
const PruneImages Topic = "prune_images"
const RequestTerminalSession Topic = "request_terminal_session"
const StartTerminalSession Topic = "start_terminal_session"
const StopTerminalSession Topic = "stop_terminal_session"
const GetAppLogHistory Topic = "get_app_log_history"
const StreamLogHistory Topic = "stream_log_history"
const ListContainers Topic = "list_containers"

const ListEthernetDevices Topic = "list_ethernet_devices"
const UpdateIPv4Configuration Topic = "update_ipv4_config"

const ListWiFiNetworks Topic = "list_wifi_networks"
const AddWiFiConfiguration Topic = "add_wifi_configuration"
const RemoveWiFiConfiguration Topic = "remove_wifi_configuration"
const ScanWifiNetworks Topic = "scan_wifi_networks"
const SelectWiFiNetwork Topic = "select_wifi_network"
const RestartWifi Topic = "restart_wifi"

const SystemReboot Topic = "system_reboot"
const SystemShutdown Topic = "system_shutdown"

const GetNetworkMetaData Topic = "get_network_metadata"

const GetAgentMetaData Topic = "get_agent_metadata"
const GetAgentLogs Topic = "get_agent_logs"

const UpdateAgent Topic = "update_agent"
const AgentProgress Topic = "agent_update_progress"

const GetOSRelease Topic = "get_os_release"
const GetOSUpdate Topic = "get_os_update"
const GetOSUpdateProgress Topic = "get_os_update_progress"
const InstallOSUpdate Topic = "install_os_update"
const InstallOSUpdateProgress Topic = "install_os_update_progress"
