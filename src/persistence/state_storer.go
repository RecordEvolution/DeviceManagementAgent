package persistence

import (
	"database/sql"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"reagent/common"
	"reagent/config"
	"reagent/system"
	"runtime"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type AppStateStorer struct {
	db     *sql.DB
	config *config.Config
}

func NewSQLiteDb(config *config.Config) (*AppStateStorer, error) {
	const databaseFileName = "reagent.db"
	db, err := sql.Open("sqlite3", "./"+databaseFileName)
	if err != nil {
		return nil, err
	}
	return &AppStateStorer{db: db, config: config}, nil
}

func (sqlite *AppStateStorer) Close() error {
	return sqlite.db.Close()
}

func (sqlite *AppStateStorer) Init() error {
	_, b, _, _ := runtime.Caller(0)
	basepath := filepath.Dir(b)
	return sqlite.executeFromFile(basepath + "/init-script.sql")
}

func (ast *AppStateStorer) UpdateAppState(app *common.App, newState common.AppState) error {
	selectStatement, err := ast.db.Prepare(QuerySelectCurrentAppStateByKeyAndStage)
	if err != nil {
		return err
	}
	rows, err := selectStatement.Query(app.AppKey, app.Stage)
	hasResult := rows.Next() // only get first result since there should only be one

	if hasResult == false {
		err := rows.Close()
		if err != nil {
			return err
		}

		return ast.insertAppState(app)
	}

	var curState string
	rows.Scan(&curState)

	if curState == string(newState) {
		err := rows.Close()
		if err != nil {
			return err
		}

		// Silently do nothing if state is already the same
		return nil
	}

	err = rows.Close()
	if err != nil {
		return err
	}

	// First add new entry in history
	insertStatement, err := ast.db.Prepare(QueryInsertAppStateHistoryEntry) // Prepare statement.
	if err != nil {
		return err
	}
	_, err = insertStatement.Exec(app.AppName, app.AppKey, app.Stage, curState, time.Now().Format(time.RFC3339))
	if err != nil {
		return err
	}

	err = insertStatement.Close()
	if err != nil {
		return err
	}

	// Update current state
	updateStatement, err := ast.db.Prepare(QueryUpdateAppStateByAppKeyAndStage) // Prepare statement.
	if err != nil {
		return err
	}
	_, err = updateStatement.Exec(newState, app.AppKey, app.Stage)
	if err != nil {
		return err
	}

	err = updateStatement.Close()
	if err != nil {
		return err
	}

	return nil
}

func (ast *AppStateStorer) GetAppState(appKey uint64, stage common.Stage) (PersistentAppState, error) {

	preppedStatement, err := ast.db.Prepare(QuerySelectAppStateByAppKeyAndStage)
	if err != nil {
		return PersistentAppState{}, err
	}

	rows, err := preppedStatement.Query(appKey, stage)

	if err != nil {
		return PersistentAppState{}, err
	}

	hasRow := rows.Next()
	if !hasRow {
		err := rows.Close()
		if err != nil {
			return PersistentAppState{}, err
		}

		return PersistentAppState{}, fmt.Errorf("no app state was found for app key: %d and stage: %s", appKey, stage)
	}

	appState := PersistentAppState{}
	err = rows.Scan(&appState.AppName, &appState.AppKey, &appState.Stage, &appState.State, &appState.Timestamp)

	if err != nil {
		return PersistentAppState{}, err
	}

	err = rows.Close()
	if err != nil {
		return PersistentAppState{}, err
	}

	return appState, nil
}

func (ast *AppStateStorer) GetAppStates() ([]PersistentAppState, error) {
	rows, err := ast.db.Query(QuerySelectAllAppStates)

	if err != nil {
		return nil, err
	}

	pAppState := []PersistentAppState{}
	for rows.Next() {
		s := PersistentAppState{}
		err = rows.Scan(&s.AppName, &s.AppKey, &s.Stage, &s.State, &s.Timestamp)
		if err != nil {
			return nil, err
		}
		pAppState = append(pAppState, s)
	}

	err = rows.Close()
	if err != nil {
		return nil, err
	}

	return pAppState, nil
}

func (ast *AppStateStorer) insertAppState(app *common.App) error {
	insertStatement, err := ast.db.Prepare(QueryInsertAppStateEntry) // Prepare statement.
	if err != nil {
		return err
	}
	_, err = insertStatement.Exec(app.AppName, app.AppKey, app.Stage, app.CurrentState, time.Now().Format(time.RFC3339))
	if err != nil {
		return err
	}

	err = insertStatement.Close()
	if err != nil {
		return err
	}

	return nil
}

func (ast *AppStateStorer) UpdateDeviceStatus(status system.DeviceStatus) error {
	return ast.updateDeviceState(status, "")
}

func (ast *AppStateStorer) UpdateNetworkInterface(intf system.NetworkInterface) error {
	return ast.updateDeviceState("", intf)
}

func (ast *AppStateStorer) GetRequestedStates() ([]common.TransitionPayload, error) {
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
		var stage common.Stage
		var version string
		var currentState common.AppState
		var requestedState common.AppState
		// var callerAuthID string

		// app_name, app_key, stage, current_state, manually_requested_state, requestor_account_key, device_to_app_key, caller_authid
		err = rows.Scan(&appName, &appKey, &stage, &version, &currentState, &requestedState, &requestorAccountKey)
		payload := common.BuildTransitionPayload(appKey, appName, requestorAccountKey, stage, currentState, requestedState, ast.config)

		if err != nil {
			return nil, err
		}
		payloads = append(payloads, payload)
	}

	return payloads, nil
}

func (ast *AppStateStorer) GetRequestedState(app *common.App) (common.TransitionPayload, error) {
	preppedStatement, err := ast.db.Prepare(QuerySelectRequestedStateByAppKeyAndStage)
	if err != nil {
		return common.TransitionPayload{}, err
	}

	rows, err := preppedStatement.Query(app.AppKey, app.Stage)
	if err != nil {
		return common.TransitionPayload{}, err
	}

	hasResult := rows.Next() // only get first result

	fmt.Println("hasResult:", hasResult)
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
	var currentState common.AppState
	var requestedState common.AppState
	// var callerAuthID string

	err = rows.Scan(&appName, &appKey, &stage, &version, &currentState, &requestedState, &requestorAccountKey)
	if err != nil {
		return common.TransitionPayload{}, err
	}

	err = rows.Close()
	if err != nil {
		return common.TransitionPayload{}, err
	}

	payload := common.BuildTransitionPayload(appKey, appName, requestorAccountKey, stage, currentState, requestedState, ast.config)
	if err != nil {
		return common.TransitionPayload{}, err
	}

	return payload, nil
}

func (ast *AppStateStorer) BulkUpsertRequestedStateChanges(payloads []common.TransitionPayload) error {
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

		_, err = upsertStatement.Exec(payload.AppName, payload.AppKey, payload.Stage, payload.Version,
			payload.CurrentState, payload.RequestedState, payload.RequestorAccountKey,
			time.Now().Format(time.RFC3339),
		)

		if err != nil {
			tx.Rollback()
			return err
		}
	}

	return tx.Commit()
}

func (ast *AppStateStorer) UpsertRequestedStateChange(payload common.TransitionPayload) error {
	upsertStatement, err := ast.db.Prepare(QueryUpsertRequestedStateEntry) // Prepare statement.
	if err != nil {
		return err
	}

	if payload.CurrentState == "" {
		state, err := ast.GetAppState(payload.AppKey, payload.Stage)
		if err != nil {
			return err
		}

		payload.CurrentState = state.State
	}

	_, err = upsertStatement.Exec(payload.AppName, payload.AppKey, payload.Stage, payload.Version,
		payload.CurrentState, payload.RequestedState, payload.RequestorAccountKey,
		time.Now().Format(time.RFC3339),
	)

	err = upsertStatement.Close()
	if err != nil {
		return err
	}

	if err != nil {
		return err
	}

	return nil
}

func (ast *AppStateStorer) updateDeviceState(newStatus system.DeviceStatus, newInt system.NetworkInterface) error {
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
	rows.Scan(&curInterfaceType, &curDeviceStatus)

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

func (ast *AppStateStorer) executeFromFile(filePath string) error {
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
