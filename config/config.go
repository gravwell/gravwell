/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package config

import (
	"errors"
	"strings"
	"time"
)

const (
	kb = 1024
	mb = 1024 * kb
	gb = 1024 * mb

	defaultMaxCache = 512
)

var (
	ErrNoConnections            = errors.New("No connections specified")
	ErrMissingIngestSecret      = errors.New("Ingest-Secret value missing")
	ErrInvalidLogLevel          = errors.New("Invalid Log Level")
	ErrInvalidConnectionTimeout = errors.New("Invalid connection timeout")
	ErrInvalidIngestCacheSize   = errors.New("Invalid Max Ingest Cache size")
	ErrCacheEnabledZeroMax      = errors.New("Ingest cache enabled with zero Max Cache size")
)

type IngestConfig struct {
	Ingest_Secret              string
	Connection_Timeout         string
	Verify_Remote_Certificates bool
	Cleartext_Backend_Target   []string
	Encrypted_Backend_Target   []string
	Pipe_Backend_Target        []string
	Ingest_Cache_Path          string
	Max_Ingest_Cache           int64 //maximum amount of data to cache in MB
	Log_Level                  string
}

func (ic *IngestConfig) Init() {
	//SECURITY SHIT!
	//default the Verifiy_Remote_Certificates to true
	ic.Verify_Remote_Certificates = true
}

func (ic *IngestConfig) Verify() error {
	ic.Log_Level = strings.ToUpper(strings.TrimSpace(ic.Log_Level))
	if ic.Max_Ingest_Cache == 0 && len(ic.Ingest_Cache_Path) != 0 {
		ic.Max_Ingest_Cache = defaultMaxCache
	}
	if to, err := ic.parseTimeout(); err != nil || to < 0 {
		if err != nil {
			return err
		}
		return ErrInvalidConnectionTimeout
	}
	if len(ic.Ingest_Secret) == 0 {
		return ErrMissingIngestSecret
	}
	//ensure there is at least one target
	if (len(ic.Cleartext_Backend_Target) + len(ic.Encrypted_Backend_Target) + len(ic.Pipe_Backend_Target)) == 0 {
		return ErrNoConnections
	}

	//normalize the log level and check it
	if err := ic.checkLogLevel(); err != nil {
		return err
	}

	//check the max cache
	if ic.Max_Ingest_Cache < 0 {
		return ErrInvalidIngestCacheSize
	} else if ic.Max_Ingest_Cache == 0 && len(ic.Ingest_Cache_Path) != 0 {
		return ErrCacheEnabledZeroMax
	}
	return nil
}

func (ic *IngestConfig) Targets() ([]string, error) {
	var conns []string
	for _, v := range ic.Cleartext_Backend_Target {
		conns = append(conns, "tcp://"+v)
	}
	for _, v := range ic.Encrypted_Backend_Target {
		conns = append(conns, "tls://"+v)
	}
	for _, v := range ic.Pipe_Backend_Target {
		conns = append(conns, "pipe://"+v)
	}
	if len(conns) == 0 {
		return nil, ErrNoConnections
	}
	return conns, nil
}

func (ic *IngestConfig) VerifyRemote() bool {
	return ic.Verify_Remote_Certificates
}

func (ic *IngestConfig) Timeout() time.Duration {
	if tos, _ := ic.parseTimeout(); tos > 0 {
		return tos
	}
	return 0
}

func (ic *IngestConfig) Secret() string {
	return ic.Ingest_Secret
}

// EnableCache indicates whether a file cache is enabled
func (ic *IngestConfig) EnableCache() bool {
	return len(ic.Ingest_Cache_Path) != 0
}

// LocalFileCachePath returns the path to the local ingest cache
// an empty string means no cache enabled
func (ic *IngestConfig) LocalFileCachePath() string {
	return ic.Ingest_Cache_Path
}

// MaxCachedData returns the maximum amount of data to be cached in bytes
func (ic *IngestConfig) MaxCachedData() uint64 {
	return uint64(ic.Max_Ingest_Cache * mb)
}

// Return the specified log level
func (ic *IngestConfig) LogLevel() string {
	return ic.Log_Level
}

func (ic *IngestConfig) checkLogLevel() error {
	if len(ic.Log_Level) == 0 {
		ic.Log_Level = `OFF`
		return nil
	}
	switch ic.Log_Level {
	case `OFF`:
		fallthrough
	case `INFO`:
		fallthrough
	case `WARN`:
		fallthrough
	case `ERROR`:
		return nil
	}
	return ErrInvalidLogLevel
}

func (ic *IngestConfig) parseTimeout() (time.Duration, error) {
	tos := strings.TrimSpace(ic.Connection_Timeout)
	if len(tos) == 0 {
		return 0, nil
	}
	return time.ParseDuration(tos)
}
