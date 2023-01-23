/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package filewatch

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const (
	bName          string = `test`
	testMultiCount int    = 16
	WATCH_OVERFLOW int    = 16384 + 1 // plus 1 to overflow
)

var (
	stateFilePath string
	tempPath      = os.TempDir()
)

func TestNewWatcher(t *testing.T) {
	fname, err := newFileName()
	if err != nil {
		t.Fatal(err)
	}
	fm, err := NewWatcher(fname)
	if err != nil {
		cleanFile(fname, t)
		t.Fatal(err)
	}
	if err := fm.Close(); err != nil {
		cleanFile(fname, t)
		t.Fatal(err)
	}
	cleanFile(fname, t)
}

type addFunction func(string, *WatchManager) error
type runFunction func(string) error
type tailFunction func(*WatchManager) error

func fireWatcher(af addFunction, pf, rf runFunction, tf tailFunction, t *testing.T) {
	//get a working dir and temp state file
	workingDir, err := ioutil.TempDir(tempPath, `watched`)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(workingDir)
	stateFilePath, err = newFileName()
	if err != nil {
		os.RemoveAll(workingDir)
		t.Fatal(err)
	}
	defer os.RemoveAll(stateFilePath)
	w, err := NewWatcher(stateFilePath)
	if err != nil {
		os.RemoveAll(stateFilePath)
		os.RemoveAll(workingDir)
		t.Fatal(err)
	}
	if af != nil {
		//run the user function
		if err := af(workingDir, w); err != nil {
			w.Close()
			os.RemoveAll(stateFilePath)
			os.RemoveAll(workingDir)
			t.Fatal(err)
		}
	}

	if pf != nil {
		// run the preflight function
		if err := pf(workingDir); err != nil {
			w.Close()
			os.RemoveAll(stateFilePath)
			os.RemoveAll(workingDir)
			t.Fatal(err)
		}
	}

	//get things started
	if err := w.Start(); err != nil {
		w.Close()
		os.RemoveAll(stateFilePath)
		os.RemoveAll(workingDir)
		t.Fatal(err)
	}

	if rf != nil {
		//run the user run function
		if err := rf(workingDir); err != nil {
			w.Close()
			os.RemoveAll(stateFilePath)
			os.RemoveAll(workingDir)
			t.Fatal(err)
		}
	}
	if tf != nil {
		if err := tf(w); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(workingDir); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(stateFilePath); err != nil {
		t.Fatal(err)
	}
}

func TestSingleWatcher(t *testing.T) {
	lh := newSafeTrackingLH()
	var err error
	var res map[string]bool
	fireWatcher(func(workingDir string, w *WatchManager) error {
		watchCfg := WatchConfig{
			ConfigName: bName,
			BaseDir:    workingDir,
			FileFilter: `paco*`,
			Hnd:        lh,
		}
		//add in one filter
		if err := w.Add(watchCfg); err != nil {
			t.Fatal(err)
		}
		if w.Filters() != 1 {
			t.Fatal(errors.New("Filter not installed"))
		}
		return nil
	},
		nil,
		func(workingDir string) error {
			_, res, err = writeLines(filepath.Join(workingDir, `paco123`))
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 100; i++ {
				if lh.Len() == len(res) {
					break
				}
				time.Sleep(time.Millisecond * 10)
			}
			return nil
		}, func(wm *WatchManager) error {
			if err := wm.fman.FlushStates(); err != nil {
				return err
			}
			sts, err := ReadStateFile(stateFilePath)
			if err != nil {
				return err
			}
			if len(sts) != len(wm.fman.followers) {
				return fmt.Errorf("state file doesn't match %d != %d", len(sts), len(wm.fman.followers))
			}
			if len(sts) != len(wm.fman.states) {
				return errors.New("states doesn't match statefile")
			}
			for k, v := range wm.fman.states {
				if v == nil {
					return errors.New("invalid state value")
				}
				if sts[filepath.Join(k.FilePath, k.BaseName)] != *v {
					return fmt.Errorf("Invalid value for %v", k)
				}
			}
			return nil
		}, t)

	//check the results
	if len(res) != lh.Len() {
		t.Fatal("line handler failed to get all the lines", len(res), lh.Len())
	}
	for k := range res {
		if _, ok := lh.mp[k]; !ok {
			t.Fatal("missing line", k)
		}
	}
}

func TestMultiWatcherNoDelete(t *testing.T) {
	var res []map[string]bool
	var lhs []*safeTrackingLH

	fireWatcher(func(workingDir string, w *WatchManager) error {
		for i := 0; i < testMultiCount; i++ {
			lh := newSafeTrackingLH()
			lhs = append(lhs, lh)
			watchCfg := WatchConfig{
				ConfigName: bName,
				BaseDir:    workingDir,
				FileFilter: fmt.Sprintf(`test%dpaco*`, i),
				Hnd:        lh,
			}
			//add in one filter
			if err := w.Add(watchCfg); err != nil {
				t.Fatal(err)
			}
		}
		if w.Filters() != testMultiCount {
			t.Fatal(errors.New("All filters not installed"))
		}
		return nil
	},
		nil,
		func(workingDir string) error {
			//perform the writes
			for i := 0; i < testMultiCount; i++ {
				_, r, err := writeLines(filepath.Join(workingDir, fmt.Sprintf(`test%dpaco123`, i)))
				if err != nil {
					t.Fatal(err)
				}
				res = append(res, r)
			}

			var i int
			for i < 100 {
				//check all our lengths
				missed := false
				for j := 0; j < testMultiCount; j++ {
					if lhs[j].Len() != len(res[j]) {
						missed = true
						break
					}
				}
				if !missed {
					break
				}
				time.Sleep(10 * time.Millisecond)
				i++
			}
			if i >= 100 {
				return errors.New("timed out waiting for all lines")
			}
			return nil
		}, nil, t)

	//check the results
	for i := range lhs {
		if len(res[i]) != lhs[i].Len() {
			t.Fatal("line handler failed to get all the lines on", i)
		}
		for k := range res[i] {
			if _, ok := lhs[i].mp[k]; !ok {
				t.Fatal("missing line", i, k)
			}
		}
	}
}

func TestOverflow(t *testing.T) {
	lh := newSafeTrackingLH()
	fireWatcher(func(workingDir string, w *WatchManager) error {
		watchCfg := WatchConfig{
			ConfigName: bName,
			BaseDir:    workingDir,
			FileFilter: `paco*`,
			Hnd:        lh,
		}
		//add in one filter
		if err := w.Add(watchCfg); err != nil {
			t.Fatal(err)
		}
		if w.Filters() != 1 {
			t.Fatal(errors.New("Filter not installed"))
		}
		return nil
	}, func(workingDir string) error {
		// just touch enough files to overload the watcher
		for i := 0; i < WATCH_OVERFLOW; i++ {
			err := os.WriteFile(filepath.Join(workingDir, fmt.Sprintf("paco%v", i)), []byte("test"), 0644)
			if err != nil {
				t.Fatal(err)
			}
		}
		return nil
	}, func(workingDir string) error {
		// actually give the fs time to open/close 16k files...
		time.Sleep(10 * time.Second)
		return nil
	}, func(wm *WatchManager) error {
		if err := wm.fman.FlushStates(); err != nil {
			return err
		}
		sts, err := ReadStateFile(stateFilePath)
		if err != nil {
			return err
		}
		if len(sts) != WATCH_OVERFLOW {
			return fmt.Errorf("state file doesn't match %d != %d", len(sts), WATCH_OVERFLOW)
		}
		if len(sts) != len(wm.fman.states) {
			return errors.New("states doesn't match statefile")
		}
		for k, v := range wm.fman.states {
			if v == nil {
				return errors.New("invalid state value")
			}
			if sts[filepath.Join(k.FilePath, k.BaseName)] != *v {
				return fmt.Errorf("Invalid value for %v", k)
			}
		}
		return nil
	}, t)
}

func TestMultiWatcherWithOverlap(t *testing.T) {
	var res map[string]bool
	var lhs []*safeTrackingLH
	matcher := `paco`

	fireWatcher(func(workingDir string, w *WatchManager) error {
		for i := 0; i < testMultiCount; i++ {
			matcher = matcher + `1`
			lh := newSafeTrackingLH()
			lhs = append(lhs, lh)
			watchCfg := WatchConfig{
				ConfigName: fmt.Sprintf("%s%d", bName, i),
				BaseDir:    workingDir,
				FileFilter: matcher + `*`,
				Hnd:        lh,
			}
			//add in one filter
			if err := w.Add(watchCfg); err != nil {
				t.Fatal(err)
			}
		}
		if w.Filters() != testMultiCount {
			t.Fatal(errors.New("All filters not installed"))
		}
		return nil
	}, nil, func(workingDir string) error {
		//perform the writes on just one file, it will match everything
		_, r, err := writeLines(filepath.Join(workingDir, matcher))
		if err != nil {
			t.Fatal(err)
		}
		res = r
		var i int
		for i < 50 {
			//check all our lengths
			missed := false
			for j := 0; j < testMultiCount; j++ {
				if lhs[j].Len() < len(res) {
					missed = true
					break
				}
			}
			if !missed {
				break
			}
			time.Sleep(100 * time.Millisecond)
			i++
		}
		if i >= 50 {
			for j := 0; j < testMultiCount; j++ {
				fmt.Println(j, lhs[j].Len(), len(res))
			}

			return errors.New("timed out waiting for all lines")
		}
		return nil
	}, nil, t)

	//check the results
	for i := range lhs {
		if len(res) != lhs[i].Len() {
			for k := range res {
				if _, ok := lhs[i].mp[k]; !ok {
					fmt.Println("RECV missed", k)
				}
			}
			t.Fatal("line handler failed to get all the lines on", i, len(res), lhs[i].Len())
		}
		for k := range res {
			if _, ok := lhs[i].mp[k]; !ok {
				t.Fatal("missing line", i, k)
			}
		}
	}
}

func TestMultiWatcherWithDelete(t *testing.T) {
	var res []map[string]bool
	var lhs []*safeTrackingLH
	var counts []int

	fireWatcher(func(workingDir string, w *WatchManager) error {
		for i := 0; i < testMultiCount; i++ {
			lh := newSafeTrackingLH()
			lhs = append(lhs, lh)
			watchCfg := WatchConfig{
				ConfigName: bName,
				BaseDir:    workingDir,
				FileFilter: fmt.Sprintf(`test%dpaco*`, i),
				Hnd:        lh,
			}
			//add in one filter
			if err := w.Add(watchCfg); err != nil {
				t.Fatal(err)
			}
		}
		if w.Filters() != testMultiCount {
			t.Fatal(errors.New("All filters not installed"))
		}
		return nil
	}, nil, func(workingDir string) error {
		//perform the writes
		for i := 0; i < testMultiCount; i++ {
			fname := fmt.Sprintf(`test%dpaco123`, i)
			cnt, r, err := writeLines(filepath.Join(workingDir, fname))
			if err != nil {
				t.Fatal(err)
			}
			res = append(res, r)
			counts = append(counts, cnt)
		}
		var i int
		for i < 100 {
			//check all our lengths
			missed := false
			for j := 0; j < testMultiCount; j++ {
				if lhs[j].Len() != len(res[j]) {
					missed = true
					break
				}
			}
			if !missed {
				break
			}
			time.Sleep(10 * time.Millisecond)
			i++
		}
		if i >= 100 {
			return errors.New("timed out waiting for all lines")
		}
		//now delete each of the files
		for i := 0; i < testMultiCount; i++ {
			fname := fmt.Sprintf(`test%dpaco123`, i)
			if err := os.Remove(filepath.Join(workingDir, fname)); err != nil {
				return err
			}
		}
		return nil
	}, func(w *WatchManager) error {
		for i := 0; i < 100; i++ {
			if w.Followers() == 0 {
				return nil
			}
			time.Sleep(time.Millisecond * 10)
		}
		return fmt.Errorf("Deleted files not removed from followers: %d", w.Followers())
	}, t)

	//check the results
	for i := range lhs {
		if len(res[i]) != lhs[i].Len() {
			t.Fatal("line handler failed to get all the lines on", i)
		}
		for k := range res[i] {
			if _, ok := lhs[i].mp[k]; !ok {
				t.Fatal("missing line", i, k)
			}
		}
	}
}

func TestMultiWatcherWithMoveNoMatch(t *testing.T) {
	var res []map[string]bool
	var lhs []*safeTrackingLH
	var counts []int

	fireWatcher(func(workingDir string, w *WatchManager) error {
		for i := 0; i < testMultiCount; i++ {
			lh := newSafeTrackingLH()
			lhs = append(lhs, lh)
			watchCfg := WatchConfig{
				ConfigName: bName,
				BaseDir:    workingDir,
				FileFilter: fmt.Sprintf(`test%dpaco*`, i),
				Hnd:        lh,
			}
			//add in one filter
			if err := w.Add(watchCfg); err != nil {
				t.Fatal(err)
			}
		}
		if w.Filters() != testMultiCount {
			t.Fatal(errors.New("All filters not installed"))
		}
		return nil
	}, nil, func(workingDir string) error {
		//perform the writes
		for i := 0; i < testMultiCount; i++ {
			fname := fmt.Sprintf(`test%dpaco123`, i)
			cnt, r, err := writeLines(filepath.Join(workingDir, fname))
			if err != nil {
				t.Fatal(err)
			}
			res = append(res, r)
			counts = append(counts, cnt)
		}
		var i int
		for i < 100 {
			//check all our lengths
			missed := false
			for j := 0; j < testMultiCount; j++ {
				if lhs[j].Len() != len(res[j]) {
					missed = true
					break
				}
			}
			if !missed {
				break
			}
			time.Sleep(10 * time.Millisecond)
			i++
		}
		if i >= 100 {
			return errors.New("timed out waiting for all lines")
		}
		//now delete each of the files
		for i := 0; i < testMultiCount; i++ {
			fname := fmt.Sprintf(`test%dpaco123`, i)
			newname := fmt.Sprintf(`test%dchico123`, i)
			if err := os.Rename(filepath.Join(workingDir, fname), filepath.Join(workingDir, newname)); err != nil {
				return err
			}
		}
		return nil
	}, func(w *WatchManager) error {
		for i := 0; i < 100; i++ {
			if w.Followers() == 0 {
				return nil
			}
			time.Sleep(time.Millisecond * 10)
		}
		return fmt.Errorf("Renamed files not removed from followers: %d", w.Followers())
	}, t)

	//check the results
	for i := range lhs {
		if len(res[i]) != lhs[i].Len() {
			t.Fatal("line handler failed to get all the lines on", i)
		}
		for k := range res[i] {
			if _, ok := lhs[i].mp[k]; !ok {
				t.Fatal("missing line", i, k)
			}
		}
	}
}

func TestMultiWatcherWithMoveWithMatch(t *testing.T) {
	var res []map[string]bool
	var lhs []*safeTrackingLH
	var counts []int

	fireWatcher(func(workingDir string, w *WatchManager) error {
		for i := 0; i < testMultiCount; i++ {
			lh := newSafeTrackingLH()
			lhs = append(lhs, lh)
			watchCfg := WatchConfig{
				ConfigName: bName,
				BaseDir:    workingDir,
				FileFilter: fmt.Sprintf(`test%dpaco*`, i),
				Hnd:        lh,
			}
			//add in one filter
			if err := w.Add(watchCfg); err != nil {
				t.Fatal(err)
			}
		}
		if w.Filters() != testMultiCount {
			t.Fatal(errors.New("All filters not installed"))
		}
		return nil
	}, nil, func(workingDir string) error {
		//perform the writes
		for i := 0; i < testMultiCount; i++ {
			fname := fmt.Sprintf(`test%dpaco123`, i)
			cnt, r, err := writeLines(filepath.Join(workingDir, fname))
			if err != nil {
				t.Fatal(err)
			}
			res = append(res, r)
			counts = append(counts, cnt)
		}
		var i int
		for i < 100 {
			//check all our lengths
			missed := false
			for j := 0; j < testMultiCount; j++ {
				if lhs[j].Len() != len(res[j]) {
					missed = true
					break
				}
			}
			if !missed {
				break
			}
			time.Sleep(10 * time.Millisecond)
			i++
		}
		if i >= 100 {
			return errors.New("timed out waiting for all lines")
		}
		//now delete each of the files
		for i := 0; i < testMultiCount; i++ {
			fname := fmt.Sprintf(`test%dpaco123`, i)
			newname := fmt.Sprintf(`test%dpaco456`, i)
			if err := os.Rename(filepath.Join(workingDir, fname), filepath.Join(workingDir, newname)); err != nil {
				return err
			}
		}
		time.Sleep(100 * time.Millisecond)
		return nil
	}, func(w *WatchManager) error {
		for i := 0; i < 100; i++ {
			if w.Followers() == testMultiCount {
				return nil
			}
			time.Sleep(time.Millisecond * 10)
		}
		return fmt.Errorf("Renamed files not removed from followers: %d", w.Followers())
	}, t)

	//check the results
	for i := range lhs {
		if len(res[i]) != lhs[i].Len() {
			t.Fatal("line handler failed to get all the lines on", i)
		}
		for k := range res[i] {
			if _, ok := lhs[i].mp[k]; !ok {
				t.Fatal("missing line", i, k)
			}
		}
	}
}

func TestMultiWatcherWithMoveWithMatchNewFilter(t *testing.T) {
	var res []map[string]bool
	var lhs []*safeTrackingLH
	var counts []int

	fireWatcher(func(workingDir string, w *WatchManager) error {
		for i := 0; i < testMultiCount; i++ {
			lh := newSafeTrackingLH()
			lhs = append(lhs, lh)
			watchCfg := WatchConfig{
				ConfigName: bName,
				BaseDir:    workingDir,
				FileFilter: fmt.Sprintf(`test%dpaco*`, i),
				Hnd:        lh,
			}
			//add in one filter
			if err := w.Add(watchCfg); err != nil {
				t.Fatal(err)
			}
		}
		if w.Filters() != testMultiCount {
			t.Fatal(errors.New("All filters not installed"))
		}
		return nil
	}, nil, func(workingDir string) error {
		//perform the writes
		for i := 0; i < testMultiCount; i++ {
			fname := fmt.Sprintf(`test%dpaco123`, i)
			cnt, r, err := writeLines(filepath.Join(workingDir, fname))
			if err != nil {
				t.Fatal(err)
			}
			res = append(res, r)
			counts = append(counts, cnt)
		}
		var i int
		for i < 100 {
			//check all our lengths
			missed := false
			for j := 0; j < testMultiCount; j++ {
				if lhs[j].Len() != len(res[j]) {
					missed = true
					break
				}
			}
			if !missed {
				break
			}
			time.Sleep(10 * time.Millisecond)
			i++
		}
		if i >= 100 {
			return errors.New("timed out waiting for all lines")
		}
		//now delete each of the files
		for i := 0; i < testMultiCount; i++ {
			fname := fmt.Sprintf(`test%dpaco123`, i)
			newname := fmt.Sprintf(`test%dpaco456`, (i+1)%testMultiCount)
			if err := os.Rename(filepath.Join(workingDir, fname), filepath.Join(workingDir, newname)); err != nil {
				return err
			}
		}
		return nil
	}, func(w *WatchManager) error {
		for i := 0; i < 100; i++ {
			if w.Followers() == testMultiCount {
				return nil
			}
			time.Sleep(time.Millisecond * 10)
		}
		return fmt.Errorf("Renamed files not removed from followers: %d", w.Followers())
	}, t)

	//check the results
	for i := range lhs {
		//lines are going to be duplicated into each one, so res > lhs
		if len(res[i]) > lhs[i].Len() {
			t.Fatal("line handler failed to get all the lines on", i, len(res[i]), lhs[i].Len())
		}
		for k := range res[i] {
			if _, ok := lhs[i].mp[k]; !ok {
				t.Fatal("missing line", i, k)
			}
		}
	}
}

type safeTrackingLH struct {
	testTagger
	mp  map[string]time.Time
	cnt int
}

func newSafeTrackingLH() *safeTrackingLH {
	return &safeTrackingLH{
		mp: map[string]time.Time{},
	}
}

func (h *safeTrackingLH) HandleLog(b []byte, ts time.Time, fname string) error {
	if h.mp == nil {
		return errors.New("not ready")
	}
	if len(b) > 0 {
		h.mp[string(b)] = ts
		h.cnt++
	}
	return nil
}

func (h *safeTrackingLH) Len() int {
	return len(h.mp)
}
