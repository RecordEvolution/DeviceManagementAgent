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
  version TEXT,
  present_version TEXT,
  newest_version TEXT,
  environment_variables TEXT,
  ports TEXT,
  docker_compose TEXT,
  release_key INTEGER NOT NULL,
  new_release_key INTEGER NOT NULL,
  request_update BOOLEAN NOT NULL CHECK (request_update IN (0,1)),
  current_state TEXT CHECK( current_state IN ('PRESENT', 'REMOVED', 'UNINSTALLED', 'FAILED', 'BUILDING', 'BUILT', 'TRANSFERED', 'TRANSFERING', 'PUBLISHING', 'PUBLISHED', 'DOWNLOADING', 'STARTING', 'STOPPING', 'UPDATING', 'DELETING', 'RUNNING', 'INHERIT') ) NOT NULL,
  manually_requested_state TEXT CHECK( manually_requested_state IN ('PRESENT', 'REMOVED', 'UNINSTALLED', 'FAILED', 'BUILDING', 'BUILT', 'TRANSFERED', 'TRANSFERING', 'PUBLISHING', 'PUBLISHED', 'DOWNLOADING', 'STARTING', 'STOPPING', 'UPDATING', 'DELETING', 'RUNNING', 'INHERIT') ) NOT NULL,
  requestor_account_key INTEGER NOT NULL,
  timestamp TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS "AppStates" (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  app_name TEXT NOT NULL,
  app_key INTEGER NOT NULL,
  version TEXT NOT NULL,
  release_key INTEGER NOT NULL,
  stage TEXT CHECK( stage IN ('DEV', 'PROD') ) NOT NULL,
  state TEXT CHECK( state IN ('PRESENT', 'REMOVED', 'UNINSTALLED', 'FAILED', 'BUILDING', 'BUILT', 'TRANSFERED', 'TRANSFERING', 'PUBLISHING', 'PUBLISHED', 'DOWNLOADING', 'STARTING', 'STOPPING', 'UPDATING', 'DELETING', 'RUNNING', 'INHERIT') ) NOT NULL,
  timestamp TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS "LogHistory" (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  app_name TEXT NOT NULL,
  app_key INTEGER NOT NULL,
  stage TEXT CHECK( stage IN ('DEV', 'PROD') ) NOT NULL,
  log_type TEXT CHECK( log_type IN ('PULL', 'PUSH', 'BUILD', 'APP') ) NOT NULL,
  log TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS "AppStateHistory" (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  app_name TEXT NOT NULL,
  app_key INTEGER NOT NULL,
  release_key INTEGER NOT NULL,
  version TEXT NOT NULL,
  stage TEXT CHECK( stage IN ('DEV', 'PROD') ) NOT NULL,
  state TEXT CHECK( state IN ('PRESENT', 'REMOVED', 'UNINSTALLED', 'FAILED', 'BUILDING', 'BUILT', 'TRANSFERED', 'TRANSFERING', 'PUBLISHING', 'PUBLISHED', 'DOWNLOADING', 'STARTING', 'STOPPING', 'UPDATING', 'DELETING', 'RUNNING', 'INHERIT') ) NOT NULL,
  timestamp TEXT NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS app_states_unique ON AppStates(app_name, app_key, stage);
CREATE UNIQUE INDEX IF NOT EXISTS requested_app_states_unique ON RequestedAppStates(app_name, app_key, stage);
CREATE UNIQUE INDEX IF NOT EXISTS log_history_unique ON LogHistory(app_name, app_key, stage, log_type);

INSERT OR IGNORE INTO DeviceStates(interface_type, device_status, timestamp) VALUES ('NONE', 'DISCONNECTED', strftime('%s','now'));