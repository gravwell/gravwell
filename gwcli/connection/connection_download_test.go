/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package connection_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"path"
	"testing"

	grav "github.com/gravwell/gravwell/v4/client"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
)

// downloadMockServer mocks the stats, download-initiate, and resource-fetch endpoints needed to
// drive GetResultsForWriter end to end.
type downloadMockServer struct {
	gotFormat      string
	resourceCalls  int
	initiateStatus int
	resourceStatus int
	resourceBody   string
}

func newDownloadMockServer(t *testing.T, sid string) *downloadMockServer {
	t.Helper()

	if clilog.Writer == nil {
		if err := clilog.Init(path.Join(t.TempDir(), "dev.log"), "DEBUG"); err != nil {
			t.Fatalf("failed to spin up logger: %v", err)
		}
	}

	m := &downloadMockServer{initiateStatus: http.StatusOK, resourceStatus: http.StatusOK, resourceBody: "entry one\nentry two\n"}

	l, err := net.Listen("tcp", "[::1]:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })

	statsPath := fmt.Sprintf(grav.SEARCHES_ID_STATS_URL, sid)
	downloadsPath := fmt.Sprintf(grav.SEARCHES_ID_DOWNLOADS_URL, sid)
	resourcePath := downloadsPath + "/resource-1"

	mux := http.NewServeMux()
	mux.HandleFunc(statsPath, func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewEncoder(w).Encode(types.OverviewStats{Finished: true}); err != nil {
			t.Errorf("failed to encode mock overview stats response: %v", err)
		}
	})
	mux.HandleFunc(downloadsPath, func(w http.ResponseWriter, r *http.Request) {
		var req types.SearchDownloadRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode download request body: %v", err)
		}
		m.gotFormat = req.Format
		if m.initiateStatus != http.StatusOK {
			w.WriteHeader(m.initiateStatus)
			return
		}
		if err := json.NewEncoder(w).Encode(types.SearchDownloadResponse{DownloadResourceURL: resourcePath, SearchID: sid}); err != nil {
			t.Errorf("failed to encode mock download response: %v", err)
		}
	})
	mux.HandleFunc(resourcePath, func(w http.ResponseWriter, r *http.Request) {
		m.resourceCalls++
		if m.resourceStatus != http.StatusOK {
			w.WriteHeader(m.resourceStatus)
			return
		}
		_, _ = w.Write([]byte(m.resourceBody))
	})
	srv := http.Server{Handler: mux}
	go srv.Serve(l)
	t.Cleanup(func() { srv.Shutdown(t.Context()) })

	c, err := grav.NewOpts(grav.Opts{Server: l.Addr().String()})
	if err != nil {
		t.Fatal(err)
	}
	// WaitForSearch requires the client to believe it's authenticated; never validated server-side.
	if err := c.ImportLoginToken("test-token"); err != nil {
		t.Fatalf("ImportLoginToken: %v", err)
	}
	connection.Client = c
	t.Cleanup(func() { connection.Client = nil })

	return m
}

// TestGetResultsForWriter covers waiting on a search, picking a download format, and driving
// DownloadSearch's initiate-then-fetch flow.
func TestGetResultsForWriter(t *testing.T) {
	t.Run("format selection", func(t *testing.T) {
		tests := []struct {
			name       string
			renderMod  string
			csv, json  bool
			wantFormat string
		}{
			{"explicit json flag wins regardless of renderer", types.RenderNameTable, false, true, types.DownloadJSON},
			{"explicit csv flag wins regardless of renderer", types.RenderNameTable, true, false, types.DownloadCSV},
			{"text renderer defaults to text", types.RenderNameText, false, false, types.DownloadText},
			{"raw renderer defaults to text", types.RenderNameRaw, false, false, types.DownloadText},
			{"hex renderer defaults to text", types.RenderNameHex, false, false, types.DownloadText},
			{"pcap renderer defaults to pcap", types.RenderNamePcap, false, false, types.DownloadPCAP},
			{"unrecognized/chart-style renderer defaults to archive", types.RenderNameTable, false, false, types.DownloadArchive},
		}
		for i, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				sid := fmt.Sprintf("search-fmt-%d", i)
				m := newDownloadMockServer(t, sid)
				s := grav.Search{ID: sid, RenderMod: tt.renderMod}

				rc, format, err := connection.GetResultsForWriter(&s, types.TimeRange{}, tt.csv, tt.json)
				if err != nil {
					t.Fatalf("GetResultsForWriter: %v", err)
				}
				defer rc.Close()

				if format != tt.wantFormat {
					t.Errorf("format = %q, want %q", format, tt.wantFormat)
				}
				if m.gotFormat != tt.wantFormat {
					t.Errorf("format requested from the server = %q, want %q", m.gotFormat, tt.wantFormat)
				}

				data, err := io.ReadAll(rc)
				if err != nil {
					t.Fatalf("reading body: %v", err)
				}
				if string(data) != m.resourceBody {
					t.Errorf("body = %q, want %q", data, m.resourceBody)
				}
			})
		}
	})

	t.Run("a non-200 from the initiate request is surfaced without ever fetching the resource", func(t *testing.T) {
		sid := "search-initiate-failure"
		m := newDownloadMockServer(t, sid)
		m.initiateStatus = http.StatusInternalServerError
		s := grav.Search{ID: sid, RenderMod: types.RenderNameText}

		if _, _, err := connection.GetResultsForWriter(&s, types.TimeRange{}, false, false); err == nil {
			t.Fatal("expected an error, got nil")
		}
		if m.resourceCalls != 0 {
			t.Errorf("resource endpoint should not have been called after the initiate request failed, got %d calls", m.resourceCalls)
		}
	})

	t.Run("a 404 from the resource fetch (e.g. an expired/aged-out download) is surfaced as an error", func(t *testing.T) {
		sid := "search-aged-out"
		m := newDownloadMockServer(t, sid)
		m.resourceStatus = http.StatusNotFound
		s := grav.Search{ID: sid, RenderMod: types.RenderNameText}

		rc, _, err := connection.GetResultsForWriter(&s, types.TimeRange{}, false, false)
		if rc != nil {
			rc.Close()
			t.Error("expected a nil reader on error")
		}
		var clientErr *grav.ClientError
		if !errors.As(err, &clientErr) {
			t.Fatalf("err = %v, want *client.ClientError", err)
		}
		if clientErr.StatusCode != http.StatusNotFound {
			t.Errorf("StatusCode = %d, want %d", clientErr.StatusCode, http.StatusNotFound)
		}
	})
}
