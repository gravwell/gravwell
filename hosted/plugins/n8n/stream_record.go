/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package n8n

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"slices"
	"time"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"
)

// Only fixed-width object-member hashes are sorted. This shared budget covers
// every open object, including ancestors of a deeply nested value. Wider
// objects spill sorted runs rather than expanding a generic JSON object graph.
const (
	memberHashBudget        = 4096
	maxNormalizedRecordSize = int64(64 * 1024 * 1024)
	maxRecordMembers        = 250_000
)

var identityFields = []string{"id", "uuid", "eventId", "executionId", "workflowId", "email", "name"}
var timestampFields = []string{"timestamp", "updatedAt", "createdAt", "startedAt", "stoppedAt", "updated_at", "created_at"}

type normalizedRecord struct {
	file                  *os.File
	path                  string
	size, limit           int64
	identity, fingerprint [sha256.Size]byte
	timestamp             time.Time
}

func (r *normalizedRecord) payload() ([]byte, error) {
	if r.size < 0 || r.limit <= 0 || r.size > r.limit || r.size > int64(^uint(0)>>1) {
		return nil, errors.New("invalid normalized n8n entry size")
	}
	data := make([]byte, int(r.size))
	if _, err := io.ReadFull(io.NewSectionReader(r.file, 0, r.size), data); err != nil {
		return nil, errors.New("read normalized n8n record failed")
	}
	return data, nil
}

func (r *normalizedRecord) close() error {
	err := r.file.Close()
	removeErr := os.Remove(r.path)
	if err != nil || (removeErr != nil && !errors.Is(removeErr, os.ErrNotExist)) {
		return errors.New("remove temporary n8n record failed")
	}
	return nil
}

type recordNormalizer struct {
	ctx           context.Context
	scanner       *responseScanner
	output        *os.File
	writer        *bufio.Writer
	size, limit   int64
	scrub         bool
	tempDir       string
	paths         map[string]struct{}
	sorters       []*memberSorter
	members       int
	memberCount   int
	memberLimit   int
	identity      [sha256.Size]byte
	identityRank  int
	timestamp     time.Time
	timestampRank int
}

func newRecordNormalizer(tempDir string, limit int64) *recordNormalizer {
	if limit > maxNormalizedRecordSize {
		limit = maxNormalizedRecordSize
	}
	return &recordNormalizer{tempDir: tempDir, limit: limit, memberLimit: maxRecordMembers, paths: make(map[string]struct{})}
}

// Standalone normalization transfers the scratch file to its caller. Streaming
// collection instead owns one normalizer and reuses its file and write buffer.
func normalizeRecordStream(ctx context.Context, scanner *responseScanner, scrub bool, tempDir string, limit int64) (result *normalizedRecord, err error) {
	n := newRecordNormalizer(tempDir, limit)
	defer func() { err = errors.Join(err, n.cleanup()) }()
	result, err = n.normalize(ctx, scanner, scrub)
	if err == nil {
		delete(n.paths, result.path)
		n.output = nil
	}
	return
}

func (n *recordNormalizer) normalize(ctx context.Context, scanner *responseScanner, scrub bool) (*normalizedRecord, error) {
	n.ctx, n.scanner, n.scrub = ctx, scanner, scrub
	n.identity, n.timestamp = [sha256.Size]byte{}, time.Time{}
	n.identityRank, n.timestampRank = len(identityFields), len(timestampFields)
	clear(n.sorters[:cap(n.sorters)])
	n.sorters, n.members, n.memberCount = n.sorters[:0], 0, 0
	if n.output == nil {
		file, err := n.scratch()
		if err != nil {
			return nil, err
		}
		n.output, n.writer = file, bufio.NewWriterSize(file, 64*1024)
	} else if err := n.rewind(0); err != nil {
		return nil, err
	}
	if !scanner.nextIs('{') {
		return nil, errors.New("expected a JSON object")
	}
	fingerprint, err := n.value(0, nil)
	if err != nil {
		return nil, err
	}
	if err := n.writer.Flush(); err != nil {
		return nil, errors.New("write normalized n8n record failed")
	}
	if n.identityRank == len(identityFields) {
		text := "sha256:" + hex.EncodeToString(fingerprint[:])
		n.identity = sha256.Sum256([]byte(text))
	}
	if n.timestampRank == len(timestampFields) {
		n.timestamp = time.Now().UTC()
	}
	result := &normalizedRecord{file: n.output, path: n.output.Name(), size: n.size, limit: n.limit,
		identity: n.identity, fingerprint: fingerprint, timestamp: n.timestamp}
	return result, nil
}

func (n *recordNormalizer) scratch() (*os.File, error) {
	f, err := os.CreateTemp(n.tempDir, "gravwell-n8n-record-*")
	if err != nil {
		return nil, errors.New("create temporary n8n record failed")
	}
	n.paths[f.Name()] = struct{}{}
	return f, nil
}

func (n *recordNormalizer) cleanup() error {
	var cleanupErr error
	if n.output != nil {
		if err := n.output.Close(); err != nil {
			cleanupErr = errors.New("close temporary n8n record failed")
		}
	}
	for path := range n.paths {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			cleanupErr = errors.New("remove temporary n8n record failed")
		}
	}
	return cleanupErr
}

func (n *recordNormalizer) cancelled() error {
	if n.ctx == nil {
		return nil
	}
	return n.ctx.Err()
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if r.ctx != nil {
		if err := r.ctx.Err(); err != nil {
			return 0, err
		}
	}
	return r.r.Read(p)
}

func (n *recordNormalizer) write(data []byte) error {
	if int64(len(data)) > n.limit-n.size {
		return fmt.Errorf("n8n record exceeds the per-record safety limit of %d bytes", n.limit)
	}
	if _, err := n.writer.Write(data); err != nil {
		return errors.New("write normalized n8n record failed")
	}
	n.size += int64(len(data))
	return nil
}

func (n *recordNormalizer) rewind(offset int64) error {
	if err := n.writer.Flush(); err != nil {
		return errors.New("write normalized n8n record failed")
	}
	if err := n.output.Truncate(offset); err != nil {
		return errors.New("truncate normalized n8n record failed")
	}
	if _, err := n.output.Seek(offset, io.SeekStart); err != nil {
		return errors.New("seek normalized n8n record failed")
	}
	n.writer.Reset(n.output)
	n.size = offset
	return nil
}

func sumHash(h hash.Hash) (result [sha256.Size]byte) { h.Sum(result[:0]); return }

func (n *recordNormalizer) value(depth int, capture *scalarCapture) ([sha256.Size]byte, error) {
	if depth > 10000 {
		return [sha256.Size]byte{}, errors.New("JSON nesting limit exceeded")
	}
	if err := n.scanner.space(); err != nil {
		return [sha256.Size]byte{}, err
	}
	peek, err := n.scanner.reader.Peek(1)
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	switch peek[0] {
	case '{':
		return n.object(depth)
	case '[':
		return n.array(depth)
	case '"':
		if capture != nil {
			capture.kind = '"'
		}
		return n.stringValue(capture, nil)
	default:
		h := sha256.New()
		_, _ = h.Write([]byte("scalar\x00"))
		var single [1]byte
		read := func() (byte, error) {
			b, err := n.scanner.reader.ReadByte()
			if err != nil {
				return 0, err
			}
			single[0] = b
			if err := n.write(single[:]); err != nil {
				return 0, err
			}
			_, _ = h.Write(single[:])
			if capture != nil {
				capture.feed(single[:])
			}
			return b, nil
		}
		var parseErr error
		switch peek[0] {
		case 't':
			parseErr = scanLiteral("true", read)
		case 'f':
			parseErr = scanLiteral("false", read)
		case 'n':
			parseErr = scanLiteral("null", read)
		default:
			if capture != nil {
				capture.kind = '0'
			}
			parseErr = scanJSONNumber(n.scanner.reader, read)
		}
		return sumHash(h), parseErr
	}
}

func (n *recordNormalizer) object(depth int) ([sha256.Size]byte, error) {
	if err := n.scanner.expect('{'); err != nil {
		return [sha256.Size]byte{}, err
	}
	if err := n.write([]byte{'{'}); err != nil {
		return [sha256.Size]byte{}, err
	}
	sorter := &memberSorter{owner: n}
	n.sorters = append(n.sorters, sorter)
	defer func() { n.members -= len(sorter.memory); n.sorters = n.sorters[:len(n.sorters)-1] }()
	written := 0
	for !n.scanner.nextIs('}') {
		start := n.size
		if written > 0 {
			if err := n.write([]byte{','}); err != nil {
				return [sha256.Size]byte{}, err
			}
		}
		key := &keyCapture{}
		keyDigest, err := n.stringValue(nil, key)
		if err != nil {
			return [sha256.Size]byte{}, err
		}
		if err := n.scanner.expect(':'); err != nil {
			return [sha256.Size]byte{}, err
		}
		if err := n.write([]byte{':'}); err != nil {
			return [sha256.Size]byte{}, err
		}
		if n.memberCount >= n.memberLimit {
			return [sha256.Size]byte{}, fmt.Errorf("n8n record exceeds the %d object member limit", n.memberLimit)
		}
		n.memberCount++
		if n.scrub && key.secret.secret() {
			if err := n.rewind(start); err != nil {
				return [sha256.Size]byte{}, err
			}
			if _, _, err := n.scanner.valueWithObjectMembers(0, false, &n.memberCount, n.memberLimit); err != nil {
				return [sha256.Size]byte{}, err
			}
		} else {
			var capture *scalarCapture
			name := key.name()
			if depth == 0 {
				capture = newScalarCapture(name)
			}
			child, err := n.value(depth+1, capture)
			if err != nil {
				return [sha256.Size]byte{}, err
			}
			if err := sorter.add(memberDigest{key: keyDigest, value: child}); err != nil {
				return [sha256.Size]byte{}, err
			}
			if capture != nil {
				n.takeCapture(name, capture)
			}
			written++
		}
		if n.scanner.nextIs('}') {
			break
		}
		if err := n.scanner.expect(','); err != nil {
			return [sha256.Size]byte{}, err
		}
		if n.scanner.nextIs('}') {
			return [sha256.Size]byte{}, errors.New("invalid JSON object separator")
		}
	}
	if err := n.scanner.expect('}'); err != nil {
		return [sha256.Size]byte{}, err
	}
	if err := n.write([]byte{'}'}); err != nil {
		return [sha256.Size]byte{}, err
	}
	return sorter.digest()
}

func (n *recordNormalizer) array(depth int) ([sha256.Size]byte, error) {
	if err := n.scanner.expect('['); err != nil {
		return [sha256.Size]byte{}, err
	}
	if err := n.write([]byte{'['}); err != nil {
		return [sha256.Size]byte{}, err
	}
	h := sha256.New()
	_, _ = h.Write([]byte("array\x00"))
	count := uint64(0)
	for !n.scanner.nextIs(']') {
		if count > 0 {
			if err := n.write([]byte{','}); err != nil {
				return [sha256.Size]byte{}, err
			}
		}
		child, err := n.value(depth+1, nil)
		if err != nil {
			return [sha256.Size]byte{}, err
		}
		_, _ = h.Write(child[:])
		count++
		if n.scanner.nextIs(']') {
			break
		}
		if err := n.scanner.expect(','); err != nil {
			return [sha256.Size]byte{}, err
		}
		if n.scanner.nextIs(']') {
			return [sha256.Size]byte{}, errors.New("invalid JSON array separator")
		}
	}
	if err := n.scanner.expect(']'); err != nil {
		return [sha256.Size]byte{}, err
	}
	if err := n.write([]byte{']'}); err != nil {
		return [sha256.Size]byte{}, err
	}
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], count)
	_, _ = h.Write(size[:])
	return sumHash(h), nil
}

// Strings are decoded in chunks. Neither a giant string nor a giant property
// name is materialized as a Go string; only the emitted file and hashes grow.
func (n *recordNormalizer) stringValue(capture *scalarCapture, key *keyCapture) ([sha256.Size]byte, error) {
	if err := n.scanner.expect('"'); err != nil {
		return [sha256.Size]byte{}, err
	}
	h := sha256.New()
	_, _ = h.Write([]byte("string\x00"))
	emit := func(encoded, decoded []byte) error {
		if err := n.write(encoded); err != nil {
			return err
		}
		_, _ = h.Write(encoded)
		if capture != nil {
			capture.feed(decoded)
		}
		if key != nil {
			key.feed(decoded)
		}
		return nil
	}
	if err := emit([]byte{'"'}, nil); err != nil {
		return [sha256.Size]byte{}, err
	}
	var decoded [utf8.UTFMax]byte
	var encoded [6]byte
	for {
		if _, err := n.scanner.reader.Peek(1); err != nil {
			return [sha256.Size]byte{}, err
		}
		chunk, _ := n.scanner.reader.Peek(n.scanner.reader.Buffered())
		length := 0
		for length < len(chunk) && chunk[length] >= 0x20 && chunk[length] < utf8.RuneSelf && chunk[length] != '"' && chunk[length] != '\\' {
			length++
		}
		if length > 0 {
			if err := emit(chunk[:length], chunk[:length]); err != nil {
				return [sha256.Size]byte{}, err
			}
			_, _ = n.scanner.reader.Discard(length)
			continue
		}
		peek, _ := n.scanner.reader.Peek(1)
		var r rune
		if peek[0] == '"' {
			_, _ = n.scanner.reader.ReadByte()
			if err := emit([]byte{'"'}, nil); err != nil {
				return [sha256.Size]byte{}, err
			}
			return sumHash(h), nil
		} else if peek[0] == '\\' {
			_, _ = n.scanner.reader.ReadByte()
			escape, err := n.scanner.reader.ReadByte()
			if err != nil {
				return [sha256.Size]byte{}, err
			}
			switch escape {
			case '"', '\\', '/':
				r = rune(escape)
			case 'b':
				r = '\b'
			case 'f':
				r = '\f'
			case 'n':
				r = '\n'
			case 'r':
				r = '\r'
			case 't':
				r = '\t'
			case 'u':
				r, err = readHexRune(n.scanner.reader)
				if err != nil {
					return [sha256.Size]byte{}, err
				}
				if utf16.IsSurrogate(r) {
					second, err := n.scanner.reader.Peek(6)
					if err == nil && r >= 0xd800 && r <= 0xdbff && second[0] == '\\' && second[1] == 'u' {
						low, parseErr := parseHexRune(second[2:])
						if parseErr == nil && low >= 0xdc00 && low <= 0xdfff {
							_, _ = n.scanner.reader.Discard(6)
							r = utf16.DecodeRune(r, low)
						} else {
							r = utf8.RuneError
						}
					} else {
						r = utf8.RuneError
					}
				}
			default:
				return [sha256.Size]byte{}, errors.New("invalid JSON escape")
			}
		} else {
			var err error
			r, _, err = n.scanner.reader.ReadRune()
			if err != nil {
				return [sha256.Size]byte{}, err
			}
			if r < 0x20 {
				return [sha256.Size]byte{}, errors.New("invalid control byte in JSON string")
			}
		}
		decodedSize := utf8.EncodeRune(decoded[:], r)
		var output []byte
		switch r {
		case '"', '\\':
			encoded[0], encoded[1] = '\\', byte(r)
			output = encoded[:2]
		case '\b':
			output = []byte(`\b`)
		case '\f':
			output = []byte(`\f`)
		case '\n':
			output = []byte(`\n`)
		case '\r':
			output = []byte(`\r`)
		case '\t':
			output = []byte(`\t`)
		default:
			if r < 0x20 || r == 0x2028 || r == 0x2029 {
				const digits = "0123456789abcdef"
				encoded = [6]byte{'\\', 'u', digits[(r>>12)&15], digits[(r>>8)&15], digits[(r>>4)&15], digits[r&15]}
				output = encoded[:]
			} else {
				output = decoded[:decodedSize]
			}
		}
		if err := emit(output, decoded[:decodedSize]); err != nil {
			return [sha256.Size]byte{}, err
		}
	}
}

func readHexRune(reader *bufio.Reader) (rune, error) {
	var text [4]byte
	if _, err := io.ReadFull(reader, text[:]); err != nil {
		return 0, err
	}
	return parseHexRune(text[:])
}

func parseHexRune(text []byte) (rune, error) {
	var result rune
	for _, b := range text {
		result <<= 4
		switch {
		case b >= '0' && b <= '9':
			result |= rune(b - '0')
		case b >= 'a' && b <= 'f':
			result |= rune(b - 'a' + 10)
		case b >= 'A' && b <= 'F':
			result |= rune(b - 'A' + 10)
		default:
			return 0, errors.New("invalid JSON unicode escape")
		}
	}
	return result, nil
}

type scalarCapture struct {
	identity hash.Hash
	preview  [128]byte
	size     int
	overflow bool
	kind     byte
	nonempty bool
}

func newScalarCapture(name string) *scalarCapture {
	i, t := slices.Index(identityFields, name), slices.Index(timestampFields, name)
	if i < 0 && t < 0 {
		return nil
	}
	c := &scalarCapture{}
	if i >= 0 {
		c.identity = sha256.New()
		_, _ = c.identity.Write([]byte(name + ":"))
	}
	return c
}

func (c *scalarCapture) feed(data []byte) {
	if len(data) > 0 {
		c.nonempty = true
	}
	if c.identity != nil {
		_, _ = c.identity.Write(data)
	}
	if len(data) > len(c.preview)-c.size {
		c.overflow = true
		return
	}
	copy(c.preview[c.size:], data)
	c.size += len(data)
}

func (n *recordNormalizer) takeCapture(name string, c *scalarCapture) {
	if i := slices.Index(identityFields, name); i >= 0 && i < n.identityRank && c.identity != nil && (c.kind == '0' || c.kind == '"' && c.nonempty) {
		// Domain-separate decoded strings from numeric scalar spellings.
		_, _ = c.identity.Write([]byte{0, c.kind})
		n.identity, n.identityRank = sumHash(c.identity), i
	}
	if i := slices.Index(timestampFields, name); i >= 0 && i < n.timestampRank && !c.overflow {
		text := string(c.preview[:c.size])
		var parsed time.Time
		valid := false
		if c.kind == '"' {
			var err error
			parsed, err = time.Parse(time.RFC3339Nano, text)
			valid = err == nil
		}
		if c.kind == '0' {
			if seconds, err := json.Number(text).Int64(); err == nil {
				parsed = time.Unix(seconds, 0)
				valid = true
			}
		}
		if valid {
			parsed = parsed.UTC()
			valid = !parsed.Before(time.Unix(0, 0).UTC()) && !parsed.After(time.Now().UTC().Add(24*time.Hour))
		}
		if valid {
			n.timestamp, n.timestampRank = parsed, i
		}
	}
}

type keyCapture struct {
	text     [32]byte
	size     int
	overflow bool
	secret   secretKeyTracker
}

func (k *keyCapture) feed(data []byte) {
	if len(data) > len(k.text)-k.size {
		k.overflow = true
	} else if !k.overflow {
		copy(k.text[k.size:], data)
		k.size += len(data)
	}
	for len(data) > 0 {
		r, size := utf8.DecodeRune(data)
		k.secret.feed(r)
		data = data[size:]
	}
}
func (k *keyCapture) name() string {
	if k.overflow {
		return ""
	}
	return string(k.text[:k.size])
}

// Matches credentialSecretKey without retaining an arbitrarily long key.
type secretKeyTracker struct {
	started, pendingSpace, matched bool
	suffix                         [14]rune
	size                           int
	exact                          [15]rune
	exactSize                      int
}

func (k *secretKeyTracker) normalized(r rune) {
	if r == '_' || r == '-' || r == '.' {
		return
	}
	r = unicode.ToLower(r)
	if k.exactSize < len(k.exact) {
		k.exact[k.exactSize] = r
		k.exactSize++
	}
	if k.size == len(k.suffix) {
		copy(k.suffix[:], k.suffix[1:])
		k.size--
	}
	k.suffix[k.size] = r
	k.size++
	for _, word := range []string{"secret", "password", "token", "key"} {
		if len(word) > k.size {
			continue
		}
		match := true
		for i := range word {
			if k.suffix[k.size-len(word)+i] != rune(word[i]) {
				match = false
				break
			}
		}
		if match {
			k.matched = true
		}
	}
}
func (k *secretKeyTracker) feed(r rune) {
	if unicode.IsSpace(r) {
		if k.started {
			k.pendingSpace = true
		}
		return
	}
	if k.pendingSpace {
		k.normalized(' ')
		k.pendingSpace = false
	}
	k.started = true
	k.normalized(r)
}
func (k *secretKeyTracker) secret() bool {
	text := string(k.exact[:k.exactSize])
	return k.matched || text == "data" || text == "credentialdata"
}

type memberDigest struct{ key, value [sha256.Size]byte }
type memberSorter struct {
	owner  *recordNormalizer
	memory []memberDigest
	runs   [64]string
}

func (s *memberSorter) add(member memberDigest) error {
	// One digest per open object is unavoidable until that object closes. Keep
	// the shared spill budget for additional digests so deep nesting cannot
	// force a singleton spill for nearly every parsed member.
	if s.owner.members >= memberHashBudget+len(s.owner.sorters) {
		var largest *memberSorter
		for _, candidate := range s.owner.sorters {
			if largest == nil || len(candidate.memory) > len(largest.memory) {
				largest = candidate
			}
		}
		if largest != nil {
			if err := largest.spill(); err != nil {
				return err
			}
		}
	}
	s.memory = append(s.memory, member)
	s.owner.members++
	return nil
}

func sortMembers(members []memberDigest) error {
	slices.SortFunc(members, func(a, b memberDigest) int { return bytes.Compare(a.key[:], b.key[:]) })
	for i := 1; i < len(members); i++ {
		if members[i-1].key == members[i].key {
			return errors.New("duplicate JSON object key")
		}
	}
	return nil
}

func (s *memberSorter) spill() error {
	if len(s.memory) == 0 {
		return nil
	}
	if err := s.owner.cancelled(); err != nil {
		return err
	}
	if err := sortMembers(s.memory); err != nil {
		return err
	}
	f, err := s.owner.scratch()
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	for _, m := range s.memory {
		if _, err = w.Write(m.key[:]); err != nil {
			break
		}
		if _, err = w.Write(m.value[:]); err != nil {
			break
		}
	}
	flushErr, closeErr := w.Flush(), f.Close()
	if err != nil || flushErr != nil || closeErr != nil {
		return errors.New("write temporary n8n member hashes failed")
	}
	s.owner.members -= len(s.memory)
	s.memory = nil
	path := f.Name()
	for level := range s.runs {
		if s.runs[level] == "" {
			s.runs[level] = path
			return nil
		}
		path, err = s.merge(s.runs[level], path)
		if err != nil {
			return err
		}
		s.runs[level] = ""
	}
	return errors.New("n8n member hash sort overflow")
}

func readMember(r io.Reader) (m memberDigest, err error) {
	var raw [64]byte
	if _, err = io.ReadFull(r, raw[:]); err != nil {
		return
	}
	copy(m.key[:], raw[:32])
	copy(m.value[:], raw[32:])
	return
}

func (s *memberSorter) merge(leftPath, rightPath string) (string, error) {
	if err := s.owner.cancelled(); err != nil {
		return "", err
	}
	left, err := os.Open(leftPath)
	if err != nil {
		return "", errors.New("open temporary n8n member hashes failed")
	}
	defer left.Close()
	right, err := os.Open(rightPath)
	if err != nil {
		return "", errors.New("open temporary n8n member hashes failed")
	}
	defer right.Close()
	out, err := s.owner.scratch()
	if err != nil {
		return "", err
	}
	w := bufio.NewWriter(out)
	lr, rr := bufio.NewReader(left), bufio.NewReader(right)
	a, ae := readMember(lr)
	b, be := readMember(rr)
	merged := 0
	for ae == nil || be == nil {
		if merged&1023 == 0 {
			if err := s.owner.cancelled(); err != nil {
				_ = out.Close()
				return "", err
			}
		}
		merged++
		if ae != nil && !errors.Is(ae, io.EOF) || be != nil && !errors.Is(be, io.EOF) {
			_ = out.Close()
			return "", errors.New("read temporary n8n member hashes failed")
		}
		var chosen memberDigest
		if ae == nil && be == nil && a.key == b.key {
			_ = out.Close()
			return "", errors.New("duplicate JSON object key")
		}
		if be != nil || ae == nil && bytes.Compare(a.key[:], b.key[:]) < 0 {
			chosen = a
			a, ae = readMember(lr)
		} else {
			chosen = b
			b, be = readMember(rr)
		}
		if _, err = w.Write(chosen.key[:]); err == nil {
			_, err = w.Write(chosen.value[:])
		}
		if err != nil {
			_ = out.Close()
			return "", errors.New("write temporary n8n member hashes failed")
		}
	}
	flushErr, closeErr := w.Flush(), out.Close()
	if flushErr != nil || closeErr != nil || !errors.Is(ae, io.EOF) || !errors.Is(be, io.EOF) {
		return "", errors.New("merge temporary n8n member hashes failed")
	}
	if left.Close() != nil || right.Close() != nil {
		return "", errors.New("close temporary n8n member hashes failed")
	}
	if err := os.Remove(leftPath); err != nil {
		return "", errors.New("remove temporary n8n member hashes failed")
	}
	delete(s.owner.paths, leftPath)
	if err := os.Remove(rightPath); err != nil {
		return "", errors.New("remove temporary n8n member hashes failed")
	}
	delete(s.owner.paths, rightPath)
	return out.Name(), nil
}

func (s *memberSorter) digest() ([sha256.Size]byte, error) {
	h := sha256.New()
	_, _ = h.Write([]byte("object\x00"))
	hasRuns := false
	for _, path := range s.runs {
		hasRuns = hasRuns || path != ""
	}
	if !hasRuns {
		if err := sortMembers(s.memory); err != nil {
			return [sha256.Size]byte{}, err
		}
		for _, member := range s.memory {
			_, _ = h.Write(member.key[:])
			_, _ = h.Write(member.value[:])
		}
		return sumHash(h), nil
	}
	if err := s.spill(); err != nil {
		return [sha256.Size]byte{}, err
	}
	var path string
	for i, run := range s.runs {
		if run == "" {
			continue
		}
		if path == "" {
			path = run
		} else {
			var err error
			path, err = s.merge(path, run)
			if err != nil {
				return [sha256.Size]byte{}, err
			}
		}
		s.runs[i] = ""
	}
	f, err := os.Open(path)
	if err != nil {
		return [sha256.Size]byte{}, errors.New("open temporary n8n member hashes failed")
	}
	_, err = io.Copy(h, contextReader{ctx: s.owner.ctx, r: f})
	closeErr := f.Close()
	removeErr := os.Remove(path)
	if removeErr == nil {
		delete(s.owner.paths, path)
	}
	if err != nil {
		return [sha256.Size]byte{}, fmt.Errorf("consume temporary n8n member hashes failed: %w", err)
	}
	if closeErr != nil || removeErr != nil {
		return [sha256.Size]byte{}, errors.New("consume temporary n8n member hashes failed")
	}
	return sumHash(h), nil
}
