/*************************************************************************
 * Copyright 2020 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"errors"
	"fmt"
	"net"
	"sort"

	"github.com/gosnmp/gosnmp"
	"github.com/gravwell/gravwell/v4/ingest"
	"github.com/gravwell/gravwell/v4/ingest/attach"
	"github.com/gravwell/gravwell/v4/ingest/config"
	"github.com/gravwell/gravwell/v4/ingest/entry"
	"github.com/gravwell/gravwell/v4/ingest/processors"
)

type listener struct {
	v3auth
	Tag_Name        string
	Bind_String     string //IP port pair 127.0.0.1:1234
	Version         string // SNMP version: 1, 2c, 3
	Community       string // for SNMP v1 and v2
	Source_Override string
	Preprocessor    []string
}

type v3auth struct {
	Username           string
	Privacy_Passphrase string
	Privacy_Protocol   string
	Auth_Passphrase    string
	Auth_Protocol      string
}

type global struct {
	config.IngestConfig
}

type cfgReadType struct {
	Global       global
	Attach       attach.AttachConfig
	Listener     map[string]*listener
	Preprocessor processors.ProcessorConfig
}

type cfgType struct {
	config.IngestConfig
	Attach       attach.AttachConfig
	Listener     map[string]*listener
	Preprocessor processors.ProcessorConfig
}

func (a *v3auth) validate() error {
	if a.Auth_Protocol != "" && a.Auth_Protocol != "MD5" && a.Auth_Protocol != "SHA" {
		return fmt.Errorf("Invalid Auth-Protocol %v. Supported protocols: MD5, SHA", a.Auth_Protocol)
	}
	if a.Privacy_Protocol != "" && a.Privacy_Protocol != "DES" {
		return fmt.Errorf("Invalid Privacy-Protocol %v. Supported protocols: DES", a.Auth_Protocol)
	}
	return nil
}

func (a *v3auth) getAuthProto() gosnmp.SnmpV3AuthProtocol {
	switch a.Auth_Protocol {
	case "MD5":
		return gosnmp.MD5
	case "SHA":
		return gosnmp.SHA
	}
	return gosnmp.NoAuth
}

func (a *v3auth) getPrivacyProto() gosnmp.SnmpV3PrivProtocol {
	switch a.Privacy_Protocol {
	case "DES":
		return gosnmp.DES
	}
	return gosnmp.NoPriv
}

func (a *v3auth) getMsgFlags() gosnmp.SnmpV3MsgFlags {
	if a.Auth_Protocol != "" {
		if a.Privacy_Protocol != "" {
			return gosnmp.AuthPriv
		}
		return gosnmp.AuthNoPriv
	}
	return gosnmp.NoAuthNoPriv
}

func GetConfig(path, overlayPath string) (*cfgType, error) {
	//read into the intermediary type to maintain backwards compatibility with the old system
	var cr cfgReadType
	if err := config.LoadConfigFile(&cr, path); err != nil {
		return nil, err
	} else if err = config.LoadConfigOverlays(&cr, overlayPath); err != nil {
		return nil, err
	}
	c := &cfgType{
		IngestConfig: cr.Global.IngestConfig,
		Attach:       cr.Attach,
		Listener:     cr.Listener,
		Preprocessor: cr.Preprocessor,
	}

	if err := c.Verify(); err != nil {
		return nil, err
	}

	return c, nil
}

func (c *cfgType) Verify() error {
	//verify the global parameters
	if err := c.IngestConfig.Verify(); err != nil {
		return err
	} else if err = c.Attach.Verify(); err != nil {
		return err
	}

	if len(c.Listener) == 0 {
		return errors.New("No listeners specified")
	}

	if err := c.Preprocessor.Validate(); err != nil {
		return err
	}

	for k, v := range c.Listener {
		if len(v.Tag_Name) == 0 {
			v.Tag_Name = entry.DefaultTagName
		}
		if ingest.CheckTag(v.Tag_Name) != nil {
			return errors.New("Invalid characters in the Tag-Name for " + k)
		}
		if v.Source_Override != `` {
			if net.ParseIP(v.Source_Override) == nil {
				return fmt.Errorf("Source-Override %s is not a valid IP address", v.Source_Override)
			}
		}
		switch v.Version {
		case "2c":
		case "3":
		default:
			return fmt.Errorf("Listener %v Invalid SNMP version %v, supported versions: 2c, 3", k, v.Version)
		}

		if err := v.v3auth.validate(); err != nil {
			return fmt.Errorf("Listener %s SNMP v3 security config is invalid: %v", k, err)
		}

		if err := c.Preprocessor.CheckProcessors(v.Preprocessor); err != nil {
			return fmt.Errorf("Listener %s preprocessor invalid: %v", k, err)
		}
	}

	return nil
}

func (c *cfgType) Tags() ([]string, error) {
	var tags []string
	tagMp := make(map[string]bool, 1)

	for _, v := range c.Listener {
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
	sort.Strings(tags)
	return tags, nil
}

func (c *cfgType) IngestBaseConfig() config.IngestConfig {
	return c.IngestConfig
}

func (c *cfgType) AttachConfig() attach.AttachConfig {
	return c.Attach
}

func (g *global) Verify() (err error) {
	if err = g.IngestConfig.Verify(); err != nil {
		return
	}
	return
}

func (l *listener) getSnmpVersion() gosnmp.SnmpVersion {
	switch l.Version {
	case "2c":
		return gosnmp.Version2c
	case "3":
		return gosnmp.Version3
	}
	return gosnmp.Version2c
}
