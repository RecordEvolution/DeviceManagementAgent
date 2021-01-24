package persistence

import (
	"database/sql"
	"io/ioutil"
	"path/filepath"
	"runtime"
	"strings"

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
	return sqlite.ExecuteFromFile(basepath + "/sql/init-script.sql")
}

func (sqlite *SQLite) ExecuteFromFile(filePath string) error {
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
