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
)

const (
	defaultMaxCache = 512
	defaultLogLevel = `ERROR`
	minThrottle     = (1024 * 1024) / 8
)

const (
	envSecret      string = `GRAVWELL_INGEST_SECRET`
	envLogLevel    string = `GRAVWELL_LOG_LEVEL`
	envClearTarget string = `GRAVWELL_CLEARTEXT_TARGETS`
	envEncTarget   string = `GRAVWELL_ENCRYPTED_TARGETS`
	envPipeTarget  string = `GRAVWELL_PIPE_TARGETS`

	DefaultCleartextPort uint16 = 4023
	DefaultTLSPort       uint16 = 4024

	commentValue = `#`
	globalHeader = `[global]`
	headerStart  = `[`
	uuidParam    = `Ingester-UUID`
)

var (
	ErrNoConnections              = errors.New("No connections specified")
	ErrMissingIngestSecret        = errors.New("Ingest-Secret value missing")
	ErrInvalidLogLevel            = errors.New("Invalid Log Level")
	ErrInvalidConnectionTimeout   = errors.New("Invalid connection timeout")
	ErrInvalidIngestCacheSize     = errors.New("Invalid Max Ingest Cache size")
	ErrCacheEnabledZeroMax        = errors.New("Ingest cache enabled with zero Max Cache size")
	ErrGlobalSectionNotFound      = errors.New("Global config section not found")
	ErrInvalidLineLocation        = errors.New("Invalid line location")
	ErrInvalidUpdateLineParameter = errors.New("Update line location does not contain the specified paramter")
)

type IngestConfig struct {
	Ingest_Secret              string
	Connection_Timeout         string
	Verify_Remote_Certificates bool //legacy, will be removed
	Insecure_Skip_TLS_Verify   bool
	Cleartext_Backend_Target   []string
	Encrypted_Backend_Target   []string
	Pipe_Backend_Target        []string
	Ingest_Cache_Path          string
	Max_Ingest_Cache           int64 //maximum amount of data to cache in MB
	Log_Level                  string
	Log_File                   string
	Source_Override            string // override normal source if desired
	Rate_Limit                 string
	Ingester_UUID              string
}

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

	// Make sure the cache directory exists.
	cachedir := filepath.Dir(ic.Ingest_Cache_Path)
	fi, err = os.Stat(cachedir)
	if err != nil {
		if os.IsNotExist(err) {
			//try to make the directory
			err = os.MkdirAll(cachedir, 0700)
			if err != nil {
				return err
			}
		} else {
			return err
		}
	} else if !fi.IsDir() {
		return errors.New("Cache Location is not a directory")
	}

	//check the max cache
	if ic.Max_Ingest_Cache < 0 {
		return ErrInvalidIngestCacheSize
	} else if ic.Max_Ingest_Cache == 0 && len(ic.Ingest_Cache_Path) != 0 {
		return ErrCacheEnabledZeroMax
	}

	if ic.Source_Override != `` {
		if net.ParseIP(ic.Source_Override) == nil {
			return errors.New("Failed to parse Source_Override")
		}
	}
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

// SetIngesterUUID modifies the configuration file at loc, setting the
// Ingester-UUID parameter to the given UUID. This function allows ingesters
// to assign themselves a UUID if one is not given in the configuration file.
func (ic *IngestConfig) SetIngesterUUID(id uuid.UUID, loc string) (err error) {
	if zeroUUID(id) {
		return errors.New("UUID is empty")
	}
	var content string
	if content, err = reloadContent(loc); err != nil {
		return
	}
	//crack the config file into lines
	lines := strings.Split(content, "\n")
	lo := argInGlobalLines(lines, uuidParam)
	if lo == -1 {
		//UUID value not set, insert immediately after global
		gStart, _, ok := globalLineBoundary(lines)
		if !ok {
			err = ErrGlobalSectionNotFound
			return
		}
		lines, err = insertLine(lines, fmt.Sprintf(`%s="%s"`, uuidParam, id.String()), gStart+1)
	} else {
		//found it, update it
		lines, err = updateLine(lines, uuidParam, fmt.Sprintf(`"%s"`, id), lo)
	}
	if err != nil {
		return
	}
	ic.Ingester_UUID = id.String()
	content = strings.Join(lines, "\n")
	err = updateConfigFile(loc, content)
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
