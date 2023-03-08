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
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/config/validate"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingesters/utils"
	"github.com/gravwell/gravwell/v3/ingesters/utils/caps"
	"github.com/gravwell/gravwell/v3/ingesters/version"

	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcap"
)

const (
	defaultConfigLoc      = `/opt/gravwell/etc/network_capture.conf`
	defaultConfigDLoc     = `/opt/gravwell/etc/network_capture.conf.d`
	packetsThrowSize  int = 1024 * 1024 * 2
	ingesterName          = "networkLog"
	appName               = `networklog`
)

var (
	confLoc        = flag.String("config-file", defaultConfigLoc, "Location for configuration file")
	confdLoc       = flag.String("config-overlays", defaultConfigDLoc, "Location for configuration overlay files")
	verbose        = flag.Bool("v", false, "Display verbose status updates to stdout")
	stderrOverride = flag.String("stderr", "", "Redirect stderr to a shared memory file")
	ver            = flag.Bool("version", false, "Print the version information and exit")

	pktTimeout time.Duration = 500 * time.Millisecond

	totalPackets uint64
	totalBytes   uint64
	v            bool
	lg           *log.Logger
)

type results struct {
	Bytes uint64
	Count uint64
	Error error
}

type sniffer struct {
	name      string
	Promisc   bool
	Interface string
	TagName   string
	tag       entry.EntryTag
	SnapLen   int
	BPFFilter string
	handle    *pcap.Handle
	src       net.IP
	die       chan bool
	res       chan results
	active    bool
}

func init() {
	flag.Parse()
	if *ver {
		version.PrintVersion(os.Stdout)
		ingest.PrintVersion(os.Stdout)
		os.Exit(0)
	}
	validate.ValidateConfig(GetConfig, *confLoc, *confdLoc)
	lg = log.New(os.Stderr) // DO NOT close this, it will prevent backtraces from firing
	lg.SetAppname(appName)
	if *stderrOverride != `` {
		if oldstderr, err := syscall.Dup(int(os.Stderr.Fd())); err != nil {
			lg.Fatal("Failed to dup stderr", log.KVErr(err))
		} else {
			lg.AddWriter(os.NewFile(uintptr(oldstderr), "oldstderr"))
		}

		fp := path.Join(`/dev/shm/`, *stderrOverride)
		fout, err := os.Create(fp)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to create %s: %v\n", fp, err)
		} else {
			version.PrintVersion(fout)
			ingest.PrintVersion(fout)
			log.PrintOSInfo(fout)
			//file created, dup it
			if err := syscall.Dup3(int(fout.Fd()), int(os.Stderr.Fd()), 0); err != nil {
				fout.Close()
				lg.Fatal("failed to dup2 stderr", log.KVErr(err))
			}
		}
	}
	v = *verbose
}

func main() {
	debug.SetTraceback("all")
	cfg, err := GetConfig(*confLoc, *confdLoc)
	if err != nil {
		lg.FatalCode(0, "failed to get configuration", log.KVErr(err))
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
		lg.FatalCode(0, "could not read ingester UUID")
	}
	igCfg := ingest.UniformMuxerConfig{
		IngestStreamConfig: cfg.IngestStreamConfig,
		Destinations:       conns,
		Tags:               tags,
		Auth:               cfg.Secret(),
		IngesterName:       ingesterName,
		IngesterVersion:    version.GetVersion(),
		IngesterUUID:       id.String(),
		IngesterLabel:      cfg.Label,
		RateLimitBps:       lmt,
		VerifyCert:         !cfg.InsecureSkipTLSVerification(),
		CacheDepth:         cfg.Cache_Depth,
		CachePath:          cfg.Ingest_Cache_Path,
		CacheSize:          cfg.Max_Ingest_Cache,
		CacheMode:          cfg.Cache_Mode,
		Logger:             lg,
		LogSourceOverride:  net.ParseIP(cfg.Log_Source_Override),
	}
	igst, err := ingest.NewUniformMuxer(igCfg)
	if err != nil {
		lg.Fatal("failed build our ingest system", log.KVErr(err))
	}
	debugout("Started ingester muxer\n")
	// Henceforth, logs will also go out via the muxer to the gravwell tag
	if cfg.SelfIngest() {
		lg.AddRelay(igst)
	}
	if err := igst.Start(); err != nil {
		lg.Fatal("failed start our ingest system", log.KVErr(err))
	}

	debugout("Waiting for connections to indexers\n")
	if err := igst.WaitForHot(cfg.Timeout()); err != nil {
		lg.FatalCode(0, "timeout waiting for backend connections", log.KV("timeout", cfg.Timeout()), log.KVErr(err))
	}
	debugout("Successfully connected to ingesters\n")

	// prepare the configuration we're going to send upstream
	err = igst.SetRawConfiguration(cfg)
	if err != nil {
		lg.FatalCode(0, "failed to set configuration for ingester state message", log.KVErr(err))
	}

	//check capabilities so we can scream and throw a potential warning upstream
	if !caps.Has(caps.NET_RAW) {
		lg.Warn("missing capability", log.KV("capability", "NET_RAW"), log.KV("warning", "may not be able establish raw sockets"))
		debugout("missing capability NET_RAW, may not be able to establish raw sockets")
	} else if !caps.Has(caps.NET_ADMIN) {
		lg.Warn("missing capability", log.KV("capability", "NET_ADMIN"), log.KV("warning", "may not be able put device into promisc mode"))
		debugout("missing capability NET_ADMIN, may not be able to put device into promisc mode")
	}

	//loop through our sniffers and get a config up for each
	var sniffs []sniffer
	for k, v := range cfg.Sniffer {
		if v == nil {
			closeSniffers(sniffs)
			lg.FatalCode(0, "Invalid sniffer, nil", log.KV("sniffer", k))
		}
		//The config may specify a particular source IP for this sniffer.
		//If not, derive one.
		var src net.IP
		if v.Source_Override != `` {
			src = net.ParseIP(v.Source_Override)
			if src == nil {
				closeSniffers(sniffs)
				lg.FatalCode(0, "Source-Override is invalid")
			}
		} else if cfg.Source_Override != `` {
			// global override
			src = net.ParseIP(cfg.Source_Override)
			if src == nil {
				closeSniffers(sniffs)
				lg.FatalCode(0, "Global Source-Override is invalid")
			}
		} else {
			src, err = getSourceIP(v.Interface)
			if err != nil {
				src = nil
			}
		}

		//get the handle on the device
		hnd, err := pcap.OpenLive(v.Interface, int32(v.Snap_Len), v.Promisc, pktTimeout)
		if err != nil {
			closeSniffers(sniffs)
			lg.FatalCode(0, "failed to initialize handler", log.KV("interface", v.Interface), log.KV("sniffer", k), log.KVErr(err))
		}
		//apply a filter if one is specified
		if v.BPF_Filter != `` {
			if err := hnd.SetBPFFilter(v.BPF_Filter); err != nil {
				hnd.Close()
				closeSniffers(sniffs)
				lg.FatalCode(0, "invalid BPF filter", log.KV("sniffer", k), log.KVErr(err))
			}
		}
		sniffs = append(sniffs, sniffer{
			name:      k,
			src:       src,
			Promisc:   v.Promisc,
			Interface: v.Interface,
			TagName:   v.Tag_Name,
			SnapLen:   v.Snap_Len,
			BPFFilter: v.BPF_Filter,
			handle:    hnd,
			die:       make(chan bool, 1),
			res:       make(chan results, 1),
		})
	}

	//set tags and source for each sniffer
	for i := range sniffs {
		tag, err := igst.GetTag(sniffs[i].TagName)
		if err != nil {
			closeSniffers(sniffs)
			lg.Fatal("failed to resolve tag", log.KV("tag", sniffs[i].TagName), log.KVErr(err))
		}
		sniffs[i].tag = tag
	}

	start := time.Now()

	for i := range sniffs {
		sniffs[i].active = true
		go pcapIngester(igst, &sniffs[i])
	}

	utils.WaitForQuit()

	requestClose(sniffs)
	res := gatherResponse(sniffs)
	closeHandles(sniffs)
	durr := time.Since(start)

	if err = igst.Sync(time.Second); err != nil {
		lg.Error("failed to sync", log.KVErr(err))
	}
	if err = igst.Close(); err != nil {
		lg.Error("failed to close", log.KVErr(err))
	}
	if err == nil {
		debugout("Completed in %v (%s)\n", durr, ingest.HumanSize(res.Bytes))
		debugout("Total Count: %s\n", ingest.HumanCount(res.Count))
		debugout("Entry Rate: %s\n", ingest.HumanEntryRate(res.Count, durr))
		debugout("Ingest Rate: %s\n", ingest.HumanRate(res.Bytes, durr))
	}
}

// Called if something bad happens and we need to re-open the packet source
func rebuildPacketSource(s *sniffer) (*pcap.Handle, bool) {
	var threwErr bool
mainLoop:
	for {
		//we sleep when we first come in
		select {
		case <-time.After(time.Second):
		case <-s.die:
			break mainLoop
		}
		//sleep over, try to reopen our pcap device
		hnd, err := pcap.OpenLive(s.Interface, int32(s.SnapLen), s.Promisc, pktTimeout)
		if err != nil {
			if !threwErr {
				threwErr = true
				lg.Error("failed to get pcap device on reopen", log.KV("interface", s.Interface), log.KVErr(err))
			}
			continue
		}
		if s.BPFFilter != `` {
			if err := hnd.SetBPFFilter(s.BPFFilter); err != nil {
				//this is fatal, this shouldn't be possible, but here we are
				lg.Error("invalid BPF Filter on reopen", log.KV("interface", s.Interface), log.KVErr(err))
				hnd.Close()
				break mainLoop
			}
		}
		//we got a good handle, return it
		return hnd, true
	}
	return nil, false //ummm... shouldn't happen?
}

// A captured packet
type capPacket struct {
	ts   entry.Timestamp
	data []byte
}

func packetExtractor(hnd *pcap.Handle, c chan []capPacket) {
	defer close(c)
	var packets []capPacket
	var packetsSize int
	var capPkt capPacket
	tckr := time.NewTicker(time.Second)
	defer tckr.Stop()

	var trimSize int
	//in order for us to deal SLL "cooked" interfaces we have to trim th first 2 bytes
	//The ethernet layer is going to be foobared, but the IP layers should be fine
	if hnd.LinkType() == layers.LinkTypeLinuxSLL {
		trimSize = 2
	}

	for {
		data, ci, err := hnd.ReadPacketData()
		if err != nil {
			if err == pcap.NextErrorTimeoutExpired || err == io.EOF {
				if len(packets) > 0 {
					c <- packets
					packets = nil
					packetsSize = 0
				}
				continue
			}
			debugout("failed to get packet from source: %v\n", err)
			break
		}
		if trimSize > 0 && len(data) > trimSize {
			data = data[trimSize:]
		}
		capPkt.data = data
		capPkt.ts = entry.FromStandard(ci.Timestamp)
		packets = append(packets, capPkt)
		packetsSize += len(capPkt.data)

		select {
		case _ = <-tckr.C:
			if len(packets) > 0 {
				c <- packets
				packets = nil
				packetsSize = 0
			}
		default:
			if packetsSize >= packetsThrowSize {
				c <- packets
				packets = nil
				packetsSize = 0
			}
		}
	}
	if len(packets) > 0 {
		c <- packets
	}
}

// Main loop for a sniffer. Gets packets from the sniffer and sends
// them to the ingester.
func pcapIngester(igst *ingest.IngestMuxer, s *sniffer) {
	count := uint64(0)
	totalBytes := uint64(0)

	//get a packet source
	ch := make(chan []capPacket, 1024)
	go packetExtractor(s.handle, ch)
	debugout("Starting sniffer %s on %s with \"%s\"\n", s.name, s.Interface, s.BPFFilter)
	lg.Info("starting sniffer", log.KV("sniffer", s.name), log.KV("interface", s.Interface), log.KV("bpffilter", s.BPFFilter))

mainLoop:
	for {
		//check if we are supposed to die
		select {
		case _ = <-s.die:
			s.handle.Close()
			break mainLoop
		case pkts, ok := <-ch: //get a packet
			if !ok {
				//Something bad happened, attempt to restart the pcap
				lg.Error("failed to read packet, attempting to rebuild packet source")
				s.handle.Close()
				s.handle, ok = rebuildPacketSource(s)
				if !ok {
					lg.Error("couldn't rebuild packet source")
					//Still failing, time to bail out
					break mainLoop
				}
				//now we need to re-start the extractor
				ch = make(chan []capPacket, 1024)
				go packetExtractor(s.handle, ch)
				debugout("Rebuilding packet source\n")
				lg.Info("rebuilt packet source")
				continue
			}
			staticSet := make([]entry.Entry, len(pkts))
			set := make([]*entry.Entry, len(pkts))
			for i := range pkts {
				staticSet[i].TS = pkts[i].ts
				staticSet[i].Data = pkts[i].data
				staticSet[i].SRC = s.src
				staticSet[i].Tag = s.tag
				set[i] = &staticSet[i]
				totalBytes += uint64(len(pkts[i].data))
				count++
			}
			if err := igst.WriteBatch(set); err != nil {
				s.handle.Close()
				lg.Error("failed to handle entry", log.KVErr(err))
				s.res <- results{
					Bytes: 0,
					Count: 0,
					Error: err,
				}
				return
			}
		}
	}

	s.res <- results{
		Bytes: totalBytes,
		Count: count,
		Error: nil,
	}
}

// Attempt to find a reasonable IP for a given interface name
// Returns the first IP it finds.
func getSourceIP(dev string) (net.IP, error) {
	iface, err := net.InterfaceByName(dev)
	if err != nil {
		return nil, err
	}
	addrs, err := iface.Addrs()
	if err != nil {
		return nil, err
	}
	for _, addr := range addrs {
		//try for cidr first
		ip, _, err := net.ParseCIDR(addr.String())
		if err == nil {
			return ip, nil
		}
		//try as an ip
		ip = net.ParseIP(addr.String())
		if ip != nil {
			return ip, nil
		}
	}
	return nil, errors.New("No IP for " + dev)
}

func debugout(format string, args ...interface{}) {
	if !v {
		return
	}
	fmt.Printf(format, args...)
}

// Add the bytes & packet count from src into dst.
func addResults(dst *results, src results) {
	if dst == nil {
		return
	}
	dst.Bytes += src.Bytes
	dst.Count += src.Count
}

// Ask each sniffer to shut down.
func requestClose(sniffs []sniffer) {
	for _, s := range sniffs {
		if s.active {
			s.die <- true
		}
	}
}

// Gather total statistics from all sniffers and return
func gatherResponse(sniffs []sniffer) results {
	var r results
	for _, s := range sniffs {
		if s.active {
			addResults(&r, <-s.res)
		}
	}
	return r
}

// Close the sniffers' pcap handles
func closeHandles(sniffs []sniffer) {
	for _, s := range sniffs {
		if s.handle != nil {
			s.handle.Close()
		}
	}
}

// Ask each sniffer to stop collection, gather the total
// statistics, and then attempt to close pcap handles just
// to be safe (should be closed by requestClose())
func closeSniffers(sniffs []sniffer) results {
	requestClose(sniffs)
	r := gatherResponse(sniffs)
	closeHandles(sniffs)
	return r
}
