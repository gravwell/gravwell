package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime/debug"
	"sync"
	"syscall"
	"time"

	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/config/validate"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingesters/utils"
	"github.com/gravwell/gravwell/v3/ingesters/utils/caps"
	"github.com/gravwell/gravwell/v3/ingesters/version"
)

const (
	defaultConfigLoc  = `/opt/gravwell/etc/collectd.conf`
	defaultConfigDLoc = `/opt/gravwell/etc/collectd.conf.d`
	ingesterName      = `collectd`
	appName           = `collectd`
)

var (
	confLoc        = flag.String("config-file", defaultConfigLoc, "Override location for configuration file")
	confdLoc       = flag.String("config-overlays", defaultConfigDLoc, "Location for configuration overlay files")
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
			lg.Fatal("Failed to dup stderr", log.KVErr(err))
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
			log.PrintOSInfo(fout)
			//file created, dup it
			if err := syscall.Dup2(int(fout.Fd()), int(os.Stderr.Fd())); err != nil {
				fout.Close()
				lg.Fatal("Failed to dup2 stderr", log.KVErr(err))
			}
		}
	}
	v = *verbose
	validate.ValidateConfig(GetConfig, *confLoc, *confdLoc) //no overlays
}

func main() {
	debug.SetTraceback("all")
	cfg, err := GetConfig(*confLoc, *confdLoc)
	if err != nil {
		lg.FatalCode(0, "failed to get configuration", log.KVErr(err))
		return
	}

	if len(cfg.Log_File) > 0 {
		fout, err := os.OpenFile(cfg.Log_File, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0640)
		if err != nil {
			lg.FatalCode(0, "failed to open log file", log.KV("path", cfg.Log_File), log.KVErr(err))
		}
		if err = lg.AddWriter(fout); err != nil {
			lg.Fatal("failed to add a writer", log.KVErr(err))
		}
		if len(cfg.Log_Level) > 0 {
			if err = lg.SetLevelString(cfg.Log_Level); err != nil {
				lg.FatalCode(0, "invalid Log Level", log.KV("loglevel", cfg.Log_Level), log.KVErr(err))
			}
		}
	}

	tags, err := cfg.Tags()
	if err != nil {
		lg.FatalCode(0, "failed to get tags from configuration", log.KVErr(err))
	}
	conns, err := cfg.Targets()
	if err != nil {
		lg.FatalCode(0, "failed to get backend targets from configuration", log.KVErr(err))
	}
	debugout("Handling %d tags over %d targets\n", len(tags), len(conns))

	lmt, err := cfg.RateLimit()
	if err != nil {
		lg.FatalCode(0, "failed to get rate limit from configuration", log.KVErr(err))
		return
	}
	debugout("Rate limiting connection to %d bps\n", lmt)

	//fire up the ingesters
	debugout("INSECURE skipping TLS verification: %v\n", cfg.InsecureSkipTLSVerification())
	id, ok := cfg.IngesterUUID()
	if !ok {
		lg.FatalCode(0, "Couldn't read ingester UUID")
	}
	igCfg := ingest.UniformMuxerConfig{
		IngestStreamConfig: cfg.IngestStreamConfig,
		Destinations:       conns,
		Tags:               tags,
		Auth:               cfg.Secret(),
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
		lg.Fatal("failed build our ingest system", log.KVErr(err))
	}
	debugout("Started ingester muxer\n")
	if cfg.SelfIngest() {
		lg.AddRelay(igst)
	}
	if err := igst.Start(); err != nil {
		lg.Fatal("failed start our ingest system", log.KVErr(err))
	}
	defer igst.Close()

	//wait for something to go hot
	debugout("Waiting for connections to indexers ... ")
	if err := igst.WaitForHot(cfg.Timeout()); err != nil {
		lg.FatalCode(0, "timeout waiting for backend connections", log.KV("timeout", cfg.Timeout()), log.KVErr(err))
	}
	debugout("Successfully connected to ingesters\n")

	//check capabilities so we can scream and throw a potential warning upstream
	if !caps.Has(caps.NET_BIND_SERVICE) {
		lg.Warn("missing capability", log.KV("capability", "NET_BIND_SERVICE"), log.KV("warning", "may not be able to bind to service ports"))
		debugout("missing capability NET_BIND_SERVICE, may not be able to bind to service ports")
	}

	// prepare the configuration we're going to send upstream
	err = igst.SetRawConfiguration(cfg)
	if err != nil {
		lg.FatalCode(0, "failed to set configuration for ingester state messages", log.KVErr(err))
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
			lg.Fatal("failed to get overrides", log.KV("collector", k), log.KVErr(err))
		}
		if cc.defTag, err = igst.GetTag(v.Tag_Name); err != nil {
			lg.Fatal("failed to resolve tag", log.KV("collector", k), log.KV("tag", v.Tag_Name), log.KVErr(err))
		}

		if cc.srcOverride, err = v.srcOverride(); err != nil {
			lg.Fatal("invalid Source-Override", log.KV("collector", k), log.KV("sourceoverride", v.Source_Override), log.KVErr(err))
		}
		if cc.proc, err = cfg.Preprocessor.ProcessorSet(igst, v.Preprocessor); err != nil {
			lg.Fatal("preprocessor error", log.KV("collector", k), log.KV("preprocessor", v.Preprocessor), log.KVErr(err))
		}

		cc.src = nil

		cc.overrides = map[string]entry.EntryTag{}
		for plugin, tagname := range overrides {
			tagid, err := igst.GetTag(tagname)
			if err != nil {
				lg.Fatal("failed to resolve tag", log.KV("collector", k), log.KV("tag", v.Tag_Name), log.KVErr(err))
			}
			cc.overrides[plugin] = tagid
		}

		//populate the creds and sec level for each collector
		cc.pl, cc.seclevel = v.creds()

		//build out UDP listeners and register them
		laddr, err := v.udpAddr()
		if err != nil {
			lg.Fatal("failed to resolve udp address", log.KV("collector", k), log.KVErr(err))
		}
		inst, err := newCollectdInstance(cc, laddr)
		if err != nil {
			lg.Fatal("failed to create a new collector", log.KV("collector", k), log.KVErr(err))
		}
		if err := inst.Start(); err != nil {
			lg.Fatal("failed to start collector", log.KV("collector", k), log.KVErr(err))
		}
		instances = append(instances, instance{name: k, inst: inst})
	}

	//listen for the stop signal so we can die gracefully
	utils.WaitForQuit()

	//ask that everything close
	for i := range instances {
		if err := instances[i].inst.Close(); err != nil {
			lg.Fatal("failed to close collector", log.KV("collector", instances[i].name), log.KVErr(err))
		}
	}

	lg.Info("collectd ingester exiting", log.KV("ingesteruuid", id))
	if err := igst.Sync(time.Second); err != nil {
		lg.Error("failed to sync", log.KVErr(err))
	}
	if err := igst.Close(); err != nil {
		lg.Error("failed to close", log.KVErr(err))
	}
}
