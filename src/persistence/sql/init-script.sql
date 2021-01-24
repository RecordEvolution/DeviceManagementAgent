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

CREATE TABLE IF NOT EXISTS "AppStates" (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  app_name TEXT NOT NULL,
  app_key INTEGER NOT NULL,
  stage TEXT CHECK( stage IN ('DEV', 'PROD') ) NOT NULL,
  state TEXT CHECK( state IN ('PRESENT', 'REMOVED', 'UNINSTALLED', 'FAILED', 'BUILDING', 'TRANSFERRED', 'TRANSFERRING', 'PUBLISHING', 'DOWNLOADING', 'STARTING', 'STOPPING', 'UPDATING', 'DELETING', 'RUNNING') ) NOT NULL,
  timestamp TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS "AppStateHistory" (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  app_name TEXT NOT NULL,
  app_key INTEGER NOT NULL,
  stage TEXT CHECK( stage IN ('DEV', 'PROD') ) NOT NULL,
  state TEXT CHECK( state IN ('PRESENT', 'REMOVED', 'UNINSTALLED', 'FAILED', 'BUILDING', 'TRANSFERRED', 'TRANSFERRING', 'PUBLISHING', 'DOWNLOADING', 'STARTING', 'STOPPING', 'UPDATING', 'DELETING', 'RUNNING') ) NOT NULL,
  timestamp TEXT NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS app_states_unique ON AppStates(app_name, app_key, stage);
CREATE UNIQUE INDEX IF NOT EXISTS app_states_history_unique ON AppStateHistory(app_name, app_key, stage);

INSERT OR IGNORE INTO DeviceStates(interface_type, device_status, timestamp) VALUES ('NONE', 'DISCONNECTED', strftime('%s','now'));