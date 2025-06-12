package ingest

import (
	"os"
	"path"
	"testing"

	"github.com/Pallinder/go-randomdata"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
)

func Test_autoingest(t *testing.T) {
	if err := clilog.Init(path.Join(t.TempDir(), "dev.log"), "debug"); err != nil {
		t.Fatal(err)
	}
	t.Run("single file, zero tags", func(t *testing.T) {
		fp := []string{"somefile.txt"}
		tags := []string{}
		src := ""

		wantErr := true

		if err := autoingest(nil, fp, tags, false, false, src); (err != nil) != wantErr {
			t.Errorf("autoingest() error = %v, wantErr %v", err, wantErr)
		}
	})
	t.Run("single file, many tags", func(t *testing.T) {
		fp := []string{"somefile.txt"}
		tags := []string{"tag1", "tag2", "tag3"}
		src := ""

		wantErr := true

		if err := autoingest(nil, fp, tags, false, false, src); (err != nil) != wantErr {
			t.Errorf("autoingest() error = %v, wantErr %v", err, wantErr)
		}
	})

	t.Run("single file, single tag", func(t *testing.T) {
		fn := path.Join(t.TempDir(), "dummyfile")
		// create a dummy file for ingestion
		if err := os.WriteFile(fn, []byte(randomdata.Paragraph()), os.ModeTemporary); err != nil {
			t.Skip("failed to create a dummy file for ingestion")
		}

		fp, tags, src := []string{fn}, []string{"tag1"}, ""
		wantErr := false
		ch := make(chan struct {
			string
			error
		})

		if err := autoingest(ch, fp, tags, false, false, src); (err != nil) != wantErr {
			t.Errorf("autoingest() error = %v, wantErr %v", err, wantErr)
		}
		if !wantErr {
			// check the ingestion results
			successes, errors := 0, 0
			for range len(fp) {
				res := <-ch
				if res.error != nil {
					errors += 1
				} else {
					successes += 1
				}
			}

		}
	})

	/*type args struct {
		res chan<- struct {
			string
			error
		}
		filepaths []string
		tags      []string
		ignoreTS  bool
		localTime bool
		src       string
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := autoingest(tt.args.res, tt.args.filepaths, tt.args.tags, tt.args.ignoreTS, tt.args.localTime, tt.args.src); (err != nil) != tt.wantErr {
				t.Errorf("autoingest() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}*/
}
