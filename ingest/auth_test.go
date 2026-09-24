/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package ingest

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/gravwell/gravwell/v4/ingest/entry"
)

const (
	pwd          = `passwords and stuff`
	testTagCount = 256
)

func TestAuthChallenge(t *testing.T) {
	var chalA Challenge
	bb := bytes.NewBuffer(nil)
	hsh, err := GenAuthHash(pwd)
	if err != nil {
		t.Fatal(err)
	}

	chal, err := NewChallenge(hsh)
	if err != nil {
		t.Fatal(err)
	}
	if err := chal.Write(bb); err != nil {
		t.Fatal(err)
	}
	if err := chalA.Read(bb); err != nil {
		t.Fatal(err)
	}
	if chalA != chal {
		t.Fatal("challenge mismatch")
	}
}

func TestAuthChallengeResponse(t *testing.T) {
	var cr ChallengeResponse
	bb := bytes.NewBuffer(nil)
	hsh, err := GenAuthHash(pwd)
	if err != nil {
		t.Fatal(err)
	}
	chal, err := NewChallenge(hsh)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := GenerateResponse(hsh, chal)
	if err != nil {
		t.Fatal(err)
	}
	if err := resp.Write(bb); err != nil {
		t.Fatal(err)
	}
	if err := cr.Read(bb); err != nil {
		t.Fatal(err)
	}
	if err := VerifyResponse(hsh, chal, cr); err != nil {
		t.Fatal(err)
	}
}

func TestAuthChallengeBadResponse(t *testing.T) {
	var cr ChallengeResponse
	bb := bytes.NewBuffer(nil)
	hsh, err := GenAuthHash(pwd)
	if err != nil {
		t.Fatal(err)
	}
	chal, err := NewChallenge(hsh)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := GenerateResponse(hsh, chal)
	if err != nil {
		t.Fatal(err)
	}
	resp.Response[0]++
	if err := resp.Write(bb); err != nil {
		t.Fatal(err)
	}
	if err := cr.Read(bb); err != nil {
		t.Fatal(err)
	}
	if err := VerifyResponse(hsh, chal, cr); err == nil {
		t.Fatalf("failed to catch bad response")
	}
}

func TestAuthStateResponse(t *testing.T) {
	bb := bytes.NewBuffer(nil)
	var sr2 StateResponse
	sr := StateResponse{
		ID:   0xDEADBEEF,
		Info: "this is a test",
	}
	if err := sr.Write(bb); err != nil {
		t.Fatal(err)
	}
	if err := sr2.Read(bb); err != nil {
		t.Fatal(err)
	}
	if sr != sr2 {
		t.Fatal("Mismatched state response")
	}
}

func TestAuthTagRequest(t *testing.T) {
	bb := bytes.NewBuffer(nil)
	var tr TagRequest
	var tr2 TagRequest

	for i := 0; i < testTagCount; i++ {
		tr.Count++
		tr.Tags = append(tr.Tags, fmt.Sprintf("Tag%d", i))
	}

	if err := tr.Write(bb); err != nil {
		t.Fatal(err)
	}

	if err := tr2.Read(bb); err != nil {
		t.Fatal(err)
	}

	if tr.Count != tr2.Count || tr2.Count != testTagCount {
		t.Fatal("Invalid tag count")
	}
	for i := range tr2.Tags {
		if tr2.Tags[i] != tr.Tags[i] {
			t.Fatal("Invalid tag match")
		}
	}
}

func TestAuthTagResponse(t *testing.T) {
	bb := bytes.NewBuffer(nil)
	tr := TagResponse{
		Tags: make(map[string]entry.EntryTag, 1),
	}
	var tr2 TagResponse

	for i := 0; i < testTagCount; i++ {
		tr.Count++
		tr.Tags[fmt.Sprintf("Tag%d", i)] = entry.EntryTag(i)
	}

	if err := tr.Write(bb); err != nil {
		t.Fatal(err)
	}

	if err := tr2.Read(bb); err != nil {
		t.Fatal(err)
	}

	if tr.Count != tr2.Count || tr2.Count != testTagCount {
		t.Fatal("Invalid tag count")
	}
	for k, v := range tr2.Tags {
		v2, ok := tr.Tags[k]
		if !ok {
			t.Fatal("Missing value ", k)
		}
		if v2 != v {
			t.Fatal("Invalid value", v2, v)
		}
	}
}

func TestAuthTenantEnabledChallengeResponse(t *testing.T) {
	var cr ChallengeResponse
	bb := bytes.NewBuffer(nil)
	hsh, err := GenAuthHash(pwd)
	if err != nil {
		t.Fatal(err)
	}
	chal, err := NewChallenge(hsh)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := GenerateResponse(hsh, chal)
	if err != nil {
		t.Fatal(err)
	}
	resp.Tenant = `bobby`
	if err := resp.Write(bb); err != nil {
		t.Fatal(err)
	}
	if err := cr.Read(bb); err != nil {
		t.Fatal(err)
	}
	if err := VerifyResponse(hsh, chal, cr); err != nil {
		t.Fatal(err)
	}
}

func TestAuthDisabledChallengeResponse(t *testing.T) {
	var cr ChallengeResponse
	bb := bytes.NewBuffer(nil)
	hsh, err := GenAuthHash(pwd)
	if err != nil {
		t.Fatal(err)
	}
	chal, err := NewChallenge(hsh)
	if err != nil {
		t.Fatal(err)
	}
	chal.Version--
	resp, err := GenerateResponse(hsh, chal)
	if err != nil {
		t.Fatal(err)
	}
	if err := resp.Write(bb); err != nil {
		t.Fatal(err)
	}
	if err := cr.Read(bb); err != nil {
		t.Fatal(err)
	}
	if err := VerifyResponse(hsh, chal, cr); err != nil {
		t.Fatal(err)
	}
}

func TestAuthTenantEnabledBadVersionChallengeResponse(t *testing.T) {
	var cr ChallengeResponse
	bb := bytes.NewBuffer(nil)
	hsh, err := GenAuthHash(pwd)
	if err != nil {
		t.Fatal(err)
	}
	chal, err := NewChallenge(hsh)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := GenerateResponse(hsh, chal)
	if err != nil {
		t.Fatal(err)
	}
	// the version should ensure that even though we have a tenant name that it won't use it
	resp.Tenant = `bobby`
	resp.Version = 6
	if err := resp.Write(bb); err != nil {
		t.Fatal(err)
	}
	if err := cr.Read(bb); err != nil {
		t.Fatal(err)
	}
	if err := VerifyResponse(hsh, chal, cr); err != nil {
		t.Fatal(err)
	}
}

func FuzzAuthChallengeResponse(f *testing.F) {
	var chal Challenge
	var hsh AuthHash
	for i := 0; i < 16; i++ {
		var err error
		pwd := fmt.Sprintf("password test with %d %x %b", i, i, i)
		bb := bytes.NewBuffer(nil)
		if hsh, err = GenAuthHash(pwd); err != nil {
			f.Fatal(err)
		} else if chal, err = NewChallenge(hsh); err != nil {
			f.Fatal(err)
		} else if resp, err := GenerateResponse(hsh, chal); err != nil {
			f.Fatal(err)
		} else if err = resp.Write(bb); err != nil {
			f.Fatal(err)
		} else {
			f.Add(bb.Bytes())
		}
	}
	f.Fuzz(func(t *testing.T, resp []byte) {
		var cr ChallengeResponse
		bb := bytes.NewBuffer(resp)
		if err := cr.Read(bb); err != nil {
			t.Log(err)
		} else if err = VerifyResponse(hsh, chal, cr); err != nil {
			t.Log(err)
		}
	})
}
