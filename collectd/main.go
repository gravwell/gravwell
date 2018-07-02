package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime/pprof"
	"sync"
	"syscall"

	"github.com/gravwell/ingest"
	"github.com/gravwell/ingest/entry"
	"github.com/gravwell/ingest/log"
	"github.com/gravwell/ingesters/version"
)

const (
	defaultConfigLoc = `/opt/gravwell/etc/collectd.conf`
	ingesterName     = `collectd`
)

var (
	cpuprofile     = flag.String("cpuprofile", "", "write cpu profile to file")
	configOverride = flag.String("config-file-override", "", "Override location for configuration file")
	verbose        = flag.Bool("v", false, "Display verbose status updates to stdout")
	ver            = flag.Bool("version", false, "Print the version information and exit")
	stderrOverride = flag.String("stderr", "", "Redirect stderr to a shared memory file")
	confLoc        string
	v              bool
	lg             *log.Logger
)

func init() {
	flag.Parse()
	if *ver {
		version.PrintVersion(os.Stdout)
		ingest.PrintVersion(os.Stdout)
		os.Exit(0)
	}
	if *stderrOverride != `` {
		fp := filepath.Join(`/dev/shm/`, *stderrOverride)
		fout, err := os.Create(fp)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to create %s: %v\n", fp, err)
		} else {
			//file created, dup it
			if err := syscall.Dup2(int(fout.Fd()), int(os.Stderr.Fd())); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to dup2 stderr: %v\n", err)
				fout.Close()
			}
		}
		version.PrintVersion(fout)
		ingest.PrintVersion(fout)
	}
	lg = log.New(os.Stderr) // DO NOT close this, it will prevent backtraces from firing

	if *configOverride == "" {
		confLoc = defaultConfigLoc
	} else {
		confLoc = *configOverride
	}
	v = *verbose
	connClosers = make(map[int]closer, 1)
}

func main() {
	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			lg.Fatal("Failed to open %s for profile file: %v\n", *cpuprofile, err)
		}
		defer f.Close()
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}

	cfg, err := GetConfig(confLoc)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get configuration: %v\n", err)
		return
	}

	if len(cfg.Log_File) > 0 {
		fout, err := os.OpenFile(cfg.Log_File, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0640)
		if err != nil {
			lg.FatalCode(0, "Failed to open log file %s: %v", cfg.Log_File, err)
		}
		if err = lg.AddWriter(fout); err != nil {
			lg.Fatal("Failed to add a writer: %v", err)
		}
		if len(cfg.Log_Level) > 0 {
			if err = lg.SetLevelString(cfg.Log_Level); err != nil {
				lg.FatalCode(0, "Invalid Log Level \"%s\": %v", cfg.Log_Level, err)
			}
		}
	}

	tags, err := cfg.Tags()
	if err != nil {
		lg.Fatal("Failed to get tags from configuration: %v\n", err)
	}
	conns, err := cfg.Targets()
	if err != nil {
		lg.Fatal("Failed to get backend targets from configuration: %v\n", err)
	}
	debugout("Handling %d tags over %d targets\n", len(tags), len(conns))

	//fire up the ingesters
	debugout("INSECURE skipping TLS verification: %v\n", cfg.InsecureSkipTLSVerification())
	igCfg := ingest.UniformMuxerConfig{
		Destinations: conns,
		Tags:         tags,
		Auth:         cfg.Secret(),
		LogLevel:     cfg.LogLevel(),
		VerifyCert:   !cfg.InsecureSkipTLSVerification(),
		IngesterName: ingesterName,
	}
	if cfg.EnableCache() {
		igCfg.EnableCache = true
		igCfg.CacheConfig.FileBackingLocation = cfg.LocalFileCachePath()
		igCfg.CacheConfig.MaxCacheSize = cfg.MaxCachedData()
	}
	igst, err := ingest.NewUniformMuxer(igCfg)
	if err != nil {
		lg.Fatal("Failed build our ingest system: %v\n", err)
	}
	debugout("Started ingester muxer\n")
	if err := igst.Start(); err != nil {
		lg.Fatal("Failed start our ingest system: %v\n", err)
	}
	defer igst.Close()

	//wait for something to go hot
	debugout("Waiting for connections to indexers ... ")
	if err := igst.WaitForHot(cfg.Timeout()); err != nil {
		lg.FatalCode(0, "Timedout waiting for backend connections: %v\n", err)
	}
	debugout("Successfully connected to ingesters\n")

	//get our collectors built up
	wg := &sync.WaitGroup{}
	cc := collConfig{
		wg:   wg,
		igst: igst,
	}

	for k, v := range cfg.Collector {
		//resolve tags for each collector
		overrides, err := v.getOverrides()
		if err != nil {
			lg.Fatal(k, "failed to get overrides", k, err)
		}
		if cc.defTag, err = igst.GetTag(v.Tag_Name); err != nil {
			lg.Fatal(k, "failed to resolve tag", v.Tag_Name, err)
		}

		cc.overrides = map[string]entry.EntryTag{}
		for plugin, tagname := range overrides {
			tagid, err := igst.GetTag(tagname)
			if err != nil {
				lg.Fatal(k, "failed to resolve tag", tagname, err)
			}
			cc.overrides[plugin] = tagid
		}

		//populate the creds and sec level for each collector
		cc.pl, cc.seclevel = v.creds()

		//build out UDP listeners and register them
		laddr, err := v.udpAddr()
		if err != nil {
			lg.Fatal(k, "failed to resolve udp address", err)
		}
		wtr, err := newCollectdInstance(cc, laddr)
		if err != nil {
			lg.Fatal(k, "failed to create a new collector", err)
		}
		addConn(wtr) //register our writer with the connection
		if err := wtr.Start(); err != nil {
			lg.Fatal("%s failed to start collector", k, err)
		}
	}

	//listen for the stop signal

	//ask that everything close
}
