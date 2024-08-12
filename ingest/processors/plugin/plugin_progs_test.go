//go:build !386 && !arm && !mips && !mipsle && !s390x
// +build !386,!arm,!mips,!mipsle,!s390x

/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package plugin

// a basic valid plugin that does nothing but is fully valid
const basicPlugin = `
package main

import (
	"gravwell"

	"github.com/gravwell/gravwell/v3/ingest/entry"
)

func main() {
	gravwell.Execute("test", cf, nop, nop, pf, ff)
}

func cf(cm gravwell.ConfigMap, tg gravwell.Tagger) error {
	return nil
}

func ff() []*entry.Entry {
	return nil
}

func pf([]*entry.Entry) ([]*entry.Entry, error) {
	return nil, nil
}

func nop() error {
	return nil
}
`

// a basic valid program that does not adhere to the plugin structure
// it will not fire the execution system and just exitplugin that does nothing but is fully valid
const basicBadPlugin = `
package main

import (
)

func main() {
	return
}`

// a basic valid program that does not adhere to the plugin structure
// it will not fire the execution system and just exitplugin that does nothing but is fully valid
const badIdlePlugin = `
package main

import (
	"time"
)

func main() {
	for {
		time.Sleep(100*time.Millisecond)
	}
	return
}`

const badPackage = `
package foobar

func foo() {}
`

const empty = ``

const broken = `foobarbaz`

const noMain = `
package main

func foobar() {}
`

const badCall = `
package main

import (
	"gravwell"

	"github.com/gravwell/gravwell/v3/ingest/entry"
)

func main() {
	gravwell.Execute("test", cf, pf, ff) //ff does not exist
}

func cf(cm gravwell.ConfigMap, tg gravwell.Tagger) error {
	return nil
}

func pf([]*entry.Entry) ([]*entry.Entry, error) {
	return nil, nil
}
`

const recase = `
package main

import (
	"gravwell" //package expose the builtin plugin funcs
	"bytes"
	"errors"
	"fmt"

	"github.com/gravwell/gravwell/v3/ingest/entry"
)

const (
	PluginName = "recase"
)

var (
	cfg CaseConfig
	tg gravwell.Tagger
	ready bool

	ErrNotReady = errors.New("not ready")
)

type CaseConfig struct {
	Upper bool
	Lower bool
}

func Config(cm gravwell.ConfigMap, tgr gravwell.Tagger) (err error) {
	if cm == nil || tgr == nil {
		err = errors.New("bad parameters")
	}
	cfg.Upper, _ = cm.GetBool("upper")
	cfg.Lower, _ = cm.GetBool("lower")

	if cfg.Upper && cfg.Lower {
		err = errors.New("upper and lower case are exclusive")
	} else {
		tg = tgr
		ready = true
	}
	return
}

func Flush() []*entry.Entry {
	return nil //we don't hold on to anything
}

func Process(ents []*entry.Entry) ([]*entry.Entry, error) {
	if !ready {
		return nil, ErrNotReady
	}
	if cfg.Upper {
		for i := range ents {
			ents[i].Data = bytes.ToUpper(ents[i].Data)
		}
	} else if cfg.Lower {
		for i := range ents {
			ents[i].Data = bytes.ToLower(ents[i].Data)
		}
	}
	return ents, nil
}

func nop() error {
	return nil
}

func main() {
	if err := gravwell.Execute(PluginName, Config, nop, nop, Process, Flush); err != nil {
		panic(fmt.Sprintf("Failed to execute dynamic plugin %s - %v\n", PluginName, err))
	}
}`

// a basic valid plugin that imports EVERYTHING
const allPackages = `
package main

import (
	"gravwell"

	"github.com/gravwell/gravwell/v3/ingest/entry"

	// all the imports
	_ "github.com/gravwell/gravwell/v3/ingest"
	_ "github.com/gravwell/gravwell/v3/ingest/config"
	_ "github.com/crewjam/rfc5424"
	_ "github.com/dchest/safefile"
	_ "github.com/gobwas/glob"
	_ "github.com/gofrs/flock"
	_ "github.com/google/gopacket"
	_ "github.com/google/uuid"
	_ "github.com/gravwell/ipfix"
	_ "github.com/h2non/filetype"
	_ "github.com/k-sone/ipmigo"
	_ "github.com/klauspost/compress"
	_ "github.com/tealeg/xlsx"
	_ "github.com/miekg/dns"
	_ "github.com/gravwell/jsonparser"
	_ "archive/tar"
	_ "archive/zip"
	_ "bufio"
	_ "bytes"
	_ "compress/bzip2"
	_ "compress/flate"
	_ "compress/gzip"
	_ "compress/lzw"
	_ "compress/zlib"
	_ "container/heap"
	_ "container/list"
	_ "container/ring"
	_ "context"
	_ "crypto"
	_ "crypto/aes"
	_ "crypto/cipher"
	_ "crypto/des"
	_ "crypto/dsa"
	_ "crypto/ecdsa"
	_ "crypto/elliptic"
	_ "crypto/hmac"
	_ "crypto/md5"
	_ "crypto/rand"
	_ "crypto/rc4"
	_ "crypto/rsa"
	_ "crypto/sha1"
	_ "crypto/sha256"
	_ "crypto/sha512"
	_ "crypto/subtle"
	_ "crypto/tls"
	_ "crypto/x509"
	_ "crypto/x509/pkix"
	_ "encoding"
	_ "encoding/ascii85"
	_ "encoding/asn1"
	_ "encoding/base32"
	_ "encoding/base64"
	_ "encoding/binary"
	_ "encoding/csv"
	_ "encoding/gob"
	_ "encoding/hex"
	_ "encoding/json"
	_ "encoding/pem"
	_ "encoding/xml"
	_ "errors"
	_ "expvar"
	_ "flag"
	_ "fmt"
	_ "go/ast"
	_ "go/build"
	_ "go/constant"
	_ "go/doc"
	_ "go/format"
	_ "go/importer"
	_ "go/parser"
	_ "go/printer"
	_ "go/scanner"
	_ "go/token"
	_ "go/types"
	_ "hash"
	_ "hash/adler32"
	_ "hash/crc32"
	_ "hash/crc64"
	_ "hash/fnv"
	_ "hash/maphash"
	_ "html"
	_ "html/template"
	_ "image"
	_ "image/color"
	_ "image/color/palette"
	_ "image/draw"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	_ "index/suffixarray"
	_ "io"
	_ "io/fs"
	_ "io/ioutil"
	_ "log"
	_ "log/syslog"
	_ "math"
	_ "math/big"
	_ "math/bits"
	_ "math/cmplx"
	_ "math/rand"
	_ "mime"
	_ "mime/multipart"
	_ "mime/quotedprintable"
	_ "net"
	_ "net/http"
	_ "net/http/cgi"
	_ "net/http/cookiejar"
	_ "net/http/fcgi"
	_ "net/http/httptest"
	_ "net/http/httptrace"
	_ "net/http/httputil"
	_ "net/http/pprof"
	_ "net/mail"
	_ "net/rpc"
	_ "net/rpc/jsonrpc"
	_ "net/smtp"
	_ "net/textproto"
	_ "net/url"
	_ "os"
	_ "os/exec"
	_ "os/user"
	_ "path"
	_ "path/filepath"
	_ "reflect"
	_ "regexp"
	_ "regexp/syntax"
	_ "runtime/debug"
	_ "sort"
	_ "strconv"
	_ "strings"
	_ "sync"
	_ "sync/atomic"
	_ "text/scanner"
	_ "text/tabwriter"
	_ "text/template"
	_ "text/template/parse"
	_ "time"
	_ "unicode"
	_ "unicode/utf16"
	_ "unicode/utf8"

)

func main() {
	gravwell.Execute("test", cf, nop, nop, pf, ff)
}

func cf(cm gravwell.ConfigMap, tg gravwell.Tagger) error {
	return nil
}

func ff() []*entry.Entry {
	return nil
}

func pf([]*entry.Entry) ([]*entry.Entry, error) {
	return nil, nil
}

func nop() error {
	return nil
}`

// a basic valid plugin that does shiftJIS language conversions
const shiftJISPlugin = `
package main

import (
	"gravwell"

	"github.com/gravwell/gravwell/v3/ingest/entry"
	"golang.org/x/text/encoding/japanese"
)

func main() {
	gravwell.Execute("test", cf, nop, nop, pf, ff)
}

func cf(cm gravwell.ConfigMap, tg gravwell.Tagger) error {
	return nil
}

func ff() []*entry.Entry {
	return nil
}

func pf(ents []*entry.Entry) ([]*entry.Entry, error) {
	dec := japanese.ShiftJIS.NewDecoder()
	for i := range ents {
		if nv, err := dec.Bytes(ents[i].Data); err == nil {
			ents[i].Data = nv
		}
	}
	return ents, nil
}

func nop() error {
	return nil
}
`
