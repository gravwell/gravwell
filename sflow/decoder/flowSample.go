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

func decodeFlowSampleFormat(r *io.LimitedReader, length uint32) (*datagram.FlowSample, error) {
	header := datagram.SampleHeader{
		Format: datagram.CounterSampleFormat,
		Length: length,
	}
	fs := &datagram.FlowSample{
		SampleHeader: header,
	}

	err := binary.Read(r, binary.BigEndian, &fs.SequenceNum)
	if err != nil {
		return nil, err
	}

	err = binary.Read(r, binary.BigEndian, &fs.SFlowDataSource)
	if err != nil {
		return nil, err
	}

	err = binary.Read(r, binary.BigEndian, &fs.SamplingRate)
	if err != nil {
		return nil, err
	}

	err = binary.Read(r, binary.BigEndian, &fs.SamplePool)
	if err != nil {
		return nil, err
	}

	err = binary.Read(r, binary.BigEndian, &fs.Drops)
	if err != nil {
		return nil, err
	}

	err = binary.Read(r, binary.BigEndian, &fs.Input)
	if err != nil {
		return nil, err
	}

	err = binary.Read(r, binary.BigEndian, &fs.Output)
	if err != nil {
		return nil, err
	}

	recordsCount, err := decodeLength(r, MinBytesPerItem)
	if err != nil {
		return nil, err
	}

	fs.Records, err = decodeFlowSampleRecords(r, recordsCount)

	return fs, err
}

func decodeFlowSampleExpandedFormat(r *io.LimitedReader, length uint32) (*datagram.FlowSampleExpanded, error) {
	header := datagram.SampleHeader{
		Format: datagram.FlowSampleExpandedFormat,
		Length: length,
	}
	fse := &datagram.FlowSampleExpanded{
		SampleHeader: header,
	}

	err := binary.Read(r, binary.BigEndian, &fse.SequenceNum)
	if err != nil {
		return nil, err
	}

	err = binary.Read(r, binary.BigEndian, &fse.SourceIDType)
	if err != nil {
		return nil, err
	}

	err = binary.Read(r, binary.BigEndian, &fse.SourceIDIndex)
	if err != nil {
		return nil, err
	}

	err = binary.Read(r, binary.BigEndian, &fse.SamplingRate)
	if err != nil {
		return nil, err
	}

	err = binary.Read(r, binary.BigEndian, &fse.SamplePool)
	if err != nil {
		return nil, err
	}

	err = binary.Read(r, binary.BigEndian, &fse.Drops)
	if err != nil {
		return nil, err
	}

	err = binary.Read(r, binary.BigEndian, &fse.Input.Format)
	if err != nil {
		return nil, err
	}

	err = binary.Read(r, binary.BigEndian, &fse.Input.Value)
	if err != nil {
		return nil, err
	}

	err = binary.Read(r, binary.BigEndian, &fse.Output.Format)
	if err != nil {
		return nil, err
	}

	err = binary.Read(r, binary.BigEndian, &fse.Output.Value)
	if err != nil {
		return nil, err
	}

	recordsCount, err := decodeLength(r, MinBytesPerItem)
	if err != nil {
		return nil, err
	}

	fse.Records, err = decodeFlowSampleRecords(r, recordsCount)

	return fse, err
}

func decodeFlowSampleRecords(r *io.LimitedReader, recordsCount uint32) ([]datagram.Record, error) {
	records := make([]datagram.Record, 0, recordsCount)
	for range recordsCount {
		var dataFormat uint32
		err := binary.Read(r, binary.BigEndian, &dataFormat)
		if err != nil {
			return nil, err
		}

		var record datagram.Record
		switch dataFormat {
		case datagram.FlowSampledHeaderRecordDataFormatValue:
			record, err = decodeFlowSampledHeader(r)
			if err != nil {
				return nil, err
			}

			records = append(records, record)
		case datagram.SampledEthernetRecordDataFormatValue:
			record, err = decodeSampledEthernet(r)
			if err != nil {
				return nil, err
			}

			records = append(records, record)
		case datagram.SampledIPv4RecordDataFormatValue:
			record, err = decodeSampledIPv4(r)
			if err != nil {
				return nil, err
			}

			records = append(records, record)
		case datagram.SampledIPv6RecordDataFormatValue:
			record, err = decodeSampledIPv6(r)
			if err != nil {
				return nil, err
			}

			records = append(records, record)
		case datagram.ExtendedSwitchRecordDataFormatValue:
			record, err = decodeExtendedSwitch(r)
			if err != nil {
				return nil, err
			}

			records = append(records, record)
		case datagram.ExtendedRouterRecordDataFormatValue:
			record, err = decodeExtendedRouter(r)
			if err != nil {
				return nil, err
			}

			records = append(records, record)
		case datagram.ExtendedGatewayRecordDataFormatValue:
			record, err = decodeExtendedGateway(r)
			if err != nil {
				return nil, err
			}

			records = append(records, record)
		case datagram.ExtendedUserRecordDataFormatValue:
			record, err = decodeExtendedUser(r)
			if err != nil {
				return nil, err
			}

			records = append(records, record)
		case datagram.ExtendedMPLSRecordDataFormatValue:
			record, err = decodeExtendedMPLS(r)
			if err != nil {
				return nil, err
			}

			records = append(records, record)
		case datagram.ExtendedNATRecordDataFormatValue:
			record, err = decodeExtendedNAT(r)
			if err != nil {
				return nil, err
			}

			records = append(records, record)
		case datagram.ExtendedMPLSTunnelRecordDataFormatValue:
			record, err = decodeExtendedMPLSTunnel(r)
			if err != nil {
				return nil, err
			}

			records = append(records, record)
		case datagram.ExtendedMPLSVCRecordDataFormatValue:
			record, err = decodeExtendedMPLSVC(r)
			if err != nil {
				return nil, err
			}

			records = append(records, record)
		case datagram.ExtendedMPLSFTNRecordDataFormatValue:
			record, err = decodeExtendedMPLSFTN(r)
			if err != nil {
				return nil, err
			}

			records = append(records, record)
		case datagram.ExtendedMPLSLDPFECRecordDataFormatValue:
			record, err = decodeExtendedMPLSLDPFEC(r)
			if err != nil {
				return nil, err
			}

			records = append(records, record)
		case datagram.ExtendedVLANTunnelRecordDataFormatValue:
			record, err = decodeExtendedVLANTunnel(r)
			if err != nil {
				return nil, err
			}

			records = append(records, record)
		case datagram.ExtendedEgressQueueRecordDataFormatValue:
			record, err = decodeExtendedEgressQueue(r)
			if err != nil {
				return nil, err
			}

			records = append(records, record)
		case datagram.ExtendedACLRecordDataFormatValue:
			record, err = decodeExtendedACL(r)
			if err != nil {
				return nil, err
			}

			records = append(records, record)
		case datagram.ExtendedFunctionRecordDataFormatValue:
			record, err = decodeExtendedFunction(r)
			if err != nil {
				return nil, err
			}

			records = append(records, record)
		case datagram.ExtendedSocketIPv4RecordDataFormatValue:
			record, err = decodeExtendedSocketIPv4(r)
			if err != nil {
				return nil, err
			}

			records = append(records, record)
		case datagram.ExtendedSocketIPv6RecordDataFormatValue:
			record, err = decodeExtendedSocketIPv6(r)
			if err != nil {
				return nil, err
			}

			records = append(records, record)
		case datagram.ExtendedTCPInfoRecordDataFormatValue:
			record, err = decodeExtendedTCPInfo(r)
			if err != nil {
				return nil, err
			}

			records = append(records, record)
		default:
			record, err = decodeUnknownRecord(r, dataFormat)
			if err != nil {
				return nil, err
			}

			records = append(records, record)
		}
	}

	return records, nil
}
