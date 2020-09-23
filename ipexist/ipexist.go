/*************************************************************************
 * Copyright 2019 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package ipexist

import (
	"compress/flate"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"os"
	"reflect"
	"unsafe"
)

const (
	flateLevel        int   = 8
	slash16bitmapSize int64 = 1024 * 8
	maxMaps                 = 0xfffe
)

var (
	ErrInvalidIPv4       = errors.New("Invalid IPv4 Address")
	ErrInvalidBaseOffset = errors.New("Invalid IPv4 base offset, potential corruption")
	ErrNotMmapBacked     = errors.New("IPBitMap is not backed by a memory map")
)

var (
	compV1Header = []byte{0x49, 0x50, 0x76, 0x34, 0x46, 0x4c, 0x54, 0x31} //IPv4FLT1
)

type slash16bitmap [1024]uint64

type IpBitMap struct {
	maxOffset     uint16
	bitmapOffsets [0xffff]uint16
	bitmaps       []slash16bitmap
	mmapBacked    bool
	mm            mmapBacker
}

type mmapBacker struct {
	f  *os.File
	fm *FileMap
}

func NewIPBitMap() *IpBitMap {
	return &IpBitMap{}
}

func LoadIPBitMap(r io.Reader) (*IpBitMap, error) {
	x := NewIPBitMap()
	if err := x.Decode(r); err != nil {
		return nil, err
	}
	return x, nil
}

func NewIPBitMapMemoryMapped(p string) (ipm *IpBitMap, err error) {
	ipm = &IpBitMap{}
	if ipm.mm, err = newMmapBacker(p); err != nil {
		ipm = nil
		return
	}
	ipm.mmapBacked = true
	return
}

func LoadIPBitMapMemoryMapped(r io.Reader, p string) (*IpBitMap, error) {
	x, err := NewIPBitMapMemoryMapped(p)
	if err != nil {
		return nil, err
	}
	if err = x.Decode(r); err != nil {
		return nil, err
	}
	return x, nil
}

func (ipbm *IpBitMap) Close() (err error) {
	ipbm.bitmaps = nil
	for i := 0; i < 0xffff; i++ {
		ipbm.bitmapOffsets[i] = 0
	}
	if ipbm.mmapBacked {
		err = ipbm.mm.Close()
		ipbm.mmapBacked = false
	}
	return
}

func (ipbm *IpBitMap) AddIP(ip net.IP) (err error) {
	if ip == nil {
		err = ErrInvalidIPv4
		return
	} else if ip = ip.To4(); len(ip) != 4 {
		err = ErrInvalidIPv4
		return
	}
	//read the upper 2 octets
	upper := binary.BigEndian.Uint16(ip[0:2])
	if upper == 0xffff {
		return // we do not support broadcast
	}
	//read the bottom two octets
	lower := binary.BigEndian.Uint16(ip[2:4])
	off := ipbm.bitmapOffsets[upper]
	if off == 0 {
		//get a new one
		if off, err = ipbm.addNewBitmap(); err != nil {
			return
		}
		//assign it
		ipbm.bitmapOffsets[upper] = off
	}
	if off == 0xFFFF {
		return
	} else if off > ipbm.maxOffset {
		err = ErrInvalidBaseOffset
	}
	ipbm.bitmaps[off-1].set(lower) //zero is special, we are starting from 1
	return
}

func (ipbm *IpBitMap) IPExists(ip net.IP) (ok bool, err error) {
	if ip == nil {
		err = ErrInvalidIPv4
		return
	} else if ip = ip.To4(); len(ip) != 4 {
		err = ErrInvalidIPv4
		return
	}
	//read the upper 2 octets
	upper := binary.BigEndian.Uint16(ip[0:2])
	if upper == 0xFFFF {
		return //we don't support broadcast
	}
	if off := ipbm.bitmapOffsets[upper]; off > 0 {
		if off == 0xffff {
			ok = true
		} else if off > ipbm.maxOffset {
			err = ErrInvalidBaseOffset
		} else {
			//read the bottom two octets
			lower := binary.BigEndian.Uint16(ip[2:4])
			//zero is special, we are starting from 1
			ok = ipbm.bitmaps[off-1].isset(lower)
		}
	}
	return
}

func (ipbm *IpBitMap) addNewBitmap() (off uint16, err error) {
	if len(ipbm.bitmaps) >= maxMaps {
		err = errors.New("Maps exhausted")
	} else {
		if ipbm.mmapBacked {
			if err = ipbm.addMmapBackedBitmap(); err != nil {
				return
			}
		} else {
			ipbm.bitmaps = append(ipbm.bitmaps, [1024]uint64{})
		}
		off = uint16(len(ipbm.bitmaps))
		ipbm.maxOffset = off
	}
	return
}

func (ipbm *IpBitMap) Encode(w io.Writer) (err error) {
	var fw *flate.Writer
	//write the header
	if err = writeAll(w, compV1Header); err != nil {
		return
	}
	//write the bitmap slice count
	x := make([]byte, 8)
	binary.LittleEndian.PutUint64(x, uint64(len(ipbm.bitmaps)))
	if err = writeAll(w, x); err != nil {
		return
	}

	//get a new flate writer
	if fw, err = flate.NewWriter(w, flateLevel); err != nil {
		return
	}
	//write the bitmap offsets
	if err = binary.Write(fw, binary.LittleEndian, ipbm.bitmapOffsets); err != nil {
		return
	}

	//write the bitmaps
	for i := range ipbm.bitmaps {
		if err = binary.Write(fw, binary.LittleEndian, ipbm.bitmaps[i]); err != nil {
			return
		}
	}
	if err = fw.Flush(); err != nil {
		return
	}

	return nil
}

func CheckDecodeHeader(r io.Reader) (err error) {
	var cnt uint64
	//write the header
	if err = checkHeader(r); err != nil {
		return
	}
	//get the slice count
	if cnt, err = readUint64(r); err != nil {
		return
	}
	if cnt > maxMaps {
		err = errors.New("file is corrupt")
	}
	return
}

func (ipbm *IpBitMap) Decode(r io.Reader) (err error) {
	var fr io.ReadCloser
	var cnt uint64
	//write the header
	if err = checkHeader(r); err != nil {
		return
	}
	//get the slice count
	if cnt, err = readUint64(r); err != nil {
		return
	}
	if cnt > maxMaps {
		err = errors.New("file is corrupt")
		return
	}
	//get a new flate Reader
	fr = flate.NewReader(r)
	//read the bitmap offsets
	if err = binary.Read(fr, binary.LittleEndian, &ipbm.bitmapOffsets); err != nil {
		return
	}

	//read the bitmaps
	if ipbm.mmapBacked {
		if err = ipbm.allocateMmapBitmaps(uint16(cnt)); err != nil {
			return
		}
	} else {
		ipbm.bitmaps = make([]slash16bitmap, cnt)
	}
	for i := range ipbm.bitmaps {
		if err = binary.Read(fr, binary.LittleEndian, &ipbm.bitmaps[i]); err != nil {
			return
		}
	}
	ipbm.maxOffset = uint16(len(ipbm.bitmaps))

	if len(ipbm.bitmaps) != int(cnt) {
		err = errors.New("bitmaps are corrupt")
	}
	return nil
}

// addMmapBackedBitmap will extend the memory mapped file and get the
// the space allocated, this may swap out the backing memory for the bitmap
// slice, so no body better be holding a reference on it
func (ipbm *IpBitMap) addMmapBackedBitmap() (err error) {
	if !ipbm.mmapBacked {
		err = ErrNotMmapBacked
		return
	} else if ipbm.maxOffset == maxMaps {
		err = errors.New("Invalid bitmap count")
		return
	}
	//figure out how big the file SHOULD be
	l := len(ipbm.bitmaps) + 1
	sz := int64(l) * slash16bitmapSize
	if err = ipbm.mm.fm.SetSize(sz); err != nil {
		return err
	}
	//ok, now use reflection to setup our slice
	hdr := (*reflect.SliceHeader)(unsafe.Pointer(&ipbm.bitmaps))
	hdr.Len = l
	hdr.Cap = l
	hdr.Data = uintptr(unsafe.Pointer(&ipbm.mm.fm.Buff[0]))
	return
}

func (ipbm *IpBitMap) allocateMmapBitmaps(cnt uint16) (err error) {
	if !ipbm.mmapBacked {
		err = ErrNotMmapBacked
		return
	} else if cnt > maxMaps {
		err = errors.New("Invalid bitmap count")
		return
	} else if cnt == 0 {
		return
	}
	l := int(cnt)
	sz := int64(l) * slash16bitmapSize
	if err = ipbm.mm.fm.SetSize(sz); err != nil {
		return err
	}
	//ok, now use reflection to setup our slice
	hdr := (*reflect.SliceHeader)(unsafe.Pointer(&ipbm.bitmaps))
	hdr.Len = l
	hdr.Cap = l
	hdr.Data = uintptr(unsafe.Pointer(&ipbm.mm.fm.Buff[0]))
	return
}

func (b *slash16bitmap) set(v uint16) {
	//get the bit offset into the field
	boff := uint64(1 << (v & 0x3f))

	//set the bit
	b[v>>6] |= boff
}

func (b *slash16bitmap) clear(v uint16) {
	//get the bit offset into the field
	boff := uint64(1 << (v & 0x3f))

	//set the bit
	b[v>>6] ^= boff
}

func (b *slash16bitmap) isset(v uint16) bool {
	//offset into the bitmap
	//get the bit offset into the field
	boff := uint64(1 << (v & 0x3f))

	//set the bit
	return (b[v>>6] & boff) != 0
}

func writeAll(w io.Writer, b []byte) (err error) {
	var n int
	if n, err = w.Write(b); err == nil {
		if n != len(b) {
			err = errors.New("failed to write buffer")
		}
	}
	return
}

func checkHeader(r io.Reader) (err error) {
	var n int
	x := make([]byte, len(compV1Header))
	if n, err = r.Read(x); err != nil {
		return
	} else if n != len(x) {
		return errors.New("failed header read")
	}
	for i := range x {
		if x[i] != compV1Header[i] {
			err = errors.New("Bad header")
			break
		}
	}
	return
}

func readUint64(r io.Reader) (v uint64, err error) {
	var n int
	x := make([]byte, 8)
	if n, err = r.Read(x); err != nil {
		return
	} else if n != len(x) {
		err = errors.New("failed read")
	} else {
		v = binary.LittleEndian.Uint64(x)
	}
	return
}

func newMmapBacker(p string) (mb mmapBacker, err error) {
	if mb.f, err = os.OpenFile(p, os.O_RDWR|os.O_CREATE, 0660); err != nil {
		return
	}
	if mb.fm, err = MapFile(mb.f); err != nil {
		mb.f.Close()
	}
	return
}

func (mb *mmapBacker) Close() (err error) {
	if mb.f == nil {
		return
	}
	if err = mb.fm.Close(); err != nil {
		mb.f.Close()
		return
	}
	nm := mb.f.Name()
	if err = mb.f.Close(); err == nil {
		err = os.Remove(nm)
	} else {
		os.Remove(nm)
	}
	mb.f = nil
	mb.fm = nil
	return
}
