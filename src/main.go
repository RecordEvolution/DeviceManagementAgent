package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	// "agent/container"
	"agent/logging"
)

func main() {

	// process CLI args
	// argsCLI := os.Args[1:]

	// define and parse CLI flags
	logFile := flag.String("logfile", "/var/log/reagent.log",
		"Log file used by the ReAgent to store all its log messages")
	logFlag := flag.Bool("logflag", true,
		"ReAgent logs to stdout/stderr (false) or given file (true)")
	cfgFile := flag.String("cfgfile", "device-config.reswarm",
		"Configuration file of IoT device running on localhost")
	logLevl := flag.String("loglevel", "INFO",
		"Log level is one of DEBUG, INFO, WARNING, ERROR, CRITICAL")
	flag.Parse()

	startts := time.Now()
	cliSummary := ("starting ReAgent" +
		" - " + "cfgFile: " + (*cfgFile) +
		" - " + "logFile: " + (*logFile) +
		" - " + "logFlag: " + strconv.FormatBool(*logFlag) +
		" - " + "logLevl: " + (*logLevl))
	fmt.Println("[" + startts.Format(time.RFC3339Nano) + "] " + cliSummary)

	// initialize logging target
	var (
		logTarget io.Writer
	)
	if *logFlag {
		logfile, err := os.OpenFile(*logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			panic(err)
		}
		defer logfile.Close()
		logTarget = logfile
	} else {
		logTarget = os.Stdout
	}

	// create logging instance(s) with target (stdout vs. file) and log level
	var (
		AgentLogger *logging.DefaultLogger
		// warnLogger *logging.DefaultLogger
		// errorLogger *logging.DefaultLogger
	)
	AgentLogger = logging.NewLogger(logTarget, logging.GetLogLevel(*logLevl))

	// submit first log message
	AgentLogger.DoLog(logging.INFO, cliSummary)

	// check for configuration file
	_, err := os.Stat(*cfgFile)
	if os.IsNotExist(err) {
		AgentLogger.DoLog(logging.ERROR, "configuration file "+(*cfgFile)+" does not exist")
	} else {
		AgentLogger.DoLog(logging.INFO, "using configuration file "+(*cfgFile))
	}

}
