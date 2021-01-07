/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package processors

import (
	"fmt"

	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/gravwell/gravwell/v3/ingest/entry"
)

const (
	DropProcessor string = `drop`
)

type DropConfig struct {
}

func DropLoadConfig(vc *config.VariableConfig) (c DropConfig, err error) {
	err = vc.MapTo(&c)
	return
}

func NewDrop(cfg DropConfig) (*Drop, error) {
	return &Drop{
		DropConfig: cfg,
	}, nil
}

// Drop does not have any state, and doesn't do much
type Drop struct {
	nocloser
	DropConfig
}

func (gd *Drop) Config(v interface{}) (err error) {
	if v == nil {
		err = ErrNilConfig
	} else if cfg, ok := v.(DropConfig); ok {
		gd.DropConfig = cfg
	} else {
		err = fmt.Errorf("Invalid configuration, unknown type type %T", v)
	}
	return
}

func (gd *Drop) Process(ent []*entry.Entry) (rset []*entry.Entry, err error) {
	return
}
