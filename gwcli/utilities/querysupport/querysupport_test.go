package querysupport

import (
	"io"
	"math/rand/v2"
	"os"
	"path"
	"strings"
	"testing"

	"github.com/Pallinder/go-randomdata"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
)

func Test_toFile(t *testing.T) {
	dir := t.TempDir()

	//toFile relies on clilog, make sure it is spinning
	if err := clilog.Init(path.Join(dir, t.Name()+".log"), "DEBUG"); err != nil {
		t.Fatal("failed to spin up logger: ", err)
	}

	type args struct {
		resultsStr string
		path       string
		append     bool
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{"output to file, truncate", args{"Hello World", path.Join(dir, "1"), false}, false},
		{"output to file, append", args{"Hello World", path.Join(dir, "2"), true}, false},
		{"output to file, append random paragraph", args{randomdata.Paragraph(), path.Join(dir, "3"), true}, false},
		{"failure case: file is dir", args{"", dir, true}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// wrap the mock results
			resultSize := len(tt.args.resultsStr)
			r := io.NopCloser(strings.NewReader(tt.args.resultsStr))

			// if append, create some garbage data to populate the file with first
			var priorSize int64
			if tt.args.append {
				priorSize = prepopulateFile(t, tt.args.path)
			}

			if err := toFile(r, tt.args.path, tt.args.append); (err != nil) != tt.wantErr {
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
