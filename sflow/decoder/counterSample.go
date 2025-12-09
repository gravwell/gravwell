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
			decoded, err := decodeCounterIfRecord(r)
			if err != nil {
				return nil, err
			}

			record = &decoded
			records = append(records, record)
		case datagram.EthernetCountersRecordDataFormatValue:
			decoded, err := decodeEthernetCountersRecord(r)
			if err != nil {
				return nil, err
			}

			record = &decoded
			records = append(records, record)
		case datagram.TokenringCountersRecordDataFormatValue:
			decoded, err := decordTokenringCountersRecord(r)
			if err != nil {
				return nil, err
			}

			record = &decoded
			records = append(records, record)
		case datagram.VgCountersRecordDataFormatValue:
			decoded, err := decodeVgCountersRecord(r)
			if err != nil {
				return nil, err
			}

			record = &decoded
			records = append(records, record)
		case datagram.VlanCountersRecordDataFormatValue:
			decoded, err := decodeVlanCountersRecord(r)
			if err != nil {
				return nil, err
			}

			record = &decoded
			records = append(records, record)
		case datagram.IEEE80211CountersRecordDataFormatValue:
			decoded, err := decodeIEEE80211CountersRecord(r)
			if err != nil {
				return nil, err
			}

			record = &decoded
			records = append(records, record)
		case datagram.LAGPortStatsRecordDataFormatValue:
			decoded, err := decodeLAGPortStatsRecord(r)
			if err != nil {
				return nil, err
			}

			record = &decoded
			records = append(records, record)
		case datagram.ProcessorCountersRecordDataFormatValue:
			decoded, err := decodeProcessorCountersRecord(r)
			if err != nil {
				return nil, err
			}

			record = &decoded
			records = append(records, record)
		case datagram.OpenFlowPortRecordDataFormatValue:
			decoded, err := decodeOpenFlowPortRecord(r)
			if err != nil {
				return nil, err
			}

			record = &decoded
			records = append(records, record)
		case datagram.OpenFlowPortNameRecordDataFormatValue:
			decoded, err := decodeOpenFlowPortNameRecord(r)
			if err != nil {
				return nil, err
			}

			record = &decoded
			records = append(records, record)
		case datagram.HostDescrRecordDataFormatValue:
			decoded, err := decodeHostDescrRecord(r)
			if err != nil {
				return nil, err
			}

			record = &decoded
			records = append(records, record)
		case datagram.HostAdaptersRecordDataFormatValue:
			decoded, err := decodeHostAdaptersRecord(r)
			if err != nil {
				return nil, err
			}

			record = &decoded
			records = append(records, record)
		case datagram.HostParentRecordDataFormatValue:
			decoded, err := decodeHostParentRecord(r)
			if err != nil {
				return nil, err
			}

			record = &decoded
			records = append(records, record)
		case datagram.HostCPURecordDataFormatValue:
			decoded, err := decodeHostCPURecord(r)
			if err != nil {
				return nil, err
			}

			record = &decoded
			records = append(records, record)
		case datagram.HostMemoryRecordDataFormatValue:
			decoded, err := decodeHostMemoryRecord(r)
			if err != nil {
				return nil, err
			}

			record = &decoded
			records = append(records, record)
		case datagram.HostDiskIORecordDataFormatValue:
			decoded, err := decodeHostDiskIORecord(r)
			if err != nil {
				return nil, err
			}

			record = &decoded
			records = append(records, record)
		case datagram.HostHetIORecordDataFormatValue:
			decoded, err := decodeHostNetIORecord(r)
			if err != nil {
				return nil, err
			}

			record = &decoded
			records = append(records, record)
		case datagram.VirtNodeRecordDataFormatValue:
			decoded, err := decodeVirtNodeRecord(r)
			if err != nil {
				return nil, err
			}

			record = &decoded
			records = append(records, record)
		case datagram.VirtCPURecordDataFormatValue:
			decoded, err := decodeVirtCPURecord(r)
			if err != nil {
				return nil, err
			}

			record = &decoded
			records = append(records, record)
		case datagram.VirtMemoryRecordDataFormatValue:
			decoded, err := decodeVirtMemoryRecord(r)
			if err != nil {
				return nil, err
			}

			record = &decoded
			records = append(records, record)
		case datagram.VirtDiskIORecordDataFormatValue:
			decoded, err := decodeVirtDiskIORecord(r)
			if err != nil {
				return nil, err
			}

			record = &decoded
			records = append(records, record)
		case datagram.VirtNetIORecordDataFormatValue:
			decoded, err := decodeVirtNetIORecord(r)
			if err != nil {
				return nil, err
			}

			record = &decoded
			records = append(records, record)
		case datagram.JVMMachineNameRecordDataFormatValue:
			decoded, err := decodeJVMMachineNameRecord(r)
			if err != nil {
				return nil, err
			}

			record = &decoded
			records = append(records, record)
		case datagram.JVMStatisticsRecordDataFormatValue:
			decoded, err := decodeJVMStatisticsRecord(r)
			if err != nil {
				return nil, err
			}

			record = &decoded
			records = append(records, record)
		case datagram.HTTPCountersRecordDataFormatValue:
			decoded, err := decodeHTTPCountersRecord(r)
			if err != nil {
				return nil, err
			}

			record = &decoded
			records = append(records, record)
		case datagram.MemcacheCountersRecordDataFormatValue:
			decoded, err := decodeMemcacheCountersRecord(r)
			if err != nil {
				return nil, err
			}

			record = &decoded
			records = append(records, record)
		case datagram.AppOperationsRecordDataFormatValue:
			decoded, err := decodeAppOperationsRecord(r)
			if err != nil {
				return nil, err
			}

			record = &decoded
			records = append(records, record)
		case datagram.AppResourcesRecordDataFormatValue:
			decoded, err := decodeAppResourcesRecord(r)
			if err != nil {
				return nil, err
			}

			record = &decoded
			records = append(records, record)
		case datagram.AppWorkersRecordDataFormatValue:
			decoded, err := decodeAppWorkersRecord(r)
			if err != nil {
				return nil, err
			}

			record = &decoded
			records = append(records, record)
		case datagram.EnergyRecordDataFormatValue:
			decoded, err := decodeEnergyRecord(r)
			if err != nil {
				return nil, err
			}

			record = &decoded
			records = append(records, record)
		case datagram.TemperatureRecordDataFormatValue:
			decoded, err := decodeTemperatureRecord(r)
			if err != nil {
				return nil, err
			}

			record = &decoded
			records = append(records, record)
		case datagram.HumidityRecordDataFormatValue:
			decoded, err := decodeHumidityRecord(r)
			if err != nil {
				return nil, err
			}

			record = &decoded
			records = append(records, record)
		case datagram.FansRecordDataFormatValue:
			decoded, err := decodeFansRecord(r)
			if err != nil {
				return nil, err
			}

			record = &decoded
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
