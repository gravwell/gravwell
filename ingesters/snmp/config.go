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
	"strings"

	"github.com/gosnmp/gosnmp"
	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/attach"
	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/processors"
)

type listener struct {
	v3auth
	Tag_Name        string
	Bind_String     string //IP port pair 127.0.0.1:1234
	Version         string // SNMP version: 1, 2c, 3
	Community       string `json:"-"` // for SNMP v1 and v2, DO NOT send this when marshalling
	Source_Override string
	Preprocessor    []string
}

type v3auth struct {
	Username           string
	Privacy_Passphrase string `json:"-"` // DO NOT send this when marshalling
	Privacy_Protocol   string
	Auth_Passphrase    string `json:"-"` // DO NOT send this when marshalling
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

// authProtocols maps the config file names for SNMPv3 authentication
// protocols onto the gosnmp values.  Keys must be normalized, see normProto.
var authProtocols = map[string]gosnmp.SnmpV3AuthProtocol{
	`MD5`:    gosnmp.MD5,
	`SHA`:    gosnmp.SHA,
	`SHA1`:   gosnmp.SHA, // alias for SHA
	`SHA224`: gosnmp.SHA224,
	`SHA256`: gosnmp.SHA256,
	`SHA384`: gosnmp.SHA384,
	`SHA512`: gosnmp.SHA512,
}

// privacyProtocols maps the config file names for SNMPv3 privacy protocols
// onto the gosnmp values.  Keys must be normalized, see normProto.
//
// AES is AES-128-CFB as specified by RFC 3826.  The 192 and 256 bit variants
// are not standardized and come in two mutually incompatible flavors that
// differ in how the localized key is extended: the Blumenthal draft (AES192
// and AES256) and the Reeder draft (AES192C and AES256C).  They are not
// interchangeable, so both are exposed and the agent being listened to
// dictates which one is correct.
var privacyProtocols = map[string]gosnmp.SnmpV3PrivProtocol{
	`DES`:     gosnmp.DES,
	`AES`:     gosnmp.AES,
	`AES128`:  gosnmp.AES, // alias for AES
	`AES192`:  gosnmp.AES192,
	`AES256`:  gosnmp.AES256,
	`AES192C`: gosnmp.AES192C,
	`AES256C`: gosnmp.AES256C,
}

// normProto normalizes a protocol name from the config file so that we accept
// the obvious spellings, "aes-256-c" and "AES256C" are the same thing.
func normProto(v string) string {
	v = strings.ToUpper(strings.TrimSpace(v))
	v = strings.ReplaceAll(v, `-`, ``)
	v = strings.ReplaceAll(v, `_`, ``)
	return v
}

// protoNames returns the sorted set of names accepted for a protocol map, it
// is only used to build error messages.
func protoNames[T comparable](mp map[string]T) string {
	names := make([]string, 0, len(mp))
	for k := range mp {
		names = append(names, k)
	}
	sort.Strings(names)
	return strings.Join(names, `, `)
}

// validate checks the v3 security settings.  v3 reports whether the listener
// actually speaks SNMPv3; the protocol names are checked either way so that a
// typo is still caught, but rules about which combinations make sense only
// apply where they can have an effect.
func (a *v3auth) validate(v3 bool) error {
	if v := normProto(a.Auth_Protocol); v != `` {
		if _, ok := authProtocols[v]; !ok {
			return fmt.Errorf("Invalid Auth-Protocol %v. Supported protocols: %s",
				a.Auth_Protocol, protoNames(authProtocols))
		}
	}
	if v := normProto(a.Privacy_Protocol); v != `` {
		if _, ok := privacyProtocols[v]; !ok {
			return fmt.Errorf("Invalid Privacy-Protocol %v. Supported protocols: %s",
				a.Privacy_Protocol, protoNames(privacyProtocols))
		}
		// SNMPv3 has no privacy without authentication, catch it here rather
		// than silently failing to decrypt every trap that shows up.  gosnmp
		// treats NoAuth as MD5 when it localizes the privacy key, so this
		// otherwise comes out as traps that never decrypt.
		if v3 && normProto(a.Auth_Protocol) == `` {
			return errors.New("Privacy-Protocol requires an Auth-Protocol")
		}
	}
	return nil
}

func (a *v3auth) getAuthProto() gosnmp.SnmpV3AuthProtocol {
	if p, ok := authProtocols[normProto(a.Auth_Protocol)]; ok {
		return p
	}
	return gosnmp.NoAuth
}

func (a *v3auth) getPrivacyProto() gosnmp.SnmpV3PrivProtocol {
	if p, ok := privacyProtocols[normProto(a.Privacy_Protocol)]; ok {
		return p
	}
	return gosnmp.NoPriv
}

func (a *v3auth) getMsgFlags() gosnmp.SnmpV3MsgFlags {
	if a.getAuthProto() != gosnmp.NoAuth {
		if a.getPrivacyProto() != gosnmp.NoPriv {
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

		if err := v.v3auth.validate(v.Version == `3`); err != nil {
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
