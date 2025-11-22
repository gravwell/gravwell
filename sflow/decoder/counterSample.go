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

	// FIXME FROM HERE DOWNWARDS, SOMETHING IS WRONG, I AM GETTING ABSOLUTELY NON SENSICAL dataFormat VALUES... Above, things seem correct
	// Use https://github.com/sflow/sflowtool to see the decoded packets and resend them to the ingester to debug this shit
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
			decoded, err := decodeCounterIfRecord(r, dataFormat, length)
			if err != nil {
				return nil, err
			}

			record = &decoded
			cs.Records = append(cs.Records, record)
		case datagram.EthernetCountersRecordDataFormatValue:
			decoded, err := decodeEthernetCountersRecord(r, dataFormat, length)
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
