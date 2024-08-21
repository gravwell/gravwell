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
	"bytes"
	"crypto/sha256"
	"errors"
	"io"
	"io/ioutil"
	"os"

	"github.com/gravwell/gravwell/v4/client/types"
)

var (
	ErrNotActive      = errors.New("Not active")
	ErrInvalidImageID = errors.New("Invalid image ID, must be an existing file GUID")
	ErrEmptyID        = errors.New("Empty bundle ID")
)

// Builder is used to construct a kit. A typical workflow is:
//
// • Instantiate Builder using NewBuilder or NewBuilderFile
//
// • Add kit items by calling Add method on Builder
//
// • Use SetCover, SetBanner, SetIcon methods if desired
//
// • Call WriteManifest method and Close method
type Builder struct {
	tw       *tar.Writer
	fout     io.WriteCloser
	manifest Manifest
}

// BuilderConfig sets basic options for a kit.
type BuilderConfig struct {
	Version      uint
	Name         string
	Description  string // A short description of the kit
	Readme       string // A more detailed description of the kit
	ID           string
	MinVersion   types.CanonicalVersion
	MaxVersion   types.CanonicalVersion
	Dependencies []types.KitDependency
	ConfigMacros []types.KitConfigMacro
}

// NewBuilder creates a new Builder object. It takes a kit configuration (BuilderConfig)
// and a WriteCloser as arguments. The completed kit will be written to the WriteCloser.
func NewBuilder(cfg BuilderConfig, fout io.WriteCloser) (pb *Builder, err error) {
	if err = cfg.Validate(); err != nil {
		return
	}
	mf := Manifest{
		ID:           cfg.ID,
		Name:         cfg.Name,
		Desc:         cfg.Description,
		Readme:       cfg.Readme,
		Version:      cfg.Version,
		MinVersion:   cfg.MinVersion,
		MaxVersion:   cfg.MaxVersion,
		Dependencies: cfg.Dependencies,
		ConfigMacros: cfg.ConfigMacros,
	}
	return &Builder{
		fout:     fout,
		tw:       tar.NewWriter(fout),
		manifest: mf,
	}, nil
}

// NewBuilderFile instantiates a new Builder object. It takes a kit configuration and
// a file path where the resulting kit should be saved.
func NewBuilderFile(cfg BuilderConfig, output string) (pb *Builder, err error) {
	if err = cfg.Validate(); err != nil {
		return
	}
	var fout *os.File
	if fout, err = os.Create(output); err != nil {
		return
	}
	if pb, err = NewBuilder(cfg, fout); err != nil {
		fout.Close()
		pb = nil
	}
	return
}

// Manifest returns the manifest for the kit.
func (pb *Builder) Manifest() Manifest {
	return pb.manifest
}

// Name returns the name set for the kit.
func (pb *Builder) Name() string {
	return pb.manifest.Name
}

// Description returns kit's description.
func (pb *Builder) Description() string {
	return pb.manifest.Desc
}

// ID returns the kit ID.
func (pb *Builder) ID() string {
	return pb.manifest.ID
}

// Abort bails and closes the output stream. It does not properly shut down the kit
// archive writer; the resulting kit will likely not be valid.
func (pb *Builder) Abort() error {
	if pb.fout == nil {
		return ErrNotActive
	}
	return pb.fout.Close()
}

// Close attempts to properly terminate the kit archive writer and close the Builder.
func (pb *Builder) Close() (err error) {
	if pb.fout == nil {
		err = ErrNotActive
		return
	}
	if err = pb.tw.Close(); err == nil {
		err = pb.fout.Close()
	}
	return
}

// WriteManifest writes the current state of the kit manifest to the archive. It
// should be the last thing called before completing the kit, after all items
// have been added. The "sig" parameter is an optional manifest signature which will be
// added to the kit if it is not nil. Call Manifest() to get the manifest, generate
// the signature based on that manifest, then call WriteManifest with that signature.
func (pb *Builder) WriteManifest(sig []byte) (err error) {
	//encode the manifest as JSON with tab indention
	var bts []byte
	hdr := tar.Header{
		Typeflag: tar.TypeReg,
		Mode:     0660,
	}

	if bts, err = pb.manifest.Marshal(); err != nil {
		return
	}
	if sig != nil && len(sig) > 0 {
		hdr.Name = ManifestSigName
		hdr.Size = int64(len(sig))
		if err = pb.tw.WriteHeader(&hdr); err != nil {
			return
		}
		if err = writeAll(pb.tw, sig); err != nil {
			return
		}
	}
	hdr.Name = ManifestName
	hdr.Size = int64(len(bts))
	if err = pb.tw.WriteHeader(&hdr); err != nil {
		return
	}
	err = writeAll(pb.tw, bts)
	return
}

// Add includes an item in the kit. The parameters are name (the name for the
// item), tp (the type of item), and v (the JSON-encoded item itself).
func (pb *Builder) Add(name string, tp ItemType, v []byte) error {
	if !tp.Valid() {
		return ErrInvalidType
	} else if name == `` {
		return ErrEmptyName
	} else if len(v) == 0 {
		return ErrEmptyContent
	}
	item := Item{
		Name: name,
		Type: tp,
		Hash: GetHash(v),
	}

	// Make sure this is not duplicated
	for i := range pb.manifest.Items {
		if pb.manifest.Items[i].Equal(item) {
			// silently return if so
			return nil
		}
	}

	hdr := tar.Header{
		Typeflag: tar.TypeReg,
		Name:     item.Filename(),
		Size:     int64(len(v)),
		Mode:     0660,
	}
	if err := pb.tw.WriteHeader(&hdr); err != nil {
		return err
	}
	if err := writeAll(pb.tw, v); err != nil {
		return err
	}
	if err := pb.manifest.Add(item); err != nil {
		return err
	}
	return nil
}

// AddFile includes an item in the kit, reading from an open file descriptor
// rather than from a slice of bytes.
func (pb *Builder) AddFile(name string, tp ItemType, f *os.File) error {
	var sz int64
	var n int64
	if !tp.Valid() {
		return ErrInvalidType
	} else if name == `` {
		return ErrEmptyName
	}
	if fi, err := f.Stat(); err != nil {
		return err
	} else if sz = fi.Size(); sz == 0 {
		return ErrEmptyContent
	}
	hsh, err := getReaderHash(f)
	if err != nil {
		return err
	}
	//grab current position
	if n, err = f.Seek(0, 1); err != nil {
		return err
	}
	//seek to start
	if _, err = f.Seek(0, 0); err != nil {
		return err
	}

	item := Item{
		Name: name,
		Type: tp,
		Hash: hsh,
	}

	hdr := tar.Header{
		Typeflag: tar.TypeReg,
		Name:     item.Filename(),
		Size:     sz,
		Mode:     0660,
	}
	if err := pb.tw.WriteHeader(&hdr); err != nil {
		return err
	}
	if _, err := io.Copy(pb.tw, f); err != nil {
		return err
	}
	//reset position
	if _, err = f.Seek(n, 0); err != nil {
		return err
	}
	if err := pb.manifest.Add(item); err != nil {
		return err
	}
	return nil
}

// AddReader includes an item in the kit, reading from an io.Reader instead
// of a slice of bytes.
func (pb *Builder) AddReader(name string, tp ItemType, r io.Reader) error {
	bb := bytes.NewBuffer(nil)
	if n, err := io.Copy(bb, r); err != nil {
		return err
	} else if n == 0 {
		return ErrEmptyContent
	}
	return pb.Add(name, tp, bb.Bytes())
}

// SetIcon sets the icon image for the kit. The parameter must be the name
// of an existing item already in the kit with ItemType == File.
func (pb *Builder) SetIcon(id string) error {
	if id == `` {
		return ErrInvalidImageID
	}
	return pb.manifest.SetIcon(id)
}

// SetCover sets the cover image for the kit. The parameter must be the name
// of an existing item already in the kit with ItemType == File.
func (pb *Builder) SetCover(id string) error {
	if id == `` {
		return ErrInvalidImageID
	}
	return pb.manifest.SetCover(id)
}

// SetBanner sets the banner image for the kit. The parameter must be the name
// of an existing item already in the kit with ItemType == File.
func (pb *Builder) SetBanner(id string) error {
	if id == `` {
		return ErrInvalidImageID
	}
	return pb.manifest.SetBanner(id)
}

func getTempFile() (f *os.File, err error) {
	f, err = ioutil.TempFile(os.TempDir(), `gravpack`)
	return
}

func GetHash(v []byte) [sha256.Size]byte {
	return sha256.Sum256(v)
}

func getReaderHash(rdr io.ReadSeeker) (hsh [sha256.Size]byte, err error) {
	var n int64
	//grab current position
	if n, err = rdr.Seek(0, 1); err != nil {
		return
	}
	//seek to start
	if _, err = rdr.Seek(0, 0); err != nil {
		return
	}
	//generate hash
	h := sha256.New()
	if _, err = io.Copy(h, rdr); err != nil {
		return
	}
	//reset position
	if _, err = rdr.Seek(n, 0); err != nil {
		return
	}
	if bts := h.Sum(nil); len(bts) != sha256.Size {
		err = ErrInvalidHash
	} else {
		copy(hsh[0:sha256.Size], bts)
	}
	return
}

// Validate ensures that the BuilderConfig is acceptable.
func (c *BuilderConfig) Validate() error {
	if c.Version == 0 {
		c.Version = Version
	}
	if c.Name == `` {
		return ErrEmptyName
	} else if c.ID == `` {
		return ErrEmptyID
	}
	return nil
}
