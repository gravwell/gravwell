package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime/debug"
	"runtime/pprof"
	"sync"
	"syscall"

	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/config/validate"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingesters/utils"
	"github.com/gravwell/gravwell/v3/ingesters/version"
)

const (
	defaultConfigLoc = `/opt/gravwell/etc/collectd.conf`
	ingesterName     = `collectd`
	appName          = `gravwell_collectd`
)

var (
	cpuprofile     = flag.String("cpuprofile", "", "write cpu profile to file")
	confLoc        = flag.String("config-file", defaultConfigLoc, "Override location for configuration file")
	verbose        = flag.Bool("v", false, "Display verbose status updates to stdout")
	ver            = flag.Bool("version", false, "Print the version information and exit")
	stderrOverride = flag.String("stderr", "", "Redirect stderr to a shared memory file")
	v              bool
	lg             *log.Logger
)

type instance struct {
	name string
	inst *collectdInstance
}

func init() {
	flag.Parse()
	if *ver {
		version.PrintVersion(os.Stdout)
		ingest.PrintVersion(os.Stdout)
		os.Exit(0)
	}
	lg = log.New(os.Stderr) // DO NOT close this, it will prevent backtraces from firing
	lg.SetAppname(appName)
	if *stderrOverride != `` {
		if oldstderr, err := syscall.Dup(int(os.Stderr.Fd())); err != nil {
			lg.Fatal("Failed to dup stderr: %v\n", err)
		} else {
			lg.AddWriter(os.NewFile(uintptr(oldstderr), "oldstderr"))
		}

		fp := filepath.Join(`/dev/shm/`, *stderrOverride)
		fout, err := os.Create(fp)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to create %s: %v\n", fp, err)
		} else {
			version.PrintVersion(fout)
			ingest.PrintVersion(fout)
			//file created, dup it
			if err := syscall.Dup2(int(fout.Fd()), int(os.Stderr.Fd())); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to dup2 stderr: %v\n", err)
				fout.Close()
			}
		}
	}
	v = *verbose
	validate.ValidateConfig(GetConfig, *confLoc)
}

func main() {
	debug.SetTraceback("all")
	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			lg.Fatal("Failed to open %s for profile file: %v\n", *cpuprofile, err)
		}
		defer f.Close()
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}

	cfg, err := GetConfig(*confLoc)
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

	lmt, err := cfg.RateLimit()
	if err != nil {
		lg.FatalCode(0, "Failed to get rate limit from configuration: %v\n", err)
		return
	}
	debugout("Rate limiting connection to %d bps\n", lmt)

	//fire up the ingesters
	debugout("INSECURE skipping TLS verification: %v\n", cfg.InsecureSkipTLSVerification())
	id, ok := cfg.IngesterUUID()
	if !ok {
		lg.FatalCode(0, "Couldn't read ingester UUID\n")
	}
	igCfg := ingest.UniformMuxerConfig{
		IngestStreamConfig: cfg.IngestStreamConfig,
		Destinations:       conns,
		Tags:               tags,
		Auth:               cfg.Secret(),
		LogLevel:           cfg.LogLevel(),
		VerifyCert:         !cfg.InsecureSkipTLSVerification(),
		IngesterName:       ingesterName,
		IngesterVersion:    version.GetVersion(),
		IngesterUUID:       id.String(),
		IngesterLabel:      cfg.Label,
		RateLimitBps:       lmt,
		Logger:             lg,
		CacheDepth:         cfg.Cache_Depth,
		CachePath:          cfg.Ingest_Cache_Path,
		CacheSize:          cfg.Max_Ingest_Cache,
		CacheMode:          cfg.Cache_Mode,
		LogSourceOverride:  net.ParseIP(cfg.Log_Source_Override),
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

	// prepare the configuration we're going to send upstream
	err = igst.SetRawConfiguration(cfg)
	if err != nil {
		lg.FatalCode(0, "Failed to set configuration for ingester state messages\n")
	}

	//get our collectors built up
	wg := &sync.WaitGroup{}
	ccBase := collConfig{
		wg:   wg,
		igst: igst,
	}

	var instances []instance

	for k, v := range cfg.Collector {
		cc := ccBase
		//resolve tags for each collector
		overrides, err := v.getOverrides()
		if err != nil {
			lg.Fatal("%s failed to get overrides: %v", k, err)
		}
		if cc.defTag, err = igst.GetTag(v.Tag_Name); err != nil {
			lg.Fatal("%s failed to resolve tag %s: %v", k, v.Tag_Name, err)
		}

		if cc.srcOverride, err = v.srcOverride(); err != nil {
			lg.Fatal("%s Source-Override %s error: %v", k, v.Source_Override, err)
		}
		if cc.proc, err = cfg.Preprocessor.ProcessorSet(igst, v.Preprocessor); err != nil {
			lg.Fatal("%s Preprocessors are invalid: %v", k, err)
		}

		cc.src = nil

		cc.overrides = map[string]entry.EntryTag{}
		for plugin, tagname := range overrides {
			tagid, err := igst.GetTag(tagname)
			if err != nil {
				lg.Fatal("%s failed to resolve tag %s: %v", k, tagname, err)
			}
			cc.overrides[plugin] = tagid
		}

		//populate the creds and sec level for each collector
		cc.pl, cc.seclevel = v.creds()

		//build out UDP listeners and register them
		laddr, err := v.udpAddr()
		if err != nil {
			lg.Fatal("%s failed to resolve udp address: %v", k, err)
		}
		inst, err := newCollectdInstance(cc, laddr)
		if err != nil {
			lg.Fatal("%s failed to create a new collector: %v", k, err)
		}
		if err := inst.Start(); err != nil {
			lg.Fatal("%s failed to start collector: %v", k, err)
		}
		instances = append(instances, instance{name: k, inst: inst})
	}

	//listen for the stop signal so we can die gracefully
	utils.WaitForQuit()

	//ask that everything close
	for i := range instances {
		if err := instances[i].inst.Close(); err != nil {
			lg.Error("%s failed to close: %v", instances[i].name, err)
		}
	}

	if err := igst.Close(); err != nil {
		lg.Error("failed to close the ingest muxer: %v", err)
	}

	lg.Info("collectd ingester exiting")
}
