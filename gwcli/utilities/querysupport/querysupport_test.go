package querysupport

import (
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"path"
	"strings"
	"testing"

	"github.com/Pallinder/go-randomdata"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
)

// Given a path, pre-populates a file with garbage data, then closes the file.
func prepopulateFile(t *testing.T, path string) (size int64) {
	f, err := os.Create(path)
	if err != nil {
		t.Skip("failed to create precursor file for append: ", err)
	}
	defer f.Close()
	// throw garbage data into the file
	var sb strings.Builder
	lineCount := rand.Int64N(100) // pick a number of lines
	for range lineCount {
		lineLength := rand.Int32N(650)
		sb.WriteString(randomdata.Alphanumeric(int(lineLength)))
	}
	if _, err := f.WriteString(sb.String()); err != nil {
		t.Skip("failed to write into precursor file for append: ", err)
	}

	if err := f.Sync(); err != nil {
		t.Skip("failed to flush precursor file for append: ", err)
	}

	fi, err := f.Stat()
	if err != nil {
		t.Skip("failed to stat precursor file for append: ", err)
	}
	return fi.Size()
}

func Test_PutResultsToWriter(t *testing.T) {
	dir := t.TempDir()

	//toFile relies on clilog, make sure it is spinning
	if err := clilog.Init(path.Join(dir, t.Name()+".log"), "DEBUG"); err != nil {
		t.Fatal("failed to spin up logger: ", err)
	}

	{ // tests that should invoke toFile
		type toFileArgs struct {
			resultsStr string
			path       string
			append     bool
			format     string
		}
		tests := []struct {
			name    string
			args    toFileArgs
			wantErr bool
		}{
			{"output to file, truncate", toFileArgs{"Hello World", path.Join(dir, "1"), false, types.DownloadText}, false},
			{"output to file, append", toFileArgs{"Hello World", path.Join(dir, "2"), true, types.DownloadText}, false},
			{"output to file, append random paragraph, empty format", toFileArgs{randomdata.Paragraph(), path.Join(dir, "3"), true, ""}, false},
			{"output to file, append random paragraph, download archive", toFileArgs{randomdata.Paragraph(), path.Join(dir, "3"), true, types.DownloadArchive}, false},
			{"failure case: file is dir, empty format", toFileArgs{"", dir, true, ""}, true},
			{"failure case: file is dir, download archive", toFileArgs{"", dir, true, types.DownloadArchive}, true},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				// wrap the mock results
				resultSize := len(tt.args.resultsStr)
				r := io.NopCloser(strings.NewReader(tt.args.resultsStr))

				// if append, create some garbage data to populate the file with first
				var priorSize int64
				if !tt.wantErr && tt.args.append {
					priorSize = prepopulateFile(t, tt.args.path)
				}

				if err := putResultsToWriter(r, nil, tt.args.path, tt.args.append, ""); (err != nil) != tt.wantErr {
					t.Errorf("toFile() error = %v, wantErr %v", err, tt.wantErr)
				}
				if !tt.wantErr {
					// check that the file exists
					fi, err := os.Stat(tt.args.path)
					if err != nil {
						t.Error(err)
					}
					// confirm file size
					if fi.Size() != int64(resultSize)+int64(priorSize) {
						t.Fatalf("output file is of wrong size. Expected %v, got %v(=%v+%v)",
							fi.Size(),
							int64(resultSize)+int64(priorSize),
							int64(resultSize),
							int64(priorSize))
					}
				}

			})
		}
	}

	{ // tests that should not invoke toFile
		// create a buffer to act as stdout
		var sb strings.Builder

		type args struct {
			resultsStr string
			format     string
		}
		tests := []struct {
			name    string
			args    args
			wantErr bool
		}{
			{"output to writer, text", args{"Hello World", types.DownloadText}, false},
			{"failure case: DownloadArchive format with results", args{"Hello World", types.DownloadArchive}, true},
			{"failure case: DownloadArchive format empty results", args{"", types.DownloadArchive}, true},
			{"output to writer, empty text", args{"", types.DownloadCSV}, false},
			{"output to writer, random text/random format", args{randomdata.Paragraph() + randomdata.Paragraph() + randomdata.Paragraph(), randomdata.Address()}, false},
		}
		for _, tt := range tests {
			sb.Reset()
			t.Run(tt.name, func(t *testing.T) {
				// wrap the mock results
				r := io.NopCloser(strings.NewReader(tt.args.resultsStr))

				if err := putResultsToWriter(r, &sb, "", false, tt.args.format); (err != nil) != tt.wantErr {
					t.Errorf("toFile() error = %v, wantErr %v", err, tt.wantErr)
				}
				if !tt.wantErr {
					var expectedOut string
					if tt.args.resultsStr == "" {
						expectedOut = NoResults
					} else {
						expectedOut = tt.args.resultsStr
					}
					if expectedOut != strings.TrimSpace(sb.String()) { // check that the buffer contains our results
						t.Fatal("data in writer does not match input data" + expectedActual(expectedOut, sb.String()))
					}

				}

			})
		}
	}
}

// Returns a string declaring what was expected and what we got instead.
// NOTE(rlandau): Prefixes the string with a newline.
func expectedActual(expected, actual any) string {
	return fmt.Sprintf("\n\tExpected:'%v'\n\tGot:'%v'", expected, actual)
}
