/*************************************************************************
 * Copyright 2019 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"crypto/sha256"
	"crypto/sha512"
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
	"time"

	"github.com/IBM/sarama"
	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/attach"
	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/gravwell/gravwell/v3/ingest/processors"
	"github.com/gravwell/gravwell/v3/ingest/processors/tags"
	"github.com/gravwell/gravwell/v3/timegrinder"
	"github.com/xdg-go/scram"
)

const (
	kb = 1024
	mb = 1024 * kb
	gb = 1024 * mb

	MAX_CONFIG_SIZE      int64  = (1024 * 1024 * 2) //2MB, even this is crazy large
	defaultPort          uint16 = 9092
	defaultBatchSize     int    = 512
	defaultConsumerGroup string = `gravwell`
	defaultSRCHeader            = `SRC`
	defaultTagHeader            = `TAG`

	authPlain       = `plain`
	authScramSHA256 = `scramsha256`
	authScramSHA512 = `scramsha512`
)

type KafkaAuthConfig struct {
	Auth_Type string
	Username  string
	Password  string
}

type ConfigConsumer struct {
	Leader             []string
	Topic              string
	Consumer_Group     string
	Source_Override    string
	Rebalance_Strategy []string
	Source_Header      string
	Tag_Header         string
	Source_As_Binary   bool
	Synchronous        bool
	Batch_Size         int
	Default_Tag        string

	tags.TaggerConfig

	KafkaAuthConfig

	//TLS stuff
	Use_TLS                  bool
	Insecure_Skip_TLS_Verify bool

	//consumer configs
	Ignore_Timestamps         bool //Just apply the current timestamp to lines as we get them
	Extract_Timestamps        bool // Ignore the kafka timestamp, use timegrinder
	Assume_Local_Timezone     bool
	Timezone_Override         string
	Timestamp_Format_Override string //override the timestamp format

	//list of preprocessors to run
	Preprocessor []string
}

type consumerCfg struct {
	tags.TaggerConfig
	defTag      string
	leader      []string
	topic       string
	group       string
	strats      []sarama.BalanceStrategy
	sync        bool
	batchSize   int
	srcKey      string
	tagKey      string
	srcBin      bool
	srcOverride net.IP

	auth KafkaAuthConfig

	//tls configs
	useTLS     bool
	skipVerify bool

	//consumer configs for timestamps and time grinding
	ignoreTS     bool
	extractTS    bool
	tg           *timegrinder.TimeGrinder
	preprocessor []string
}

type cfgReadType struct {
	Global       config.IngestConfig
	Attach       attach.AttachConfig
	Consumer     map[string]*ConfigConsumer
	Preprocessor processors.ProcessorConfig
	TimeFormat   config.CustomTimeFormat
}

type cfgType struct {
	config.IngestConfig
	Attach       attach.AttachConfig
	Consumers    map[string]*consumerCfg
	Preprocessor processors.ProcessorConfig
	TimeFormat   config.CustomTimeFormat
}

func GetConfig(path, overlayPath string) (*cfgType, error) {
	//read into the intermediary type to maintain backwards compatibility with the old system
	var cr cfgReadType
	if err := config.LoadConfigFile(&cr, path); err != nil {
		return nil, err
	} else if err = config.LoadConfigOverlays(&cr, overlayPath); err != nil {
		return nil, err
	}

	//create our actual config
	c := &cfgType{
		IngestConfig: cr.Global,
		Attach:       cr.Attach,
		Consumers:    make(map[string]*consumerCfg, len(cr.Consumer)),
		Preprocessor: cr.Preprocessor,
		TimeFormat:   cr.TimeFormat,
	}

	for k, v := range cr.Consumer {
		if _, ok := c.Consumers[k]; ok {
			return nil, fmt.Errorf("Consumer %s is duplicated", k)
		}
		if err := c.Preprocessor.CheckProcessors(v.Preprocessor); err != nil {
			return nil, fmt.Errorf("Consumer %s preprocessor invalid: %v", k, err)
		}
		if cnsmr, err := v.validateAndProcess(cr); err != nil {
			return nil, err
		} else if err := c.TimeFormat.LoadFormats(cnsmr.tg); err != nil {
			return nil, err
		} else {
			if v.Timestamp_Format_Override != `` {
				if err = cnsmr.tg.SetFormatOverride(v.Timestamp_Format_Override); err != nil {
					return nil, err
				}
			}
			c.Consumers[k] = &cnsmr
		}
	}
	if err := c.Verify(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *cfgType) Verify() error {
	//validate the global params
	if err := c.IngestConfig.Verify(); err != nil {
		return err
	} else if err = c.Attach.Verify(); err != nil {
		return err
	} else if len(c.Consumers) == 0 {
		return errors.New("no consumers defined")
	} else if err = c.Preprocessor.Validate(); err != nil {
		return err
	} else if err = c.TimeFormat.Validate(); err != nil {
		return err
	}

	return nil
}

func (c *cfgType) Tags() (tags []string, err error) {
	tagMp := make(map[string]bool, len(c.Consumers))
	//iterate over consumers
	for name, v := range c.Consumers {
		var ltags []string
		if v.defTag == `` {
			err = fmt.Errorf("Consumer %s is missing Default-Tag definition", name)
			return
		} else if err = ingest.CheckTag(v.defTag); err != nil {
			err = fmt.Errorf("Consumer %s Default-Tag is invalid: %v", name, err)
			return
		} else if _, ok := tagMp[v.defTag]; !ok {
			tags = append(tags, v.defTag)
			tagMp[v.defTag] = true
		}
		if ltags, _, err = v.TaggerConfig.TagSet(); err != nil {
			return
		} else if len(ltags) == 0 {
			continue
		} else {
			for _, lt := range ltags {
				if _, ok := tagMp[lt]; !ok {
					tags = append(tags, lt)
					tagMp[lt] = true
				}
			}
		}

	}

	if len(tags) == 0 {
		err = errors.New("No tags specified")
	} else {
		sort.Strings(tags)
	}
	return
}

func (c *cfgType) IngestBaseConfig() config.IngestConfig {
	return c.IngestConfig
}

func (c *cfgType) AttachConfig() attach.AttachConfig {
	return c.Attach
}

func (cc ConfigConsumer) validateAndProcess(cr cfgReadType) (c consumerCfg, err error) {
	//check tag
	if len(cc.Default_Tag) == 0 {
		err = errors.New("missing Default-Tag")
		return
	} else if err = ingest.CheckTag(cc.Default_Tag); err != nil {
		return
	} else if err = cc.TaggerConfig.Validate(); err != nil {
		return
	} else if err = cc.KafkaAuthConfig.Validate(); err != nil {
		return
	}
	c.auth = cc.KafkaAuthConfig
	c.defTag = cc.Default_Tag
	c.TaggerConfig = cc.TaggerConfig
	if cc.Source_Header == `` {
		cc.Source_Header = defaultSRCHeader
	}
	if cc.Tag_Header == `` {
		cc.Tag_Header = defaultTagHeader
	}
	c.srcKey = cc.Source_Header
	c.tagKey = cc.Tag_Header
	c.srcBin = cc.Source_As_Binary

	//check leader
	if len(cc.Leader) == 0 {
		err = errors.New("Missing Kafka Leader(s)")
		return
	}

	for _, l := range cc.Leader {
		leader := config.AppendDefaultPort(l, defaultPort)
		if _, _, err = net.SplitHostPort(leader); err != nil {
			//wrap the error to show what we are mad about
			err = fmt.Errorf("invalid Leader %q - %w", l, err)
			return
		}
		c.leader = append(c.leader, leader)
	}

	//check the topic
	if len(cc.Topic) == 0 {
		err = errors.New("Missing topic name")
		return
	}
	c.topic = cc.Topic

	//just set the sync
	c.sync = cc.Synchronous

	// check that the source override is valid
	if len(cc.Source_Override) > 0 {
		if c.srcOverride = net.ParseIP(cc.Source_Override); c.srcOverride == nil {
			err = fmt.Errorf("Invalid source override %s", cc.Source_Override)
			return
		}
	}

	if cc.Timezone_Override != "" {
		if cc.Assume_Local_Timezone {
			// cannot do both
			err = fmt.Errorf("Cannot specify Assume-Local-Timezone and Timezone-Override in the same consumer")
			return
		}
		if _, err = time.LoadLocation(cc.Timezone_Override); err != nil {
			err = fmt.Errorf("Invalid timezone override %v in consumer: %v", cc.Timezone_Override, err)
			return
		}
	}
	if cc.Ignore_Timestamps {
		c.ignoreTS = true
	} else if cc.Extract_Timestamps {
		c.extractTS = true
		var window timegrinder.TimestampWindow
		window, err = cr.Global.GlobalTimestampWindow()
		if err != nil {
			err = fmt.Errorf("Failed to get global timestamp window", err)
			return
		}
		tcfg := timegrinder.Config{
			EnableLeftMostSeed: true,
			FormatOverride:     cc.Timestamp_Format_Override,
			TSWindow:           window,
		}
		if c.tg, err = timegrinder.NewTimeGrinder(tcfg); err != nil {
			err = fmt.Errorf("Failed to generate new timegrinder: %v", err)
			return
		}
		if cc.Assume_Local_Timezone {
			c.tg.SetLocalTime()
		}
		if cc.Timezone_Override != `` {
			if err = c.tg.SetTimezone(cc.Timezone_Override); err != nil {
				err = fmt.Errorf("Failed to override timezone: %v", err)
				return
			}
		}
	}

	if cc.Batch_Size <= 0 {
		c.batchSize = defaultBatchSize
	} else {
		c.batchSize = cc.Batch_Size
	}
	if cc.Consumer_Group != `` {
		c.group = cc.Consumer_Group
	} else {
		c.group = defaultConsumerGroup
	}

	if cc.Use_TLS {
		c.useTLS = true
		c.skipVerify = cc.Insecure_Skip_TLS_Verify
	}

	c.preprocessor = cc.Preprocessor

	c.strats, err = cc.balanceStrats()
	return
}

func (cc ConfigConsumer) balanceStrats() (st []sarama.BalanceStrategy, err error) {
	//if non specified just use a default of all of them
	if len(cc.Rebalance_Strategy) == 0 {
		st = []sarama.BalanceStrategy{
			sarama.NewBalanceStrategyRoundRobin(),
			sarama.NewBalanceStrategyRange(),
			sarama.NewBalanceStrategySticky(),
		}
		return
	}
	for _, v := range cc.Rebalance_Strategy {
		switch strings.ToLower(strings.TrimSpace(v)) {
		case `sticky`:
			st = append(st, sarama.NewBalanceStrategySticky())
		case `range`:
			st = append(st, sarama.NewBalanceStrategyRange())
		case `roundrobin`:
			st = append(st, sarama.NewBalanceStrategyRoundRobin())
		default:
			err = fmt.Errorf("Unknown balance strategy %q", v)
		}
	}
	return
}

func (kac KafkaAuthConfig) Validate() (err error) {
	if kac.Auth_Type == `` {
	}
	switch strings.ToLower(kac.Auth_Type) {
	case ``:
		return //no auth
	case authPlain:
	case authScramSHA256:
	case authScramSHA512:
	default:
		err = fmt.Errorf("Unknown auth type %q", kac.Auth_Type)
		return
	}
	//auth is active
	if kac.Username == `` {
		err = fmt.Errorf("Missing Username")
	} else if kac.Password == `` {
		err = fmt.Errorf("Missing Password")
	}
	return
}

func (kac KafkaAuthConfig) SetAuth(cfg *sarama.Config) (err error) {
	if err = kac.Validate(); err != nil {
		return
	} else if kac.Auth_Type == `` {
		return
	}
	//enable the basics
	cfg.Net.SASL.Enable = true
	cfg.Net.SASL.Handshake = true
	cfg.Net.SASL.User = kac.Username
	cfg.Net.SASL.Password = kac.Password

	switch strings.ToLower(kac.Auth_Type) {
	case authPlain:
		cfg.Net.SASL.Mechanism = sarama.SASLTypePlaintext
	case authScramSHA256:
		cfg.Net.SASL.SCRAMClientGeneratorFunc = func() sarama.SCRAMClient {
			return &XDGSCRAMClient{HashGeneratorFcn: SHA256}
		}
		cfg.Net.SASL.Mechanism = sarama.SASLTypeSCRAMSHA256
	case authScramSHA512:
		cfg.Net.SASL.SCRAMClientGeneratorFunc = func() sarama.SCRAMClient {
			return &XDGSCRAMClient{HashGeneratorFcn: SHA512}
		}
		cfg.Net.SASL.Mechanism = sarama.SASLTypeSCRAMSHA512
	}
	return
}

var (
	SHA256 scram.HashGeneratorFcn = sha256.New
	SHA512 scram.HashGeneratorFcn = sha512.New
)

type XDGSCRAMClient struct {
	*scram.Client
	*scram.ClientConversation
	scram.HashGeneratorFcn
}

func (x *XDGSCRAMClient) Begin(userName, password, authzID string) (err error) {
	x.Client, err = x.HashGeneratorFcn.NewClient(userName, password, authzID)
	if err != nil {
		return err
	}
	x.ClientConversation = x.Client.NewConversation()
	return nil
}

func (x *XDGSCRAMClient) Step(challenge string) (response string, err error) {
	response, err = x.ClientConversation.Step(challenge)
	return
}

func (x *XDGSCRAMClient) Done() bool {
	return x.ClientConversation.Done()
}
