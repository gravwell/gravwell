/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package kits

import (
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"os"
	"testing"

	"github.com/gravwell/gravwell/v3/ingesters/utils"
)

var (
	defCfg = BuilderConfig{
		Version:     0,
		Name:        `test`,
		Description: `test package`,
		ID:          `io.gravwell.testpackage`,
	}
)

func TestKitNewBuilder(t *testing.T) {
	tf, err := ioutil.TempFile(baseDir, `kit`)
	if err != nil {
		t.Fatal(err)
	}
	//ensure it bombs with a bad version
	pb, err := NewBuilder(defCfg, tf)
	if err != nil {
		t.Fatal(err)
	}
	if pb.manifest.Version != Version {
		t.Fatal("Failed to init default version")
	}
	if err := pb.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestBuilderAbort(t *testing.T) {
	tf, err := ioutil.TempFile(baseDir, `kit`)
	if err != nil {
		t.Fatal(err)
	}
	pb, err := NewBuilder(defCfg, tf)
	if err != nil {
		t.Fatal(err)
	}
	if pb.manifest.Version != Version {
		t.Fatal("Failed to init default version")
	}
	if err = pb.Abort(); err != nil {
		t.Fatal(err)
	}
}

func TestBuilderAdd(t *testing.T) {
	tf, err := ioutil.TempFile(baseDir, `kit`)
	if err != nil {
		t.Fatal(err)
	}
	c := defCfg
	c.Version = 4
	pb, err := NewBuilder(c, tf)
	if err != nil {
		t.Fatal(err)
	}
	if pb.manifest.Version != 4 {
		t.Fatal("Failed to set version")
	}
	b, err := genRandomBuff()
	if err != nil {
		t.Fatal(err)
	}
	if err = pb.Add(`test1`, Resource, b); err != nil {
		pb.Abort()
		t.Fatal(err)
	}
	f, err := genRandomFile()
	if err != nil {
		t.Fatal(err)
	}
	if err = pb.AddFile(`test2`, Resource, f); err != nil {
		pb.Abort()
		f.Close()
		t.Fatal(err)
	}
	//write without manifest signature
	if err = pb.WriteManifest(nil); err != nil {
		pb.Abort()
		t.Fatal(err)
	}
	if err := pb.Close(); err != nil {
		t.Fatal(err)
	}
	fin, err := utils.OpenFileReader(tf.Name())
	if err != nil {
		t.Fatal(err)
	}
	//open and verify with no key
	pr, err := NewReader(fin, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = pr.Verify(); err != nil {
		t.Fatal(err)
	}
	if !testing.Short() {
		if pr, err = NewReader(fin, nil); err != nil {
			t.Fatal(err)
		}

		//close the reader
		if err = fin.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if err = os.Remove(tf.Name()); err != nil {
		t.Fatal(err)
	}
}

func TestBuilderAddSigned(t *testing.T) {
	if testing.Short() {
		return
	}
	tf, err := ioutil.TempFile(baseDir, `kit`)
	if err != nil {
		t.Fatal(err)
	}
	c := defCfg
	c.Version = 4

	pb, err := NewBuilder(c, tf)
	if err != nil {
		t.Fatal(err)
	}
	if pb.manifest.Version != 4 {
		t.Fatal("Failed to set version")
	}
	b, err := genRandomBuff()
	if err != nil {
		t.Fatal(err)
	}
	if err = pb.Add(`test1`, Resource, b); err != nil {
		pb.Abort()
		t.Fatal(err)
	}
	f, err := genRandomFile()
	if err != nil {
		t.Fatal(err)
	}
	if err = pb.AddFile(`test2`, Resource, f); err != nil {
		pb.Abort()
		f.Close()
		t.Fatal(err)
	}
	//write with manifest signature
	if err = pb.WriteManifest(nil); err != nil {
		pb.Abort()
		t.Fatal(err)
	}
	if err := pb.Close(); err != nil {
		t.Fatal(err)
	}

	if err = gzipCompressFile(tf.Name()); err != nil {
		t.Fatal(err)
	}

	fin, err := utils.OpenFileReader(tf.Name())
	if err != nil {
		t.Fatal(err)
	}
	//open and verify with no key
	pr, err := NewReader(fin, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = pr.Verify(); err != nil {
		t.Fatal(err)
	}
	if pr, err = NewReader(fin, nil); err != nil {
		t.Fatal(err)
	}
	if err = pr.Verify(); err != nil {
		t.Fatal(err)
	}
	//close the reader
	if err = fin.Close(); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(tf.Name()); err != nil {
		t.Fatal(err)
	}
}

type tstruct struct {
	tp  ItemType
	hsh [sha256.Size]byte
}

func TestProcess(t *testing.T) {
	tf, err := ioutil.TempFile(baseDir, `kit`)
	if err != nil {
		t.Fatal(err)
	}
	c := defCfg
	c.Version = 4
	pb, err := NewBuilder(c, tf)
	if err != nil {
		t.Fatal(err)
	}
	if pb.manifest.Version != 4 {
		t.Fatal("Failed to set version")
	}
	b, err := genRandomBuff()
	if err != nil {
		t.Fatal(err)
	}
	if err = pb.Add(`test1`, Resource, b); err != nil {
		pb.Abort()
		t.Fatal(err)
	}
	f, err := genRandomFile()
	if err != nil {
		t.Fatal(err)
	}
	if err = pb.AddFile(`test2`, Resource, f); err != nil {
		pb.Abort()
		f.Close()
		t.Fatal(err)
	}
	f2, err := genRandomFile()
	if err != nil {
		t.Fatal(err)
	}
	if err = pb.AddFile(`testlic`, License, f2); err != nil {
		pb.Abort()
		f2.Close()
		t.Fatal(err)
	}
	//write without manifest signature
	if err = pb.WriteManifest(nil); err != nil {
		pb.Abort()
		t.Fatal(err)
	}
	if err := pb.Close(); err != nil {
		t.Fatal(err)
	}
	fin, err := utils.OpenFileReader(tf.Name())
	if err != nil {
		t.Fatal(err)
	}
	//open and verify with no key
	pr, err := NewReader(fin, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = pr.Verify(); err != nil {
		t.Fatal(err)
	}
	mp := map[string]tstruct{}
	mp[`test1`] = tstruct{
		tp:  Resource,
		hsh: GetHash(b),
	}
	hsh, _ := getReaderHash(f)
	mp[`test2`] = tstruct{
		tp:  Resource,
		hsh: hsh,
	}
	hsh, _ = getReaderHash(f2)
	mp[`testlic`] = tstruct{
		tp:  License,
		hsh: hsh,
	}

	if err = pr.Process(func(n string, tp ItemType, hash [sha256.Size]byte, rdr io.Reader) error {
		if v, ok := mp[n]; !ok {
			return fmt.Errorf("Unknown resource: %v", n)
		} else if v.tp != tp {
			return fmt.Errorf("type mismatch: %v != %v", tp, v.tp)
		} else if v.hsh != getReaderTestHash(rdr) {
			return fmt.Errorf("hash missmatch: %v", v.hsh)
		}
		delete(mp, n)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	//close the reader
	if err = fin.Close(); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(tf.Name()); err != nil {
		t.Fatal(err)
	}
	if len(mp) > 0 {
		t.Fatal("leftover resources")
	}
}

func genRandomFile() (fout *os.File, err error) {
	var b []byte
	if b, err = genRandomBuff(); err != nil {
		return
	}
	if fout, err = ioutil.TempFile(baseDir, "t"); err != nil {
		return
	}
	if _, err = rand.Read(b); err != nil {
		fout.Close()
		return
	}
	if err = writeAll(fout, b); err != nil {
		fout.Close()
		return
	}
	return
}

func genRandomBuff() (b []byte, err error) {
	b = make([]byte, 32*1024)
	if _, err = rand.Read(b); err != nil {
		return
	}
	return
}

func getReaderTestHash(rdr io.Reader) (hsh [sha256.Size]byte) {
	//generate hash
	h := sha256.New()
	if _, err := io.Copy(h, rdr); err != nil {
		return
	}
	if bts := h.Sum(nil); len(bts) == sha256.Size {
		copy(hsh[0:sha256.Size], bts)
	}
	return
}

func gzipCompressFile(p string) (err error) {
	var fout *os.File
	var fin *os.File
	if fin, err = os.Open(p); err != nil {
		return
	}
	if fout, err = os.Create(p + `.gz`); err != nil {
		fin.Close()
		return
	}
	wtr := gzip.NewWriter(fout)
	if _, err = io.Copy(wtr, fin); err != nil {
		fin.Close()
		fout.Close()
		return
	}

	if err = fin.Close(); err != nil {
		fout.Close()
		return
	}

	if err = wtr.Flush(); err == nil {
		if err = wtr.Close(); err == nil {
			err = os.Rename(p+`.gz`, p)
		}
	}
	return
}
