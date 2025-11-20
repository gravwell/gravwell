package sflow

import (
	"encoding/binary"
	"errors"
	"io"
)

var (
	ErrSampleMalformedOrIncomplete = errors.New("sample is malformed or incomplete")
)

// TODO  Likely will move this and `decode.go` to it's own package

func decodeSample(r io.Reader) (Sample, error) {
	var format uint32
	var length uint32
	var err error

	err = binary.Read(r, binary.BigEndian, &format)
	if err != nil {
		return nil, err
	}

	err = binary.Read(r, binary.BigEndian, &length)
	if err != nil {
		return nil, err
	}

	var sample Sample
	switch format {
		// TODO  Lets add more as we go!
	default:
		rest := make([]byte, length)
		n, err := r.Read(rest)
		if err != nil {
			return nil, err
		}

		if n != int(length) {
			return nil, ErrSampleMalformedOrIncomplete
		}

		usample := UnknownSample(rest)
		sample = &usample
	}

	return sample, nil
}
