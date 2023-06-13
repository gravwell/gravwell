/*************************************************************************
* Copyright 2017 Gravwell, Inc. All rights reserved.
* Contact: <legal@gravwell.io>
*
* This software may be modified and distributed under the terms of the
* BSD 2-clause license. See the LICENSE file for details.
**************************************************************************/

package main

import (
	"encoding/csv"
	"errors"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gravwell/gcfg"
	"github.com/gravwell/gravwell/v3/ingest/log"
)

const (
	defaultMaxRestarts          = 3
	defaultRestartPeriod        = 10
	defaultCooldownPeriod       = 60
	defaultLogLevel             = `WARN`
	serviceDisablePrefix        = `DISABLE_`
	errHandlerDisableEnv        = `DISABLE_ERROR_HANDLER`
	disableTrue                 = `true`
	maxConfigSize         int64 = 1024 * 1024 * 4
)

type processReadCfg struct {
	Exec            string //command to start
	Working_Dir     string //working directory
	Max_Restarts    int    //max restarts before cooldown engages
	Start_Delay     int    //max restarts before cooldown engages
	Restart_Period  int    //period in which the restarts can occur
	Cooldown_Period int    //in seconds
}

type ProcessConfig struct {
	Exec           string
	WorkingDir     string
	StartDelay     int
	MaxRestarts    int
	RestartPeriod  time.Duration
	CooldownPeriod time.Duration
	Name           string
	ErrHandler     string
	lg             *log.Logger
}

type errHandler struct {
	Exec string //path to binary to fire
}

type global struct {
	Log_File  string
	Log_Level string
}

type cfgType struct {
	Global        global
	Error_Handler errHandler
	Process       map[string]*processReadCfg
}

func GetConfig(path string) (c cfgType, err error) {
	var fin *os.File
	var fi os.FileInfo
	var data []byte

	if fin, err = os.Open(path); err != nil {
		return
	}
	defer fin.Close()
	if fi, err = fin.Stat(); err != nil {
		return
	}

	//This is just a sanity check
	if fi.Size() > maxConfigSize {
		err = errors.New("Config File Far too large")
		return
	}
	if data, err = ioutil.ReadAll(fin); err != nil {
		return
	}

	//read into the intermediary type to maintain backwards compatibility with the old system
	if err = gcfg.ReadStringInto(&c, string(data)); err != nil {
		return
	}
	c.CheckServiceDisable()
	err = c.Validate()
	return
}

// Validate the data we read in, e.g. is there good stuff there
func (c cfgType) Validate() error {
	if len(c.Process) == 0 {
		return errors.New("No processes specified")
	}
	for n, p := range c.Process {
		if len(n) == 0 {
			return errors.New("Process block missing name")
		}
		if strings.TrimSpace(p.Exec) == `` {
			return errors.New("Empty Exec-Statement")
		}
		if p.Max_Restarts < 0 {
			return errors.New("Invalid max restarts, must be > 0")
		}
		if p.Start_Delay < 0 {
			return errors.New("Invalid start delay, must be >= 0")
		}
		if p.Cooldown_Period < 0 {
			return errors.New("Invalid cooldown period, must be > 0")
		}
		if p.Restart_Period < 0 {
			return errors.New("Invalid cooldown period, must be > 0")
		}
	}
	if err := c.checkBinaries(); err != nil {
		return err
	}
	return nil
}

func (c *cfgType) CheckServiceDisable() {
	var envName string
	for k := range c.Process {
		//try with uppercase name
		envName = serviceDisablePrefix + strings.ToUpper(k)
		if v, ok := os.LookupEnv(envName); ok {
			if strings.ToLower(v) == disableTrue {
				delete(c.Process, k)
			}
			continue
		}
		//try with lower case name
		envName = serviceDisablePrefix + strings.ToLower(k)
		if v, ok := os.LookupEnv(envName); ok {
			if strings.ToLower(v) == disableTrue {
				delete(c.Process, k)
			}
			continue
		}
	}
	if v, ok := os.LookupEnv(errHandlerDisableEnv); ok && v == disableTrue {
		c.Error_Handler.Exec = ``
	}
}

func (c cfgType) checkBinaries() error {
	var v string
	if v = getFirst(c.Error_Handler.Exec); v != `` {
		if err := checkExecutable(v); err != nil {
			return err
		}
	}

	for _, n := range c.Process {
		flds, err := split(n.Exec)
		if err != nil {
			return err
		} else if len(flds) == 0 {
			return err
		}
		if err := checkExecutable(flds[0]); err != nil {
			return err
		}
	}
	return nil
}

func (c cfgType) ErrorHandler() (p string, ok bool) {
	if c.Error_Handler.Exec != `` {
		p = c.Error_Handler.Exec
		ok = true
	}
	return
}

func (c cfgType) ProcessConfigs(lg *log.Logger) (pc []ProcessConfig) {
	errExec, errExecActive := c.ErrorHandler()
	pc = make([]ProcessConfig, 0, len(c.Process))
	for k, v := range c.Process {
		if v == nil {
			continue
		}
		p := ProcessConfig{
			Name:       k,
			Exec:       v.Exec,
			WorkingDir: filepath.Clean(v.Working_Dir),
			lg:         lg,
		}
		if errExecActive {
			p.ErrHandler = errExec
		}
		if v.Max_Restarts <= 0 {
			p.MaxRestarts = defaultMaxRestarts
		} else {
			p.MaxRestarts = v.Max_Restarts
		}
		if v.Start_Delay > 0 {
			p.StartDelay = v.Start_Delay
		}
		if v.Restart_Period <= 0 {
			p.RestartPeriod = time.Minute * defaultRestartPeriod
		} else {
			p.RestartPeriod = time.Duration(v.Restart_Period) * time.Minute
		}
		if v.Cooldown_Period <= 0 {
			p.CooldownPeriod = time.Minute * defaultCooldownPeriod
		} else {
			p.CooldownPeriod = time.Duration(v.Cooldown_Period) * time.Minute
		}
		pc = append(pc, p)
	}
	return
}

func (c cfgType) GetLogger() (l *log.Logger, err error) {
	var ll log.Level
	if c.Global.Log_File == `` {
		l = log.NewDiscardLogger()
		return
	}
	if ll, err = log.LevelFromString(c.Global.Log_Level); err != nil {
		return
	}
	if ll == log.OFF {
		l = log.NewDiscardLogger()
		return
	}

	if l, err = log.NewFile(c.Global.Log_File); err != nil {
		return
	}
	err = l.SetLevel(ll)
	return
}

func getFirst(s string) string {
	flds := strings.Fields(strings.TrimSpace(s))
	if len(flds) > 0 {
		return flds[0]
	}
	return ``
}

// check that the file exists and has at least some executable bits set
func checkExecutable(p string) error {
	fi, err := os.Stat(p)
	if err != nil {
		return err
	}
	perm := fi.Mode().Perm()
	if (perm & 0x49) == 0 { //0111
		return errors.New(p + " Is not executable")
	}
	return nil
}

func split(s string) (ret []string, err error) {
	r := csv.NewReader(strings.NewReader(s))
	r.Comma = ' '
	r.Comment = '#'
	if ret, err = r.Read(); err == nil {
		ret = replaceEnvVars(ret)
	}
	return
}

func replaceEnvVars(set []string) (ret []string) {
	for _, val := range set {
		if len(val) > 0 {
			ret = append(ret, replaceEnv(val))
		}
	}
	return
}

func replaceEnv(v string) string {
	if len(v) >= 3 {
		if v[0] == '$' && v[1] == '{' && v[len(v)-1] == '}' {
			v = v[2 : len(v)-1]
			if bits := strings.Split(v, ":"); len(bits) == 2 {
				//try with a default
				if v = os.Getenv(bits[0]); v == `` {
					v = bits[1] // use the default
				}
			} else {
				v = os.Getenv(v)
			}
		}
	}
	return v
}
