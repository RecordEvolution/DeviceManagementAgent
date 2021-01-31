CREATE TABLE IF NOT EXISTS "DeviceStates" (
  id INTEGER PRIMARY KEY CHECK (id = 1) DEFAULT 1,
  interface_type TEXT CHECK( interface_type IN ('WLAN', 'ETHERNET', 'NONE') ) NOT NULL,
  device_status TEXT CHECK( device_status IN ('CONNECTED', 'DISCONNECTED') ) NOT NULL,
  timestamp TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS "DeviceStateHistory" (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  interface_type TEXT CHECK( interface_type IN ('WLAN', 'ETHERNET', 'NONE') ) NOT NULL,
  device_status TEXT CHECK( device_status IN ('CONNECTED', 'DISCONNECTED') ) NOT NULL,
  timestamp TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS "RequestedAppStates" (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  app_name TEXT NOT NULL,
  app_key INTEGER NOT NULL,
  stage TEXT CHECK( stage IN ('DEV', 'PROD') ) NOT NULL,
  container_name TEXT NOT NULL,
  current_state TEXT CHECK( current_state IN ('PRESENT', 'REMOVED', 'UNINSTALLED', 'FAILED', 'BUILDING', 'TRANSFERED', 'TRANSFERING', 'PUBLISHING', 'DOWNLOADING', 'STARTING', 'STOPPING', 'UPDATING', 'DELETING', 'RUNNING') ) NOT NULL,
  manually_requested_state TEXT CHECK( manually_requested_state IN ('PRESENT', 'REMOVED', 'UNINSTALLED', 'FAILED', 'BUILDING', 'TRANSFERED', 'TRANSFERING', 'PUBLISHING', 'DOWNLOADING', 'STARTING', 'STOPPING', 'UPDATING', 'DELETING', 'RUNNING') ) NOT NULL,
  image_name TEXT NOT NULL,
  repository_image_name TEXT NOT NULL,
  requestor_account_key INTEGER NOT NULL,
  device_to_app_key INTEGER NOT NULL,
  timestamp TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS "AppStates" (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  app_name TEXT NOT NULL,
  app_key INTEGER NOT NULL,
  stage TEXT CHECK( stage IN ('DEV', 'PROD') ) NOT NULL,
  state TEXT CHECK( state IN ('PRESENT', 'REMOVED', 'UNINSTALLED', 'FAILED', 'BUILDING', 'TRANSFERED', 'TRANSFERING', 'PUBLISHING', 'DOWNLOADING', 'STARTING', 'STOPPING', 'UPDATING', 'DELETING', 'RUNNING') ) NOT NULL,
  timestamp TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS "AppStateHistory" (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  app_name TEXT NOT NULL,
  app_key INTEGER NOT NULL,
  stage TEXT CHECK( stage IN ('DEV', 'PROD') ) NOT NULL,
  state TEXT CHECK( state IN ('PRESENT', 'REMOVED', 'UNINSTALLED', 'FAILED', 'BUILDING', 'TRANSFERED', 'TRANSFERING', 'PUBLISHING', 'DOWNLOADING', 'STARTING', 'STOPPING', 'UPDATING', 'DELETING', 'RUNNING') ) NOT NULL,
  timestamp TEXT NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS app_states_unique ON AppStates(app_name, app_key, stage);
CREATE UNIQUE INDEX IF NOT EXISTS requested_app_states_unique ON RequestedAppStates(app_name, app_key, stage);

INSERT OR IGNORE INTO DeviceStates(interface_type, device_status, timestamp) VALUES ('NONE', 'DISCONNECTED', strftime('%s','now'));