/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package icon

import (
	"crypto/sha256"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// Package icon rebuilds a plugin supplied SVG into markup that is safe to put in a page.
//
// The whole reason for this function to exist is because SVGs can contain a whole host of
// rather nasty embedded stuff.  The spec got huge, we don't need all of it, just need to
// draw logos.
//
// An icon is drawn by whoever wrote the ingester, and it arrives here over the wire.  It
// is therefore untrusted markup, and SVG is a document format rather than an image one: it
// can carry <script>, event handler attributes, <foreignObject> holding arbitrary HTML,
// and references out to other origins.  Inlining one as it arrived would let any ingester
// holding the shared token run script in the browser of whoever opens this page, which is
// a straight line from "can ingest" to "can drive the webserver interface".
//
// So nothing is inlined as it arrived.  The markup is parsed and rebuilt from an allow
// list: an element not named here does not survive, an attribute not named here does not
// survive, and anything that fails to parse is dropped entirely rather than patched up.
// An allow list is the only shape that is safe to be wrong about, because the failure of
// a list of known-bad things is to let something through, and the failure of this is a
// plugin that draws no icon.
const (
	// maxIconBytes caps what will be parsed at all.  An icon is a few hundred bytes of
	// path data, and a page drawing a list of them should not be able to be made
	// enormous by an ingester that claims otherwise.
	maxIconBytes = 32 * 1024

	// iconViewBox is used when a plugin ships an icon with no viewBox of its own, which
	// would otherwise scale unpredictably once the width and height are stripped.
	iconViewBox = `0 0 24 24`
)

// iconElements is every element that may appear.  Shapes and grouping only: no <script>,
// no <foreignObject>, no <image> or <use> (both reference other documents), no <style>
// (a stylesheet can reach out with url()), and no animation elements.
var iconElements = map[string]bool{
	`svg`: true, `g`: true, `title`: true, `desc`: true,
	`path`: true, `circle`: true, `ellipse`: true, `line`: true,
	`polyline`: true, `polygon`: true, `rect`: true,
	// paint servers.  A brand mark is usually a gradient over a shape, and without these
	// the shape survives while the paint it points at does not, which renders as nothing
	// at all rather than as a plain colour.  None of them can reference another document:
	// the one attribute that would, xlink:href, is namespaced and dropped with every
	// other namespaced attribute.
	`defs`: true, `lineargradient`: true, `radialgradient`: true, `stop`: true,
}

// iconAttrs is every attribute that may appear: geometry and presentation, nothing that
// can name a URL or carry code.  Notably absent are style, href and xlink:href, every
// on* handler, and id/aria-labelledby, which are dropped because a page draws many icons
// at once and duplicated ids break both the document and the references into it.
var iconAttrs = map[string]bool{
	`viewbox`: true, `transform`: true, `d`: true, `points`: true,
	`x`: true, `y`: true, `x1`: true, `y1`: true, `x2`: true, `y2`: true,
	`cx`: true, `cy`: true, `r`: true, `rx`: true, `ry`: true,
	`fx`: true, `fy`: true, `width`: true, `height`: true,
	`preserveaspectratio`: true,
	`fill`:                true, `fill-opacity`: true, `fill-rule`: true, `clip-rule`: true,
	`stroke`: true, `stroke-width`: true, `stroke-linecap`: true,
	`stroke-linejoin`: true, `stroke-dasharray`: true, `stroke-opacity`: true,
	`stroke-miterlimit`: true, `opacity`: true, `vector-effect`: true,
	// gradients
	`id`: true, `offset`: true, `stop-color`: true, `stop-opacity`: true,
	`gradientunits`: true, `gradienttransform`: true, `spreadmethod`: true,
}

// rootOnlyAttrs are the attributes only the outermost <svg> may carry.  width and height
// are dropped everywhere else too, see sanitizeIcon.
var rootOnlyAttrs = map[string]bool{`viewbox`: true, `preserveaspectratio`: true}

// paintAttrs are the attributes whose value may name a paint server rather than a colour.
// They are the only place a url() reference is allowed, and even there only a local one.
var paintAttrs = map[string]bool{`fill`: true, `stroke`: true, `stop-color`: true}

// idChars is the shape an id has to have to be kept: the ASCII part of an XML Name, which
// may begin with a letter, an underscore or a colon.
//
// The underscore matters more than it looks.  Illustrator encodes an id that would
// otherwise start with a digit as something like _x31_, so a pattern that insisted on a
// leading letter threw away the ids of a whole family of exported artwork, and with them
// the references pointing at those ids, leaving a shape with nothing to paint it.
//
// It stays an allow list because the rewriting below writes ids straight into an
// attribute: every character here is one that cannot end it early.
const idChars = `[A-Za-z_:][A-Za-z0-9_.:-]*`

// idPattern is what an id has to look like to be kept.
var idPattern = regexp.MustCompile(`^` + idChars + `$`)

// localRef matches a reference to a paint server in the same document, which is the only
// kind allowed: url(#name), with optional whitespace and quoting.
var localRef = regexp.MustCompile(`^url\(\s*['"]?#(` + idChars + `)['"]?\s*\)$`)

// canonicalNames restores the spelling of the SVG names that are camel case.
//
// Everything here is matched in lower case, because that is the only way to compare names
// without caring how the source wrote them, but SVG itself is case sensitive: an element
// written out as "lineargradient" is not a gradient.  An HTML parser happens to repair
// these when it adopts foreign content, so the mistake renders anyway and would sit here
// unnoticed until the same markup was put somewhere that parses it as XML.
var canonicalNames = map[string]string{
	`lineargradient`:      `linearGradient`,
	`radialgradient`:      `radialGradient`,
	`gradientunits`:       `gradientUnits`,
	`gradienttransform`:   `gradientTransform`,
	`spreadmethod`:        `spreadMethod`,
	`preserveaspectratio`: `preserveAspectRatio`,
	`viewbox`:             `viewBox`,
}

// canonical returns the spelling to emit for a lower cased name.
func canonical(name string) string {
	if c, ok := canonicalNames[name]; ok {
		return c
	}
	return name
}

// sizeValue reads a width or height off the root element.
//
// Any unit is accepted, not just px: a bare number is user units, and every absolute unit
// SVG allows still carries the number that says how far the drawing extends.  Trimming
// only px meant an export in pt or mm parsed as nothing and fell back to a square, which
// cropped the artwork to a corner of itself.
//
// A percentage is the exception and is deliberately refused.  It is a fraction of
// something else rather than an extent, so there is genuinely no coordinate space to
// recover from it and the square fallback is the only answer left.
func sizeValue(v string) (float64, bool) {
	v = strings.TrimSpace(v)
	if strings.HasSuffix(v, `%`) {
		return 0, false
	}
	v = strings.TrimRightFunc(v, unicode.IsLetter)
	f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil || f <= 0 {
		return 0, false
	}
	return f, true
}

// iconPrefix derives the namespace an icon's ids are rewritten into.
//
// Ids have to survive now that gradients do, and a plugin's ids are its own business: two
// different plugins drawn on the same page will both have been exported by the same tool
// and will both call their gradient "linearGradient-1".  Left alone, the second icon
// would paint itself with the first one's gradient.
//
// The prefix comes from the icon's own bytes rather than from a counter, so the same icon
// always sanitizes to the same markup: a fragment that is rendered twice on a page is
// byte for byte the same both times, and only genuinely different icons get different
// ids.  Two copies of one icon do repeat an id, which no browser minds because the thing
// being referenced is identical.
func iconPrefix(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return fmt.Sprintf("i%x-", sum[:4])
}

// Sanitize rebuilds a plugin supplied SVG from the allow lists above.
//
// ok is false when nothing safe survived.  An icon that is empty, oversized, not an SVG,
// or not well formed comes back as nothing at all and the caller falls back to drawing the
// plugin's name, which is the correct outcome for a decorative element: a broken icon is
// never worth a broken page.
//
// The result is a plain string rather than a template.HTML.  This package is called by a
// JSON API as well as by something rendering a page, and handing back an html/template
// type would force that import on a caller that is not rendering HTML.  A caller that is
// converts at its own call site, where the decision to trust this output is visible.
//
// The root is re-emitted rather than copied.  Its width and height are dropped so that
// the stylesheet decides how big an icon is instead of whoever drew it, and it is marked
// aria-hidden because the name it sits next to is already the accessible label.
func Sanitize(raw string) (svg string, ok bool) {
	if strings.TrimSpace(raw) == `` || len(raw) > maxIconBytes {
		return ``, false
	}
	prefix := iconPrefix(raw)
	dec := xml.NewDecoder(strings.NewReader(raw))
	// an entity a plugin invented is not something to go and resolve, and a reference to
	// an external one is a way out of this process
	dec.Strict = true
	dec.Entity = xml.HTMLEntity

	var sb strings.Builder
	var depth, skip int
	var shapes int
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		} else if err != nil {
			return ``, false // malformed, and a half parsed icon is not worth salvaging
		}
		switch t := tok.(type) {
		case xml.StartElement:
			name := strings.ToLower(t.Name.Local)
			if skip > 0 {
				skip++ // already inside something disallowed, stay inside it
				continue
			}
			if !iconElements[name] {
				skip = 1
				continue
			}
			if depth == 0 {
				if name != `svg` {
					return ``, false // not an icon at all
				}
				sb.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" class="icon" aria-hidden="true" focusable="false"`)
				writeIconAttrs(&sb, t.Attr, true, prefix)
				sb.WriteString(`>`)
				depth++
				continue
			}
			switch name {
			case `title`, `desc`, `g`, `defs`, `lineargradient`, `radialgradient`, `stop`:
				// structure and paint, not something that draws on its own
			default:
				shapes++
			}
			sb.WriteString(`<` + canonical(name))
			writeIconAttrs(&sb, t.Attr, false, prefix)
			sb.WriteString(`>`)
			depth++
		case xml.EndElement:
			if skip > 0 {
				skip--
				continue
			}
			if depth == 0 {
				continue
			}
			depth--
			sb.WriteString(`</` + canonical(strings.ToLower(t.Name.Local)) + `>`)
		case xml.CharData:
			// text only survives inside a title or desc, and only as escaped text.
			// Everything else in an icon is attributes.
			if skip == 0 && depth > 0 {
				sb.WriteString(html.EscapeString(string(t)))
			}
		}
		// comments, processing instructions and directives are simply never emitted
	}
	if depth != 0 || shapes == 0 {
		return ``, false // unbalanced, or nothing left that would draw anything
	}
	return sb.String(), true
}

// writeIconAttrs emits the attributes an element is allowed to keep.
//
// Namespaced attributes are dropped wholesale: the only ones that turn up in practice are
// xlink:href and the provenance namespaces, and none of them belong in an icon.  The root
// keeps its viewBox but loses width and height so the stylesheet sizes it; a child keeps
// width and height because that is geometry on a <rect>.
func writeIconAttrs(sb *strings.Builder, attrs []xml.Attr, root bool, prefix string) {
	var sawViewBox bool
	var w, h float64
	var haveW, haveH bool
	for _, a := range attrs {
		if a.Name.Space != `` {
			continue
		}
		name := strings.ToLower(a.Name.Local)
		if name == `xmlns` {
			continue // written by hand on the root, never carried over
		}
		if !iconAttrs[name] {
			continue
		}
		// an id is kept so a gradient can be pointed at, but namespaced first, see
		// iconPrefix.  One that could never be referenced is dropped rather than escaped.
		if name == `id` {
			if idPattern.MatchString(a.Value) {
				sb.WriteString(` id="` + prefix + a.Value + `"`)
			}
			continue
		}
		// a paint may name a paint server, and that is the one place a url() is allowed.
		// Only a reference into this same icon: anything else, in particular a url()
		// pointing at another origin, is dropped rather than followed.
		if paintAttrs[name] && strings.Contains(a.Value, `url(`) {
			if m := localRef.FindStringSubmatch(strings.TrimSpace(a.Value)); m != nil {
				sb.WriteString(` ` + canonical(name) + `="url(#` + prefix + m[1] + `)"`)
			}
			continue
		}
		if root {
			// the stylesheet decides how big an icon is, not the plugin.  The numbers
			// are still worth reading: an icon with no viewBox is drawn in the
			// coordinate space these describe, and dropping them without recording it
			// would leave nothing to say how far the drawing extends.
			if name == `width` {
				w, haveW = sizeValue(a.Value)
				continue
			} else if name == `height` {
				h, haveH = sizeValue(a.Value)
				continue
			}
		} else if rootOnlyAttrs[name] {
			continue
		}
		if name == `viewbox` {
			if !root {
				continue
			}
			sawViewBox = true
			sb.WriteString(` viewBox="` + html.EscapeString(a.Value) + `"`)
			continue
		}
		sb.WriteString(` ` + canonical(name) + `="` + html.EscapeString(a.Value) + `"`)
	}
	if root && !sawViewBox {
		// No viewBox, so the drawing has no declared coordinate space and the size it was
		// authored at is the only thing that says how far it extends.  Guessing a square
		// instead would crop every icon that was not drawn at exactly that size to its
		// top left corner, which is a far worse failure than an icon that is the wrong
		// shape: it looks like a rendering bug rather than a missing attribute.
		switch {
		case haveW && haveH:
			sb.WriteString(fmt.Sprintf(` viewBox="0 0 %s %s"`, trimNum(w), trimNum(h)))
		default:
			sb.WriteString(` viewBox="` + iconViewBox + `"`)
		}
	}
}

// trimNum renders a dimension without a trailing run of zeroes.
func trimNum(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}
