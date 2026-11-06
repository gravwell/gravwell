/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package connection_test

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
)

func TestParseJWT(t *testing.T) {
	// a valid, but outdated, token to use for testing
	validTkn := "7b22616c676f223a36323733382c22747970223a226a7774227d.7b22756964223a312c2265787069726573223a22323032352d30362d31305432323a34313a31312e3532353838333730325a222c22696174223a5b3139382c32332c39342c3232332c32342c34372c3134372c3139302c35382c3231342c3135362c3137362c3234342c3131302c35322c37352c3137322c3231372c3139382c3231352c3130352c3139302c342c3230352c38342c3130362c39352c3233332c3131322c3130362c31302c3133305d2c226e6f4c6f67696e4368616e6765223a66616c73652c226e6f44697361626c654d4641223a66616c73657d.526de9ffa6a7950c9812c3d378b9d8bc873b60770e485d041a33560f248579ece40f38b6bf84363dd4724cf14f735cdb3a120b414b7a6003dbe855a7f0bc3b45"
	// values known to be contained in the above JWT for validation purposes
	var expectedHeader = connection.JWTHeader{
		Algo: 62738,
		Typ:  "jwt",
	}
	expectedTime, err := time.Parse("2006-01-02 15:04:05.999999999 +0000 UTC", "2025-06-10 22:41:11.525883702 +0000 UTC")
	if err != nil {
		t.Fatal(err)
	}
	var expectedPayload = connection.JWTPayload{
		UID:     1,
		Expires: expectedTime,
	}

	if hdr, payload, sig, err := connection.ParseJWT(validTkn); err != nil {
		t.Fatal("unexpected error:", testsupport.ExpectedActual(nil, err))
	} else if hdr.Algo != expectedHeader.Algo || hdr.Typ != expectedHeader.Typ {
		t.Fatal("header mismatch:", testsupport.ExpectedActual(expectedHeader, hdr))
	} else if payload.UID != expectedPayload.UID || payload.Expires != expectedPayload.Expires {
		t.Fatal("payload mismatch:", testsupport.ExpectedActual(expectedPayload, payload))
	} else if sig == nil {
		t.Fatalf("nil signature")
	}
}

// TestDestroyTokenFile checks that DestroyTokenFile removes an existing token file,
// no-ops (returns nil) if the file is already absent, and otherwise propagates any real
// removal error instead of swallowing it.
//
// Regression coverage for gwcli logout never deleting the cached JWT file, which left a
// stale/invalidated token behind forever after logging out.
func TestDestroyTokenFile(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(t *testing.T) (file string)
		wantErr bool
	}{
		{
			name: "removes an existing file",
			setup: func(t *testing.T) string {
				pth := path.Join(t.TempDir(), "tknfile")
				if err := os.WriteFile(pth, []byte("someuser\nsometoken"), 0600); err != nil {
					t.Fatal(err)
				}
				return pth
			},
			wantErr: false,
		},
		{
			name: "no-ops when the file does not exist",
			setup: func(t *testing.T) string {
				return path.Join(t.TempDir(), "does-not-exist")
			},
			wantErr: false,
		},
		{
			name: "propagates a real removal error",
			setup: func(t *testing.T) string {
				// os.Remove cannot remove a non-empty directory; this gives us a
				// deterministic, non-ErrNotExist error without relying on permission
				// tricks that behave inconsistently (e.g. when run as root).
				dir := t.TempDir()
				if err := os.WriteFile(path.Join(dir, "child"), []byte("x"), 0600); err != nil {
					t.Fatal(err)
				}
				return dir
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file := tt.setup(t)
			err := connection.DestroyTokenFile(file)
			if (err != nil) != tt.wantErr {
				t.Fatalf("DestroyTokenFile() error = %v, wantErr %v", err, tt.wantErr)
			}
			// DestroyTokenFile should never surface a "file does not exist" error; that
			// case is meant to be a no-op.
			if err != nil && errors.Is(err, fs.ErrNotExist) {
				t.Fatal("DestroyTokenFile should swallow ErrNotExist, not return it:", err)
			}
			if !tt.wantErr {
				if _, statErr := os.Stat(file); !errors.Is(statErr, fs.ErrNotExist) {
					t.Fatalf("expected file to no longer exist, stat returned: %v", statErr)
				}
			}
		})
	}
}
