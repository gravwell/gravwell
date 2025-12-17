/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package decoder

import (
	"encoding/binary"
	"errors"
	"io"

	"github.com/gravwell/gravwell/v3/sflow/datagram"
)

var (
	ErrInvalidCounterIfRecordSize         = errors.New("counter if record size is invalid")
	ErrInvalidEthernetCountersRecordSize  = errors.New("counter ethernet record size is invalid")
	ErrInvalidTokenringCountersRecordSize = errors.New("counter tokenring record size is invalid")
	ErrInvalidVgCountersRecordSize        = errors.New("counter vg record size is invalid")
	ErrInvalidVlanCountersRecordSize      = errors.New("counter vlan record size is invalid")
	ErrInvalidIEEE80211CountersRecordSize = errors.New("ieee80211 counters record size is invalid")
	ErrInvalidLAGPortStatsRecordSize      = errors.New("lag port stats record size is invalid")
	ErrInvalidProcessorCountersRecordSize = errors.New("counter processor record size is invalid")
	ErrInvalidQueueLengthRecordSize       = errors.New("queue length record size is invalid")
	ErrInvalidOpenFlowPortRecordSize      = errors.New("openflow port record size is invalid")
	ErrInvalidOpenFlowPortNameRecordSize  = errors.New("openflow port name record size is invalid")
	ErrOpenFlowPortNameTooLong            = errors.New("openflow port name exceeds maximum length")
	ErrInvalidHostDescrRecordSize         = errors.New("host descr record size is invalid")
	ErrHostNameTooLong                    = errors.New("hostname exceeds maximum length")
	ErrOSReleaseTooLong                   = errors.New("OS release exceeds maximum length")
	ErrInvalidHostParentRecordSize        = errors.New("host parent record size is invalid")
	ErrInvalidHostCPURecordSize           = errors.New("host cpu record size is invalid")
	ErrInvalidHostMemoryRecordSize        = errors.New("host memory record size is invalid")
	ErrInvalidHostDiskIORecordSize        = errors.New("host disk io record size is invalid")
	ErrInvalidHostNetIORecordSize         = errors.New("host net io record size is invalid")
	ErrInvalidVirtNodeRecordSize          = errors.New("virt node record size is invalid")
	ErrInvalidVirtCPURecordSize           = errors.New("virt cpu record size is invalid")
	ErrInvalidVirtMemoryRecordSize        = errors.New("virt memory record size is invalid")
	ErrInvalidVirtDiskIORecordSize        = errors.New("virt disk io record size is invalid")
	ErrInvalidVirtNetIORecordSize         = errors.New("virt net io record size is invalid")
	ErrInvalidJVMMachineNameRecordSize    = errors.New("jvm machine name record size is invalid")
	ErrJVMVMNameTooLong                   = errors.New("JVM vm name exceeds maximum length")
	ErrJVMVMVendorTooLong                 = errors.New("JVM vm vendor exceeds maximum length")
	ErrJVMVMVersionTooLong                = errors.New("JVM vm version exceeds maximum length")
	ErrInvalidJVMStatisticsRecordSize     = errors.New("JVM statistics record size is invalid")
	ErrInvalidHTTPCountersRecordSize      = errors.New("http counters record size is invalid")
	ErrInvalidAppOperationsRecordSize     = errors.New("app operations record size is invalid")
	ErrApplicationTooLong                 = errors.New("application name exceeds maximum length")
	ErrInvalidAppResourcesRecordSize      = errors.New("app resources record size is invalid")
	ErrInvalidMemcacheCountersRecordSize  = errors.New("memcache counters record size is invalid")
	ErrInvalidAppWorkersRecordSize        = errors.New("app workers record size is invalid")
	ErrInvalidEnergyRecordSize            = errors.New("energy record size is invalid")
	ErrInvalidTemperatureRecordSize       = errors.New("temperature record size is invalid")
	ErrInvalidHumidityRecordSize          = errors.New("humidity record size is invalid")
	ErrInvalidFansRecordSize              = errors.New("fans record size is invalid")
	ErrInvalidMIB2IPGroupRecordSize       = errors.New("mib2 ip group record size is invalid")
	ErrInvalidMIB2ICMPGroupRecordSize     = errors.New("mib2 icmp group record size is invalid")
	ErrInvalidMIB2TCPGroupRecordSize      = errors.New("mib2 tcp group record size is invalid")
	ErrInvalidMIB2UDPGroupRecordSize      = errors.New("mib2 udp group record size is invalid")
	ErrInvalidOVSDPStatsRecordSize        = errors.New("ovs dp stats record size is invalid")
)

func decodeCounterIfRecord(r io.Reader) (*datagram.CounterIfRecord, error) {
	cir := datagram.CounterIfRecord{
		RecordHeader: datagram.RecordHeader{
			Format: datagram.CounterIfRecordDataFormatValue,
		},
	}

	if err := binary.Read(r, binary.BigEndian, &cir.Length); err != nil {
		return nil, err
	}

	if cir.Length != uint32(datagram.CounterIfRecordValidLength) {
		return nil, ErrInvalidCounterIfRecordSize
	}

	if err := binary.Read(r, binary.BigEndian, &cir.IfIndex); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &cir.IfType); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &cir.IfSpeed); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &cir.IfDirection); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &cir.IfStatus); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &cir.IfInOctets); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &cir.IfInUcastPkts); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &cir.IfInMulticastPkts); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &cir.IfInBroadcastPkts); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &cir.IfInDiscards); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &cir.IfInErrors); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &cir.IfInUnknownProtos); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &cir.IfOutOctets); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &cir.IfOutUcastPkts); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &cir.IfOutMulticastPkts); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &cir.IfOutBroadcastPkts); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &cir.IfOutDiscards); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &cir.IfOutErrors); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &cir.IfPromiscuousMode); err != nil {
		return nil, err
	}

	return &cir, nil
}

func decodeEthernetCountersRecord(r io.Reader) (*datagram.EthernetCounters, error) {
	ecr := datagram.EthernetCounters{
		RecordHeader: datagram.RecordHeader{
			Format: datagram.EthernetCountersRecordDataFormatValue,
		},
	}

	if err := binary.Read(r, binary.BigEndian, &ecr.Length); err != nil {
		return nil, err
	}

	if ecr.Length != uint32(datagram.EthernetCountersRecordValidLength) {
		return nil, ErrInvalidEthernetCountersRecordSize
	}

	if err := binary.Read(r, binary.BigEndian, &ecr.Dot3StatsAlignmentErrors); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &ecr.Dot3StatsFCSErrors); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &ecr.Dot3StatsSingleCollisionFrames); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &ecr.Dot3StatsMultipleCollisionFrames); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &ecr.Dot3StatsSQETestErrors); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &ecr.Dot3StatsDeferredTransmissions); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &ecr.Dot3StatsLateCollisions); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &ecr.Dot3StatsExcessiveCollisions); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &ecr.Dot3StatsExcessiveCollisions); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &ecr.Dot3StatsInternalMacTransmitErrors); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &ecr.Dot3StatsCarrierSenseErrors); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &ecr.Dot3StatsFrameTooLongs); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &ecr.Dot3StatsInternalMacReceiveErrors); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &ecr.Dot3StatsSymbolErrors); err != nil {
		return nil, err
	}

	return &ecr, nil
}

func decordTokenringCountersRecord(r io.Reader) (*datagram.TokenringCounters, error) {
	trc := datagram.TokenringCounters{
		RecordHeader: datagram.RecordHeader{
			Format: datagram.TokenringCountersRecordDataFormatValue,
		},
	}

	if err := binary.Read(r, binary.BigEndian, &trc.Length); err != nil {
		return nil, err
	}

	if trc.Length != uint32(datagram.TokenringCountersRecordValidLength) {
		return nil, ErrInvalidTokenringCountersRecordSize
	}

	if err := binary.Read(r, binary.BigEndian, &trc.Dot3StatsLineErrors); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &trc.Dot3StatsBurstErrors); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &trc.Dot3StatsACErrors); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &trc.Dot3StatsAbortTransErrors); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &trc.Dot3StatsInternalErrors); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &trc.Dot3StatsLostFrameErrors); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &trc.Dot3StatsReceiveCongestions); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &trc.Dot3StatsFrameCopiedErrors); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &trc.Dot3StatsTokenErrors); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &trc.Dot3StatsSoftErrors); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &trc.Dot3StatsHardErrors); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &trc.Dot3StatsSignalLoss); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &trc.Dot3StatsTransmitBeacons); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &trc.Dot3StatsRecoverys); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &trc.Dot3StatsLobeWires); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &trc.Dot3StatsRemoves); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &trc.Dot3StatsSingles); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &trc.Dot3StatsFreqErrors); err != nil {
		return nil, err
	}

	return &trc, nil
}

func decodeVgCountersRecord(r io.Reader) (*datagram.VgCounters, error) {
	vgc := datagram.VgCounters{
		RecordHeader: datagram.RecordHeader{
			Format: datagram.VgCountersRecordDataFormatValue,
		},
	}

	if err := binary.Read(r, binary.BigEndian, &vgc.Length); err != nil {
		return nil, err
	}

	if vgc.Length != uint32(datagram.VgCountersRecordValidLength) {
		return nil, ErrInvalidVgCountersRecordSize
	}

	if err := binary.Read(r, binary.BigEndian, &vgc.Dot12InHighPriorityFrames); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &vgc.Dot12InHighPriorityOctets); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &vgc.Dot12InNormPriorityFrames); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &vgc.Dot12InNormPriorityOctets); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &vgc.Dot12InIPMErrors); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &vgc.Dot12InOversizeFrameErrors); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &vgc.Dot12InDataErrors); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &vgc.Dot12InNullAddressedFrames); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &vgc.Dot12OutHighPriorityFrames); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &vgc.Dot12OutHighPriorityOctets); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &vgc.Dot12TransitionIntoTrainings); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &vgc.Dot12HCInHighPriorityOctets); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &vgc.Dot12HCInNormPriorityOctets); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &vgc.Dot12HCOutHighPriorityOctets); err != nil {
		return nil, err
	}

	return &vgc, nil
}

func decodeVlanCountersRecord(r io.Reader) (*datagram.VlanCounters, error) {
	vlc := datagram.VlanCounters{
		RecordHeader: datagram.RecordHeader{
			Format: datagram.VlanCountersRecordDataFormatValue,
		},
	}

	if err := binary.Read(r, binary.BigEndian, &vlc.Length); err != nil {
		return nil, err
	}

	if vlc.Length != uint32(datagram.VlanCountersRecordValidLength) {
		return nil, ErrInvalidVlanCountersRecordSize
	}

	if err := binary.Read(r, binary.BigEndian, &vlc.ID); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &vlc.Octets); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &vlc.UnicastPackets); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &vlc.MulticastPackets); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &vlc.BroadcastPackets); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &vlc.Discards); err != nil {
		return nil, err
	}

	return &vlc, nil
}

func decodeIEEE80211CountersRecord(r io.Reader) (*datagram.IEEE80211Counters, error) {
	i8c := datagram.IEEE80211Counters{
		RecordHeader: datagram.RecordHeader{
			Format: datagram.IEEE80211CountersRecordDataFormatValue,
		},
	}

	if err := binary.Read(r, binary.BigEndian, &i8c.Length); err != nil {
		return nil, err
	}

	if i8c.Length != uint32(datagram.IEEE80211CountersRecordValidLength) {
		return nil, ErrInvalidIEEE80211CountersRecordSize
	}

	if err := binary.Read(r, binary.BigEndian, &i8c.Dot11TransmittedFragmentCount); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &i8c.Dot11MulticastTransmittedFrameCount); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &i8c.Dot11FailedCount); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &i8c.Dot11RetryCount); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &i8c.Dot11MultipleRetryCount); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &i8c.Dot11FrameDuplicateCount); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &i8c.Dot11RTSSuccessCount); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &i8c.Dot11RTSFailureCount); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &i8c.Dot11ACKFailureCount); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &i8c.Dot11ReceivedFragmentCount); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &i8c.Dot11MulticastReceivedFrameCount); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &i8c.Dot11FCSErrorCount); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &i8c.Dot11TransmittedFrameCount); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &i8c.Dot11WEPUndecryptableCount); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &i8c.Dot11QoSDiscardedFragmentCount); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &i8c.Dot11AssociatedStationCount); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &i8c.Dot11QoSCFPollsReceivedCount); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &i8c.Dot11QoSCFPollsUnusedCount); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &i8c.Dot11QoSCFPollsUnusableCount); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &i8c.Dot11QoSCFPollsLostCount); err != nil {
		return nil, err
	}

	return &i8c, nil
}

func decodeLAGPortStatsRecord(r io.Reader) (*datagram.LAGPortStats, error) {
	lps := datagram.LAGPortStats{
		RecordHeader: datagram.RecordHeader{
			Format: datagram.LAGPortStatsRecordDataFormatValue,
		},
	}

	if err := binary.Read(r, binary.BigEndian, &lps.Length); err != nil {
		return nil, err
	}

	if lps.Length != uint32(datagram.LAGPortStatsRecordValidLength) {
		return nil, ErrInvalidLAGPortStatsRecordSize
	}

	if err := binary.Read(r, binary.BigEndian, &lps.Dot3adAggPortActorSystemID); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &lps.Dot3adAggPortPartnerOperSystemID); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &lps.Dot3adAggPortAttachedAggID); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &lps.Dot3adAggPortState); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &lps.Dot3adAggPortStatsLACPDUsRx); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &lps.Dot3adAggPortStatsMarkerPDUsRx); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &lps.Dot3adAggPortStatsMarkerResponsePDUsRx); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &lps.Dot3adAggPortStatsUnknownRx); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &lps.Dot3adAggPortStatsIllegalRx); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &lps.Dot3adAggPortStatsLACPDUsTx); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &lps.Dot3adAggPortStatsMarkerPDUsTx); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &lps.Dot3adAggPortStatsMarkerResponsePDUsTx); err != nil {
		return nil, err
	}

	return &lps, nil
}

func decodeProcessorCountersRecord(r io.Reader) (*datagram.ProcessorCounters, error) {
	pc := datagram.ProcessorCounters{
		RecordHeader: datagram.RecordHeader{
			Format: datagram.ProcessorCountersRecordDataFormatValue,
		},
	}

	if err := binary.Read(r, binary.BigEndian, &pc.Length); err != nil {
		return nil, err
	}

	if pc.Length != uint32(datagram.ProcessorCountersRecordValidLength) {
		return nil, ErrInvalidProcessorCountersRecordSize
	}

	if err := binary.Read(r, binary.BigEndian, &pc.CPU5s); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &pc.CPU1m); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &pc.CPU5m); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &pc.TotalMemory); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &pc.FreeMemory); err != nil {
		return nil, err
	}

	return &pc, nil
}

func decodeQueueLengthRecord(r io.Reader) (*datagram.QueueLength, error) {
	ql := datagram.QueueLength{
		RecordHeader: datagram.RecordHeader{
			Format: datagram.QueueLengthRecordDataFormatValue,
		},
	}

	if err := binary.Read(r, binary.BigEndian, &ql.Length); err != nil {
		return nil, err
	}

	if ql.Length != uint32(datagram.QueueLengthRecordValidLength) {
		return nil, ErrInvalidQueueLengthRecordSize
	}

	if err := binary.Read(r, binary.BigEndian, &ql.QueueIndex); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &ql.SegmentSize); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &ql.QueueSegments); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &ql.QueueLength0); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &ql.QueueLength1); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &ql.QueueLength2); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &ql.QueueLength4); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &ql.QueueLength8); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &ql.QueueLength32); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &ql.QueueLength128); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &ql.QueueLength1024); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &ql.QueueLengthMore); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &ql.Dropped); err != nil {
		return nil, err
	}

	return &ql, nil
}

func decodeOpenFlowPortRecord(r io.Reader) (*datagram.OpenFlowPort, error) {
	ofp := datagram.OpenFlowPort{
		RecordHeader: datagram.RecordHeader{
			Format: datagram.OpenFlowPortRecordDataFormatValue,
		},
	}

	if err := binary.Read(r, binary.BigEndian, &ofp.Length); err != nil {
		return nil, err
	}

	if ofp.Length != uint32(datagram.OpenFlowPortRecordValidLength) {
		return nil, ErrInvalidOpenFlowPortRecordSize
	}

	if err := binary.Read(r, binary.BigEndian, &ofp.DataPathID); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &ofp.PortNumber); err != nil {
		return nil, err
	}

	return &ofp, nil
}

func decodeOpenFlowPortNameRecord(r io.Reader) (*datagram.OpenFlowPortName, error) {
	ofpn := datagram.OpenFlowPortName{
		RecordHeader: datagram.RecordHeader{
			Format: datagram.OpenFlowPortNameRecordDataFormatValue,
		},
	}

	if err := binary.Read(r, binary.BigEndian, &ofpn.Length); err != nil {
		return nil, err
	}

	if ofpn.Length > uint32(datagram.OpenFlowPortNameRecordMaxLength) {
		return nil, ErrInvalidOpenFlowPortNameRecordSize
	}

	var err error
	ofpn.Name, err = decodeXDRString(r)
	if err != nil {
		return nil, err
	}

	if ofpn.Name.Len() > datagram.OpenFlowPortNameMaxLength {
		return nil, ErrOpenFlowPortNameTooLong
	}

	return &ofpn, nil
}

func decodeHostDescrRecord(r io.Reader) (*datagram.HostDescr, error) {
	hd := datagram.HostDescr{
		RecordHeader: datagram.RecordHeader{
			Format: datagram.HostDescrRecordDataFormatValue,
		},
	}

	if err := binary.Read(r, binary.BigEndian, &hd.Length); err != nil {
		return nil, err
	}

	if hd.Length > uint32(datagram.HostDescrRecordMaxLength) {
		return nil, ErrInvalidHostDescrRecordSize
	}

	var err error
	hd.HostName, err = decodeXDRString(r)
	if err != nil {
		return nil, err
	}

	if hd.HostName.Len() > datagram.HostNameMaxSize {
		return nil, ErrHostNameTooLong
	}

	if err := binary.Read(r, binary.BigEndian, &hd.UUID); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &hd.MachineType); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &hd.OSName); err != nil {
		return nil, err
	}

	hd.OSRelease, err = decodeXDRString(r)
	if err != nil {
		return nil, err
	}

	if hd.OSRelease.Len() > datagram.OSReleaseMaxSize {
		return nil, ErrOSReleaseTooLong
	}

	return &hd, nil
}

func decodeHostAdaptersRecord(r io.Reader) (*datagram.HostAdapters, error) {
	ha := datagram.HostAdapters{
		RecordHeader: datagram.RecordHeader{
			Format: datagram.HostAdaptersRecordDataFormatValue,
		},
	}

	if err := binary.Read(r, binary.BigEndian, &ha.Length); err != nil {
		return nil, err
	}

	var adaptersCount uint32
	if err := binary.Read(r, binary.BigEndian, &adaptersCount); err != nil {
		return nil, err
	}

	ha.Adapters = make([]datagram.HostAdapter, 0, adaptersCount)
	for range adaptersCount {
		var err error
		adapter := datagram.HostAdapter{}
		if err = binary.Read(r, binary.BigEndian, &adapter.IFIndex); err != nil {
			return nil, err
		}

		var addressCount uint32
		if err = binary.Read(r, binary.BigEndian, &addressCount); err != nil {
			return nil, err
		}

		adapter.MACAddresses = make([]datagram.XDRMACAddress, 0, addressCount)
		for range addressCount {
			var addr datagram.XDRMACAddress
			if err := binary.Read(r, binary.BigEndian, &addr); err != nil {
				return nil, err
			}

			adapter.MACAddresses = append(adapter.MACAddresses, addr)
		}

		ha.Adapters = append(ha.Adapters, adapter)
	}

	return &ha, nil
}

func decodeHostParentRecord(r io.Reader) (*datagram.HostParent, error) {
	hp := datagram.HostParent{
		RecordHeader: datagram.RecordHeader{
			Format: datagram.HostParentRecordDataFormatValue,
		},
	}

	if err := binary.Read(r, binary.BigEndian, &hp.Length); err != nil {
		return nil, err
	}

	if hp.Length != uint32(datagram.HostParentRecordValidLength) {
		return nil, ErrInvalidHostParentRecordSize
	}

	if err := binary.Read(r, binary.BigEndian, &hp.ContainerType); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &hp.ContainerIndex); err != nil {
		return nil, err
	}

	return &hp, nil
}

func decodeHostCPURecord(r io.Reader) (*datagram.HostCPU, error) {
	hcp := datagram.HostCPU{
		RecordHeader: datagram.RecordHeader{
			Format: datagram.HostCPURecordDataFormatValue,
		},
	}

	if err := binary.Read(r, binary.BigEndian, &hcp.Length); err != nil {
		return nil, err
	}

	if hcp.Length != uint32(datagram.HostCPURecordValidLength) {
		return nil, ErrInvalidHostCPURecordSize
	}

	if err := binary.Read(r, binary.BigEndian, &hcp.LoadOne); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &hcp.LoadFive); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &hcp.LoadFifteen); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &hcp.ProcessesRunning); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &hcp.ProcessesTotal); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &hcp.CPUNume); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &hcp.CPUSpeed); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &hcp.Uptime); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &hcp.CPUUser); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &hcp.CPUNice); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &hcp.CPUSys); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &hcp.CPUIdle); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &hcp.CPUWio); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &hcp.CPUIntr); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &hcp.CPUSoftIntr); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &hcp.Interrupts); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &hcp.Contexts); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &hcp.CPUSteal); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &hcp.CPUGuest); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &hcp.CPUGuestNice); err != nil {
		return nil, err
	}

	return &hcp, nil
}

func decodeHostMemoryRecord(r io.Reader) (*datagram.HostMemory, error) {
	hm := datagram.HostMemory{
		RecordHeader: datagram.RecordHeader{
			Format: datagram.HostMemoryRecordDataFormatValue,
		},
	}

	if err := binary.Read(r, binary.BigEndian, &hm.Length); err != nil {
		return nil, err
	}

	if hm.Length != uint32(datagram.HostMemoryRecordValidLength) {
		return nil, ErrInvalidHostMemoryRecordSize
	}

	if err := binary.Read(r, binary.BigEndian, &hm.MemTotal); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &hm.MemFree); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &hm.MemShared); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &hm.MemBuffers); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &hm.MemCached); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &hm.SwapTotal); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &hm.SwapFree); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &hm.PageIn); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &hm.PageOut); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &hm.SwapIn); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &hm.SwapOut); err != nil {
		return nil, err
	}

	return &hm, nil
}

func decodeHostDiskIORecord(r io.Reader) (*datagram.HostDiskIO, error) {
	hdio := datagram.HostDiskIO{
		RecordHeader: datagram.RecordHeader{
			Format: datagram.HostDiskIORecordDataFormatValue,
		},
	}

	if err := binary.Read(r, binary.BigEndian, &hdio.Length); err != nil {
		return nil, err
	}

	if hdio.Length != uint32(datagram.HostDiskIORecordValidLength) {
		return nil, ErrInvalidHostDiskIORecordSize
	}

	if err := binary.Read(r, binary.BigEndian, &hdio.DiskTotal); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &hdio.DiskFree); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &hdio.MaxUsedPartitionPercent); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &hdio.Reads); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &hdio.BytesRead); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &hdio.ReadTime); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &hdio.Writes); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &hdio.BytesWritten); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &hdio.WriteTime); err != nil {
		return nil, err
	}

	return &hdio, nil
}

func decodeHostNetIORecord(r io.Reader) (*datagram.HostNetIO, error) {
	hnio := datagram.HostNetIO{
		RecordHeader: datagram.RecordHeader{
			Format: datagram.HostNetIORecordDataFormatValue,
		},
	}

	if err := binary.Read(r, binary.BigEndian, &hnio.Length); err != nil {
		return nil, err
	}

	if hnio.Length != uint32(datagram.HostNetIORecordValidLength) {
		return nil, ErrInvalidHostNetIORecordSize
	}

	if err := binary.Read(r, binary.BigEndian, &hnio.BytesIn); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &hnio.PacketsIn); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &hnio.ErrorsIn); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &hnio.DropsIn); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &hnio.BytesOut); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &hnio.PacketsOut); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &hnio.ErrorsOut); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &hnio.DropsOut); err != nil {
		return nil, err
	}

	return &hnio, nil
}

func decodeVirtNodeRecord(r io.Reader) (*datagram.VirtNode, error) {
	vn := datagram.VirtNode{
		RecordHeader: datagram.RecordHeader{
			Format: datagram.VirtNodeRecordDataFormatValue,
		},
	}

	if err := binary.Read(r, binary.BigEndian, &vn.Length); err != nil {
		return nil, err
	}

	if vn.Length != uint32(datagram.VirtNodeRecordValidLength) {
		return nil, ErrInvalidVirtNodeRecordSize
	}

	if err := binary.Read(r, binary.BigEndian, &vn.Mhz); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &vn.CPUs); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &vn.Memory); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &vn.MemoryFree); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &vn.NumDomains); err != nil {
		return nil, err
	}

	return &vn, nil
}

func decodeVirtCPURecord(r io.Reader) (*datagram.VirtCPU, error) {
	vc := datagram.VirtCPU{
		RecordHeader: datagram.RecordHeader{
			Format: datagram.VirtCPURecordDataFormatValue,
		},
	}

	if err := binary.Read(r, binary.BigEndian, &vc.Length); err != nil {
		return nil, err
	}

	if vc.Length != uint32(datagram.VirtCPURecordValidLength) {
		return nil, ErrInvalidVirtCPURecordSize
	}

	if err := binary.Read(r, binary.BigEndian, &vc.State); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &vc.CPUTime); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &vc.VirtualCPUCount); err != nil {
		return nil, err
	}

	return &vc, nil
}

func decodeVirtMemoryRecord(r io.Reader) (*datagram.VirtMemory, error) {
	hp := datagram.VirtMemory{
		RecordHeader: datagram.RecordHeader{
			Format: datagram.VirtMemoryRecordDataFormatValue,
		},
	}

	if err := binary.Read(r, binary.BigEndian, &hp.Length); err != nil {
		return nil, err
	}

	if hp.Length != uint32(datagram.VirtMemoryRecordValidLength) {
		return nil, ErrInvalidVirtMemoryRecordSize
	}

	if err := binary.Read(r, binary.BigEndian, &hp.Memory); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &hp.MaxMemory); err != nil {
		return nil, err
	}

	return &hp, nil
}

func decodeVirtDiskIORecord(r io.Reader) (*datagram.VirtDiskIO, error) {
	vdio := datagram.VirtDiskIO{
		RecordHeader: datagram.RecordHeader{
			Format: datagram.VirtDiskIORecordDataFormatValue,
		},
	}

	if err := binary.Read(r, binary.BigEndian, &vdio.Length); err != nil {
		return nil, err
	}

	if vdio.Length != uint32(datagram.VirtDiskIORecordValidLength) {
		return nil, ErrInvalidVirtDiskIORecordSize
	}

	if err := binary.Read(r, binary.BigEndian, &vdio.Capacity); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &vdio.Allocation); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &vdio.Available); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &vdio.RDReq); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &vdio.RDBytes); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &vdio.WRReq); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &vdio.WRBytes); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &vdio.Errors); err != nil {
		return nil, err
	}

	return &vdio, nil
}

func decodeVirtNetIORecord(r io.Reader) (*datagram.VirtNetIO, error) {
	vnio := datagram.VirtNetIO{
		RecordHeader: datagram.RecordHeader{
			Format: datagram.VirtNetIORecordDataFormatValue,
		},
	}

	if err := binary.Read(r, binary.BigEndian, &vnio.Length); err != nil {
		return nil, err
	}

	if vnio.Length != uint32(datagram.VirtNetIORecordValidLength) {
		return nil, ErrInvalidVirtNetIORecordSize
	}

	if err := binary.Read(r, binary.BigEndian, &vnio.RXBytes); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &vnio.RXPackets); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &vnio.RXErrs); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &vnio.RXDrop); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &vnio.TXBytes); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &vnio.TXPackets); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &vnio.TXErrs); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &vnio.TXDrop); err != nil {
		return nil, err
	}

	return &vnio, nil
}

func decodeJVMMachineNameRecord(r io.Reader) (*datagram.JVMMachineName, error) {
	jmn := datagram.JVMMachineName{
		RecordHeader: datagram.RecordHeader{
			Format: datagram.JVMMachineNameRecordDataFormatValue,
		},
	}

	if err := binary.Read(r, binary.BigEndian, &jmn.Length); err != nil {
		return nil, err
	}

	if jmn.Length > uint32(datagram.JVMMachineNameRecordMaxLength) {
		return nil, ErrInvalidJVMMachineNameRecordSize
	}

	var err error
	jmn.VMName, err = decodeXDRString(r)
	if err != nil {
		return nil, err
	}

	if jmn.VMName.Len() > datagram.VMNameMaxSize {
		return nil, ErrJVMVMNameTooLong
	}

	jmn.VMVendor, err = decodeXDRString(r)
	if err != nil {
		return nil, err
	}

	if jmn.VMVendor.Len() > datagram.VMVendorMaxSize {
		return nil, ErrJVMVMVendorTooLong
	}

	jmn.VMVersion, err = decodeXDRString(r)
	if err != nil {
		return nil, err
	}

	if jmn.VMVersion.Len() > datagram.VMVersionMaxSize {
		return nil, ErrJVMVMVersionTooLong
	}

	return &jmn, nil
}

func decodeJVMStatisticsRecord(r io.Reader) (*datagram.JVMStatistics, error) {
	js := datagram.JVMStatistics{
		RecordHeader: datagram.RecordHeader{
			Format: datagram.JVMStatisticsRecordDataFormatValue,
		},
	}

	if err := binary.Read(r, binary.BigEndian, &js.Length); err != nil {
		return nil, err
	}

	if js.Length != uint32(datagram.JVMStatisticsRecordValidLength) {
		return nil, ErrInvalidJVMStatisticsRecordSize
	}

	if err := binary.Read(r, binary.BigEndian, &js.HeapInitial); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &js.HeapUsed); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &js.HeapCommitted); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &js.HeapMax); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &js.NonHeapInitial); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &js.NonHeapUsed); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &js.NonHeapCommitted); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &js.NonHeapMax); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &js.GCCount); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &js.GCTime); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &js.ClassesLoaded); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &js.ClassesTotal); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &js.ClassesUnloaded); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &js.CompilationTime); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &js.ThreadNumLive); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &js.ThreadNumDaemon); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &js.ThreadNumStarted); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &js.OpenFileDescCount); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &js.MaxFileDescCount); err != nil {
		return nil, err
	}

	return &js, nil
}

func decodeHTTPCountersRecord(r io.Reader) (*datagram.HTTPCounters, error) {
	hc := datagram.HTTPCounters{
		RecordHeader: datagram.RecordHeader{
			Format: datagram.HTTPCountersRecordDataFormatValue,
		},
	}

	if err := binary.Read(r, binary.BigEndian, &hc.Length); err != nil {
		return nil, err
	}

	if hc.Length != uint32(datagram.HTTPCountersRecordValidLength) {
		return nil, ErrInvalidHTTPCountersRecordSize
	}

	if err := binary.Read(r, binary.BigEndian, &hc.MethodOptionCount); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &hc.MethodGetCount); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &hc.MethodHeadCount); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &hc.MethodPostCount); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &hc.MethodPutCount); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &hc.MethodDeleteCount); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &hc.MethodTraceCount); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &hc.MethodConnectCount); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &hc.MethodOtherCount); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &hc.Status1XXCount); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &hc.Status2XXCount); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &hc.Status3XXCount); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &hc.Status4XXCount); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &hc.Status5XXCount); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &hc.StatusOtherCount); err != nil {
		return nil, err
	}

	return &hc, nil
}

func decodeAppOperationsRecord(r io.Reader) (*datagram.AppOperations, error) {
	ao := datagram.AppOperations{
		RecordHeader: datagram.RecordHeader{
			Format: datagram.AppOperationsRecordDataFormatValue,
		},
	}

	if err := binary.Read(r, binary.BigEndian, &ao.Length); err != nil {
		return nil, err
	}

	if ao.Length > uint32(datagram.AppOperationsRecordMaxLength) {
		return nil, ErrInvalidAppOperationsRecordSize
	}

	var err error
	ao.Application, err = decodeXDRString(r)
	if err != nil {
		return nil, err
	}

	if ao.Application.Len() > datagram.ApplicationMaxSize {
		return nil, ErrApplicationTooLong
	}

	if err := binary.Read(r, binary.BigEndian, &ao.Success); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &ao.Other); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &ao.Timeout); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &ao.InternalError); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &ao.BadRequest); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &ao.Forbidden); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &ao.TooLarge); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &ao.NotImplemented); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &ao.NotFound); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &ao.Unavailable); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &ao.Unauthorized); err != nil {
		return nil, err
	}

	return &ao, nil
}

func decodeAppResourcesRecord(r io.Reader) (*datagram.AppResources, error) {
	ar := datagram.AppResources{
		RecordHeader: datagram.RecordHeader{
			Format: datagram.AppResourcesRecordDataFormatValue,
		},
	}

	if err := binary.Read(r, binary.BigEndian, &ar.Length); err != nil {
		return nil, err
	}

	if ar.Length != uint32(datagram.AppResourcesRecordValidLength) {
		return nil, ErrInvalidAppResourcesRecordSize
	}

	if err := binary.Read(r, binary.BigEndian, &ar.UserTime); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &ar.SystemTime); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &ar.MemUsed); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &ar.MemMax); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &ar.FDOpen); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &ar.FDMax); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &ar.ConnOpen); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &ar.ConnMax); err != nil {
		return nil, err
	}

	return &ar, nil
}

func decodeMemcacheCountersRecord(r io.Reader) (*datagram.MemcacheCounters, error) {
	mc := datagram.MemcacheCounters{
		RecordHeader: datagram.RecordHeader{
			Format: datagram.MemcacheCountersRecordDataFormatValue,
		},
	}

	if err := binary.Read(r, binary.BigEndian, &mc.Length); err != nil {
		return nil, err
	}

	if mc.Length != uint32(datagram.MemcacheCountersRecordValidLength) {
		return nil, ErrInvalidMemcacheCountersRecordSize
	}

	if err := binary.Read(r, binary.BigEndian, &mc.CmdSet); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mc.CmdTouch); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mc.CmdFlush); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mc.GetHits); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mc.GetMisses); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mc.DeleteHits); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mc.DeleteMisses); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mc.IncrHits); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mc.IncrMisses); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mc.DecrHits); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mc.DecrMisses); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mc.CasHits); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mc.CasMisses); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mc.CasBadval); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mc.AuthCmds); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mc.AuthErrors); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mc.Threads); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mc.ConnYields); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mc.ListenDisabledNum); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mc.CurrConnections); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mc.RejectedConnections); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mc.TotalConnections); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mc.ConnectionStructures); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mc.Evictions); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mc.Reclaimed); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mc.CurrItems); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mc.TotalItems); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mc.BytesRead); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mc.BytesWritten); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mc.Bytes); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mc.LimitMaxbytes); err != nil {
		return nil, err
	}

	return &mc, nil
}

func decodeAppWorkersRecord(r io.Reader) (*datagram.AppWorkers, error) {
	aw := datagram.AppWorkers{
		RecordHeader: datagram.RecordHeader{
			Format: datagram.AppWorkersRecordDataFormatValue,
		},
	}

	if err := binary.Read(r, binary.BigEndian, &aw.Length); err != nil {
		return nil, err
	}

	if aw.Length != uint32(datagram.AppWorkersRecordValidLength) {
		return nil, ErrInvalidAppWorkersRecordSize
	}

	if err := binary.Read(r, binary.BigEndian, &aw.WorkersActive); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &aw.WorkersIdle); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &aw.WorkersMax); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &aw.ReqDelayed); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &aw.ReqDropped); err != nil {
		return nil, err
	}

	return &aw, nil
}

func decodeOVSDPStatsRecord(r io.Reader) (*datagram.OVSDPStats, error) {
	ovs := datagram.OVSDPStats{
		RecordHeader: datagram.RecordHeader{
			Format: datagram.OVSDPStatsRecordDataFormatValue,
		},
	}

	if err := binary.Read(r, binary.BigEndian, &ovs.Length); err != nil {
		return nil, err
	}

	if ovs.Length != uint32(datagram.OVSDPStatsRecordValidLength) {
		return nil, ErrInvalidOVSDPStatsRecordSize
	}

	if err := binary.Read(r, binary.BigEndian, &ovs.Hits); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &ovs.Misses); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &ovs.Lost); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &ovs.MaskHits); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &ovs.Flows); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &ovs.Masks); err != nil {
		return nil, err
	}

	return &ovs, nil
}

func decodeEnergyRecord(r io.Reader) (*datagram.Energy, error) {
	e := datagram.Energy{
		RecordHeader: datagram.RecordHeader{
			Format: datagram.EnergyRecordDataFormatValue,
		},
	}

	if err := binary.Read(r, binary.BigEndian, &e.Length); err != nil {
		return nil, err
	}

	if e.Length != uint32(datagram.EnergyRecordValidLength) {
		return nil, ErrInvalidEnergyRecordSize
	}

	if err := binary.Read(r, binary.BigEndian, &e.Voltage); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &e.Current); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &e.RealPower); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &e.PowerFactor); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &e.Energy); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &e.Errors); err != nil {
		return nil, err
	}

	return &e, nil
}

func decodeTemperatureRecord(r io.Reader) (*datagram.Temperature, error) {
	t := datagram.Temperature{
		RecordHeader: datagram.RecordHeader{
			Format: datagram.TemperatureRecordDataFormatValue,
		},
	}

	if err := binary.Read(r, binary.BigEndian, &t.Length); err != nil {
		return nil, err
	}

	if t.Length != uint32(datagram.TemperatureRecordValidLength) {
		return nil, ErrInvalidTemperatureRecordSize
	}

	if err := binary.Read(r, binary.BigEndian, &t.Minimum); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &t.Maximum); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &t.Errors); err != nil {
		return nil, err
	}

	return &t, nil
}

func decodeHumidityRecord(r io.Reader) (*datagram.Humidity, error) {
	h := datagram.Humidity{
		RecordHeader: datagram.RecordHeader{
			Format: datagram.HumidityRecordDataFormatValue,
		},
	}

	if err := binary.Read(r, binary.BigEndian, &h.Length); err != nil {
		return nil, err
	}

	if h.Length != uint32(datagram.HumidityRecordValidLength) {
		return nil, ErrInvalidHumidityRecordSize
	}

	if err := binary.Read(r, binary.BigEndian, &h.Relative); err != nil {
		return nil, err
	}

	return &h, nil
}

func decodeFansRecord(r io.Reader) (*datagram.Fans, error) {
	f := datagram.Fans{
		RecordHeader: datagram.RecordHeader{
			Format: datagram.FansRecordDataFormatValue,
		},
	}

	if err := binary.Read(r, binary.BigEndian, &f.Length); err != nil {
		return nil, err
	}

	if f.Length != uint32(datagram.FansRecordValidLength) {
		return nil, ErrInvalidFansRecordSize
	}

	if err := binary.Read(r, binary.BigEndian, &f.Total); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &f.Failed); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &f.Speed); err != nil {
		return nil, err
	}

	return &f, nil
}

func decodeMIB2IPGroupRecord(r io.Reader) (*datagram.MIB2IPGroup, error) {
	mib := datagram.MIB2IPGroup{
		RecordHeader: datagram.RecordHeader{
			Format: datagram.MIB2IPGroupRecordDataFormatValue,
		},
	}

	if err := binary.Read(r, binary.BigEndian, &mib.Length); err != nil {
		return nil, err
	}

	if mib.Length != uint32(datagram.MIB2IPGroupRecordValidLength) {
		return nil, ErrInvalidMIB2IPGroupRecordSize
	}

	if err := binary.Read(r, binary.BigEndian, &mib.IPForwarding); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mib.IPDefaultTTL); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mib.IPInReceives); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mib.IPInHdrErrors); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mib.IPInAddrErrors); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mib.IPForwDatagrams); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mib.IPInUnknownProtos); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mib.IPInDiscards); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mib.IPInDelivers); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mib.IPOutRequests); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mib.IPOutDiscards); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mib.IPOutNoRoutes); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mib.IPReasmTimeout); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mib.IPReasmReqds); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mib.IPReasmOKs); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mib.IPReasmFails); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mib.IPFragOKs); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mib.IPFragFails); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mib.IPFragCreates); err != nil {
		return nil, err
	}

	return &mib, nil
}

func decodeMIB2ICMPGroupRecord(r io.Reader) (*datagram.MIB2ICMPGroup, error) {
	mib := datagram.MIB2ICMPGroup{
		RecordHeader: datagram.RecordHeader{
			Format: datagram.MIB2ICMPGroupRecordDataFormatValue,
		},
	}

	if err := binary.Read(r, binary.BigEndian, &mib.Length); err != nil {
		return nil, err
	}

	if mib.Length != uint32(datagram.MIB2ICMPGroupRecordValidLength) {
		return nil, ErrInvalidMIB2ICMPGroupRecordSize
	}

	if err := binary.Read(r, binary.BigEndian, &mib.ICMPInMsgs); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mib.ICMPInErrors); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mib.ICMPInDestUnreachs); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mib.ICMPInTimeExcds); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mib.ICMPInParamProbs); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mib.ICMPInSrcQuenchs); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mib.ICMPInRedirects); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mib.ICMPInEchos); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mib.ICMPInEchoReps); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mib.ICMPInTimestamps); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mib.ICMPInAddrMasks); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mib.ICMPInAddrMaskReps); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mib.ICMPOutMsgs); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mib.ICMPOutErrors); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mib.ICMPOutDestUnreachs); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mib.ICMPOutTimeExcds); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mib.ICMPOutParamProbs); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mib.ICMPOutSrcQuenchs); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mib.ICMPOutRedirects); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mib.ICMPOutEchos); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mib.ICMPOutEchoReps); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mib.ICMPOutTimestamps); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mib.ICMPOutTimestampReps); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mib.ICMPOutAddrMasks); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mib.ICMPOutAddrMaskReps); err != nil {
		return nil, err
	}

	return &mib, nil
}

func decodeMIB2TCPGroupRecord(r io.Reader) (*datagram.MIB2TCPGroup, error) {
	mib := datagram.MIB2TCPGroup{
		RecordHeader: datagram.RecordHeader{
			Format: datagram.MIB2TCPGroupRecordDataFormatValue,
		},
	}

	if err := binary.Read(r, binary.BigEndian, &mib.Length); err != nil {
		return nil, err
	}

	if mib.Length != uint32(datagram.MIB2TCPGroupRecordValidLength) {
		return nil, ErrInvalidMIB2TCPGroupRecordSize
	}

	if err := binary.Read(r, binary.BigEndian, &mib.TCPRtoAlgorithm); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mib.TCPRtoMin); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mib.TCPRtoMax); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mib.TCPMaxConn); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mib.TCPActiveOpens); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mib.TCPPassiveOpens); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mib.TCPAttemptFails); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mib.TCPEstabResets); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mib.TCPCurrEstab); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mib.TCPInSegs); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mib.TCPOutSegs); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mib.TCPRetransSegs); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mib.TCPInErrs); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mib.TCPOutRsts); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mib.TCPInCsumErrors); err != nil {
		return nil, err
	}

	return &mib, nil
}

func decodeMIB2UDPGroupRecord(r io.Reader) (*datagram.MIB2UDPGroup, error) {
	mib := datagram.MIB2UDPGroup{
		RecordHeader: datagram.RecordHeader{
			Format: datagram.MIB2UDPGroupRecordDataFormatValue,
		},
	}

	if err := binary.Read(r, binary.BigEndian, &mib.Length); err != nil {
		return nil, err
	}

	if mib.Length != uint32(datagram.MIB2UDPGroupRecordValidLength) {
		return nil, ErrInvalidMIB2UDPGroupRecordSize
	}

	if err := binary.Read(r, binary.BigEndian, &mib.UDPInDatagrams); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mib.UDPNoPorts); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mib.UDPInErrors); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mib.UDPOutDatagrams); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mib.UDPRcvbufErrors); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mib.UDPSndbufErrors); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &mib.UDPInCsumErrors); err != nil {
		return nil, err
	}

	return &mib, nil
}
