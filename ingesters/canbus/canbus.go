/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"

	"golang.org/x/sys/unix"
)

const (
	minBuff      int = 16
	rechargeSize int = 1024 * 1024
)

var (
	ErrFailedPacketRead = errors.New("Failed to read complete CAN packet")
)

type Cansock struct {
	fd   int
	sock *unix.SockaddrCAN
	buff []byte
}

// Create a new CAN device bound to device such as can0 or vcan0
func New(dev string) (*Cansock, error) {
	//create the socket
	fd, err := unix.Socket(unix.AF_CAN, unix.SOCK_RAW, unix.CAN_RAW)
	if err != nil {
		return nil, err
	}
	//get the interface for indexing
	iface, err := net.InterfaceByName(dev)
	if err != nil {
		unix.Close(fd)
		return nil, err
	}
	addr := &unix.SockaddrCAN{Ifindex: iface.Index}
	//actually bind the socket to the interface
	if err := unix.Bind(fd, addr); err != nil {
		unix.Close(fd)
		return nil, err
	}

	return &Cansock{
		fd:   fd,
		sock: &unix.SockaddrCAN{Ifindex: iface.Index},
	}, nil
}

func (c *Cansock) Close() error {
	if c.fd == 0 || c.sock == nil {
		return nil
	}
	if err := unix.Close(c.fd); err != nil {
		return err
	}
	c.fd = 0
	c.sock = nil
	return nil
}

func (c *Cansock) Read() ([]byte, error) {
	if len(c.buff) < minBuff {
		c.buff = make([]byte, rechargeSize)
	}
	n, err := unix.Read(c.fd, c.buff[:minBuff])
	if err != nil {
		return nil, err
	}
	r := c.buff[:n]
	c.buff = c.buff[n:]

	pkt, err := packPacket(r)
	if err != nil {
		return nil, err
	}
	return pkt, nil
}

func packPacket(r []byte) (pkt []byte, err error) {
	//ensure incoming packet has the correct size
	if len(r) != minBuff {
		err = ErrFailedPacketRead
		return
	}
	pkt = r
	//rectify the packet based on data length (we basically just chop off the CRC)
	canlen := r[4] & 0x0f
	for i := 0; i < int(canlen); i++ {
		pkt[5+i] = pkt[8+i]
	}
	pkt = pkt[:5+canlen]
	return
}

type CanPacket struct {
	ID       uint32
	Extended bool
	RTR      bool
	Data     []byte
}

func ExtractPacket(d []byte) (pkt CanPacket, err error) {
	//we need enough to get the address, length and bitfield out
	if len(d) < 5 {
		err = ErrInvalidPacket
		return
	}
	//pull the length field
	l := d[4] & 0xf
	if len(d) < int(5+l) {
		err = ErrInvalidPacket
		return
	}
	pkt.Data = d[5:]
	pkt.Extended = (d[3]&0x80 == 0x80)
	pkt.RTR = (d[3]&0x40 == 0x40)
	if !pkt.Extended {
		pkt.ID = binary.LittleEndian.Uint32(d[:4]) & unix.CAN_SFF_MASK
	} else {
		pkt.ID = binary.LittleEndian.Uint32(d[:4]) & unix.CAN_EFF_MASK
	}
	return
}

func (cp CanPacket) String() string {
	str := fmt.Sprintf("%.08x", cp.ID)
	if cp.RTR {
		str += " RTR"
	}
	return fmt.Sprintf("%s %x", str, cp.Data)
}
