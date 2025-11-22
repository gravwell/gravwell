package decoder

import (
	"encoding/binary"
	"errors"
	"io"

	"github.com/gravwell/gravwell/v3/sflow/datagram"
)

var (
	ErrInvalidCounterIfRecordSize = errors.New("counter if record size is invalid")
	ErrInvalidEthernetCountersRecordSize = errors.New("counter ethernet record size is invalid")
)

func decodeCounterIfRecord(r io.Reader, dataFormat, length uint32) (datagram.CounterIfRecord, error) {
	cir := datagram.CounterIfRecord{
		DataFormat: dataFormat,
		Length: length,
	}

	if cir.Length != uint32(datagram.CounterIfRecordValidLength) {
		return cir, ErrInvalidCounterIfRecordSize
	}

	err := binary.Read(r, binary.BigEndian, cir.IfIndex)
	if err != nil {
		return cir, err
	}

	err = binary.Read(r, binary.BigEndian, cir.IfType)
	if err != nil {
		return cir, err
	}

	err = binary.Read(r, binary.BigEndian, cir.IfSpeed)
	if err != nil {
		return cir, err
	}

	err = binary.Read(r, binary.BigEndian, cir.IfDirection)
	if err != nil {
		return cir, err
	}

	err = binary.Read(r, binary.BigEndian, cir.IfStatus)
	if err != nil {
		return cir, err
	}

	err = binary.Read(r, binary.BigEndian, cir.IfInOctets)
	if err != nil {
		return cir, err
	}

	err = binary.Read(r, binary.BigEndian, cir.IfInUcastPkts)
	if err != nil {
		return cir, err
	}

	err = binary.Read(r, binary.BigEndian, cir.IfInMulticastPkts)
	if err != nil {
		return cir, err
	}

	err = binary.Read(r, binary.BigEndian, cir.IfInBroadcastPkts)
	if err != nil {
		return cir, err
	}

	err = binary.Read(r, binary.BigEndian, cir.IfInDiscards)
	if err != nil {
		return cir, err
	}

	err = binary.Read(r, binary.BigEndian, cir.IfInErrors)
	if err != nil {
		return cir, err
	}

	err = binary.Read(r, binary.BigEndian, cir.IfInUnknownProtos)
	if err != nil {
		return cir, err
	}

	err = binary.Read(r, binary.BigEndian, cir.IfOutOctets)
	if err != nil {
		return cir, err
	}

	err = binary.Read(r, binary.BigEndian, cir.IfOutUcastPkts)
	if err != nil {
		return cir, err
	}

	err = binary.Read(r, binary.BigEndian, cir.IfOutMulticastPkts)
	if err != nil {
		return cir, err
	}

	err = binary.Read(r, binary.BigEndian, cir.IfOutBroadcastPkts)
	if err != nil {
		return cir, err
	}

	err = binary.Read(r, binary.BigEndian, cir.IfOutDiscards)
	if err != nil {
		return cir, err
	}

	err = binary.Read(r, binary.BigEndian, cir.IfOutErrors)
	if err != nil {
		return cir, err
	}

	err = binary.Read(r, binary.BigEndian, cir.IfPromiscuousMode)
	if err != nil {
		return cir, err
	}

	return cir, err
}

func decodeEthernetCountersRecord(r io.Reader, dataFormat, length uint32) (datagram.EthernetCounters, error) {
	ecr := datagram.EthernetCounters{
		DataFormat: dataFormat,
		Length: length,
	}

	if ecr.Length != uint32(datagram.EthernetCountersRecordValidLength) {
		return ecr, ErrInvalidCounterIfRecordSize
	}

	err := binary.Read(r, binary.BigEndian, ecr.Dot3StatsAlignmentErrors)
	if err != nil {
		return ecr, err
	}

	err = binary.Read(r, binary.BigEndian, ecr.Dot3StatsFCSErrors)
	if err != nil {
		return ecr, err
	}

	err = binary.Read(r, binary.BigEndian, ecr.Dot3StatsSingleCollisionFrames)
	if err != nil {
		return ecr, err
	}

	err = binary.Read(r, binary.BigEndian, ecr.Dot3StatsMultipleCollisionFrames)
	if err != nil {
		return ecr, err
	}

	err = binary.Read(r, binary.BigEndian, ecr.Dot3StatsSQETestErrors)
	if err != nil {
		return ecr, err
	}

	err = binary.Read(r, binary.BigEndian, ecr.Dot3StatsDeferredTransmissions)
	if err != nil {
		return ecr, err
	}

	err = binary.Read(r, binary.BigEndian, ecr.Dot3StatsLateCollisions)
	if err != nil {
		return ecr, err
	}

	err = binary.Read(r, binary.BigEndian, ecr.Dot3StatsLateCollisions)
	if err != nil {
		return ecr, err
	}

	err = binary.Read(r, binary.BigEndian, ecr.Dot3StatsExcessiveCollisions)
	if err != nil {
		return ecr, err
	}

	err = binary.Read(r, binary.BigEndian, ecr.Dot3StatsInternalMacTransmitErrors)
	if err != nil {
		return ecr, err
	}

	err = binary.Read(r, binary.BigEndian, ecr.Dot3StatsCarrierSenseErrors)
	if err != nil {
		return ecr, err
	}

	err = binary.Read(r, binary.BigEndian, ecr.Dot3StatsFrameTooLongs)
	if err != nil {
		return ecr, err
	}

	err = binary.Read(r, binary.BigEndian, ecr.Dot3StatsInternalMacReceiveErrors)
	if err != nil {
		return ecr, err
	}

	err = binary.Read(r, binary.BigEndian, ecr.Dot3StatsSymbolErrors)
	if err != nil {
		return ecr, err
	}

	return ecr, err
}
