/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package ipexist

import (
	"io/ioutil"
	"log"
	"net"
	"os"
	"testing"

	"github.com/bxcodec/faker/v3"
)

var (
	testDir  string
	reserved string = "34.5.66.70"
)

func TestMain(m *testing.M) {
	var err error
	if testDir, err = ioutil.TempDir(os.TempDir(), "mmap"); err != nil {
		log.Fatal("Failed to create temp dir", err)
	}

	r := m.Run()
	if err = os.RemoveAll(testDir); err != nil {
		log.Fatal("Failed to clean up", err)
	}
	os.Exit(r)
}

func TestBitmap(t *testing.T) {
	var b slash16bitmap
	var i uint16

	//check that nothing is set
	for {
		if b.isset(i) {
			t.Fatal("bad bit status", i)
		}
		i++
		if i == 0 {
			break
		}
	}

	//set everything
	i = 0
	for {
		b.set(i)
		i++
		if i == 0 {
			break
		}
	}

	//check that its set
	i = 0
	for {
		if !b.isset(i) {
			t.Fatal("bad bit status", i)
		}
		i++
		if i == 0 {
			break
		}
	}

	//clear everything
	i = 0
	for {
		b.clear(i)
		i++
		if i == 0 {
			break
		}
	}

	//check that nothing is set
	for {
		if b.isset(i) {
			t.Fatal("bad bit status", i)
		}
		i++
		if i == 0 {
			break
		}
	}
}

func TestEncodeDecode(t *testing.T) {
	f, err := ioutil.TempFile(testDir, "test")
	if err != nil {
		t.Fatal(err)
	}
	fname := f.Name()
	ip := NewIPBitMap()
	if err := ip.Encode(f); err != nil {
		t.Fatal(err)
	}

	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	//do it with the decoder
	if f, err = os.Open(fname); err != nil {
		t.Fatal(err)
	}

	if err = ip.Decode(f); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}

	//do it with the Load
	if f, err = os.Open(fname); err != nil {
		t.Fatal(err)
	}
	if ip, err = LoadIPBitMap(f); err != nil {
		t.Fatal(err)
	}
	if _, err = f.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	//check with a simple header check
	if err = CheckDecodeHeader(f); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestAdd(t *testing.T) {
	var ips []net.IP
	bm := NewIPBitMap()
	//build up our set
	for i := 0; i < 10000; i++ {
		ip := genIP()
		if err := bm.AddIP(ip); err != nil {
			t.Fatal(err)
		}
		ips = append(ips, ip)
	}

	//ensure we get good hits
	for i, ip := range ips {
		if ok, err := bm.IPExists(ip); err != nil {
			t.Fatal(err)
		} else if !ok {
			t.Fatal("IP missed", i, ip, len(ip), ip[0], ip[1])
		}
	}
	//encode to a file
	f, err := ioutil.TempFile(testDir, "test")
	if err != nil {
		t.Fatal(err)
	}
	fname := f.Name()
	if err := bm.Encode(f); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	//load it back up
	if f, err = os.Open(fname); err != nil {
		t.Fatal(err)
	}
	if bm, err = LoadIPBitMap(f); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	for i, ip := range ips {
		if ok, err := bm.IPExists(ip); err != nil {
			t.Fatal(err)
		} else if !ok {
			t.Fatal("IP missed", i, ip)
		}
	}
}

func TestRemove(t *testing.T) {
	var ips []net.IP
	bm := NewIPBitMap()
	//build up our set
	for i := 0; i < 10000; i++ {
		ip := genIP()
		if err := bm.AddIP(ip); err != nil {
			t.Fatal(err)
		}
		ips = append(ips, ip)
	}

	//ensure we get good hits
	for i, ip := range ips {
		if ok, err := bm.IPExists(ip); err != nil {
			t.Fatal(err)
		} else if !ok {
			t.Fatal("IP missed", i, ip, len(ip), ip[0], ip[1])
		}
	}
	//encode to a file
	f, err := ioutil.TempFile(testDir, "test")
	if err != nil {
		t.Fatal(err)
	}
	fname := f.Name()
	if err := bm.Encode(f); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	//load it back up
	if f, err = os.Open(fname); err != nil {
		t.Fatal(err)
	}
	if bm, err = LoadIPBitMap(f); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	for i, ip := range ips {
		if ok, err := bm.IPExists(ip); err != nil {
			t.Fatal(err)
		} else if !ok {
			t.Fatal("IP missed", i, ip)
		}
	}

	idxs := []int{5, 77, 200, 5000, 542, 700}
	for _, v := range idxs {
		if err = bm.RemoveIP(ips[v]); err != nil {
			t.Fatal(err)
		}
	}

	// attempt to remove the reserved ip, which should just be a non-error
	if err = bm.RemoveIP(net.ParseIP(reserved)); err != nil {
		t.Fatal(err)
	}

	//encode to a file
	f, err = ioutil.TempFile(testDir, "test")
	if err != nil {
		t.Fatal(err)
	}
	fname = f.Name()
	if err := bm.Encode(f); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	//load it back up
	if f, err = os.Open(fname); err != nil {
		t.Fatal(err)
	}
	if bm, err = LoadIPBitMap(f); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}

	for i, ip := range ips {
		if ok, err := bm.IPExists(ip); err != nil {
			t.Fatal(err)
		} else if !ok {
			var hit bool
			for _, v := range idxs {
				if ip.Equal(ips[v]) {
					hit = true
					break
				}
			}
			if !hit {
				t.Fatal("IP missed", i, ip)
			}
		}
	}
}

// All the same tests but now with memory mapped file backing
func TestEncodeDecodeMemoryMapped(t *testing.T) {
	mmn, err := getTempFileName()
	if err != nil {
		t.Fatal(err)
	}
	f, err := ioutil.TempFile(testDir, "test")
	if err != nil {
		t.Fatal(err)
	}
	fname := f.Name()
	ip, err := NewIPBitMapMemoryMapped(mmn)
	if err != nil {
		t.Fatal(err)
	}
	if err := ip.Encode(f); err != nil {
		t.Fatal(err)
	}

	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	//do it with the decoder
	if f, err = os.Open(fname); err != nil {
		t.Fatal(err)
	}

	if err = ip.Decode(f); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}

	//do it with the Load
	if f, err = os.Open(fname); err != nil {
		t.Fatal(err)
	}
	if mmn, err = getTempFileName(); err != nil {
		t.Fatal(err)
	}
	if ip, err = LoadIPBitMapMemoryMapped(f, mmn); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestAddMemoryMapped(t *testing.T) {
	mmn, err := getTempFileName()
	if err != nil {
		t.Fatal(err)
	}
	var ips []net.IP
	bm, err := NewIPBitMapMemoryMapped(mmn)
	if err != nil {
		t.Fatal(err)
	}
	//build up our set
	for i := 0; i < 10000; i++ {
		ip := genIP()
		if err := bm.AddIP(ip); err != nil {
			t.Fatal(err)
		}
		ips = append(ips, ip)
	}

	//ensure we get good hits
	for i, ip := range ips {
		if ok, err := bm.IPExists(ip); err != nil {
			t.Fatal(err)
		} else if !ok {
			t.Fatal("IP missed", i, ip, len(ip), ip[0], ip[1])
		}
	}
	//encode to a file
	f, err := ioutil.TempFile(testDir, "test")
	if err != nil {
		t.Fatal(err)
	}
	fname := f.Name()
	if err := bm.Encode(f); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	//load it back up
	if f, err = os.Open(fname); err != nil {
		t.Fatal(err)
	}
	if bm, err = LoadIPBitMapMemoryMapped(f, mmn); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	for i, ip := range ips {
		if ok, err := bm.IPExists(ip); err != nil {
			t.Fatal(err)
		} else if !ok {
			t.Fatal("IP missed", i, ip)
		}
	}
}

func BenchmarkTest(b *testing.B) {
	var ips []net.IP
	bm := NewIPBitMap()
	//build up our set
	for i := 0; i < 10000; i++ {
		ip := genIP()
		if (i & 1) == 0 {
			if err := bm.AddIP(ip); err != nil {
				b.Fatal(err)
			}
		}
		ips = append(ips, ip)
	}
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		idx := n % len(ips)
		if ok, err := bm.IPExists(ips[idx]); err != nil {
			b.Fatal(err)
		} else if ok != ((idx & 1) == 0) {
			b.Fatal("Bad hit")
		}
	}
}

func BenchmarkTestMemoryMapped(b *testing.B) {
	mmn, err := getTempFileName()
	if err != nil {
		b.Fatal(err)
	}
	var ips []net.IP
	bm, err := NewIPBitMapMemoryMapped(mmn)
	if err != nil {
		b.Fatal(err)
	}
	//build up our set
	for i := 0; i < 10000; i++ {
		ip := genIP()
		if (i & 1) == 0 {
			if err := bm.AddIP(ip); err != nil {
				b.Fatal(err)
			}
		}
		ips = append(ips, ip)
	}
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		idx := n % len(ips)
		if ok, err := bm.IPExists(ips[idx]); err != nil {
			b.Fatal(err)
		} else if ok != ((idx & 1) == 0) {
			b.Fatal("Bad hit")
		}
	}
}

func genIP() (ip net.IP) {
	for {
		if ip = net.ParseIP(faker.IPv4()); ip == nil {
			continue
		}
		if ip = ip.To4(); ip == nil {
			continue
		}
		if ip.IsLoopback() || ip.IsMulticast() || (ip[0] == 0xff && ip[1] == 0xff) {
			continue
		}
		if ip.Equal(net.ParseIP(reserved)) {
			continue
		}
		break
	}
	return
}

func getTempFileName() (s string, err error) {
	var f *os.File
	if f, err = ioutil.TempFile(testDir, "mm"); err != nil {
		return
	}
	s = f.Name()
	err = f.Close()
	return
}
