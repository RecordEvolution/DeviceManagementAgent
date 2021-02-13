package persistence

const QuerySelectCurrentAppStateByKeyAndStage = `SELECT state, version, release_key FROM AppStates WHERE app_key = ? AND stage = ?`

const QuerySelectAllDeviceState = `SELECT interface_type, device_status FROM DeviceStates`
const QuerySelectAllAppStates = `SELECT app_name, app_key, version, release_key, stage, state, timestamp FROM AppStates`

const QuerySelectAllRequestedStates = `SELECT app_name, app_key, stage, version, present_version, newest_version, current_state,
manually_requested_state, requestor_account_key, release_key, new_release_key, environment_variables FROM RequestedAppStates`
const QuerySelectRequestedStateByAppKeyAndStage = `SELECT app_name, app_key, stage, version, present_version, newest_version, current_state,
manually_requested_state, requestor_account_key, release_key, new_release_key, environment_variables FROM RequestedAppStates WHERE app_key = ? AND stage = ?`
const QuerySelectAppStateByAppKeyAndStage = `SELECT app_name, app_key, version, release_key, stage, state, timestamp FROM AppStates WHERE app_key = ? AND stage = ?`

const QueryUpdateRequestedAppStateCurrentStateByAppKeyAndStage = `UPDATE RequestedAppStates SET current_state = ?, present_version = ?, release_key = ?, manually_requested_state = ? WHERE app_key = ? AND stage = ?`
const QueryUpdateAppStateByAppKeyAndStage = `UPDATE AppStates SET state = ?, version = ?, release_key = ? WHERE app_key = ? AND stage = ?`
const QueryUpdateDeviceState = `UPDATE DeviceStates SET device_status = ?, interface_type = ?`

const QueryInsertAppStateEntry = `INSERT INTO AppStates(app_name, app_key, version, release_key, stage, state, timestamp) VALUES (?, ?, ?, ?, ?, ?, ?)`
const QueryInsertAppStateHistoryEntry = `INSERT INTO AppStateHistory(app_name, app_key, version, release_key, stage, state, timestamp) VALUES (?, ?, ?, ?, ?, ?, ?)`
const QueryInsertDeviceStateHistoryEntry = `INSERT INTO DeviceStateHistory(interface_type, device_status, timestamp) VALUES (?, ?, ?)`

const QueryUpsertRequestedStateEntry = `INSERT INTO RequestedAppStates(app_name, app_key, stage, version, present_version, newest_version, current_state, manually_requested_state, requestor_account_key, release_key, new_release_key, request_update, environment_variables, timestamp)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) on conflict(app_name, app_key, stage) do update set
manually_requested_state=excluded.manually_requested_state,
current_state=excluded.current_state,
request_update=excluded.request_update`
