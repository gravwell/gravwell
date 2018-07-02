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
	"fmt"
	"net"
	"os"
	"sort"
	"strings"

	"github.com/gravwell/ingest"
	"github.com/gravwell/ingest/config"

	"collectd.org/network"
	"gopkg.in/gcfg.v1"
)

const (
	MAX_CONFIG_SIZE int64                 = (1024 * 1024 * 2) //2MB, even this is crazy large
	defBindPort     uint16                = 25826
	defSecLevel     string                = `encrypt`
	defSecLevelVal  network.SecurityLevel = network.Encrypt
	defUser         string                = `user`
	defPass         string                = `secret`
)

const (
	jsonEncode encType = iota
	bsonEncode encType = iota
)

type encType int

type collector struct {
	Bind_String         string //IP port pair 127.0.0.1:1234
	Tag_Name            string
	Security_Level      string
	User                string
	Password            string
	Tag_Plugin_Override []string
	Encoder             string
}

type cfgReadType struct {
	Global    config.IngestConfig
	Collector map[string]*collector
}

type cfgType struct {
	config.IngestConfig
	Collector map[string]*collector
}

func GetConfig(path string) (*cfgType, error) {
	var content []byte
	fin, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	fi, err := fin.Stat()
	if err != nil {
		fin.Close()
		return nil, err
	}
	//This is just a sanity check
	if fi.Size() > MAX_CONFIG_SIZE {
		fin.Close()
		return nil, errors.New("Config File Far too large")
	}
	content = make([]byte, fi.Size())
	n, err := fin.Read(content)
	fin.Close()
	if int64(n) != fi.Size() {
		return nil, errors.New("Failed to read config file")
	}
	//read into the intermediary type to maintain backwards compatibility with the old system
	var cr cfgReadType
	if err := gcfg.ReadStringInto(&cr, string(content)); err != nil {
		return nil, err
	}
	c := &cfgType{
		IngestConfig: cr.Global,
		Collector:    cr.Collector,
	}

	if err := verifyConfig(c); err != nil {
		return nil, err
	}
	return c, nil
}

func verifyConfig(c *cfgType) error {
	//verify the global parameters
	if err := c.Verify(); err != nil {
		return err
	}
	if len(c.Collector) == 0 {
		return errors.New("No collectors specified")
	}
	bindMp := make(map[string]string, 1)
	for k, v := range c.Collector {
		if len(v.Bind_String) == 0 {
			v.Bind_String = config.AppendDefaultPort(`0.0.0.0`, defBindPort)
			return errors.New("No Bind-String provided for " + k)
		}
		if v.Security_Level == `` {
			v.Security_Level = defSecLevel
		}
		if _, err := SecLevelFromString(v.Security_Level); err != nil {
			return err
		}
		if v.User == `` {
			v.User = defUser
		}
		if v.Password == `` {
			v.Password = defPass
		}
		if len(v.Tag_Name) == 0 {
			v.Tag_Name = `default`
		}
		if strings.ContainsAny(v.Tag_Name, ingest.FORBIDDEN_TAG_SET) {
			return errors.New("Invalid characters in the Tag-Name for " + k)
		}
		if n, ok := bindMp[v.Bind_String]; ok {
			return errors.New("Bind-String for " + k + " already in use by " + n)
		}
		bindMp[v.Bind_String] = k
		if _, err := v.getOverrides(); err != nil {
			return err
		}
		if _, err := TranslateEncoder(v.Encoder); err != nil {
			return err
		}
	}
	return nil
}

func (c *cfgType) Tags() ([]string, error) {
	var tags []string
	tagMp := make(map[string]bool, 1)
	for _, v := range c.Collector {
		if len(v.Tag_Name) == 0 {
			continue
		}
		if _, ok := tagMp[v.Tag_Name]; !ok {
			tags = append(tags, v.Tag_Name)
			tagMp[v.Tag_Name] = true
		}
		ovr, err := v.getOverrides()
		if err != nil {
			return nil, err
		}
		for _, v := range ovr {
			if _, ok := tagMp[v]; !ok {
				tags = append(tags, v)
				tagMp[v] = true
			}
		}
	}
	if len(tags) == 0 {
		return nil, errors.New("No tags specified")
	}
	sort.Strings(tags)
	return tags, nil
}

func (c collector) getOverrides() (map[string]string, error) {
	mp := make(map[string]string, len(c.Tag_Plugin_Override))
	if len(c.Tag_Plugin_Override) == 0 {
		return nil, nil
	}
	for _, v := range c.Tag_Plugin_Override {
		//split the tags
		bits := strings.Split(v, ":")
		if len(bits) != 2 {
			return nil, fmt.Errorf("%s is an invalid tag override", v)
		}
		pluginName := strings.TrimSpace(bits[0])
		tagName := strings.TrimSpace(bits[1])
		if xx, ok := mp[tagName]; ok {
			return nil, fmt.Errorf("Tag-Plugin-Override plugin %s is already assigned tag %s", pluginName, xx)
		}
		if strings.ContainsAny(tagName, ingest.FORBIDDEN_TAG_SET) {
			return nil, errors.New("Invalid characters in the tag override " + tagName)
		}
		mp[pluginName] = tagName
	}
	return mp, nil
}

func (c collector) udpAddr() (addr *net.UDPAddr, err error) {
	s := c.Bind_String
	if len(s) == 0 {
		s = `0.0.0.0`
	}
	s = config.AppendDefaultPort(s, defBindPort)
	addr, err = net.ResolveUDPAddr(`udp`, s)
	return
}

func (c collector) creds() (pl passlookup, seclevel network.SecurityLevel) {
	//username
	if c.User != `` {
		pl.user = c.User
	} else {
		pl.user = defUser
	}
	//password
	if c.Password != `` {
		pl.pass = c.Password
	} else {
		pl.pass = defPass
	}
	var err error
	if seclevel, err = SecLevelFromString(c.Security_Level); err != nil {
		seclevel = defSecLevelVal
	}
	return
}

func SecLevelFromString(s string) (network.SecurityLevel, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case ``:
		fallthrough
	case `none`:
		return network.None, nil
	case `sign`:
		return network.Sign, nil
	case `encrypt`:
		return network.Encrypt, nil
	default:
	}
	return 0, errors.New(s + " is an invalid security level")
}

func TranslateEncoder(v string) (t encType, err error) {
	v = strings.ToLower(strings.TrimSpace(v))
	switch v {
	case ``:
		fallthrough
	case `json`:
		t = jsonEncode
	case `bson`:
		t = bsonEncode
	default:
		err = errors.New("Unknown encoder")
	}
	return
}

func (e encType) String() string {
	switch e {
	case jsonEncode:
		return `JSON`
	case bsonEncode:
		return `BSON`
	}
	return `UNKNOWN`
}

type passlookup struct {
	user string
	pass string
}

func (pl passlookup) Password(user string) (string, error) {
	if user == pl.user {
		return pl.pass, nil
	}
	return ``, errors.New("Invalid user/password")
}
