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
	"crypto/md5"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"math/rand"

	"github.com/gravwell/gravwell/v3/ingest/entry"
)

const (
	// The number of times to hash the shared secret
	HASH_ITERATIONS uint16 = 16
	// Auth protocol version number
	VERSION uint16 = 0x9
	// Authenticated, but not ready for ingest
	STATE_AUTHENTICATED uint32 = 0xBEEF42
	// Not authenticated
	STATE_NOT_AUTHENTICATED uint32 = 0xFEED51
	// Authenticated and ready for ingest
	STATE_HOT uint32 = 0xCAFE54

	// Minimum auth version supporting tenants
	MinTenantAuthVersion uint16 = 0x7
	MaxTenantNameLength  uint16 = 512 //maximum length of a tenant name in bytes
	SystemTenant         string = ``  // blank string, basically the root/system/infrastructure user

	// Max length for a state response message
	maxStateResponseLen uint16 = 4096
	// Maximum size of a message requesting tags from ingester
	maxTagRequestLen uint32 = 32 * 1024 * 1024 //32megs for a request, which is crazy huge
	// Maximum size of a message mapping tag names to tag numbers
	maxTagResponseLen uint32 = 64 * 1024 * 1024 //64megs for a response, which is crazy huge

	minPrngLife  = 1024 //how many iterations on the PRNG before we demand a new cryptographically secure seed
	prngVariance = 1024 //how much randomness in the PRNG life we allow

)

var (
	ErrInvalidStateResponseLen = errors.New("Invalid state response length")
	ErrInvalidTagRequestLen    = errors.New("Invalid tag request length")
	ErrInvalidTagResponseLen   = errors.New("Invalid tag response length")
	ErrFailedAuthHashGen       = errors.New("Failed to generate authentication hash")
	ErrFailedAuth              = errors.New("Failed authentication, bad secret")
	ErrFailedTagNegotiation    = errors.New("Failed to negotiate tags")
	ErrShortRead               = errors.New("Failed to read complete buffer")
	ErrShortWrite              = errors.New("Failed to write complete buffer")
	ErrInvalidAuthVersion      = errors.New("auth version is invalid")
	ErrInvalidTenantName       = errors.New("auth tenant name is invalid")
	ErrNilChallengeResponse    = errors.New("Got a nil challenge response")
	ErrTenantAuthUnsupported   = errors.New("authentication endpoint does not support tenants")

	prng        *rand.Rand
	prngCounter int
)

var tenantAuthHeader = [32]byte{
	0x67, 0x72, 0x61, 0x76, 0x77, 0x65, 0x6c, 0x6c,
	0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0,
	0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0,
	0x74, 0x65, 0x6e, 0x61, 0x6e, 0x74, 0x30, 0x31}

// AuthHash represents a hashed shared secret.
type AuthHash [16]byte

// Challenge request, used to validate remote clients.
// The server generates RandChallenge, a random number which is hashed
// with the pre-hashed shared secret, then run through Iterate iterations
// of md5 and sha256 to create the response.
type Challenge struct {
	// Number of times to iterate the hash
	Iterate uint16
	// The random number to be hashed with the secret
	RandChallenge [32]byte
	// Authentication version number
	Version uint16
}

// ChallengeResponse is the resulting hash sent back as part of
// the challenge/response process.
type ChallengeResponse struct {
	Response [32]byte
	Version  uint16
	Tenant   string
}

// TagRequest is used to request tags for the ingester
type TagRequest struct {
	Count uint32
	Tags  []string
}

// TagResponse represents the Tag Name to Tag Number mapping supported by the ingest server
type TagResponse struct {
	Count uint32
	Tags  map[string]entry.EntryTag
}

// StateResponse defines a state ID and associated message when authenticating
type StateResponse struct {
	ID   uint32
	Info string
}

func init() {
	var err error
	prng, err = NewRNG()
	if err != nil {
		panic(err)
	}
	prngCounter = rand.Intn(prngVariance) + minPrngLife
}

// GenAuthHash takes a key and generates a hash using the "password" token
// we iterate over the value, hashing with MD5 and SHA256.  We choose these
// two algorithms because they aren't too heavy, but the alternating makes it
// very difficult to optimize in an FPGA or ASIC.
func GenAuthHash(password string) (AuthHash, error) {
	var runningHash []byte
	var auth AuthHash
	// hash first with SHA512 to ensure we don't accidentally shrink our keyspace
	h512 := sha512.New()
	io.WriteString(h512, password)
	runningHash = h512.Sum(nil)

	for i := uint16(0); i < HASH_ITERATIONS; i++ {
		md := md5.New()
		size, err := md.Write(runningHash)
		if err != nil {
			return auth, err
		}
		if size != len(runningHash) {
			return auth, errors.New("Failed to write to MD5")
		}
		runningHash = md.Sum(nil)
		sh := sha256.New()
		size, err = sh.Write(runningHash)
		if err != nil {
			return auth, err
		}
		if size != len(runningHash) {
			return auth, errors.New("Failed to write to MD5")
		}
		runningHash = sh.Sum(nil)
	}
	for i := 0; i < len(auth); i++ {
		auth[i] = runningHash[i]
	}
	return auth, nil
}

// VerifyResponse takes a hash and challenge and computes a completed response.
// If the computed response does not match an error is returned.  If the response
// matches, nil is returned
func VerifyResponse(auth AuthHash, chal Challenge, resp ChallengeResponse) error {
	vResp, err := GenerateResponse(auth, chal)
	if err != nil {
		return err
	}
	for i := 0; i < len(vResp.Response); i++ {
		if vResp.Response[i] != resp.Response[i] {
			return errors.New("Verification Failed")
		}
	}
	return nil
}

func checkAndReseedPRNG() {
	prngCounter -= 1
	if prngCounter <= 0 {
		if seed, err := SecureSeed(); err == nil {
			prng.Seed(seed)
		}
		prngCounter = rand.Intn(prngVariance) + minPrngLife
	}
}

// NewChallenge generates a random hash string and a random iteration count
func NewChallenge(auth AuthHash) (Challenge, error) {
	var chal [32]byte
	checkAndReseedPRNG()
	iter := uint16(10000 + prng.Intn(10000))
	for i := 0; i < len(chal); i++ {
		chal[i] = byte(prng.Intn(0xff))
	}
	return Challenge{
		Iterate:       iter,
		RandChallenge: chal,
		Version:       VERSION,
	}, nil
}

// GenerateResponse creates a ChallengeResponse based on the Challenge and AuthHash
func GenerateResponse(auth AuthHash, ch Challenge) (resp *ChallengeResponse, err error) {
	resp = &ChallengeResponse{
		Version: ch.Version,
	}
	//hash first with SHA512
	runningHash := make([]byte, 32)
	authSlice := make([]byte, len(auth))
	for i := 0; i < 32; i++ {
		runningHash[i] = ch.RandChallenge[i]
	}
	copy(authSlice, auth[:])
	sha := sha512.New()
	if _, err = sha.Write(runningHash); err != nil {
		return
	} else if _, err = sha.Write(authSlice); err != nil {
		return
	}
	runningHash = sha.Sum(nil)

	for i := uint16(0); i < ch.Iterate; i++ {
		md := md5.New()
		if _, err = md.Write(runningHash); err != nil {
			return
		}
		runningHash = md.Sum(nil)

		sha := sha256.New()
		if _, err = sha.Write(runningHash); err != nil {
			return
		}
		runningHash = sha.Sum(nil)
	}
	//NOTE: The last hash HAS to be SHA256 because we expect
	//a 32 byte result
	for i := 0; i < len(resp.Response); i++ {
		resp.Response[i] = runningHash[i]
	}
	return
}

// Read decodes the ChallengeResponse from the reader
func (cr *ChallengeResponse) Read(r io.Reader) error {
	var buff [32]byte
	if n, err := r.Read(buff[:]); err != nil {
		return err
	} else if n != len(cr.Response) {
		return ErrShortRead
	}
	if buff != tenantAuthHeader {
		cr.Response = buff
		return nil
	}
	if n, err := r.Read(buff[:]); err != nil {
		return err
	} else if n != len(cr.Response) {
		return ErrShortRead
	}
	cr.Response = buff

	return cr.readTenantResponse(r)
}

func (cr *ChallengeResponse) readTenantResponse(r io.Reader) error {
	//read the version and length
	var version uint16
	var length uint16
	if err := binary.Read(r, binary.LittleEndian, &version); err != nil {
		return err
	} else if err = binary.Read(r, binary.LittleEndian, &length); err != nil {
		return err
	}
	if version < MinTenantAuthVersion {
		return ErrInvalidAuthVersion
	} else if length > MaxTenantNameLength {
		return ErrInvalidTenantName
	}
	namebuff := make([]byte, length)
	if n, err := r.Read(namebuff); err != nil {
		return err
	} else if n != int(length) {
		return ErrShortRead
	}
	cr.Version = version
	cr.Tenant = string(namebuff)
	return nil
}

// Write the challenge response to the writer
func (cr *ChallengeResponse) Write(w io.Writer) error {
	if cr.Version < MinTenantAuthVersion || len(cr.Tenant) == 0 {
		return cr.writeNonTenantAuth(w)
	}
	return cr.writeTenantAuth(w)
}

func (cr *ChallengeResponse) writeNonTenantAuth(w io.Writer) error {
	if n, err := w.Write(cr.Response[:]); err != nil {
		return err
	} else if n != len(cr.Response) {
		return ErrShortWrite
	}
	return nil
}

func (cr *ChallengeResponse) writeTenantAuth(w io.Writer) error {
	//double check what we have
	if len(cr.Tenant) > int(MaxTenantNameLength) {
		return ErrInvalidTenantName
	} else if cr.Version < MinTenantAuthVersion {
		return ErrInvalidAuthVersion
	}

	// write header
	if n, err := w.Write(tenantAuthHeader[:]); err != nil {
		return err
	} else if n != len(tenantAuthHeader) {
		return ErrShortWrite
	}

	// then response
	if n, err := w.Write(cr.Response[:]); err != nil {
		return err
	} else if n != len(cr.Response) {
		return ErrShortWrite
	}

	// then version and tenant length and tenant string
	if err := binary.Write(w, binary.LittleEndian, cr.Version); err != nil {
		return err
	} else if err = binary.Write(w, binary.LittleEndian, uint16(len(cr.Tenant))); err != nil {
		return err
	}
	// then tenant name
	return writeString(w, cr.Tenant)
}

func writeString(w io.Writer, v string) error {
	l := len(v)
	if n, err := w.Write([]byte(v)); err != nil {
		return err
	} else if l != n {
		return ErrShortWrite
	}
	return nil
}

// Read the challenge from reader
func (c *Challenge) Read(r io.Reader) error {
	return binary.Read(r, binary.LittleEndian, c)
}

// Write out the challenge to w
func (c *Challenge) Write(w io.Writer) error {
	return binary.Write(w, binary.LittleEndian, c)
}

// Read reads a state response from the reader
func (sr *StateResponse) Read(r io.Reader) error {
	var l uint16
	if err := binary.Read(r, binary.LittleEndian, &l); err != nil {
		return err
	}
	if l > maxStateResponseLen {
		return ErrInvalidStateResponseLen
	}
	bb := make([]byte, int(l))
	if _, err := io.ReadFull(r, bb); err != nil {
		return err
	}
	if err := json.Unmarshal(bb, sr); err != nil {
		return err
	}
	return nil
}

// Write the StateResponse
func (sr *StateResponse) Write(w io.Writer) error {
	bb, err := json.Marshal(sr)
	if err != nil {
		return err
	}
	if int(maxStateResponseLen) < len(bb) {
		return ErrInvalidStateResponseLen
	}
	l := uint16(len(bb))
	if err := binary.Write(w, binary.LittleEndian, l); err != nil {
		return err
	}
	n, err := io.Copy(w, bytes.NewBuffer(bb))
	if err != nil {
		return err
	}
	if uint16(n) != l {
		return errors.New("Failed to write entire state response")
	}
	return nil
}

// Read a TagRequest structure
func (tr *TagRequest) Read(r io.Reader) error {
	var l uint32
	if err := binary.Read(r, binary.LittleEndian, &l); err != nil {
		return err
	}
	if l > maxTagRequestLen {
		return ErrInvalidTagRequestLen
	}
	bb := make([]byte, int(l))
	if _, err := io.ReadFull(r, bb); err != nil {
		return err
	}
	if err := json.Unmarshal(bb, tr); err != nil {
		return err
	}
	return nil
}

// Write a TagRequest
func (tr *TagRequest) Write(w io.Writer) error {
	bs, err := json.Marshal(tr)
	if err != nil {
		return err
	}
	if uint32(len(bs)) > maxTagRequestLen {
		return ErrInvalidTagRequestLen
	}
	if err := binary.Write(w, binary.LittleEndian, uint32(len(bs))); err != nil {
		return err
	}
	if _, err := io.Copy(w, bytes.NewBuffer(bs)); err != nil {
		return err
	}
	return nil
}

// Read a TagResponse
func (tr *TagResponse) Read(r io.Reader) error {
	var l uint32
	if err := binary.Read(r, binary.LittleEndian, &l); err != nil {
		return err
	}
	if l > maxTagRequestLen {
		return ErrInvalidTagResponseLen
	}
	bb := make([]byte, int(l))
	if _, err := io.ReadFull(r, bb); err != nil {
		return err
	}
	if err := json.Unmarshal(bb, tr); err != nil {
		return err
	}
	return nil
}

// Write a TagResponse
func (tr *TagResponse) Write(w io.Writer) error {
	bs, err := json.Marshal(tr)
	if err != nil {
		return err
	}
	if uint32(len(bs)) > maxTagRequestLen {
		return ErrInvalidTagResponseLen
	}
	if err := binary.Write(w, binary.LittleEndian, uint32(len(bs))); err != nil {
		return err
	}
	if _, err := io.Copy(w, bytes.NewBuffer(bs)); err != nil {
		return err
	}
	return nil
}
