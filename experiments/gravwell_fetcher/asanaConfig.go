/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"errors"
	"fmt"
	"time"
)

const (
	defaultRequestsPerMin = "6"
	asanaDomain           = "https://app.asana.com"
	asanaURL              = "/api/1.0/workspaces/%v/audit_log_events?start_at=%v"
	asanaEmptySleepDur    = 15 * time.Second
)

type asanaConf struct {
	StartTime    time.Time
	Token        string
	Workspace    string
	Tag_Name     string
	Preprocessor []string
	RateLimit    int
}

func (c cfgType) AsanaVerify() error {

	for k, v := range c.AsanaConf {
		if v == nil {
			return fmt.Errorf("Asana Conf %v config is nil", k)
		}
		if err := c.Preprocessor.Validate(); err != nil {
			return err
		}
		if v.Token == "" {
			return errors.New("Token not specified")
		}
		if v.Workspace == "" {
			return errors.New("Workspace not specified")
		}
		if v.Tag_Name == "" {
			return errors.New("Tag-Name not specified")
		}
		if err := c.Preprocessor.CheckProcessors(v.Preprocessor); err != nil {
			return fmt.Errorf("Listener %s preprocessor invalid: %v", k, err)
		}
		if v.StartTime.IsZero() {
			v.StartTime = time.Now()
		}
	}
	return nil
}
