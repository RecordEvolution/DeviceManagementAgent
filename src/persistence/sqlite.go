package persistence

import (
	"database/sql"
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

func New() (*SQLite, error) {
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

func (sqlite *SQLite) UpdateAppState(appName string, appKey int, stage apps.Stage, oldState apps.AppState, newState apps.AppState) error {
	// First add new entry in history
	insertAppHistoryStatement := `INSERT INTO AppStateHistory(app_name, app_key, stage, state, timestamp) VALUES (?, ?, ?, ?, ?)`
	insertStatement, err := sqlite.db.Prepare(insertAppHistoryStatement) // Prepare statement.
	if err != nil {
		return err
	}
	_, err = insertStatement.Exec(appName, appKey, stage, oldState, time.Now().Format(time.RFC3339))
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

func (sqlite *SQLite) InsertAppState(appName string, appKey, stage apps.Stage, curState apps.AppState) error {
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

func (sqlite *SQLite) UpdateDeviceState(oldState DeviceState, newState DeviceState, oldInt NetworkInterface, newInt NetworkInterface) error {
	// Add new entry in history
	insertAppHistoryStatement := `INSERT INTO DeviceStateHistory(interface_type, device_status, timestamp) VALUES (?, ?, ?)`
	insertStatement, err := sqlite.db.Prepare(insertAppHistoryStatement) // Prepare statement.
	if err != nil {
		return err
	}
	_, err = insertStatement.Exec(oldInt, oldState, time.Now().Format(time.RFC3339))
	if err != nil {
		return err
	}

	// Update current state
	updateAppStatement := `UPDATE DeviceStates SET device_status = ?, interface_type = ?`
	updateStatement, err := sqlite.db.Prepare(updateAppStatement) // Prepare statement.
	if err != nil {
		return err
	}
	_, err = updateStatement.Exec(newState, newInt)
	if err != nil {
		return err
	}
	return nil
}

func (sqlite *SQLite) InsertDeviceState(state DeviceState, intf NetworkInterface) error {
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
