// Package state carries the universal state of gwcli.
// Properties in this package are considered RO as soon as they are set, unless explicitly marked otherwise.
package state

var interactive bool // is interactivity allowed?

// SetInteractive set the global state of allowed interactivity.
// It should only be called once, during command pre-run (ppre()).
func SetInteractive(allowed bool) {
	interactive = allowed
}

// Interactive returns whether or not interactivity is allowed.
func Interactive() bool {
	return interactive
}

// TODO move debug mode into here rather than repeatedly checking on clilog.Level
