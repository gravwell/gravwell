/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package filewatch

// NewReader creates a new reader based on either the regex engine or line reader engine
// the Linux version of file follow does NOT support EVTX engines
func NewReader(cfg ReaderConfig) (Reader, error) {
	switch cfg.Engine {
	case RegexEngine:
		return NewRegexReader(cfg)
	case LineEngine: //default/empty is line reader
		return NewLineReader(cfg)
	}
	return nil, errors.New("Unknown engine")
}
