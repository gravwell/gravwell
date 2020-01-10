/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package config

import (
	"bufio"
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
	if err := LoadEnvVarList(&ic.Cleartext_Backend_Target, envClearTarget); err != nil {
		return err
	}
	//Encrypted targets
	if err := LoadEnvVarList(&ic.Encrypted_Backend_Target, envEncTarget); err != nil {
		return err
	}
	//Pipe targets
	if err := LoadEnvVarList(&ic.Pipe_Backend_Target, envPipeTarget); err != nil {
		return err
	}
	return nil
}

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

func (ic *IngestConfig) InsecureSkipTLSVerification() bool {
	return ic.Insecure_Skip_TLS_Verify
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

// Attempts to read a value from environment variable named envName
// If there's nothing there, it attempt to append _FILE to the variable
// name and see if it contains a filename; if so, it reads the
// contents of the file into cnd.
func LoadEnvVar(cnd *string, envName, defVal string) error {
	if cnd == nil {
		return errors.New("Invalid argument")
	} else if len(*cnd) > 0 {
		return nil
	} else if len(envName) == 0 {
		return nil
	}
	*cnd = os.Getenv(envName)
	if *cnd != `` {
		// we read something out of the variable, return
		return nil
	}

	// Set default value
	*cnd = defVal

	// No joy in the environment variable, append _FILE and try
	filename := os.Getenv(fmt.Sprintf("%s_FILE", envName))
	if filename == `` {
		// Nothing, screw it, return the default value
		return nil
	}
	file, err := os.Open(filename)
	if err != nil {
		// they specified a file but we can't open it
		return err
	}
	defer file.Close()

	s := bufio.NewScanner(file)
	s.Scan()
	l := s.Text()
	if l == `` {
		// there was nothing in the file?
		return errors.New("Empty file or blank first line of file")
	}
	*cnd = l

	return nil
}

func LoadEnvVarList(lst *[]string, envName string) error {
	if lst == nil {
		return errors.New("Invalid argument")
	} else if len(*lst) > 0 {
		return nil
	} else if len(envName) == 0 {
		return nil
	}
	arg := os.Getenv(envName)
	if len(arg) == 0 {
		// Nothing in the env variable, let's try reading from a file
		filename := os.Getenv(fmt.Sprintf("%s_FILE", envName))
		if filename == `` {
			// Nothing, return
			return nil
		}
		file, err := os.Open(filename)
		if err != nil {
			// they specified a file but we can't open it
			return err
		}
		defer file.Close()

		s := bufio.NewScanner(file)
		s.Scan()
		l := s.Text()
		if l == `` {
			// there was nothing in the file?
			return errors.New("Empty file or blank first line of file")
		}
		arg = l
	}
	if bits := strings.Split(arg, ","); len(bits) > 0 {
		for _, b := range bits {
			if b = strings.TrimSpace(b); len(b) > 0 {
				*lst = append(*lst, b)
			}
		}
	}
	return nil
}
