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
	"os"
	"strings"
	"time"

	"github.com/gravwell/ingest"

	"gopkg.in/gcfg.v1"
)

const (
	maxConfigSize  int64 = (1024 * 1024 * 2) //2MB, even this is crazy large
	maxSnapLen     int   = 0xffff
	defaultSnapLen int   = 96
)

type cfgType struct {
	Global struct {
		Ingest_Secret              string //shared secret to authenticate
		Connection_Timeout         string
		Verify_Remote_Certificates bool
		Cleartext_Backend_Target   []string //unencrypted IP targets
		Encrypted_Backend_Target   []string //encrypted IP targets
		Pipe_Backend_Target        []string //Unix named pipe targets (local machine only)
		Ingest_Cache_Path          string   //filename to use for caching
		Log_Level                  string   //level at which to emit logs
	}
	Sniffer map[string]*struct {
		Interface       string //interface name to bind to
		Promisc         bool   //whether we are binding in promisc mode
		Tag_Name        string //tag to apply to ingested data
		Snap_Len        int    //max capture length for packets
		BPF_Filter      string //BPF-syntax expression to filter packets captured
		Source_Override string //override normal source IP of the interface
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
	if fi.Size() > maxConfigSize {
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
	if err := gcfg.ReadStringInto(&c, string(content)); err != nil {
		return nil, err
	}
	if err := verifyConfig(&c); err != nil {
		return nil, err
	}
	return &c, nil
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
			v.Tag_Name = `default`
		}
		if strings.ContainsAny(v.Tag_Name, ingest.FORBIDDEN_TAG_SET) {
			return errors.New("Invalid characters in the \"" + v.Tag_Name + "\"Tag-Name for " + k)
		}
		if v.Snap_Len > maxSnapLen || v.Snap_Len < 0 {
			return errors.New("Invalid snaplen. Must be < 65535 and > 0")
		}
		if v.Snap_Len == 0 {
			v.Snap_Len = defaultSnapLen
		}
		if v.Source_Override != `` {
			if net.ParseIP(v.Source_Override) == nil {
				return errors.New("Failed to parse Source_Override")
			}
		}
	}
	return nil
}

// Generate a slice of all targets, prepending with the appropriate scheme (e.g. http://)
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

// Generate a list of all tags used by this ingester
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
