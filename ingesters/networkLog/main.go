/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
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
	"io"
	"net"
	"os"
	"time"

	"github.com/gravwell/gravwell/v4/ingest"
	"github.com/gravwell/gravwell/v4/ingest/entry"
	"github.com/gravwell/gravwell/v4/ingest/log"
	"github.com/gravwell/gravwell/v4/ingesters/base"
	"github.com/gravwell/gravwell/v4/ingesters/utils"
	"github.com/gravwell/gravwell/v4/ingesters/utils/caps"

	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcap"
)

const (
	defaultConfigLoc                = `/opt/gravwell/etc/network_capture.conf`
	defaultConfigDLoc               = `/opt/gravwell/etc/network_capture.conf.d`
	packetsThrowSize  int           = 1024 * 1024 * 2
	ingesterName                    = "networkLog"
	appName                         = `networklog`
	pktTimeout        time.Duration = 500 * time.Millisecond
)

var (
	totalPackets uint64
	totalBytes   uint64
	debugOn      bool
	lg           *log.Logger

	exitCtx, exitFn = context.WithCancel(context.Background())
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

func main() {
	var cfg *cfgType
	ibc := base.IngesterBaseConfig{
		IngesterName:                 ingesterName,
		AppName:                      appName,
		DefaultConfigLocation:        defaultConfigLoc,
		DefaultConfigOverlayLocation: defaultConfigDLoc,
		GetConfigFunc:                GetConfig,
	}
	ib, err := base.Init(ibc)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to get configuration %v\n", err)
		return
	} else if err = ib.AssignConfig(&cfg); err != nil || cfg == nil {
		fmt.Fprintf(os.Stderr, "failed to assign configuration %v %v\n", err, cfg == nil)
		return
	}
	debugOn = ib.Verbose
	lg = ib.Logger

	igst, err := ib.GetMuxer()
	if err != nil {
		ib.Logger.FatalCode(0, "failed to get ingest connection", log.KVErr(err))
		return
	}
	defer igst.Close()
	ib.AnnounceStartup()

	debugout("Started ingester muxer\n")

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
	ib.AnnounceShutdown()

	requestClose(sniffs)
	res := gatherResponse(sniffs)
	closeHandles(sniffs)
	durr := time.Since(start)

	exitFn()

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
			if err := igst.WriteBatchContext(exitCtx, set); err != nil {
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
	if debugOn {
		fmt.Printf(format, args...)
	}
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
