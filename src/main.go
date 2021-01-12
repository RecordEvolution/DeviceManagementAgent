package main

import (
  "fmt"
  "time"
  "log"
  "os"
  "flag"
  "strconv"
)

func main() {

  // process CLI args
  // argsCLI := os.Args[1:]

  // define and parse CLI flags
  logFile := flag.String("logfile","/var/log/reagent.log",
                         "Log file used by the Reagent to store all its log messages")
  logFlag := flag.Bool(  "logflag",true,
                         "Reagent logs to stdout/stderr (false) or given file (true)")
  cfgFile := flag.String("cfgfile","device-config.reswarm",
                         "Configuration file of IoT device running on localhost")
  flag.Parse()

  startts := time.Now()
  fmt.Println( "["   + startts.Format(time.RFC3339Nano)  + "] starting Reagent"         +
               " - " + "logFile: "     + (*logFile)                   +
               " - " + "logFlag: "     + strconv.FormatBool(*logFlag) +
               " - " + "cfgFile: "     + (*cfgFile)                      )

  // check whether to log to stdout/stderr or given log file
  if *logFlag {

    //  open central log file
    logfile, err := os.OpenFile(*logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
    if err != nil {
      log.Fatal(err)
    }

    // redirect all logging to file
    log.SetOutput(logfile)
  }

  log.Println("starting Reagent")
}
