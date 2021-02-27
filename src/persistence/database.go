package persistence

import (
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"reagent/common"
	"reagent/config"
	"reagent/system"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/rs/zerolog/log"
)

type AppStateDatabase struct {
	db     *sql.DB
	config *config.Config
}

const (
	driver      = "sqlite3"
	cacheShared = "cache=shared"
	busyTimeout = "_busy_timeout=2500"
	journalMode = "_journal_mode=WAL"
	syncOff     = "_synchronous=OFF"
	maxOpenConn = 1
)

func NewSQLiteDb(config *config.Config) (*AppStateDatabase, error) {
	databaseFileName := config.CommandLineArguments.DatabaseFileName
	connectionString := fmt.Sprintf("./%s?%s&%s&%s&%s", databaseFileName, cacheShared, busyTimeout, journalMode, syncOff)
	log.Debug().Msgf("Db: Setup database with %s as connection string", connectionString)
	db, err := sql.Open(driver, connectionString)
	if err != nil {
		return nil, err
	}

	// SQLite cannot handle concurrent writes, so we limit sqlite to one connection. https://github.com/mattn/go-sqlite3/issues/274
	// will not effect performance noticably, even for large amounts of operations: https://stackoverflow.com/questions/35804884/sqlite-concurrent-writing-performance/35805826
	db.SetMaxOpenConns(maxOpenConn)

	return &AppStateDatabase{db: db, config: config}, nil
}

func (sqlite *AppStateDatabase) Close() error {
	return sqlite.db.Close()
}

//go:embed update-scripts/*
var scriptFiles embed.FS

const scriptsDir = "update-scripts"

func (sqlite *AppStateDatabase) Init() error {
	scriptFiles, err := fs.ReadDir(scriptFiles, scriptsDir)
	if err != nil {
		return err
	}

	for _, file := range scriptFiles {
		fileName := file.Name()
		log.Debug().Msgf("Executing Database script: %s", fileName)
		err := sqlite.execute(scriptsDir + "/" + fileName)
		if err != nil {
			return err
		}
	}

	return nil
}

func (ast *AppStateDatabase) UpsertAppState(app *common.App, newState common.AppState) (common.Timestamp, error) {
	app.StateLock.Lock()

	selectStatement, err := ast.db.Prepare(QuerySelectCurrentAppStateByKeyAndStage)
	if err != nil {
		app.StateLock.Unlock()
		return "", err
	}

	rows, err := selectStatement.Query(app.AppKey, app.Stage)
	hasResult := rows.Next() // only get first result since there should only be one

	if hasResult == false {
		err := rows.Close()
		if err != nil {
			app.StateLock.Unlock()
			return "", err
		}

		app.StateLock.Unlock()
		return ast.insertAppState(app)
	}

	var curState string
	var curVersion string
	var curReleaseKey uint64
	err = rows.Scan(&curState, &curVersion, &curReleaseKey)
	if err != nil {
		app.StateLock.Unlock()
		return "", rows.Close()
	}

	if curState == string(newState) && curVersion == app.Version && curReleaseKey == app.ReleaseKey {
		err := rows.Close()
		if err != nil {
			app.StateLock.Unlock()
			return "", err
		}

		// Silently do nothing if state is already the same
		app.StateLock.Unlock()
		return "", nil
	}

	err = rows.Close()
	if err != nil {
		app.StateLock.Unlock()
		return "", err
	}

	// First add new entry in history
	insertStatement, err := ast.db.Prepare(QueryInsertAppStateHistoryEntry) // Prepare statement.
	if err != nil {
		app.StateLock.Unlock()
		return "", err
	}

	historyTimestamp := time.Now().Format(time.RFC3339)
	_, err = insertStatement.Exec(app.AppName, app.AppKey, app.Version, app.ReleaseKey, app.Stage, curState, historyTimestamp)
	if err != nil {
		app.StateLock.Unlock()
		return "", err
	}

	err = insertStatement.Close()
	if err != nil {
		app.StateLock.Unlock()
		return "", err
	}

	// Update current app state
	updateStatement, err := ast.db.Prepare(QueryUpdateAppStateByAppKeyAndStage) // Prepare statement.
	if err != nil {
		app.StateLock.Unlock()
		return "", err
	}
	_, err = updateStatement.Exec(newState, app.Version, app.ReleaseKey, app.AppKey, app.Stage)
	if err != nil {
		app.StateLock.Unlock()
		return "", err
	}

	err = updateStatement.Close()
	if err != nil {
		app.StateLock.Unlock()
		return "", err
	}

	// Update RequestedAppState
	requestedState, err := ast.GetRequestedState(app.AppKey, app.Stage)
	if err != nil {
		app.StateLock.Unlock()
		return "", err
	}

	// if true: when it reconnects, it can try to let the database know it is now a different version as the remote database
	// note: this is only neccessary because we do not force users to update to the latest version
	// The database will then check if this version is actually the latest version, if not it will request another update
	requestedState.RequestUpdate = app.RequestUpdate
	requestedState.CurrentState = newState
	requestedState.RequestedState = app.RequestedState
	requestedState.PresentVersion = app.Version
	requestedState.ReleaseKey = app.ReleaseKey

	err = ast.UpsertRequestedStateChange(requestedState)
	if err != nil {
		app.StateLock.Unlock()
		return "", err
	}

	timestamp := common.Timestamp(historyTimestamp)

	app.StateLock.Unlock()

	return timestamp, nil
}

func (ast *AppStateDatabase) GetAppState(appKey uint64, stage common.Stage) (*common.App, error) {
	preppedStatement, err := ast.db.Prepare(QuerySelectAppStateByAppKeyAndStage)
	if err != nil {
		return &common.App{}, err
	}

	rows, err := preppedStatement.Query(appKey, stage)
	if err != nil {
		return &common.App{}, err
	}

	hasRow := rows.Next()
	if !hasRow {
		if err != nil {
			rows.Close()
			return &common.App{}, err
		}

		rows.Close()
		return nil, nil
	}

	app := &common.App{}
	err = rows.Scan(&app.AppName, &app.AppKey, &app.Version, &app.ReleaseKey, &app.Stage, &app.CurrentState, &app.LastUpdated)
	if err != nil {
		rows.Close()
		return &common.App{}, err
	}

	err = rows.Close()
	if err != nil {
		return &common.App{}, err
	}

	requestedState, err := ast.GetRequestedState(app.AppKey, app.Stage)
	if err != nil {
		return &common.App{}, err
	}

	// neccessary to update remote app state (need to know e.g. who to publish updates to)
	app.RequestUpdate = requestedState.RequestUpdate
	app.RequestorAccountKey = requestedState.RequestorAccountKey
	app.RequestedState = requestedState.RequestedState

	return app, nil
}

func (ast *AppStateDatabase) GetAppStates() ([]*common.App, error) {
	rows, err := ast.db.Query(QuerySelectAllAppStates)
	if err != nil {
		return nil, err
	}

	apps := []*common.App{}
	for rows.Next() {
		app := &common.App{}
		err = rows.Scan(&app.AppName, &app.AppKey, &app.Version, &app.ReleaseKey, &app.Stage, &app.CurrentState, &app.LastUpdated)
		if err != nil {
			return nil, err
		}

		apps = append(apps, app)
	}

	err = rows.Close()
	if err != nil {
		return nil, err
	}

	requestedStates, err := ast.GetRequestedStates()
	if err != nil {
		return nil, err
	}

	for _, requestedState := range requestedStates {
		for appIndex := range apps {
			app := apps[appIndex]

			if app.AppKey == requestedState.AppKey && app.Stage == requestedState.Stage {
				// neccessary to update remote app state (need to know e.g. who to publish updates to)
				app.RequestorAccountKey = requestedState.RequestorAccountKey
				app.RequestedState = requestedState.RequestedState
				app.RequestUpdate = requestedState.RequestUpdate
			}

		}
	}

	return apps, nil
}

// insertAppState inserts a new AppState entry into the database and returns the timestamp
func (ast *AppStateDatabase) insertAppState(app *common.App) (common.Timestamp, error) {
	insertStatement, err := ast.db.Prepare(QueryInsertAppStateEntry) // Prepare statement.
	if err != nil {
		return "", err
	}

	defer insertStatement.Close()

	timestamp := time.Now().Format(time.RFC3339)

	app.StateLock.Lock()
	appName := app.AppName
	appKey := app.AppKey
	version := app.Version
	releaseKey := app.ReleaseKey
	stage := app.Stage
	currentState := app.CurrentState
	app.StateLock.Unlock()

	_, err = insertStatement.Exec(appName, appKey, version, releaseKey, stage, currentState, timestamp)
	if err != nil {
		return "", err
	}

	return common.Timestamp(timestamp), nil
}

func (ast *AppStateDatabase) UpdateDeviceStatus(status system.DeviceStatus) error {
	return ast.updateDeviceState(status, "")
}

func (ast *AppStateDatabase) UpdateNetworkInterface(intf system.NetworkInterface) error {
	return ast.updateDeviceState("", intf)
}

func (ast *AppStateDatabase) GetRequestedStates() ([]common.TransitionPayload, error) {
	rows, err := ast.db.Query(QuerySelectAllRequestedStates)
	if err != nil {
		return nil, err
	}

	defer rows.Close()

	payloads := []common.TransitionPayload{}
	for rows.Next() {
		var appName string
		var appKey uint64
		// var deviceToAppKey uint64
		var requestorAccountKey uint64
		var releaseKey uint64
		var newReleaseKey uint64
		var stage common.Stage
		var version string
		var presentVersion string
		var newestVersion string
		var environmentVariablesString string
		var currentState common.AppState
		var requestedState common.AppState
		// var callerAuthID string

		// app_name, app_key, stage, current_state, manually_requested_state, requestor_account_key, device_to_app_key, caller_authid
		err = rows.Scan(&appName, &appKey, &stage, &version, &presentVersion, &newestVersion, &currentState, &requestedState, &requestorAccountKey, &releaseKey, &newReleaseKey, &environmentVariablesString)
		if err != nil {
			return nil, err
		}

		payload := common.BuildTransitionPayload(appKey, appName, requestorAccountKey, stage, currentState, requestedState, releaseKey, newReleaseKey, ast.config)
		payload.Version = version
		payload.NewestVersion = newestVersion
		payload.PresentVersion = presentVersion

		environmentVariables := make(map[string]interface{})
		if environmentVariablesString != "" {
			err := json.Unmarshal([]byte(environmentVariablesString), &environmentVariables)
			if err != nil {
				return nil, err
			}

			payload.EnvironmentVariables = environmentVariables
		}

		payloads = append(payloads, payload)
	}

	return payloads, nil
}

func (ast *AppStateDatabase) GetRequestedState(aKey uint64, aStage common.Stage) (common.TransitionPayload, error) {
	preppedStatement, err := ast.db.Prepare(QuerySelectRequestedStateByAppKeyAndStage)
	if err != nil {
		return common.TransitionPayload{}, err
	}

	rows, err := preppedStatement.Query(aKey, aStage)
	if err != nil {
		return common.TransitionPayload{}, err
	}

	hasResult := rows.Next() // only get first result

	if hasResult == false {
		err := rows.Close()
		if err != nil {
			return common.TransitionPayload{}, err
		}

		return common.TransitionPayload{}, fmt.Errorf("No requested state found for app_key: %d with stage: %s", aKey, aStage)
	}

	var appName string
	var appKey uint64
	var requestorAccountKey uint64
	var stage common.Stage
	var version string
	var presentVersion string
	var releaseKey uint64
	var newReleaseKey uint64
	var newestVersion string
	var currentState common.AppState
	var requestedState common.AppState
	var environmentVariablesString string

	err = rows.Scan(&appName, &appKey, &stage, &version, &presentVersion, &newestVersion, &currentState, &requestedState, &requestorAccountKey, &releaseKey, &newReleaseKey, &environmentVariablesString)
	if err != nil {
		return common.TransitionPayload{}, err
	}

	err = rows.Close()
	if err != nil {
		return common.TransitionPayload{}, err
	}

	payload := common.BuildTransitionPayload(appKey, appName, requestorAccountKey, stage, currentState, requestedState, releaseKey, newReleaseKey, ast.config)
	payload.Version = version
	payload.NewestVersion = newestVersion
	payload.PresentVersion = presentVersion

	environmentVariables := make(map[string]interface{})
	if environmentVariablesString != "" {
		err := json.Unmarshal([]byte(environmentVariablesString), &environmentVariables)
		if err != nil {
			return common.TransitionPayload{}, err
		}

		payload.EnvironmentVariables = environmentVariables
	}

	if err != nil {
		return common.TransitionPayload{}, err
	}

	return payload, nil
}

func (ast *AppStateDatabase) BulkUpsertRequestedStateChanges(payloads []common.TransitionPayload) error {
	tx, err := ast.db.Begin()
	if err != nil {
		return err
	}

	for _, payload := range payloads {
		upsertStatement, err := tx.Prepare(QueryUpsertRequestedStateEntry) // Prepare statement.
		if err != nil {
			tx.Rollback()
			return err
		}

		environmentsJSONBytes, err := json.Marshal(payload.EnvironmentVariables)
		if err != nil {
			return err
		}

		environmentsJSONString := string(environmentsJSONBytes)

		_, err = upsertStatement.Exec(payload.AppName, payload.AppKey, payload.Stage, payload.Version, payload.PresentVersion, payload.NewestVersion,
			payload.CurrentState, payload.RequestedState, payload.RequestorAccountKey, payload.ReleaseKey, payload.NewReleaseKey, payload.RequestUpdate, environmentsJSONString,
			time.Now().Format(time.RFC3339),
		)

		if err != nil {
			tx.Rollback()
			return err
		}
	}

	return tx.Commit()
}

func (ast *AppStateDatabase) UpsertRequestedStateChange(payload common.TransitionPayload) error {
	upsertStatement, err := ast.db.Prepare(QueryUpsertRequestedStateEntry) // Prepare statement.
	if err != nil {
		return err
	}

	defer upsertStatement.Close()

	// if the payload we received from the backend/database does not include a current state
	// // TODO: test if this is still relevant --> I don't think so since we always send full state now
	// if payload.CurrentState == "" {
	// 	app, err := ast.GetAppState(payload.AppKey, payload.Stage)
	// 	if err != nil {
	// 		return err
	// 	}

	// 	payload.CurrentState = app.CurrentState
	// }

	environmentsJSONBytes, err := json.Marshal(payload.EnvironmentVariables)
	if err != nil {
		return err
	}

	environmentsJSONString := string(environmentsJSONBytes)

	_, err = upsertStatement.Exec(payload.AppName, payload.AppKey, payload.Stage, payload.Version, payload.PresentVersion, payload.NewestVersion,
		payload.CurrentState, payload.RequestedState, payload.RequestorAccountKey, payload.ReleaseKey, payload.NewReleaseKey, payload.RequestUpdate, environmentsJSONString,
		time.Now().Format(time.RFC3339),
	)

	if err != nil {
		return err
	}

	return nil
}

func (ast *AppStateDatabase) ClearAllLogHistory(appName string, appKey uint64, stage common.Stage) error {
	logsBytes, err := json.Marshal([]string{})
	if err != nil {
		return err
	}

	tx, err := ast.db.Begin()
	if err != nil {
		tx.Rollback()
		return err
	}

	for _, logType := range []common.LogType{common.APP, common.BUILD, common.PULL, common.PUSH} {
		upsertStatement, err := tx.Prepare(QueryUpsertLogHistoryEntry)
		if err != nil {
			tx.Rollback()
			return err
		}

		_, err = upsertStatement.Exec(appName, appKey, stage, logType, string(logsBytes))
		if err != nil {
			tx.Rollback()
			return err
		}

		if err != nil {
			tx.Rollback()
			return err
		}
	}

	return tx.Commit()
}

func (ast *AppStateDatabase) UpsertLogHistory(appName string, appKey uint64, stage common.Stage, logType common.LogType, logs []string) error {
	upsertStatement, err := ast.db.Prepare(QueryUpsertLogHistoryEntry)
	if err != nil {
		return err
	}

	defer upsertStatement.Close()

	logsBytes, err := json.Marshal(logs)
	if err != nil {
		return err
	}

	_, err = upsertStatement.Exec(appName, appKey, stage, logType, string(logsBytes))
	if err != nil {
		return err
	}

	return nil
}

func (ast *AppStateDatabase) GetAppLogHistory(appName string, appKey uint64, stage common.Stage, logType common.LogType) ([]string, error) {
	preppedStatement, err := ast.db.Prepare(QuerySelectLogHistoryByAppKeyStageAndType)
	if err != nil {
		return nil, err
	}

	rows, err := preppedStatement.Query(appKey, stage, logType)
	if err != nil {
		return nil, err
	}

	hasResult := rows.Next() // only get first result
	if hasResult == false {
		err := rows.Close()
		if err != nil {
			return nil, err
		}

		return nil, fmt.Errorf("No logs found (%s) for %d (%s) ", logType, appKey, stage)
	}

	var logsString string

	err = rows.Scan(&logsString)
	if err != nil {
		return nil, err
	}

	err = rows.Close()
	if err != nil {
		return nil, err
	}

	var logsArray []string
	err = json.Unmarshal([]byte(logsString), &logsArray)
	if err != nil {
		return nil, err
	}

	return logsArray, nil
}

func (ast *AppStateDatabase) updateDeviceState(newStatus system.DeviceStatus, newInt system.NetworkInterface) error {
	selectStatement, err := ast.db.Prepare(QuerySelectAllDeviceState)
	if err != nil {
		return err
	}

	defer selectStatement.Close()

	rows, err := selectStatement.Query()
	hasResult := rows.Next() // only get first result

	if hasResult == false {
		err := rows.Close()
		if err != nil {
			return err
		}

		return fmt.Errorf("No device state to update")
	}

	var curInterfaceType string
	var curDeviceStatus string
	err = rows.Scan(&curInterfaceType, &curDeviceStatus)
	if err != nil {
		return err
	}

	if curInterfaceType == string(newInt) {
		err := rows.Close()
		if err != nil {
			return err
		}

		return fmt.Errorf("The current interface is already %s", curInterfaceType)
	}

	if curDeviceStatus == string(newStatus) {
		err := rows.Close()
		if err != nil {
			return err
		}

		return fmt.Errorf("The device status is already %s", curDeviceStatus)
	}

	err = rows.Close()
	if err != nil {
		return err
	}

	// Add new entry in history
	insertStatement, err := ast.db.Prepare(QueryInsertDeviceStateHistoryEntry) // Prepare statement.
	if err != nil {
		return err
	}
	_, err = insertStatement.Exec(curInterfaceType, curDeviceStatus, time.Now().Format(time.RFC3339))
	if err != nil {
		return err
	}

	err = insertStatement.Close()
	if err != nil {
		return err
	}

	// Fallback to the current when value is left out
	var newestStatus string
	var newestInterface string

	if newStatus == "" {
		newestStatus = curDeviceStatus
	} else {
		newestStatus = string(newStatus)
	}

	if newInt == "" {
		newestInterface = curInterfaceType
	} else {
		newestInterface = string(newInt)
	}

	// Update current state
	updateStatement, err := ast.db.Prepare(QueryUpdateDeviceState) // Prepare statement.
	if err != nil {
		return err
	}
	_, err = updateStatement.Exec(newestStatus, newestInterface)
	if err != nil {
		return err
	}

	err = updateStatement.Close()
	if err != nil {
		return err
	}

	return nil
}

func (ast *AppStateDatabase) execute(fileName string) error {
	file, err := fs.ReadFile(scriptFiles, fileName)

	if err != nil {
		return err
	}

	requests := strings.Split(string(file), ";\n")

	for _, request := range requests {
		_, err := ast.db.Exec(request)
		if err != nil {
			return err
		}
	}

	return nil
}
