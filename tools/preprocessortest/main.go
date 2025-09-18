/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"os"
	"time"

	// Embed tzdata so that we don't rely on potentially broken timezone DBs on the host
	_ "time/tzdata"

	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/processors"
	"github.com/gravwell/gravwell/v3/ingesters/utils"
)

var (
	configPath = flag.String("config-path", "", "Path to the preprocessor configuration")
	dataPath   = flag.String("data-path", "", "Optional path to data export file")
	fmtF       = flag.String("import-format", "", "Set the import file format manually")
	verbose    = flag.Bool("verbose", false, "Print each entry as its processed")
)

func main() {
	flag.Parse()
	var rdr utils.ReimportReader
	var ps *processors.ProcessorSet
	var ew *entWriter
	var err error
	if *configPath == `` {
		fmt.Println("missing config-path")
		os.Exit(1)
	}
	if config_data, err := os.ReadFile(*configPath); err != nil {
		fmt.Printf("Failed to load plugin config file %q: %v\n", *configPath, err)
		os.Exit(1)
	} else if ew, ps, err = loadConfig(config_data); err != nil {
		fmt.Printf("Failed to load plugin config: %v\n", err)
		os.Exit(1)
	}

	if *dataPath != `` {
		var format string
		var fin *os.File
		if format, err = utils.GetImportFormat(*fmtF, *dataPath); err != nil {
			fmt.Printf("%v, please set -import-format\n", err)
			os.Exit(1)
		} else if fin, err = os.Open(*dataPath); err != nil {
			fmt.Printf("Failed to open data file %s: %v\n", *dataPath, err)
			os.Exit(1)
		} else if rdr, err = utils.GetImportReader(format, fin, ew.tth); err != nil {
			fmt.Printf("Failed to determine data format: %v\n", err)
			os.Exit(1)
		}
		defer fin.Close()
	}
	start := time.Now()
	var input int
	if rdr == nil {
		fmt.Println("no data file provided")
	} else {
		//start iterating through the data file providing entries to the plugin
		// we randomly provide blocks and single entries
		for ents, err := popEnts(rdr); ents != nil && err == nil; ents, err = popEnts(rdr) {
			if err = ps.ProcessBatch(ents); err != nil {
				fmt.Printf("plugin returned error: %v\n", err)
				break
			}
			input += len(ents)
		}
		if err != nil {
			fmt.Println("data file contains invalid data, reader go", err)
		}
	}

	if err := ps.Close(); err != nil {
		fmt.Println("failed to close and flush processors", err)
		return
	}

	dur := time.Since(start)
	fmt.Printf("INPUT: %d\n", input)
	fmt.Printf("OUTPUT: %d\n", ew.count)
	fmt.Println("PROCESSING TIME:", dur)
	fmt.Println("PROCESSING RATE:", ingest.HumanEntryRate(uint64(input), dur))
}

func popEnts(rdr utils.ReimportReader) (ents []*entry.Entry, err error) {
	cnt := rand.Intn(15) + 1
	for i := 0; i < cnt; i++ {
		var ent *entry.Entry
		if ent, err = rdr.ReadEntry(); err == nil {
			ents = append(ents, ent)
		} else if err == io.EOF {
			err = nil
			return
		} else {
			return
		}
	}
	return
}

func loadConfig(cnt []byte) (ew *entWriter, ps *processors.ProcessorSet, err error) {
	var cfg testConfig
	//load it up and make sure there is exactly one preprocessor config defined
	if err = config.LoadConfigBytes(&cfg, cnt); err != nil {
		return
	} else if err = cfg.Preprocessor.Validate(); err != nil {
		return
	}
	ew = &entWriter{
		tth: newTestTagHandler(),
	}
	ps = processors.NewProcessorSet(ew)
	return
}

type globalConfig struct {
	Preprocessor []string
}

type testConfig struct {
	Global       globalConfig
	Preprocessor processors.ProcessorConfig
}

type testTagHandler struct {
	overrideTag bool
	override    entry.EntryTag
	mp          map[string]entry.EntryTag
}

func newTestTagHandler() *testTagHandler {
	return &testTagHandler{
		mp: map[string]entry.EntryTag{},
	}
}

func (tth *testTagHandler) OverrideTags(v entry.EntryTag) {
	tth.overrideTag = true
	tth.override = v
}

func (tth *testTagHandler) NegotiateTag(v string) (entry.EntryTag, error) {
	return tth.GetTag(v)
}

func (tth *testTagHandler) GetTag(v string) (r entry.EntryTag, err error) {
	if err = ingest.CheckTag(v); err != nil {
		return
	}
	var ok bool
	if r, ok = tth.mp[v]; !ok {
		r = entry.EntryTag(len(tth.mp))
		if tth.mp == nil {
			tth.mp = map[string]entry.EntryTag{}
		}
		tth.mp[v] = r
	}
	return
}

func (tth *testTagHandler) LookupTag(tag entry.EntryTag) (r string, ok bool) {
	for k, v := range tth.mp {
		if v == tag {
			r, ok = k, true
			break
		}
	}
	return
}

func (tth *testTagHandler) KnownTags() (r []string) {
	if len(tth.mp) > 0 {
		r = make([]string, 0, len(tth.mp))
		for k := range tth.mp {
			r = append(r, k)
		}
	}
	return
}

type entWriter struct {
	tth   *testTagHandler
	count uint64
	bytes uint64
}

func (ew *entWriter) WriteEntry(ent *entry.Entry) (err error) {
	if ent != nil {
		if *verbose {
			r, ok := ew.tth.LookupTag(ent.Tag)
			if !ok {
				r = `UNKNOWN`
			}
			fmt.Printf("%s (%v)\n\t%s\n", r, ent.TS, string(ent.Data))
			if cnt := ent.EVB.Count(); cnt > 0 {
				fmt.Printf("\tEnumeratedValues (%d):\n", cnt)
				for _, ev := range ent.EVB.Values() {
					fmt.Printf("\t\t%s - %s\n", ev.Name, ev.Value.String())
				}
			}
		}
		ew.count++
		ew.bytes = ent.Size()
	}
	return
}

func (ew *entWriter) WriteEntryContext(ctx context.Context, ent *entry.Entry) error {
	return ew.WriteEntry(ent) // we are just writing to stdout, no need for breakout here
}

func (ew *entWriter) WriteBatch(ents []*entry.Entry) error {
	for _, ent := range ents {
		if err := ew.WriteEntry(ent); err != nil {
			return err
		}
	}
	return nil
}

func (ew *entWriter) WriteBatchContext(ctx context.Context, ents []*entry.Entry) error {
	return ew.WriteBatch(ents) // we are just writing to stdout, no need for breakout here
}
