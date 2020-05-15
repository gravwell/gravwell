/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package args

import (
	"errors"
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/config"
)

var (
	tagName         = flag.String("tag-name", "default", "Tag name for ingested data")
	pipeConns       = flag.String("pipe-conn", "", "Path to pipe connection")
	clearConns      = flag.String("clear-conns", "", "comma seperated server:port list of cleartext targets")
	tlsConns        = flag.String("tls-conns", "", "comma seperated server:port list of TLS connections")
	tlsPublicKey    = flag.String("tls-public-key", "", "Path to TLS public key")
	tlsPrivateKey   = flag.String("tls-private-key", "", "Path to TLS private key")
	tlsRemoteVerify = flag.Bool("tls-remote-verify", true, "Validate remote TLS certificates")
	ingestSecret    = flag.String("ingest-secret", "IngestSecrets", "Ingest key")
	timeoutSec      = flag.Int("timeout", 1, "Connection timeout in seconds")
)

type Args struct {
	Tags            []string
	Conns           []string
	TLSPublicKey    string
	TLSPrivateKey   string
	TLSRemoteVerify bool
	IngestSecret    string
	Timeout         time.Duration
}

func Parse() (a Args, err error) {
	flag.Parse()
	if *timeoutSec < 0 {
		err = errors.New("Invalid timeout")
		return
	}
	a.Timeout = time.Second * time.Duration(*timeoutSec)
	if *tagName == "" {
		err = errors.New("tag name required")
		return
	} else {
		//verify that the tag name is valid
		*tagName = strings.TrimSpace(*tagName)
		if strings.ContainsAny(*tagName, ingest.FORBIDDEN_TAG_SET) {
			err = errors.New("Forbidden characters in tag")
			return
		}
	}
	a.Tags = []string{*tagName}
	if *clearConns != "" {
		for _, conn := range strings.Split(*clearConns, ",") {
			conn = strings.TrimSpace(conn)
			if len(conn) > 0 {
				conn = fmt.Sprintf("tcp://%s", config.AppendDefaultPort(conn, config.DefaultCleartextPort))
				a.Conns = append(a.Conns, conn)
			}
		}
	}
	if *tlsConns != "" {
		for _, conn := range strings.Split(*tlsConns, ",") {
			conn = strings.TrimSpace(conn)
			if len(conn) > 0 {
				conn = fmt.Sprintf("tls://%s", config.AppendDefaultPort(conn, config.DefaultTLSPort))
				a.Conns = append(a.Conns, conn)
			}
		}
	}
	if *pipeConns != "" {
		for _, conn := range strings.Split(*pipeConns, ",") {
			conn = strings.TrimSpace(conn)
			if len(conn) > 0 {
				a.Conns = append(a.Conns, fmt.Sprintf("pipe://%s", conn))
			}
		}
	}
	if len(a.Conns) == 0 {
		err = errors.New("No indexer connections specified")
		return
	}
	a.TLSPublicKey = *tlsPublicKey
	a.TLSPrivateKey = *tlsPrivateKey
	if a.TLSPublicKey != "" && a.TLSPrivateKey == "" {
		err = errors.New("A private key is required when specifying a public key")
		return
	} else if a.TLSPublicKey == "" && a.TLSPrivateKey != "" {
		err = errors.New("A public key is required when specifying a private key")
		return
	}
	a.TLSRemoteVerify = *tlsRemoteVerify
	a.IngestSecret = *ingestSecret
	if a.IngestSecret == "" {
		err = errors.New("Ingest secret required")
	}
	return
}
