/*************************************************************************
 *
 * Gravwell - "Consume all the things!"
 *
 * ________________________________________________________________________
 *
 * Copyright 2017 - All Rights Reserved
 * Gravwell Inc <legal@gravwell.io>
 * ________________________________________________________________________
 *
 * NOTICE:  This code is part of the Gravwell project and may not be shared,
 * published, sold, or otherwise distributed in any from without the express
 * written consent of its owners.
 *
 **************************************************************************/
package ingest

import (
	"bytes"
	"crypto/md5"
	crand "crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"math/rand"

	"github.com/gravwell/ingest/entry"
)

const (
	HASH_ITERATIONS         uint16 = 16
	VERSION                 uint16 = 0x1
	STATE_AUTHENTICATED     uint32 = 0xBEEF42
	STATE_NOT_AUTHENTICATED uint32 = 0xFEED51
	STATE_HOT               uint32 = 0xCAFE54 //ready to roll

	maxStateResponseLen uint16 = 4096
	maxTagRequestLen    uint32 = 32 * 1024 * 1024 //32megs for a request, which is crazy huge
	maxTagResponseLen   uint32 = 64 * 1024 * 1024 //64megs for a request, which is crazy huge
)

var (
	errInvalidStateResponseLen = errors.New("Invalid state response length")
	errInvalidTagRequestLen    = errors.New("Invalid tag request length")
	errInvalidTagResponseLen   = errors.New("Invalid tag response length")

	prng *rand.Rand
)

type AuthHash [16]byte

// Challenge request, used to validate remote clients
type Challenge struct {
	Iterate       uint16
	RandChallenge [32]byte
	Version       uint16
}

// ChallengeResponse the resulting hash sent back as part of the challenge
type ChallengeResponse struct {
	Response [32]byte
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

// StateResponse
type StateResponse struct {
	ID   uint32
	Info string
}

func init() {
	//seed the random number generator with a cryptographically secure seed
	//we use crypto/rand to generate this initial seed
	//the cheap way of doing it is to ask for 8 bytes of random
	//cast it to an int64 and seed math/rand
	v := make([]byte, 8)
	if _, err := crand.Read(v); err != nil {
		panic("Failed to get random seed " + err.Error())
	}
	prng = rand.New(rand.NewSource(int64(binary.LittleEndian.Uint64(v))))
}

/* GenAuthHash takes a key and generates a hash using the "password" token
   we iterate over the value, hashing with MD5 and SHA256.  We choose these
   two algorithms because they aren't too heavy, but the alternating makes it
   very difficult to optimize in an FPGA or ASIC.  */
func GenAuthHash(password string) (AuthHash, error) {
	var runningHash []byte
	var auth AuthHash
	/* hash first with SHA512 to ensure we don't accidentially shrink our keyspace */
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

// NewChallenge generates a random hash string and a random iteration count
func NewChallenge(auth AuthHash) (Challenge, error) {
	var chal [32]byte
	iter := uint16(10000 + prng.Intn(10000))
	for i := 0; i < len(chal); i++ {
		chal[i] = byte(prng.Intn(0xff))
	}
	return Challenge{iter, chal, VERSION}, nil
}

// GenerateResponse creates a ChallengeResponse based on the Challenge and AuthHash
func GenerateResponse(auth AuthHash, ch Challenge) (*ChallengeResponse, error) {
	var resp ChallengeResponse
	//hash first with SHA512
	runningHash := make([]byte, 32)
	authSlice := make([]byte, len(auth))
	for i := 0; i < 32; i++ {
		runningHash[i] = ch.RandChallenge[i]
	}
	for i := 0; i < len(auth); i++ {
		authSlice[i] = auth[i]
	}
	sha := sha512.New()
	_, err := sha.Write(runningHash)
	if err != nil {
		return nil, err
	}
	_, err = sha.Write(authSlice)
	if err != nil {
		return nil, err
	}
	runningHash = sha.Sum(nil)
	if err != nil {
		return nil, err
	}

	for i := uint16(0); i < ch.Iterate; i++ {
		md := md5.New()
		_, err := md.Write(runningHash)
		if err != nil {
			return nil, err
		}
		runningHash = md.Sum(nil)

		sha := sha256.New()
		_, err = sha.Write(runningHash)
		if err != nil {
			return nil, err
		}
		runningHash = sha.Sum(nil)
	}
	//NOTE: The last hash HAS to be SHA256 because we expect
	//a 32 byte result
	for i := 0; i < len(resp.Response); i++ {
		resp.Response[i] = runningHash[i]
	}
	return &resp, nil
}

// Decode the ChallengeResponse form the reader
func (cr *ChallengeResponse) Read(r io.Reader) error {
	return binary.Read(r, binary.LittleEndian, cr)
}

// Write the challenge response to the writer
func (cr *ChallengeResponse) Write(w io.Writer) error {
	return binary.Write(w, binary.LittleEndian, cr)
}

// Read the chellenge from reader
func (c *Challenge) Read(r io.Reader) error {
	return binary.Read(r, binary.LittleEndian, c)
}

// Write the challenge challnge
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
		return errInvalidStateResponseLen
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
		return errInvalidStateResponseLen
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
		return errInvalidTagRequestLen
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
		return errInvalidTagRequestLen
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
		return errInvalidTagResponseLen
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
		return errInvalidTagResponseLen
	}
	if err := binary.Write(w, binary.LittleEndian, uint32(len(bs))); err != nil {
		return err
	}
	if _, err := io.Copy(w, bytes.NewBuffer(bs)); err != nil {
		return err
	}
	return nil
}
