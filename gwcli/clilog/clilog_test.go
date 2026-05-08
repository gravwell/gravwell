package clilog_test

import (
	"bufio"
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path"
	"strings"
	"testing"

	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
)

func TestTee(t *testing.T) {
	t.Run("debug", func(t *testing.T) {
		// make sure the logger is dead
		clilog.Destroy()
		// initialize the logger
		logPath := path.Join(t.TempDir(), "debug.dev.log")
		clilog.Init(logPath, "debug")

		type args struct {
			lvl clilog.Level
			str string
		}
		tests := []struct {
			name    string
			args    args
			wantAlt string
		}{
			{"matching level", args{clilog.DEBUG, "Case"}, "Case"},
			{"higher level", args{clilog.WARN, "Molly"}, "Molly"},
		}
		for i, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				alt := &bytes.Buffer{}
				clilog.Tee(tt.args.lvl, alt, tt.args.str)
				if gotAlt := alt.String(); gotAlt != tt.wantAlt {
					t.Errorf("Tee() = %v, want %v", gotAlt, tt.wantAlt)
				}

				// slurp the log file
				// due to amortization, the file may not be flushed yet
				expectedLineCount := i + 3 // we get three starter logs when initializing clilog at debug level
				fc, err := os.ReadFile(logPath)
				if err != nil {
					t.Fatal("failed to read log file:", err)
				} else if exploded := strings.Split(string(fc), "\n"); len(exploded) != expectedLineCount {
					// NOTE(rlandau): this could be broken by logs spanning multiple lines, but our test data should be free of that
					t.Fatal("incorrect number of lines for test count.", testsupport.ExpectedActual(expectedLineCount, len(exploded)))
				}
			})
		}
	})

	t.Run("error", func(t *testing.T) {
		// make sure the logger is dead
		clilog.Destroy()

		// initialize the logger
		logPath := path.Join(t.TempDir(), "error.dev.log")
		if err := clilog.Init(logPath, "error"); err != nil {
			t.Fatal(err)
		}

		var printingTests = 0
		type args struct {
			lvl clilog.Level
			str string
		}
		tests := []struct {
			name    string
			args    args
			wantAlt string
		}{
			{"matching level", args{clilog.ERROR, "Case"}, "Case"},
			{"higher level", args{clilog.CRITICAL, "Molly"}, "Molly"},
			{"lower level", args{clilog.INFO, "Armitage"}, "Armitage"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				if clilog.Active(tt.args.lvl) {
					printingTests += 1
				}

				alt := &bytes.Buffer{}
				clilog.Tee(tt.args.lvl, alt, tt.args.str)
				if gotAlt := alt.String(); gotAlt != tt.wantAlt {
					t.Errorf("Tee() = %v, want %v", gotAlt, tt.wantAlt)
				}
			})
		}

		// close out the log
		if err := clilog.Destroy(); err != nil {
			t.Fatal(err)
		}

		// check that the log file looks as expected

		// slurp the log file
		// due to amortization, the file may not be flushed yet
		fc, err := os.ReadFile(logPath)
		if err != nil {
			t.Fatal("failed to read log file:", err)
		}
		scan := bufio.NewScanner(strings.NewReader(string(fc)))
		//scan.Scan() // error level does not have an initialization line
		var i int
		for scan.Scan() {
			line := scan.Text()
			t.Log(line)
			// check for the text of each test
			if !strings.Contains(line, tests[i].args.str) {
				t.Fatalf("expected log entry %v to contain %v", i, tests[i].args.str)
			}
			i += 1
		}
	})
}

func TestInit(t *testing.T) {
	// ensure the singleton does not exist
	clilog.Destroy()

	type args struct {
		path string
		lvl  string
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{"bad level", args{"dev.log", "fake level"}, true},
		{"empty path", args{"", "debug"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clilog.Destroy()

			if err := clilog.Init(tt.args.path, tt.args.lvl); (err != nil) != tt.wantErr {
				t.Errorf("Init() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
	t.Run("valid path", func(t *testing.T) {
		clilog.Destroy()
		path := path.Join(t.TempDir(), "dev.log")

		if err := clilog.Init(path, "info"); err != nil {
			t.Fatal("failed to initialize clilog on valid path:", err)
		}
		// make a test write
		if err := clilog.Writer.Critical("test log"); err != nil {
			t.Fatal("failed to send test log:", err)
		}
	})
	t.Run("reinitialize", func(t *testing.T) {
		// call Init a second time without calling Destroy
		// should not create the new file

		if err := clilog.Destroy(); err != nil {
			t.Fatal(err)
		}

		origLogPath := path.Join(t.TempDir(), "dev.log.orig")
		if err := clilog.Init(origLogPath, "critical"); err != nil {
			t.Fatal(err)
		}
		secondLogPath := path.Join(t.TempDir(), "should_not_be_created.log")
		if err := clilog.Init(secondLogPath, "critical"); err != nil {
			t.Fatal(err)
		}
		_, err := os.Stat(secondLogPath)
		if err == nil {
			t.Fatal("successfully stat'd file that should not exist")
		}
		if perr, ok := err.(*os.PathError); !ok {
			t.Fatal("failed to cast error to PathError")
		} else if !errors.Is(perr.Err, fs.ErrNotExist) {
			t.Fatal("incorrect error:", testsupport.ExpectedActual(fs.ErrNotExist, perr.Err))
		}

	})

}
