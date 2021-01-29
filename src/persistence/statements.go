package persistence

const QuerySelectCurrentAppStateByKeyAndStage = `SELECT state FROM AppStates WHERE app_key = ? AND stage = ?`

const QuerySelectAllDeviceState = `SELECT interface_type, device_status FROM DeviceStates`
const QuerySelectAllAppStates = `SELECT * FROM AppStates`
const QuerySelectAllRequestedStates = `SELECT app_name, app_key, stage, container_name, current_state,
manually_requested_state, image_name, repository_image_name, requestor_account_key, device_to_app_key
FROM RequestedAppStates`

const QueryUpdateAppStateByAppKeyAndStage = `UPDATE AppStates SET state = ? WHERE app_key = ? AND stage = ?`
const QueryUpdateDeviceState = `UPDATE DeviceStates SET device_status = ?, interface_type = ?`

const QueryInsertAppStateEntry = `INSERT INTO AppStates(app_name, app_key, stage, state, timestamp) VALUES (?, ?, ?, ?, ?)`
const QueryInsertAppStateHistoryEntry = `INSERT INTO AppStateHistory(app_name, app_key, stage, state, timestamp) VALUES (?, ?, ?, ?, ?)`
const QueryInsertDeviceStateHistoryEntry = `INSERT INTO DeviceStateHistory(interface_type, device_status, timestamp) VALUES (?, ?, ?)`

const QueryUpsertRequestedStateEntry = `INSERT INTO RequestedAppStates(app_name, app_key, stage, container_name, current_state, manually_requested_state, image_name, repository_image_name, requestor_account_key, device_to_app_key, timestamp)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) on conflict(app_name, app_key, stage) do update set
manually_requested_state=excluded.manually_requested_state,
current_state=excluded.current_state`
