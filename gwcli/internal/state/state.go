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

const Version string = "v0.8"

var directInvoked bool

// DirectInvoked means that an action was launched directly, but -x was not specified.
// This implies that mother was booted, therefore interactivity is allowed, and that we'll die after this action invocation.
func DirectInvoked() bool {
	return directInvoked
}

// SetDirectInvoked notes that an action was launched directly and that Mother will execute it, then die.
// It should only be called once if/when Mother has deemed that the call was a direct invocation and that gwcli will exit on action completion.
func SetDirectInvoked() {
	directInvoked = true
}

var debug bool

// DebugMode authorizes additional run-time sanity checks.
// It is currently set at startup time if --loglevel=DEBUG.
func DebugMode() bool {
	return debug
}

// SetDebugMode enables additional checks and verbose logging.
func SetDebugMode() {
	debug = true
}
