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
	"os"
	"sort"
	"strings"
	"time"

	"github.com/Shopify/sarama"
	"github.com/gravwell/ingest/v3"
	"github.com/gravwell/ingest/v3/config"

	"gopkg.in/gcfg.v1"
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
	Read_Timeout       string
	Source_Override    string
	Rebalance_Strategy string
	Key_As_Source      bool
	Synchronous        bool
	Batch_Size         int
}

type consumerCfg struct {
	tag         string
	leader      string
	topic       string
	group       string
	strat       sarama.BalanceStrategy
	sync        bool
	batchSize   int
	keyAsSrc    bool
	timeout     time.Duration
	srcOverride net.IP
}

type cfgReadType struct {
	Global   config.IngestConfig
	Consumer map[string]*ConfigConsumer
}

type cfgType struct {
	config.IngestConfig
	Consumers map[string]*consumerCfg
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
	//validate the global params
	if err := cr.Global.Verify(); err != nil {
		return nil, err
	} else if len(cr.Consumer) == 0 {
		return nil, errors.New("no consumers defined")
	}

	//create our actual config
	c := &cfgType{
		IngestConfig: cr.Global,
		Consumers:    make(map[string]*consumerCfg, len(cr.Consumer)),
	}
	for k, v := range cr.Consumer {
		if _, ok := c.Consumers[k]; ok {
			return nil, fmt.Errorf("Consumer %s is duplicated", k)
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

	//check the timeout
	if len(cc.Read_Timeout) > 0 {
		//attempt to parse it
		v := strings.Join(strings.Fields(cc.Read_Timeout), "")
		if c.timeout, err = time.ParseDuration(v); err != nil {
			return
		}
	}

	// check that the source override is valid
	if len(cc.Source_Override) > 0 {
		if c.srcOverride = net.ParseIP(cc.Source_Override); c.srcOverride == nil {
			err = fmt.Errorf("Invalid source override %s", cc.Source_Override)
			return
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
		st = sarama.BalanceStrategySticky
	default:
		err = errors.New("Unknown balance strategy")
	}
	return
}
