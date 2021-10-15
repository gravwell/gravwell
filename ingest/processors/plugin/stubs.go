//go:build 386 || arm || mips || mipsle || s390x
// +build 386 arm mips mipsle s390x

package plugin

import (
	"errors"
	"io/fs"
	"time"

	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/gravwell/gravwell/v3/ingest/entry"
)

type PluginProgram struct{}
type Tagger struct{}

var (
	ErrNotSupported = errors.New("plugins are not supported on 32bit architectures")
)

func NewPluginProgram(content []byte) (*PluginProgram, error) {
	return nil, ErrNotSupported
}

func NewPlugin(fsys fs.FS) (*PluginProgram, error) {
	return nil, ErrNotSupported
}

func (pp *PluginProgram) Run(to time.Duration) error {
	return ErrNotSupported
}

func (pp *PluginProgram) Config(*config.VariableConfig, Tagger) error {
	return ErrNotSupported
}

func (pp *PluginProgram) Flush() []*entry.Entry {
	return nil
}

func (pp *PluginProgram) Process(ents []*entry.Entry) ([]*entry.Entry, error) {
	return nil, ErrNotSupported
}

func (pp *PluginProgram) Ready() bool {
	return false
}
