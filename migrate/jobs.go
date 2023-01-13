/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
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
	"math/rand"
	"sync"
)

type job struct {
	id      int
	cf      context.CancelFunc
	updates chan string
	done    bool

	name         string
	latestUpdate string
}

func (j *job) IdString() string {
	return fmt.Sprintf("Job %d: %s", j.id, j.name)
}

func (j *job) LatestUpdate() string {
updateLoop:
	for {
		select {
		case u := <-j.updates:
			j.latestUpdate = u
		default:
			break updateLoop
		}
	}
	if j.done {
		return fmt.Sprintf("Done: %v", j.latestUpdate)
	}
	return j.latestUpdate
}

func (j *job) Cancel() {
	j.cf()
}

func (j *job) Done() bool {
	return j.done
}

type jobTracker struct {
	sync.Mutex
	jobs map[string]*job
	cfg  *cfgType
	id   int
}

func newJobTracker(cfg *cfgType) *jobTracker {
	return &jobTracker{jobs: map[string]*job{}, cfg: cfg}
}

func (t *jobTracker) StartSourcetypeScanJob(cfgName string) *job {
	t.Lock()
	defer t.Unlock()
	key := fmt.Sprintf("%s:%d", cfgName, rand.Int31())
	ctx, cf := context.WithCancel(context.Background())
	updateChan := make(chan string, 1000)
	infostr := fmt.Sprintf("Check sourcetypes on %s", cfgName)
	j := &job{cf: cf, updates: updateChan, id: t.id, name: infostr}
	t.jobs[key] = j
	t.id++
	go func() {
		err := checkMappings(cfgName, ctx, updateChan)
		if err != nil {
			lg.Warnf("Job returned %v", err)
			updateChan <- fmt.Sprintf("Job returned error: %v", err)
		}
		t.done(key)
	}()
	return j
}

func (t *jobTracker) StartFileJob(cfgName string) *job {
	t.Lock()
	defer t.Unlock()
	key := fmt.Sprintf("files:%s", cfgName)
	if j, ok := t.jobs[key]; ok {
		if !j.done {
			return nil
		}
	}
	ctx, cf := context.WithCancel(context.Background())
	updateChan := make(chan string, 1000)
	infostr := fmt.Sprintf("File config %s", cfgName)
	j := &job{cf: cf, updates: updateChan, id: t.id, name: infostr}
	t.jobs[key] = j
	t.id++
	go func() {
		err := fileJob(cfgName, ctx, updateChan)
		if err != nil {
			lg.Warnf("Job returned %v", err)
			updateChan <- fmt.Sprintf("Job returned error: %v", err)
		}
		t.done(key)
	}()
	return j
}

func (t *jobTracker) StartSplunkJob(cfgName string, progress SplunkToGravwell) *job {
	t.Lock()
	defer t.Unlock()
	key := fmt.Sprintf("%s:%s", cfgName, progress.key())
	if j, ok := t.jobs[key]; ok {
		if !j.done {
			return nil
		}
	}
	ctx, cf := context.WithCancel(context.Background())
	updateChan := make(chan string, 1000)
	infostr := fmt.Sprintf("Server %s index %s sourcetype %s", cfgName, progress.Index, progress.Sourcetype)
	j := &job{cf: cf, updates: updateChan, id: t.id, name: infostr}
	t.jobs[key] = j
	t.id++
	go func() {
		err := splunkJob(cfgName, progress, t.cfg, ctx, updateChan)
		if err != nil {
			lg.Warnf("Job returned %v", err)
			updateChan <- fmt.Sprintf("Job returned error: %v", err)
		}
		t.done(key)
	}()
	return j
}

func (t *jobTracker) done(key string) {
	t.Lock()
	defer t.Unlock()
	if j, ok := t.jobs[key]; ok {
		j.done = true
		t.jobs[key] = j
	}
}

func (t *jobTracker) Shutdown() {
	t.Lock()
	defer t.Unlock()
	for _, v := range t.jobs {
		v.Cancel()
	}
}

func (t *jobTracker) JobsDone() bool {
	t.Lock()
	defer t.Unlock()
	for _, v := range t.jobs {
		if !v.done {
			return false
		}
	}
	return true
}

func (t *jobTracker) ActiveJobs() int {
	var count int
	t.Lock()
	defer t.Unlock()
	for _, v := range t.jobs {
		if !v.done {
			count++
		}
	}
	return count
}

func (t *jobTracker) GetAllJobs() (jobs []*job) {
	t.Lock()
	defer t.Unlock()
	for _, v := range t.jobs {
		jobs = append(jobs, v)
	}
	return
}

func (t *jobTracker) GetJobById(id int) (j *job, err error) {
	t.Lock()
	defer t.Unlock()
	for _, v := range t.jobs {
		if v.id == id {
			return v, nil
		}
	}
	return nil, errors.New("Job not found")
}
