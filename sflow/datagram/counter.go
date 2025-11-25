package datagram

import (
	"net"
	"unsafe"
)

const (
	CounterSampleFormat         = 2
	CounterSampleExtendedFormat = 4
	DataFormatSize              = 4
	LengthSize                  = 4
	CommonHeaderSize            = DataFormatSize + LengthSize
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

func (cs *CounterSample) GetHeader() (SampleHeader, error) {
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

func (cs *CounterSampleExpanded) GetHeader() (SampleHeader, error) {
	return cs.SampleHeader, nil
}

// CounterIfRecord see https://sflow.org/sflow_version_5.txt , pag 40, `if_counters`
type CounterIfRecord struct {
	DataFormat         uint32
	Length             uint32
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

func (cr *CounterIfRecord) GetDataFormat() uint32 {
	return cr.DataFormat
}

const (
	CounterIfRecordValidLength     = unsafe.Sizeof(CounterIfRecord{}) - CommonHeaderSize
	CounterIfRecordDataFormatValue = 1
)

// EthernetCounters see https://sflow.org/sflow_version_5.txt , Pag 41, `ethernet_counters`
type EthernetCounters struct {
	DataFormat                         uint32
	Length                             uint32
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

func (ec *EthernetCounters) GetDataFormat() uint32 {
	return ec.DataFormat
}

const (
	EthernetCountersRecordValidLength     = unsafe.Sizeof(EthernetCounters{}) - CommonHeaderSize
	EthernetCountersRecordDataFormatValue = 2
)

// TokenringCounters see https://sflow.org/sflow_version_5.txt , Pag 41, `tokenring_counters`
type TokenringCounters struct {
	DataFormat                  uint32
	Length                      uint32
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

func (tr *TokenringCounters) GetDataFormat() uint32 {
	return tr.DataFormat
}

const (
	TokenringCountersRecordValidLength     = unsafe.Sizeof(TokenringCounters{}) - CommonHeaderSize
	TokenringCountersRecordDataFormatValue = 3
)

// VgCounters see https://sflow.org/sflow_version_5.txt , Pag 42, `vg_counters`
type VgCounters struct {
	DataFormat                   uint32
	Length                       uint32
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

func (v *VgCounters) GetDataFormat() uint32 {
	return v.DataFormat
}

const (
	VgCountersRecordValidLength     = unsafe.Sizeof(VgCounters{}) - CommonHeaderSize
	VgCountersRecordDataFormatValue = 4
)

// VlanCounters see https://sflow.org/sflow_version_5.txt , Pag 42, `vlan_counters`
type VlanCounters struct {
	DataFormat       uint32
	Length           uint32
	ID               uint32
	Octets           uint64
	UnicastPackets   uint32
	MulticastPackets uint32
	BroadcastPackets uint32
	Discards         uint32
}

func (v *VlanCounters) GetDataFormat() uint32 {
	return v.DataFormat
}

const (
	VlanCountersRecordValidLength     = unsafe.Sizeof(VlanCounters{}) - CommonHeaderSize
	VlanCountersRecordDataFormatValue = 5
)

// ProcessorCounters see https://sflow.org/sflow_version_5.txt , Pag 42, `processor`
type ProcessorCounters struct {
	DataFormat  uint32
	Length      uint32
	CPU5s       uint32
	CPU1m       uint32
	CPU5m       uint32
	TotalMemory uint64
	FreeMemory  uint64
}

func (v *ProcessorCounters) GetDataFormat() uint32 {
	return v.DataFormat
}

const (
	ProcessorCountersRecordValidLength     = unsafe.Sizeof(ProcessorCounters{}) - CommonHeaderSize
	ProcessorCountersRecordDataFormatValue = 1001
)

// HostDescr see https://sflow.org/sflow_host.txt, Pag 7, `host_descr`
type HostDescr struct {
	DataFormat  uint32
	Length      uint32
	HostName    [64]byte // string
	UUID        [16]byte // string
	MachineType uint32
	OSName      uint32
	OSRelease   [32]byte // string
}

func (v *HostDescr) GetDataFormat() uint32 {
	return v.DataFormat
}

const (
	HostDescrRecordValidLength     = unsafe.Sizeof(HostDescr{}) - CommonHeaderSize
	HostDescrRecordDataFormatValue = 2000
)

// HostAdapters see https://sflow.org/sflow_host.txt, Pag 7, `host_adapters`
type HostAdapters struct {
	DataFormat    uint32
	Length        uint32
	AdaptersCount uint32
	Adapters      []HostAdapter
}

type HostAdapter struct {
	IFIndex    uint32
	MACLength  uint32
	MACAddress net.HardwareAddr
}

func (v *HostAdapters) GetDataFormat() uint32 {
	return v.DataFormat
}

// NOTE  HostAdapters is variable length, so no way to validate it
const (
	HostAdaptersRecordDataFormatValue = 2001
)

// HostParent see https://sflow.org/sflow_host.txt, Pag 8, `host_parent`
type HostParent struct {
	DataFormat     uint32
	Length         uint32
	ContainerType  uint32
	ContainerIndex uint32
}

func (v *HostParent) GetDataFormat() uint32 {
	return v.DataFormat
}

const (
	HostParentRecordValidLength     = unsafe.Sizeof(HostParent{}) - CommonHeaderSize
	HostParentRecordDataFormatValue = 2002
)

// HostCPU see https://sflow.org/sflow_host.txt, Pag 8, `host_cpu`
type HostCPU struct {
	DataFormat       uint32
	Length           uint32
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

func (v *HostCPU) GetDataFormat() uint32 {
	return v.DataFormat
}

const (
	HostCPURecordValidLength     = unsafe.Sizeof(HostCPU{}) - CommonHeaderSize
	HostCPURecordDataFormatValue = 2003
)

// HostMemory see https://sflow.org/sflow_host.txt, Pag 9, `host_memory`
type HostMemory struct {
	DataFormat   uint32
	Length       uint32
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

func (v *HostMemory) GetDataFormat() uint32 {
	return v.DataFormat
}

const (
	HostMemoryRecordValidLength     = unsafe.Sizeof(HostMemory{}) - CommonHeaderSize
	HostMemoryRecordDataFormatValue = 2004
)

// HostDiskIO see https://sflow.org/sflow_host.txt, Pag 9, `host_disk_io`
type HostDiskIO struct {
	DataFormat              uint32
	Length                  uint32
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

func (v *HostDiskIO) GetDataFormat() uint32 {
	return v.DataFormat
}

const (
	HostDiskIORecordValidLength     = unsafe.Sizeof(HostDiskIO{}) - CommonHeaderSize
	HostDiskIORecordDataFormatValue = 2005
)

// HostNetIO see https://sflow.org/sflow_host.txt, Pag 9, `host_net_io`
type HostNetIO struct {
	DataFormat uint32
	Length     uint32
	BytesIn    uint64
	PacketsIn  uint32
	ErrorsIn   uint32
	DropsIn    uint32
	BytesOut   uint64
	PacketsOut uint32
	ErrorsOut  uint32
	DropsOut   uint32
}

func (v *HostNetIO) GetDataFormat() uint32 {
	return v.DataFormat
}

const (
	HostNetIORecordValidLength     = unsafe.Sizeof(HostNetIO{}) - CommonHeaderSize
	HostHetIORecordDataFormatValue = 2006
)

// VirtNode see https://sflow.org/sflow_host.txt, Pag 10, `virt_node`
type VirtNode struct {
	DataFormat uint32
	Length     uint32
	Mhz        uint32
	CPUs       uint32
	Memory     uint64
	MemoryFree uint64
	NumDomains uint32
}

func (v *VirtNode) GetDataFormat() uint32 {
	return v.DataFormat
}

const (
	VirtNodeRecordValidLength     = unsafe.Sizeof(VirtNode{}) - CommonHeaderSize
	VirtNodeRecordDataFormatValue = 2100
)

// VirtCPU see https://sflow.org/sflow_host.txt, Pag 10, `virt_cpu`
type VirtCPU struct {
	DataFormat      uint32
	Length          uint32
	State           uint32
	CPUTime         uint32
	VirtualCPUCount uint64
}

func (v *VirtCPU) GetDataFormat() uint32 {
	return v.DataFormat
}

const (
	VirtCPURecordValidLength     = unsafe.Sizeof(VirtCPU{}) - CommonHeaderSize
	VirtCPURecordDataFormatValue = 2101
)

// VirtMemory see https://sflow.org/sflow_host.txt, Pag 10, `virt_memory`
type VirtMemory struct {
	DataFormat uint32
	Length     uint32
	Memory     uint64
	MaxMemory  uint64
}

func (v *VirtMemory) GetDataFormat() uint32 {
	return v.DataFormat
}

const (
	VirtMemoryRecordValidLength     = unsafe.Sizeof(VirtMemory{}) - CommonHeaderSize
	VirtMemoryRecordDataFormatValue = 2102
)

// VirtDiskIO see https://sflow.org/sflow_host.txt, Pag 11, `virt_disk_io`
type VirtDiskIO struct {
	DataFormat uint32
	Length     uint32
	Capacity   uint64
	Allocation uint64
	Available  uint64
	RDReq      uint32
	RDBytes    uint64
	WRReq      uint32
	WRBytes    uint64
	Errors     uint32
}

func (v *VirtDiskIO) GetDataFormat() uint32 {
	return v.DataFormat
}

const (
	VirtDiskIORecordValidLength     = unsafe.Sizeof(VirtDiskIO{}) - CommonHeaderSize
	VirtDiskIORecordDataFormatValue = 2103
)

// VirtNetIO see https://sflow.org/sflow_host.txt, Pag 11, `virt_net_io`
type VirtNetIO struct {
	DataFormat uint32
	Length     uint32
	RXBytes    uint64
	RXPackets  uint32
	RXErrs     uint32
	RXDrop     uint32
	TXBytes    uint64
	TXPackets  uint32
	TXErrs     uint32
	TXDrop     uint32
}

func (v *VirtNetIO) GetDataFormat() uint32 {
	return v.DataFormat
}

const (
	VirtNetIORecordValidLength     = unsafe.Sizeof(VirtNetIO{}) - CommonHeaderSize
	VirtNetIORecordDataFormatValue = 2104
)
