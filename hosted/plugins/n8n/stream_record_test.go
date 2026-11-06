package n8n

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/gravwell/gravwell/v3/ingest/entry"
)

type repeatedByteReader struct {
	remaining int64
	value     byte
}

func (r *repeatedByteReader) Read(p []byte) (int, error) {
	if r.remaining == 0 {
		return 0, io.EOF
	}
	size := min(int64(len(p)), r.remaining)
	for i := range p[:int(size)] {
		p[i] = r.value
	}
	r.remaining -= size
	return int(size), nil
}

func normalizeTestRecord(t *testing.T, text string, scrub bool) *normalizedRecord {
	t.Helper()
	r, err := normalizeRecordStream(t.Context(), newResponseScanner(strings.NewReader(text)), scrub, t.TempDir(), int64(entry.MaxDataSize))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := r.close(); err != nil {
			t.Error(err)
		}
	})
	return r
}

func TestStreamingTinyRecordsReuseScratchAndRetainNoMetadata(t *testing.T) {
	const count = 2000
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	text := `{"data":[` + strings.TrimSuffix(strings.Repeat(`{"id":"x"},`, count), ",") + `],"nextCursor":null}`
	hc := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(text))}, nil
	})}
	c, err := newClient("https://example.invalid/api/v1", "synthetic", 10000, 100, 1, hc)
	if err != nil {
		t.Fatal(err)
	}
	files := make(map[string]struct{})
	seen := 0
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	result, err := c.collectStreaming(t.Context(), datasetCatalog["users"], func(_ collectedRecord, body *normalizedRecord) error {
		files[body.file.Name()] = struct{}{}
		seen++
		return nil
	})
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatal(err)
	}
	if seen != count {
		t.Fatalf("delivered=%d want %d", seen, count)
	}
	if len(result.Records) != 0 {
		t.Errorf("streaming retained %d unnecessary record descriptors", len(result.Records))
	}
	if len(files) != 1 {
		t.Errorf("created %d scratch files for %d records, want one reusable file", len(files), count)
	}
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > count*16*1024 {
		t.Errorf("tiny-record allocation=%d bytes (%d/record), want less than 16 KiB/record", allocated, allocated/count)
	} else {
		t.Logf("tiny-record allocation=%d bytes (%d/record)", allocated, allocated/count)
	}
	left, err := os.ReadDir(dir)
	if err != nil || len(left) != 0 {
		t.Fatalf("scratch cleanup=%v error=%v", left, err)
	}
}

func TestStreamIdentityDistinguishesNumericAndStringIDs(t *testing.T) {
	a := normalizeTestRecord(t, `{"id":"1"}`, false)
	b := normalizeTestRecord(t, `{"id":1}`, false)
	if a.identity == b.identity {
		t.Fatal("string and numeric IDs collide")
	}
}

func TestStreamingScratchReusePreservesPayloadAndCleansCallbackFailure(t *testing.T) {
	for _, failWrite := range []bool{false, true} {
		t.Run(fmt.Sprint("writeFailure=", failWrite), func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("TMPDIR", dir)
			records := []string{`{"id":"long","value":"` + strings.Repeat("x", 128*1024) + `"}`, `{"id":"s"}`, `{"id":"unicode","value":"λ"}`}
			hc := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"data":[` + strings.Join(records, ",") + `]}`))}, nil
			})}
			c, err := newClient("https://example.invalid/api/v1", "synthetic", 10000, 100, 1, hc)
			if err != nil {
				t.Fatal(err)
			}
			i := 0
			writeErr := errors.New("synthetic write failure")
			result, err := c.collectStreaming(t.Context(), datasetCatalog["users"], func(_ collectedRecord, body *normalizedRecord) error {
				payload, err := body.payload()
				if err != nil || string(payload) != records[i] {
					t.Fatalf("record %d payload mismatch, error=%v", i, err)
				}
				i++
				if failWrite && i == 2 {
					return writeErr
				}
				return nil
			})
			if failWrite {
				if !errors.Is(err, writeErr) {
					t.Fatalf("lost callback error: %v", err)
				}
			} else if err != nil || i != len(records) || result.Count != len(records) {
				t.Fatalf("records=%d count=%d error=%v", i, result.Count, err)
			}
			files, err := os.ReadDir(dir)
			if err != nil || len(files) != 0 {
				t.Fatalf("leftover scratch=%v, error=%v", files, err)
			}
		})
	}
}

// Independent, buffered oracle retained only for the streaming scrubber test.
func credentialSecretKey(key string) bool {
	normalized := strings.NewReplacer("_", "", "-", "", ".", "").Replace(strings.ToLower(strings.TrimSpace(key)))
	if normalized == "data" || normalized == "credentialdata" {
		return true
	}
	return strings.Contains(normalized, "secret") || strings.Contains(normalized, "password") || strings.Contains(normalized, "token") || strings.Contains(normalized, "key")
}

func TestStreamNormalizerDoesNotAllocateRecordSizedInputOrMap(t *testing.T) {
	// No payload() call: an unchanged record must never allocate entry data.
	// The source is generated, so the probe itself does not hold a giant string.
	const length = 32 * 1024 * 1024
	for _, prefix := range []string{`{"id":"fixed","value":"`, `{"id":"`} {
		t.Run(prefix, func(t *testing.T) {
			dir := t.TempDir()
			reader := io.MultiReader(strings.NewReader(prefix), &repeatedByteReader{length, 'x'}, strings.NewReader(`"}`))
			runtime.GC()
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			r, err := normalizeRecordStream(t.Context(), newResponseScanner(reader), false, dir, int64(entry.MaxDataSize))
			runtime.ReadMemStats(&after)
			if err != nil {
				t.Fatal(err)
			}
			if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 8*1024*1024 {
				t.Errorf("normalization allocated %d bytes for a %d-byte value; want bounded scratch below 8 MiB", allocated, length)
			}
			info, err := r.file.Stat()
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != 0600 {
				t.Errorf("scratch mode=%o", info.Mode().Perm())
			}
			if r.size < length {
				t.Errorf("normalized size=%d", r.size)
			}
			if err := r.close(); err != nil {
				t.Fatal(err)
			}
			files, err := os.ReadDir(dir)
			if err != nil || len(files) != 0 {
				t.Fatalf("scratch files=%v error=%v", files, err)
			}
		})
	}
}

func TestClientCollectionDoesNotAllocateRecordSizedCopies(t *testing.T) {
	const length = 32 * 1024 * 1024
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"data":[{"id":"fixed","value":"`)
		_, _ = io.Copy(w, &repeatedByteReader{length, 'x'})
		_, _ = io.WriteString(w, `"}],"nextCursor":null}`)
	}))
	defer server.Close()
	c, err := newClient(server.URL+"/api/v1", "synthetic", 10000, 100, 1, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	result, err := c.collect(t.Context(), datasetCatalog["users"])
	runtime.ReadMemStats(&after)
	if err != nil || len(result.Records) != 1 {
		t.Fatalf("collection records=%d error=%v", len(result.Records), err)
	}
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 12*1024*1024 {
		t.Fatalf("collection allocated %d bytes for a %d-byte record before entry data was needed", allocated, length)
	}
}

func TestClientRejectsRecordAbovePerRecordSafetyLimitBeforeDelivery(t *testing.T) {
	if maxNormalizedRecordSize != 64*1024*1024 {
		t.Fatalf("production per-record safety limit=%d, want %d", maxNormalizedRecordSize, 64*1024*1024)
	}
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	const testLimit = 256
	hc := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		body := io.MultiReader(
			strings.NewReader(`{"data":[{"id":"one","value":"`),
			&repeatedByteReader{remaining: testLimit, value: 'x'},
			strings.NewReader(`"}],"nextCursor":null}`),
		)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(body)}, nil
	})}
	c, err := newClient("https://example.invalid/api/v1", "synthetic", 10000, 100, 1, hc)
	if err != nil {
		t.Fatal(err)
	}
	c.recordLimit = testLimit
	delivered := 0
	result, err := c.collectStreaming(t.Context(), datasetCatalog["users"], func(collectedRecord, *normalizedRecord) error {
		delivered++
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "per-record safety limit of 256 bytes") {
		t.Fatalf("oversized record error=%v", err)
	}
	if delivered != 0 || result.Count != 0 || len(result.Records) != 0 {
		t.Fatalf("oversized record reached delivery: callbacks=%d count=%d records=%d", delivered, result.Count, len(result.Records))
	}
	if files, readErr := os.ReadDir(dir); readErr != nil || len(files) != 0 {
		t.Fatalf("oversized-record scratch files=%v error=%v", files, readErr)
	}
}

func TestNormalizedRecordPayloadRejectsSizeAboveBoundBeforeReading(t *testing.T) {
	normalizer := newRecordNormalizer(t.TempDir(), int64(entry.MaxDataSize))
	if normalizer.limit != maxNormalizedRecordSize {
		t.Fatalf("normalizer limit=%d, want hard cap %d", normalizer.limit, maxNormalizedRecordSize)
	}
	record := &normalizedRecord{size: maxNormalizedRecordSize + 1, limit: maxNormalizedRecordSize}
	if _, err := record.payload(); err == nil || !strings.Contains(err.Error(), "invalid normalized n8n entry size") {
		t.Fatalf("oversized payload error=%v", err)
	}
}

func TestStreamNormalizerGiantKeysUseBoundedMemory(t *testing.T) {
	const length = 8 * 1024 * 1024
	dir := t.TempDir()
	reader := io.MultiReader(strings.NewReader(`{"`), &repeatedByteReader{length, 'x'}, strings.NewReader(`Secret":"DROP","id":"one"}`))
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	r, err := normalizeRecordStream(t.Context(), newResponseScanner(reader), true, dir, int64(entry.MaxDataSize))
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatal(err)
	}
	defer r.close()
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 8*1024*1024 {
		t.Errorf("giant-key scratch allocations=%d", allocated)
	}
	payload, err := r.payload()
	if err != nil || string(payload) != `{"id":"one"}` {
		t.Fatalf("payload=%s error=%v", payload, err)
	}
}

func TestStreamNormalizerSemanticFingerprintAndScrubbing(t *testing.T) {
	a := normalizeTestRecord(t, `{"id":"one","name":"\u0061","nested":{"z":1,"a":[true,null,"\ud83d\ude00"]},"api_key":"DROP","child":{"password":"DROP","safe":"<ok>"}}`, true)
	b := normalizeTestRecord(t, `{"child":{"safe":"<ok>"},"nested":{"a":[true,null,"😀"],"z":1},"name":"a","id":"one"}`, true)
	if a.fingerprint != b.fingerprint || a.identity != b.identity {
		t.Fatal("key or string-escape reordering changed identity/fingerprint")
	}
	data, err := a.payload()
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(data) || bytes.Contains(data, []byte("DROP")) || bytes.Contains(data, []byte{'\n'}) || !bytes.Contains(data, []byte("<ok>")) {
		t.Fatalf("invalid compact/scrubbed data: %s", data)
	}
	wantID := sha256.Sum256([]byte("id:one\x00\""))
	if a.identity != wantID {
		t.Fatal("identity selection changed")
	}
	c := normalizeTestRecord(t, `{"id":"one","name":"a","nested":{"a":[null,true,"😀"],"z":1},"child":{"safe":"<ok>"}}`, true)
	if a.fingerprint == c.fingerprint {
		t.Fatal("array order did not change fingerprint")
	}
}

func TestStreamNormalizerWideObjectsSpillAndRemainOrderInvariant(t *testing.T) {
	const count = 3*memberHashBudget + 17
	var a, b strings.Builder
	a.WriteByte('{')
	b.WriteByte('{')
	for i := 0; i < count; i++ {
		if i > 0 {
			a.WriteByte(',')
			b.WriteByte(',')
		}
		fmt.Fprintf(&a, `"f%d":%d`, i, i)
		fmt.Fprintf(&b, `"f%d":%d`, count-i-1, count-i-1)
	}
	a.WriteByte('}')
	b.WriteByte('}')
	first := normalizeTestRecord(t, a.String(), false)
	second := normalizeTestRecord(t, b.String(), false)
	if first.fingerprint != second.fingerprint {
		t.Fatal("spill changed key-order-independent fingerprint")
	}
	// Duplicate keys split across sorted runs must fail before any write.
	bad := strings.TrimSuffix(a.String(), "}") + `,"f0":2}`
	dir := t.TempDir()
	if r, err := normalizeRecordStream(t.Context(), newResponseScanner(strings.NewReader(bad)), false, dir, int64(entry.MaxDataSize)); err == nil {
		_ = r.close()
		t.Fatal("duplicate key across runs was accepted")
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 0 {
		t.Fatalf("failed-spill scratch files=%v error=%v", files, err)
	}
}

func TestStreamNormalizerValidationAndCleanup(t *testing.T) {
	for _, text := range []string{`null`, `{"a":1,}`, `{"a":[1,]}`, `{"a":"\x"}`, `{"a":01}`, `{"a":true,"\u0061":false}`, `{"a":"unterminated`} {
		t.Run(text, func(t *testing.T) {
			dir := t.TempDir()
			if r, err := normalizeRecordStream(t.Context(), newResponseScanner(strings.NewReader(text)), false, dir, int64(entry.MaxDataSize)); err == nil {
				_ = r.close()
				t.Fatal("invalid record was accepted")
			}
			files, err := os.ReadDir(dir)
			if err != nil || len(files) != 0 {
				t.Fatalf("scratch files=%v error=%v", files, err)
			}
		})
	}
	dir := t.TempDir()
	if r, err := normalizeRecordStream(t.Context(), newResponseScanner(strings.NewReader(`{"a":"more than twenty bytes of data"}`)), false, dir, 20); err == nil || !strings.Contains(err.Error(), "per-record safety limit") {
		if r != nil {
			_ = r.close()
		}
		t.Fatalf("entry limit error=%v", err)
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 0 {
		t.Fatalf("entry-limit scratch files=%v error=%v", files, err)
	}
}

type failingRecordReader struct{ calls int }

func (r *failingRecordReader) Read(p []byte) (int, error) {
	if r.calls == 0 {
		r.calls++
		return copy(p, `{"a":"partial`), nil
	}
	return 0, errors.New("synthetic read failure")
}
func TestStreamNormalizerReadFailureCleansScratch(t *testing.T) {
	dir := t.TempDir()
	if r, err := normalizeRecordStream(t.Context(), newResponseScanner(&failingRecordReader{}), false, dir, int64(entry.MaxDataSize)); err == nil {
		_ = r.close()
		t.Fatal("reader failure was ignored")
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 0 {
		t.Fatalf("scratch files=%v error=%v", files, err)
	}
}

func TestStreamingSecretKeyMatcherMatchesExistingRules(t *testing.T) {
	for _, key := range []string{"data", " credentialData ", "api_key", "s_e-c.r_e_t", "PASSword", "accesstoken", "monkey", "turkey", "keyring", "value", " a . data ", "._data  ", "  credential.data  ", " a\u2003data ", "Key", "da_ta", " ", "__", "data value"} {
		var tracker secretKeyTracker
		for _, r := range key {
			tracker.feed(r)
		}
		if got, want := tracker.secret(), credentialSecretKey(key); got != want {
			t.Errorf("key=%q got %v want %v", key, got, want)
		}
	}
}

func TestStreamNormalizerObjectMemberLimitCountsNestedMembersAndResets(t *testing.T) {
	dir := t.TempDir()
	n := newRecordNormalizer(dir, maxNormalizedRecordSize)
	if n.memberLimit != maxRecordMembers || maxRecordMembers != 250_000 {
		t.Fatalf("default member limit=%d constant=%d want 250000", n.memberLimit, maxRecordMembers)
	}
	n.memberLimit = 5
	defer func() {
		if err := n.cleanup(); err != nil {
			t.Error(err)
		}
	}()

	tooWide := `{"outer":{"a":1,"b":2,"c":3},"d":4,"e":5}`
	if _, err := n.normalize(t.Context(), newResponseScanner(strings.NewReader(tooWide)), false); err == nil || !strings.Contains(err.Error(), "object member limit") {
		t.Fatalf("nested member-limit error=%v", err)
	}

	atLimit := `{"a":1,"b":2,"c":3,"d":4,"e":5}`
	if _, err := n.normalize(t.Context(), newResponseScanner(strings.NewReader(atLimit)), false); err != nil {
		t.Fatalf("record at member limit: %v", err)
	}
	if _, err := n.normalize(t.Context(), newResponseScanner(strings.NewReader(`{"a":1}`)), false); err != nil {
		t.Fatalf("member count was not reset between records: %v", err)
	}
}

func TestMemberHashBudgetReservesOneDigestPerOpenObject(t *testing.T) {
	dir := t.TempDir()
	n := newRecordNormalizer(dir, maxNormalizedRecordSize)
	n.memberLimit = memberHashBudget + 1
	n.members = memberHashBudget
	n.sorters = make([]*memberSorter, memberHashBudget)
	for i := range n.sorters {
		n.sorters[i] = &memberSorter{owner: n, memory: []memberDigest{{}}}
	}

	if err := n.sorters[len(n.sorters)-1].add(memberDigest{}); err != nil {
		t.Fatal(err)
	}
	if entries, err := os.ReadDir(dir); err != nil {
		t.Fatal(err)
	} else if len(entries) != 0 {
		t.Fatalf("adding one digest with one digest per open object created %d spill files", len(entries))
	}
}

func TestStreamNormalizerObjectMemberLimitCountsScrubbedMembers(t *testing.T) {
	n := newRecordNormalizer(t.TempDir(), maxNormalizedRecordSize)
	n.memberLimit = 3
	defer func() {
		if err := n.cleanup(); err != nil {
			t.Error(err)
		}
	}()

	atLimit := `{"password":1,"token":2,"id":"x"}`
	record, err := n.normalize(t.Context(), newResponseScanner(strings.NewReader(atLimit)), true)
	if err != nil {
		t.Fatalf("record at member limit: %v", err)
	}
	if payload, err := record.payload(); err != nil {
		t.Fatal(err)
	} else if string(payload) != `{"id":"x"}` {
		t.Fatalf("record at member limit payload=%s", payload)
	}
	overLimit := `{"password":1,"token":2,"secret":3,"id":"x"}`
	if _, err := n.normalize(t.Context(), newResponseScanner(strings.NewReader(overLimit)), true); err == nil || !strings.Contains(err.Error(), "object member limit") {
		t.Fatalf("scrubbed member-limit error=%v", err)
	}
	nestedOverLimit := `{"password":{"a":1,"b":2,"c":3},"id":"x"}`
	if _, err := n.normalize(t.Context(), newResponseScanner(strings.NewReader(nestedOverLimit)), true); err == nil || !strings.Contains(err.Error(), "object member limit") {
		t.Fatalf("nested scrubbed member-limit error=%v", err)
	}
}

type cancelAfterErrChecks struct {
	context.Context
	checks int
	pass   int
}

func (c *cancelAfterErrChecks) Err() error {
	c.checks++
	if c.checks > c.pass {
		return context.Canceled
	}
	return nil
}

func writeMemberRun(t *testing.T, n *recordNormalizer, start byte) string {
	t.Helper()
	f, err := n.scratch()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2048; i++ {
		var raw [64]byte
		raw[30] = byte(i >> 8)
		raw[31] = byte(i)
		raw[63] = start
		if _, err := f.Write(raw[:]); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return f.Name()
}

func TestMemberHashMergeAndFinalDigestHonorCancellation(t *testing.T) {
	t.Run("merge loop", func(t *testing.T) {
		n := newRecordNormalizer(t.TempDir(), maxNormalizedRecordSize)
		defer n.cleanup()
		n.ctx = &cancelAfterErrChecks{Context: context.Background(), pass: 1}
		s := &memberSorter{owner: n}
		left, right := writeMemberRun(t, n, 1), writeMemberRun(t, n, 2)
		if _, err := s.merge(left, right); !errors.Is(err, context.Canceled) {
			t.Fatalf("merge error=%v want context.Canceled", err)
		}
	})

	t.Run("final digest", func(t *testing.T) {
		n := newRecordNormalizer(t.TempDir(), maxNormalizedRecordSize)
		defer n.cleanup()
		n.ctx = &cancelAfterErrChecks{Context: context.Background(), pass: 0}
		s := &memberSorter{owner: n}
		s.runs[0] = writeMemberRun(t, n, 1)
		if _, err := s.digest(); !errors.Is(err, context.Canceled) {
			t.Fatalf("digest error=%v want context.Canceled", err)
		}
	})
}
