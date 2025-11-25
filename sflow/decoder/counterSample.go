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

	err = binary.Read(r, binary.BigEndian, &cs.CounterRecordsCount)
	if err != nil {
		return nil, err
	}

	cs.Records = make([]datagram.Record, 0, cs.CounterRecordsCount)
	for i := uint32(0); i < cs.CounterRecordsCount; i++ {
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
			return nil, datagram.ErrUnknownRecordType
		}

	}

	return cs, nil
}
