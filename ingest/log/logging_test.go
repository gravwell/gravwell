/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package log

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	testFile string = `test.log`
)

var (
	tempdir string
)

func TestMain(m *testing.M) {
	var err error
	if tempdir, err = ioutil.TempDir(os.TempDir(), ``); err != nil {
		fmt.Println("Failed to create temp dir", err)
		os.Exit(-1)
	}
	r := m.Run()
	os.RemoveAll(tempdir)
	os.Exit(r)
}

func newLogger() (*Logger, error) {
	p := filepath.Join(tempdir, testFile)
	fout, err := os.Create(p)
	if err != nil {
		return nil, err
	}
	return New(fout), nil
}

func appendLogger() (*Logger, error) {
	p := filepath.Join(tempdir, testFile)
	return NewFile(p)
}

func TestNew(t *testing.T) {
	lgr, err := newLogger()
	if err != nil {
		t.Fatal(err)
	}
	if err = lgr.Criticalf("test: %d", 99); err != nil {
		t.Fatal(err)
	}

	if err = lgr.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestAppend(t *testing.T) {
	lgr, err := appendLogger()
	if err != nil {
		t.Fatal(err)
	}
	if err = lgr.Errorf("test: %d", 99); err != nil {
		t.Fatal(err)
	}

	if err = lgr.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestValue(t *testing.T) {
	lgr, err := appendLogger()
	if err != nil {
		t.Fatal(err)
	}
	testOutputs(t, lgr)
}

func TestRawValue(t *testing.T) {
	pth := filepath.Join(t.TempDir(), `testraw.log`)
	lgr, err := NewFile(pth)
	if err != nil {
		t.Fatal(err)
	}
	lgr.raw = true
	testOutputs(t, lgr)
	bts, err := ioutil.ReadFile(pth)
	if err != nil {
		t.Fatal(err)
	}
	s := string(bts)
	if strings.Contains(s, "<") {
		t.Fatal("raw contains RFC header", s)
	}

}

func testOutputs(t *testing.T, lgr *Logger) {
	var err error
	if err = lgr.Warnf("ERROR test: %d", 99); err != nil {
		t.Fatal(err)
	}
	if err = lgr.Warnf("WARN test: %d", 99); err != nil {
		t.Fatal(err)
	}
	if err = lgr.Infof("INFO test: %d\n", 99); err != nil {
		t.Fatal(err)
	}
	if err = lgr.Debugf("DEBUG test: %d", 99); err != nil {
		t.Fatal(err)
	}
	if err = lgr.Error("tester", KV("id", 99)); err != nil {
		t.Fatal(err)
	}
	if err = lgr.SetLevel(OFF); err != nil {
		t.Fatal(err)
	}
	if err = lgr.Criticalf("CRITICAL testing off: %d", 88); err != nil {
		t.Fatal(err)
	}
	if err = lgr.Close(); err != nil {
		t.Fatal(err)
	}
	bts, err := ioutil.ReadFile(filepath.Join(tempdir, testFile))
	if err != nil {
		t.Fatal(err)
	}
	s := string(bts)
	if !strings.Contains(s, "ERROR test: 99\n") {
		t.Fatal("Missing error value: ", s)
	}
	if !strings.Contains(s, "WARN test: 99\n") {
		t.Fatal("Missing warn value: ", s)
	}
	if !strings.Contains(s, "INFO test: 99\n") {
		t.Fatal("Missing info value: ", s)
	}
	if !strings.Contains(s, "tester") || !strings.Contains(s, `id="99"`) {
		t.Fatal("Missing info value: ", s)
	}
	if strings.Contains(s, "DEBUG test: 99\n") {
		t.Fatal("Has debug level: ", s)
	}
	if strings.Contains(s, "CRITICAL testing off: 88\n") {
		t.Fatal("Has CRITICAL level: ", s)
	}
	if strings.Contains(s, "\n\n") {
		t.Fatalf("did not filter double newlines:\n%q\n", s)
	}
}

func TestMulti(t *testing.T) {
	lgr, err := newLogger()
	if err != nil {
		t.Fatal(err)
	}
	var toCheck []string
	for i := 0; i < 8; i++ {
		fout, err := ioutil.TempFile(tempdir, ``)
		if err != nil {
			t.Fatal(err)
		}
		if err = lgr.AddWriter(fout); err != nil {
			t.Fatal(err)
		}
		toCheck = append(toCheck, fout.Name())
	}

	if err = lgr.Criticalf("CRITICAL 0x%x", 0x1337); err != nil {
		t.Fatal(err)
	}

	if err = lgr.Errorf("ERROR test %d", 1337); err != nil {
		t.Fatal(err)
	}
	for _, n := range toCheck {
		bts, err := ioutil.ReadFile(n)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(bts), "CRITICAL 0x1337\n") {
			t.Fatal(n, " missing critical log value")
		}
		if !strings.Contains(string(bts), "ERROR test 1337\n") {
			t.Fatal(n, " missing error log value ")
		}
	}
	if err = lgr.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestAddRemove(t *testing.T) {
	lgr, err := newLogger()
	if err != nil {
		t.Fatal(err)
	}
	var added []io.WriteCloser
	var toCheck []string
	for i := 0; i < 8; i++ {
		fout, err := ioutil.TempFile(tempdir, ``)
		if err != nil {
			t.Fatal(err)
		}
		if err = lgr.AddWriter(fout); err != nil {
			t.Fatal(err)
		}
		defer fout.Close()
		added = append(added, fout)
		toCheck = append(toCheck, fout.Name())
	}

	if err = lgr.Criticalf("CRITICAL 0x%x", 0x1337); err != nil {
		t.Fatal(err)
	}

	//remove all the added items
	for i := range added {
		if err = lgr.DeleteWriter(added[i]); err != nil {
			t.Fatal(err)
		}
	}

	//log something that should ONLY go to the tempdir
	if err = lgr.Errorf("ERROR test %d", 1337); err != nil {
		t.Fatal(err)
	}

	for _, n := range toCheck {
		bts, err := ioutil.ReadFile(n)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(bts), "CRITICAL 0x1337\n") {
			t.Fatal(n, " missing critical log value")
		}
		if strings.Contains(string(bts), "ERROR test 1337\n") {
			t.Fatal(n, " contains values it should not")
		}
	}

	//check the original which should have both
	bts, err := ioutil.ReadFile(filepath.Join(tempdir, testFile))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(bts), "CRITICAL 0x1337\n") {
		t.Fatal("original missing critical log value")
	}
	if !strings.Contains(string(bts), "ERROR test 1337\n") {
		t.Fatal("original missing ERROR values")
	}

	if err = lgr.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestTrimLength(t *testing.T) {
	input := "twelve bytes"
	output := trimLength(10, input)
	if output != "twelve byt" {
		t.Fatal("trimLength", output)
	}
}

func TestTrimPathLength(t *testing.T) {
	input := "KafkaFederator/kafkaWriter.go:355"
	output := trimPathLength(32, input)
	if output != "kafkaWriter.go:355" {
		t.Fatal("trimPathLength", output)
	}
}

func TestTrimPathLengthBaseTooLong(t *testing.T) {
	input := "KafkaFederator/wayTooManyBytesInThisFilenameWhoDidThis.go:355"
	output := trimPathLength(32, input)
	if output != "sInThisFilenameWhoDidThis.go:355" {
		t.Fatal("trimPathLength", output)
	}
}

func TestStdLibLogger(t *testing.T) {
	pth := filepath.Join(t.TempDir(), `test_stdlib.log`)
	lgr, err := NewFile(pth)
	if err != nil {
		t.Fatal(err)
	}
	//manually create a logger and log something
	slogger := slog.New(lgr)
	slogger.LogAttrs(context.Background(), slog.LevelError, "testing", slog.Attr{Key: `testkey`, Value: slog.AnyValue(99)})

	//now grab a standard logger
	stdlg := lgr.StandardLogger()
	stdlg.Println("testing2")

	//close it out and test the results
	if err := lgr.Close(); err != nil {
		t.Fatal(err)
	}
	bts, err := ioutil.ReadFile(pth)
	if err != nil {
		t.Fatal(err)
	}
	s := string(bts)
	if !strings.Contains(s, "testing\n") {
		t.Fatal("Missing error value: ", s)
	} else if !strings.Contains(s, "testkey=\"99\"") {
		t.Fatal("Missing error value: ", s)
	} else if !strings.Contains(s, "testing2\n") {
		t.Fatal("Missing error value: ", s)
	}

}

func TestUdpLogger(t *testing.T) {
	conn, err := net.ListenPacket("udp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	lgr, err := NewUDPLogger(conn.LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}

	if err = conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Error(err)
	}

	wg := &sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i += 4 {
			if err = lgr.Criticalf("log line %d - CRITICAL", i); err != nil {
				t.Error(err)
				break
			} else if lgr.Errorf("log line %d - ERROR", i+1); err != nil {
				t.Error(err)
				break
			} else if lgr.Warnf("log line %d - WARN", i+2); err != nil {
				t.Error(err)
				break
			} else if lgr.Infof("log line %d - INFO", i+3); err != nil {
				t.Error(err)
				break
			}
		}

		if err = lgr.Close(); err != nil {
			t.Error(err)
		}
	}()

	// read 100 packets, we don't care about the other 9900, UDP should keep trucking
	buff := make([]byte, 4096)
	for i := 0; i < 100; i++ {
		n, _, err := conn.ReadFrom(buff)
		if err != nil {
			t.Fatal(err)
		} else if n <= 0 || n > 4096 {
			t.Fatalf("got an invalid packet from our logger %d", n)
		} else {
			b := buff[0:n]
			want := fmt.Sprintf("log line %d - ", i)
			if !strings.Contains(string(b), want) {
				t.Fatalf("Got a bad log line: (%d) (%s) %s\n", n, want, string(b))
			}
		}
	}
	wg.Wait()

}
