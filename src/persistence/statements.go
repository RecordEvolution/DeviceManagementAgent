package persistence

const QuerySelectCurrentAppStateByKeyAndStage = `SELECT state FROM AppStates WHERE app_key = ? AND stage = ?`

const QuerySelectAllDeviceState = `SELECT interface_type, device_status FROM DeviceStates`
const QuerySelectAllAppStates = `SELECT * FROM AppStates`
const QuerySelectAllRequestedStates = `SELECT app_name, app_key, stage, version, current_state,
manually_requested_state, requestor_account_key FROM RequestedAppStates`
const QuerySelectRequestedStateByAppKeyAndStage = `SELECT app_name, app_key, stage, version, current_state,
manually_requested_state, requestor_account_key FROM RequestedAppStates WHERE app_key = ? AND stage = ?`

const QueryUpdateAppStateByAppKeyAndStage = `UPDATE AppStates SET state = ? WHERE app_key = ? AND stage = ?`
const QueryUpdateDeviceState = `UPDATE DeviceStates SET device_status = ?, interface_type = ?`

const QueryInsertAppStateEntry = `INSERT INTO AppStates(app_name, app_key, stage, state, timestamp) VALUES (?, ?, ?, ?, ?)`
const QueryInsertAppStateHistoryEntry = `INSERT INTO AppStateHistory(app_name, app_key, stage, state, timestamp) VALUES (?, ?, ?, ?, ?)`
const QueryInsertDeviceStateHistoryEntry = `INSERT INTO DeviceStateHistory(interface_type, device_status, timestamp) VALUES (?, ?, ?)`

const QueryUpsertRequestedStateEntry = `INSERT INTO RequestedAppStates(app_name, app_key, stage, version, current_state, manually_requested_state, requestor_account_key, timestamp)
VALUES (?, ?, ?, ?, ?, ?, ?, ?) on conflict(app_name, app_key, stage) do update set
manually_requested_state=excluded.manually_requested_state,
current_state=excluded.current_state`
