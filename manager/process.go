/*************************************************************************
* Copyright 2017 Gravwell, Inc. All rights reserved.
* Contact: <legal@gravwell.io>
*
* This software may be modified and distributed under the terms of the
* BSD 2-clause license. See the LICENSE file for details.
**************************************************************************/

package main

import (
	"errors"
	"io"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gravwell/gravwell/v3/ingest/log"
)

var (
	killTimeout = 10 * time.Second
)

type processManager struct {
	ProcessConfig
	sync.Mutex
	sync.WaitGroup
	die chan bool
}

func NewProcessManager(pc ProcessConfig) (*processManager, error) {
	return &processManager{
		ProcessConfig: pc,
	}, nil
}

func (pm *processManager) Close() error {
	pm.Lock()
	defer pm.Unlock()
	if pm.die == nil {
		return errors.New("Not running")
	}
	close(pm.die)
	pm.die = nil
	pm.WaitGroup.Wait()
	return nil
}

func (pm *processManager) Start() error {
	pm.Lock()
	defer pm.Unlock()
	if pm.die != nil {
		return errors.New("Already running")
	}
	pm.die = make(chan bool, 1)
	pm.Add(1)
	go pm.routine(pm.die)
	return nil
}

type exitstatus struct {
	code int
	err  error
}

func (pm *processManager) routine(die chan bool) {
	defer pm.Done()
	args, _ := split(pm.Exec)
	rstr := newRestarter(pm.ProcessConfig, pm.lg)
	exitCh := make(chan exitstatus, 1)
	defer close(exitCh)

	if pm.StartDelay > 0 {
		if died := interruptSleep(die, time.Duration(pm.StartDelay)*time.Second); died {
			return
		}
	}

	for {
		if died := rstr.RequestStart(die); died {
			break
		}
		attr := syscall.SysProcAttr{
			Setpgid: true,
		}
		if pm.UID > 0 || pm.GID > 0 {
			attr.Credential = &syscall.Credential{
				Uid: uint32(pm.UID),
				Gid: uint32(pm.GID),
			}
		}
		cmd := &exec.Cmd{
			Path:        args[0],
			Args:        args,
			Dir:         pm.WorkingDir,
			SysProcAttr: &attr,
		}
		pm.lg.Info("starting process", log.KV("name", pm.Name), log.KV("binary", args[0]), log.KV("args", args[1:]))
		go func(c *exec.Cmd, ec chan exitstatus) {
			var x exitstatus
			if x.err = c.Start(); x.err == nil {
				if x.err = c.Wait(); x.err != nil {
					if exiterr, ok := x.err.(*exec.ExitError); ok {
						if status, ok := exiterr.Sys().(syscall.WaitStatus); ok {
							x.code = status.ExitStatus()
						}
					}
				}
			}
			ec <- x
		}(cmd, exitCh)

		select {
		case <-die:
			//kill the process and wait for it to exit
			if cmd.Process != nil {
				pm.lg.Info("shutting down", log.KV("name", pm.Name))
				if err := requestKill(cmd, exitCh); err != nil {
					pm.lg.Error("failed to kill when exiting", log.KV("name", pm.Name), log.KVErr(err))
				}
			}
			return
		case status := <-exitCh:
			pm.lg.Info("process exited", log.KV("name", pm.Name), log.KV("code", status.code), log.KVErr(status.err))
			//this will just cycle and retry
			if status.code != 0 && pm.ErrHandler != `` {
				//fire of the crash report
				flds := strings.Fields(pm.ErrHandler)
				cmd = &exec.Cmd{
					Path: flds[0],
					Args: append(flds, pm.Name),
					Dir:  pm.WorkingDir,
				}
				if err := cmd.Run(); err != nil {
					pm.lg.Warn("crash handler failed", log.KV("name", pm.Name), log.KVErr(err))
				} else {
					pm.lg.Info("crash handler fired", log.KV("name", pm.Name))
				}
			}
		}
	}
}

func requestKill(cmd *exec.Cmd, exitCh chan exitstatus) (err error) {
	//first send the sigint signal
	if err = cmd.Process.Signal(syscall.SIGINT); err != nil {
		return
	}

	//make chan to signal exit
	//wait for up to 10 seconds
	timeout := time.After(killTimeout)
	select {
	case <-timeout:
		if err = cmd.Process.Kill(); err == nil {
			err = errors.New("Timed out, process killed")
		}
		<-exitCh
	case es, ok := <-exitCh:
		if ok {
			err = es.err
		}
	}
	return
}

type restarter struct {
	ProcessConfig
	rs  []time.Time
	lgr *log.Logger
}

func newRestarter(cfg ProcessConfig, l *log.Logger) restarter {
	return restarter{
		ProcessConfig: cfg,
		rs:            make([]time.Time, cfg.MaxRestarts),
		lgr:           l,
	}
}

func (r restarter) RequestStart(die chan bool) (shouldExit bool) {
	//check if we have exceeded our max restarts
	if d := r.shouldSleep(); d > 0 {
		if shouldExit = r.sleepit(die, d); shouldExit {
			return
		}
	}

	//add our time and shift
	r.shift()
	return
}

func (r restarter) sleepit(die chan bool, d time.Duration) (died bool) {
	if d <= 0 {
		return
	}
	r.lgr.Info("restarted too many times, sleeping", log.KV("name", r.Name), log.KV("duration", d))
	died = interruptSleep(die, d)
	return
}

func (r restarter) shift() {
	for i := (len(r.rs) - 1); i > 0; i-- {
		r.rs[i] = r.rs[i-1]
	}
	r.rs[0] = time.Now()
}

func (r restarter) shouldSleep() (d time.Duration) {
	//first startup, just skip
	if r.rs[0].IsZero() {
		return
	}

	//check if we are in a cooldown sleep
	oldestRestart := r.rs[len(r.rs)-1]
	if oldestRestart.IsZero() {
		return
	} else if time.Since(oldestRestart) < r.RestartPeriod {
		d = r.CooldownPeriod
		r.lgr.Info("restart cooldown", log.KV("elapsed", time.Since(oldestRestart)), log.KV("restartperiod", r.RestartPeriod))
	}
	return
}

func interruptSleep(dc chan bool, d time.Duration) (interrupted bool) {
	if d <= 0 {
		return
	}
	tmr := time.NewTimer(d)
	select {
	case <-tmr.C:
	case <-dc:
		interrupted = true
	}
	tmr.Stop()
	return
}

type discarder bool

func (d discarder) Close() error                { return nil }
func (d discarder) Write(b []byte) (int, error) { return len(b), nil }
func (d discarder) Read(b []byte) (int, error)  { return 0, io.EOF }
