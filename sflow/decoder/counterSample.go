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
	"io"

	"github.com/gravwell/gravwell/v3/sflow/datagram"
)

func decodeCounterSampleFormat(r io.Reader, length uint32) (*datagram.CounterSample, error) {
	header := datagram.SampleHeader{
		Format: datagram.CounterSampleFormat,
		Length: length,
	}
	cs := &datagram.CounterSample{
		SampleHeader: header,
	}

	err := binary.Read(r, binary.BigEndian, &cs.SequenceNum)
	if err != nil {
		return nil, err
	}

	err = binary.Read(r, binary.BigEndian, &cs.SFlowDataSource)
	if err != nil {
		return nil, err
	}

	var recordsCount uint32
	err = binary.Read(r, binary.BigEndian, &recordsCount)
	if err != nil {
		return nil, err
	}

	cs.Records, err = decodeCounterSampleRecords(r, recordsCount)

	return cs, err
}

func decodeCounterSampleExpandedFormat(r io.Reader, length uint32) (*datagram.CounterSampleExpanded, error) {
	header := datagram.SampleHeader{
		Format: datagram.CounterSampleExpandedFormat,
		Length: length,
	}
	cs := &datagram.CounterSampleExpanded{
		SampleHeader: header,
	}

	err := binary.Read(r, binary.BigEndian, &cs.SequenceNum)
	if err != nil {
		return nil, err
	}

	err = binary.Read(r, binary.BigEndian, &cs.SourceIDType)
	if err != nil {
		return nil, err
	}

	err = binary.Read(r, binary.BigEndian, &cs.SourceIDIndex)
	if err != nil {
		return nil, err
	}

	var recordsCount uint32
	err = binary.Read(r, binary.BigEndian, &recordsCount)
	if err != nil {
		return nil, err
	}

	cs.Records, err = decodeCounterSampleRecords(r, recordsCount)

	return cs, err
}

func decodeCounterSampleRecords(r io.Reader, recordsCount uint32) ([]datagram.Record, error) {
	records := make([]datagram.Record, 0, recordsCount)
	for range recordsCount {
		var dataFormat uint32
		err := binary.Read(r, binary.BigEndian, &dataFormat)
		if err != nil {
			return nil, err
		}

		var record datagram.Record
		switch dataFormat {
		case datagram.CounterIfRecordDataFormatValue:
			record, err = decodeCounterIfRecord(r)
			if err != nil {
				return nil, err
			}
			records = append(records, record)
		case datagram.EthernetCountersRecordDataFormatValue:
			record, err = decodeEthernetCountersRecord(r)
			if err != nil {
				return nil, err
			}
			records = append(records, record)
		case datagram.TokenringCountersRecordDataFormatValue:
			record, err = decordTokenringCountersRecord(r)
			if err != nil {
				return nil, err
			}
			records = append(records, record)
		case datagram.VgCountersRecordDataFormatValue:
			record, err = decodeVgCountersRecord(r)
			if err != nil {
				return nil, err
			}
			records = append(records, record)
		case datagram.VlanCountersRecordDataFormatValue:
			record, err = decodeVlanCountersRecord(r)
			if err != nil {
				return nil, err
			}
			records = append(records, record)
		case datagram.IEEE80211CountersRecordDataFormatValue:
			record, err = decodeIEEE80211CountersRecord(r)
			if err != nil {
				return nil, err
			}
			records = append(records, record)
		case datagram.LAGPortStatsRecordDataFormatValue:
			record, err = decodeLAGPortStatsRecord(r)
			if err != nil {
				return nil, err
			}
			records = append(records, record)
		case datagram.ProcessorCountersRecordDataFormatValue:
			record, err = decodeProcessorCountersRecord(r)
			if err != nil {
				return nil, err
			}
			records = append(records, record)
		case datagram.QueueLengthRecordDataFormatValue:
			record, err = decodeQueueLengthRecord(r)
			if err != nil {
				return nil, err
			}
			records = append(records, record)
		case datagram.OpenFlowPortRecordDataFormatValue:
			record, err = decodeOpenFlowPortRecord(r)
			if err != nil {
				return nil, err
			}
			records = append(records, record)
		case datagram.OpenFlowPortNameRecordDataFormatValue:
			record, err = decodeOpenFlowPortNameRecord(r)
			if err != nil {
				return nil, err
			}
			records = append(records, record)
		case datagram.HostDescrRecordDataFormatValue:
			record, err = decodeHostDescrRecord(r)
			if err != nil {
				return nil, err
			}
			records = append(records, record)
		case datagram.HostAdaptersRecordDataFormatValue:
			record, err = decodeHostAdaptersRecord(r)
			if err != nil {
				return nil, err
			}
			records = append(records, record)
		case datagram.HostParentRecordDataFormatValue:
			record, err = decodeHostParentRecord(r)
			if err != nil {
				return nil, err
			}
			records = append(records, record)
		case datagram.HostCPURecordDataFormatValue:
			record, err = decodeHostCPURecord(r)
			if err != nil {
				return nil, err
			}
			records = append(records, record)
		case datagram.HostMemoryRecordDataFormatValue:
			record, err = decodeHostMemoryRecord(r)
			if err != nil {
				return nil, err
			}
			records = append(records, record)
		case datagram.HostDiskIORecordDataFormatValue:
			record, err = decodeHostDiskIORecord(r)
			if err != nil {
				return nil, err
			}
			records = append(records, record)
		case datagram.HostNetIORecordDataFormatValue:
			record, err = decodeHostNetIORecord(r)
			if err != nil {
				return nil, err
			}
			records = append(records, record)
		case datagram.MIB2IPGroupRecordDataFormatValue:
			record, err = decodeMIB2IPGroupRecord(r)
			if err != nil {
				return nil, err
			}
			records = append(records, record)
		case datagram.MIB2ICMPGroupRecordDataFormatValue:
			record, err = decodeMIB2ICMPGroupRecord(r)
			if err != nil {
				return nil, err
			}
			records = append(records, record)
		case datagram.MIB2TCPGroupRecordDataFormatValue:
			record, err = decodeMIB2TCPGroupRecord(r)
			if err != nil {
				return nil, err
			}
			records = append(records, record)
		case datagram.MIB2UDPGroupRecordDataFormatValue:
			record, err = decodeMIB2UDPGroupRecord(r)
			if err != nil {
				return nil, err
			}
			records = append(records, record)
		case datagram.VirtNodeRecordDataFormatValue:
			record, err = decodeVirtNodeRecord(r)
			if err != nil {
				return nil, err
			}
			records = append(records, record)
		case datagram.VirtCPURecordDataFormatValue:
			record, err = decodeVirtCPURecord(r)
			if err != nil {
				return nil, err
			}
			records = append(records, record)
		case datagram.VirtMemoryRecordDataFormatValue:
			record, err = decodeVirtMemoryRecord(r)
			if err != nil {
				return nil, err
			}
			records = append(records, record)
		case datagram.VirtDiskIORecordDataFormatValue:
			record, err = decodeVirtDiskIORecord(r)
			if err != nil {
				return nil, err
			}
			records = append(records, record)
		case datagram.VirtNetIORecordDataFormatValue:
			record, err = decodeVirtNetIORecord(r)
			if err != nil {
				return nil, err
			}
			records = append(records, record)
		case datagram.JVMMachineNameRecordDataFormatValue:
			record, err = decodeJVMMachineNameRecord(r)
			if err != nil {
				return nil, err
			}
			records = append(records, record)
		case datagram.JVMStatisticsRecordDataFormatValue:
			record, err = decodeJVMStatisticsRecord(r)
			if err != nil {
				return nil, err
			}
			records = append(records, record)
		case datagram.HTTPCountersRecordDataFormatValue:
			record, err = decodeHTTPCountersRecord(r)
			if err != nil {
				return nil, err
			}
			records = append(records, record)
		case datagram.MemcacheCountersRecordDataFormatValue:
			record, err = decodeMemcacheCountersRecord(r)
			if err != nil {
				return nil, err
			}
			records = append(records, record)
		case datagram.AppOperationsRecordDataFormatValue:
			record, err = decodeAppOperationsRecord(r)
			if err != nil {
				return nil, err
			}
			records = append(records, record)
		case datagram.AppResourcesRecordDataFormatValue:
			record, err = decodeAppResourcesRecord(r)
			if err != nil {
				return nil, err
			}
			records = append(records, record)
		case datagram.AppWorkersRecordDataFormatValue:
			record, err = decodeAppWorkersRecord(r)
			if err != nil {
				return nil, err
			}
			records = append(records, record)
		case datagram.OVSDPStatsRecordDataFormatValue:
			record, err = decodeOVSDPStatsRecord(r)
			if err != nil {
				return nil, err
			}
			records = append(records, record)
		case datagram.EnergyRecordDataFormatValue:
			record, err = decodeEnergyRecord(r)
			if err != nil {
				return nil, err
			}
			records = append(records, record)
		case datagram.TemperatureRecordDataFormatValue:
			record, err = decodeTemperatureRecord(r)
			if err != nil {
				return nil, err
			}
			records = append(records, record)
		case datagram.HumidityRecordDataFormatValue:
			record, err = decodeHumidityRecord(r)
			if err != nil {
				return nil, err
			}
			records = append(records, record)
		case datagram.FansRecordDataFormatValue:
			record, err = decodeFansRecord(r)
			if err != nil {
				return nil, err
			}
			records = append(records, record)
		default:
			record, err := decodeUnknownRecord(r, dataFormat)
			if err != nil {
				return nil, err
			}

			records = append(records, record)
		}
	}

	return records, nil
}
