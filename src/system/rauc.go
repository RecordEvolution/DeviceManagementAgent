package system

import (
	"errors"
	"fmt"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/rs/zerolog/log"
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

// NewRaucDBus initializes a new SystemBus and a DBus Object for RAUC
func NewRaucDBus() (raucDBus, error) {

	raucbus := raucDBus{}

	var err error
	raucbus.conn, err = dbus.SystemBus()
	if err != nil {
		return raucDBus{}, err
	}

	// https://github.com/godbus/dbus/blob/v4.1.0/conn.go#L419
	raucbus.object = raucbus.conn.Object(raucDBusInterface, raucDBusObjectPath)

	// subscribe DBus object to "Complete" signal
	raucbus.conn.AddMatchSignal(
		dbus.WithMatchInterface(raucDBusMethodBase),
		dbus.WithMatchMember("Completed"),
		dbus.WithMatchObjectPath(raucbus.object.Path()))

	return raucbus, nil
}

// ------------------------------------------------------------------------- //
// methods

func raucInstallBundle(bundlePath string, progressCallback func(operationName string, progressPercent uint64)) (err error) {

	log.Debug().Msg("raucInstallBundle: " + bundlePath)

	// get DBus instance connected to RAUC daemon
	raucbus, err := NewRaucDBus()
	if err != nil {
		return errors.New("failed to set up new RAUC DBus instance")
	}
	defer raucbus.conn.Close()

	// https://github.com/godbus/dbus/blob/v4.1.0/conn.go#L560
	completedChannel := make(chan *dbus.Signal, 60)
	raucbus.conn.Signal(completedChannel)
	log.Debug().Msg("raucInstallBundle: " + "setup Channel")

	opts := map[string]interface{}{"ignore-compatible": false}
	call := raucbus.object.Call(raucDBusMethodInstall, 0, bundlePath, opts)
	log.Debug().Msg("raucInstallBundle: " + "launched install...")

	// https://pkg.go.dev/github.com/godbus/dbus#Call
	if call.Err != nil {
		fmt.Printf(call.Err.Error() + "\n")
		return errors.New("D-Bus call to " + raucDBusMethodInstall + " failed: " + call.Err.Error())
	}

	// launch lightweight thread to track/publish progress of bundle installation
	go raucReportProgress(&raucbus, progressCallback)

	// wait for complete signal while reading from channel
	for {
		signal, ok := <-completedChannel
		if !ok {
			return errors.New("could not retrieve channel from RAUC DBus")
		}
		log.Debug().Msg("raucInstallBundle: " + "retrieved Channel" + "signal.Name " + signal.Name)

		// check for error code and evtl. retrieve LastError
		var code int32
		err = dbus.Store(signal.Body, &code)
		if err != nil {
			return err
		}
		if code != 0 {
			errorString, err := raucGetLastError(&raucbus)
			if err != nil {
				return err
			}

			return errors.New(errorString)
		}

		// got signal
		if signal.Name == raucDBusSignalFinish {
			log.Debug().Msg("raucInstallBundle: " + "got final signal " + signal.Name)
			return nil
		}
	}

	return nil
}

func raucReportProgress(raucbus *raucDBus, progressCallback func(operationName string, progressPercent uint64)) {

	var completed uint64 = 0
	for completed < 100 {
		pctng, mssg, _, err := raucGetProgress(raucbus)
		completed = uint64(pctng)
		if err != nil {
			log.Debug().Msg("raucInstallBundle: raucReportProgress: " + err.Error())
		}
		progressCallback(mssg, uint64(pctng))
		//log.Debug().Msg("raucInstallBundle: raucReportProgress " + fmt.Sprintf("%s - %s - %s",pctng,mssg,nstng) )
		time.Sleep(200 * time.Millisecond)
	}
	log.Debug().Msg("raucInstallBundle: raucReportProgress: " + "done")
}

// ------------------------------------------------------------------------- //
// properties

func raucGetOperation(raucbus *raucDBus) (operation string, err error) {

	// get DBus instance connected to RAUC daemon
	//raucbus, err := NewRaucDBus()
	//if err != nil {
	//	return "", errors.New("failed to set up new RAUC DBus instance")
	//}

	oper, err := raucbus.object.GetProperty(raucDBusPropertyOperation)
	if err != nil {
		return "", err
	}

	return oper.String(), nil
}

func raucGetLastError(raucbus *raucDBus) (errormessage string, err error) {

	// get DBus instance connected to RAUC daemon
	//raucbus, err := NewRaucDBus()
	//if err != nil {
	//	return "", errors.New("failed to set up new RAUC DBus instance")
	//}

	variant, err := raucbus.object.GetProperty(raucDBusPropertyLastError)
	if err != nil {
		return "", err
	}

	return variant.String(), nil
}

func raucGetProgress(raucbus *raucDBus) (percentage int32, message string, nestingDepth int32, err error) {

	// get DBus instance connected to RAUC daemon
	//raucbus, err := NewRaucDBus()
	//if err != nil {
	//	return 0, "", 0, errors.New("failed to set up new RAUC DBus instance")
	//}

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
		return 0, "", 0, fmt.Errorf("RAUC: failed to store result of GetPropertyProgress: %v", err)
	}

	return response.Percentage, response.Message, response.NestingDepth, nil
}

// ------------------------------------------------------------------------- //
