/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package config provides a common base for Gravwell ingester config files.
// The ingester will typically need to extend the config struct to allow configuration of data sources.
// An ingester might implement something like the following:
//
//	type cfgType struct {
//		Global       config.IngestConfig
//		Listener     map[string]*lst
//		Preprocessor processors.ProcessorConfig
//	}
//
//	func GetConfig(path string) (*cfgType, error) {
//		var cr cfgType
//		if err := config.LoadConfigFile(&cr, path); err != nil {
//			return nil, err
//		}
//		if err := cr.Global.Verify(); err != nil {
//			return nil, err
//		}
//		// Verify and set UUID
//		if _, ok := cr.Global.IngesterUUID(); !ok {
//			id := uuid.New()
//			if err := cr.Global.SetIngesterUUID(id, path); err != nil {
//				return nil, err
//			}
//			if id2, ok := cr.Global.IngesterUUID(); !ok || id != id2 {
//				return nil, errors.New("Failed to set a new ingester UUID")
//			}
//		}
//		return c, nil
//	}
package config

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/go-write"
	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/timegrinder"
)

const (
	defaultLogLevel = `ERROR`
	minThrottle     = (1024 * 1024) / 8
)

const (
	envSecret            string = `GRAVWELL_INGEST_SECRET`
	envLogLevel          string = `GRAVWELL_LOG_LEVEL`
	envClearTarget       string = `GRAVWELL_CLEARTEXT_TARGETS`
	envEncTarget         string = `GRAVWELL_ENCRYPTED_TARGETS`
	envPipeTarget        string = `GRAVWELL_PIPE_TARGETS`
	envCompressionTarget string = `GRAVWELL_ENABLE_COMPRESSION`
	envCacheMode         string = `GRAVWELL_CACHE_MODE`
	envCachePath         string = `GRAVWELL_CACHE_PATH`
	envMaxCache          string = `GRAVWELL_CACHE_SIZE`

	DefaultCleartextPort uint16 = 4023
	DefaultTLSPort       uint16 = 4024

	commentValue = `#`
	globalHeader = `[global]`
	headerStart  = `[`
	uuidParam    = `Ingester-UUID`

	CACHE_MODE_DEFAULT  = "always"
	CACHE_DEPTH_DEFAULT = 128
	CACHE_SIZE_DEFAULT  = 1000
)

var (
	ErrNoConnections              = errors.New("No connections specified")
	ErrMissingIngestSecret        = errors.New("Ingest-Secret value missing")
	ErrInvalidLogLevel            = errors.New("Invalid Log Level")
	ErrInvalidConnectionTimeout   = errors.New("Invalid connection timeout")
	ErrGlobalSectionNotFound      = errors.New("Global config section not found")
	ErrInvalidLineLocation        = errors.New("Invalid line location")
	ErrInvalidUpdateLineParameter = errors.New("Update line location does not contain the specified paramter")
)

type IngestConfig struct {
	IngestStreamConfig
	Ingest_Secret              string   `json:"-"` // DO NOT send this when marshalling
	Connection_Timeout         string   `json:",omitempty"`
	Verify_Remote_Certificates bool     `json:"-"` //legacy, will be removed
	Insecure_Skip_TLS_Verify   bool     `json:",omitempty"`
	Cleartext_Backend_Target   []string `json:",omitempty"`
	Encrypted_Backend_Target   []string `json:",omitempty"`
	Pipe_Backend_Target        []string `json:",omitempty"`
	Log_Level                  string   `json:",omitempty"`
	Log_File                   string   `json:",omitempty"`
	Source_Override            string   `json:",omitempty"` // override normal source if desired
	Rate_Limit                 string   `json:",omitempty"`
	Ingester_UUID              string   `json:",omitempty"`
	Cache_Depth                int      `json:",omitempty"`
	Cache_Mode                 string   `json:",omitempty"`
	Ingest_Cache_Path          string   `json:",omitempty"`
	Max_Ingest_Cache           int      `json:",omitempty"`
	Log_Source_Override        string   `json:",omitempty"` // override log messages only
	Label                      string   `json:",omitempty"` //arbitrary label that can be attached to an ingester
}

type IngestStreamConfig struct {
	Enable_Compression bool `json:",omitempty"`
}

type TimeFormat struct {
	Format string
	Regex  string
}

type CustomTimeFormat map[string]*TimeFormat

func (ic *IngestConfig) loadDefaults() error {
	//arrange the logic to be secure by default or when there is ambiguity
	if ic.Verify_Remote_Certificates {
		ic.Insecure_Skip_TLS_Verify = false
	}
	//Ingest secret
	if err := LoadEnvVar(&ic.Ingest_Secret, envSecret, ``); err != nil {
		return err
	}
	//Log level
	if err := LoadEnvVar(&ic.Log_Level, envLogLevel, defaultLogLevel); err != nil {
		return err
	}
	//Cleartext targets
	if err := LoadEnvVar(&ic.Cleartext_Backend_Target, envClearTarget, nil); err != nil {
		return err
	}
	//Encrypted targets
	if err := LoadEnvVar(&ic.Encrypted_Backend_Target, envEncTarget, nil); err != nil {
		return err
	}
	//Pipe targets
	if err := LoadEnvVar(&ic.Pipe_Backend_Target, envPipeTarget, nil); err != nil {
		return err
	}
	//Compression
	if err := LoadEnvVar(&ic.Enable_Compression, envCompressionTarget, false); err != nil {
		return err
	}
	// Cache
	if err := LoadEnvVar(&ic.Cache_Mode, envCacheMode, nil); err != nil {
		return err
	}
	if err := LoadEnvVar(&ic.Ingest_Cache_Path, envCachePath, nil); err != nil {
		return err
	}
	if err := LoadEnvVar(&ic.Max_Ingest_Cache, envMaxCache, nil); err != nil {
		return err
	}
	return nil
}

// Verify checks the configuration parameters of the IngestConfig, verifying
// that there is at least one indexer target, creating directories as necessary,
// and generally making sure values are sensible.
func (ic *IngestConfig) Verify() error {
	if err := ic.loadDefaults(); err != nil {
		return err
	}

	if ic.Ingester_UUID != `` {
		if _, err := uuid.Parse(ic.Ingester_UUID); err != nil {
			return fmt.Errorf("Malformed ingester UUID %v: %v", ic.Ingester_UUID, err)
		}
	}

	ic.Log_Level = strings.ToUpper(strings.TrimSpace(ic.Log_Level))
	if ic.Max_Ingest_Cache == 0 && len(ic.Ingest_Cache_Path) != 0 {
		ic.Max_Ingest_Cache = CACHE_SIZE_DEFAULT
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

	// Make sure the log directory exists.
	logdir := filepath.Dir(ic.Log_File)
	fi, err := os.Stat(logdir)
	if err != nil {
		if os.IsNotExist(err) {
			//try to make the directory
			err = os.MkdirAll(logdir, 0700)
			if err != nil {
				return err
			}
		} else {
			return err
		}
	} else if !fi.IsDir() {
		return errors.New("Log Location is not a directory")
	}

	if ic.Source_Override != `` {
		if net.ParseIP(ic.Source_Override) == nil {
			return errors.New("Failed to parse Source_Override")
		}
	}
	if ic.Log_Source_Override != `` {
		if net.ParseIP(ic.Log_Source_Override) == nil {
			return errors.New("Failed to parse Log_Source_Override")
		}
	}

	// cache checks and defaults
	switch strings.ToLower(ic.Cache_Mode) {
	case "":
		ic.Cache_Mode = CACHE_MODE_DEFAULT
	case "always", "fail":
	default:
		return errors.New("Cache-Mode must be [always,fail]")
	}
	if ic.Cache_Depth == 0 {
		ic.Cache_Depth = CACHE_DEPTH_DEFAULT
	}
	// there are no defaults for the cache_size.

	return nil
}

// Targets returns a list of indexer targets, including TCP, TLS, and Unix pipes.
// Each target will be prepended with the connection type, e.g.:
//  tcp://10.0.0.1:4023
func (ic *IngestConfig) Targets() ([]string, error) {
	var conns []string
	for _, v := range ic.Cleartext_Backend_Target {
		conns = append(conns, "tcp://"+AppendDefaultPort(v, DefaultCleartextPort))
	}
	for _, v := range ic.Encrypted_Backend_Target {
		conns = append(conns, "tls://"+AppendDefaultPort(v, DefaultTLSPort))
	}
	for _, v := range ic.Pipe_Backend_Target {
		conns = append(conns, "pipe://"+v)
	}
	if len(conns) == 0 {
		return nil, ErrNoConnections
	}
	return conns, nil
}

// InsecureSkipTLSVerification returns true if the Insecure-Skip-TLS-Verify
// config parameter was set.
func (ic *IngestConfig) InsecureSkipTLSVerification() bool {
	return ic.Insecure_Skip_TLS_Verify
}

// Timeout returns the timeout for an ingester connection to go live before
// giving up.
func (ic *IngestConfig) Timeout() time.Duration {
	if tos, _ := ic.parseTimeout(); tos > 0 {
		return tos
	}
	return 0
}

// Secret returns the value of the Ingest-Secret parameter, used to authenticate to the indexer.
func (ic *IngestConfig) Secret() string {
	return ic.Ingest_Secret
}

// Return the specified log level
func (ic *IngestConfig) LogLevel() string {
	return ic.Log_Level
}

func (ic *IngestConfig) checkLogLevel() error {
	if len(ic.Log_Level) == 0 {
		ic.Log_Level = defaultLogLevel
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

// RateLimit returns the bandwidth limit, in bits per second, which
// should be applied to the indexer connection.
func (ic *IngestConfig) RateLimit() (bps int64, err error) {
	if ic.Rate_Limit == `` {
		return
	}
	bps, err = ParseRate(ic.Rate_Limit)
	if bps < minThrottle {
		err = errors.New("Ingest cannot be limited below 1mbit")
	}
	return
}

//returns whether the supplied uuid is all zeros
func zeroUUID(id uuid.UUID) bool {
	for _, v := range id {
		if v != 0 {
			return false
		}
	}
	return true
}

// IngesterUUID returns the UUID of this ingester, set with the `Ingester-UUID`
// parameter. If the UUID is not set, the UUID is invalid, or the UUID is all
// zeroes, the function will return ok = false. If the UUID is valid, it returns
// the UUID and ok = true.
func (ic *IngestConfig) IngesterUUID() (id uuid.UUID, ok bool) {
	if ic.Ingester_UUID == `` {
		return
	}
	var err error
	if id, err = uuid.Parse(ic.Ingester_UUID); err == nil {
		ok = true
	}
	if zeroUUID(id) {
		ok = false
	}
	return
}

func reloadContent(loc string) (content string, err error) {
	if loc == `` {
		err = errors.New("not loaded from file")
		return
	}
	var bts []byte
	bts, err = ioutil.ReadFile(loc)
	content = string(bts)
	return
}

func (ic *IngestConfig) GetLogger() (l *log.Logger, err error) {
	var ll log.Level
	if ll, err = log.LevelFromString(ic.Log_Level); err != nil {
		return
	}

	if ic.Log_File == `` {
		l = log.NewDiscardLogger()
	} else {
		l, err = log.NewFile(ic.Log_File)
	}
	if err == nil {
		err = l.SetLevel(ll)
	}
	return
}

func updateConfigFile(loc string, content string) error {
	if loc == `` {
		return errors.New("Configuration was loaded with bytes, cannot update")
	}
	fout, err := write.TempFile(filepath.Dir(loc), loc)
	if err != nil {
		return err
	}
	if err := writeFull(fout, []byte(content)); err != nil {
		return err
	}
	return fout.CloseAtomicallyReplace()
}

func writeFull(w io.Writer, b []byte) error {
	var written int
	for written < len(b) {
		if n, err := w.Write(b[written:]); err != nil {
			return err
		} else if n == 0 {
			return errors.New("empty write")
		} else {
			written += n
		}
	}
	return nil
}

func (ctf CustomTimeFormat) Validate() (err error) {
	if len(ctf) == 0 {
		return
	}
	for k, v := range ctf {
		if v == nil {
			continue
		}
		cf := timegrinder.CustomFormat {
			Name: k,
			Format: v.Format,
			Regex:  v.Regex,
		}
		if err = cf.Validate(); err != nil {
			return
		}
	}
	return
}

func (ctf CustomTimeFormat) LoadFormats(tg *timegrinder.TimeGrinder) (err error) {
	if len(ctf) == 0 {
		return
	} else if err = ctf.Validate(); err != nil {
		return
	}
	for k, v := range ctf {
		var p timegrinder.Processor
		if v == nil {
			continue
		}
		cf := timegrinder.CustomFormat {
			Name: k,
			Format: v.Format,
			Regex:  v.Regex,
		}
		if p, err = timegrinder.NewCustomProcessor(cf); err != nil {
			return
		} else if _, err = tg.AddProcessor(p); err != nil {
			return
		}
	}
	return
}
