package decoder

import (
	"encoding/binary"
	"errors"
	"io"

	"github.com/gravwell/gravwell/v3/sflow/datagram"
)

var (
	ErrInvalidCounterIfRecordSize        = errors.New("counter if record size is invalid")
	ErrInvalidEthernetCountersRecordSize = errors.New("counter ethernet record size is invalid")
	// TODO  One of these for each fucking record T__T
)

func decodeCounterIfRecord(r io.Reader) (datagram.CounterIfRecord, error) {
	cir := datagram.CounterIfRecord{
		DataFormat: datagram.CounterIfRecordDataFormatValue,
	}

	if err := binary.Read(r, binary.BigEndian, &cir.Length); err != nil {
		return cir, err
	}

	if cir.Length != uint32(datagram.CounterIfRecordValidLength) {
		return cir, ErrInvalidCounterIfRecordSize
	}

	if err := binary.Read(r, binary.BigEndian, &cir.IfIndex); err != nil {
		return cir, err
	}

	if err := binary.Read(r, binary.BigEndian, &cir.IfType); err != nil {
		return cir, err
	}

	if err := binary.Read(r, binary.BigEndian, &cir.IfSpeed); err != nil {
		return cir, err
	}

	if err := binary.Read(r, binary.BigEndian, &cir.IfDirection); err != nil {
		return cir, err
	}

	if err := binary.Read(r, binary.BigEndian, &cir.IfStatus); err != nil {
		return cir, err
	}

	if err := binary.Read(r, binary.BigEndian, &cir.IfInOctets); err != nil {
		return cir, err
	}

	if err := binary.Read(r, binary.BigEndian, &cir.IfInUcastPkts); err != nil {
		return cir, err
	}

	if err := binary.Read(r, binary.BigEndian, &cir.IfInMulticastPkts); err != nil {
		return cir, err
	}

	if err := binary.Read(r, binary.BigEndian, &cir.IfInBroadcastPkts); err != nil {
		return cir, err
	}

	if err := binary.Read(r, binary.BigEndian, &cir.IfInDiscards); err != nil {
		return cir, err
	}

	if err := binary.Read(r, binary.BigEndian, &cir.IfInErrors); err != nil {
		return cir, err
	}

	if err := binary.Read(r, binary.BigEndian, &cir.IfInUnknownProtos); err != nil {
		return cir, err
	}

	if err := binary.Read(r, binary.BigEndian, &cir.IfOutOctets); err != nil {
		return cir, err
	}

	if err := binary.Read(r, binary.BigEndian, &cir.IfOutUcastPkts); err != nil {
		return cir, err
	}

	if err := binary.Read(r, binary.BigEndian, &cir.IfOutMulticastPkts); err != nil {
		return cir, err
	}

	if err := binary.Read(r, binary.BigEndian, &cir.IfOutBroadcastPkts); err != nil {
		return cir, err
	}

	if err := binary.Read(r, binary.BigEndian, &cir.IfOutDiscards); err != nil {
		return cir, err
	}

	if err := binary.Read(r, binary.BigEndian, &cir.IfOutErrors); err != nil {
		return cir, err
	}

	if err := binary.Read(r, binary.BigEndian, &cir.IfPromiscuousMode); err != nil {
		return cir, err
	}

	return cir, nil
}

func decodeEthernetCountersRecord(r io.Reader) (datagram.EthernetCounters, error) {
	ecr := datagram.EthernetCounters{
		DataFormat: datagram.EthernetCountersRecordDataFormatValue,
	}

	if err := binary.Read(r, binary.BigEndian, &ecr.Length); err != nil {
		return ecr, err
	}

	if ecr.Length != uint32(datagram.EthernetCountersRecordValidLength) {
		return ecr, ErrInvalidCounterIfRecordSize
	}

	if err := binary.Read(r, binary.BigEndian, &ecr.Dot3StatsAlignmentErrors); err != nil {
		return ecr, err
	}

	if err := binary.Read(r, binary.BigEndian, &ecr.Dot3StatsFCSErrors); err != nil {
		return ecr, err
	}

	if err := binary.Read(r, binary.BigEndian, &ecr.Dot3StatsSingleCollisionFrames); err != nil {
		return ecr, err
	}

	if err := binary.Read(r, binary.BigEndian, &ecr.Dot3StatsMultipleCollisionFrames); err != nil {
		return ecr, err
	}

	if err := binary.Read(r, binary.BigEndian, &ecr.Dot3StatsSQETestErrors); err != nil {
		return ecr, err
	}

	if err := binary.Read(r, binary.BigEndian, &ecr.Dot3StatsDeferredTransmissions); err != nil {
		return ecr, err
	}

	if err := binary.Read(r, binary.BigEndian, &ecr.Dot3StatsLateCollisions); err != nil {
		return ecr, err
	}

	if err := binary.Read(r, binary.BigEndian, &ecr.Dot3StatsLateCollisions); err != nil {
		return ecr, err
	}

	if err := binary.Read(r, binary.BigEndian, &ecr.Dot3StatsExcessiveCollisions); err != nil {
		return ecr, err
	}

	if err := binary.Read(r, binary.BigEndian, &ecr.Dot3StatsInternalMacTransmitErrors); err != nil {
		return ecr, err
	}

	if err := binary.Read(r, binary.BigEndian, &ecr.Dot3StatsCarrierSenseErrors); err != nil {
		return ecr, err
	}

	if err := binary.Read(r, binary.BigEndian, &ecr.Dot3StatsFrameTooLongs); err != nil {
		return ecr, err
	}

	if err := binary.Read(r, binary.BigEndian, &ecr.Dot3StatsInternalMacReceiveErrors); err != nil {
		return ecr, err
	}

	if err := binary.Read(r, binary.BigEndian, &ecr.Dot3StatsSymbolErrors); err != nil {
		return ecr, err
	}

	return ecr, nil
}

func decordTokenringCountersRecord(r io.Reader) (datagram.TokenringCounters, error) {
	trc := datagram.TokenringCounters{
		DataFormat: datagram.TokenringCountersRecordDataFormatValue,
	}

	if err := binary.Read(r, binary.BigEndian, &trc.Length); err != nil {
		return trc, err
	}

	if trc.Length != uint32(datagram.TokenringCountersRecordValidLength) {
		return trc, ErrInvalidCounterIfRecordSize
	}

	if err := binary.Read(r, binary.BigEndian, &trc.Dot3StatsLineErrors); err != nil {
		return trc, err
	}

	if err := binary.Read(r, binary.BigEndian, &trc.Dot3StatsBurstErrors); err != nil {
		return trc, err
	}

	if err := binary.Read(r, binary.BigEndian, &trc.Dot3StatsACErrors); err != nil {
		return trc, err
	}

	if err := binary.Read(r, binary.BigEndian, &trc.Dot3StatsAbortTransErrors); err != nil {
		return trc, err
	}

	if err := binary.Read(r, binary.BigEndian, &trc.Dot3StatsInternalErrors); err != nil {
		return trc, err
	}

	if err := binary.Read(r, binary.BigEndian, &trc.Dot3StatsLostFrameErrors); err != nil {
		return trc, err
	}

	if err := binary.Read(r, binary.BigEndian, &trc.Dot3StatsReceiveCongestions); err != nil {
		return trc, err
	}

	if err := binary.Read(r, binary.BigEndian, &trc.Dot3StatsFrameCopiedErrors); err != nil {
		return trc, err
	}

	if err := binary.Read(r, binary.BigEndian, &trc.Dot3StatsTokenErrors); err != nil {
		return trc, err
	}

	if err := binary.Read(r, binary.BigEndian, &trc.Dot3StatsSoftErrors); err != nil {
		return trc, err
	}

	if err := binary.Read(r, binary.BigEndian, &trc.Dot3StatsHardErrors); err != nil {
		return trc, err
	}

	if err := binary.Read(r, binary.BigEndian, &trc.Dot3StatsSignalLoss); err != nil {
		return trc, err
	}

	if err := binary.Read(r, binary.BigEndian, &trc.Dot3StatsTransmitBeacons); err != nil {
		return trc, err
	}

	if err := binary.Read(r, binary.BigEndian, &trc.Dot3StatsRecoverys); err != nil {
		return trc, err
	}

	if err := binary.Read(r, binary.BigEndian, &trc.Dot3StatsLobeWires); err != nil {
		return trc, err
	}

	if err := binary.Read(r, binary.BigEndian, &trc.Dot3StatsRemoves); err != nil {
		return trc, err
	}

	if err := binary.Read(r, binary.BigEndian, &trc.Dot3StatsSingles); err != nil {
		return trc, err
	}

	if err := binary.Read(r, binary.BigEndian, &trc.Dot3StatsFreqErrors); err != nil {
		return trc, err
	}

	return trc, nil
}

func decodeVgCountersRecord(r io.Reader) (datagram.VgCounters, error) {
	vgc := datagram.VgCounters{
		DataFormat: datagram.VgCountersRecordDataFormatValue,
	}

	if err := binary.Read(r, binary.BigEndian, &vgc.Length); err != nil {
		return vgc, err
	}

	if vgc.Length != uint32(datagram.VgCountersRecordValidLength) {
		return vgc, ErrInvalidCounterIfRecordSize
	}

	if err := binary.Read(r, binary.BigEndian, &vgc.Dot12InHighPriorityFrames); err != nil {
		return vgc, err
	}

	if err := binary.Read(r, binary.BigEndian, &vgc.Dot12InHighPriorityOctets); err != nil {
		return vgc, err
	}

	if err := binary.Read(r, binary.BigEndian, &vgc.Dot12InNormPriorityFrames); err != nil {
		return vgc, err
	}

	if err := binary.Read(r, binary.BigEndian, &vgc.Dot12InNormPriorityOctets); err != nil {
		return vgc, err
	}

	if err := binary.Read(r, binary.BigEndian, &vgc.Dot12InIPMErrors); err != nil {
		return vgc, err
	}

	if err := binary.Read(r, binary.BigEndian, &vgc.Dot12InOversizeFrameErrors); err != nil {
		return vgc, err
	}

	if err := binary.Read(r, binary.BigEndian, &vgc.Dot12InDataErrors); err != nil {
		return vgc, err
	}

	if err := binary.Read(r, binary.BigEndian, &vgc.Dot12InNullAddressedFrames); err != nil {
		return vgc, err
	}

	if err := binary.Read(r, binary.BigEndian, &vgc.Dot12OutHighPriorityFrames); err != nil {
		return vgc, err
	}

	if err := binary.Read(r, binary.BigEndian, &vgc.Dot12OutHighPriorityOctets); err != nil {
		return vgc, err
	}

	if err := binary.Read(r, binary.BigEndian, &vgc.Dot12TransitionIntoTrainings); err != nil {
		return vgc, err
	}

	if err := binary.Read(r, binary.BigEndian, &vgc.Dot12HCInHighPriorityOctets); err != nil {
		return vgc, err
	}

	if err := binary.Read(r, binary.BigEndian, &vgc.Dot12HCInNormPriorityOctets); err != nil {
		return vgc, err
	}

	if err := binary.Read(r, binary.BigEndian, &vgc.Dot12HCOutHighPriorityOctets); err != nil {
		return vgc, err
	}

	return vgc, nil
}

func decodeVlanCountersRecord(r io.Reader) (datagram.VlanCounters, error) {
	vlc := datagram.VlanCounters{
		DataFormat: datagram.VgCountersRecordDataFormatValue,
	}

	if err := binary.Read(r, binary.BigEndian, &vlc.Length); err != nil {
		return vlc, err
	}

	if vlc.Length != uint32(datagram.VlanCountersRecordValidLength) {
		return vlc, ErrInvalidCounterIfRecordSize
	}

	if err := binary.Read(r, binary.BigEndian, &vlc.ID); err != nil {
		return vlc, err
	}

	if err := binary.Read(r, binary.BigEndian, &vlc.Octets); err != nil {
		return vlc, err
	}

	if err := binary.Read(r, binary.BigEndian, &vlc.UnicastPackets); err != nil {
		return vlc, err
	}

	if err := binary.Read(r, binary.BigEndian, &vlc.MulticastPackets); err != nil {
		return vlc, err
	}

	if err := binary.Read(r, binary.BigEndian, &vlc.BroadcastPackets); err != nil {
		return vlc, err
	}

	if err := binary.Read(r, binary.BigEndian, &vlc.Discards); err != nil {
		return vlc, err
	}

	return vlc, nil
}

func decodeProcessorCountersRecord(r io.Reader) (datagram.ProcessorCounters, error) {
	pc := datagram.ProcessorCounters{
		DataFormat: datagram.ProcessorCountersRecordDataFormatValue,
	}

	if err := binary.Read(r, binary.BigEndian, &pc.Length); err != nil {
		return pc, err
	}

	if pc.Length != uint32(datagram.ProcessorCountersRecordValidLength) {
		return pc, ErrInvalidCounterIfRecordSize
	}

	if err := binary.Read(r, binary.BigEndian, &pc.CPU5s); err != nil {
		return pc, err
	}

	if err := binary.Read(r, binary.BigEndian, &pc.CPU1m); err != nil {
		return pc, err
	}

	if err := binary.Read(r, binary.BigEndian, &pc.CPU5m); err != nil {
		return pc, err
	}

	if err := binary.Read(r, binary.BigEndian, &pc.TotalMemory); err != nil {
		return pc, err
	}

	if err := binary.Read(r, binary.BigEndian, &pc.FreeMemory); err != nil {
		return pc, err
	}

	return pc, nil
}

func decodeHostDescrRecord(r io.Reader) (datagram.HostDescr, error) {
	hd := datagram.HostDescr{
		DataFormat: datagram.HostDescrRecordDataFormatValue,
	}

	if err := binary.Read(r, binary.BigEndian, &hd.Length); err != nil {
		return hd, err
	}

	if hd.Length != uint32(datagram.HostDescrRecordValidLength) {
		return hd, ErrInvalidCounterIfRecordSize
	}

	if err := binary.Read(r, binary.BigEndian, &hd.HostName); err != nil {
		return hd, err
	}

	if err := binary.Read(r, binary.BigEndian, &hd.UUID); err != nil {
		return hd, err
	}

	if err := binary.Read(r, binary.BigEndian, &hd.MachineType); err != nil {
		return hd, err
	}

	if err := binary.Read(r, binary.BigEndian, &hd.OSName); err != nil {
		return hd, err
	}

	if err := binary.Read(r, binary.BigEndian, &hd.OSRelease); err != nil {
		return hd, err
	}

	return hd, nil
}

func decodeHostAdaptersRecord(r io.Reader) (datagram.HostAdapters, error) {
	ha := datagram.HostAdapters{
		DataFormat: datagram.HostAdaptersRecordDataFormatValue,
	}

	if err := binary.Read(r, binary.BigEndian, &ha.Length); err != nil {
		return ha, err
	}

	if err := binary.Read(r, binary.BigEndian, &ha.AdaptersCount); err != nil {
		return ha, err
	}

	ha.Adapters = make([]datagram.HostAdapter, 0, ha.AdaptersCount)
	for i := uint32(0); i < ha.AdaptersCount; i++ {
		adapter := datagram.HostAdapter{}
		if err := binary.Read(r, binary.BigEndian, &adapter.IFIndex); err != nil {
			return ha, err
		}

		if err := binary.Read(r, binary.BigEndian, &adapter.MACLength); err != nil {
			return ha, err
		}

		addr := make([]byte, adapter.MACLength)
		_, err := r.Read(addr)
		if err != nil {
			return ha, err
		}

		adapter.MACAddress = addr

		ha.Adapters = append(ha.Adapters, adapter)
	}

	return ha, nil
}

func decodeHostParentRecord(r io.Reader) (datagram.HostParent, error) {
	hp := datagram.HostParent{
		DataFormat: datagram.HostParentRecordDataFormatValue,
	}

	if err := binary.Read(r, binary.BigEndian, &hp.Length); err != nil {
		return hp, err
	}

	if hp.Length != uint32(datagram.HostParentRecordValidLength) {
		return hp, ErrInvalidCounterIfRecordSize
	}

	if err := binary.Read(r, binary.BigEndian, &hp.DataFormat); err != nil {
		return hp, err
	}

	if err := binary.Read(r, binary.BigEndian, &hp.Length); err != nil {
		return hp, err
	}

	if err := binary.Read(r, binary.BigEndian, &hp.ContainerType); err != nil {
		return hp, err
	}

	if err := binary.Read(r, binary.BigEndian, &hp.ContainerIndex); err != nil {
		return hp, err
	}

	return hp, nil
}

func decodeHostCPURecord(r io.Reader) (datagram.HostCPU, error) {
	hcp := datagram.HostCPU{
		DataFormat: datagram.HostParentRecordDataFormatValue,
	}

	if err := binary.Read(r, binary.BigEndian, &hcp.Length); err != nil {
		return hcp, err
	}

	if hcp.Length != uint32(datagram.HostCPURecordValidLength) {
		return hcp, ErrInvalidCounterIfRecordSize
	}

	if err := binary.Read(r, binary.BigEndian, &hcp.LoadOne); err != nil {
		return hcp, err
	}

	if err := binary.Read(r, binary.BigEndian, &hcp.LoadFive); err != nil {
		return hcp, err
	}

	if err := binary.Read(r, binary.BigEndian, &hcp.LoadFifteen); err != nil {
		return hcp, err
	}

	if err := binary.Read(r, binary.BigEndian, &hcp.ProcessesRunning); err != nil {
		return hcp, err
	}

	if err := binary.Read(r, binary.BigEndian, &hcp.ProcessesTotal); err != nil {
		return hcp, err
	}

	if err := binary.Read(r, binary.BigEndian, &hcp.CPUNume); err != nil {
		return hcp, err
	}

	if err := binary.Read(r, binary.BigEndian, &hcp.CPUSpeed); err != nil {
		return hcp, err
	}

	if err := binary.Read(r, binary.BigEndian, &hcp.Uptime); err != nil {
		return hcp, err
	}

	if err := binary.Read(r, binary.BigEndian, &hcp.CPUUser); err != nil {
		return hcp, err
	}

	if err := binary.Read(r, binary.BigEndian, &hcp.CPUNice); err != nil {
		return hcp, err
	}

	if err := binary.Read(r, binary.BigEndian, &hcp.CPUSys); err != nil {
		return hcp, err
	}

	if err := binary.Read(r, binary.BigEndian, &hcp.CPUIdle); err != nil {
		return hcp, err
	}

	if err := binary.Read(r, binary.BigEndian, &hcp.CPUWio); err != nil {
		return hcp, err
	}

	if err := binary.Read(r, binary.BigEndian, &hcp.CPUIntr); err != nil {
		return hcp, err
	}

	if err := binary.Read(r, binary.BigEndian, &hcp.CPUSoftIntr); err != nil {
		return hcp, err
	}

	if err := binary.Read(r, binary.BigEndian, &hcp.Interrupts); err != nil {
		return hcp, err
	}

	if err := binary.Read(r, binary.BigEndian, &hcp.Contexts); err != nil {
		return hcp, err
	}

	return hcp, nil
}

func decodeHostMemoryRecord(r io.Reader) (datagram.HostMemory, error) {
	hm := datagram.HostMemory{
		DataFormat: datagram.HostParentRecordDataFormatValue,
	}

	if err := binary.Read(r, binary.BigEndian, &hm.Length); err != nil {
		return hm, err
	}

	if hm.Length != uint32(datagram.HostCPURecordValidLength) {
		return hm, ErrInvalidCounterIfRecordSize
	}

	if err := binary.Read(r, binary.BigEndian, &hm.MemTotal); err != nil {
		return hm, err
	}

	if err := binary.Read(r, binary.BigEndian, &hm.MemFree); err != nil {
		return hm, err
	}

	if err := binary.Read(r, binary.BigEndian, &hm.MemShared); err != nil {
		return hm, err
	}

	if err := binary.Read(r, binary.BigEndian, &hm.MemBuffers); err != nil {
		return hm, err
	}

	if err := binary.Read(r, binary.BigEndian, &hm.MemCached); err != nil {
		return hm, err
	}

	if err := binary.Read(r, binary.BigEndian, &hm.MemSwapTotal); err != nil {
		return hm, err
	}

	if err := binary.Read(r, binary.BigEndian, &hm.SwapFree); err != nil {
		return hm, err
	}

	if err := binary.Read(r, binary.BigEndian, &hm.PageIn); err != nil {
		return hm, err
	}

	if err := binary.Read(r, binary.BigEndian, &hm.PageOut); err != nil {
		return hm, err
	}

	if err := binary.Read(r, binary.BigEndian, &hm.SwapIn); err != nil {
		return hm, err
	}

	if err := binary.Read(r, binary.BigEndian, &hm.SwapOut); err != nil {
		return hm, err
	}

	return hm, nil
}

func decodeHostDiskIORecord(r io.Reader) (datagram.HostDiskIO, error) {
	hdio := datagram.HostDiskIO{
		DataFormat: datagram.HostDiskIORecordDataFormatValue,
	}

	if err := binary.Read(r, binary.BigEndian, &hdio.Length); err != nil {
		return hdio, err
	}

	if hdio.Length != uint32(datagram.HostDiskIORecordValidLength) {
		return hdio, ErrInvalidCounterIfRecordSize
	}

	if err := binary.Read(r, binary.BigEndian, &hdio.DiskTotal); err != nil {
		return hdio, err
	}

	if err := binary.Read(r, binary.BigEndian, &hdio.DiskFree); err != nil {
		return hdio, err
	}

	if err := binary.Read(r, binary.BigEndian, &hdio.MaxUsedPartitionPercent); err != nil {
		return hdio, err
	}

	if err := binary.Read(r, binary.BigEndian, &hdio.Reads); err != nil {
		return hdio, err
	}

	if err := binary.Read(r, binary.BigEndian, &hdio.BytesRead); err != nil {
		return hdio, err
	}

	if err := binary.Read(r, binary.BigEndian, &hdio.ReadTime); err != nil {
		return hdio, err
	}

	if err := binary.Read(r, binary.BigEndian, &hdio.Writes); err != nil {
		return hdio, err
	}

	if err := binary.Read(r, binary.BigEndian, &hdio.BytesWritten); err != nil {
		return hdio, err
	}

	if err := binary.Read(r, binary.BigEndian, &hdio.WriteTime); err != nil {
		return hdio, err
	}

	return hdio, nil
}

func decodeHostNetIORecord(r io.Reader) (datagram.HostNetIO, error) {
	hnio := datagram.HostNetIO{
		DataFormat: datagram.HostDiskIORecordDataFormatValue,
	}

	if err := binary.Read(r, binary.BigEndian, &hnio.Length); err != nil {
		return hnio, err
	}

	if hnio.Length != uint32(datagram.HostNetIORecordValidLength) {
		return hnio, ErrInvalidCounterIfRecordSize
	}

	if err := binary.Read(r, binary.BigEndian, &hnio.BytesIn); err != nil {
		return hnio, err
	}

	if err := binary.Read(r, binary.BigEndian, &hnio.PacketsIn); err != nil {
		return hnio, err
	}

	if err := binary.Read(r, binary.BigEndian, &hnio.ErrorsIn); err != nil {
		return hnio, err
	}

	if err := binary.Read(r, binary.BigEndian, &hnio.DropsIn); err != nil {
		return hnio, err
	}

	if err := binary.Read(r, binary.BigEndian, &hnio.BytesOut); err != nil {
		return hnio, err
	}

	if err := binary.Read(r, binary.BigEndian, &hnio.PacketsOut); err != nil {
		return hnio, err
	}

	if err := binary.Read(r, binary.BigEndian, &hnio.ErrorsOut); err != nil {
		return hnio, err
	}

	if err := binary.Read(r, binary.BigEndian, &hnio.DropsOut); err != nil {
		return hnio, err
	}

	return hnio, nil
}

func decodeVirtNodeRecord(r io.Reader) (datagram.VirtNode, error) {
	vn := datagram.VirtNode{
		DataFormat: datagram.VirtNodeRecordDataFormatValue,
	}

	if err := binary.Read(r, binary.BigEndian, &vn.Length); err != nil {
		return vn, err
	}

	if vn.Length != uint32(datagram.VirtNodeRecordValidLength) {
		return vn, ErrInvalidCounterIfRecordSize
	}

	if err := binary.Read(r, binary.BigEndian, &vn.Mhz); err != nil {
		return vn, err
	}

	if err := binary.Read(r, binary.BigEndian, &vn.CPUs); err != nil {
		return vn, err
	}

	if err := binary.Read(r, binary.BigEndian, &vn.Memory); err != nil {
		return vn, err
	}

	if err := binary.Read(r, binary.BigEndian, &vn.MemoryFree); err != nil {
		return vn, err
	}

	if err := binary.Read(r, binary.BigEndian, &vn.NumDomains); err != nil {
		return vn, err
	}

	return vn, nil
}

func decodeVirtCPURecord(r io.Reader) (datagram.VirtCPU, error) {
	vc := datagram.VirtCPU{
		DataFormat: datagram.VirtNodeRecordDataFormatValue,
	}

	if err := binary.Read(r, binary.BigEndian, &vc.Length); err != nil {
		return vc, err
	}

	if vc.Length != uint32(datagram.VirtCPURecordValidLength) {
		return vc, ErrInvalidCounterIfRecordSize
	}

	if err := binary.Read(r, binary.BigEndian, &vc.State); err != nil {
		return vc, err
	}

	if err := binary.Read(r, binary.BigEndian, &vc.CPUTime); err != nil {
		return vc, err
	}

	if err := binary.Read(r, binary.BigEndian, &vc.VirtualCPUCount); err != nil {
		return vc, err
	}

	return vc, nil
}

func decodeVirtMemoryRecord(r io.Reader) (datagram.VirtMemory, error) {
	hp := datagram.VirtMemory{
		DataFormat: datagram.VirtNodeRecordDataFormatValue,
	}

	if err := binary.Read(r, binary.BigEndian, &hp.Length); err != nil {
		return hp, err
	}

	if hp.Length != uint32(datagram.VirtMemoryRecordValidLength) {
		return hp, ErrInvalidCounterIfRecordSize
	}

	if err := binary.Read(r, binary.BigEndian, &hp.Memory); err != nil {
		return hp, err
	}

	if err := binary.Read(r, binary.BigEndian, &hp.MaxMemory); err != nil {
		return hp, err
	}

	return hp, nil
}

// FIXME  Wrong size?
func decodeVirtDiskIORecord(r io.Reader) (datagram.VirtDiskIO, error) {
	vdio := datagram.VirtDiskIO{
		DataFormat: datagram.VirtDiskIORecordDataFormatValue,
	}

	if err := binary.Read(r, binary.BigEndian, &vdio.Length); err != nil {
		return vdio, err
	}

	if vdio.Length != uint32(datagram.VirtDiskIORecordValidLength) {
		return vdio, ErrInvalidCounterIfRecordSize
	}

	if err := binary.Read(r, binary.BigEndian, &vdio.Capacity); err != nil {
		return vdio, err
	}

	if err := binary.Read(r, binary.BigEndian, &vdio.Allocation); err != nil {
		return vdio, err
	}

	if err := binary.Read(r, binary.BigEndian, &vdio.Available); err != nil {
		return vdio, err
	}

	if err := binary.Read(r, binary.BigEndian, &vdio.RDReq); err != nil {
		return vdio, err
	}

	if err := binary.Read(r, binary.BigEndian, &vdio.RDBytes); err != nil {
		return vdio, err
	}

	if err := binary.Read(r, binary.BigEndian, &vdio.WRReq); err != nil {
		return vdio, err
	}

	if err := binary.Read(r, binary.BigEndian, &vdio.WRBytes); err != nil {
		return vdio, err
	}

	if err := binary.Read(r, binary.BigEndian, &vdio.Errors); err != nil {
		return vdio, err
	}

	return vdio, nil
}

func decodeVirtNetIORecord(r io.Reader) (datagram.VirtNetIO, error) {
	vnio := datagram.VirtNetIO{
		DataFormat: datagram.HostAdaptersRecordDataFormatValue,
	}

	if err := binary.Read(r, binary.BigEndian, &vnio.Length); err != nil {
		return vnio, err
	}

	if vnio.Length != uint32(datagram.VirtNetIORecordValidLength) {
		return vnio, ErrInvalidCounterIfRecordSize
	}

	if err := binary.Read(r, binary.BigEndian, &vnio.RXBytes); err != nil {
		return vnio, err
	}

	if err := binary.Read(r, binary.BigEndian, &vnio.RXPackets); err != nil {
		return vnio, err
	}

	if err := binary.Read(r, binary.BigEndian, &vnio.RXErrs); err != nil {
		return vnio, err
	}

	if err := binary.Read(r, binary.BigEndian, &vnio.RXDrop); err != nil {
		return vnio, err
	}

	if err := binary.Read(r, binary.BigEndian, &vnio.TXBytes); err != nil {
		return vnio, err
	}

	if err := binary.Read(r, binary.BigEndian, &vnio.TXPackets); err != nil {
		return vnio, err
	}

	if err := binary.Read(r, binary.BigEndian, &vnio.TXErrs); err != nil {
		return vnio, err
	}

	if err := binary.Read(r, binary.BigEndian, &vnio.TXDrop); err != nil {
		return vnio, err
	}

	return vnio, nil
}
