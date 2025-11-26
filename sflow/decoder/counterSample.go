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

func decodeCounterSampleExpandedFormat(r io.Reader, format, length uint32) (*datagram.CounterSampleExpanded, error) {
	header := datagram.SampleHeader{
		Format: format,
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

	// TODO From here downwards is the same for normal CounterSample, so this should be `decodeCounterSampleRecords` or something like that
	err = binary.Read(r, binary.BigEndian, &cs.RecordsCount)
	if err != nil {
		return nil, err
	}

	cs.Records = make([]datagram.Record, 0, cs.RecordsCount)
	for i := uint32(0); i < cs.RecordsCount; i++ {
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
			cs.Records = append(cs.Records, record)
		case datagram.EthernetCountersRecordDataFormatValue:
			decoded, err := decodeEthernetCountersRecord(r)
			if err != nil {
				return nil, err
			}

			record = &decoded
			cs.Records = append(cs.Records, record)
		case datagram.TokenringCountersRecordDataFormatValue:
			decoded, err := decordTokenringCountersRecord(r)
			if err != nil {
				return nil, err
			}

			record = &decoded
			cs.Records = append(cs.Records, record)
		case datagram.VgCountersRecordDataFormatValue:
			decoded, err := decodeVgCountersRecord(r)
			if err != nil {
				return nil, err
			}

			record = &decoded
			cs.Records = append(cs.Records, record)
		case datagram.VlanCountersRecordDataFormatValue:
			decoded, err := decodeVlanCountersRecord(r)
			if err != nil {
				return nil, err
			}

			record = &decoded
			cs.Records = append(cs.Records, record)
		case datagram.ProcessorCountersRecordDataFormatValue:
			decoded, err := decodeProcessorCountersRecord(r)
			if err != nil {
				return nil, err
			}

			record = &decoded
			cs.Records = append(cs.Records, record)
		case datagram.OpenFlowPortRecordDataFormatValue:
			decoded, err := decodeOpenFlowPortRecord(r)
			if err != nil {
				return nil, err
			}

			record = &decoded
			cs.Records = append(cs.Records, record)
		case datagram.OpenFlowPortNameRecordDataFormatValue:
			decoded, err := decodeOpenFlowPortNameRecord(r)
			if err != nil {
				return nil, err
			}

			record = &decoded
			cs.Records = append(cs.Records, record)
		case datagram.HostDescrRecordDataFormatValue:
			decoded, err := decodeHostDescrRecord(r)
			if err != nil {
				return nil, err
			}

			record = &decoded
			cs.Records = append(cs.Records, record)
		case datagram.HostAdaptersRecordDataFormatValue:
			decoded, err := decodeHostAdaptersRecord(r)
			if err != nil {
				return nil, err
			}

			record = &decoded
			cs.Records = append(cs.Records, record)
		case datagram.HostParentRecordDataFormatValue:
			decoded, err := decodeHostParentRecord(r)
			if err != nil {
				return nil, err
			}

			record = &decoded
			cs.Records = append(cs.Records, record)
		case datagram.HostCPURecordDataFormatValue:
			decoded, err := decodeHostCPURecord(r)
			if err != nil {
				return nil, err
			}

			record = &decoded
			cs.Records = append(cs.Records, record)
		case datagram.HostMemoryRecordDataFormatValue:
			decoded, err := decodeHostMemoryRecord(r)
			if err != nil {
				return nil, err
			}

			record = &decoded
			cs.Records = append(cs.Records, record)
		case datagram.HostDiskIORecordDataFormatValue:
			decoded, err := decodeHostDiskIORecord(r)
			if err != nil {
				return nil, err
			}

			record = &decoded
			cs.Records = append(cs.Records, record)
		case datagram.HostHetIORecordDataFormatValue:
			decoded, err := decodeHostNetIORecord(r)
			if err != nil {
				return nil, err
			}

			record = &decoded
			cs.Records = append(cs.Records, record)
		case datagram.VirtNodeRecordDataFormatValue:
			decoded, err := decodeVirtNodeRecord(r)
			if err != nil {
				return nil, err
			}

			record = &decoded
			cs.Records = append(cs.Records, record)
		case datagram.VirtCPURecordDataFormatValue:
			decoded, err := decodeVirtCPURecord(r)
			if err != nil {
				return nil, err
			}

			record = &decoded
			cs.Records = append(cs.Records, record)
		case datagram.VirtMemoryRecordDataFormatValue:
			decoded, err := decodeVirtMemoryRecord(r)
			if err != nil {
				return nil, err
			}

			record = &decoded
			cs.Records = append(cs.Records, record)
		case datagram.VirtDiskIORecordDataFormatValue:
			decoded, err := decodeVirtDiskIORecord(r)
			if err != nil {
				return nil, err
			}

			record = &decoded
			cs.Records = append(cs.Records, record)
		case datagram.VirtNetIORecordDataFormatValue:
			decoded, err := decodeVirtNetIORecord(r)
			if err != nil {
				return nil, err
			}

			record = &decoded
			cs.Records = append(cs.Records, record)
		default:
			record, err := decodeUnknownRecord(r, dataFormat)
			if err != nil {
				return nil, err
			}

			cs.Records = append(cs.Records, record)
		}

	}

	return cs, nil
}
