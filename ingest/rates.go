/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package ingest

import (
	"fmt"
	"time"
)

const (
	KB       uint64  = 1024
	MB       uint64  = 1024 * KB
	GB       uint64  = 1024 * MB
	TB       uint64  = 1024 * GB
	PB       uint64  = 1024 * TB
	YB       uint64  = 1024 * PB
	K                = 1000.0
	M                = K * 1000.0
	G                = M * 1000.0
	T                = G * 1000.0
	P                = G * 1000.0
	Y                = P * 1000.0
	NsPerSec float64 = 1000000000.0
)

// HumanSize will take a byte count and duration and produce a human
// readable string showing data per second.  e.g. Megabytes/s (MB/s)
func HumanSize(b uint64) string {
	if b < KB {
		return fmt.Sprintf("%d B", b)
	} else if b <= MB {
		return fmt.Sprintf("%.02f KB", float64(b)/float64(KB))
	} else if b <= GB {
		return fmt.Sprintf("%.02f MB", float64(b)/float64(MB))
	} else if b <= TB {
		return fmt.Sprintf("%.02f GB", float64(b)/float64(GB))
	} else if b <= PB {
		return fmt.Sprintf("%.02f TB", float64(b)/float64(TB))
	} else if b <= YB {
		return fmt.Sprintf("%.02f PB", float64(b)/float64(PB))
	}
	return fmt.Sprintf("%.02f YB", float64(b)/float64(YB))

}

// HumanRate will take a byte count and duration and produce a human
// readable string showing data per second.  e.g. Megabytes/s (MB/s)
func HumanRate(b uint64, dur time.Duration) string {
	fS := float64(dur.Nanoseconds()) / NsPerSec
	v := float64(b) / fS
	if uint64(v) < KB {
		return fmt.Sprintf("%.02f Bps", v)
	} else if uint64(v) <= MB {
		return fmt.Sprintf("%.02f KB/s", (v / float64(KB)))
	} else if uint64(v) <= GB {
		return fmt.Sprintf("%.02f MB/s", (v / float64(MB)))
	} else if uint64(v) <= TB {
		return fmt.Sprintf("%.02f GB/s", (v / float64(GB)))
	} else if uint64(v) <= PB {
		return fmt.Sprintf("%.02f TB/s", (v / float64(TB)))
	} else if uint64(v) <= YB {
		return fmt.Sprintf("%.02f PB/s", (v / float64(PB)))
	}
	return fmt.Sprintf("%.02f YB/s", (v / float64(YB)))
}

// HumanLineRate will take a byte count and duration and produce a human
// readable string in terms of bits.  e.g. Megabits/s (Mbps)
func HumanLineRate(b uint64, dur time.Duration) string {
	b = b * 8
	fS := float64(dur.Nanoseconds()) / NsPerSec
	v := float64(b) / fS
	if uint64(v) < KB {
		return fmt.Sprintf("%d bps", b)
	} else if uint64(v) <= MB {
		return fmt.Sprintf("%.02f Kb/s", (v / float64(KB)))
	} else if uint64(v) <= GB {
		return fmt.Sprintf("%.02f Mb/s", (v / float64(MB)))
	} else if uint64(v) <= TB {
		return fmt.Sprintf("%.02f Gb/s", (v / float64(GB)))
	} else if uint64(v) <= PB {
		return fmt.Sprintf("%.02f Tb/s", (v / float64(TB)))
	} else if uint64(v) <= YB {
		return fmt.Sprintf("%.02f Pb/s", (v / float64(PB)))
	}
	return fmt.Sprintf("%.02f Yb/s", (v / float64(YB)))
}

// HumanEntryRate will take an entry count and duration and produce a human
// readable string in terms of entries per second.  e.g. 2400 K entries /s
func HumanEntryRate(b uint64, dur time.Duration) string {
	ps := (NsPerSec * float64(b)) / float64(dur.Nanoseconds())
	if ps < K {
		return fmt.Sprintf("%.02f E/s", ps)
	} else if ps <= M {
		return fmt.Sprintf("%.02f KE/s", ps/float64(K))
	} else if ps <= G {
		return fmt.Sprintf("%.02f ME/s", ps/float64(M))
	} else if ps <= T {
		return fmt.Sprintf("%.02f BE/s", ps/float64(G))
	} else if ps <= P {
		return fmt.Sprintf("%.02f TE/s", ps/float64(T))
	} else if ps <= Y {
		return fmt.Sprintf("%.02f QE/s", ps/float64(P))
	}
	return fmt.Sprintf("%.02f YE/s", ps/float64(Y))
}

// HumanCount will take a number and return an appropriately-scaled
// string, e.g. HumanCount(12500) will return "12.50 K"
func HumanCount(b uint64) string {
	ps := float64(b)
	if ps < K {
		return fmt.Sprintf("%.02f", ps)
	} else if ps <= M {
		return fmt.Sprintf("%.02f K", ps/float64(K))
	} else if ps <= G {
		return fmt.Sprintf("%.02f M", ps/float64(M))
	} else if ps <= T {
		return fmt.Sprintf("%.02f B", ps/float64(G))
	} else if ps <= P {
		return fmt.Sprintf("%.02f T", ps/float64(T))
	} else if ps <= Y {
		return fmt.Sprintf("%.02f Q", ps/float64(P))
	}
	return fmt.Sprintf("%.02f Y", ps/float64(Y))

}
