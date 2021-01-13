package logging

import (
	"io"
	"time"
	// "log"
	// "github.com/sirupsen/logrus"
	// "github.com/golang/glog"
)

// define log levels
type LogLevel int

const (
	DEBUG    LogLevel = iota // 0
	INFO                     // 1
	WARNING                  // 2
	ERROR                    // 3
	CRITICAL                 // 4
)

func (ll LogLevel) string() string {
	return [...]string{"DEBUG", "INFO", "WARNING", "ERROR", "CRITICAL"}[ll]
}

func GetLogLevel(logArg string) LogLevel {
	var ll LogLevel
	switch logArg {
	case "DEBUG":
		ll = DEBUG
	case "INFO":
		ll = INFO
	case "WARNING":
		ll = WARNING
	case "ERROR":
		ll = ERROR
	case "CRITICAL":
		ll = CRITICAL
	default:
		panic("invalid log level " + logArg)
	}
	return ll
}

// declare standard logging type
type DefaultLogger struct {

	// standard library log package (https://golang.org/pkg/log/#Logger)
	// *log.Logger

	// github.com/sirupsen/logrus
	// *logrus.Logger

	// custom logger
	outTgt io.Writer
	logLvl LogLevel
}

// NewLogger creates a new logger instance to be adjusted and used as logging tool
func NewLogger(out io.Writer, level LogLevel) *DefaultLogger {

	// standard library logger
	// var theLogger = log.New(out, "ReAgent: ", log.Ldate|log.Ltime|log.Lshortfile)
	// var defLogger = &DefaultLogger{theLogger}

	// custom logger
	var defLogger = &DefaultLogger{out, level}

	return defLogger
}

func (L *DefaultLogger) DoLog(logLevel LogLevel, logmessage string) {

	if logLevel >= L.logLvl {
		// get formatted timestamp
		logts := time.Now()
		logtsstr := logts.Format(time.RFC3339Nano)

		io.WriteString(L.outTgt, ""+logLevel.string()+" "+"["+logtsstr+"] "+logmessage)
		io.WriteString(L.outTgt, "\n")
	}
}
