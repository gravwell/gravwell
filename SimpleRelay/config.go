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
	"os"
	"strings"
	"time"

	"github.com/gravwell/ingest"

	"gopkg.in/gcfg.v1"
)

const (
	MAX_CONFIG_SIZE int64    = (1024 * 1024 * 2) //2MB, even this is crazy large
	tcp             bindType = iota
	udp             bindType = iota
	tcp6            bindType = iota
	udp6            bindType = iota

	lineReader    readerType = iota
	rfc5424Reader readerType = iota
)

type bindType int
type readerType int

type cfgType struct {
	Global struct {
		Ingest_Secret              string
		Connection_Timeout         string
		Verify_Remote_Certificates bool
		Cleartext_Backend_Target   []string
		Encrypted_Backend_Target   []string
		Pipe_Backend_Target        []string
		Ingest_Cache_Path          string
		Log_Level                  string
	}
	Listener map[string]*struct {
		Bind_String           string //IP port pair 127.0.0.1:1234
		Tag_Name              string
		Ignore_Timestamps     bool //Just apply the current timestamp to lines as we get them
		Assume_Local_Timezone bool
		Reader_Type           string
	}
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

	var c cfgType
	//SECURITY SHIT!
	//default the Verifiy_Remote_Certificates to true
	c.Global.Verify_Remote_Certificates = true

	if err := gcfg.ReadStringInto(&c, string(content)); err != nil {
		return nil, err
	}
	if err := verifyConfig(c); err != nil {
		return nil, err
	}
	return &c, nil
}

func verifyConfig(c cfgType) error {
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
	if len(c.Listener) == 0 {
		return errors.New("No listeners specified")
	}
	bindMp := make(map[string]string, 1)
	for k, v := range c.Listener {
		if len(v.Bind_String) == 0 {
			return errors.New("No Bind-String provided for " + k)
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

func (c *cfgType) EnableCache() bool {
	return len(c.Global.Ingest_Cache_Path) != 0
}

func (c *cfgType) LocalFileCachePath() string {
	return c.Global.Ingest_Cache_Path
}

func (c *cfgType) LogLevel() string {
	return c.Global.Log_Level
}

func (c *cfgType) parseTimeout() (time.Duration, error) {
	tos := strings.TrimSpace(c.Global.Connection_Timeout)
	if len(tos) == 0 {
		return 0, nil
	}
	return time.ParseDuration(tos)
}

func translateBindType(bstr string) (bindType, string, error) {
	bits := strings.SplitN(bstr, "://", 2)
	//if nothing specified, just return the tcp type
	if len(bits) != 2 {
		return tcp, bstr, nil
	}
	id := strings.ToLower(bits[0])
	switch id {
	case "tcp":
		return tcp, bits[1], nil
	case "udp":
		return udp, bits[1], nil
	case "tcp6":
		return tcp6, bits[1], nil
	case "udp6":
		return udp6, bits[1], nil
	default:
	}
	return -1, "", errors.New("invalid bind protocol specifier of " + id)
}

func (bt bindType) TCP() bool {
	if bt == tcp || bt == tcp6 {
		return true
	}
	return false
}

func (bt bindType) UDP() bool {
	if bt == udp || bt == udp6 {
		return true
	}
	return false
}

func (bt bindType) String() string {
	switch bt {
	case tcp:
		return "tcp"
	case tcp6:
		return "tcp6"
	case udp:
		return "udp"
	case udp6:
		return "udp6"
	}
	return "unknown"
}

func translateReaderType(s string) (readerType, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case `line`:
		return lineReader, nil
	case `rfc5424`:
		return rfc5424Reader, nil
	case ``:
		return lineReader, nil
	}
	return -1, errors.New("invalid reader type")
}

func (rt readerType) String() string {
	switch rt {
	case lineReader:
		return `LINE`
	case rfc5424Reader:
		return `RFC5424`
	}
	return "UNKNOWN"
}
