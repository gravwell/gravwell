/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package kits

import (
	"archive/tar"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"path/filepath"

	"encoding/hex"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/ingesters/utils"
)

var (
	ErrFailedSeek  = errors.New("failed to seek on reader")
	ErrNotOpen     = errors.New("Kit Reader is not open")
	ErrNotVerified = errors.New("Kit Reader has not verified the kit")
)

// Reader is used to extract kit archives for processing and installation.
// A typical workflow is:
//
// • Instantiate Reader using NewReader
//
// • Call Verify method to ensure kit file is valid
//
// • Optionally call Signed method to ensure kit file was signed
//
// • Call Process method with a callback function to extract items from kit.
type Reader struct {
	rdr      utils.ReadResetCloser
	verify   SigVerificationFunc
	manifest Manifest
	verified bool
	signed   bool
	sigerr   error // any error from signing
}

// NewReader returns a Reader which will parse a kit from the given ReadResetCloser.
// Note that rdr is a ReadResetCloser; the Reset function is used to reset the reader to
// the beginning of the stream. The github.com/gravwell/gravwell/v4/ingesters/utils package
// includes several convenient ReadResetCloser implementations.
//
// The sigVerify parameter is an optional function used to validate the kit's manifest signature.
// The function will be called with the manifest and signature passed as slices of bytes;
// it is the responsibility of the user to implement signature validation. Pass 'nil' to
// disable signature verification.
func NewReader(rdr utils.ReadResetCloser, sigVerify SigVerificationFunc) (rp *Reader, err error) {
	if err = rdr.Reset(); err != nil {
		return
	}
	rp = &Reader{
		rdr:    rdr,
		verify: sigVerify,
	}
	return
}

// Manifest returns the manifest object for the kit. It will return
// an error if the reader is not properly initialized or if the Verify
// function has not been called.
func (rp *Reader) Manifest() (m Manifest, err error) {
	if rp.rdr == nil {
		err = ErrNotOpen
	} else if !rp.verified {
		err = ErrNotVerified
	} else {
		m = rp.manifest
	}
	return
}

// Signed returns true if the kit has been signed. It will return an error if
// the reader has not been initialized, if the Verify method has not been
// previously called, or if there was a problem with the kit signature.
func (rp *Reader) Signed() (signed bool, err error) {
	err = rp.sigerr
	if rp.rdr == nil {
		err = ErrNotOpen
	} else if !rp.verified {
		err = ErrNotVerified
	} else {
		signed = rp.signed
	}
	return
}

// Verify validates the contents of the kit and prepares the Reader for use.
// It calls the Verify function to extract the kit's manifest and check the signature.
// Note that Verify does not return an error if the kit signature is invalid, because
// a kit should be able to pass basic verification and still fail the sig check;
// use the Signed function to check that.
func (rp *Reader) Verify() (err error) {
	if rp.rdr == nil {
		err = ErrNotOpen
		return
	}
	if err = rp.rdr.Reset(); err != nil {
		return
	}

	rp.signed, rp.manifest, rp.sigerr, err = Verify(rp.rdr, rp.verify)
	if err == nil {
		rp.verified = true
	}
	return
}

// CallbackFunc is the function type which is passed to the Process method. The function
// will be called for each Item in the kit. The item itself can be read from the io.Reader.
type CallbackFunc func(itm types.KitItem, rdr io.Reader) error

// Process walks the contents of the kit, extracting individual items and calling
// the CallbackFunc for each item. If the callback returns an error, Process will terminate
// early.
func (rp *Reader) Process(cb CallbackFunc) (err error) {
	if rp.rdr == nil {
		err = ErrNotOpen
		return
	} else if !rp.verified {
		err = ErrNotVerified
		return
	}
	if err = rp.rdr.Reset(); err != nil {
		return
	}
	var item types.KitItem
	var hdr *tar.Header
	tr := tar.NewReader(rp.rdr)
	for {
		if hdr, err = tr.Next(); err != nil {
			if err == io.EOF {
				break
			}
			return
		}
		nm := filepath.Base(hdr.Name)
		if nm == ManifestName || nm == ManifestSigName {
			continue
		}
		if item, err = rp.getItem(nm); err != nil {
			return err
		}
		if err = cb(item, tr); err != nil {
			return err
		}
	}
	return nil
}

func (rp *Reader) getItem(n string) (item types.KitItem, err error) {
	for _, v := range rp.manifest.Items {
		if v.Filename() == n {
			item = v
			return
		}
	}
	err = fmt.Errorf("%s not found in manifest", n)
	return
}

// SigVerificationFunc is the function type used to validate a manifest signature. It is
// passed the manifest and signature as slices of bytes. For standard public-key signature
// verification, use a lambda function which captures the key.
type SigVerificationFunc func(manifest []byte, sig []byte) error

// Verify reads a kit from the rdr and checks that all items are valid. If sigVerify is not
// nil, it will be called to verify the manifest signature.
// It returns two errors, one from the signature verification function and
// one for all other errors.
func Verify(rdr io.Reader, sigVerify SigVerificationFunc) (signed bool, manifest Manifest, sigerr error, err error) {
	fileHashes := map[string]string{}
	var m, s []byte
	var hdr *tar.Header

	tr := tar.NewReader(rdr)
	for {
		if hdr, err = tr.Next(); err != nil {
			if err == io.EOF {
				break
			}
			return
		}
		nm := filepath.Base(hdr.Name)
		if nm == ManifestName {
			lr := io.LimitedReader{
				R: tr,
				N: maxManifestSize,
			}
			if m, err = io.ReadAll(&lr); err != nil {
				return
			}
		} else if nm == ManifestSigName {
			lr := io.LimitedReader{
				R: tr,
				N: maxManifestSigSize,
			}
			if s, err = io.ReadAll(&lr); err != nil {
				return
			}
		} else {
			//we are just hashing the file and parking it in the map
			h := sha256.New()
			io.Copy(h, tr)
			x := h.Sum(nil)
			var v [sha256.Size]byte
			if len(x) != sha256.Size {
				err = ErrInvalidHash
				return
			}
			copy(v[0:sha256.Size], x)
			fileHashes[hdr.Name] = hex.EncodeToString(v[:])
		}
	}
	//check that we got a manifest
	if m == nil {
		err = ErrMissingManifest
		return
	}
	//sigs match, lets checkout the files and manifest
	if err = manifest.Unmarshal(m); err != nil {
		return
	}
	if len(manifest.Items) != len(fileHashes) {
		err = fmt.Errorf("%w - Expected %d items, got %d", ErrManifestMismatch, len(manifest.Items), len(fileHashes))
		return
	}
	for _, item := range manifest.Items {
		if hsh, ok := fileHashes[item.Filename()]; !ok {
			err = fmt.Errorf("%s (%s) in manifest missing from kit", item.Name, item.Filename())
			return
		} else if hsh != item.Hash {
			err = fmt.Errorf("%s in kit is corrupted", item.Name)
			return
		}
	}
	// Do the signing verification if provided
	if sigVerify != nil {
		if sigerr = sigVerify(m, s); sigerr != nil {
			sigerr = fmt.Errorf("%w - %v", sigerr, ErrInvalidSignature)
		} else {
			signed = true
		}
	}

	return
}
