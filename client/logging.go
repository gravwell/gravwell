/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package client

import (
	"fmt"
)

type logMsg struct {
	Body string
}

// AccessLogF submits a log message to the webserver at the Access log level.
func (c *Client) AccessLogF(format string, a ...interface{}) error {
	s := fmt.Sprintf(format, a...)
	msg := logMsg{Body: s}
	return c.postStaticURL(loggingAccessUrl(), msg, nil)
}

// InfoLogF submits a log message to the webserver at the Info log level.
func (c *Client) InfoLogF(format string, a ...interface{}) error {
	s := fmt.Sprintf(format, a...)
	msg := logMsg{Body: s}
	return c.postStaticURL(loggingInfoUrl(), msg, nil)
}

// WarnLogF submits a log message to the webserver at the Warn log level.
func (c *Client) WarnLogF(format string, a ...interface{}) error {
	s := fmt.Sprintf(format, a...)
	msg := logMsg{Body: s}
	return c.postStaticURL(loggingWarnUrl(), msg, nil)
}

// ErrorLogF submits a log message to the webserver at the Error log level.
func (c *Client) ErrorLogF(format string, a ...interface{}) error {
	s := fmt.Sprintf(format, a...)
	msg := logMsg{Body: s}
	return c.postStaticURL(loggingErrorUrl(), msg, nil)
}
