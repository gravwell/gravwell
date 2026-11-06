/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package dynamic lets an ingester take runner configurations from a Gravwell webserver
// at runtime rather than only from its config file.  A Manager syncs definitions from the
// webserver, validates and stores them locally, and starts or stops runners to match.
package dynamic

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gravwell/gravwell/v4/ingest/config"
)

const (
	// defaultWebserverScheme is attached to webserver endpoints that do not specify a protocol.
	defaultWebserverScheme = `http`

	// storageMode is the permission set applied when we have to create the storage directory.
	storageMode = 0770

	// confExt is the extension the overlay loader consumes.  It is spelled out here
	// rather than borrowed from ingest/config, which keeps its own copy unexported, and a
	// disagreement between the two would mean writing files that nothing ever reads.
	confExt = `.conf`

	// envIngestControlAuth is where Auth-Token may be supplied from the environment.
	//
	// It is deliberately the same variable the webserver reads for Ingest-Control-Auth.
	// This is one secret with two ends, and giving each end its own name is an invitation
	// for them to drift -- which fails as an authentication error on the ingester, the one
	// place with no way to explain that the two halves disagree.  It also lets a container
	// hand the same value to both without baking a credential into an image.
	envIngestControlAuth = `GRAVWELL_INGEST_CONTROL_AUTH`
)

// Config type manages the static config for dynamic ingesters which specifies
// the storage location, webserver addresses, authentication token, and other
// configuration parameters needed to perform dynamic configuration
type Config struct {
	Webserver  []string //REQUIRED list of webservers to communicate with
	Auth_Token string   //REQUIRED authentication token for webservers
	Storage    string   //REQUIRED storage location for configs
	Class      string   //OPTIONAL free form string to categorize an ingester for pulling configs

	//OPTIONAL how often to ask the webserver for configuration, as a duration string
	//e.g. "30s" or "5m".  Empty takes the default.  A webserver that pushes a change
	//reaches us sooner, this is the floor rather than the only path.
	Poll_Interval string
}

// DefaultPollInterval is how often a connected ingester asks for its configuration when
// Poll_Interval is not set.
const DefaultPollInterval = 30 * time.Second

// MinPollInterval is the floor.  Polling faster than this is a mistake that turns a fleet
// of ingesters into load on the webserver, so it is clamped rather than honored.
const MinPollInterval = time.Second

// PollInterval is the configured interval, or the default.  Verify has already checked
// that it parses, so an unparseable value here can only mean Verify was not called and
// the default is the safe answer.
func (c Config) PollInterval() time.Duration {
	if c.Poll_Interval == `` {
		return DefaultPollInterval
	}
	d, err := time.ParseDuration(c.Poll_Interval)
	if err != nil || d < MinPollInterval {
		return DefaultPollInterval
	}
	return d
}

func (c *Config) Verify() (err error) {
	if c == nil {
		return errors.New("nil config")
	}
	if !c.Enabled() {
		return
	}

	// A token in the configuration file wins; the environment only fills in one that is
	// absent, which is the same precedence every other secret here uses.  This is checked
	// after Enabled on purpose: an environment variable on its own must not switch dynamic
	// configuration on for an ingester whose config never asked for it.
	if err = config.LoadEnvVar(&c.Auth_Token, envIngestControlAuth, ``); err != nil {
		return fmt.Errorf("failed to load %s %w", envIngestControlAuth, err)
	}

	if len(c.Webserver) == 0 {
		return errors.New("missing webserver endpoints")
	} else if len(c.Auth_Token) == 0 {
		return errors.New("missing Auth-Token")
	} else if len(c.Storage) == 0 {
		return errors.New("missing Storage")
	}

	// iterate over the the Webserver endpoints and parse them as URLS
	// if a URL does not have protocol attach http://
	for i, ws := range c.Webserver {
		orig := strings.TrimSpace(ws)
		if orig == `` {
			return fmt.Errorf("webserver endpoint %d is empty", i)
		}
		ws = orig
		if !strings.Contains(ws, `://`) {
			ws = defaultWebserverScheme + `://` + ws
		}
		var uri *url.URL
		if uri, err = url.Parse(ws); err != nil {
			return fmt.Errorf("invalid webserver endpoint %q %w", orig, err)
		} else if uri.Host == `` {
			return fmt.Errorf("invalid webserver endpoint %q missing host", orig)
		}
		switch uri.Scheme {
		case `http`, `https`:
		case ``:
			uri.Scheme = `http` // set http if no scheme
		default:
			return fmt.Errorf("invalid webserver endpoint %q unsupported scheme %q", orig, uri.Scheme)
		}
		c.Webserver[i] = uri.String()
	}

	// the poll interval is optional, but if it is set it has to make sense
	if c.Poll_Interval != `` {
		d, perr := time.ParseDuration(c.Poll_Interval)
		if perr != nil {
			return fmt.Errorf("invalid Poll-Interval %q %w", c.Poll_Interval, perr)
		} else if d < MinPollInterval {
			return fmt.Errorf("invalid Poll-Interval %v, the minimum is %v", d, MinPollInterval)
		}
	}

	// check that storage points to a writable directory
	// if we get a not exist error, make it with user/group RWX permissions
	if fi, lerr := os.Stat(c.Storage); lerr != nil {
		if !os.IsNotExist(lerr) {
			return fmt.Errorf("failed to stat Storage %q %w", c.Storage, lerr)
		} else if err = os.MkdirAll(c.Storage, storageMode); err != nil {
			return fmt.Errorf("failed to create Storage %q %w", c.Storage, err)
		}
	} else if !fi.IsDir() {
		return fmt.Errorf("Storage %q is not a directory", c.Storage)
	}

	// permission bits alone are not enough to know that we can write, so actually try it
	if err = checkWritableDir(c.Storage); err != nil {
		return fmt.Errorf("Storage %q is not writable %w", c.Storage, err)
	}

	return
}

// Enabled reports whether anything in this block was set.
//
// Every member counts, Poll_Interval included.  A member left out of this check is one
// somebody can set on its own and have silently ignored: the block reads as disabled,
// Verify returns without looking at it, and the ingester starts with no dynamic
// configuration and no complaint about the half filled in section meant to turn it on.
func (c Config) Enabled() bool {
	if len(c.Webserver) == 0 && c.Auth_Token == `` && c.Storage == `` &&
		c.Class == `` && c.Poll_Interval == `` {
		return false
	}
	return true // SOMETHING was enabled, so run validate
}

// checkWritableDir ensures that we can create files in the given directory by
// creating a temporary file and then cleaning it up.
func checkWritableDir(dir string) (err error) {
	var fout *os.File
	if fout, err = os.CreateTemp(dir, `.gravwell`); err != nil {
		return
	}
	name := fout.Name()
	if err = fout.Close(); err != nil {
		os.Remove(name)
		return
	}
	return os.Remove(name)
}
