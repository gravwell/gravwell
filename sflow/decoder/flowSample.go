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

func decodeFlowSampleFormat(r io.Reader, length uint32) (*datagram.FlowSample, error) {
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

	var recordsCount uint32
	err = binary.Read(r, binary.BigEndian, &recordsCount)
	if err != nil {
		return nil, err
	}

	fs.Records, err = decodeFlowSampleRecords(r, recordsCount)

	return fs, err
}

func decodeFlowSampleExpandedFormat(r io.Reader, length uint32) (*datagram.FlowSampleExpanded, error) {
	header := datagram.SampleHeader{
		Format: datagram.CounterSampleExpandedFormat,
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

	err = binary.Read(r, binary.BigEndian, &fse.Input)
	if err != nil {
		return nil, err
	}

	err = binary.Read(r, binary.BigEndian, &fse.Output)
	if err != nil {
		return nil, err
	}

	var recordsCount uint32
	err = binary.Read(r, binary.BigEndian, &recordsCount)
	if err != nil {
		return nil, err
	}

	fse.Records, err = decodeFlowSampleRecords(r, recordsCount)

	return fse, err
}

func decodeFlowSampleRecords(r io.Reader, recordsCount uint32) ([]datagram.Record, error) {
	records := make([]datagram.Record, 0, recordsCount)
	for range recordsCount {
		var dataFormat uint32
		err := binary.Read(r, binary.BigEndian, &dataFormat)
		if err != nil {
			return nil, err
		}

		var record datagram.Record
		switch dataFormat {
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
