package system

import (
	"errors"
	"fmt"

	"github.com/godbus/dbus/v5"
)

// usage of D-Bus API recommended!!
// $ busctl call de.pengutronix.rauc / de.pengutronix.rauc.Installer InstallBundle sa{sv} /tmp/ReswarmOS-0.3.8-raspberrypi3.raucb 0
// $ busctl emit / de.pengutronix.rauc Completed
// $ busctl get-property de.pengutronix.rauc / de.pengutronix.rauc.Installer LastError
// $ busctl monitor de.pengutronix.rauc

// for reference, see:
// - https://rauc.readthedocs.io/en/latest/using.html#using-the-d-bus-api
// - https://rauc.readthedocs.io/en/latest/reference.html#gdbus-method-de-pengutronix-rauc-installer-installbundle
// - https://github.com/godbus/dbus/tree/master/_examples
// - https://pkg.go.dev/github.com/godbus/dbus#SystemBus
// - https://github.com/holoplot/go-rauc/blob/master/installer.go

const (
	raucDBusInterface  = "de.pengutronix.rauc"
	raucDBusObjectPath = "/"
)

const (
	raucDBusMethodBase = raucDBusInterface + ".Installer"

	raucDBusMethodInstall = raucDBusMethodBase + ".InstallBundle"
	raucDBusMethodInfo    = raucDBusMethodBase + ".Info"
	raucDBusMethodMark    = raucDBusMethodBase + ".Mark"
	raucDBusMethodSlot    = raucDBusMethodBase + ".GetSlotStatus"
	raucDBusMethodPrimary = raucDBusMethodBase + ".GetPrimary"

	raucDBusSignalFinish = raucDBusMethodBase + ".Completed"

	raucDBusPropertyOperation  = raucDBusMethodBase + ".Operation"
	raucDBusPropertyLastError  = raucDBusMethodBase + ".LastError"
	raucDBusPropertyProgress   = raucDBusMethodBase + ".Progress"
	raucDBusPropertyCompatible = raucDBusMethodBase + ".Compatible"
	raucDBusPropertyVariant    = raucDBusMethodBase + ".Variant"
	raucDBusPropertyBootSlot   = raucDBusMethodBase + ".BootSlot"
)

type raucDBus struct {
	conn   *dbus.Conn
	object dbus.BusObject
}

// ------------------------------------------------------------------------- //

// NewRaucDBus initializes a new SystemBus and an DBus Object for RAUC
func NewRaucDBus() (raucDBus, error) {

	raucbus := raucDBus{}

	var err error
	raucbus.conn, err = dbus.SystemBus()
	if err != nil {
		return raucDBus{}, err
	}

	// https://github.com/godbus/dbus/blob/v4.1.0/conn.go#L419
	raucbus.object = raucbus.conn.Object(raucDBusInterface, raucDBusObjectPath)

	return raucbus, nil
}

// ------------------------------------------------------------------------- //
// methods

func raucInstallBundle(bundlePath string) (err error) {

	// get DBus instance connected to RAUC daemon
	raucbus, err := NewRaucDBus()
	if err != nil {
		return errors.New("failed to set up new RAUC DBus instance")
	}

	opts := map[string]interface{}{"ignore-compatible": false}
	call := raucbus.object.Call(raucDBusMethodInstall, 0, bundlePath, opts)

	// https://pkg.go.dev/github.com/godbus/dbus#Call
	if call.Err != nil {
		fmt.Printf(call.Err.Error() + "\n")
		return errors.New("D-Bus call to " + raucDBusMethodInstall + " failed: " + call.Err.Error())
	}

	return nil
}

// ------------------------------------------------------------------------- //
// signals

func raucGetSignalCompleted() (completed bool, err error) {

	// get DBus instance connected to RAUC daemon
	raucbus, err := NewRaucDBus()
	if err != nil {
		return false, errors.New("failed to set up new RAUC DBus instance")
	}

	// https://github.com/godbus/dbus/blob/v4.1.0/conn.go#L560
	completedChannel := make(chan *dbus.Signal, 60)
	raucbus.conn.Signal(completedChannel)

	signal, ok := <-completedChannel
	if !ok {
		return false, errors.New("D-Bus RAUC: failed to read from channel")
	}

	// https://github.com/godbus/dbus/blob/a389bdde4dd695d414e47b755e95e72b7826432c/conn.go#L608
	if signal.Name == raucDBusSignalFinish {
		return true, nil
	} else {
		return false, nil
	}
}

// ------------------------------------------------------------------------- //
// properties

func raucGetOperation() (operation string, err error) {

	// get DBus instance connected to RAUC daemon
	raucbus, err := NewRaucDBus()
	if err != nil {
		return "", errors.New("failed to set up new RAUC DBus instance")
	}

	variant, err := raucbus.object.GetProperty(raucDBusPropertyOperation)
	if err != nil {
		return "", err
	}

	return variant.String(), nil
}

func raucGetLastError() (errormessage string, err error) {

	// get DBus instance connected to RAUC daemon
	raucbus, err := NewRaucDBus()
	if err != nil {
		return "", errors.New("failed to set up new RAUC DBus instance")
	}

	variant, err := raucbus.object.GetProperty(raucDBusPropertyLastError)
	if err != nil {
		return "", err
	}

	return variant.String(), nil
}

func raucGetProgress() (percentage int32, message string, nestingDepth int32, err error) {

	// get DBus instance connected to RAUC daemon
	raucbus, err := NewRaucDBus()
	if err != nil {
		return 0, "", 0, errors.New("failed to set up new RAUC DBus instance")
	}

	// call the DBus
	variant, err := raucbus.object.GetProperty(raucDBusPropertyProgress)
	if err != nil {
		return 0, "", 0, fmt.Errorf("RAUC: failed to GetPropertyProgress: %v", err)
	}

	// process response
	type progressResponse struct {
		Percentage   int32
		Message      string
		NestingDepth int32
	}

	src := make([]interface{}, 1)
	src[0] = variant.Value()

	var response progressResponse
	err = dbus.Store(src, &response)
	if err != nil {
		return 0, "", 0, fmt.Errorf("RAUC: failed store result of GetPropertyProgress: %v", err)
	}

	return response.Percentage, response.Message, response.NestingDepth, nil
}

// ------------------------------------------------------------------------- //
