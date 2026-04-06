/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package scaffoldcreate

import "github.com/charmbracelet/bubbles/textinput"

// FieldProvider is the interface implemented by types that supply field rendering details.
type FieldProvider interface {
	fieldProvider() // marker method; unexported to restrict external implementations
}

// TextProvider implements FieldProvider for standard text input fields.
type TextProvider struct {
	Title      string
	CustomInit func() textinput.Model
}

func (*TextProvider) fieldProvider() {}
