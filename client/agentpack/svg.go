/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package agentpack

import (
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"image/png"
	"io"
	"strings"
)

// An agent's StateDiagram and Avatar are SVG documents that whatever renders
// the agent shows as given, so they are checked for scripting on the way in.

const svgNS = "http://www.w3.org/2000/svg"

var (
	ErrNotSVG       = errors.New("not an SVG document")
	ErrActiveSVG    = errors.New("SVG contains scripting")
	ErrMalformedSVG = errors.New("malformed SVG")
)

// CheckSVG requires s to be a well-formed SVG document with no scripting: no
// script or foreignObject elements, no event handler attributes and no
// javascript: links. Not every renderer confines an SVG the way an <img> tag
// does.
func CheckSVG(s string) error {
	dec := xml.NewDecoder(strings.NewReader(s))
	dec.Strict = true
	var sawRoot bool
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("%w: %v", ErrMalformedSVG, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if !sawRoot {
				if t.Name.Local != "svg" || t.Name.Space != svgNS {
					return fmt.Errorf("%w: root element is <%s>, not an <svg> in the %s namespace", ErrNotSVG, t.Name.Local, svgNS)
				}
				sawRoot = true
			}
			switch strings.ToLower(t.Name.Local) {
			case "script", "foreignobject", "handler", "listener":
				return fmt.Errorf("%w: <%s> element", ErrActiveSVG, t.Name.Local)
			}
			for _, at := range t.Attr {
				name := strings.ToLower(at.Name.Local)
				if strings.HasPrefix(name, "on") {
					return fmt.Errorf("%w: %s attribute on <%s>", ErrActiveSVG, at.Name.Local, t.Name.Local)
				}
				v := strings.ToLower(strings.Join(strings.Fields(at.Value), ""))
				if strings.HasPrefix(v, "javascript:") || strings.HasPrefix(v, "data:text/html") || strings.HasPrefix(v, "data:image/svg") {
					return fmt.Errorf("%w: %s link on <%s>", ErrActiveSVG, at.Name.Local, t.Name.Local)
				}
			}
		case xml.Directive:
			// A DOCTYPE can declare entities, which is how XML bombs work.
			return fmt.Errorf("%w: directives such as DOCTYPE are not allowed", ErrNotSVG)
		}
	}
	if !sawRoot {
		return ErrNotSVG
	}
	return nil
}

// SVGFromPNG wraps a PNG in an SVG document of the same size, so raster art
// can fill an agent's SVG image fields. The PNG is fully decoded first so a
// truncated or corrupt file is never embedded.
func SVGFromPNG(raw []byte) (string, error) {
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		return "", fmt.Errorf("not a valid PNG: %w", err)
	}
	size := img.Bounds().Size()
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%[1]d" height="%[2]d" viewBox="0 0 %[1]d %[2]d">`+
		`<image width="%[1]d" height="%[2]d" href="data:image/png;base64,%[3]s"/></svg>`,
		size.X, size.Y, base64.StdEncoding.EncodeToString(raw)), nil
}

// SVGFromFile turns an image file's contents into an SVG for an agent image
// field: an SVG is checked and used as is, and a PNG is wrapped.
func SVGFromFile(raw []byte) (string, error) {
	if bytes.HasPrefix(raw, []byte("\x89PNG\r\n\x1a\n")) {
		return SVGFromPNG(raw)
	}
	s := string(raw)
	if err := CheckSVG(s); err != nil {
		return "", err
	}
	return s, nil
}
