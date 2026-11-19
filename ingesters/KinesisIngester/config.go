/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/kinesis/types"
	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/attach"
	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/processors"
	"github.com/gravwell/gravwell/v3/sqs_common"
)

const (
	defaultStateStore = `/opt/gravwell/etc/kinesis_ingest.state`
	defaultLogFile    = `/opt/gravwell/log/kinesis.log`
)

type bindType int
type readerType int

type global struct {
	config.IngestConfig
	State_Store_Location  string
	Credentials_Type      string
	AWS_Access_Key_ID     string `json:"-"` // DO NOT send this when marshalling
	AWS_Secret_Access_Key string `json:"-"` // DO NOT send this when marshalling
}

type streamDef struct {
	Stream_Name           string
	Tag_Name              string
	Iterator_Type         string
	Endpoint              string
	Region                string
	Assume_Local_Timezone bool
	Timezone_Override     string
	Parse_Time            bool
	Metrics_Interval      int  // Seconds between metrics update, 0 disables
	JSON_Metrics          bool // set to true for json-output metrics instead of plaintext
	Preprocessor          []string
}

type cfgType struct {
	Global        global
	Attach        attach.AttachConfig
	KinesisStream map[string]*streamDef
	Preprocessor  processors.ProcessorConfig
	TimeFormat    config.CustomTimeFormat
}

func GetConfig(path, overlayPath string) (*cfgType, error) {
	var c cfgType
	if err := config.LoadConfigFile(&c, path); err != nil {
		return nil, err
	} else if err = config.LoadConfigOverlays(&c, overlayPath); err != nil {
		return nil, err
	}
	//initialize the state store location if its empty
	if c.Global.State_Store_Location == `` {
		c.Global.State_Store_Location = defaultStateStore
	}
	if c.Global.Log_File == `` {
		c.Global.Log_File = defaultLogFile
	}
	if err := c.Verify(); err != nil {
		return nil, err
	}
	return &c, nil
}

func (c *cfgType) Verify() error {
	if err := c.Global.IngestConfig.Verify(); err != nil {
		return err
	} else if err = c.Attach.Verify(); err != nil {
		return err
	}
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
	if _, err := sqs_common.GetCredentials(c.Global.Credentials_Type, c.Global.AWS_Access_Key_ID, c.Global.AWS_Secret_Access_Key); err != nil {
		return fmt.Errorf("Global credentials invalid (Credentials-Type, AWS-Access-Key-ID, AWS-Secret-Access-Key): %w", err)
	}
	if len(c.KinesisStream) == 0 {
		return errors.New("At least one Kinesis stream required.")
	}
	if err := c.Preprocessor.Validate(); err != nil {
		return err
	} else if err = c.TimeFormat.Validate(); err != nil {
		return err
	}
	sdkRegion := sync.OnceValues(loadSDKRegion)
	for k, v := range c.KinesisStream {
		if err := verifyStream(k, v, sdkRegion); err != nil {
			return err
		}
		if err := c.Preprocessor.CheckProcessors(v.Preprocessor); err != nil {
			return fmt.Errorf("Kinesis stream %s preprocessor invalid: %v", k, err)
		}
	}
	return nil
}

// verifyStream checks a stream definition and fills in its defaults.
// sdkRegion is called only when the stream sets no Region.
func verifyStream(k string, v *streamDef, sdkRegion func() (string, error)) error {
	if v == nil {
		return fmt.Errorf("Kinesis stream %v config is nil", k)
	}
	if v.Stream_Name == `` {
		return fmt.Errorf("Kinesis stream %v Stream-Name parameter is empty", k)
	}
	if v.Region == `` {
		// the AWS SDK falls back to AWS_REGION and the shared config
		if r, err := sdkRegion(); err != nil {
			return fmt.Errorf("Kinesis stream %v Region parameter is empty and the AWS config failed to load: %w", k, err)
		} else if r == `` {
			return fmt.Errorf("Kinesis stream %v Region parameter is empty", k)
		}
	}
	if v.Tag_Name == `` {
		v.Tag_Name = entry.DefaultTagName
	}
	if err := ingest.CheckTag(v.Tag_Name); err != nil {
		return fmt.Errorf("Kinesis stream %v Tag-Name %q is invalid: %w", k, v.Tag_Name, err)
	}
	// main.go sets no starting sequence number or timestamp, so only these two work
	switch it := types.ShardIteratorType(strings.ToUpper(strings.TrimSpace(v.Iterator_Type))); it {
	case ``:
		v.Iterator_Type = string(types.ShardIteratorTypeLatest)
	case types.ShardIteratorTypeLatest, types.ShardIteratorTypeTrimHorizon:
		v.Iterator_Type = string(it)
	default:
		return fmt.Errorf("Kinesis stream %v Iterator-Type %q is invalid, must be %s or %s",
			k, v.Iterator_Type, types.ShardIteratorTypeLatest, types.ShardIteratorTypeTrimHorizon)
	}
	if v.Timezone_Override != `` {
		if v.Assume_Local_Timezone {
			return fmt.Errorf("Kinesis stream %v cannot specify both Assume-Local-Timezone and Timezone-Override", k)
		}
		if _, err := time.LoadLocation(v.Timezone_Override); err != nil {
			return fmt.Errorf("Kinesis stream %v Timezone-Override %q is invalid: %w", k, v.Timezone_Override, err)
		}
	}
	return nil
}

// loadSDKRegion returns the region the AWS SDK resolves when the config sets none.
func loadSDKRegion() (string, error) {
	cfg, err := awsconfig.LoadDefaultConfig(context.Background())
	if err != nil {
		return ``, err
	}
	return cfg.Region, nil
}

func (c *cfgType) Targets() ([]string, error) {
	return c.Global.Targets()
}

func (c *cfgType) Tags() ([]string, error) {
	var tags []string
	tagMp := make(map[string]bool, 1)
	for _, v := range c.KinesisStream {
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

func (c *cfgType) Secret() string {
	return c.Global.Ingest_Secret
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
