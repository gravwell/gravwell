package datagram

import "unsafe"

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

// TokenRingCounters see https://sflow.org/sflow_version_5.txt , Pag 41, `tokenring_counters`
type TokenRingCounters struct {
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

func (tr *TokenRingCounters) GetDataFormat() uint32 {
	return tr.DataFormat
}

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
