/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"errors"
	"net"
	"strings"
	"time"

	"github.com/gravwell/gravwell/v4/ingest"
	"github.com/gravwell/gravwell/v4/ingest/config"
	"github.com/gravwell/gravwell/v4/ingest/entry"
)

const (
	maxConfigSize  int64 = (1024 * 1024 * 2) //2MB, even this is crazy large
	maxSnapLen     int   = 0xffff
	defaultSnapLen int   = 96
)

type cfgType struct {
	Global struct {
		Ingest_Secret              string `json:"-"` // DO NOT send this when marshalling
		Connection_Timeout         string
		Verify_Remote_Certificates bool
		Cleartext_Backend_Target   []string
		Encrypted_Backend_Target   []string
		Pipe_Backend_Target        []string
	}
	Sniffer map[string]*struct {
		Interface       string //interface name to bind to
		Tag_Name        string
		Source_Override string
	}
}

func GetConfig(path, overlayPath string) (*cfgType, error) {
	var cr cfgType
	if err := config.LoadConfigFile(&cr, path); err != nil {
		return nil, err
	} else if err = config.LoadConfigOverlays(&cr, overlayPath); err != nil {
		return nil, err
	}
	if err := verifyConfig(&cr); err != nil {
		return nil, err
	}
	return &cr, nil
}

func verifyConfig(c *cfgType) error {
	if to, err := c.parseTimeout(); err != nil || to < 0 {
		if err != nil {
			return err
		}
		return errors.New("Invalid connection timeout")
	}
	if c.Global.Ingest_Secret == "" {
		return errors.New("Ingest-Secret not specified")
	}
	//ensure there is at least one target
	connCount := len(c.Global.Cleartext_Backend_Target) +
		len(c.Global.Encrypted_Backend_Target) +
		len(c.Global.Pipe_Backend_Target)
	if connCount == 0 {
		return errors.New("No backend targets specified")
	}
	if len(c.Sniffer) == 0 {
		return errors.New("No Sniffers specified")
	}
	for k, v := range c.Sniffer {
		if len(v.Interface) == 0 {
			return errors.New("No Inteface provided for " + k)
		}
		if len(v.Tag_Name) == 0 {
			v.Tag_Name = entry.DefaultTagName
		}
		if ingest.CheckTag(v.Tag_Name) != nil {
			return errors.New("Invalid characters in the \"" + v.Tag_Name + "\"Tag-Name for " + k)
		}
		if v.Source_Override != `` {
			if net.ParseIP(v.Source_Override) == nil {
				return errors.New("Failed to parse Source_Override")
			}
		}
	}
	return nil
}

func (c *cfgType) Targets() ([]string, error) {
	var conns []string
	for _, v := range c.Global.Cleartext_Backend_Target {
		conns = append(conns, "tcp://"+v)
	}
	for _, v := range c.Global.Encrypted_Backend_Target {
		conns = append(conns, "tls://"+v)
	}
	for _, v := range c.Global.Pipe_Backend_Target {
		conns = append(conns, "pipe://"+v)
	}
	if len(conns) == 0 {
		return nil, errors.New("no connections specified")
	}
	return conns, nil
}

func (c *cfgType) Tags() ([]string, error) {
	var tags []string
	tagMp := make(map[string]bool, 1)
	for _, v := range c.Sniffer {
		if len(v.Tag_Name) == 0 {
			continue
		}
		if _, ok := tagMp[v.Tag_Name]; !ok {
			tags = append(tags, v.Tag_Name)
			tagMp[v.Tag_Name] = true
		}
	}
	if len(tags) == 0 {
		return nil, errors.New("No tags specified")
	}
	return tags, nil
}

func (c *cfgType) VerifyRemote() bool {
	return c.Global.Verify_Remote_Certificates
}

func (c *cfgType) Timeout() time.Duration {
	if tos, _ := c.parseTimeout(); tos > 0 {
		return tos
	}
	return 0
}

func (c *cfgType) Secret() string {
	return c.Global.Ingest_Secret
}

func (c *cfgType) parseTimeout() (time.Duration, error) {
	tos := strings.TrimSpace(c.Global.Connection_Timeout)
	if len(tos) == 0 {
		return 0, nil
	}
	return time.ParseDuration(tos)
}
