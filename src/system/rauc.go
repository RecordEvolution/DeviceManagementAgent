package system

import (
  "fmt"
  "errors"
	"github.com/godbus/dbus/v5"
)

// usage of D-Bus API recommended!!
// $ busctl call de.pengutronix.rauc / de.pengutronix.rauc.Installer InstallBundle sa{sv} /tmp/ReswarmOS-0.3.8-raspberrypi3.raucb 0
// $ busctl get-property de.pengutronix.rauc / de.pengutronix.rauc.Installer Operation
// $ busctl get-property de.pengutronix.rauc / de.pengutronix.rauc.Installer Progress
// $ busctl get-property de.pengutronix.rauc / de.pengutronix.rauc.Installer LastError
// $ busctl monitor de.pengutronix.rauc

// for reference, see:
// - https://rauc.readthedocs.io/en/latest/using.html#using-the-d-bus-api
// - https://rauc.readthedocs.io/en/latest/reference.html#gdbus-method-de-pengutronix-rauc-installer-installbundle
// - https://github.com/godbus/dbus/tree/master/_examples
// - https://pkg.go.dev/github.com/godbus/dbus#SystemBus
// - https://github.com/holoplot/go-rauc/blob/master/installer.go

const (
  raucDBusInterface = "de.pengutronix.rauc"
  raucDBusObjectPath = "/"
)

type raucDBus struct {
	conn   *dbus.Conn
	object dbus.BusObject
}

// NewRaucDBus initializes a new SystemBus and an DBus Object for RAUC
func NewRaucDBus() (raucDBus, error) {
  raucbus := raucDBus{}
  var err error
  raucbus.conn, err = dbus.SystemBus()
  if err != nil {
		return raucDBus{}, err
	}
  raucbus.object = raucbus.conn.Object(raucDBusInterface,raucDBusObjectPath)
  return raucbus, nil
}

func raucInstallBundle(bundlePath string, progressCallback func(percent uint64)) (error) {

  // get DBus instance connected to RAUC daemon
  raucbus, err := NewRaucDBus()
  if err != nil {
    return errors.New("failed to set up new RAUC DBus instance")
  }
  method := fmt.Sprintf("%s.%s.%s",raucDBusInterface,"Installer","InstallBundle")
  call := raucbus.object.Call(method,0,bundlePath)
  // https://pkg.go.dev/github.com/godbus/dbus#Call
  if call.Err != nil {
    fmt.Printf(call.Err.Error()+"\n")
    return errors.New("D-Bus call to " + raucDBusInterface + ".Installer for Install Bundle: "+call.Err.Error())
  }

  return nil
}
