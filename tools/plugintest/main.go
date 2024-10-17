/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"os"
	"strings"
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
	configPath = flag.String("config-path", "", "Path to the plugin configuration")
	dataPath   = flag.String("data-path", "", "Optional path to data export file")
	fmtF       = flag.String("import-format", "", "Set the import file format manually")
	verbose    = flag.Bool("verbose", false, "Print each entry as its processed")
)

func main() {
	flag.Parse()
	var pc processors.PluginConfig
	var p *processors.Plugin
	var rdr utils.ReimportReader
	var vc *config.VariableConfig
	var err error
	if *configPath == `` {
		fmt.Println("missing config-path")
		os.Exit(1)
	}
	if config_data, err := ioutil.ReadFile(*configPath); err != nil {
		fmt.Printf("Failed to load plugin config file %q: %v\n", *configPath, err)
		os.Exit(1)
	} else if vc, err = loadPluginConfig(config_data); err != nil {
		fmt.Printf("Failed to load plugin config: %v\n", err)
		os.Exit(1)
	} else if pc, err = processors.PluginLoadConfig(vc); err != nil {
		fmt.Printf("Failed to PluginLoadConfig: %v\n", err)
		os.Exit(1)
	} else if p, err = processors.NewPluginProcessor(pc, &testTagHandler{}); err != nil {
		fmt.Printf("Failed to create plugin: %v\n", err)
		os.Exit(1)
	}

	if *dataPath != `` {
		th := &testTagHandler{}
		var format string
		var fin *os.File
		if format, err = utils.GetImportFormat(*fmtF, *dataPath); err != nil {
			fmt.Printf("%v, please set -import-format\n", err)
			os.Exit(1)
		} else if fin, err = os.Open(*dataPath); err != nil {
			fmt.Printf("Failed to open data file %s: %v\n", *dataPath, err)
			os.Exit(1)
		} else if rdr, err = utils.GetImportReader(format, fin, th); err != nil {
			fmt.Printf("Failed to determine data format: %v\n", err)
			os.Exit(1)
		}
		defer fin.Close()
	}
	start := time.Now()
	var input int
	var output int
	if rdr == nil {
		fmt.Println("no data file provided")
	} else {
		//start iterating through the data file providing entries to the plugin
		// we randomly provide blocks and single entries
		for ents, err := popEnts(rdr); ents != nil && err == nil; ents, err = popEnts(rdr) {
			set, err := p.Process(ents)
			if err != nil {
				fmt.Printf("plugin returned error: %v\n", err)
				break
			}
			input += len(ents)
			output += len(set)
			if *verbose {
				for _, ent := range set {
					fmt.Printf("%v\t%s\n", ent.TS, string(ent.Data))
				}
			}
		}
		if err != nil {
			fmt.Println("data file contains invalid data, reader go", err)
		}
	}

	if set := p.Flush(); len(set) > 0 {
		output += len(set)
		if *verbose {
			for _, ent := range set {
				fmt.Printf("%v\t%s\n", ent.TS, string(ent.Data))
			}
		}
	}

	if err = p.Close(); err != nil {
		fmt.Printf("Failed to close plugin: %v\n", err)
		os.Exit(1)
	}
	dur := time.Since(start)
	fmt.Printf("INPUT: %d\n", input)
	fmt.Printf("OUTPUT: %d\n", output)
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

func loadPluginConfig(cnt []byte) (r *config.VariableConfig, err error) {
	var cfg testConfig
	//load it up and make sure there is exactly one preprocessor config defined
	if err = config.LoadConfigBytes(&cfg, cnt); err != nil {
		return
	} else if cfg.count() != 1 {
		err = fmt.Errorf("plugin config does not contain exactly one plugin configuration: count %d", len(cfg.Preprocessor))
		return
	}
	//grab the preprocessor and check that the defined preprocessor is of type plugin
	ptype, vc, ok := cfg.pop()
	if !ok || vc == nil {
		err = fmt.Errorf("failed to pull plugin configuration")
		return
	} else if ptype != "plugin" {
		err = fmt.Errorf("Configuration stanza is of the wrong type: %q != plugin", ptype)
		return
	}
	r = vc
	return
}

type testConfig struct {
	Preprocessor map[string]*config.VariableConfig
}

func (tt testConfig) count() int {
	return len(tt.Preprocessor)
}

func (tt testConfig) pop() (ptype string, vc *config.VariableConfig, ok bool) {
	for k, v := range tt.Preprocessor {
		if v != nil {
			vc = v
			ok = true
			var err error
			if ptype, err = vc.GetString("type"); err != nil {
				ptype = strings.TrimSpace(strings.ToLower(ptype))
				delete(tt.Preprocessor, k)
				break
			}
		}
	}
	return
}

type testTagHandler struct {
	overrideTag bool
	override    entry.EntryTag
	mp          map[string]entry.EntryTag
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
