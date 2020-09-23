/*************************************************************************
 * Copyright 2019 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package ipexist

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"syscall"
	"unsafe"
)

const (
	maxMmapFileMapSize int64 = 0x100000000000  //16TB //we don't allow files to expand past 16TB, its a sanity check to ensure we don't clobber the 48 bits
	pageSize           int64 = 0x1000          //assuming a standard 4k page
	mmapFileAlign      int64 = pageSize - 1    //align on 4k pages
	minPumpSize              = 4 * 1024 * 1024 //always ask in 4MB chunks

	//mmap sync flags
	blockingSync    uintptr = 0x4
	nonblockingSync uintptr = 0x1

	//madv flags
	madv_normal     uintptr = 0
	madv_random     uintptr = 1
	madv_sequential uintptr = 2
	madv_dontdump   uintptr = 16
	madv_willneed   uintptr = 3

	AccessNormal     uintptr = 0
	AccessRandom     uintptr = 1
	AccessSequential uintptr = 2

	//memory map protection bits
	ProtRO uintptr = 0x1
	ProtWO uintptr = 0x2
	ProtRW uintptr = 0x3

	//memory map control bits
	MapShared  uintptr = 0x1
	MapPrivate uintptr = 0x2

	//some default startup flags
	startupFlags = MapShared //by default write back to file
	startupProt  = ProtRW
)

var (
	ErrInvalidFileHandle = errors.New("Invalid file handle")
	ErrMapClosed         = errors.New("File mapping closed")
	ErrOutsideOfBounds   = errors.New("Size is outside of file bounds")
	ErrFileTooLarge      = errors.New("Mapped file is too large")

	//make sure we don't allow this region to be dumped on a segfault
	startupAdvise []uintptr = []uintptr{
		0xa,  //advise don't fork MADV_DONTFORK
		0x10, //advise that this shouldn't dump MADV_DONTDUMP
	}
	preloadMadviseFlags = [2]uintptr{
		madv_willneed,
		madv_sequential,
	}
)

type FileMap struct {
	fmap
}

type fmap struct {
	r    region
	fio  *os.File
	Buff []byte
	open bool
}

// prepFileMap figures out how large of a memory map we should declare
// if the file size is not page aligned, we extend it so that it is
func prepFileMap(f *os.File) (sz int64, err error) {
	var fi os.FileInfo
	if f == nil {
		err = ErrInvalidFileHandle
		return
	} else if fi, err = f.Stat(); err != nil {
		return
	}
	sz = fi.Size()
	if (sz%pageSize) == 0 && sz != 0 {
		return //already aligned
	} else if sz == 0 {
		sz = pageSize
	}
	//how much do we need to add to make it page aligned
	nsz := alignedSize(sz)
	if err = f.Truncate(nsz); err == nil {
		sz = nsz
	}
	return
}

func alignedSize(sz int64) int64 {
	rem := sz % pageSize
	if rem == 0 {
		return sz
	}
	return sz + (pageSize - rem)
}

func MapFile(f *os.File) (*FileMap, error) {
	sz, err := prepFileMap(f)
	if err != nil {
		return nil, err
	}
	fptr := f.Fd()
	if fptr == 0 {
		return nil, ErrInvalidFileHandle
	}

	r, err := newRegion(fptr, uintptr(sz), uintptr(startupProt), uintptr(startupFlags))
	if err != nil {
		return nil, err
	}
	return &FileMap{
		fmap: fmap{
			r:    r,
			fio:  f,
			Buff: r.mp[0:sz],
			open: true,
		},
	}, nil
}

func (m *fmap) Close() error {
	if !m.open {
		return ErrMapClosed
	}
	if err := m.r.unmap(); err != nil {
		return err
	}
	m.Buff = nil
	m.open = false
	return nil
}

func (m *fmap) Expand() (err error) {
	fi, err := m.fio.Stat()
	if err != nil {
		return err
	}
	sz := fi.Size()
	if sz > maxMmapFileMapSize {
		return ErrFileTooLarge
	}
	if sz > int64(cap(m.r.mp)) {
		//going to have to remap the file
		nsz := alignedSize(sz)
		if err = m.r.remap(uintptr(nsz)); err != nil {
			return
		}
	}
	m.Buff = m.r.mp[0:sz]
	return
}

func (m *fmap) SetSize(sz int64) error {
	bsize := int64(len(m.Buff))
	if bsize >= sz {
		m.Buff = m.r.mp[0:sz]
		//good to go
		return nil
	}

	//we need to expand
	if err := m.Expand(); err != nil {
		return err
	}

	//recheck to see if its large enough
	bsize = int64(len(m.Buff))
	if bsize >= sz {
		m.Buff = m.r.mp[0:sz]
		//expansion got us covered
		return nil
	}
	//still not big enough, expand the file
	if err := m.fio.Truncate(alignedSize(sz)); err != nil {
		return err
	}
	if err := m.Expand(); err != nil {
		return err
	}
	bsize = int64(len(m.Buff))
	if bsize >= sz {
		m.Buff = m.r.mp[0:sz]
		//expansion got us covered
		return nil
	}
	return ErrOutsideOfBounds
}

func (m *fmap) Size() int64 {
	return int64(len(m.Buff))
}

func (m *fmap) PreloadFile() (err error) {
	return m.Preload(0, m.Size())
}

func (m *fmap) Preload(offset int64, sz int64) (err error) {
	//do some work without the lock set
	if sz < minPumpSize {
		sz = minPumpSize
	}
	//make sure we are page alligned
	mod := offset % pageSize
	offset -= mod
	if offset < 0 {
		offset = 0
	}
	sz += mod

	//check our size against the size of the buffer
	if (offset + sz) > int64(len(m.Buff)) {
		sz = int64(len(m.Buff)) - offset
	}
	p := uintptr(unsafe.Pointer(&m.Buff[offset]))
	if lerr := madvisePreload(p, uintptr(sz)); lerr != 0 {
		err = lerr
	}
	return
}

type region struct {
	mp   []byte
	sz   int64
	base uintptr
}

func newRegion(fd, sz, prot, flg uintptr) (r region, err error) {
	//ok, get our memory map
	if r.base, err = lin64mmap(0, sz, prot, flg, fd, 0); err != nil {
		fmt.Println("lin64mmap error", err)
		fmt.Println(sz, prot, flg, fd)
		return
	}
	dh := (*reflect.SliceHeader)(unsafe.Pointer(&r.mp))
	dh.Data = r.base
	dh.Len = int(sz)
	dh.Cap = dh.Len
	r.sz = int64(sz)
	return
}

func (r *region) remap(sz uintptr) (err error) {
	var addr uintptr
	old := uintptr(r.base)
	oldl := uintptr(r.sz)
	if addr, err = lin64mremap(old, oldl, sz); err == nil {
		dh := (*reflect.SliceHeader)(unsafe.Pointer(&r.mp))
		dh.Data = addr
		dh.Len = int(sz)
		dh.Cap = dh.Len
		r.sz = int64(sz)
		r.base = uintptr(addr)
	}
	return
}

func (r *region) madvise(flg uintptr) error {
	return lin64madvise(r.base, uintptr(r.sz), flg)
}

func (r *region) unmap() (err error) {
	if r.base == 0 {
		err = errors.New("already unmapped")
		return
	}
	if err = lin64munmap(r.base, uintptr(r.sz)); err == nil {
		r.base = 0
		r.sz = 0
		r.mp = nil
	}
	return
}

func (r *region) sync(flgs uintptr) (err error) {
	var errno syscall.Errno
	_, _, errno = syscall.Syscall(syscall.SYS_MSYNC, r.base, uintptr(r.sz), flgs)
	if errno != 0 {
		err = errors.New(errno.Error())
	}
	return
}

func lin64mmap(base, length, prot, flags, fd uintptr, offset int64) (addr uintptr, err error) {
	var errno syscall.Errno
	addr, _, errno = syscall.Syscall6(syscall.SYS_MMAP, base, length, prot, flags, fd, uintptr(offset))
	if errno != 0 {
		err = errors.New(errno.Error())
	}
	return
}

func lin64munmap(base, length uintptr) (err error) {
	var errno syscall.Errno
	if _, _, errno = syscall.Syscall(syscall.SYS_MUNMAP, base, length, 0); errno != 0 {
		err = errors.New(errno.Error())
	}
	return
}

func lin64mremap(old, old_len, new_len uintptr) (addr uintptr, err error) {
	flags := uintptr(1) //MREMAP_MAYMOVE
	var errno syscall.Errno
	addr, _, errno = syscall.Syscall6(syscall.SYS_MREMAP, old, old_len, new_len, flags, 0, 0)
	if errno != 0 {
		err = errors.New(errno.Error())
	}
	return
}

func lin64madvise(base, sz, flag uintptr) (err error) {
	var errno syscall.Errno
	_, _, errno = syscall.Syscall(syscall.SYS_MADVISE, base, sz, flag)
	if errno != 0 {
		err = errors.New(errno.Error())
	}
	return
}

func madvisePatternFlag(accesspattern uintptr) (uintptr, error) {
	switch accesspattern {
	case AccessNormal:
		return madv_normal, nil
	case AccessRandom:
		return madv_random, nil
	case AccessSequential:
		return madv_sequential, nil
	}
	return 0xffff, errors.New("Unknown pattern")
}

func translateMapFlag(flg uintptr) (v uintptr, err error) {
	switch flg {
	case MapShared:
		v = MapShared
	case MapPrivate:
		v = MapPrivate
	default:
		err = errors.New("Unknown map flag")
	}
	return
}

func madvisePreload(p uintptr, sz uintptr) (err syscall.Errno) {
	for _, v := range preloadMadviseFlags {
		if _, _, err = syscall.Syscall(syscall.SYS_MADVISE, p, uintptr(sz), v); err != 0 {
			break
		}
	}
	return
}
