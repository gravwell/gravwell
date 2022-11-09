/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/timegrinder"
)

const (
	maxFileHandles int64 = 2048
	mask           int64 = ^0x7FFF
)

var (
	nl    = []byte("\n")
	nilbs = []byte{}
)

type fileMultiplexer struct {
	fmap  map[int64]*os.File
	wdir  string
	total int64
}

type updater struct {
	total int64
	last  int64
}

func newUpdater(total int64) *updater {
	return &updater{
		total: total,
	}
}

func (u *updater) update(c int64) {
	if (c*100)/u.total != u.last {
		u.last = (c * 100) / u.total
		fmt.Printf("\b\b\b\b\b\b\b\b\b\b\b\b\b\b\b\b%d%%", u.last)
	}
}

// this is used when there isn't enough working RAM to do everything
func groupLargeLogs(src, wrk string, totalSize int64) error {
	fmt.Println("Running pre-optimization phase 1/2...")
	start := time.Now()
	fm := newfm(wrk)
	ud := newUpdater(totalSize)
	tcfg := timegrinder.Config{
		EnableLeftMostSeed: true,
	}
	tg, err := timegrinder.NewTimeGrinder(tcfg)
	if err != nil {
		return err
	}

	if *tzo != `` {
		err = tg.SetTimezone(*tzo)
		if err != nil {
			return err
		}
	}

	//walk the files
	if err := filepath.Walk(src, func(p string, fi os.FileInfo, err error) error {
		var lastTS time.Time
		if !fi.Mode().IsRegular() {
			return nil
		}
		fin, err := os.Open(p)
		if err != nil {
			return err
		}
		scn := bufio.NewScanner(fin)
		for scn.Scan() {
			v := scn.Bytes()
			if len(v) == 0 {
				continue
			}
			//extract the ts
			ts, ok, err := tg.Extract(v)
			if err != nil {
				return err
			}
			if !ok {
				ts = lastTS
			}
			if err := fm.writeLine(int64(ts.Second()), v); err != nil {
				return err
			}
			ud.update(fm.total)
		}
		if err := fin.Close(); err != nil {
			return err
		}
		return nil
	}); err != nil {
		fm.Close()
		return err
	}

	fmt.Println("\nFinished pre-optimization phase 1/2 in", time.Since(start))
	return nil
}

func newfm(wdir string) *fileMultiplexer {
	return &fileMultiplexer{
		fmap: make(map[int64]*os.File, maxFileHandles),
		wdir: wdir,
	}
}

func (fm *fileMultiplexer) Close() error {
	for k, v := range fm.fmap {
		if err := v.Close(); err != nil {
			return err
		}
		delete(fm.fmap, k)
	}
	return nil
}

func (fm *fileMultiplexer) writeLine(ts int64, ln []byte) error {
	//check if we have the file handle for this one
	fID := (ts & mask)
	f, ok := fm.fmap[fID]
	if !ok {
		if int64(len(fm.fmap)) >= maxFileHandles {
			if err := fm.trimFileHandles(); err != nil {
				return err
			}
		}
		var err error
		f, err = os.OpenFile(path.Join(fm.wdir, strconv.FormatInt(fID, 16)),
			os.O_CREATE|os.O_RDWR, 0666)
		if err != nil {
			return err
		}
		if _, err := f.Seek(0, 2); err != nil {
			return err
		}
		fm.fmap[fID] = f
	}
	n, err := f.Write(ln)
	if err != nil {
		return err
	}
	if _, err := f.Write(nl); err != nil {
		return err
	}
	fm.total += int64(n + 1)
	return nil
}

// basically just closes 16 random file handles, no optimization here
// we are relying on the fact that a map ranges in kindof random order
func (fm *fileMultiplexer) trimFileHandles() error {
	var c int
	for k, v := range fm.fmap {
		if err := v.Close(); err != nil {
			return err
		}
		delete(fm.fmap, k)
		c++
		if c >= 16 {
			break
		}
	}
	return nil
}

type ent struct {
	ts   entry.Timestamp
	data []byte
}

type entsFunc func(string, []ent) error

func optimizeGroups(dir string, totalSize int64, iv *ingestVars) error {
	if iv != nil {
		fmt.Println("Starting final optimization phase with ingest...")
	} else {
		fmt.Println("Starting pre-optimization phase 2/2...")
	}
	start := time.Now()
	errCh := make(chan error, 1)
	entsCh := make(chan []ent, 1)
	go func(errC chan error, outC chan []ent) {
		errCh <- walkAndReadFiles(dir, totalSize, iv, func(p string, ents []ent) error {
			var err error
			sort.Sort(sents(ents))
			if iv != nil {
				outC <- ents
			} else {
				err = writebackEntryGroup(p, ents)
			}
			if err != nil {
				return err
			}
			return nil
		})
	}(errCh, entsCh)

consumeLoop:
	for {
		select {
		case err := <-errCh:
			if err != nil {
				return err
			}
			break consumeLoop
		case ents := <-entsCh:
			if err := ingestEntryGroup(iv, ents); err != nil {
				return err //process exits on error, so everything will shut down
			}
		}
	}

	if iv != nil {
		fmt.Println("Finished final optimizaton and ingest in", time.Since(start))
	} else {
		fmt.Println("\nFinished pre-optimization phase 2/2 in", time.Since(start))
	}
	return nil
}

// here we assume the working set is alrady optimized, so we read them in and ingest directly
// without sorting first
func ingestFromFiles(dir string, totalSize int64, iv *ingestVars) error {
	fmt.Println("Starting pre-optimized file ingest...")
	start := time.Now()
	errCh := make(chan error, 1)
	entsCh := make(chan []ent, 1)
	go func(errC chan error, outC chan []ent) {
		errCh <- walkAndReadFiles(dir, totalSize, iv, func(p string, ents []ent) error {
			if iv == nil {
				return errors.New("No ingester active")
			}
			outC <- ents
			return nil
		})
	}(errCh, entsCh)

consumeLoop:
	for {
		select {
		case err := <-errCh:
			if err != nil {
				return err
			}
			break consumeLoop
		case ents := <-entsCh:
			if err := ingestEntryGroup(iv, ents); err != nil {
				return err
			}
		}
	}
	fmt.Println("\nFinished optimized file ingest in", time.Since(start))
	return nil
}

func walkAndReadFiles(dir string, totalSize int64, iv *ingestVars, f entsFunc) error {
	tcfg := timegrinder.Config{
		EnableLeftMostSeed: true,
	}
	tg, err := timegrinder.NewTimeGrinder(tcfg)
	if err != nil {
		return err
	}
	if *tzo != `` {
		err = tg.SetTimezone(*tzo)
		if err != nil {
			return err
		}
	}
	ud := newUpdater(totalSize)
	var tally int64
	//walk the files
	if err := filepath.Walk(dir, func(p string, fi os.FileInfo, err error) error {
		var lastTS time.Time
		if !fi.Mode().IsRegular() {
			return nil
		}
		var ents []ent
		fin, err := os.Open(p)
		if err != nil {
			return err
		}
		scn := bufio.NewScanner(fin)
		for scn.Scan() {
			v := scn.Bytes()
			if len(v) == 0 {
				continue
			}
			//extract the ts
			ts, ok, err := tg.Extract(v)
			if err != nil {
				return err
			}
			if !ok {
				ts = lastTS
			}
			ents = append(ents, ent{
				ts:   entry.FromStandard(ts),
				data: append(nilbs, v...),
			})
		}
		if err := fin.Close(); err != nil {
			return err
		}
		if err := f(p, ents); err != nil {
			return err
		}
		tally += fi.Size()
		ud.update(tally)
		return nil
	}); err != nil {
		return err
	}
	return nil
}

func ingestEntryGroup(iv *ingestVars, ents []ent) error {
	for i := range ents {
		if err := iv.m.WriteEntry(&entry.Entry{
			TS:   ents[i].ts,
			SRC:  iv.src,
			Tag:  iv.tag,
			Data: ents[i].data,
		}); err != nil {
			return err
		}
	}
	return nil
}

func writebackEntryGroup(p string, ents []ent) error {
	fout, err := os.Create(p)
	if err != nil {
		return err
	}
	wtr := bufio.NewWriter(fout)
	for i := range ents {
		if _, err := wtr.Write(ents[i].data); err != nil {
			fout.Close()
			return err
		}
		if _, err := wtr.Write(nl); err != nil {
			fout.Close()
			return err
		}
	}
	if err := wtr.Flush(); err != nil {
		fout.Close()
		return err
	}

	return fout.Close()
}

type sents []ent

func (s sents) Len() int           { return len(s) }
func (s sents) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
func (s sents) Less(i, j int) bool { return s[i].ts.Before(s[j].ts) }
