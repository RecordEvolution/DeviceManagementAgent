package persistence

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io/ioutil"
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

func NewSQLiteDb(config *config.Config) (*AppStateDatabase, error) {
	databaseFileName := config.CommandLineArguments.DatabaseFileName
	db, err := sql.Open("sqlite3", "./"+databaseFileName)
	if err != nil {
		return nil, err
	}
	return &AppStateDatabase{db: db, config: config}, nil
}

func (sqlite *AppStateDatabase) Close() error {
	return sqlite.db.Close()
}

func (sqlite *AppStateDatabase) Init() error {
	scriptsDir := sqlite.config.CommandLineArguments.DatabaseScriptsDirectory
	files, err := ioutil.ReadDir(scriptsDir)
	if err != nil {
		return err
	}

	for _, file := range files {
		scriptFilePath := scriptsDir + "/" + file.Name()
		log.Debug().Msgf("Executing Database script with path: %s", scriptFilePath)
		err := sqlite.executeFromFile(scriptFilePath)
		if err != nil {
			return err
		}
	}

	return nil
}

func (ast *AppStateDatabase) UpsertAppState(app *common.App, newState common.AppState) (common.Timestamp, error) {
	selectStatement, err := ast.db.Prepare(QuerySelectCurrentAppStateByKeyAndStage)
	defer selectStatement.Close()
	if err != nil {
		return "", err
	}

	rows, err := selectStatement.Query(app.AppKey, app.Stage)
	hasResult := rows.Next() // only get first result since there should only be one

	if hasResult == false {
		err := rows.Close()
		if err != nil {
			return "", err
		}

		return ast.insertAppState(app)
	}

	var curState string
	var curVersion string
	var curReleaseKey uint64
	err = rows.Scan(&curState, &curVersion, &curReleaseKey)
	if err != nil {
		return "", err
	}

	if curState == string(newState) && curVersion == app.Version && curReleaseKey == app.ReleaseKey {
		err := rows.Close()
		if err != nil {
			return "", err
		}

		// Silently do nothing if state is already the same
		return "", nil
	}

	err = rows.Close()
	if err != nil {
		return "", err
	}

	// First add new entry in history
	insertStatement, err := ast.db.Prepare(QueryInsertAppStateHistoryEntry) // Prepare statement.
	if err != nil {
		return "", err
	}

	historyTimestamp := time.Now().Format(time.RFC3339)
	_, err = insertStatement.Exec(app.AppName, app.AppKey, app.Version, app.ReleaseKey, app.Stage, curState, historyTimestamp)
	if err != nil {
		return "", err
	}

	err = insertStatement.Close()
	if err != nil {
		return "", err
	}

	// Update current app state
	updateStatement, err := ast.db.Prepare(QueryUpdateAppStateByAppKeyAndStage) // Prepare statement.
	if err != nil {
		return "", err
	}
	_, err = updateStatement.Exec(newState, app.Version, app.ReleaseKey, app.AppKey, app.Stage)
	if err != nil {
		return "", err
	}

	err = updateStatement.Close()
	if err != nil {
		return "", err
	}

	// Update RequestedAppState
	requestedState, err := ast.GetRequestedState(app)
	if err != nil {
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
		return "", err
	}

	return common.Timestamp(historyTimestamp), nil
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
		err := rows.Close()
		if err != nil {
			return &common.App{}, err
		}

		return &common.App{}, fmt.Errorf("no app state was found for app key: %d and stage: %s", appKey, stage)
	}

	appState := &common.App{}
	err = rows.Scan(&appState.AppName, &appState.AppKey, &appState.Version, &appState.ReleaseKey, &appState.Stage, &appState.CurrentState, &appState.LastUpdated)

	if err != nil {
		return &common.App{}, err
	}

	err = rows.Close()
	if err != nil {
		return &common.App{}, err
	}

	return appState, nil
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

	return apps, nil
}

// insertAppState inserts a new AppState entry into the database and returns the timestamp
func (ast *AppStateDatabase) insertAppState(app *common.App) (common.Timestamp, error) {
	insertStatement, err := ast.db.Prepare(QueryInsertAppStateEntry) // Prepare statement.
	if err != nil {
		return "", err
	}

	timestamp := time.Now().Format(time.RFC3339)
	_, err = insertStatement.Exec(app.AppName, app.AppKey, app.Version, app.ReleaseKey, app.Stage, app.CurrentState, timestamp)
	if err != nil {
		return "", err
	}

	err = insertStatement.Close()
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

func (ast *AppStateDatabase) GetRequestedState(app *common.App) (common.TransitionPayload, error) {
	preppedStatement, err := ast.db.Prepare(QuerySelectRequestedStateByAppKeyAndStage)
	defer preppedStatement.Close()

	if err != nil {
		return common.TransitionPayload{}, err
	}

	rows, err := preppedStatement.Query(app.AppKey, app.Stage)
	if err != nil {
		return common.TransitionPayload{}, err
	}

	hasResult := rows.Next() // only get first result

	if hasResult == false {
		err := rows.Close()
		if err != nil {
			return common.TransitionPayload{}, err
		}

		return common.TransitionPayload{}, fmt.Errorf("No requested state found for app_key: %d with stage: %s", app.AppKey, app.Stage)
	}

	var appName string
	var appKey uint64
	// var deviceToAppKey uint64
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
	// var callerAuthID string

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

		defer upsertStatement.Close()

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
	// TODO: test if this is still relevant --> I don't think so since we always send full state now
	if payload.CurrentState == "" {
		app, err := ast.GetAppState(payload.AppKey, payload.Stage)
		if err != nil {
			return err
		}

		payload.CurrentState = app.CurrentState
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
		return err
	}

	return nil
}

func (ast *AppStateDatabase) updateDeviceState(newStatus system.DeviceStatus, newInt system.NetworkInterface) error {
	selectStatement, err := ast.db.Prepare(QuerySelectAllDeviceState)
	if err != nil {
		return err
	}
	rows, err := selectStatement.Query()
	hasResult := rows.Next() // only get first result

	if hasResult == false {
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

func (ast *AppStateDatabase) executeFromFile(filePath string) error {
	file, err := ioutil.ReadFile(filePath)

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
