package main

import (
	"context"
	"errors"
	"strings"
	"time"


	"github.com/gravwell/gravwell/v4/ingest/attach"
	"github.com/gravwell/gravwell/v4/ingest/config"
	"github.com/gravwell/gravwell/v4/ingest/log"
	"github.com/gravwell/gravwell/v4/ingest/processors"
)

var (
	lg *log.Logger
)

const (
	defaultRequestPerMinute        = 6
	defaultConfigLoc               = `/opt/gravwell/etc/gravwell_fetcher.conf`
	defaultConfigDLoc              = `/opt/gravwell/etc/gravwell_fetcher.conf.d`
	defaultStateLoc                = `/opt/gravwell/etc/gravwell_fetcher.state`
	appName                 string = `fetcher`
	ingesterName            string = "fetcher"
)

type global struct {
	config.IngestConfig
	RequestPerMinute     string
	State_Store_Location string
}

/*
Add the config to the general cfgType to register it

*/

type cfgType struct {
	Global       global
	Attach       attach.AttachConfig
	Preprocessor processors.ProcessorConfig
	DuoConf      map[string]*duoConf
	ThinkstConf  map[string]*ThinkstConf
	OktaConf     map[string]*OktaConf
	AsanaConf    map[string]*asanaConf
	ShodanConf   map[string]*ShodanConf
}

func (c cfgType) Verify() error {
	if err := c.Global.IngestConfig.Verify(); err != nil {
		return err
	} else if err = c.Attach.Verify(); err != nil {
		return err
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

	return nil
}

func GetConfig(path, overlayPath string) (*cfgType, error) {
	var c cfgType
	if err := config.LoadConfigFile(&c, path); err != nil {
		return nil, err
	} else if err = config.LoadConfigOverlays(&c, overlayPath); err != nil {
		return nil, err
	}

	/*
		Configure Verify
	*/
	if err := c.Verify(); err != nil {
		return nil, err
	}
	if err := c.AsanaVerify(); err != nil {
		return nil, err
	}
	if err := c.ThinkstVerify(); err != nil {
		return nil, err
	}
	if err := c.OktaVerify(); err != nil {
		return nil, err
	}
	if err := c.ShodanVerify(); err != nil {
		return nil, err
	}

	//initialize the state store location if its empty
	if c.Global.State_Store_Location == `` {
		c.Global.State_Store_Location = defaultStateLoc
	}
	if c.Global.Log_File == `` {
		c.Global.Log_File = defaultConfigLoc
	}
	if c.Global.RequestPerMinute == `` {
		c.Global.RequestPerMinute = defaultRequestsPerMin
	}
	return &c, nil
}

func (c *cfgType) Tags() ([]string, error) {
	var tags []string
	tagMp := make(map[string]bool, 1)
	/*
		Configure Tags
	*/
	for _, v := range c.AsanaConf {
		if len(v.Tag_Name) == 0 {
			continue
		}
		if _, ok := tagMp[v.Tag_Name]; !ok {
			tags = append(tags, v.Tag_Name)
			tagMp[v.Tag_Name] = true
		}
	}
	for _, v := range c.DuoConf {
		if len(v.Tag_Name) == 0 {
			continue
		}
		if _, ok := tagMp[v.Tag_Name]; !ok {
			tags = append(tags, v.Tag_Name)
			tagMp[v.Tag_Name] = true
		}
	}
	for _, v := range c.ThinkstConf {
		if len(v.Tag_Name) == 0 {
			continue
		}
		if _, ok := tagMp[v.Tag_Name]; !ok {
			tags = append(tags, v.Tag_Name)
			tagMp[v.Tag_Name] = true
		}
	}
	//Special one for okta since it can have two tags
	for _, v := range c.OktaConf {
		if len(v.Tag_Name) == 0 {
			continue
		}
		if _, ok := tagMp[v.Tag_Name]; !ok {
			tags = append(tags, v.Tag_Name)
			tagMp[v.Tag_Name] = true
		}
		if _, ok := tagMp[v.UserTag]; !ok {
			tags = append(tags, v.UserTag)
			tagMp[v.UserTag] = true
		}
	}
	for _, v := range c.ShodanConf {
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

func (c *cfgType) IngestBaseConfig() config.IngestConfig {
	return c.Global.IngestConfig
}

func (c *cfgType) AttachConfig() attach.AttachConfig {
	return c.Attach
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

func (c *cfgType) parseTimeout() (time.Duration, error) {
	tos := strings.TrimSpace(c.Global.Connection_Timeout)
	if len(tos) == 0 {
		return 0, nil
	}
	return time.ParseDuration(tos)
}

func quitableSleep(ctx context.Context, to time.Duration) (quit bool) {
	tmr := time.NewTimer(to)
	defer tmr.Stop()
	select {
	case <-tmr.C:
	case <-ctx.Done():
		quit = true
	}
	return
}

func setObjectTracker(ot *objectTracker, group string, key string, latestTime time.Time) {
	state := trackedObjectState{
		Updated:    time.Now(),
		LatestTime: latestTime,
	}
	err := ot.Set(group, key, state, false)
	if err != nil {
		lg.Fatal("failed to set state tracker", log.KV("listener", key), log.KV("fetcherType", group), log.KVErr(err))

	}
	err = ot.Flush()
	if err != nil {
		lg.Fatal("failed to flush state tracker", log.KV("listener", key), log.KV("fetcherType", group), log.KVErr(err))
	}
}
