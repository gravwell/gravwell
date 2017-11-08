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
	"log"
	"net"
	"os"
	"os/signal"
	"path"
	"syscall"
	"time"

	"github.com/gravwell/ingest"
	"github.com/gravwell/ingest/entry"

	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcap"
)

const (
	defaultConfigLoc     = `/opt/gravwell/etc/network_capture.conf`
	packetsThrowSize int = 1024 * 1024 * 2
)

var (
	configOverride = flag.String("config-file-override", "", "Override location for configuration file")
	verbose        = flag.Bool("v", false, "Display verbose status updates to stdout")
	stderrOverride = flag.String("stderr", "", "Redirect stderr to a shared memory file")

	pktTimeout time.Duration = 500 * time.Millisecond

	confLoc      string
	totalPackets uint64
	totalBytes   uint64
	v            bool
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
	if *stderrOverride != `` {
		fp := path.Join(`/dev/shm/`, *stderrOverride)
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
	}

	if *configOverride == "" {
		confLoc = defaultConfigLoc
	} else {
		confLoc = *configOverride
	}
	v = *verbose
}

func main() {
	cfg, err := GetConfig(confLoc)
	if err != nil {
		log.Fatal("Failed to get configuration: ", err)
	}

	tags, err := cfg.Tags()
	if err != nil {
		log.Fatal("Failed to get tags from configuration: ", err)
	}
	conns, err := cfg.Targets()
	if err != nil {
		log.Fatal("Failed to get backend targets from configuration: ", err)
	}
	debugout("Handling %d tags over %d targets\n", len(tags), len(conns))

	//loop through our sniffers and get a config up for each
	var sniffs []sniffer
	for k, v := range cfg.Sniffer {
		if v == nil {
			closeSniffers(sniffs)
			log.Fatal("Invalid sniffer named ", k, ".  Nil struct")
			return
		}
		//The config may specify a particular source IP for this sniffer.
		//If not, derive one.
		var src net.IP
		if v.Source_Override != `` {
			src = net.ParseIP(v.Source_Override)
			if src == nil {
				closeSniffers(sniffs)
				log.Fatal("Source-Override is invalid")
			}
		} else {
			src, err = getSourceIP(v.Interface)
			if err != nil {
				closeSniffers(sniffs)
				log.Fatal("Failed to get source for ", v.Interface, ": ", err)
			}
		}

		//get the handle on the device
		hnd, err := pcap.OpenLive(v.Interface, int32(v.Snap_Len), v.Promisc, pktTimeout)
		if err != nil {
			closeSniffers(sniffs)
			log.Fatal("Failed to get initialize handler on ", v.Interface, " for ", k)
		}
		//apply a filter if one is specified
		if v.BPF_Filter != `` {
			if err := hnd.SetBPFFilter(v.BPF_Filter); err != nil {
				hnd.Close()
				closeSniffers(sniffs)
				log.Fatal("Invalid BPF Filter for ", k, " : ", err)
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

	//fire up the ingesters
	igCfg := ingest.UniformMuxerConfig{
		Destinations: conns,
		Tags:         tags,
		Auth:         cfg.Secret(),
		LogLevel:     cfg.LogLevel(),
		IngesterName: "networkLog",
	}
	if cfg.EnableCache() {
		igCfg.EnableCache = true
		igCfg.CacheConfig.FileBackingLocation = cfg.LocalFileCachePath()
	}
	igst, err := ingest.NewUniformMuxer(igCfg)
	if err != nil {
		log.Fatal("Failed to create new uniform muxer ", err)
		return
	}
	debugout("Started ingester muxer\n")
	if err := igst.Start(); err != nil {
		closeSniffers(sniffs)
		log.Fatal("Failed start our ingest system: ", err)
		return
	}
	debugout("Waiting for connections to indexers\n")
	if err := igst.WaitForHot(cfg.Timeout()); err != nil {
		closeSniffers(sniffs)
		log.Fatal("Timedout waiting for backend connections: ", err)
		return
	}
	debugout("Successfully connected to ingesters\n")

	//set tags and source for each sniffer
	for i := range sniffs {
		tag, err := igst.GetTag(sniffs[i].TagName)
		if err != nil {
			closeSniffers(sniffs)
			log.Fatal("Failed to resolve tag ", sniffs[i].TagName, ": ", err)
		}
		sniffs[i].tag = tag
	}

	start := time.Now()

	//register quit signals so we can die gracefully
	quitSig := make(chan os.Signal, 1)
	signal.Notify(quitSig, os.Interrupt)

	for i := range sniffs {
		sniffs[i].active = true
		go pcapIngester(igst, &sniffs[i])
	}

	<-quitSig
	requestClose(sniffs)
	res := gatherResponse(sniffs)
	closeHandles(sniffs)
	if err := igst.Close(); err != nil {
		log.Fatal("Failed to close ingester", err)
	}
	durr := time.Since(start)

	if err == nil {
		fmt.Printf("Completed in %v (%s)\n", durr, ingest.HumanSize(res.Bytes))
		fmt.Printf("Total Count: %s\n", ingest.HumanCount(res.Count))
		fmt.Printf("Entry Rate: %s\n", ingest.HumanEntryRate(res.Count, durr))
		fmt.Printf("Ingest Rate: %s\n", ingest.HumanRate(res.Bytes, durr))
	}
	if err := igst.Sync(time.Second); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to sync the ingester: %v\n", err)
		return
	}
	if err := igst.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to close the ingester: %v\n", err)
		return
	}
}

//Called if something bad happens and we need to re-open the packet source
func rebuildPacketSource(s *sniffer) (*pcap.Handle, bool) {
	var threwErr bool
	for {
		//we sleep when we first come in
		select {
		case <-time.After(time.Second):
		case <-s.die:
			return nil, false
		}
		//sleep over, try to reopen our pcap device
		hnd, err := pcap.OpenLive(s.Interface, int32(s.SnapLen), s.Promisc, pktTimeout)
		if err != nil {
			if !threwErr {
				threwErr = true
				fmt.Fprintf(os.Stderr, "Error: Failed to get pcap device on reopen (%v)\n", err)
			}
			continue
		}
		if s.BPFFilter != `` {
			if err := hnd.SetBPFFilter(s.BPFFilter); err != nil {
				//this is fatal, this shouldn't be possible, but here we are
				fmt.Fprintf(os.Stderr, "Invalid BPF Filter on reopen: %v\n", err)
				hnd.Close()
				return nil, false
			}
		}
		//we got a good handle, return it
		return hnd, true
	}
	return nil, false //ummm... shouldn't happen?
}

//A captured packet
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
			if err == pcap.NextErrorTimeoutExpired {
				if len(packets) > 0 {
					c <- packets
					packets = nil
					packetsSize = 0
				}
				continue
			}
			debugout("Failed to get packet from source: %v\n", err)
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

//Main loop for a sniffer. Gets packets from the sniffer and sends
//them to the ingester.
func pcapIngester(igst *ingest.IngestMuxer, s *sniffer) {
	count := uint64(0)
	totalBytes := uint64(0)

	//get a packet source
	ch := make(chan []capPacket, 1024)
	go packetExtractor(s.handle, ch)
	debugout("Starting sniffer %s on %s with \"%s\"\n", s.name, s.Interface, s.BPFFilter)

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
				s.handle.Close()
				s.handle, ok = rebuildPacketSource(s)
				if !ok {
					//Still failing, time to bail out
					break mainLoop
				}
				//now we need to re-start the extractor
				ch = make(chan []capPacket, 1024)
				go packetExtractor(s.handle, ch)
				debugout("Rebuilding packet source\n")
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
				fmt.Fprintf(os.Stderr, "Failed to write entry: %v\n", err)
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

//Attempt to find a reasonable IP for a given interface name
//Returns the first IP it finds.
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

//Add the bytes & packet count from src into dst.
func addResults(dst *results, src results) {
	if dst == nil {
		return
	}
	dst.Bytes += src.Bytes
	dst.Count += src.Count
}

//Ask each sniffer to shut down.
func requestClose(sniffs []sniffer) {
	for _, s := range sniffs {
		if s.active {
			s.die <- true
		}
	}
}

//Gather total statistics from all sniffers and return
func gatherResponse(sniffs []sniffer) results {
	var r results
	for _, s := range sniffs {
		if s.active {
			addResults(&r, <-s.res)
		}
	}
	return r
}

//Close the sniffers' pcap handles
func closeHandles(sniffs []sniffer) {
	for _, s := range sniffs {
		if s.handle != nil {
			s.handle.Close()
		}
	}
}

//Ask each sniffer to stop collection, gather the total
//statistics, and then attempt to close pcap handles just
//to be safe (should be closed by requestClose())
func closeSniffers(sniffs []sniffer) results {
	requestClose(sniffs)
	r := gatherResponse(sniffs)
	closeHandles(sniffs)
	return r
}
