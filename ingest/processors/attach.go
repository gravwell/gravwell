/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package processors

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v4/ingest/attach"
	"github.com/gravwell/gravwell/v4/ingest/config"
	"github.com/gravwell/gravwell/v4/ingest/entry"
)

const (
	AttachProcessor string = `attach`
)

var (
	ErrAttachUUIDNotSupported = errors.New("$UUID is not supported in the attach preprocessor; it is only available in the global Attach configuration")
	ErrEmptyAttachValue       = errors.New("empty attach value")
	ErrMultipleAttachValues   = errors.New("multiple attach values")
)

func validateAttachConfig(c attach.AttachConfig) error {
	for _, v := range c {
		if v == "$UUID" {
			return ErrAttachUUIDNotSupported
		}
	}
	return nil
}

// AttachLoadConfig loads the configuration for the attach processor
// It converts the VariableConfig to an attach.AttachConfig
func AttachLoadConfig(vc *config.VariableConfig) (c attach.AttachConfig, err error) {
	c = make(attach.AttachConfig)
	names := vc.Names()

	for _, name := range names {
		valptr := vc.Vals[vc.Idx(name)]
		if valptr == nil {
			continue
		}
		vals := *valptr
		if len(vals) == 0 {
			return nil, fmt.Errorf("%w for %s", ErrEmptyAttachValue, name)
		}
		if len(vals) > 1 {
			return nil, fmt.Errorf("%w for %s, %d values given", ErrMultipleAttachValues, name, len(vals))
		}

		c[name] = vals[0]
	}

	// Filter out the "type" key which is used for preprocessor selection
	// but should not be attached as an enumerated value
	delete(c, "type")

	// Check for $UUID which is not supported in preprocessor attach
	if err = validateAttachConfig(c); err != nil {
		return
	}

	err = c.Verify()
	return
}

// NewAttachProcessor creates a new attach processor
func NewAttachProcessor(cfg attach.AttachConfig) (*AttachProc, error) {
	if err := cfg.Verify(); err != nil {
		return nil, err
	}
	// Check for $UUID which is not supported in preprocessor attach
	//This check ensures the rule is enforced regardless of how the config was created.
	if err := validateAttachConfig(cfg); err != nil {
		return nil, err
	}

	attacher, err := attach.NewAttacher(cfg, uuid.UUID{})
	if err != nil {
		return nil, err
	}
	return &AttachProc{
		cfg:      cfg,
		attacher: attacher,
	}, nil
}

type AttachProc struct {
	nocloser
	cfg      attach.AttachConfig
	attacher *attach.Attacher
}

// Config updates the configuration for the attach processor
func (a *AttachProc) Config(v interface{}) (err error) {
	if v == nil {
		err = ErrNilConfig
	} else if cfg, ok := v.(attach.AttachConfig); ok {
		if err = cfg.Verify(); err != nil {
			return
		}
		// Check for $UUID which is not supported in preprocessor attach
		// Config allows runtime updates to the processor, and we must ensure those updates also obey the "no $UUID" rule.
		if err = validateAttachConfig(cfg); err != nil {
			return
		}

		// Create a new attacher with the updated config
		var attacher *attach.Attacher
		if attacher, err = attach.NewAttacher(cfg, uuid.UUID{}); err != nil {
			return
		}
		a.cfg = cfg
		a.attacher = attacher
	} else {
		err = fmt.Errorf("Invalid configuration, unknown type %T", v)
	}
	return
}

// Process attaches enumerated values to each entry
func (a *AttachProc) Process(ents []*entry.Entry) (rset []*entry.Entry, err error) {
	if len(ents) == 0 {
		return
	}
	rset = ents
	for _, ent := range ents {
		if ent == nil {
			continue
		}
		a.attacher.Attach(ent)
	}
	return
}
