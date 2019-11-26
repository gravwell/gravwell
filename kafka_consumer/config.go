/*************************************************************************
 * Copyright 2019 Gravwell, Inc. All rights reserved.
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
	"time"

	"github.com/Shopify/sarama"
	"github.com/gravwell/ingest/v3"
	"github.com/gravwell/ingest/v3/config"
	"github.com/gravwell/ingest/v3/processors"
	"github.com/gravwell/timegrinder/v3"
)

const (
	kb = 1024
	mb = 1024 * kb
	gb = 1024 * mb

	MAX_CONFIG_SIZE      int64  = (1024 * 1024 * 2) //2MB, even this is crazy large
	defaultPort          uint16 = 9092
	defaultBatchSize     int    = 512
	defaultConsumerGroup string = `gravwell`
)

type ConfigConsumer struct {
	Tag_Name           string
	Leader             string
	Topic              string
	Consumer_Group     string
	Source_Override    string
	Rebalance_Strategy string
	Key_As_Source      bool
	Synchronous        bool
	Batch_Size         int

	Ignore_Timestamps         bool //Just apply the current timestamp to lines as we get them
	Extract_Timestamps        bool // Ignore the kafka timestamp, use timegrinder
	Assume_Local_Timezone     bool
	Timezone_Override         string
	Timestamp_Format_Override string //override the timestamp format
	Preprocessor              []string
}

type consumerCfg struct {
	tag          string
	leader       string
	topic        string
	group        string
	strat        sarama.BalanceStrategy
	sync         bool
	batchSize    int
	keyAsSrc     bool
	srcOverride  net.IP
	ignoreTS     bool
	extractTS    bool
	tg           *timegrinder.TimeGrinder
	preprocessor []string
}

type cfgReadType struct {
	Global       config.IngestConfig
	Consumer     map[string]*ConfigConsumer
	Preprocessor processors.ProcessorConfig
}

type cfgType struct {
	config.IngestConfig
	Consumers    map[string]*consumerCfg
	Preprocessor processors.ProcessorConfig
}

func GetConfig(path string) (*cfgType, error) {
	//read into the intermediary type to maintain backwards compatibility with the old system
	var cr cfgReadType
	if err := config.LoadConfigFile(&cr, path); err != nil {
		return nil, err
	}
	//validate the global params
	if err := cr.Global.Verify(); err != nil {
		return nil, err
	} else if len(cr.Consumer) == 0 {
		return nil, errors.New("no consumers defined")
	} else if err := cr.Preprocessor.Validate(); err != nil {
		return nil, err
	}

	//create our actual config
	c := &cfgType{
		IngestConfig: cr.Global,
		Consumers:    make(map[string]*consumerCfg, len(cr.Consumer)),
		Preprocessor: cr.Preprocessor,
	}
	for k, v := range cr.Consumer {
		if _, ok := c.Consumers[k]; ok {
			return nil, fmt.Errorf("Consumer %s is duplicated", k)
		}
		if err := c.Preprocessor.CheckProcessors(v.Preprocessor); err != nil {
			return nil, fmt.Errorf("Consumer %s preprocessor invalid: %v", k, err)
		}
		if cnsmr, err := v.validateAndProcess(); err != nil {
			return nil, err
		} else {
			c.Consumers[k] = &cnsmr
		}
	}
	return c, nil
}

func (c *cfgType) Tags() (tags []string, err error) {
	tagMp := make(map[string]bool, len(c.Consumers))
	//iterate over consumers
	for _, v := range c.Consumers {
		if len(v.tag) == 0 {
			continue
		}
		if _, ok := tagMp[v.tag]; !ok {
			tags = append(tags, v.tag)
			tagMp[v.tag] = true
		}
	}

	if len(tags) == 0 {
		err = errors.New("No tags specified")
	} else {
		sort.Strings(tags)
	}
	return
}

func (cc ConfigConsumer) validateAndProcess() (c consumerCfg, err error) {
	//check tag
	if len(cc.Tag_Name) == 0 {
		err = errors.New("missing tag name")
		return
	} else if err = ingest.CheckTag(cc.Tag_Name); err != nil {
		return
	}
	c.tag = cc.Tag_Name

	//check leader
	if len(cc.Leader) == 0 {
		err = errors.New("Missing leader type")
		return
	}
	c.leader = config.AppendDefaultPort(cc.Leader, defaultPort)
	if _, _, err = net.SplitHostPort(c.leader); err != nil {
		return
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
		tcfg := timegrinder.Config{
			EnableLeftMostSeed: true,
			FormatOverride:     cc.Timestamp_Format_Override,
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
	c.preprocessor = cc.Preprocessor
	c.keyAsSrc = cc.Key_As_Source
	c.strat, err = cc.balanceStrat()
	return
}

func (cc ConfigConsumer) balanceStrat() (st sarama.BalanceStrategy, err error) {
	switch strings.ToLower(strings.TrimSpace(cc.Rebalance_Strategy)) {
	case `sticky`:
		st = sarama.BalanceStrategySticky
	case `range`:
		st = sarama.BalanceStrategyRange
	case `roundrobin`:
		st = sarama.BalanceStrategyRoundRobin
	case ``:
		st = sarama.BalanceStrategyRoundRobin
	default:
		err = errors.New("Unknown balance strategy")
	}
	return
}
