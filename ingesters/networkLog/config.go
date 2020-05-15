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
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/config"

	"github.com/gravwell/gcfg"
)

const (
	maxConfigSize    int64  = (1024 * 1024 * 2) //2MB, even this is crazy large
	maxSnapLen       int    = 0xffff
	defaultSnapLen   int    = 96
	defaultBpfFilter string = `not tcp port 4023 and not tcp port 4024`

	envInterface string = `GRAVWELL_SNIFF_INTERFACE`
	envBPFFilter string = `GRAVWELL_SNIFF_BPF_FILTER`
	envSniffTag  string = `GRAVWELL_SNIFF_TAG`
	envSnapLen   string = `GRAVWELL_SNIFF_SNAPLEN`
)

type cfgReadType struct {
	Global  config.IngestConfig
	Sniffer map[string]*snif
}

type snif struct {
	Interface       string //interface name to bind to
	Promisc         bool   //whether we are binding in promisc mode
	Tag_Name        string //tag to apply to ingested data
	Snap_Len        int    //max capture length for packets
	BPF_Filter      string //BPF-syntax expression to filter packets captured
	Source_Override string //override normal source IP of the interface
}

type cfgType struct {
	config.IngestConfig
	Sniffer map[string]*snif
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

	var cr cfgReadType
	if err := gcfg.ReadStringInto(&cr, string(content)); err != nil {
		return nil, err
	}
	c := &cfgType{
		IngestConfig: cr.Global,
		Sniffer:      cr.Sniffer,
	}
	if err := verifyConfig(c); err != nil {
		return nil, err
	}
	// Verify and set UUID
	if _, ok := c.IngesterUUID(); !ok {
		id := uuid.New()
		if err = c.SetIngesterUUID(id, path); err != nil {
			return nil, err
		}
		if id2, ok := c.IngesterUUID(); !ok || id != id2 {
			return nil, errors.New("Failed to set a new ingester UUID")
		}
	}
	return c, nil
}

func verifyConfig(c *cfgType) error {
	if err := c.Verify(); err != nil {
		return err
	}
	if len(c.Sniffer) == 0 {
		return errors.New("No Sniffers specified")
	}
	for k, v := range c.Sniffer {
		if err := config.LoadEnvVar(&v.Interface, envInterface, ``); err != nil {
			return err
		}
		if len(v.Interface) == 0 {
			if defIface, err := getNonLoopbackInterface(); err != nil {
				v.Interface = defIface
			} else {
				return errors.New("No Inteface provided for " + k)
			}
		}
		if err := config.LoadEnvVar(&v.Tag_Name, envSniffTag, `default`); err != nil {
			return err
		}
		if len(v.Tag_Name) == 0 {
			v.Tag_Name = `default`
		}
		if strings.ContainsAny(v.Tag_Name, ingest.FORBIDDEN_TAG_SET) {
			return errors.New("Invalid characters in the \"" + v.Tag_Name + "\"Tag-Name for " + k)
		}
		if err := getEnvInt(&v.Snap_Len, defaultSnapLen, envSnapLen); err != nil {
			return err
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
		if err := config.LoadEnvVar(&v.BPF_Filter, envBPFFilter, defaultBpfFilter); err != nil {
			return err
		}
	}
	return nil
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
	sort.Strings(tags)
	return tags, nil
}

func getEnvInt(cnd *int, defval int, nm string) (err error) {
	var s string
	if *cnd > 0 {
		return
	} else if err = config.LoadEnvVar(&s, nm, ``); err != nil {
		return
	} else if s == `` {
		*cnd = defval
	} else {
		//attempt to parse snaplen
		var t uint64
		if t, err = config.ParseUint64(s); err == nil {
			*cnd = int(t)
		}
	}
	return
}

func getNonLoopbackInterface() (name string, err error) {
	var ifaces []net.Interface
	if ifaces, err = net.Interfaces(); err != nil {
		return
	} else if len(ifaces) == 0 {
		err = errors.New("No interfaces found")
		return
	}

	for _, iface := range ifaces {
		if (iface.Flags & net.FlagLoopback) != 0 {
			continue
		} else if (iface.Flags & net.FlagPointToPoint) != 0 {
			continue
		} else if (iface.Flags * net.FlagUp) == 0 {
			continue
		}
		//got one!
		name = iface.Name
		return
	}
	err = errors.New("No non-loopback interface found")
	return
}
