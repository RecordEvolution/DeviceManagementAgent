package persistence

import (
	"context"
	"database/sql"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"reagent/api/common"
	"reagent/messenger"
	"reagent/system"
	"runtime"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type AppStateStorer struct {
	Messenger messenger.Messenger
	db        *sql.DB
}

func NewSQLiteDb() (*AppStateStorer, error) {
	const databaseFileName = "reagent.db"
	db, err := sql.Open("sqlite3", "./"+databaseFileName)
	if err != nil {
		return nil, err
	}
	return &AppStateStorer{db: db}, nil
}

func (sqlite *AppStateStorer) Close() error {
	return sqlite.db.Close()
}

func (sqlite *AppStateStorer) Init() error {
	_, b, _, _ := runtime.Caller(0)
	basepath := filepath.Dir(b)
	return sqlite.executeFromFile(basepath + "/sql/init-script.sql")
}

func (sqlite *AppStateStorer) setActualAppOnDeviceState(app *common.App, stateToSet common.AppState) error {
	ctx := context.Background()
	config := sqlite.Messenger.GetConfig()
	payload := []common.Dict{{
		"app_key":                  app.AppKey,
		"device_key":               config.DeviceKey,
		"swarm_key":                config.SwarmKey,
		"stage":                    app.Stage,
		"state":                    stateToSet,
		"request_update":           app.RequestUpdate,
		"manually_requested_state": app.ManuallyRequestedState,
	}}

	// See containers.ts
	if stateToSet == common.BUILDING {
		payload[0]["version"] = "latest"
	}

	// args := []messenger.Dict{payload}

	_, err := sqlite.Messenger.Call(ctx, common.TopicSetActualAppOnDeviceState, payload, nil, nil, nil)
	if err != nil {
		return err
	}

	return nil
}

// UpdateAppState updates the app state in the local database and remote database
func (sqlite *AppStateStorer) UpdateAppState(app *common.App, newState common.AppState) error {
	err := sqlite.updateAppState(app, newState)
	if err != nil {
		return err
	}
	err = sqlite.setActualAppOnDeviceState(app, newState)
	if err != nil {
		// Silently fail, it's okay if a remote sync is done while the device is offline.
		// Will resync once the device is online again
		// The messaging protocol should not fail with a valid internet connection
		fmt.Printf("Failed to sync app state %s for %+v with remote database", newState, app)
	}
	return nil
}

func (ast *AppStateStorer) updateAppState(app *common.App, newState common.AppState) error {
	previousAppStatement := `SELECT state FROM AppStates WHERE app_key = ? AND stage = ?`
	selectStatement, err := ast.db.Prepare(previousAppStatement)
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
		// Not sure if we should throw an error?
		fmt.Printf("The current state is already %s", newState)
		return nil
	}

	err = rows.Close()
	if err != nil {
		return err
	}

	// First add new entry in history
	insertAppHistoryStatement := `INSERT INTO AppStateHistory(app_name, app_key, stage, state, timestamp) VALUES (?, ?, ?, ?, ?)`
	insertStatement, err := ast.db.Prepare(insertAppHistoryStatement) // Prepare statement.
	if err != nil {
		return err
	}
	_, err = insertStatement.Exec(app.AppName, app.AppKey, app.Stage, curState, time.Now().Format(time.RFC3339))
	if err != nil {
		return err
	}

	// Update current state
	updateAppStatement := `UPDATE AppStates SET state = ? WHERE app_key = ? AND stage = ?`
	updateStatement, err := ast.db.Prepare(updateAppStatement) // Prepare statement.
	if err != nil {
		return err
	}
	_, err = updateStatement.Exec(newState, app.AppKey, app.Stage)
	if err != nil {
		return err
	}
	return nil
}

func (sqlite *AppStateStorer) GetLocalAppStates() ([]PersistentAppState, error) {
	selectAppStatesStatement := `SELECT * FROM AppStates`
	rows, err := sqlite.db.Query(selectAppStatesStatement)

	if err != nil {
		return nil, err
	}

	pAppState := []PersistentAppState{}
	for rows.Next() {
		s := PersistentAppState{}
		err = rows.Scan(&s.ID, &s.AppName, &s.AppName, &s.AppKey, &s.Stage, &s.State, &s.Timestamp)
		if err != nil {
			return nil, err
		}
		pAppState = append(pAppState, s)
	}

	return pAppState, nil
}

func (sqlite *AppStateStorer) insertAppState(app *common.App) error {
	insertAppHistoryStatement := `INSERT INTO AppStates(app_name, app_key, stage, state, timestamp) VALUES (?, ?, ?, ?, ?)`
	insertStatement, err := sqlite.db.Prepare(insertAppHistoryStatement) // Prepare statement.
	if err != nil {
		return err
	}
	_, err = insertStatement.Exec(app.AppName, app.AppKey, app.Stage, app.CurrentState, time.Now().Format(time.RFC3339))
	if err != nil {
		return err
	}

	return nil
}

func (sqlite *AppStateStorer) UpdateDeviceStatus(status system.DeviceStatus) error {
	return sqlite.updateDeviceState(status, "")
}

func (sqlite *AppStateStorer) UpdateNetworkInterface(intf system.NetworkInterface) error {
	return sqlite.updateDeviceState("", intf)
}

func (sqlite *AppStateStorer) updateDeviceState(newStatus system.DeviceStatus, newInt system.NetworkInterface) error {
	prevDeviceStateSQL := `SELECT interface_type, device_status FROM DeviceStates`
	selectStatement, err := sqlite.db.Prepare(prevDeviceStateSQL)
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
		rows.Close()
		return fmt.Errorf("The current interface is already %s", curInterfaceType)
	}

	if curDeviceStatus == string(newStatus) {
		rows.Close()
		return fmt.Errorf("The device status is already %s", curDeviceStatus)
	}

	err = rows.Close()
	if err != nil {
		return err
	}

	// Add new entry in history
	insertAppHistoryStatement := `INSERT INTO DeviceStateHistory(interface_type, device_status, timestamp) VALUES (?, ?, ?)`
	insertStatement, err := sqlite.db.Prepare(insertAppHistoryStatement) // Prepare statement.
	if err != nil {
		return err
	}
	_, err = insertStatement.Exec(curInterfaceType, curDeviceStatus, time.Now().Format(time.RFC3339))
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
	updateAppStatement := `UPDATE DeviceStates SET device_status = ?, interface_type = ?`
	updateStatement, err := sqlite.db.Prepare(updateAppStatement) // Prepare statement.
	if err != nil {
		return err
	}
	_, err = updateStatement.Exec(newestStatus, newestInterface)
	if err != nil {
		return err
	}
	return nil
}

func (sqlite *AppStateStorer) executeFromFile(filePath string) error {
	file, err := ioutil.ReadFile(filePath)

	if err != nil {
		return err
	}

	requests := strings.Split(string(file), ";\n")

	for _, request := range requests {
		_, err := sqlite.db.Exec(request)
		if err != nil {
			return err
		}
	}

	return nil
}