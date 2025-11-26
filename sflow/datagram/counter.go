/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package datagram

import (
	"net"
)

const (
	CounterSampleFormat         = 2
	CounterSampleExtendedFormat = 4
)

// SFlowDataSource see https://sflow.org/sflow_version_5.txt, pag 30, `sflow_data_source`
type SFlowDataSource = uint32

// SFlowDataSourceExpanded see https://sflow.org/sflow_version_5.txt, pag 30, `sflow_data_source_expanded`
type SFlowDataSourceExpanded struct {
	SourceIDType  uint32
	SourceIDIndex uint32
}

// CounterSample see https://sflow.org/sflow_version_5.txt, pag 29, `counters_sample`
type CounterSample struct {
	SampleHeader
	SequenceNum uint32
	SFlowDataSource
	CounterRecordsCount uint32
	Records             []Record
}

func (cs *CounterSample) GetSampleHeader() (RecordHeader, error) {
	return cs.SampleHeader, nil
}

// CounterSampleExpanded see https://sflow.org/sflow_version_5.txt, pag 31, `counters_sample_expanded`
type CounterSampleExpanded struct {
	SampleHeader
	SequenceNum uint32
	SFlowDataSourceExpanded
	CounterRecordsCount uint32
	Records             []Record
}

func (cs *CounterSampleExpanded) GetSampleHeader() (RecordHeader, error) {
	return cs.SampleHeader, nil
}

type RecordHeader = SampleHeader

// CounterIfRecord see https://sflow.org/sflow_version_5.txt , pag 40, `if_counters`
type CounterIfRecord struct {
	RecordHeader
	IfIndex            uint32
	IfType             uint32
	IfSpeed            uint64
	IfDirection        uint32
	IfStatus           uint32
	IfInOctets         uint64
	IfInUcastPkts      uint32
	IfInMulticastPkts  uint32
	IfInBroadcastPkts  uint32
	IfInDiscards       uint32
	IfInErrors         uint32
	IfInUnknownProtos  uint32
	IfOutOctets        uint64
	IfOutUcastPkts     uint32
	IfOutMulticastPkts uint32
	IfOutBroadcastPkts uint32
	IfOutDiscards      uint32
	IfOutErrors        uint32
	IfPromiscuousMode  uint32
}

func (cr *CounterIfRecord) GetRecordHeader() (RecordHeader, error) {
	return cr.RecordHeader, nil
}

var CounterIfRecordValidLength = packetSizeOf(CounterIfRecord{}) - RecordHeaderSize

const CounterIfRecordDataFormatValue uint32 = 1

// EthernetCounters see https://sflow.org/sflow_version_5.txt , Pag 41, `ethernet_counters`
type EthernetCounters struct {
	RecordHeader
	Dot3StatsAlignmentErrors           uint32
	Dot3StatsFCSErrors                 uint32
	Dot3StatsSingleCollisionFrames     uint32
	Dot3StatsMultipleCollisionFrames   uint32
	Dot3StatsSQETestErrors             uint32
	Dot3StatsDeferredTransmissions     uint32
	Dot3StatsLateCollisions            uint32
	Dot3StatsExcessiveCollisions       uint32
	Dot3StatsInternalMacTransmitErrors uint32
	Dot3StatsCarrierSenseErrors        uint32
	Dot3StatsFrameTooLongs             uint32
	Dot3StatsInternalMacReceiveErrors  uint32
	Dot3StatsSymbolErrors              uint32
}

func (ec *EthernetCounters) GetRecordHeader() (RecordHeader, error) {
	return ec.RecordHeader, nil
}

var EthernetCountersRecordValidLength = packetSizeOf(EthernetCounters{}) - RecordHeaderSize

const EthernetCountersRecordDataFormatValue uint32 = 2

// TokenringCounters see https://sflow.org/sflow_version_5.txt , Pag 41, `tokenring_counters`
type TokenringCounters struct {
	RecordHeader
	Dot3StatsLineErrors         uint32
	Dot3StatsBurstErrors        uint32
	Dot3StatsACErrors           uint32
	Dot3StatsAbortTransErrors   uint32
	Dot3StatsInternalErrors     uint32
	Dot3StatsLostFrameErrors    uint32
	Dot3StatsReceiveCongestions uint32
	Dot3StatsFrameCopiedErrors  uint32
	Dot3StatsTokenErrors        uint32
	Dot3StatsSoftErrors         uint32
	Dot3StatsHardErrors         uint32
	Dot3StatsSignalLoss         uint32
	Dot3StatsTransmitBeacons    uint32
	Dot3StatsRecoverys          uint32
	Dot3StatsLobeWires          uint32
	Dot3StatsRemoves            uint32
	Dot3StatsSingles            uint32
	Dot3StatsFreqErrors         uint32
}

func (tr *TokenringCounters) GetRecordHeader() (RecordHeader, error) {
	return tr.RecordHeader, nil
}

var TokenringCountersRecordValidLength = packetSizeOf(TokenringCounters{}) - RecordHeaderSize

const TokenringCountersRecordDataFormatValue uint32 = 3

// VgCounters see https://sflow.org/sflow_version_5.txt , Pag 42, `vg_counters`
type VgCounters struct {
	RecordHeader
	Dot12InHighPriorityFrames    uint32
	Dot12InHighPriorityOctets    uint64
	Dot12InNormPriorityFrames    uint32
	Dot12InNormPriorityOctets    uint64
	Dot12InIPMErrors             uint32
	Dot12InOversizeFrameErrors   uint32
	Dot12InDataErrors            uint32
	Dot12InNullAddressedFrames   uint32
	Dot12OutHighPriorityFrames   uint32
	Dot12OutHighPriorityOctets   uint64
	Dot12TransitionIntoTrainings uint32
	Dot12HCInHighPriorityOctets  uint64
	Dot12HCInNormPriorityOctets  uint64
	Dot12HCOutHighPriorityOctets uint64
}

func (v *VgCounters) GetRecordHeader() (RecordHeader, error) {
	return v.RecordHeader, nil
}

var VgCountersRecordValidLength = packetSizeOf(VgCounters{}) - RecordHeaderSize

const VgCountersRecordDataFormatValue uint32 = 4

// VlanCounters see https://sflow.org/sflow_version_5.txt , Pag 42, `vlan_counters`
type VlanCounters struct {
	RecordHeader
	ID               uint32
	Octets           uint64
	UnicastPackets   uint32
	MulticastPackets uint32
	BroadcastPackets uint32
	Discards         uint32
}

func (v *VlanCounters) GetRecordHeader() (RecordHeader, error) {
	return v.RecordHeader, nil
}

var VlanCountersRecordValidLength = packetSizeOf(VlanCounters{}) - RecordHeaderSize

const VlanCountersRecordDataFormatValue uint32 = 5

// ProcessorCounters see https://sflow.org/sflow_version_5.txt , Pag 42, `processor`
type ProcessorCounters struct {
	RecordHeader
	CPU5s       uint32
	CPU1m       uint32
	CPU5m       uint32
	TotalMemory uint64
	FreeMemory  uint64
}

func (v *ProcessorCounters) GetRecordHeader() (RecordHeader, error) {
	return v.RecordHeader, nil
}

var ProcessorCountersRecordValidLength = packetSizeOf(ProcessorCounters{}) - RecordHeaderSize

const ProcessorCountersRecordDataFormatValue uint32 = 1001

// OpenFlowPort see https://sflow.org/sflow_openflow.txt, Pag 2, `of_port`
type OpenFlowPort struct {
	RecordHeader
	DataPathID uint64
	PortNumber uint32
}

func (v *OpenFlowPort) GetRecordHeader() (RecordHeader, error) {
	return v.RecordHeader, nil
}

var OpenFlowPortRecordValidLength = packetSizeOf(OpenFlowPort{}) - RecordHeaderSize

const OpenFlowPortRecordDataFormatValue uint32 = 1004

// OpenFlowPortName see https://sflow.org/sflow_openflow.txt, Pag 2, `port_name`
type OpenFlowPortName struct {
	RecordHeader
	NameLength uint32
	Name       string // 128
}

func (v *OpenFlowPortName) GetRecordHeader() (RecordHeader, error) {
	return v.RecordHeader, nil
}

var OpenFlowPortNameRecordValidLength = packetSizeOf(OpenFlowPortName{}) - RecordHeaderSize

const (
	OpenFlowPortNameMaxLength                    = 128
	OpenFlowPortNameRecordMaxLength              = 128 + 4
	OpenFlowPortNameRecordDataFormatValue uint32 = 1005
)

// HostDescr see https://sflow.org/sflow_host.txt, Pag 7, `host_descr`
type HostDescr struct {
	RecordHeader
	HostNameLen  uint32
	// TODO  XDR Strings T___T
	HostName     string    // max size 64 bytes
	UUID         SFlowUUID // fixed size 16 bytes
	MachineType  uint32
	OSName       uint32
	OSReleaseLen uint32
	OSRelease    string // max size 32 bytes
}

func (v *HostDescr) GetRecordHeader() (RecordHeader, error) {
	return v.RecordHeader, nil
}

const (
	HostDescrRecordDataFormatValue uint32 = 2000

	hostNameLenSize  = 4
	HostNameMaxSize  = 64
	uuidSize         = 16
	machineTypeSize  = 4
	osNameSize       = 4
	osReleaseSizeLen = 4
	OSReleaseMaxSize = 32

	HostDescrRecordMaxLength = hostNameLenSize + HostNameMaxSize + uuidSize + machineTypeSize + osNameSize + osReleaseSizeLen + OSReleaseMaxSize // 128 Bytes max
)

// HostAdapters see https://sflow.org/sflow_host.txt, Pag 7, `host_adapters`
type HostAdapters struct {
	RecordHeader
	AdaptersCount uint32
	Adapters      []HostAdapter
}

type HostAdapter struct {
	IFIndex    uint32
	MACLength  uint32
	MACAddress net.HardwareAddr
}

func (v *HostAdapters) GetRecordHeader() (RecordHeader, error) {
	return v.RecordHeader, nil
}

// NOTE  HostAdapters is variable length, so no way to validate it

var HostAdaptersRecordDataFormatValue uint32 = 2001

// HostParent see https://sflow.org/sflow_host.txt, Pag 8, `host_parent`
type HostParent struct {
	RecordHeader
	ContainerType  uint32
	ContainerIndex uint32
}

func (v *HostParent) GetRecordHeader() (RecordHeader, error) {
	return v.RecordHeader, nil
}

var HostParentRecordValidLength = packetSizeOf(HostParent{}) - RecordHeaderSize

const HostParentRecordDataFormatValue uint32 = 2002

// HostCPU see https://sflow.org/sflow_host.txt, Pag 8, `host_cpu`
type HostCPU struct {
	RecordHeader
	LoadOne          float32
	LoadFive         float32
	LoadFifteen      float32
	ProcessesRunning uint32
	ProcessesTotal   uint32
	CPUNume          uint32
	CPUSpeed         uint32
	Uptime           uint32
	CPUUser          uint32
	CPUNice          uint32
	CPUSys           uint32
	CPUIdle          uint32
	CPUWio           uint32
	CPUIntr          uint32
	CPUSoftIntr      uint32
	Interrupts       uint32
	Contexts         uint32
}

func (v *HostCPU) GetRecordHeader() (RecordHeader, error) {
	return v.RecordHeader, nil
}

var HostCPURecordValidLength = packetSizeOf(HostCPU{}) - RecordHeaderSize

const HostCPURecordDataFormatValue uint32 = 2003

// HostMemory see https://sflow.org/sflow_host.txt, Pag 9, `host_memory`
type HostMemory struct {
	RecordHeader
	MemTotal     uint64
	MemFree      uint64
	MemShared    uint64
	MemBuffers   uint64
	MemCached    uint64
	MemSwapTotal uint64
	SwapFree     uint64
	PageIn       uint32
	PageOut      uint32
	SwapIn       uint32
	SwapOut      uint32
}

func (v *HostMemory) GetRecordHeader() (RecordHeader, error) {
	return v.RecordHeader, nil
}

var HostMemoryRecordValidLength = packetSizeOf(HostMemory{}) - RecordHeaderSize

const HostMemoryRecordDataFormatValue uint32 = 2004

// HostDiskIO see https://sflow.org/sflow_host.txt, Pag 9, `host_disk_io`
type HostDiskIO struct {
	RecordHeader
	DiskTotal               uint64
	DiskFree                uint64
	MaxUsedPartitionPercent float32
	Reads                   uint32
	BytesRead               uint64
	ReadTime                uint32
	Writes                  uint32
	BytesWritten            uint64
	WriteTime               uint32
}

func (v *HostDiskIO) GetRecordHeader() (RecordHeader, error) {
	return v.RecordHeader, nil
}

var HostDiskIORecordValidLength = packetSizeOf(HostDiskIO{}) - RecordHeaderSize

const HostDiskIORecordDataFormatValue uint32 = 2005

// HostNetIO see https://sflow.org/sflow_host.txt, Pag 9, `host_net_io`
type HostNetIO struct {
	RecordHeader
	BytesIn    uint64
	PacketsIn  uint32
	ErrorsIn   uint32
	DropsIn    uint32
	BytesOut   uint64
	PacketsOut uint32
	ErrorsOut  uint32
	DropsOut   uint32
}

func (v *HostNetIO) GetRecordHeader() (RecordHeader, error) {
	return v.RecordHeader, nil
}

var HostNetIORecordValidLength = packetSizeOf(HostNetIO{}) - RecordHeaderSize

const HostHetIORecordDataFormatValue uint32 = 2006

// VirtNode see https://sflow.org/sflow_host.txt, Pag 10, `virt_node`
type VirtNode struct {
	RecordHeader
	Mhz        uint32
	CPUs       uint32
	Memory     uint64
	MemoryFree uint64
	NumDomains uint32
}

func (v *VirtNode) GetRecordHeader() (RecordHeader, error) {
	return v.RecordHeader, nil
}

var VirtNodeRecordValidLength = packetSizeOf(VirtNode{}) - RecordHeaderSize

const VirtNodeRecordDataFormatValue uint32 = 2100

// VirtCPU see https://sflow.org/sflow_host.txt, Pag 10, `virt_cpu`
type VirtCPU struct {
	RecordHeader
	State           uint32
	CPUTime         uint32
	VirtualCPUCount uint32
}

func (v *VirtCPU) GetRecordHeader() (RecordHeader, error) {
	return v.RecordHeader, nil
}

var VirtCPURecordValidLength = packetSizeOf(VirtCPU{}) - RecordHeaderSize

const VirtCPURecordDataFormatValue uint32 = 2101

// VirtMemory see https://sflow.org/sflow_host.txt, Pag 10, `virt_memory`
type VirtMemory struct {
	RecordHeader
	Memory    uint64
	MaxMemory uint64
}

func (v *VirtMemory) GetRecordHeader() (RecordHeader, error) {
	return v.RecordHeader, nil
}

var VirtMemoryRecordValidLength = packetSizeOf(VirtMemory{}) - RecordHeaderSize

const VirtMemoryRecordDataFormatValue uint32 = 2102

// VirtDiskIO see https://sflow.org/sflow_host.txt, Pag 11, `virt_disk_io`
type VirtDiskIO struct {
	RecordHeader
	Capacity   uint64
	Allocation uint64
	Available  uint64
	RDReq      uint32
	RDBytes    uint64
	WRReq      uint32
	WRBytes    uint64
	Errors     uint32
}

func (v *VirtDiskIO) GetRecordHeader() (RecordHeader, error) {
	return v.RecordHeader, nil
}

var VirtDiskIORecordValidLength = packetSizeOf(VirtDiskIO{}) - RecordHeaderSize

const VirtDiskIORecordDataFormatValue uint32 = 2103

// VirtNetIO see https://sflow.org/sflow_host.txt, Pag 11, `virt_net_io`
type VirtNetIO struct {
	RecordHeader
	RXBytes   uint64
	RXPackets uint32
	RXErrs    uint32
	RXDrop    uint32
	TXBytes   uint64
	TXPackets uint32
	TXErrs    uint32
	TXDrop    uint32
}

func (v *VirtNetIO) GetRecordHeader() (RecordHeader, error) {
	return v.RecordHeader, nil
}

var VirtNetIORecordValidLength = packetSizeOf(VirtNetIO{}) - RecordHeaderSize

const VirtNetIORecordDataFormatValue uint32 = 2104
