/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package rotate

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

type extTest struct {
	v    string
	base string
	ext  string
	ok   bool
}

func TestGetExt(t *testing.T) {
	tests := []extTest{
		extTest{v: `test.log`, base: `test`, ext: `.log`, ok: true},
		extTest{v: `test`, base: `test`, ext: ``, ok: false},
		extTest{v: `test.log.gz`, base: `test`, ext: `.log.gz`, ok: true},
		extTest{v: `test.`, base: `test`, ext: `.`, ok: true},
		extTest{v: `test.gz`, base: `test`, ext: `.gz`, ok: true},
		extTest{v: `test_foobar.gz.1.log`, base: `test_foobar.gz.1`, ext: `.log`, ok: true},
		extTest{v: `test_foobar.gz.1.log.gz`, base: `test_foobar.gz.1`, ext: `.log.gz`, ok: true},
	}

	for _, v := range tests {
		base, ext, ok := getExt(v.v)
		if ok != v.ok {
			t.Fatalf("ERROR: %v: %v != %v", v.v, v.ok, ok)
		} else if !ok {
			continue
		}
		if v.ext != ext {
			t.Fatalf("ERROR, invalid extension: %v: %v != %v", v.v, v.ext, ext)
		} else if v.base != base {
			t.Fatalf("ERROR, invalid base: %v: %v != %v", v.v, v.base, base)
		}
	}
}

func TestResolveHistoryFile(t *testing.T) {
	base := `/var/log/foobar`
	tests := []historyFile{
		historyFile{base: base, orig: `test.log`, baseName: `test`, ext: `.log`, historyID: 0},
		historyFile{base: base, orig: `test.1.log`, baseName: `test`, ext: `.log`, historyID: 1},
		historyFile{base: base, orig: `test.1.log.gz`, baseName: `test`, ext: `.log.gz`, historyID: 1},
		historyFile{base: base, orig: `test.99.log.gz`, baseName: `test`, ext: `.log.gz`, historyID: 99},
		historyFile{base: base, orig: `test.ZZ.log`, baseName: `test.ZZ`, ext: `.log`, historyID: 0},
		historyFile{base: base, orig: `test99.log`, baseName: `test99`, ext: `.log`, historyID: 0},
		historyFile{base: base, orig: `test99.log.gz`, baseName: `test99`, ext: `.log.gz`, historyID: 0},
		historyFile{base: base, orig: `test_99.log.gz`, baseName: `test_99`, ext: `.log.gz`, historyID: 0},
		historyFile{base: base, orig: `test_99.99.log.gz`, baseName: `test_99`, ext: `.log.gz`, historyID: 99},
	}

	for _, v := range tests {
		h, ok := resolveHistory(base, v.orig)
		if !ok {
			t.Fatalf("Failed to resolve historyFile on %v", v.orig)
		} else if !reflect.DeepEqual(v, h) {
			t.Fatalf("resolveHistory failed on %v\n\t%+v\n\t%+v", v.orig, v, h)
		} else if og := filepath.Join(base, v.orig); og != h.path() {
			t.Fatalf("invalid output path: %v ! %v", og, h.path())
		} else if v.orig != h.name() {
			t.Fatalf("invalid output name: %v != %v", og, h.name())
		}
	}

	x := historyFile{base: base, orig: `test.log`, baseName: `test`, ext: `.log`, historyID: 0}
	if x.name() != x.orig {
		t.Fatal(x.name(), x.orig)
	}

	x.historyID = 12345
	x.ext = `.log.gz`
	if x.name() != `test.12345.log.gz` {
		t.Fatal(x.name())
	}
}

func TestNewWriter(t *testing.T) {
	//test with a path that ends in slash
	if _, err := Open("./foobar", 0660); err == nil {
		t.Fatal("failed to catch no extension")
	} else if _, err := Open("./foobar/", 0660); err == nil {
		t.Fatal("Failed to catch directory filepath")
	}

	base := t.TempDir()
	pth := filepath.Join(base, `testing.log`)

	fout, err := Open(pth, 0660)
	if err != nil {
		t.Fatal(err)
	} else if err = fout.Close(); err != nil {
		t.Fatal(err)
	}

	//open it back up
	if fout, err = Open(pth, 0660); err != nil {
		t.Fatal(err)
	} else if err = dropLines(fout, 64); err != nil {
		t.Fatal(err)
	} else if err = fout.Close(); err != nil {
		t.Fatal(err)
	}
	cnt, err := countFileLines(pth)
	if err != nil {
		t.Fatal(err)
	} else if cnt != 64 {
		t.Fatalf("invalid line count: %v != 64", cnt)
	}

	if fout, err = Open(pth, 0660); err != nil {
		t.Fatal(err)
	} else if err = dropLines(fout, 64); err != nil {
		t.Fatal(err)
	} else if err = fout.Close(); err != nil {
		t.Fatal(err)
	}
	if cnt, err = countFileLines(pth); err != nil {
		t.Fatal(err)
	} else if cnt != 128 {
		t.Fatalf("invalid line count: %v != 128", cnt)
	}
}

func TestNewWriterWithRotate(t *testing.T) {
	base := t.TempDir()

	pth := filepath.Join(base, `testing.log`)

	var lines int
	fout, err := Open(pth, 0660)
	if err != nil {
		t.Fatal(err)
	} else if lines, err = dropLineBytes(fout, 256*1024); err != nil {
		t.Fatal(err)
	} else if err = fout.Close(); err != nil {
		t.Fatal(err)
	}

	//open it back up with the advanced open API
	if fout, err = OpenEx(pth, 0660, 128*1024, 3, false); err != nil {
		t.Fatal(err)
	} else if err = dropLines(fout, 32); err != nil {
		t.Fatal(err)
	} else if err = fout.Close(); err != nil {
		t.Fatal(err)
	}

	if cnt, err := countFileLines(pth); err != nil {
		t.Fatal(err)
	} else if cnt != 32 {
		t.Fatalf("invalid line count: %v != 32", cnt)
	}

	//check the rotated file
	if cnt, err := countFileLines(filepath.Join(base, `testing.1.log`)); err != nil {
		t.Fatal(err)
	} else if cnt != lines {
		t.Fatalf("invalid line count: %v != %v", cnt, lines)
	}

	//open it back up but asking for things to be compressed
	var lines2 int
	if fout, err = OpenEx(pth, 0660, 128*1024, 3, true); err != nil {
		t.Fatal(err)
	} else if lines2, err = dropLineBytes(fout, 128*1024); err != nil {
		t.Fatal(err)
	} else if err = fout.Close(); err != nil {
		t.Fatal(err)
	}

	//check the rotated file
	if cnt, err := countFileLines(filepath.Join(base, `testing.2.log`)); err != nil {
		t.Fatal(err)
	} else if cnt != lines {
		t.Fatalf("invalid line count: %v != %v", cnt, lines)
	}

	//check the old current
	if old1, err := countFileLines(filepath.Join(base, `testing.1.log.gz`)); err != nil {
		t.Fatal(err)
	} else if curr, err := countFileLines(filepath.Join(base, `testing.log`)); err != nil {
		t.Fatal(err)
	} else if (old1 + curr) != (lines2 + 32) {
		t.Fatalf("Invalid line count after hot rotation: %v != %v\n\t(%d %d) != (%d + 32)", (old1 + curr), lines2+32, old1, curr, lines2)
	}
}

func TestWithWriterRotateDelete(t *testing.T) {
	base := t.TempDir()
	pth := filepath.Join(base, `testing.log`)

	//create some random files
	randomFiles := createRandomFiles(base, `testing.log`)

	fout, err := OpenEx(pth, 0660, 32*1024, 3, true)
	if err != nil {
		t.Fatal(err)
	} else if _, err = dropLineBytes(fout, 256*1024); err != nil {
		t.Fatal(err)
	} else if err = fout.Close(); err != nil {
		t.Fatal(err)
	}

	// there should be 3 files with > 0 lines
	exist := []string{
		pth,
		filepath.Join(base, `testing.1.log.gz`),
		filepath.Join(base, `testing.2.log.gz`),
		filepath.Join(base, `testing.3.log.gz`),
	}
	exist = append(exist, randomFiles...) //these should be untouched

	//and 2-3 more that should not exist at all
	noExist := []string{
		filepath.Join(base, `testing.4.log.gz`),
		filepath.Join(base, `testing.5.log.gz`),
		filepath.Join(base, `testing.6.log.gz`),
	}

	for _, e := range exist {
		if cnt, err := countFileLines(e); err != nil {
			t.Fatal(err)
		} else if cnt <= 0 {
			t.Fatal(cnt)
		}
	}

	for _, ne := range noExist {
		if fi, err := os.Stat(ne); err == nil || !os.IsNotExist(err) {
			t.Fatalf("file either exists or something else broken on %v: %v %v", ne, err, fi)
		}
	}
}

func TestNoExtension(t *testing.T) {
	base := t.TempDir()
	pth := filepath.Join(base, `testing.nocomp.log`)

	//create some random files
	randomFiles := createRandomFiles(base, `testing.nocomp`)

	fout, err := OpenEx(pth, 0660, 32*1024, 3, true)
	if err != nil {
		t.Fatal(err)
	} else if _, err = dropLineBytes(fout, 256*1024); err != nil {
		t.Fatal(err)
	} else if err = fout.Close(); err != nil {
		t.Fatal(err)
	}

	// there should be 3 files with > 0 lines
	exist := []string{
		pth,
		filepath.Join(base, `testing.nocomp.1.log.gz`),
		filepath.Join(base, `testing.nocomp.2.log.gz`),
		filepath.Join(base, `testing.nocomp.3.log.gz`),
	}
	exist = append(exist, randomFiles...) //these should be untouched

	//and 2-3 more that should not exist at all
	noExist := []string{
		filepath.Join(base, `testing.nocomp.4.log.gz`),
		filepath.Join(base, `testing.nocomp.5.log.gz`),
		filepath.Join(base, `testing.nocomp.6.log.gz`),
		filepath.Join(base, `testing.nocomp.12345.log.gz`),
	}

	for _, e := range exist {
		if cnt, err := countFileLines(e); err != nil {
			t.Fatal(err)
		} else if cnt <= 0 {
			t.Fatal(cnt)
		}
	}

	for _, ne := range noExist {
		if fi, err := os.Stat(ne); err == nil || !os.IsNotExist(err) {
			t.Fatalf("file either exists or something else broken on %v: %v %v", ne, err, fi)
		}
	}
}

func TestNoExtesionNoCompression(t *testing.T) {
	base := t.TempDir()
	pth := filepath.Join(base, `testing.log`)

	//create some random files
	randomFiles := createRandomFiles(base, `testing`)

	fout, err := OpenEx(pth, 0660, 32*1024, 3, false)
	if err != nil {
		t.Fatal(err)
	} else if _, err = dropLineBytes(fout, 256*1024); err != nil {
		t.Fatal(err)
	} else if err = fout.Close(); err != nil {
		t.Fatal(err)
	}

	// there should be 3 files with > 0 lines
	exist := []string{
		pth,
		filepath.Join(base, `testing.1.log`),
		filepath.Join(base, `testing.2.log`),
		filepath.Join(base, `testing.3.log`),
	}
	exist = append(exist, randomFiles...) //these should be untouched

	//and 2-3 more that should not exist at all
	noExist := []string{
		filepath.Join(base, `testing.4`),
		filepath.Join(base, `testing.5`),
		filepath.Join(base, `testing.6`),
		filepath.Join(base, `testing.12345`),
	}

	for _, e := range exist {
		if cnt, err := countFileLines(e); err != nil {
			t.Fatal(err)
		} else if cnt <= 0 {
			t.Fatal(cnt)
		}
	}

	for _, ne := range noExist {
		if fi, err := os.Stat(ne); err == nil || !os.IsNotExist(err) {
			t.Fatalf("file either exists or something else broken on %v: %v %v", ne, err, fi)
		}
	}

}

func dropLines(wtr io.Writer, cnt int) (err error) {
	for i := 0; i < cnt; i++ {
		if _, err = fmt.Fprintf(wtr, "%v line %d\n", time.Now(), i); err != nil {
			break
		}
	}
	return
}

func dropLineBytes(wtr io.Writer, bts int) (cnt int, err error) {
	var n int
	for n < bts {
		var written int
		if written, err = fmt.Fprintf(wtr, "%v line %d with some stuff in it\n", time.Now(), cnt); err != nil {
			break
		}
		n += written
		cnt++
	}
	return
}

func countFileLines(pth string) (int, error) {
	fin, err := os.Open(pth)
	if err != nil {
		return -1, err
	}
	defer fin.Close()
	if filepath.Ext(pth) == `.gz` {
		rdr, err := gzip.NewReader(fin)
		if err != nil {
			return -1, err
		}
		return countLines(rdr), nil
	}

	return countLines(fin), nil
}

func countLines(fin io.Reader) (cnt int) {
	rdr := bufio.NewReader(fin)
	for _, err := rdr.ReadSlice('\n'); err == nil; _, err = rdr.ReadSlice('\n') {
		cnt++
	}
	return
}

func createRandomFile(pth string, lines int) (err error) {
	var fout *os.File
	if fout, err = os.Create(pth); err != nil {
		return
	} else if dropLines(fout, lines); err != nil {
		return
	} else if err = fout.Close(); err != nil {
		return
	}
	return
}

func createRandomFiles(dir, target string) (r []string) {
	names := []string{
		target + `.keeper.log`,
		target + target,
		`foobar`,
		`foo.to.the.bar`,
		`testing`,
	}
	//add in the one that will match and should get shitcanned
	createRandomFile(filepath.Join(dir, `testing.12345`), 256)

	for _, n := range names {
		f := filepath.Join(dir, n)
		createRandomFile(f, 1024)
		r = append(r, f)
	}
	return
}
