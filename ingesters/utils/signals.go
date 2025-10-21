// -build windows
/*************************************************************************
 * Copyright 2019 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package utils

import (
	"os"
	"os/signal"
	"syscall"
)

// WaitForQuit waits until it receives one of the following signals:
// SIGINT, SIGQUIT, SIGTERM
// It returns the received signal.
func WaitForQuit() (r os.Signal) {
	quitSig := make(chan os.Signal, 1)
	defer close(quitSig)
	signal.Notify(quitSig, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM)
	r = <-quitSig
	signal.Stop(quitSig)
	return
}

// GetQuitChannel registers and returns a channel that will be notified upon receipt of the following signals:
// SIGINT, SIGQUIT, SIGTERM
func GetQuitChannel() chan os.Signal {
	quitSig := make(chan os.Signal, 1)
	signal.Notify(quitSig, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM)
	return quitSig
}

// GetSighupChannel registers and returns a channel that will be notified of SIGHUP signals.  This is used
// for signaling that we need to reload configurations.
func GetSighupChannel() chan os.Signal {
	quitSig := make(chan os.Signal, 1)
	signal.Notify(quitSig, syscall.SIGHUP)
	return quitSig
}
