package persistence

import (
	"database/sql"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"reagent/apps"
	"runtime"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type SQLite struct {
	db *sql.DB
}

const databaseFileName = "reagent.db"

func NewSQLiteDb() (*SQLite, error) {
	db, err := sql.Open("sqlite3", "./reagent.db")
	if err != nil {
		return nil, err
	}
	return &SQLite{db: db}, nil
}

func (sqlite *SQLite) Close() error {
	return sqlite.db.Close()
}

func (sqlite *SQLite) Init() error {
	_, b, _, _ := runtime.Caller(0)
	basepath := filepath.Dir(b)
	return sqlite.executeFromFile(basepath + "/sql/init-script.sql")
}

func (sqlite *SQLite) UpdateAppState(appName string, appKey int, stage apps.Stage, newState apps.AppState) error {
	previousAppStatement := `SELECT state FROM AppStates WHERE app_key = ? AND stage = ?`
	selectStatement, err := sqlite.db.Prepare(previousAppStatement)
	if err != nil {
		return err
	}
	rows, err := selectStatement.Query(appKey, stage)
	hasResult := rows.Next() // only get first result

	if hasResult == false {
		return fmt.Errorf("No app state to update")
	}

	var curState string
	rows.Scan(&curState)

	if curState == string(newState) {
		return fmt.Errorf("The current state is already %s", newState)
	}

	err = rows.Close()
	if err != nil {
		return err
	}

	// First add new entry in history
	insertAppHistoryStatement := `INSERT INTO AppStateHistory(app_name, app_key, stage, state, timestamp) VALUES (?, ?, ?, ?, ?)`
	insertStatement, err := sqlite.db.Prepare(insertAppHistoryStatement) // Prepare statement.
	if err != nil {
		return err
	}
	_, err = insertStatement.Exec(appName, appKey, stage, curState, time.Now().Format(time.RFC3339))
	if err != nil {
		return err
	}

	// Update current state
	updateAppStatement := `UPDATE AppStates SET state = ? WHERE app_key = ? AND stage = ?`
	updateStatement, err := sqlite.db.Prepare(updateAppStatement) // Prepare statement.
	if err != nil {
		return err
	}
	_, err = updateStatement.Exec(newState, appKey, stage)
	if err != nil {
		return err
	}
	return nil
}

func (sqlite *SQLite) InsertAppState(appName string, appKey int, stage apps.Stage, curState apps.AppState) error {
	insertAppHistoryStatement := `INSERT INTO AppStates(app_name, app_key, stage, state, timestamp) VALUES (?, ?, ?, ?, ?)`
	insertStatement, err := sqlite.db.Prepare(insertAppHistoryStatement) // Prepare statement.
	if err != nil {
		return err
	}
	_, err = insertStatement.Exec(appName, appKey, stage, curState, time.Now().Format(time.RFC3339))
	if err != nil {
		return err
	}

	return nil
}

func (sqlite *SQLite) UpdateDeviceStatus(status DeviceStatus) error {
	return sqlite.updateDeviceState(status, "")
}

func (sqlite *SQLite) UpdateNetworkInterface(intf NetworkInterface) error {
	return sqlite.updateDeviceState("", intf)
}

func (sqlite *SQLite) updateDeviceState(newStatus DeviceStatus, newInt NetworkInterface) error {
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

func (sqlite *SQLite) InsertDeviceState(state DeviceStatus, intf NetworkInterface) error {
	insertAppHistoryStatement := `INSERT INTO DeviceStates(interface_type, device_status, timestamp) VALUES (?, ?, ?)`
	insertStatement, err := sqlite.db.Prepare(insertAppHistoryStatement) // Prepare statement.
	if err != nil {
		return err
	}
	_, err = insertStatement.Exec(intf, state, time.Now().Format(time.RFC3339))
	if err != nil {
		return err
	}
	return nil
}

func (sqlite *SQLite) executeFromFile(filePath string) error {
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
