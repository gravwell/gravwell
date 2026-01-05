/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package datagram

const (
	CounterSampleFormat         = 2
	CounterSampleExpandedFormat = 4
)

// CounterSample see https://sflow.org/sflow_version_5.txt, pag 29, `counters_sample`
type CounterSample struct {
	SampleHeader
	SequenceNum uint32
	SFlowDataSource
	Records []Record
}

func (cs *CounterSample) GetHeader() SampleHeader {
	return cs.SampleHeader
}

// CounterSampleExpanded see https://sflow.org/sflow_version_5.txt, pag 31, `counters_sample_expanded`
type CounterSampleExpanded struct {
	SampleHeader
	SequenceNum uint32
	SFlowDataSourceExpanded
	Records []Record
}

func (cs *CounterSampleExpanded) GetHeader() SampleHeader {
	return cs.SampleHeader
}

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

func (cr *CounterIfRecord) GetHeader() RecordHeader {
	return cr.RecordHeader
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

func (ec *EthernetCounters) GetHeader() RecordHeader {
	return ec.RecordHeader
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

func (tr *TokenringCounters) GetHeader() RecordHeader {
	return tr.RecordHeader
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

func (v *VgCounters) GetHeader() RecordHeader {
	return v.RecordHeader
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

func (v *VlanCounters) GetHeader() RecordHeader {
	return v.RecordHeader
}

var VlanCountersRecordValidLength = packetSizeOf(VlanCounters{}) - RecordHeaderSize

const VlanCountersRecordDataFormatValue uint32 = 5

// IEEE80211Counters see https://sflow.org/sflow_80211.txt , `ieee80211_counters`
type IEEE80211Counters struct {
	RecordHeader
	Dot11TransmittedFragmentCount       uint32
	Dot11MulticastTransmittedFrameCount uint32
	Dot11FailedCount                    uint32
	Dot11RetryCount                     uint32
	Dot11MultipleRetryCount             uint32
	Dot11FrameDuplicateCount            uint32
	Dot11RTSSuccessCount                uint32
	Dot11RTSFailureCount                uint32
	Dot11ACKFailureCount                uint32
	Dot11ReceivedFragmentCount          uint32
	Dot11MulticastReceivedFrameCount    uint32
	Dot11FCSErrorCount                  uint32
	Dot11TransmittedFrameCount          uint32
	Dot11WEPUndecryptableCount          uint32
	Dot11QoSDiscardedFragmentCount      uint32
	Dot11AssociatedStationCount         uint32
	Dot11QoSCFPollsReceivedCount        uint32
	Dot11QoSCFPollsUnusedCount          uint32
	Dot11QoSCFPollsUnusableCount        uint32
	Dot11QoSCFPollsLostCount            uint32
}

func (v *IEEE80211Counters) GetHeader() RecordHeader {
	return v.RecordHeader
}

var IEEE80211CountersRecordValidLength = packetSizeOf(IEEE80211Counters{}) - RecordHeaderSize

const IEEE80211CountersRecordDataFormatValue uint32 = 6

// LAGPortStats see https://sflow.org/sflow_lag.txt , `lag_port_stats`
type LAGPortStats struct {
	RecordHeader
	Dot3adAggPortActorSystemID             XDRMACAddress
	Dot3adAggPortPartnerOperSystemID       XDRMACAddress
	Dot3adAggPortAttachedAggID             uint32
	Dot3adAggPortState                     [4]byte
	Dot3adAggPortStatsLACPDUsRx            uint32
	Dot3adAggPortStatsMarkerPDUsRx         uint32
	Dot3adAggPortStatsMarkerResponsePDUsRx uint32
	Dot3adAggPortStatsUnknownRx            uint32
	Dot3adAggPortStatsIllegalRx            uint32
	Dot3adAggPortStatsLACPDUsTx            uint32
	Dot3adAggPortStatsMarkerPDUsTx         uint32
	Dot3adAggPortStatsMarkerResponsePDUsTx uint32
}

func (v *LAGPortStats) GetHeader() RecordHeader {
	return v.RecordHeader
}

var LAGPortStatsRecordValidLength = packetSizeOf(LAGPortStats{}) - RecordHeaderSize

const LAGPortStatsRecordDataFormatValue uint32 = 7

// ProcessorCounters see https://sflow.org/sflow_version_5.txt , Pag 42, `processor`
type ProcessorCounters struct {
	RecordHeader
	CPU5s       uint32
	CPU1m       uint32
	CPU5m       uint32
	TotalMemory uint64
	FreeMemory  uint64
}

func (v *ProcessorCounters) GetHeader() RecordHeader {
	return v.RecordHeader
}

var ProcessorCountersRecordValidLength = packetSizeOf(ProcessorCounters{}) - RecordHeaderSize

const ProcessorCountersRecordDataFormatValue uint32 = 1001

// QueueLength see https://groups.google.com/g/sflow/c/dz0nsXqBYAw, `queue_length`
type QueueLength struct {
	RecordHeader
	QueueIndex      uint32
	SegmentSize     uint32
	QueueSegments   uint32
	QueueLength0    uint32
	QueueLength1    uint32
	QueueLength2    uint32
	QueueLength4    uint32
	QueueLength8    uint32
	QueueLength32   uint32
	QueueLength128  uint32
	QueueLength1024 uint32
	QueueLengthMore uint32
	Dropped         uint32
}

func (v *QueueLength) GetHeader() RecordHeader {
	return v.RecordHeader
}

var QueueLengthRecordValidLength = packetSizeOf(QueueLength{}) - RecordHeaderSize

const QueueLengthRecordDataFormatValue uint32 = 1003

// OpenFlowPort see https://sflow.org/sflow_openflow.txt, Pag 2, `of_port`
type OpenFlowPort struct {
	RecordHeader
	DataPathID uint64
	PortNumber uint32
}

func (v *OpenFlowPort) GetHeader() RecordHeader {
	return v.RecordHeader
}

var OpenFlowPortRecordValidLength = packetSizeOf(OpenFlowPort{}) - RecordHeaderSize

const OpenFlowPortRecordDataFormatValue uint32 = 1004

// OpenFlowPortName see https://sflow.org/sflow_openflow.txt, Pag 2, `port_name`
type OpenFlowPortName struct {
	RecordHeader
	Name XDRString // 128 max length
}

func (v *OpenFlowPortName) GetHeader() RecordHeader {
	return v.RecordHeader
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
	HostName    XDRString // max size 64 bytes
	UUID        SFlowUUID // fixed size 16 bytes
	MachineType uint32
	OSName      uint32
	OSRelease   XDRString // max size 32 bytes
}

func (v *HostDescr) GetHeader() RecordHeader {
	return v.RecordHeader
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
	Adapters []HostAdapter
}

type HostAdapter struct {
	IFIndex      uint32
	MACAddresses []XDRMACAddress
}

func (v *HostAdapters) GetHeader() RecordHeader {
	return v.RecordHeader
}

const HostAdaptersRecordDataFormatValue uint32 = 2001

// HostParent see https://sflow.org/sflow_host.txt, Pag 8, `host_parent`
type HostParent struct {
	RecordHeader
	ContainerType  uint32
	ContainerIndex uint32
}

func (v *HostParent) GetHeader() RecordHeader {
	return v.RecordHeader
}

var HostParentRecordValidLength = packetSizeOf(HostParent{}) - RecordHeaderSize

const HostParentRecordDataFormatValue uint32 = 2002

// HostCPU see https://sflow.org/sflow_host.txt, Pag 8, `host_cpu`
// The official spec defines 17 fields (68 bytes), but host-sflow added
// cpu_steal, cpu_guest, cpu_guest_nice fields (80 bytes total).
// See https://github.com/sflow/host-sflow/commit/8c326ac
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
	// Extension
	CPUSteal     uint32
	CPUGuest     uint32
	CPUGuestNice uint32
}

func (v *HostCPU) GetHeader() RecordHeader {
	return v.RecordHeader
}

var (
	HostCPURecordExtendedValidLength = packetSizeOf(HostCPU{}) - RecordHeaderSize
	HostCPURecordValidLength         = HostCPURecordExtendedValidLength - 12 // 12 = CPUSteal(4) + CPUGuest(4) + CPUGuestNice(4)
)

const HostCPURecordDataFormatValue uint32 = 2003

// HostMemory see https://sflow.org/sflow_host.txt, Pag 9, `host_memory`
type HostMemory struct {
	RecordHeader
	MemTotal   uint64
	MemFree    uint64
	MemShared  uint64
	MemBuffers uint64
	MemCached  uint64
	SwapTotal  uint64
	SwapFree   uint64
	PageIn     uint32
	PageOut    uint32
	SwapIn     uint32
	SwapOut    uint32
}

func (v *HostMemory) GetHeader() RecordHeader {
	return v.RecordHeader
}

var HostMemoryRecordValidLength = packetSizeOf(HostMemory{}) - RecordHeaderSize

const HostMemoryRecordDataFormatValue uint32 = 2004

// HostDiskIO see https://sflow.org/sflow_host.txt, Pag 9, `host_disk_io`
type HostDiskIO struct {
	RecordHeader
	DiskTotal uint64
	DiskFree  uint64
	// Spec uses "percentage" type but doesn't define it; sflowtool treats as uint32 hundredths (1666 = 16.66%)
	MaxUsedPartitionPercent uint32
	Reads                   uint32
	BytesRead               uint64
	ReadTime                uint32
	Writes                  uint32
	BytesWritten            uint64
	WriteTime               uint32
}

func (v *HostDiskIO) GetHeader() RecordHeader {
	return v.RecordHeader
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

func (v *HostNetIO) GetHeader() RecordHeader {
	return v.RecordHeader
}

var HostNetIORecordValidLength = packetSizeOf(HostNetIO{}) - RecordHeaderSize

const HostNetIORecordDataFormatValue uint32 = 2006

// MIB2IPGroup see https://sflow.org/sflow_host_ip.txt, Pag 2, `mib2_ip_group`
type MIB2IPGroup struct {
	RecordHeader
	IPForwarding      uint32
	IPDefaultTTL      uint32
	IPInReceives      uint32
	IPInHdrErrors     uint32
	IPInAddrErrors    uint32
	IPForwDatagrams   uint32
	IPInUnknownProtos uint32
	IPInDiscards      uint32
	IPInDelivers      uint32
	IPOutRequests     uint32
	IPOutDiscards     uint32
	IPOutNoRoutes     uint32
	IPReasmTimeout    uint32
	IPReasmReqds      uint32
	IPReasmOKs        uint32
	IPReasmFails      uint32
	IPFragOKs         uint32
	IPFragFails       uint32
	IPFragCreates     uint32
}

func (v *MIB2IPGroup) GetHeader() RecordHeader {
	return v.RecordHeader
}

var MIB2IPGroupRecordValidLength = packetSizeOf(MIB2IPGroup{}) - RecordHeaderSize

const MIB2IPGroupRecordDataFormatValue uint32 = 2007

// MIB2ICMPGroup see https://sflow.org/sflow_host_ip.txt, Pag 2, `mib2_icmp_group`
type MIB2ICMPGroup struct {
	RecordHeader
	ICMPInMsgs           uint32
	ICMPInErrors         uint32
	ICMPInDestUnreachs   uint32
	ICMPInTimeExcds      uint32
	ICMPInParamProbs     uint32
	ICMPInSrcQuenchs     uint32
	ICMPInRedirects      uint32
	ICMPInEchos          uint32
	ICMPInEchoReps       uint32
	ICMPInTimestamps     uint32
	ICMPInAddrMasks      uint32
	ICMPInAddrMaskReps   uint32
	ICMPOutMsgs          uint32
	ICMPOutErrors        uint32
	ICMPOutDestUnreachs  uint32
	ICMPOutTimeExcds     uint32
	ICMPOutParamProbs    uint32
	ICMPOutSrcQuenchs    uint32
	ICMPOutRedirects     uint32
	ICMPOutEchos         uint32
	ICMPOutEchoReps      uint32
	ICMPOutTimestamps    uint32
	ICMPOutTimestampReps uint32
	ICMPOutAddrMasks     uint32
	ICMPOutAddrMaskReps  uint32
}

func (v *MIB2ICMPGroup) GetHeader() RecordHeader {
	return v.RecordHeader
}

var MIB2ICMPGroupRecordValidLength = packetSizeOf(MIB2ICMPGroup{}) - RecordHeaderSize

const MIB2ICMPGroupRecordDataFormatValue uint32 = 2008

// MIB2TCPGroup see https://sflow.org/sflow_host_ip.txt, Pag 2, `mib2_tcp_group`
type MIB2TCPGroup struct {
	RecordHeader
	TCPRtoAlgorithm uint32
	TCPRtoMin       uint32
	TCPRtoMax       uint32
	TCPMaxConn      uint32
	TCPActiveOpens  uint32
	TCPPassiveOpens uint32
	TCPAttemptFails uint32
	TCPEstabResets  uint32
	TCPCurrEstab    uint32
	TCPInSegs       uint32
	TCPOutSegs      uint32
	TCPRetransSegs  uint32
	TCPInErrs       uint32
	TCPOutRsts      uint32
	TCPInCsumErrors uint32
}

func (v *MIB2TCPGroup) GetHeader() RecordHeader {
	return v.RecordHeader
}

var MIB2TCPGroupRecordValidLength = packetSizeOf(MIB2TCPGroup{}) - RecordHeaderSize

const MIB2TCPGroupRecordDataFormatValue uint32 = 2009

// MIB2UDPGroup see https://sflow.org/sflow_host_ip.txt, Pag 2, `mib2_udp_group`
type MIB2UDPGroup struct {
	RecordHeader
	UDPInDatagrams  uint32
	UDPNoPorts      uint32
	UDPInErrors     uint32
	UDPOutDatagrams uint32
	UDPRcvbufErrors uint32
	UDPSndbufErrors uint32
	UDPInCsumErrors uint32
}

func (v *MIB2UDPGroup) GetHeader() RecordHeader {
	return v.RecordHeader
}

var MIB2UDPGroupRecordValidLength = packetSizeOf(MIB2UDPGroup{}) - RecordHeaderSize

const MIB2UDPGroupRecordDataFormatValue uint32 = 2010

// VirtNode see https://sflow.org/sflow_host.txt, Pag 10, `virt_node`
type VirtNode struct {
	RecordHeader
	Mhz        uint32
	CPUs       uint32
	Memory     uint64
	MemoryFree uint64
	NumDomains uint32
}

func (v *VirtNode) GetHeader() RecordHeader {
	return v.RecordHeader
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

func (v *VirtCPU) GetHeader() RecordHeader {
	return v.RecordHeader
}

var VirtCPURecordValidLength = packetSizeOf(VirtCPU{}) - RecordHeaderSize

const VirtCPURecordDataFormatValue uint32 = 2101

// VirtMemory see https://sflow.org/sflow_host.txt, Pag 10, `virt_memory`
type VirtMemory struct {
	RecordHeader
	Memory    uint64
	MaxMemory uint64
}

func (v *VirtMemory) GetHeader() RecordHeader {
	return v.RecordHeader
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

func (v *VirtDiskIO) GetHeader() RecordHeader {
	return v.RecordHeader
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

func (v *VirtNetIO) GetHeader() RecordHeader {
	return v.RecordHeader
}

var VirtNetIORecordValidLength = packetSizeOf(VirtNetIO{}) - RecordHeaderSize

const VirtNetIORecordDataFormatValue uint32 = 2104

// JVMMachineName see https://sflow.org/sflow_jvm.txt, Pag 2, `jvm_runtime`
type JVMMachineName struct {
	RecordHeader
	VMName    XDRString // max size 64 bytes
	VMVendor  XDRString // max size 32 bytes
	VMVersion XDRString // max size 32 bytes
}

func (v *JVMMachineName) GetHeader() RecordHeader {
	return v.RecordHeader
}

const (
	JVMMachineNameRecordDataFormatValue uint32 = 2105

	vmNameLenSize    = 4
	VMNameMaxSize    = 64
	vmVendorLenSize  = 4
	VMVendorMaxSize  = 32
	vmVersionLenSize = 4
	VMVersionMaxSize = 32

	JVMMachineNameRecordMaxLength = vmNameLenSize + VMNameMaxSize + vmVendorLenSize + VMVendorMaxSize + vmVersionLenSize + VMVersionMaxSize // 140 Bytes
)

// JVMStatistics see https://sflow.org/sflow_jvm.txt, Pag 2, `jvm_statistics`
type JVMStatistics struct {
	RecordHeader
	HeapInitial       uint64
	HeapUsed          uint64
	HeapCommitted     uint64
	HeapMax           uint64
	NonHeapInitial    uint64
	NonHeapUsed       uint64
	NonHeapCommitted  uint64
	NonHeapMax        uint64
	GCCount           uint32
	GCTime            uint32
	ClassesLoaded     uint32
	ClassesTotal      uint32
	ClassesUnloaded   uint32
	CompilationTime   uint32
	ThreadNumLive     uint32
	ThreadNumDaemon   uint32
	ThreadNumStarted  uint32
	OpenFileDescCount uint32
	MaxFileDescCount  uint32
}

func (v *JVMStatistics) GetHeader() RecordHeader {
	return v.RecordHeader
}

var JVMStatisticsRecordValidLength = packetSizeOf(JVMStatistics{}) - RecordHeaderSize

const JVMStatisticsRecordDataFormatValue uint32 = 2106

// HTTPCounters see https://sflow.org/sflow_http.txt, Pag 3, `http_counters`
type HTTPCounters struct {
	RecordHeader
	MethodOptionCount  uint32
	MethodGetCount     uint32
	MethodHeadCount    uint32
	MethodPostCount    uint32
	MethodPutCount     uint32
	MethodDeleteCount  uint32
	MethodTraceCount   uint32
	MethodConnectCount uint32
	MethodOtherCount   uint32
	Status1XXCount     uint32
	Status2XXCount     uint32
	Status3XXCount     uint32
	Status4XXCount     uint32
	Status5XXCount     uint32
	StatusOtherCount   uint32
}

func (v *HTTPCounters) GetHeader() RecordHeader {
	return v.RecordHeader
}

var HTTPCountersRecordValidLength = packetSizeOf(HTTPCounters{}) - RecordHeaderSize

const HTTPCountersRecordDataFormatValue uint32 = 2201

// Application see https://sflow.org/sflow_application.txt, Pag 3
type Application = XDRString

// AppOperations see https://sflow.org/sflow_application.txt, Pag 5, `app_operations`
type AppOperations struct {
	RecordHeader
	Application    Application // max size 32 bytes
	Success        uint32
	Other          uint32
	Timeout        uint32
	InternalError  uint32
	BadRequest     uint32
	Forbidden      uint32
	TooLarge       uint32
	NotImplemented uint32
	NotFound       uint32
	Unavailable    uint32
	Unauthorized   uint32
}

func (v *AppOperations) GetHeader() RecordHeader {
	return v.RecordHeader
}

const (
	AppOperationsRecordDataFormatValue uint32 = 2202

	applicationLenSize = 4
	ApplicationMaxSize = 32

	successSize        = 4
	otherSize          = 4
	timeoutSize        = 4
	internalErrorSize  = 4
	badRequestSize     = 4
	forbiddenSize      = 4
	tooLargeSize       = 4
	notImplementedSize = 4
	notFoundSize       = 4
	unavailableSize    = 4
	unauthorizedSize   = 4

	AppOperationsRecordMaxLength = applicationLenSize + ApplicationMaxSize +
		successSize + otherSize + timeoutSize + internalErrorSize + badRequestSize +
		forbiddenSize + tooLargeSize + notImplementedSize + notFoundSize +
		unavailableSize + unauthorizedSize // 80 bytes
)

// AppResources see https://sflow.org/sflow_application.txt, Pag 5, `app_resources`
type AppResources struct {
	RecordHeader
	UserTime   uint32
	SystemTime uint32
	MemUsed    uint64
	MemMax     uint64
	FDOpen     uint32
	FDMax      uint32
	ConnOpen   uint32
	ConnMax    uint32
}

func (v *AppResources) GetHeader() RecordHeader {
	return v.RecordHeader
}

var AppResourcesRecordValidLength = packetSizeOf(AppResources{}) - RecordHeaderSize

const AppResourcesRecordDataFormatValue uint32 = 2203

// MemcacheCounters see https://sflow.org/sflow_memcache.txt, Pag 4, `memcache_counters`
type MemcacheCounters struct {
	RecordHeader
	CmdSet               uint32
	CmdTouch             uint32
	CmdFlush             uint32
	GetHits              uint32
	GetMisses            uint32
	DeleteHits           uint32
	DeleteMisses         uint32
	IncrHits             uint32
	IncrMisses           uint32
	DecrHits             uint32
	DecrMisses           uint32
	CasHits              uint32
	CasMisses            uint32
	CasBadval            uint32
	AuthCmds             uint32
	AuthErrors           uint32
	Threads              uint32
	ConnYields           uint32
	ListenDisabledNum    uint32
	CurrConnections      uint32
	RejectedConnections  uint32
	TotalConnections     uint32
	ConnectionStructures uint32
	Evictions            uint32
	Reclaimed            uint32
	CurrItems            uint32
	TotalItems           uint32
	BytesRead            uint64
	BytesWritten         uint64
	Bytes                uint64
	LimitMaxbytes        uint64
}

func (v *MemcacheCounters) GetHeader() RecordHeader {
	return v.RecordHeader
}

var MemcacheCountersRecordValidLength = packetSizeOf(MemcacheCounters{}) - RecordHeaderSize

const MemcacheCountersRecordDataFormatValue uint32 = 2204

// AppWorkers see https://sflow.org/sflow_application.txt, Pag 6, `app_workers`
type AppWorkers struct {
	RecordHeader
	WorkersActive uint32
	WorkersIdle   uint32
	WorkersMax    uint32
	ReqDelayed    uint32
	ReqDropped    uint32
}

func (v *AppWorkers) GetHeader() RecordHeader {
	return v.RecordHeader
}

var AppWorkersRecordValidLength = packetSizeOf(AppWorkers{}) - RecordHeaderSize

const AppWorkersRecordDataFormatValue uint32 = 2206

// OVSDPStats see https://blog.sflow.com/2015/01/open-vswitch-performance-monitoring.html, `ovs_dp_stats`
type OVSDPStats struct {
	RecordHeader
	Hits     uint32
	Misses   uint32
	Lost     uint32
	MaskHits uint32
	Flows    uint32
	Masks    uint32
}

func (v *OVSDPStats) GetHeader() RecordHeader {
	return v.RecordHeader
}

var OVSDPStatsRecordValidLength = packetSizeOf(OVSDPStats{}) - RecordHeaderSize

const OVSDPStatsRecordDataFormatValue uint32 = 2207

// Energy see https://groups.google.com/g/sflow/c/gN3nxSi2SBs, `energy`
type Energy struct {
	RecordHeader
	Voltage     uint32
	Current     uint32
	RealPower   uint32
	PowerFactor int32
	Energy      uint32
	Errors      uint32
}

func (v *Energy) GetHeader() RecordHeader {
	return v.RecordHeader
}

var EnergyRecordValidLength = packetSizeOf(Energy{}) - RecordHeaderSize

const EnergyRecordDataFormatValue uint32 = 3000

// Temperature see https://groups.google.com/g/sflow/c/gN3nxSi2SBs, `temperature`
type Temperature struct {
	RecordHeader
	Minimum int32
	Maximum int32
	Errors  uint32
}

func (v *Temperature) GetHeader() RecordHeader {
	return v.RecordHeader
}

var TemperatureRecordValidLength = packetSizeOf(Temperature{}) - RecordHeaderSize

const TemperatureRecordDataFormatValue uint32 = 3001

// Humidity see https://groups.google.com/g/sflow/c/gN3nxSi2SBs, `humidity`
type Humidity struct {
	RecordHeader
	Relative int32
}

func (v *Humidity) GetHeader() RecordHeader {
	return v.RecordHeader
}

var HumidityRecordValidLength = packetSizeOf(Humidity{}) - RecordHeaderSize

const HumidityRecordDataFormatValue uint32 = 3002

// Fans see https://groups.google.com/g/sflow/c/gN3nxSi2SBs, `fans`
type Fans struct {
	RecordHeader
	Total  uint32
	Failed uint32
	Speed  uint32
}

func (v *Fans) GetHeader() RecordHeader {
	return v.RecordHeader
}

var FansRecordValidLength = packetSizeOf(Fans{}) - RecordHeaderSize

const FansRecordDataFormatValue uint32 = 3003
