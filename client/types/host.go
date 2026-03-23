/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types

import (
	"encoding/json"
	"errors"

	"github.com/shirou/gopsutil/load"
)

var (
	// ErrPSINotAvailable is returned on platforms that do not support Linux Pressure Stall Information.
	ErrPSINotAvailable = errors.New("pressure stall information is not available on this platform")
)

// SysInfo as displayed in the System Overview in Gravwell.
type SysInfo struct {
	VirtSystem    string `json:",omitempty"` // e.g. "kvm" or "xen"
	VirtRole      string `json:",omitempty"` // "host" or "guest"
	CPUCount      int    `json:",omitempty"`
	CPUModel      string `json:",omitempty"`
	CPUMhz        string `json:",omitempty"`
	CPUCache      string `json:",omitempty"`
	TotalMemoryMB uint64 `json:",omitempty"`
	SystemVersion string `json:",omitempty"`
	Error         string `json:",omitempty"`
}

// DiskStats as shown in the System Stats - Hardware and Disks view in Gravwell.
type DiskStats struct {
	Mount     string
	Partition string
	Total     uint64
	Used      uint64
	// unique ID for this disk on this host
	// essentially a hash of indexer UUID, Mount, and Partition
	// this is used to uniquely identify a disk and mount on a specific host
	// uses are for when multiple indexers have the same disk topology
	// or docker clusters where everything is identical
	ID string
}

// DiskIO statistics as shown in the System Stats - Hardware and Disks view in Gravwell.
type DiskIO struct {
	Device string
	Read   uint64
	Write  uint64
}

type NetworkUsage struct {
	Up   uint64
	Down uint64
}

// HostSysStats statistics, used by the System Stats view in Gravwell.
type HostSysStats struct {
	Uptime                uint64  `json:",omitempty"`
	TotalMemory           uint64  `json:",omitempty"`
	ProcessHeapAllocation uint64  `json:",omitempty"` // bytes allocated by this process's heap
	ProcessSysReserved    uint64  `json:",omitempty"` // total bytes obtained from the OS
	MemoryUsedPercent     float64 `json:",omitempty"`
	Disks                 []DiskStats
	CPUUsage              float64
	CPUCount              int `json:",omitempty"`
	HostHash              string
	Net                   NetworkUsage `json:",omitempty"`
	IO                    []DiskIO
	VirtSystem            string       `json:",omitempty"` // e.g. "kvm" or "xen"
	VirtRole              string       `json:",omitempty"` // "host" or "guest"
	BuildInfo             BuildInfo    `json:",omitempty"` // e.g. 3.3.1
	LoadAverage           load.AvgStat `json:",omitempty"`
	Iowait                float64
	PSI                   PSIStats `json:"psi,omitempty"` // Pressure Stall Information, for CPU, memory, and IO
}

type DeploymentInfo struct {
	Distributed      bool //distributed webservers, meaning more than one
	CBACEnabled      bool //whether CBAC is enabled on the system
	DefaultLanguage  string
	AIEnabled        bool   // is the AI system available at all
	AIProcessor      string // URL of system that services Logbot AI requests
	AIDisabledReason string `json:",omitempty"` // if AI is disabled, explain why
	RenderStoreLimit uint   //maximum amount of data that can be stored in a renderer per search
}

type PSIStats struct {
	CPU    PressureStats `json:"cpu,omitempty"`
	Memory PressureStats `json:"memory,omitempty"`
	IO     PressureStats `json:"io,omitempty"`
}

type PressureStats struct {
	SomeAvg10  float64 `json:"some_avg_10,omitempty"`
	SomeAvg60  float64 `json:"some_avg_60,omitempty"`
	SomeAvg300 float64 `json:"some_avg_300,omitempty"`

	// "full" lines are only present in memory and IO pressure files, not CPU
	FullAvg10  float64 `json:"full_avg_10,omitempty"`
	FullAvg60  float64 `json:"full_avg_60,omitempty"`
	FullAvg300 float64 `json:"full_avg_300,omitempty"`
}

func (si SysInfo) Empty() bool {
	if si.CPUCount > 0 || si.TotalMemoryMB > 0 || si.CPUModel != "" {
		return false
	} else if si.CPUMhz != "" || si.CPUCache != "" || si.SystemVersion != "" {
		return false
	}
	return true
}

func (si SysInfo) MarshalJSON() ([]byte, error) {
	if si.Empty() {
		return emptyObj, nil
	}
	type alias SysInfo
	return json.Marshal(struct {
		alias
	}{
		alias: alias(si),
	})
}
