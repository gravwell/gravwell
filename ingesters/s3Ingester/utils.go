package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/bmatcuk/doublestar/v4"
)

type matcher struct {
	patterns []string
}

func newMatcher(patterns []string) (*matcher, error) {
	for _, v := range patterns {
		if _, err := doublestar.Match(v, `foobar`); err != nil {
			return nil, fmt.Errorf("file pattern %q is invalid %v", v, err)
		}
	}
	return &matcher{
		patterns: patterns,
	}, nil
}

func (m *matcher) match(name string) (matched bool) {
	if m == nil {
		return
	}
	if m.patterns == nil {
		matched = true
	} else {
		for _, v := range m.patterns {
			if ok, err := doublestar.Match(v, name); err == nil && ok {
				matched = true
				break
			}
		}
	}
	return
}

func (m *matcher) addPattern(v string) (err error) {
	if _, err = filepath.Match(v, `foobar`); err == nil {
		m.patterns = append(m.patterns, v)
	}
	return
}

type objectTracker struct {
	sync.Mutex
	flushed   bool
	statePath string
	states    map[string]bucketObjects
}

type bucketObjects map[string]trackedObjectState

type trackedObjectState struct {
	Updated time.Time
	Size    int64
}

func NewObjectTracker(pth string) (ot *objectTracker, err error) {
	if pth == `` {
		err = errors.New("invalid path")
		return
	}
	states := map[string]bucketObjects{}
	var fin *os.File
	if fin, err = os.Open(pth); err != nil {
		if !os.IsNotExist(err) {
			return
		}
		//all good, just empty
		err = nil
	} else if err = json.NewDecoder(fin).Decode(&states); err != nil {
		fin.Close()
		err = fmt.Errorf("state file is corrupt %w", err)
		return
	} else if err = fin.Close(); err != nil {
		err = fmt.Errorf("failed to close state file %w", err)
		return
	}
	ot = &objectTracker{
		flushed:   true,
		statePath: pth,
		states:    states,
	}
	return
}

func (ot *objectTracker) Flush() (err error) {
	ot.Lock()
	if ot.flushed { //no need to flush
		ot.Unlock()
		return
	}
	bb := bytes.NewBuffer(nil)
	if err = json.NewEncoder(bb).Encode(ot.states); err == nil {
		tpath := ot.statePath + `.temp`
		if err = os.WriteFile(tpath, bb.Bytes(), 0660); err == nil {
			if err = os.Rename(tpath, ot.statePath); err != nil {
				err = fmt.Errorf("failed to update state file with temporary file: %w", err)
			} else {
				ot.flushed = true
			}
			//else all good

		} else {
			err = fmt.Errorf("failed to write temporary state file %w", err)
		}
	} else {
		err = fmt.Errorf("failed to encode states %w", err)
	}
	ot.Unlock()
	return
}

func (ot *objectTracker) Set(bucket, obj string, state trackedObjectState, forceFlush bool) (err error) {
	ot.Lock()
	bkt, ok := ot.states[bucket]
	if !ok || bkt == nil {
		bkt = bucketObjects{}
	}
	bkt[obj] = state
	ot.states[bucket] = bkt
	ot.flushed = false
	ot.Unlock()
	if forceFlush {
		err = ot.Flush()
	}
	return
}

func (ot *objectTracker) Get(bucket, obj string) (state trackedObjectState, ok bool) {
	var bkt bucketObjects
	ot.Lock()
	if bkt, ok = ot.states[bucket]; ok && bkt != nil {
		state, ok = bkt[obj]
	}
	ot.Unlock()
	return
}
