/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package ingest

import (
	"crypto/tls"
	"errors"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gravwell/ingest/v3/entry"
)

const (
	MIN_REMOTE_KEYSIZE int           = 16
	DIAL_TIMEOUT       time.Duration = (1 * time.Second)
	CHANNEL_BUFFER     int           = 4096
	DEFAULT_TLS_PORT   int           = 4024
	DEFAULT_CLEAR_PORT int           = 4023
	DEFAULT_PIPE_PATH  string        = "/opt/gravwell/comms/pipe"
	FORBIDDEN_TAG_SET  string        = "!@#$%^&*()=+<>,.:;`\"'{[}]|\\ 	" // includes space and tab characters at the end
)

var (
	ErrFailedParseLocalIP    = errors.New("Failed to parse the local address")
	ErrMalformedDestination  = errors.New("Malformed destination string")
	ErrInvalidCerts          = errors.New("Failed to get certificates")
	ErrInvalidDest           = errors.New("Invalid destination")
	ErrInvalidRemoteKeySize  = errors.New("Invalid remote keysize")
	ErrInvalidConnectionType = errors.New("Invalid connection type")
	ErrInvalidSecret         = errors.New("Failed to login, invalid secret")

	localhostAddr = net.ParseIP("::1")
)

type TLSCerts struct {
	Cert tls.Certificate
}

//ConnectionType cracks out the type of connection and returns its type, the target, and/or an error
func ConnectionType(dst string) (string, string, error) {
	bits := strings.Split(dst, "://")
	if len(bits) != 2 {
		return "", "", ErrMalformedDestination
	}
	t := strings.ToLower(bits[0])

	switch t {
	case `tls`:
		return t, bits[1], nil
	case `tcp`:
		return t, bits[1], nil
	case `pipe`:
		return t, bits[1], nil
	default:
		break
	}
	return "", "", ErrInvalidConnectionType
}

// InitializeConnection is a simple wrapper to get a line to an ingester.
// callers can just call this function and get back a hot ingest connection
// We take care of establishing the connection and shuttling auth around
//
// Deprecated: Use the IngestMuxer instead.
func InitializeConnection(dst, authString string, tags []string, pubKey, privKey string, verifyRemoteKey bool) (*IngestConnection, error) {
	auth, err := GenAuthHash(authString)
	if err != nil {
		return nil, err
	}
	t, dest, err := ConnectionType(dst)
	if err != nil {
		return nil, err
	}
	switch t {
	//figure out which connection is specified
	case "tls":
		if err = verifyTlsKeys(pubKey, privKey); err != nil {
			return nil, err
		}
		//build up the certs so they can be thrown at the new TLS connection
		certs, err := getCerts(pubKey, privKey)
		if err != nil {
			return nil, err
		} else if certs == nil {
			return nil, ErrInvalidCerts
		}
		return NewTLSConnection(dest, auth, certs, verifyRemoteKey, tags)
	case "tcp":
		return NewTCPConnection(dest, auth, tags)
	case "pipe":
		return NewPipeConnection(dest, auth, tags)
	default:
		break
	}
	//this SHOULD never hit, but safety first kids
	return nil, ErrInvalidDest
}

// verifyTlsKeys function will verify that public and private keys can be parsed
func verifyTlsKeys(pub, priv string) error {
	_, err := getCerts(pub, priv)
	return err
}

func getCerts(pub, priv string) (*TLSCerts, error) {
	var cert tls.Certificate
	var err error
	if pub != "" && priv != "" {
		cert, err = tls.LoadX509KeyPair(pub, priv)
		if err != nil {
			return nil, ErrInvalidCerts
		}
	}
	certs := &TLSCerts{cert} //nil on remote pub because we aren't verifying
	return certs, nil
}

func loadRemotePublicKey(remote string) ([]byte, error) {
	fin, err := os.Open(remote)
	if err != nil {
		return nil, err
	}
	keysize, err := fin.Seek(0, 2)
	if err != nil {
		return nil, err
	}

	//TODO: do some additional checks
	//there are only just a few acceptable key sizes
	if keysize <= int64(MIN_REMOTE_KEYSIZE) {
		return nil, ErrInvalidRemoteKeySize
	}
	remoteKey := make([]byte, keysize+1)
	_, err = fin.Seek(0, 0)
	if err != nil {
		return nil, err
	}
	bytesRead, err := fin.Read(remoteKey)
	if err != nil {
		return nil, err
	}
	if int64(bytesRead) != keysize {
		return nil, ErrInvalidRemoteKeySize
	}
	fin.Close()
	return remoteKey, nil
}

func checkTLSPublicKey(local, remote []byte) bool {
	//both keys must be populated, > 0 and the same length
	if local == nil || remote == nil {
		return false
	}
	if len(local) == 0 || len(remote) == 0 || len(local) != len(remote) {
		return false
	}
	for i := 0; i < len(local); i++ {
		if local[i] != remote[i] {
			return false
		}
	}
	return true
}

// NewTLSConnection will create a new connection to a remote system using a secure
// TLS tunnel.  If the remotePubKey in TLSCerts is set, we will verify the public key
// of the remote server and bail if it doesn't match.  This is a basic MitM
// protection.  This requires that we HAVE the remote public key, getting that will
// be done else where.
//
// Deprecated: Use the IngestMuxer instead.
func NewTLSConnection(dst string, auth AuthHash, certs *TLSCerts, verify bool, tags []string) (*IngestConnection, error) {
	if err := checkTags(tags); err != nil {
		return nil, err
	}
	conn, src, err := newTlsConn(dst, certs, verify)
	if err != nil {
		return nil, err
	}

	return completeIngestConnection(conn, src, auth, tags)
}

//negotiate a TLS connection and check the public cert if requested
func newTlsConn(dst string, certs *TLSCerts, verify bool) (net.Conn, net.IP, error) {
	var src net.IP

	config := tls.Config{
		InsecureSkipVerify: !verify,
	}
	if certs != nil {
		config.Certificates = []tls.Certificate{certs.Cert}
	}

	dialer := &net.Dialer{Timeout: 5 * time.Second}
	conn, err := tls.DialWithDialer(dialer, "tcp", dst, &config)
	if err != nil {
		return nil, src, err
	}
	host, _, err := net.SplitHostPort(conn.LocalAddr().String())
	if err != nil {
		return nil, src, ErrFailedParseLocalIP
	}
	if src = net.ParseIP(host); src == nil {
		return nil, src, ErrFailedParseLocalIP
	}

	return conn, src, nil
}

// This function will create a new cleartext TCP connection to a remote system.
// No verification of the server is performed AT ALL.  All traffic is snoopable
// and modifiable.  If someone has control of the network, they will be able to
// inject and monitor this traffic.
//
// dst: should be a address:port pair.
// For example "ingest.gravwell.com:4042" or "10.0.0.1:4042"
//
// Deprecated: Use the IngestMuxer instead.
func NewTCPConnection(dst string, auth AuthHash, tags []string) (*IngestConnection, error) {
	err := checkTags(tags)
	if err != nil {
		return nil, err
	}
	conn, src, err := newTcpConn(dst)
	if err != nil {
		return nil, err
	}
	return completeIngestConnection(conn, src, auth, tags)
}

func newTcpConn(dst string) (net.Conn, net.IP, error) {
	var src net.IP
	//try to dial with a timeout of 3 seconds
	conn, err := net.DialTimeout("tcp", dst, DIAL_TIMEOUT)
	if err != nil {
		return nil, src, err
	}
	host, _, err := net.SplitHostPort(conn.LocalAddr().String())
	if err != nil {
		return nil, src, err
	}
	src = net.ParseIP(host)
	if src == nil {
		return nil, src, ErrFailedParseLocalIP
	}
	return conn, src, nil
}

// This function will create a new NamedPipe connection to a local system.
// We have NO WAY of knowing which process is REALLY on the other end of the
// pipe.  But it is assumed that gravwell will be running with highly limited
// privileges, so if the integrity of the local system is compromised,
// its already over.
//
// Deprecated: Use the IngestMuxer instead.
func NewPipeConnection(dst string, auth AuthHash, tags []string) (*IngestConnection, error) {
	err := checkTags(tags)
	if err != nil {
		return nil, err
	}
	conn, src, err := newPipeConn(dst)
	if err != nil {
		return nil, err
	}
	return completeIngestConnection(conn, src, auth, tags)
}

func newPipeConn(dst string) (net.Conn, net.IP, error) {
	var src net.IP
	conn, err := net.DialTimeout("unix", dst, DIAL_TIMEOUT)
	if err != nil {
		return nil, src, err
	}
	return conn, localhostAddr, nil
}

func negotiateEntryWriter(conn net.Conn, auth AuthHash, tags []string) (*EntryWriter, map[string]entry.EntryTag, error) {
	tagIDs, serverVersion, err := authenticate(conn, auth, tags)
	if err != nil {
		conn.Close()
		return nil, nil, err
	}

	ew, err := NewEntryWriter(conn)
	if err != nil {
		return nil, nil, err
	}
	ew.serverVersion = serverVersion
	return ew, tagIDs, nil
}

//completeIngestConnection performs the authentication and tag negotiation
func completeIngestConnection(conn net.Conn, src net.IP, auth AuthHash, tags []string) (*IngestConnection, error) {
	ew, tagIDs, err := negotiateEntryWriter(conn, auth, tags)
	if err != nil {
		return nil, err
	}
	//make and fire up the IngestConnection
	igst := IngestConnection{
		conn:    conn,
		ew:      ew,
		src:     src,
		tags:    tagIDs,
		running: true,
		mtx:     sync.RWMutex{},
	}
	return &igst, nil
}
