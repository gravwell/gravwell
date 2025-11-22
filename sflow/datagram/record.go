package datagram

import "errors"

var (
	ErrUnknownRecordType = errors.New("record has unknown data format")
)

type Record interface {
	// See https://sflow.org/sflow_version_5.txt, pag 25, `Data Format`
	//
	// The DataFormat uniquely identifies the format of an opaque structure in the sFlow specification.
	GetDataFormat() uint32
}
