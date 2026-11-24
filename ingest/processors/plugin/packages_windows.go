//go:build windows

package plugin

import (
	tar "archive/tar"
	zip "archive/zip"
	bufio "bufio"
	bytes "bytes"
	bzip2 "compress/bzip2"
	flate "compress/flate"
	gzip "compress/gzip"
	lzw "compress/lzw"
	zlib "compress/zlib"
	heap "container/heap"
	list "container/list"
	ring "container/ring"
	context "context"
	crypto "crypto"
	aes "crypto/aes"
	cipher "crypto/cipher"
	des "crypto/des"
	dsa "crypto/dsa"
	ecdsa "crypto/ecdsa"
	elliptic "crypto/elliptic"
	hmac "crypto/hmac"
	md5 "crypto/md5"
	rand "crypto/rand"
	rc4 "crypto/rc4"
	rsa "crypto/rsa"
	sha1 "crypto/sha1"
	sha256 "crypto/sha256"
	sha512 "crypto/sha512"
	subtle "crypto/subtle"
	tls "crypto/tls"
	x509 "crypto/x509"
	pkix "crypto/x509/pkix"
	encoding "encoding"
	ascii85 "encoding/ascii85"
	asn1 "encoding/asn1"
	base32 "encoding/base32"
	base64 "encoding/base64"
	binary "encoding/binary"
	csv "encoding/csv"
	gob "encoding/gob"
	hex "encoding/hex"
	json "encoding/json"
	pem "encoding/pem"
	xml "encoding/xml"
	errors "errors"
	expvar "expvar"
	flag "flag"
	fmt "fmt"
	ast "go/ast"
	build "go/build"
	constant "go/constant"
	doc "go/doc"
	format "go/format"
	importer "go/importer"
	parser "go/parser"
	printer "go/printer"
	scanner "go/scanner"
	token "go/token"
	types "go/types"
	hash "hash"
	adler32 "hash/adler32"
	crc32 "hash/crc32"
	crc64 "hash/crc64"
	fnv "hash/fnv"
	maphash "hash/maphash"
	html "html"
	template "html/template"
	image "image"
	color "image/color"
	palette "image/color/palette"
	draw "image/draw"
	gif "image/gif"
	jpeg "image/jpeg"
	png "image/png"
	suffixarray "index/suffixarray"
	io "io"
	fs "io/fs"
	ioutil "io/ioutil"
	log "log"
	math "math"
	big "math/big"
	bits "math/bits"
	cmplx "math/cmplx"
	rand_2 "math/rand"
	mime "mime"
	multipart "mime/multipart"
	quotedprintable "mime/quotedprintable"
	net "net"
	http "net/http"
	cgi "net/http/cgi"
	cookiejar "net/http/cookiejar"
	fcgi "net/http/fcgi"
	httptest "net/http/httptest"
	httptrace "net/http/httptrace"
	httputil "net/http/httputil"
	pprof "net/http/pprof"
	mail "net/mail"
	rpc "net/rpc"
	jsonrpc "net/rpc/jsonrpc"
	smtp "net/smtp"
	textproto "net/textproto"
	url "net/url"
	os "os"
	exec "os/exec"
	user "os/user"
	path "path"
	filepath "path/filepath"
	reflect "reflect"
	regexp "regexp"
	syntax "regexp/syntax"
	debug "runtime/debug"
	sort "sort"
	strconv "strconv"
	strings "strings"
	sync "sync"
	atomic "sync/atomic"
	scanner_2 "text/scanner"
	tabwriter "text/tabwriter"
	template_2 "text/template"
	parse "text/template/parse"
	time "time"
	_ "time/tzdata"
	unicode "unicode"
	utf16 "unicode/utf16"
	utf8 "unicode/utf8"

	rfc5424 "github.com/crewjam/rfc5424"
	glob "github.com/gobwas/glob"
	uuid "github.com/google/uuid"
	ingest "github.com/gravwell/gravwell/v4/ingest"
	config "github.com/gravwell/gravwell/v4/ingest/config"
	entry "github.com/gravwell/gravwell/v4/ingest/entry"
	ingestLog "github.com/gravwell/gravwell/v4/ingest/log"
	ipfix "github.com/gravwell/ipfix"
	jsonparser_2 "github.com/gravwell/jsonparser"
	filetype "github.com/h2non/filetype"
	compress "github.com/klauspost/compress"
	dns "github.com/miekg/dns"
	"github.com/open2b/scriggo/native"
	japanese "golang.org/x/text/encoding/japanese"
)

func init() {
	packages = make(native.Packages, 137)
	var decs native.Declarations
	// "archive/tar"
	decs = make(native.Declarations, 30)
	decs["ErrFieldTooLong"] = &tar.ErrFieldTooLong
	decs["ErrHeader"] = &tar.ErrHeader
	decs["ErrInsecurePath"] = &tar.ErrInsecurePath
	decs["ErrWriteAfterClose"] = &tar.ErrWriteAfterClose
	decs["ErrWriteTooLong"] = &tar.ErrWriteTooLong
	decs["FileInfoHeader"] = tar.FileInfoHeader
	decs["Format"] = reflect.TypeFor[tar.Format]()
	decs["FormatGNU"] = tar.FormatGNU
	decs["FormatPAX"] = tar.FormatPAX
	decs["FormatUSTAR"] = tar.FormatUSTAR
	decs["FormatUnknown"] = tar.FormatUnknown
	decs["Header"] = reflect.TypeFor[tar.Header]()
	decs["NewReader"] = tar.NewReader
	decs["NewWriter"] = tar.NewWriter
	decs["Reader"] = reflect.TypeFor[tar.Reader]()
	decs["TypeBlock"] = native.UntypedNumericConst("52")
	decs["TypeChar"] = native.UntypedNumericConst("51")
	decs["TypeCont"] = native.UntypedNumericConst("55")
	decs["TypeDir"] = native.UntypedNumericConst("53")
	decs["TypeFifo"] = native.UntypedNumericConst("54")
	decs["TypeGNULongLink"] = native.UntypedNumericConst("75")
	decs["TypeGNULongName"] = native.UntypedNumericConst("76")
	decs["TypeGNUSparse"] = native.UntypedNumericConst("83")
	decs["TypeLink"] = native.UntypedNumericConst("49")
	decs["TypeReg"] = native.UntypedNumericConst("48")
	decs["TypeRegA"] = native.UntypedNumericConst("0")
	decs["TypeSymlink"] = native.UntypedNumericConst("50")
	decs["TypeXGlobalHeader"] = native.UntypedNumericConst("103")
	decs["TypeXHeader"] = native.UntypedNumericConst("120")
	decs["Writer"] = reflect.TypeFor[tar.Writer]()
	packages["archive/tar"] = native.Package{
		Name:         "tar",
		Declarations: decs,
	}
	// "archive/zip"
	decs = make(native.Declarations, 19)
	decs["Compressor"] = reflect.TypeFor[zip.Compressor]()
	decs["Decompressor"] = reflect.TypeFor[zip.Decompressor]()
	decs["Deflate"] = zip.Deflate
	decs["ErrAlgorithm"] = &zip.ErrAlgorithm
	decs["ErrChecksum"] = &zip.ErrChecksum
	decs["ErrFormat"] = &zip.ErrFormat
	decs["ErrInsecurePath"] = &zip.ErrInsecurePath
	decs["File"] = reflect.TypeFor[zip.File]()
	decs["FileHeader"] = reflect.TypeFor[zip.FileHeader]()
	decs["FileInfoHeader"] = zip.FileInfoHeader
	decs["NewReader"] = zip.NewReader
	decs["NewWriter"] = zip.NewWriter
	decs["OpenReader"] = zip.OpenReader
	decs["ReadCloser"] = reflect.TypeFor[zip.ReadCloser]()
	decs["Reader"] = reflect.TypeFor[zip.Reader]()
	decs["RegisterCompressor"] = zip.RegisterCompressor
	decs["RegisterDecompressor"] = zip.RegisterDecompressor
	decs["Store"] = zip.Store
	decs["Writer"] = reflect.TypeFor[zip.Writer]()
	packages["archive/zip"] = native.Package{
		Name:         "zip",
		Declarations: decs,
	}
	// "bufio"
	decs = make(native.Declarations, 25)
	decs["ErrAdvanceTooFar"] = &bufio.ErrAdvanceTooFar
	decs["ErrBadReadCount"] = &bufio.ErrBadReadCount
	decs["ErrBufferFull"] = &bufio.ErrBufferFull
	decs["ErrFinalToken"] = &bufio.ErrFinalToken
	decs["ErrInvalidUnreadByte"] = &bufio.ErrInvalidUnreadByte
	decs["ErrInvalidUnreadRune"] = &bufio.ErrInvalidUnreadRune
	decs["ErrNegativeAdvance"] = &bufio.ErrNegativeAdvance
	decs["ErrNegativeCount"] = &bufio.ErrNegativeCount
	decs["ErrTooLong"] = &bufio.ErrTooLong
	decs["MaxScanTokenSize"] = native.UntypedNumericConst("65536")
	decs["NewReadWriter"] = bufio.NewReadWriter
	decs["NewReader"] = bufio.NewReader
	decs["NewReaderSize"] = bufio.NewReaderSize
	decs["NewScanner"] = bufio.NewScanner
	decs["NewWriter"] = bufio.NewWriter
	decs["NewWriterSize"] = bufio.NewWriterSize
	decs["ReadWriter"] = reflect.TypeFor[bufio.ReadWriter]()
	decs["Reader"] = reflect.TypeFor[bufio.Reader]()
	decs["ScanBytes"] = bufio.ScanBytes
	decs["ScanLines"] = bufio.ScanLines
	decs["ScanRunes"] = bufio.ScanRunes
	decs["ScanWords"] = bufio.ScanWords
	decs["Scanner"] = reflect.TypeFor[bufio.Scanner]()
	decs["SplitFunc"] = reflect.TypeFor[bufio.SplitFunc]()
	decs["Writer"] = reflect.TypeFor[bufio.Writer]()
	packages["bufio"] = native.Package{
		Name:         "bufio",
		Declarations: decs,
	}
	// "bytes"
	decs = make(native.Declarations, 59)
	decs["Buffer"] = reflect.TypeFor[bytes.Buffer]()
	decs["Clone"] = bytes.Clone
	decs["Compare"] = bytes.Compare
	decs["Contains"] = bytes.Contains
	decs["ContainsAny"] = bytes.ContainsAny
	decs["ContainsFunc"] = bytes.ContainsFunc
	decs["ContainsRune"] = bytes.ContainsRune
	decs["Count"] = bytes.Count
	decs["Cut"] = bytes.Cut
	decs["CutPrefix"] = bytes.CutPrefix
	decs["CutSuffix"] = bytes.CutSuffix
	decs["Equal"] = bytes.Equal
	decs["EqualFold"] = bytes.EqualFold
	decs["ErrTooLarge"] = &bytes.ErrTooLarge
	decs["Fields"] = bytes.Fields
	decs["FieldsFunc"] = bytes.FieldsFunc
	decs["HasPrefix"] = bytes.HasPrefix
	decs["HasSuffix"] = bytes.HasSuffix
	decs["Index"] = bytes.Index
	decs["IndexAny"] = bytes.IndexAny
	decs["IndexByte"] = bytes.IndexByte
	decs["IndexFunc"] = bytes.IndexFunc
	decs["IndexRune"] = bytes.IndexRune
	decs["Join"] = bytes.Join
	decs["LastIndex"] = bytes.LastIndex
	decs["LastIndexAny"] = bytes.LastIndexAny
	decs["LastIndexByte"] = bytes.LastIndexByte
	decs["LastIndexFunc"] = bytes.LastIndexFunc
	decs["Map"] = bytes.Map
	decs["MinRead"] = native.UntypedNumericConst("512")
	decs["NewBuffer"] = bytes.NewBuffer
	decs["NewBufferString"] = bytes.NewBufferString
	decs["NewReader"] = bytes.NewReader
	decs["Reader"] = reflect.TypeFor[bytes.Reader]()
	decs["Repeat"] = bytes.Repeat
	decs["Replace"] = bytes.Replace
	decs["ReplaceAll"] = bytes.ReplaceAll
	decs["Runes"] = bytes.Runes
	decs["Split"] = bytes.Split
	decs["SplitAfter"] = bytes.SplitAfter
	decs["SplitAfterN"] = bytes.SplitAfterN
	decs["SplitN"] = bytes.SplitN
	decs["Title"] = bytes.Title
	decs["ToLower"] = bytes.ToLower
	decs["ToLowerSpecial"] = bytes.ToLowerSpecial
	decs["ToTitle"] = bytes.ToTitle
	decs["ToTitleSpecial"] = bytes.ToTitleSpecial
	decs["ToUpper"] = bytes.ToUpper
	decs["ToUpperSpecial"] = bytes.ToUpperSpecial
	decs["ToValidUTF8"] = bytes.ToValidUTF8
	decs["Trim"] = bytes.Trim
	decs["TrimFunc"] = bytes.TrimFunc
	decs["TrimLeft"] = bytes.TrimLeft
	decs["TrimLeftFunc"] = bytes.TrimLeftFunc
	decs["TrimPrefix"] = bytes.TrimPrefix
	decs["TrimRight"] = bytes.TrimRight
	decs["TrimRightFunc"] = bytes.TrimRightFunc
	decs["TrimSpace"] = bytes.TrimSpace
	decs["TrimSuffix"] = bytes.TrimSuffix
	packages["bytes"] = native.Package{
		Name:         "bytes",
		Declarations: decs,
	}
	// "compress/bzip2"
	decs = make(native.Declarations, 2)
	decs["NewReader"] = bzip2.NewReader
	decs["StructuralError"] = reflect.TypeFor[bzip2.StructuralError]()
	packages["compress/bzip2"] = native.Package{
		Name:         "bzip2",
		Declarations: decs,
	}
	// "compress/flate"
	decs = make(native.Declarations, 16)
	decs["BestCompression"] = native.UntypedNumericConst("9")
	decs["BestSpeed"] = native.UntypedNumericConst("1")
	decs["CorruptInputError"] = reflect.TypeFor[flate.CorruptInputError]()
	decs["DefaultCompression"] = native.UntypedNumericConst("-1")
	decs["HuffmanOnly"] = native.UntypedNumericConst("-2")
	decs["InternalError"] = reflect.TypeFor[flate.InternalError]()
	decs["NewReader"] = flate.NewReader
	decs["NewReaderDict"] = flate.NewReaderDict
	decs["NewWriter"] = flate.NewWriter
	decs["NewWriterDict"] = flate.NewWriterDict
	decs["NoCompression"] = native.UntypedNumericConst("0")
	decs["ReadError"] = reflect.TypeFor[flate.ReadError]()
	decs["Reader"] = reflect.TypeFor[flate.Reader]()
	decs["Resetter"] = reflect.TypeFor[flate.Resetter]()
	decs["WriteError"] = reflect.TypeFor[flate.WriteError]()
	decs["Writer"] = reflect.TypeFor[flate.Writer]()
	packages["compress/flate"] = native.Package{
		Name:         "flate",
		Declarations: decs,
	}
	// "compress/gzip"
	decs = make(native.Declarations, 13)
	decs["BestCompression"] = native.UntypedNumericConst("9")
	decs["BestSpeed"] = native.UntypedNumericConst("1")
	decs["DefaultCompression"] = native.UntypedNumericConst("-1")
	decs["ErrChecksum"] = &gzip.ErrChecksum
	decs["ErrHeader"] = &gzip.ErrHeader
	decs["Header"] = reflect.TypeFor[gzip.Header]()
	decs["HuffmanOnly"] = native.UntypedNumericConst("-2")
	decs["NewReader"] = gzip.NewReader
	decs["NewWriter"] = gzip.NewWriter
	decs["NewWriterLevel"] = gzip.NewWriterLevel
	decs["NoCompression"] = native.UntypedNumericConst("0")
	decs["Reader"] = reflect.TypeFor[gzip.Reader]()
	decs["Writer"] = reflect.TypeFor[gzip.Writer]()
	packages["compress/gzip"] = native.Package{
		Name:         "gzip",
		Declarations: decs,
	}
	// "compress/lzw"
	decs = make(native.Declarations, 7)
	decs["LSB"] = lzw.LSB
	decs["MSB"] = lzw.MSB
	decs["NewReader"] = lzw.NewReader
	decs["NewWriter"] = lzw.NewWriter
	decs["Order"] = reflect.TypeFor[lzw.Order]()
	decs["Reader"] = reflect.TypeFor[lzw.Reader]()
	decs["Writer"] = reflect.TypeFor[lzw.Writer]()
	packages["compress/lzw"] = native.Package{
		Name:         "lzw",
		Declarations: decs,
	}
	// "compress/zlib"
	decs = make(native.Declarations, 15)
	decs["BestCompression"] = native.UntypedNumericConst("9")
	decs["BestSpeed"] = native.UntypedNumericConst("1")
	decs["DefaultCompression"] = native.UntypedNumericConst("-1")
	decs["ErrChecksum"] = &zlib.ErrChecksum
	decs["ErrDictionary"] = &zlib.ErrDictionary
	decs["ErrHeader"] = &zlib.ErrHeader
	decs["HuffmanOnly"] = native.UntypedNumericConst("-2")
	decs["NewReader"] = zlib.NewReader
	decs["NewReaderDict"] = zlib.NewReaderDict
	decs["NewWriter"] = zlib.NewWriter
	decs["NewWriterLevel"] = zlib.NewWriterLevel
	decs["NewWriterLevelDict"] = zlib.NewWriterLevelDict
	decs["NoCompression"] = native.UntypedNumericConst("0")
	decs["Resetter"] = reflect.TypeFor[zlib.Resetter]()
	decs["Writer"] = reflect.TypeFor[zlib.Writer]()
	packages["compress/zlib"] = native.Package{
		Name:         "zlib",
		Declarations: decs,
	}
	// "container/heap"
	decs = make(native.Declarations, 6)
	decs["Fix"] = heap.Fix
	decs["Init"] = heap.Init
	decs["Interface"] = reflect.TypeFor[heap.Interface]()
	decs["Pop"] = heap.Pop
	decs["Push"] = heap.Push
	decs["Remove"] = heap.Remove
	packages["container/heap"] = native.Package{
		Name:         "heap",
		Declarations: decs,
	}
	// "container/list"
	decs = make(native.Declarations, 3)
	decs["Element"] = reflect.TypeFor[list.Element]()
	decs["List"] = reflect.TypeFor[list.List]()
	decs["New"] = list.New
	packages["container/list"] = native.Package{
		Name:         "list",
		Declarations: decs,
	}
	// "container/ring"
	decs = make(native.Declarations, 2)
	decs["New"] = ring.New
	decs["Ring"] = reflect.TypeFor[ring.Ring]()
	packages["container/ring"] = native.Package{
		Name:         "ring",
		Declarations: decs,
	}
	// "context"
	decs = make(native.Declarations, 17)
	decs["AfterFunc"] = context.AfterFunc
	decs["Background"] = context.Background
	decs["CancelCauseFunc"] = reflect.TypeFor[context.CancelCauseFunc]()
	decs["CancelFunc"] = reflect.TypeFor[context.CancelFunc]()
	decs["Canceled"] = &context.Canceled
	decs["Cause"] = context.Cause
	decs["Context"] = reflect.TypeFor[context.Context]()
	decs["DeadlineExceeded"] = &context.DeadlineExceeded
	decs["TODO"] = context.TODO
	decs["WithCancel"] = context.WithCancel
	decs["WithCancelCause"] = context.WithCancelCause
	decs["WithDeadline"] = context.WithDeadline
	decs["WithDeadlineCause"] = context.WithDeadlineCause
	decs["WithTimeout"] = context.WithTimeout
	decs["WithTimeoutCause"] = context.WithTimeoutCause
	decs["WithValue"] = context.WithValue
	decs["WithoutCancel"] = context.WithoutCancel
	packages["context"] = native.Package{
		Name:         "context",
		Declarations: decs,
	}
	// "crypto"
	decs = make(native.Declarations, 27)
	decs["BLAKE2b_256"] = crypto.BLAKE2b_256
	decs["BLAKE2b_384"] = crypto.BLAKE2b_384
	decs["BLAKE2b_512"] = crypto.BLAKE2b_512
	decs["BLAKE2s_256"] = crypto.BLAKE2s_256
	decs["Decrypter"] = reflect.TypeFor[crypto.Decrypter]()
	decs["DecrypterOpts"] = reflect.TypeFor[crypto.DecrypterOpts]()
	decs["Hash"] = reflect.TypeFor[crypto.Hash]()
	decs["MD4"] = crypto.MD4
	decs["MD5"] = crypto.MD5
	decs["MD5SHA1"] = crypto.MD5SHA1
	decs["PrivateKey"] = reflect.TypeFor[crypto.PrivateKey]()
	decs["PublicKey"] = reflect.TypeFor[crypto.PublicKey]()
	decs["RIPEMD160"] = crypto.RIPEMD160
	decs["RegisterHash"] = crypto.RegisterHash
	decs["SHA1"] = crypto.SHA1
	decs["SHA224"] = crypto.SHA224
	decs["SHA256"] = crypto.SHA256
	decs["SHA384"] = crypto.SHA384
	decs["SHA3_224"] = crypto.SHA3_224
	decs["SHA3_256"] = crypto.SHA3_256
	decs["SHA3_384"] = crypto.SHA3_384
	decs["SHA3_512"] = crypto.SHA3_512
	decs["SHA512"] = crypto.SHA512
	decs["SHA512_224"] = crypto.SHA512_224
	decs["SHA512_256"] = crypto.SHA512_256
	decs["Signer"] = reflect.TypeFor[crypto.Signer]()
	decs["SignerOpts"] = reflect.TypeFor[crypto.SignerOpts]()
	packages["crypto"] = native.Package{
		Name:         "crypto",
		Declarations: decs,
	}
	// "crypto/aes"
	decs = make(native.Declarations, 3)
	decs["BlockSize"] = native.UntypedNumericConst("16")
	decs["KeySizeError"] = reflect.TypeFor[aes.KeySizeError]()
	decs["NewCipher"] = aes.NewCipher
	packages["crypto/aes"] = native.Package{
		Name:         "aes",
		Declarations: decs,
	}
	// "crypto/cipher"
	decs = make(native.Declarations, 15)
	decs["AEAD"] = reflect.TypeFor[cipher.AEAD]()
	decs["Block"] = reflect.TypeFor[cipher.Block]()
	decs["BlockMode"] = reflect.TypeFor[cipher.BlockMode]()
	decs["NewCBCDecrypter"] = cipher.NewCBCDecrypter
	decs["NewCBCEncrypter"] = cipher.NewCBCEncrypter
	decs["NewCFBDecrypter"] = cipher.NewCFBDecrypter
	decs["NewCFBEncrypter"] = cipher.NewCFBEncrypter
	decs["NewCTR"] = cipher.NewCTR
	decs["NewGCM"] = cipher.NewGCM
	decs["NewGCMWithNonceSize"] = cipher.NewGCMWithNonceSize
	decs["NewGCMWithTagSize"] = cipher.NewGCMWithTagSize
	decs["NewOFB"] = cipher.NewOFB
	decs["Stream"] = reflect.TypeFor[cipher.Stream]()
	decs["StreamReader"] = reflect.TypeFor[cipher.StreamReader]()
	decs["StreamWriter"] = reflect.TypeFor[cipher.StreamWriter]()
	packages["crypto/cipher"] = native.Package{
		Name:         "cipher",
		Declarations: decs,
	}
	// "crypto/des"
	decs = make(native.Declarations, 4)
	decs["BlockSize"] = native.UntypedNumericConst("8")
	decs["KeySizeError"] = reflect.TypeFor[des.KeySizeError]()
	decs["NewCipher"] = des.NewCipher
	decs["NewTripleDESCipher"] = des.NewTripleDESCipher
	packages["crypto/des"] = native.Package{
		Name:         "des",
		Declarations: decs,
	}
	// "crypto/dsa"
	decs = make(native.Declarations, 13)
	decs["ErrInvalidPublicKey"] = &dsa.ErrInvalidPublicKey
	decs["GenerateKey"] = dsa.GenerateKey
	decs["GenerateParameters"] = dsa.GenerateParameters
	decs["L1024N160"] = dsa.L1024N160
	decs["L2048N224"] = dsa.L2048N224
	decs["L2048N256"] = dsa.L2048N256
	decs["L3072N256"] = dsa.L3072N256
	decs["ParameterSizes"] = reflect.TypeFor[dsa.ParameterSizes]()
	decs["Parameters"] = reflect.TypeFor[dsa.Parameters]()
	decs["PrivateKey"] = reflect.TypeFor[dsa.PrivateKey]()
	decs["PublicKey"] = reflect.TypeFor[dsa.PublicKey]()
	decs["Sign"] = dsa.Sign
	decs["Verify"] = dsa.Verify
	packages["crypto/dsa"] = native.Package{
		Name:         "dsa",
		Declarations: decs,
	}
	// "crypto/ecdsa"
	decs = make(native.Declarations, 7)
	decs["GenerateKey"] = ecdsa.GenerateKey
	decs["PrivateKey"] = reflect.TypeFor[ecdsa.PrivateKey]()
	decs["PublicKey"] = reflect.TypeFor[ecdsa.PublicKey]()
	decs["Sign"] = ecdsa.Sign
	decs["SignASN1"] = ecdsa.SignASN1
	decs["Verify"] = ecdsa.Verify
	decs["VerifyASN1"] = ecdsa.VerifyASN1
	packages["crypto/ecdsa"] = native.Package{
		Name:         "ecdsa",
		Declarations: decs,
	}
	// "crypto/elliptic"
	decs = make(native.Declarations, 11)
	decs["Curve"] = reflect.TypeFor[elliptic.Curve]()
	decs["CurveParams"] = reflect.TypeFor[elliptic.CurveParams]()
	decs["GenerateKey"] = elliptic.GenerateKey
	decs["Marshal"] = elliptic.Marshal
	decs["MarshalCompressed"] = elliptic.MarshalCompressed
	decs["P224"] = elliptic.P224
	decs["P256"] = elliptic.P256
	decs["P384"] = elliptic.P384
	decs["P521"] = elliptic.P521
	decs["Unmarshal"] = elliptic.Unmarshal
	decs["UnmarshalCompressed"] = elliptic.UnmarshalCompressed
	packages["crypto/elliptic"] = native.Package{
		Name:         "elliptic",
		Declarations: decs,
	}
	// "crypto/hmac"
	decs = make(native.Declarations, 2)
	decs["Equal"] = hmac.Equal
	decs["New"] = hmac.New
	packages["crypto/hmac"] = native.Package{
		Name:         "hmac",
		Declarations: decs,
	}
	// "crypto/md5"
	decs = make(native.Declarations, 4)
	decs["BlockSize"] = native.UntypedNumericConst("64")
	decs["New"] = md5.New
	decs["Size"] = native.UntypedNumericConst("16")
	decs["Sum"] = md5.Sum
	packages["crypto/md5"] = native.Package{
		Name:         "md5",
		Declarations: decs,
	}
	// "crypto/rand"
	decs = make(native.Declarations, 4)
	decs["Int"] = rand.Int
	decs["Prime"] = rand.Prime
	decs["Read"] = rand.Read
	decs["Reader"] = &rand.Reader
	packages["crypto/rand"] = native.Package{
		Name:         "rand",
		Declarations: decs,
	}
	// "crypto/rc4"
	decs = make(native.Declarations, 3)
	decs["Cipher"] = reflect.TypeFor[rc4.Cipher]()
	decs["KeySizeError"] = reflect.TypeFor[rc4.KeySizeError]()
	decs["NewCipher"] = rc4.NewCipher
	packages["crypto/rc4"] = native.Package{
		Name:         "rc4",
		Declarations: decs,
	}
	// "crypto/rsa"
	decs = make(native.Declarations, 23)
	decs["CRTValue"] = reflect.TypeFor[rsa.CRTValue]()
	decs["DecryptOAEP"] = rsa.DecryptOAEP
	decs["DecryptPKCS1v15"] = rsa.DecryptPKCS1v15
	decs["DecryptPKCS1v15SessionKey"] = rsa.DecryptPKCS1v15SessionKey
	decs["EncryptOAEP"] = rsa.EncryptOAEP
	decs["EncryptPKCS1v15"] = rsa.EncryptPKCS1v15
	decs["ErrDecryption"] = &rsa.ErrDecryption
	decs["ErrMessageTooLong"] = &rsa.ErrMessageTooLong
	decs["ErrVerification"] = &rsa.ErrVerification
	decs["GenerateKey"] = rsa.GenerateKey
	decs["GenerateMultiPrimeKey"] = rsa.GenerateMultiPrimeKey
	decs["OAEPOptions"] = reflect.TypeFor[rsa.OAEPOptions]()
	decs["PKCS1v15DecryptOptions"] = reflect.TypeFor[rsa.PKCS1v15DecryptOptions]()
	decs["PSSOptions"] = reflect.TypeFor[rsa.PSSOptions]()
	decs["PSSSaltLengthAuto"] = native.UntypedNumericConst("0")
	decs["PSSSaltLengthEqualsHash"] = native.UntypedNumericConst("-1")
	decs["PrecomputedValues"] = reflect.TypeFor[rsa.PrecomputedValues]()
	decs["PrivateKey"] = reflect.TypeFor[rsa.PrivateKey]()
	decs["PublicKey"] = reflect.TypeFor[rsa.PublicKey]()
	decs["SignPKCS1v15"] = rsa.SignPKCS1v15
	decs["SignPSS"] = rsa.SignPSS
	decs["VerifyPKCS1v15"] = rsa.VerifyPKCS1v15
	decs["VerifyPSS"] = rsa.VerifyPSS
	packages["crypto/rsa"] = native.Package{
		Name:         "rsa",
		Declarations: decs,
	}
	// "crypto/sha1"
	decs = make(native.Declarations, 4)
	decs["BlockSize"] = native.UntypedNumericConst("64")
	decs["New"] = sha1.New
	decs["Size"] = native.UntypedNumericConst("20")
	decs["Sum"] = sha1.Sum
	packages["crypto/sha1"] = native.Package{
		Name:         "sha1",
		Declarations: decs,
	}
	// "crypto/sha256"
	decs = make(native.Declarations, 7)
	decs["BlockSize"] = native.UntypedNumericConst("64")
	decs["New"] = sha256.New
	decs["New224"] = sha256.New224
	decs["Size"] = native.UntypedNumericConst("32")
	decs["Size224"] = native.UntypedNumericConst("28")
	decs["Sum224"] = sha256.Sum224
	decs["Sum256"] = sha256.Sum256
	packages["crypto/sha256"] = native.Package{
		Name:         "sha256",
		Declarations: decs,
	}
	// "crypto/sha512"
	decs = make(native.Declarations, 13)
	decs["BlockSize"] = native.UntypedNumericConst("128")
	decs["New"] = sha512.New
	decs["New384"] = sha512.New384
	decs["New512_224"] = sha512.New512_224
	decs["New512_256"] = sha512.New512_256
	decs["Size"] = native.UntypedNumericConst("64")
	decs["Size224"] = native.UntypedNumericConst("28")
	decs["Size256"] = native.UntypedNumericConst("32")
	decs["Size384"] = native.UntypedNumericConst("48")
	decs["Sum384"] = sha512.Sum384
	decs["Sum512"] = sha512.Sum512
	decs["Sum512_224"] = sha512.Sum512_224
	decs["Sum512_256"] = sha512.Sum512_256
	packages["crypto/sha512"] = native.Package{
		Name:         "sha512",
		Declarations: decs,
	}
	// "crypto/subtle"
	decs = make(native.Declarations, 7)
	decs["ConstantTimeByteEq"] = subtle.ConstantTimeByteEq
	decs["ConstantTimeCompare"] = subtle.ConstantTimeCompare
	decs["ConstantTimeCopy"] = subtle.ConstantTimeCopy
	decs["ConstantTimeEq"] = subtle.ConstantTimeEq
	decs["ConstantTimeLessOrEq"] = subtle.ConstantTimeLessOrEq
	decs["ConstantTimeSelect"] = subtle.ConstantTimeSelect
	decs["XORBytes"] = subtle.XORBytes
	packages["crypto/subtle"] = native.Package{
		Name:         "subtle",
		Declarations: decs,
	}
	// "crypto/tls"
	decs = make(native.Declarations, 110)
	decs["AlertError"] = reflect.TypeFor[tls.AlertError]()
	decs["Certificate"] = reflect.TypeFor[tls.Certificate]()
	decs["CertificateRequestInfo"] = reflect.TypeFor[tls.CertificateRequestInfo]()
	decs["CertificateVerificationError"] = reflect.TypeFor[tls.CertificateVerificationError]()
	decs["CipherSuite"] = reflect.TypeFor[tls.CipherSuite]()
	decs["CipherSuiteName"] = tls.CipherSuiteName
	decs["CipherSuites"] = tls.CipherSuites
	decs["Client"] = tls.Client
	decs["ClientAuthType"] = reflect.TypeFor[tls.ClientAuthType]()
	decs["ClientHelloInfo"] = reflect.TypeFor[tls.ClientHelloInfo]()
	decs["ClientSessionCache"] = reflect.TypeFor[tls.ClientSessionCache]()
	decs["ClientSessionState"] = reflect.TypeFor[tls.ClientSessionState]()
	decs["Config"] = reflect.TypeFor[tls.Config]()
	decs["Conn"] = reflect.TypeFor[tls.Conn]()
	decs["ConnectionState"] = reflect.TypeFor[tls.ConnectionState]()
	decs["CurveID"] = reflect.TypeFor[tls.CurveID]()
	decs["CurveP256"] = tls.CurveP256
	decs["CurveP384"] = tls.CurveP384
	decs["CurveP521"] = tls.CurveP521
	decs["Dial"] = tls.Dial
	decs["DialWithDialer"] = tls.DialWithDialer
	decs["Dialer"] = reflect.TypeFor[tls.Dialer]()
	decs["ECDSAWithP256AndSHA256"] = tls.ECDSAWithP256AndSHA256
	decs["ECDSAWithP384AndSHA384"] = tls.ECDSAWithP384AndSHA384
	decs["ECDSAWithP521AndSHA512"] = tls.ECDSAWithP521AndSHA512
	decs["ECDSAWithSHA1"] = tls.ECDSAWithSHA1
	decs["Ed25519"] = tls.Ed25519
	decs["InsecureCipherSuites"] = tls.InsecureCipherSuites
	decs["Listen"] = tls.Listen
	decs["LoadX509KeyPair"] = tls.LoadX509KeyPair
	decs["NewLRUClientSessionCache"] = tls.NewLRUClientSessionCache
	decs["NewListener"] = tls.NewListener
	decs["NewResumptionState"] = tls.NewResumptionState
	decs["NoClientCert"] = tls.NoClientCert
	decs["PKCS1WithSHA1"] = tls.PKCS1WithSHA1
	decs["PKCS1WithSHA256"] = tls.PKCS1WithSHA256
	decs["PKCS1WithSHA384"] = tls.PKCS1WithSHA384
	decs["PKCS1WithSHA512"] = tls.PKCS1WithSHA512
	decs["PSSWithSHA256"] = tls.PSSWithSHA256
	decs["PSSWithSHA384"] = tls.PSSWithSHA384
	decs["PSSWithSHA512"] = tls.PSSWithSHA512
	decs["ParseSessionState"] = tls.ParseSessionState
	decs["QUICClient"] = tls.QUICClient
	decs["QUICConfig"] = reflect.TypeFor[tls.QUICConfig]()
	decs["QUICConn"] = reflect.TypeFor[tls.QUICConn]()
	decs["QUICEncryptionLevel"] = reflect.TypeFor[tls.QUICEncryptionLevel]()
	decs["QUICEncryptionLevelApplication"] = tls.QUICEncryptionLevelApplication
	decs["QUICEncryptionLevelEarly"] = tls.QUICEncryptionLevelEarly
	decs["QUICEncryptionLevelHandshake"] = tls.QUICEncryptionLevelHandshake
	decs["QUICEncryptionLevelInitial"] = tls.QUICEncryptionLevelInitial
	decs["QUICEvent"] = reflect.TypeFor[tls.QUICEvent]()
	decs["QUICEventKind"] = reflect.TypeFor[tls.QUICEventKind]()
	decs["QUICHandshakeDone"] = tls.QUICHandshakeDone
	decs["QUICNoEvent"] = tls.QUICNoEvent
	decs["QUICRejectedEarlyData"] = tls.QUICRejectedEarlyData
	decs["QUICServer"] = tls.QUICServer
	decs["QUICSessionTicketOptions"] = reflect.TypeFor[tls.QUICSessionTicketOptions]()
	decs["QUICSetReadSecret"] = tls.QUICSetReadSecret
	decs["QUICSetWriteSecret"] = tls.QUICSetWriteSecret
	decs["QUICTransportParameters"] = tls.QUICTransportParameters
	decs["QUICTransportParametersRequired"] = tls.QUICTransportParametersRequired
	decs["QUICWriteData"] = tls.QUICWriteData
	decs["RecordHeaderError"] = reflect.TypeFor[tls.RecordHeaderError]()
	decs["RenegotiateFreelyAsClient"] = tls.RenegotiateFreelyAsClient
	decs["RenegotiateNever"] = tls.RenegotiateNever
	decs["RenegotiateOnceAsClient"] = tls.RenegotiateOnceAsClient
	decs["RenegotiationSupport"] = reflect.TypeFor[tls.RenegotiationSupport]()
	decs["RequestClientCert"] = tls.RequestClientCert
	decs["RequireAndVerifyClientCert"] = tls.RequireAndVerifyClientCert
	decs["RequireAnyClientCert"] = tls.RequireAnyClientCert
	decs["Server"] = tls.Server
	decs["SessionState"] = reflect.TypeFor[tls.SessionState]()
	decs["SignatureScheme"] = reflect.TypeFor[tls.SignatureScheme]()
	decs["TLS_AES_128_GCM_SHA256"] = tls.TLS_AES_128_GCM_SHA256
	decs["TLS_AES_256_GCM_SHA384"] = tls.TLS_AES_256_GCM_SHA384
	decs["TLS_CHACHA20_POLY1305_SHA256"] = tls.TLS_CHACHA20_POLY1305_SHA256
	decs["TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA"] = tls.TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA
	decs["TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA256"] = tls.TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA256
	decs["TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256"] = tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256
	decs["TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA"] = tls.TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA
	decs["TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384"] = tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384
	decs["TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305"] = tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305
	decs["TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256"] = tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256
	decs["TLS_ECDHE_ECDSA_WITH_RC4_128_SHA"] = tls.TLS_ECDHE_ECDSA_WITH_RC4_128_SHA
	decs["TLS_ECDHE_RSA_WITH_3DES_EDE_CBC_SHA"] = tls.TLS_ECDHE_RSA_WITH_3DES_EDE_CBC_SHA
	decs["TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA"] = tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA
	decs["TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA256"] = tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA256
	decs["TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256"] = tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256
	decs["TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA"] = tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA
	decs["TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384"] = tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384
	decs["TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305"] = tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305
	decs["TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256"] = tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256
	decs["TLS_ECDHE_RSA_WITH_RC4_128_SHA"] = tls.TLS_ECDHE_RSA_WITH_RC4_128_SHA
	decs["TLS_FALLBACK_SCSV"] = tls.TLS_FALLBACK_SCSV
	decs["TLS_RSA_WITH_3DES_EDE_CBC_SHA"] = tls.TLS_RSA_WITH_3DES_EDE_CBC_SHA
	decs["TLS_RSA_WITH_AES_128_CBC_SHA"] = tls.TLS_RSA_WITH_AES_128_CBC_SHA
	decs["TLS_RSA_WITH_AES_128_CBC_SHA256"] = tls.TLS_RSA_WITH_AES_128_CBC_SHA256
	decs["TLS_RSA_WITH_AES_128_GCM_SHA256"] = tls.TLS_RSA_WITH_AES_128_GCM_SHA256
	decs["TLS_RSA_WITH_AES_256_CBC_SHA"] = tls.TLS_RSA_WITH_AES_256_CBC_SHA
	decs["TLS_RSA_WITH_AES_256_GCM_SHA384"] = tls.TLS_RSA_WITH_AES_256_GCM_SHA384
	decs["TLS_RSA_WITH_RC4_128_SHA"] = tls.TLS_RSA_WITH_RC4_128_SHA
	decs["VerifyClientCertIfGiven"] = tls.VerifyClientCertIfGiven
	decs["VersionName"] = tls.VersionName
	decs["VersionSSL30"] = native.UntypedNumericConst("768")
	decs["VersionTLS10"] = native.UntypedNumericConst("769")
	decs["VersionTLS11"] = native.UntypedNumericConst("770")
	decs["VersionTLS12"] = native.UntypedNumericConst("771")
	decs["VersionTLS13"] = native.UntypedNumericConst("772")
	decs["X25519"] = tls.X25519
	decs["X509KeyPair"] = tls.X509KeyPair
	packages["crypto/tls"] = native.Package{
		Name:         "tls",
		Declarations: decs,
	}
	// "crypto/x509"
	decs = make(native.Declarations, 106)
	decs["CANotAuthorizedForExtKeyUsage"] = x509.CANotAuthorizedForExtKeyUsage
	decs["CANotAuthorizedForThisName"] = x509.CANotAuthorizedForThisName
	decs["CertPool"] = reflect.TypeFor[x509.CertPool]()
	decs["Certificate"] = reflect.TypeFor[x509.Certificate]()
	decs["CertificateInvalidError"] = reflect.TypeFor[x509.CertificateInvalidError]()
	decs["CertificateRequest"] = reflect.TypeFor[x509.CertificateRequest]()
	decs["ConstraintViolationError"] = reflect.TypeFor[x509.ConstraintViolationError]()
	decs["CreateCertificate"] = x509.CreateCertificate
	decs["CreateCertificateRequest"] = x509.CreateCertificateRequest
	decs["CreateRevocationList"] = x509.CreateRevocationList
	decs["DSA"] = x509.DSA
	decs["DSAWithSHA1"] = x509.DSAWithSHA1
	decs["DSAWithSHA256"] = x509.DSAWithSHA256
	decs["DecryptPEMBlock"] = x509.DecryptPEMBlock
	decs["ECDSA"] = x509.ECDSA
	decs["ECDSAWithSHA1"] = x509.ECDSAWithSHA1
	decs["ECDSAWithSHA256"] = x509.ECDSAWithSHA256
	decs["ECDSAWithSHA384"] = x509.ECDSAWithSHA384
	decs["ECDSAWithSHA512"] = x509.ECDSAWithSHA512
	decs["Ed25519"] = x509.Ed25519
	decs["EncryptPEMBlock"] = x509.EncryptPEMBlock
	decs["ErrUnsupportedAlgorithm"] = &x509.ErrUnsupportedAlgorithm
	decs["Expired"] = x509.Expired
	decs["ExtKeyUsage"] = reflect.TypeFor[x509.ExtKeyUsage]()
	decs["ExtKeyUsageAny"] = x509.ExtKeyUsageAny
	decs["ExtKeyUsageClientAuth"] = x509.ExtKeyUsageClientAuth
	decs["ExtKeyUsageCodeSigning"] = x509.ExtKeyUsageCodeSigning
	decs["ExtKeyUsageEmailProtection"] = x509.ExtKeyUsageEmailProtection
	decs["ExtKeyUsageIPSECEndSystem"] = x509.ExtKeyUsageIPSECEndSystem
	decs["ExtKeyUsageIPSECTunnel"] = x509.ExtKeyUsageIPSECTunnel
	decs["ExtKeyUsageIPSECUser"] = x509.ExtKeyUsageIPSECUser
	decs["ExtKeyUsageMicrosoftCommercialCodeSigning"] = x509.ExtKeyUsageMicrosoftCommercialCodeSigning
	decs["ExtKeyUsageMicrosoftKernelCodeSigning"] = x509.ExtKeyUsageMicrosoftKernelCodeSigning
	decs["ExtKeyUsageMicrosoftServerGatedCrypto"] = x509.ExtKeyUsageMicrosoftServerGatedCrypto
	decs["ExtKeyUsageNetscapeServerGatedCrypto"] = x509.ExtKeyUsageNetscapeServerGatedCrypto
	decs["ExtKeyUsageOCSPSigning"] = x509.ExtKeyUsageOCSPSigning
	decs["ExtKeyUsageServerAuth"] = x509.ExtKeyUsageServerAuth
	decs["ExtKeyUsageTimeStamping"] = x509.ExtKeyUsageTimeStamping
	decs["HostnameError"] = reflect.TypeFor[x509.HostnameError]()
	decs["IncompatibleUsage"] = x509.IncompatibleUsage
	decs["IncorrectPasswordError"] = &x509.IncorrectPasswordError
	decs["InsecureAlgorithmError"] = reflect.TypeFor[x509.InsecureAlgorithmError]()
	decs["InvalidReason"] = reflect.TypeFor[x509.InvalidReason]()
	decs["IsEncryptedPEMBlock"] = x509.IsEncryptedPEMBlock
	decs["KeyUsage"] = reflect.TypeFor[x509.KeyUsage]()
	decs["KeyUsageCRLSign"] = x509.KeyUsageCRLSign
	decs["KeyUsageCertSign"] = x509.KeyUsageCertSign
	decs["KeyUsageContentCommitment"] = x509.KeyUsageContentCommitment
	decs["KeyUsageDataEncipherment"] = x509.KeyUsageDataEncipherment
	decs["KeyUsageDecipherOnly"] = x509.KeyUsageDecipherOnly
	decs["KeyUsageDigitalSignature"] = x509.KeyUsageDigitalSignature
	decs["KeyUsageEncipherOnly"] = x509.KeyUsageEncipherOnly
	decs["KeyUsageKeyAgreement"] = x509.KeyUsageKeyAgreement
	decs["KeyUsageKeyEncipherment"] = x509.KeyUsageKeyEncipherment
	decs["MD2WithRSA"] = x509.MD2WithRSA
	decs["MD5WithRSA"] = x509.MD5WithRSA
	decs["MarshalECPrivateKey"] = x509.MarshalECPrivateKey
	decs["MarshalPKCS1PrivateKey"] = x509.MarshalPKCS1PrivateKey
	decs["MarshalPKCS1PublicKey"] = x509.MarshalPKCS1PublicKey
	decs["MarshalPKCS8PrivateKey"] = x509.MarshalPKCS8PrivateKey
	decs["MarshalPKIXPublicKey"] = x509.MarshalPKIXPublicKey
	decs["NameConstraintsWithoutSANs"] = x509.NameConstraintsWithoutSANs
	decs["NameMismatch"] = x509.NameMismatch
	decs["NewCertPool"] = x509.NewCertPool
	decs["NotAuthorizedToSign"] = x509.NotAuthorizedToSign
	decs["PEMCipher"] = reflect.TypeFor[x509.PEMCipher]()
	decs["PEMCipher3DES"] = x509.PEMCipher3DES
	decs["PEMCipherAES128"] = x509.PEMCipherAES128
	decs["PEMCipherAES192"] = x509.PEMCipherAES192
	decs["PEMCipherAES256"] = x509.PEMCipherAES256
	decs["PEMCipherDES"] = x509.PEMCipherDES
	decs["ParseCRL"] = x509.ParseCRL
	decs["ParseCertificate"] = x509.ParseCertificate
	decs["ParseCertificateRequest"] = x509.ParseCertificateRequest
	decs["ParseCertificates"] = x509.ParseCertificates
	decs["ParseDERCRL"] = x509.ParseDERCRL
	decs["ParseECPrivateKey"] = x509.ParseECPrivateKey
	decs["ParsePKCS1PrivateKey"] = x509.ParsePKCS1PrivateKey
	decs["ParsePKCS1PublicKey"] = x509.ParsePKCS1PublicKey
	decs["ParsePKCS8PrivateKey"] = x509.ParsePKCS8PrivateKey
	decs["ParsePKIXPublicKey"] = x509.ParsePKIXPublicKey
	decs["ParseRevocationList"] = x509.ParseRevocationList
	decs["PublicKeyAlgorithm"] = reflect.TypeFor[x509.PublicKeyAlgorithm]()
	decs["PureEd25519"] = x509.PureEd25519
	decs["RSA"] = x509.RSA
	decs["RevocationList"] = reflect.TypeFor[x509.RevocationList]()
	decs["RevocationListEntry"] = reflect.TypeFor[x509.RevocationListEntry]()
	decs["SHA1WithRSA"] = x509.SHA1WithRSA
	decs["SHA256WithRSA"] = x509.SHA256WithRSA
	decs["SHA256WithRSAPSS"] = x509.SHA256WithRSAPSS
	decs["SHA384WithRSA"] = x509.SHA384WithRSA
	decs["SHA384WithRSAPSS"] = x509.SHA384WithRSAPSS
	decs["SHA512WithRSA"] = x509.SHA512WithRSA
	decs["SHA512WithRSAPSS"] = x509.SHA512WithRSAPSS
	decs["SetFallbackRoots"] = x509.SetFallbackRoots
	decs["SignatureAlgorithm"] = reflect.TypeFor[x509.SignatureAlgorithm]()
	decs["SystemCertPool"] = x509.SystemCertPool
	decs["SystemRootsError"] = reflect.TypeFor[x509.SystemRootsError]()
	decs["TooManyConstraints"] = x509.TooManyConstraints
	decs["TooManyIntermediates"] = x509.TooManyIntermediates
	decs["UnconstrainedName"] = x509.UnconstrainedName
	decs["UnhandledCriticalExtension"] = reflect.TypeFor[x509.UnhandledCriticalExtension]()
	decs["UnknownAuthorityError"] = reflect.TypeFor[x509.UnknownAuthorityError]()
	decs["UnknownPublicKeyAlgorithm"] = x509.UnknownPublicKeyAlgorithm
	decs["UnknownSignatureAlgorithm"] = x509.UnknownSignatureAlgorithm
	decs["VerifyOptions"] = reflect.TypeFor[x509.VerifyOptions]()
	packages["crypto/x509"] = native.Package{
		Name:         "x509",
		Declarations: decs,
	}
	// "crypto/x509/pkix"
	decs = make(native.Declarations, 10)
	decs["AlgorithmIdentifier"] = reflect.TypeFor[pkix.AlgorithmIdentifier]()
	decs["AttributeTypeAndValue"] = reflect.TypeFor[pkix.AttributeTypeAndValue]()
	decs["AttributeTypeAndValueSET"] = reflect.TypeFor[pkix.AttributeTypeAndValueSET]()
	decs["CertificateList"] = reflect.TypeFor[pkix.CertificateList]()
	decs["Extension"] = reflect.TypeFor[pkix.Extension]()
	decs["Name"] = reflect.TypeFor[pkix.Name]()
	decs["RDNSequence"] = reflect.TypeFor[pkix.RDNSequence]()
	decs["RelativeDistinguishedNameSET"] = reflect.TypeFor[pkix.RelativeDistinguishedNameSET]()
	decs["RevokedCertificate"] = reflect.TypeFor[pkix.RevokedCertificate]()
	decs["TBSCertificateList"] = reflect.TypeFor[pkix.TBSCertificateList]()
	packages["crypto/x509/pkix"] = native.Package{
		Name:         "pkix",
		Declarations: decs,
	}
	// "encoding"
	decs = make(native.Declarations, 4)
	decs["BinaryMarshaler"] = reflect.TypeFor[encoding.BinaryMarshaler]()
	decs["BinaryUnmarshaler"] = reflect.TypeFor[encoding.BinaryUnmarshaler]()
	decs["TextMarshaler"] = reflect.TypeFor[encoding.TextMarshaler]()
	decs["TextUnmarshaler"] = reflect.TypeFor[encoding.TextUnmarshaler]()
	packages["encoding"] = native.Package{
		Name:         "encoding",
		Declarations: decs,
	}
	// "encoding/ascii85"
	decs = make(native.Declarations, 6)
	decs["CorruptInputError"] = reflect.TypeFor[ascii85.CorruptInputError]()
	decs["Decode"] = ascii85.Decode
	decs["Encode"] = ascii85.Encode
	decs["MaxEncodedLen"] = ascii85.MaxEncodedLen
	decs["NewDecoder"] = ascii85.NewDecoder
	decs["NewEncoder"] = ascii85.NewEncoder
	packages["encoding/ascii85"] = native.Package{
		Name:         "ascii85",
		Declarations: decs,
	}
	// "encoding/asn1"
	decs = make(native.Declarations, 36)
	decs["BitString"] = reflect.TypeFor[asn1.BitString]()
	decs["ClassApplication"] = native.UntypedNumericConst("1")
	decs["ClassContextSpecific"] = native.UntypedNumericConst("2")
	decs["ClassPrivate"] = native.UntypedNumericConst("3")
	decs["ClassUniversal"] = native.UntypedNumericConst("0")
	decs["Enumerated"] = reflect.TypeFor[asn1.Enumerated]()
	decs["Flag"] = reflect.TypeFor[asn1.Flag]()
	decs["Marshal"] = asn1.Marshal
	decs["MarshalWithParams"] = asn1.MarshalWithParams
	decs["NullBytes"] = &asn1.NullBytes
	decs["NullRawValue"] = &asn1.NullRawValue
	decs["ObjectIdentifier"] = reflect.TypeFor[asn1.ObjectIdentifier]()
	decs["RawContent"] = reflect.TypeFor[asn1.RawContent]()
	decs["RawValue"] = reflect.TypeFor[asn1.RawValue]()
	decs["StructuralError"] = reflect.TypeFor[asn1.StructuralError]()
	decs["SyntaxError"] = reflect.TypeFor[asn1.SyntaxError]()
	decs["TagBMPString"] = native.UntypedNumericConst("30")
	decs["TagBitString"] = native.UntypedNumericConst("3")
	decs["TagBoolean"] = native.UntypedNumericConst("1")
	decs["TagEnum"] = native.UntypedNumericConst("10")
	decs["TagGeneralString"] = native.UntypedNumericConst("27")
	decs["TagGeneralizedTime"] = native.UntypedNumericConst("24")
	decs["TagIA5String"] = native.UntypedNumericConst("22")
	decs["TagInteger"] = native.UntypedNumericConst("2")
	decs["TagNull"] = native.UntypedNumericConst("5")
	decs["TagNumericString"] = native.UntypedNumericConst("18")
	decs["TagOID"] = native.UntypedNumericConst("6")
	decs["TagOctetString"] = native.UntypedNumericConst("4")
	decs["TagPrintableString"] = native.UntypedNumericConst("19")
	decs["TagSequence"] = native.UntypedNumericConst("16")
	decs["TagSet"] = native.UntypedNumericConst("17")
	decs["TagT61String"] = native.UntypedNumericConst("20")
	decs["TagUTCTime"] = native.UntypedNumericConst("23")
	decs["TagUTF8String"] = native.UntypedNumericConst("12")
	decs["Unmarshal"] = asn1.Unmarshal
	decs["UnmarshalWithParams"] = asn1.UnmarshalWithParams
	packages["encoding/asn1"] = native.Package{
		Name:         "asn1",
		Declarations: decs,
	}
	// "encoding/base32"
	decs = make(native.Declarations, 9)
	decs["CorruptInputError"] = reflect.TypeFor[base32.CorruptInputError]()
	decs["Encoding"] = reflect.TypeFor[base32.Encoding]()
	decs["HexEncoding"] = &base32.HexEncoding
	decs["NewDecoder"] = base32.NewDecoder
	decs["NewEncoder"] = base32.NewEncoder
	decs["NewEncoding"] = base32.NewEncoding
	decs["NoPadding"] = base32.NoPadding
	decs["StdEncoding"] = &base32.StdEncoding
	decs["StdPadding"] = base32.StdPadding
	packages["encoding/base32"] = native.Package{
		Name:         "base32",
		Declarations: decs,
	}
	// "encoding/base64"
	decs = make(native.Declarations, 11)
	decs["CorruptInputError"] = reflect.TypeFor[base64.CorruptInputError]()
	decs["Encoding"] = reflect.TypeFor[base64.Encoding]()
	decs["NewDecoder"] = base64.NewDecoder
	decs["NewEncoder"] = base64.NewEncoder
	decs["NewEncoding"] = base64.NewEncoding
	decs["NoPadding"] = base64.NoPadding
	decs["RawStdEncoding"] = &base64.RawStdEncoding
	decs["RawURLEncoding"] = &base64.RawURLEncoding
	decs["StdEncoding"] = &base64.StdEncoding
	decs["StdPadding"] = base64.StdPadding
	decs["URLEncoding"] = &base64.URLEncoding
	packages["encoding/base64"] = native.Package{
		Name:         "base64",
		Declarations: decs,
	}
	// "encoding/binary"
	decs = make(native.Declarations, 19)
	decs["AppendByteOrder"] = reflect.TypeFor[binary.AppendByteOrder]()
	decs["AppendUvarint"] = binary.AppendUvarint
	decs["AppendVarint"] = binary.AppendVarint
	decs["BigEndian"] = &binary.BigEndian
	decs["ByteOrder"] = reflect.TypeFor[binary.ByteOrder]()
	decs["LittleEndian"] = &binary.LittleEndian
	decs["MaxVarintLen16"] = native.UntypedNumericConst("3")
	decs["MaxVarintLen32"] = native.UntypedNumericConst("5")
	decs["MaxVarintLen64"] = native.UntypedNumericConst("10")
	decs["NativeEndian"] = &binary.NativeEndian
	decs["PutUvarint"] = binary.PutUvarint
	decs["PutVarint"] = binary.PutVarint
	decs["Read"] = binary.Read
	decs["ReadUvarint"] = binary.ReadUvarint
	decs["ReadVarint"] = binary.ReadVarint
	decs["Size"] = binary.Size
	decs["Uvarint"] = binary.Uvarint
	decs["Varint"] = binary.Varint
	decs["Write"] = binary.Write
	packages["encoding/binary"] = native.Package{
		Name:         "binary",
		Declarations: decs,
	}
	// "encoding/csv"
	decs = make(native.Declarations, 9)
	decs["ErrBareQuote"] = &csv.ErrBareQuote
	decs["ErrFieldCount"] = &csv.ErrFieldCount
	decs["ErrQuote"] = &csv.ErrQuote
	decs["ErrTrailingComma"] = &csv.ErrTrailingComma
	decs["NewReader"] = csv.NewReader
	decs["NewWriter"] = csv.NewWriter
	decs["ParseError"] = reflect.TypeFor[csv.ParseError]()
	decs["Reader"] = reflect.TypeFor[csv.Reader]()
	decs["Writer"] = reflect.TypeFor[csv.Writer]()
	packages["encoding/csv"] = native.Package{
		Name:         "csv",
		Declarations: decs,
	}
	// "encoding/gob"
	decs = make(native.Declarations, 9)
	decs["CommonType"] = reflect.TypeFor[gob.CommonType]()
	decs["Decoder"] = reflect.TypeFor[gob.Decoder]()
	decs["Encoder"] = reflect.TypeFor[gob.Encoder]()
	decs["GobDecoder"] = reflect.TypeFor[gob.GobDecoder]()
	decs["GobEncoder"] = reflect.TypeFor[gob.GobEncoder]()
	decs["NewDecoder"] = gob.NewDecoder
	decs["NewEncoder"] = gob.NewEncoder
	decs["Register"] = gob.Register
	decs["RegisterName"] = gob.RegisterName
	packages["encoding/gob"] = native.Package{
		Name:         "gob",
		Declarations: decs,
	}
	// "encoding/hex"
	decs = make(native.Declarations, 12)
	decs["Decode"] = hex.Decode
	decs["DecodeString"] = hex.DecodeString
	decs["DecodedLen"] = hex.DecodedLen
	decs["Dump"] = hex.Dump
	decs["Dumper"] = hex.Dumper
	decs["Encode"] = hex.Encode
	decs["EncodeToString"] = hex.EncodeToString
	decs["EncodedLen"] = hex.EncodedLen
	decs["ErrLength"] = &hex.ErrLength
	decs["InvalidByteError"] = reflect.TypeFor[hex.InvalidByteError]()
	decs["NewDecoder"] = hex.NewDecoder
	decs["NewEncoder"] = hex.NewEncoder
	packages["encoding/hex"] = native.Package{
		Name:         "hex",
		Declarations: decs,
	}
	// "encoding/json"
	decs = make(native.Declarations, 25)
	decs["Compact"] = json.Compact
	decs["Decoder"] = reflect.TypeFor[json.Decoder]()
	decs["Delim"] = reflect.TypeFor[json.Delim]()
	decs["Encoder"] = reflect.TypeFor[json.Encoder]()
	decs["HTMLEscape"] = json.HTMLEscape
	decs["Indent"] = json.Indent
	decs["InvalidUTF8Error"] = reflect.TypeFor[json.InvalidUTF8Error]()
	decs["InvalidUnmarshalError"] = reflect.TypeFor[json.InvalidUnmarshalError]()
	decs["Marshal"] = json.Marshal
	decs["MarshalIndent"] = json.MarshalIndent
	decs["Marshaler"] = reflect.TypeFor[json.Marshaler]()
	decs["MarshalerError"] = reflect.TypeFor[json.MarshalerError]()
	decs["NewDecoder"] = json.NewDecoder
	decs["NewEncoder"] = json.NewEncoder
	decs["Number"] = reflect.TypeFor[json.Number]()
	decs["RawMessage"] = reflect.TypeFor[json.RawMessage]()
	decs["SyntaxError"] = reflect.TypeFor[json.SyntaxError]()
	decs["Token"] = reflect.TypeFor[json.Token]()
	decs["Unmarshal"] = json.Unmarshal
	decs["UnmarshalFieldError"] = reflect.TypeFor[json.UnmarshalFieldError]()
	decs["UnmarshalTypeError"] = reflect.TypeFor[json.UnmarshalTypeError]()
	decs["Unmarshaler"] = reflect.TypeFor[json.Unmarshaler]()
	decs["UnsupportedTypeError"] = reflect.TypeFor[json.UnsupportedTypeError]()
	decs["UnsupportedValueError"] = reflect.TypeFor[json.UnsupportedValueError]()
	decs["Valid"] = json.Valid
	packages["encoding/json"] = native.Package{
		Name:         "json",
		Declarations: decs,
	}
	// "encoding/pem"
	decs = make(native.Declarations, 4)
	decs["Block"] = reflect.TypeFor[pem.Block]()
	decs["Decode"] = pem.Decode
	decs["Encode"] = pem.Encode
	decs["EncodeToMemory"] = pem.EncodeToMemory
	packages["encoding/pem"] = native.Package{
		Name:         "pem",
		Declarations: decs,
	}
	// "encoding/xml"
	decs = make(native.Declarations, 32)
	decs["Attr"] = reflect.TypeFor[xml.Attr]()
	decs["CharData"] = reflect.TypeFor[xml.CharData]()
	decs["Comment"] = reflect.TypeFor[xml.Comment]()
	decs["CopyToken"] = xml.CopyToken
	decs["Decoder"] = reflect.TypeFor[xml.Decoder]()
	decs["Directive"] = reflect.TypeFor[xml.Directive]()
	decs["Encoder"] = reflect.TypeFor[xml.Encoder]()
	decs["EndElement"] = reflect.TypeFor[xml.EndElement]()
	decs["Escape"] = xml.Escape
	decs["EscapeText"] = xml.EscapeText
	decs["HTMLAutoClose"] = &xml.HTMLAutoClose
	decs["HTMLEntity"] = &xml.HTMLEntity
	decs["Header"] = native.UntypedStringConst("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
	decs["Marshal"] = xml.Marshal
	decs["MarshalIndent"] = xml.MarshalIndent
	decs["Marshaler"] = reflect.TypeFor[xml.Marshaler]()
	decs["MarshalerAttr"] = reflect.TypeFor[xml.MarshalerAttr]()
	decs["Name"] = reflect.TypeFor[xml.Name]()
	decs["NewDecoder"] = xml.NewDecoder
	decs["NewEncoder"] = xml.NewEncoder
	decs["NewTokenDecoder"] = xml.NewTokenDecoder
	decs["ProcInst"] = reflect.TypeFor[xml.ProcInst]()
	decs["StartElement"] = reflect.TypeFor[xml.StartElement]()
	decs["SyntaxError"] = reflect.TypeFor[xml.SyntaxError]()
	decs["TagPathError"] = reflect.TypeFor[xml.TagPathError]()
	decs["Token"] = reflect.TypeFor[xml.Token]()
	decs["TokenReader"] = reflect.TypeFor[xml.TokenReader]()
	decs["Unmarshal"] = xml.Unmarshal
	decs["UnmarshalError"] = reflect.TypeFor[xml.UnmarshalError]()
	decs["Unmarshaler"] = reflect.TypeFor[xml.Unmarshaler]()
	decs["UnmarshalerAttr"] = reflect.TypeFor[xml.UnmarshalerAttr]()
	decs["UnsupportedTypeError"] = reflect.TypeFor[xml.UnsupportedTypeError]()
	packages["encoding/xml"] = native.Package{
		Name:         "xml",
		Declarations: decs,
	}
	// "errors"
	decs = make(native.Declarations, 6)
	decs["As"] = errors.As
	decs["ErrUnsupported"] = &errors.ErrUnsupported
	decs["Is"] = errors.Is
	decs["Join"] = errors.Join
	decs["New"] = errors.New
	decs["Unwrap"] = errors.Unwrap
	packages["errors"] = native.Package{
		Name:         "errors",
		Declarations: decs,
	}
	// "expvar"
	decs = make(native.Declarations, 15)
	decs["Do"] = expvar.Do
	decs["Float"] = reflect.TypeFor[expvar.Float]()
	decs["Func"] = reflect.TypeFor[expvar.Func]()
	decs["Get"] = expvar.Get
	decs["Handler"] = expvar.Handler
	decs["Int"] = reflect.TypeFor[expvar.Int]()
	decs["KeyValue"] = reflect.TypeFor[expvar.KeyValue]()
	decs["Map"] = reflect.TypeFor[expvar.Map]()
	decs["NewFloat"] = expvar.NewFloat
	decs["NewInt"] = expvar.NewInt
	decs["NewMap"] = expvar.NewMap
	decs["NewString"] = expvar.NewString
	decs["Publish"] = expvar.Publish
	decs["String"] = reflect.TypeFor[expvar.String]()
	decs["Var"] = reflect.TypeFor[expvar.Var]()
	packages["expvar"] = native.Package{
		Name:         "expvar",
		Declarations: decs,
	}
	// "flag"
	decs = make(native.Declarations, 44)
	decs["Arg"] = flag.Arg
	decs["Args"] = flag.Args
	decs["Bool"] = flag.Bool
	decs["BoolFunc"] = flag.BoolFunc
	decs["BoolVar"] = flag.BoolVar
	decs["CommandLine"] = &flag.CommandLine
	decs["ContinueOnError"] = flag.ContinueOnError
	decs["Duration"] = flag.Duration
	decs["DurationVar"] = flag.DurationVar
	decs["ErrHelp"] = &flag.ErrHelp
	decs["ErrorHandling"] = reflect.TypeFor[flag.ErrorHandling]()
	decs["ExitOnError"] = flag.ExitOnError
	decs["Flag"] = reflect.TypeFor[flag.Flag]()
	decs["FlagSet"] = reflect.TypeFor[flag.FlagSet]()
	decs["Float64"] = flag.Float64
	decs["Float64Var"] = flag.Float64Var
	decs["Func"] = flag.Func
	decs["Getter"] = reflect.TypeFor[flag.Getter]()
	decs["Int"] = flag.Int
	decs["Int64"] = flag.Int64
	decs["Int64Var"] = flag.Int64Var
	decs["IntVar"] = flag.IntVar
	decs["Lookup"] = flag.Lookup
	decs["NArg"] = flag.NArg
	decs["NFlag"] = flag.NFlag
	decs["NewFlagSet"] = flag.NewFlagSet
	decs["PanicOnError"] = flag.PanicOnError
	decs["Parse"] = flag.Parse
	decs["Parsed"] = flag.Parsed
	decs["PrintDefaults"] = flag.PrintDefaults
	decs["Set"] = flag.Set
	decs["String"] = flag.String
	decs["StringVar"] = flag.StringVar
	decs["TextVar"] = flag.TextVar
	decs["Uint"] = flag.Uint
	decs["Uint64"] = flag.Uint64
	decs["Uint64Var"] = flag.Uint64Var
	decs["UintVar"] = flag.UintVar
	decs["UnquoteUsage"] = flag.UnquoteUsage
	decs["Usage"] = &flag.Usage
	decs["Value"] = reflect.TypeFor[flag.Value]()
	decs["Var"] = flag.Var
	decs["Visit"] = flag.Visit
	decs["VisitAll"] = flag.VisitAll
	packages["flag"] = native.Package{
		Name:         "flag",
		Declarations: decs,
	}
	// "fmt"
	decs = make(native.Declarations, 29)
	decs["Append"] = fmt.Append
	decs["Appendf"] = fmt.Appendf
	decs["Appendln"] = fmt.Appendln
	decs["Errorf"] = fmt.Errorf
	decs["FormatString"] = fmt.FormatString
	decs["Formatter"] = reflect.TypeFor[fmt.Formatter]()
	decs["Fprint"] = fmt.Fprint
	decs["Fprintf"] = fmt.Fprintf
	decs["Fprintln"] = fmt.Fprintln
	decs["Fscan"] = fmt.Fscan
	decs["Fscanf"] = fmt.Fscanf
	decs["Fscanln"] = fmt.Fscanln
	decs["GoStringer"] = reflect.TypeFor[fmt.GoStringer]()
	decs["Print"] = fmt.Print
	decs["Printf"] = fmt.Printf
	decs["Println"] = fmt.Println
	decs["Scan"] = fmt.Scan
	decs["ScanState"] = reflect.TypeFor[fmt.ScanState]()
	decs["Scanf"] = fmt.Scanf
	decs["Scanln"] = fmt.Scanln
	decs["Scanner"] = reflect.TypeFor[fmt.Scanner]()
	decs["Sprint"] = fmt.Sprint
	decs["Sprintf"] = fmt.Sprintf
	decs["Sprintln"] = fmt.Sprintln
	decs["Sscan"] = fmt.Sscan
	decs["Sscanf"] = fmt.Sscanf
	decs["Sscanln"] = fmt.Sscanln
	decs["State"] = reflect.TypeFor[fmt.State]()
	decs["Stringer"] = reflect.TypeFor[fmt.Stringer]()
	packages["fmt"] = native.Package{
		Name:         "fmt",
		Declarations: decs,
	}
	// "github.com/crewjam/rfc5424"
	decs = make(native.Declarations, 34)
	decs["Alert"] = rfc5424.Alert
	decs["Auth"] = rfc5424.Auth
	decs["Authpriv"] = rfc5424.Authpriv
	decs["Crit"] = rfc5424.Crit
	decs["Cron"] = rfc5424.Cron
	decs["Daemon"] = rfc5424.Daemon
	decs["Debug"] = rfc5424.Debug
	decs["Emergency"] = rfc5424.Emergency
	decs["ErrBadFormat"] = reflect.TypeFor[rfc5424.ErrBadFormat]()
	decs["ErrInvalidValue"] = reflect.TypeFor[rfc5424.ErrInvalidValue]()
	decs["Error"] = rfc5424.Error
	decs["Ftp"] = rfc5424.Ftp
	decs["Info"] = rfc5424.Info
	decs["Kern"] = rfc5424.Kern
	decs["Local0"] = rfc5424.Local0
	decs["Local1"] = rfc5424.Local1
	decs["Local2"] = rfc5424.Local2
	decs["Local3"] = rfc5424.Local3
	decs["Local4"] = rfc5424.Local4
	decs["Local5"] = rfc5424.Local5
	decs["Local6"] = rfc5424.Local6
	decs["Local7"] = rfc5424.Local7
	decs["Lpr"] = rfc5424.Lpr
	decs["Mail"] = rfc5424.Mail
	decs["Message"] = reflect.TypeFor[rfc5424.Message]()
	decs["News"] = rfc5424.News
	decs["Notice"] = rfc5424.Notice
	decs["Priority"] = reflect.TypeFor[rfc5424.Priority]()
	decs["SDParam"] = reflect.TypeFor[rfc5424.SDParam]()
	decs["StructuredData"] = reflect.TypeFor[rfc5424.StructuredData]()
	decs["Syslog"] = rfc5424.Syslog
	decs["User"] = rfc5424.User
	decs["Uucp"] = rfc5424.Uucp
	decs["Warning"] = rfc5424.Warning
	packages["github.com/crewjam/rfc5424"] = native.Package{
		Name:         "rfc5424",
		Declarations: decs,
	}
	// "github.com/gobwas/glob"
	decs = make(native.Declarations, 4)
	decs["Compile"] = glob.Compile
	decs["Glob"] = reflect.TypeFor[glob.Glob]()
	decs["MustCompile"] = glob.MustCompile
	decs["QuoteMeta"] = glob.QuoteMeta
	packages["github.com/gobwas/glob"] = native.Package{
		Name:         "glob",
		Declarations: decs,
	}
	// "github.com/google/uuid"
	decs = make(native.Declarations, 52)
	decs["ClockSequence"] = uuid.ClockSequence
	decs["DisableRandPool"] = uuid.DisableRandPool
	decs["Domain"] = reflect.TypeFor[uuid.Domain]()
	decs["EnableRandPool"] = uuid.EnableRandPool
	decs["FromBytes"] = uuid.FromBytes
	decs["Future"] = uuid.Future
	decs["GetTime"] = uuid.GetTime
	decs["Group"] = uuid.Group
	decs["Invalid"] = uuid.Invalid
	decs["IsInvalidLengthError"] = uuid.IsInvalidLengthError
	decs["Max"] = &uuid.Max
	decs["Microsoft"] = uuid.Microsoft
	decs["Must"] = uuid.Must
	decs["MustParse"] = uuid.MustParse
	decs["NameSpaceDNS"] = &uuid.NameSpaceDNS
	decs["NameSpaceOID"] = &uuid.NameSpaceOID
	decs["NameSpaceURL"] = &uuid.NameSpaceURL
	decs["NameSpaceX500"] = &uuid.NameSpaceX500
	decs["New"] = uuid.New
	decs["NewDCEGroup"] = uuid.NewDCEGroup
	decs["NewDCEPerson"] = uuid.NewDCEPerson
	decs["NewDCESecurity"] = uuid.NewDCESecurity
	decs["NewHash"] = uuid.NewHash
	decs["NewMD5"] = uuid.NewMD5
	decs["NewRandom"] = uuid.NewRandom
	decs["NewRandomFromReader"] = uuid.NewRandomFromReader
	decs["NewSHA1"] = uuid.NewSHA1
	decs["NewString"] = uuid.NewString
	decs["NewUUID"] = uuid.NewUUID
	decs["NewV6"] = uuid.NewV6
	decs["NewV7"] = uuid.NewV7
	decs["NewV7FromReader"] = uuid.NewV7FromReader
	decs["Nil"] = &uuid.Nil
	decs["NodeID"] = uuid.NodeID
	decs["NodeInterface"] = uuid.NodeInterface
	decs["NullUUID"] = reflect.TypeFor[uuid.NullUUID]()
	decs["Org"] = uuid.Org
	decs["Parse"] = uuid.Parse
	decs["ParseBytes"] = uuid.ParseBytes
	decs["Person"] = uuid.Person
	decs["RFC4122"] = uuid.RFC4122
	decs["Reserved"] = uuid.Reserved
	decs["SetClockSequence"] = uuid.SetClockSequence
	decs["SetNodeID"] = uuid.SetNodeID
	decs["SetNodeInterface"] = uuid.SetNodeInterface
	decs["SetRand"] = uuid.SetRand
	decs["Time"] = reflect.TypeFor[uuid.Time]()
	decs["UUID"] = reflect.TypeFor[uuid.UUID]()
	decs["UUIDs"] = reflect.TypeFor[uuid.UUIDs]()
	decs["Validate"] = uuid.Validate
	decs["Variant"] = reflect.TypeFor[uuid.Variant]()
	decs["Version"] = reflect.TypeFor[uuid.Version]()
	packages["github.com/google/uuid"] = native.Package{
		Name:         "uuid",
		Declarations: decs,
	}
	// "github.com/gravwell/gravwell/v4/ingest"
	decs = make(native.Declarations, 175)
	decs["ABSOLUTE_MAX_UNCONFIRMED_WRITES"] = ingest.ABSOLUTE_MAX_UNCONFIRMED_WRITES
	decs["ACK_SIZE"] = ingest.ACK_SIZE
	decs["ACK_WRITER_BUFFER_SIZE"] = ingest.ACK_WRITER_BUFFER_SIZE
	decs["ACK_WRITER_CAN_WRITE"] = ingest.ACK_WRITER_CAN_WRITE
	decs["API_VERSION_MAJOR"] = ingest.API_VERSION_MAJOR
	decs["API_VERSION_MINOR"] = ingest.API_VERSION_MINOR
	decs["API_VER_MAGIC"] = ingest.API_VER_MAGIC
	decs["AuthHash"] = reflect.TypeFor[ingest.AuthHash]()
	decs["BUFFERED_ACK_READER_SIZE"] = ingest.BUFFERED_ACK_READER_SIZE
	decs["CHANNEL_BUFFER"] = ingest.CHANNEL_BUFFER
	decs["CLOSING_SERVICE_ACK_TIMEOUT"] = ingest.CLOSING_SERVICE_ACK_TIMEOUT
	decs["CONFIRM_API_VER_MAGIC"] = ingest.CONFIRM_API_VER_MAGIC
	decs["CONFIRM_ENTRY_MAGIC"] = ingest.CONFIRM_ENTRY_MAGIC
	decs["CONFIRM_ID_MAGIC"] = ingest.CONFIRM_ID_MAGIC
	decs["CONFIRM_INGESTER_STATE_MAGIC"] = ingest.CONFIRM_INGESTER_STATE_MAGIC
	decs["CONFIRM_INGEST_OK_MAGIC"] = ingest.CONFIRM_INGEST_OK_MAGIC
	decs["CONFIRM_TAG_MAGIC"] = ingest.CONFIRM_TAG_MAGIC
	decs["CacheModeAlways"] = native.UntypedStringConst("always")
	decs["CacheModeFail"] = native.UntypedStringConst("fail")
	decs["Challenge"] = reflect.TypeFor[ingest.Challenge]()
	decs["ChallengeResponse"] = reflect.TypeFor[ingest.ChallengeResponse]()
	decs["CheckTag"] = ingest.CheckTag
	decs["CircularIndex"] = reflect.TypeFor[ingest.CircularIndex]()
	decs["CompressNone"] = ingest.CompressNone
	decs["CompressSnappy"] = ingest.CompressSnappy
	decs["CompressionType"] = reflect.TypeFor[ingest.CompressionType]()
	decs["ConnectionType"] = ingest.ConnectionType
	decs["DEFAULT_CLEAR_PORT"] = ingest.DEFAULT_CLEAR_PORT
	decs["DEFAULT_PIPE_PATH"] = ingest.DEFAULT_PIPE_PATH
	decs["DEFAULT_TLS_PORT"] = ingest.DEFAULT_TLS_PORT
	decs["DIAL_TIMEOUT"] = ingest.DIAL_TIMEOUT
	decs["ERROR_TAG_MAGIC"] = ingest.ERROR_TAG_MAGIC
	decs["EnableKeepAlive"] = ingest.EnableKeepAlive
	decs["EntryBuffer"] = reflect.TypeFor[ingest.EntryBuffer]()
	decs["EntryReader"] = reflect.TypeFor[ingest.EntryReader]()
	decs["EntryReaderWriterConfig"] = reflect.TypeFor[ingest.EntryReaderWriterConfig]()
	decs["EntryWriter"] = reflect.TypeFor[ingest.EntryWriter]()
	decs["ErrAllConnsDown"] = &ingest.ErrAllConnsDown
	decs["ErrBadTagCharacter"] = &ingest.ErrBadTagCharacter
	decs["ErrConnectionTimeout"] = &ingest.ErrConnectionTimeout
	decs["ErrEmergencyListOverflow"] = &ingest.ErrEmergencyListOverflow
	decs["ErrEmptyAuth"] = &ingest.ErrEmptyAuth
	decs["ErrEmptyConfigBlock"] = &ingest.ErrEmptyConfigBlock
	decs["ErrEmptyTag"] = &ingest.ErrEmptyTag
	decs["ErrFailedAuth"] = &ingest.ErrFailedAuth
	decs["ErrFailedAuthHashGen"] = &ingest.ErrFailedAuthHashGen
	decs["ErrFailedParseLocalIP"] = &ingest.ErrFailedParseLocalIP
	decs["ErrFailedTagNegotiation"] = &ingest.ErrFailedTagNegotiation
	decs["ErrForbiddenTag"] = &ingest.ErrForbiddenTag
	decs["ErrInvalidAuthVersion"] = &ingest.ErrInvalidAuthVersion
	decs["ErrInvalidBuffer"] = &ingest.ErrInvalidBuffer
	decs["ErrInvalidCerts"] = &ingest.ErrInvalidCerts
	decs["ErrInvalidConnectionType"] = &ingest.ErrInvalidConnectionType
	decs["ErrInvalidCount"] = &ingest.ErrInvalidCount
	decs["ErrInvalidDest"] = &ingest.ErrInvalidDest
	decs["ErrInvalidEntry"] = &ingest.ErrInvalidEntry
	decs["ErrInvalidIngestStateHeader"] = &ingest.ErrInvalidIngestStateHeader
	decs["ErrInvalidRemoteKeySize"] = &ingest.ErrInvalidRemoteKeySize
	decs["ErrInvalidSecret"] = &ingest.ErrInvalidSecret
	decs["ErrInvalidStateResponseLen"] = &ingest.ErrInvalidStateResponseLen
	decs["ErrInvalidTagRequestLen"] = &ingest.ErrInvalidTagRequestLen
	decs["ErrInvalidTagResponseLen"] = &ingest.ErrInvalidTagResponseLen
	decs["ErrInvalidTenantName"] = &ingest.ErrInvalidTenantName
	decs["ErrMalformedDestination"] = &ingest.ErrMalformedDestination
	decs["ErrNilChallengeResponse"] = &ingest.ErrNilChallengeResponse
	decs["ErrNoTargets"] = &ingest.ErrNoTargets
	decs["ErrNotReady"] = &ingest.ErrNotReady
	decs["ErrNotRunning"] = &ingest.ErrNotRunning
	decs["ErrOversizedConfigBlock"] = &ingest.ErrOversizedConfigBlock
	decs["ErrOversizedEntry"] = &ingest.ErrOversizedEntry
	decs["ErrOversizedTag"] = &ingest.ErrOversizedTag
	decs["ErrShortRead"] = &ingest.ErrShortRead
	decs["ErrShortWrite"] = &ingest.ErrShortWrite
	decs["ErrSyncTimeout"] = &ingest.ErrSyncTimeout
	decs["ErrTagMapInvalid"] = &ingest.ErrTagMapInvalid
	decs["ErrTagNotFound"] = &ingest.ErrTagNotFound
	decs["ErrTenantAuthUnsupported"] = &ingest.ErrTenantAuthUnsupported
	decs["ErrTimeout"] = &ingest.ErrTimeout
	decs["ErrUnderFill"] = &ingest.ErrUnderFill
	decs["ErrWriteTimeout"] = &ingest.ErrWriteTimeout
	decs["FORBIDDEN_TAG_SET"] = ingest.FORBIDDEN_TAG_SET
	decs["FORCE_ACK_MAGIC"] = ingest.FORCE_ACK_MAGIC
	decs["G"] = native.UntypedNumericConst("1000000000.0")
	decs["GB"] = ingest.GB
	decs["GenAuthHash"] = ingest.GenAuthHash
	decs["GenerateResponse"] = ingest.GenerateResponse
	decs["HASH_ITERATIONS"] = ingest.HASH_ITERATIONS
	decs["HumanCount"] = ingest.HumanCount
	decs["HumanEntryRate"] = ingest.HumanEntryRate
	decs["HumanLineRate"] = ingest.HumanLineRate
	decs["HumanRate"] = ingest.HumanRate
	decs["HumanSize"] = ingest.HumanSize
	decs["ID_MAGIC"] = ingest.ID_MAGIC
	decs["INGESTER_STATE_MAGIC"] = ingest.INGESTER_STATE_MAGIC
	decs["INGEST_OK_MAGIC"] = ingest.INGEST_OK_MAGIC
	decs["INVALID_MAGIC"] = ingest.INVALID_MAGIC
	decs["IngestCommand"] = reflect.TypeFor[ingest.IngestCommand]()
	decs["IngestConnection"] = reflect.TypeFor[ingest.IngestConnection]()
	decs["IngestLogger"] = reflect.TypeFor[ingestLog.IngestLogger]()
	decs["IngestMuxer"] = reflect.TypeFor[ingest.IngestMuxer]()
	decs["IngesterState"] = reflect.TypeFor[ingest.IngesterState]()
	decs["IngesterStateCallback"] = reflect.TypeFor[ingest.IngesterStateCallback]()
	decs["InitializeConnection"] = ingest.InitializeConnection
	decs["K"] = native.UntypedNumericConst("1000.0")
	decs["KB"] = ingest.KB
	decs["LockedSource"] = reflect.TypeFor[ingest.LockedSource]()
	decs["Logger"] = reflect.TypeFor[ingestLog.Logger]()
	decs["M"] = native.UntypedNumericConst("1000000.0")
	decs["MAX_ENTRY_SIZE"] = ingest.MAX_ENTRY_SIZE
	decs["MAX_TAG_LENGTH"] = ingest.MAX_TAG_LENGTH
	decs["MAX_UNCONFIRMED_COUNT"] = ingest.MAX_UNCONFIRMED_COUNT
	decs["MAX_WRITE_ERROR"] = ingest.MAX_WRITE_ERROR
	decs["MB"] = ingest.MB
	decs["MINIMUM_DYN_CONFIG_VERSION"] = ingest.MINIMUM_DYN_CONFIG_VERSION
	decs["MINIMUM_ID_VERSION"] = ingest.MINIMUM_ID_VERSION
	decs["MINIMUM_INGEST_EV_VERSION"] = ingest.MINIMUM_INGEST_EV_VERSION
	decs["MINIMUM_INGEST_OK_VERSION"] = ingest.MINIMUM_INGEST_OK_VERSION
	decs["MINIMUM_INGEST_STATE_VERSION"] = ingest.MINIMUM_INGEST_STATE_VERSION
	decs["MINIMUM_TAG_RENEGOTIATE_VERSION"] = ingest.MINIMUM_TAG_RENEGOTIATE_VERSION
	decs["MIN_REMOTE_KEYSIZE"] = ingest.MIN_REMOTE_KEYSIZE
	decs["MaxTenantNameLength"] = ingest.MaxTenantNameLength
	decs["MinTenantAuthVersion"] = ingest.MinTenantAuthVersion
	decs["MuxerConfig"] = reflect.TypeFor[ingest.MuxerConfig]()
	decs["NEW_ENTRY_MAGIC"] = ingest.NEW_ENTRY_MAGIC
	decs["NewChallenge"] = ingest.NewChallenge
	decs["NewCircularIndex"] = ingest.NewCircularIndex
	decs["NewEntryBuffer"] = ingest.NewEntryBuffer
	decs["NewEntryReader"] = ingest.NewEntryReader
	decs["NewEntryReaderEx"] = ingest.NewEntryReaderEx
	decs["NewEntryWriter"] = ingest.NewEntryWriter
	decs["NewEntryWriterEx"] = ingest.NewEntryWriterEx
	decs["NewIngestMuxer"] = ingest.NewIngestMuxer
	decs["NewIngestMuxerExt"] = ingest.NewIngestMuxerExt
	decs["NewInsecureRNG"] = ingest.NewInsecureRNG
	decs["NewLockedSource"] = ingest.NewLockedSource
	decs["NewMuxer"] = ingest.NewMuxer
	decs["NewPipeConnection"] = ingest.NewPipeConnection
	decs["NewRNG"] = ingest.NewRNG
	decs["NewTCPConnection"] = ingest.NewTCPConnection
	decs["NewTLSConnection"] = ingest.NewTLSConnection
	decs["NewUniformIngestMuxer"] = ingest.NewUniformIngestMuxer
	decs["NewUniformIngestMuxerExt"] = ingest.NewUniformIngestMuxerExt
	decs["NewUniformMuxer"] = ingest.NewUniformMuxer
	decs["NoLogger"] = ingestLog.NoLogger
	decs["NsPerSec"] = ingest.NsPerSec
	decs["P"] = native.UntypedNumericConst("1000000000000.0")
	decs["PB"] = ingest.PB
	decs["PING_MAGIC"] = ingest.PING_MAGIC
	decs["PONG_MAGIC"] = ingest.PONG_MAGIC
	decs["ParseCompression"] = ingest.ParseCompression
	decs["PrintVersion"] = ingest.PrintVersion
	decs["READ_BUFFER_SIZE"] = ingest.READ_BUFFER_SIZE
	decs["READ_ENTRY_HEADER_SIZE"] = ingest.READ_ENTRY_HEADER_SIZE
	decs["RemapTag"] = ingest.RemapTag
	decs["STATE_AUTHENTICATED"] = ingest.STATE_AUTHENTICATED
	decs["STATE_HOT"] = ingest.STATE_HOT
	decs["STATE_NOT_AUTHENTICATED"] = ingest.STATE_NOT_AUTHENTICATED
	decs["SecureSeed"] = ingest.SecureSeed
	decs["StateResponse"] = reflect.TypeFor[ingest.StateResponse]()
	decs["StreamConfiguration"] = reflect.TypeFor[ingest.StreamConfiguration]()
	decs["SystemTenant"] = ingest.SystemTenant
	decs["T"] = native.UntypedNumericConst("1000000000000.0")
	decs["TAG_MAGIC"] = ingest.TAG_MAGIC
	decs["TB"] = ingest.TB
	decs["THROTTLE_MAGIC"] = ingest.THROTTLE_MAGIC
	decs["TLSCerts"] = reflect.TypeFor[ingest.TLSCerts]()
	decs["TagManager"] = reflect.TypeFor[ingest.TagManager]()
	decs["TagRequest"] = reflect.TypeFor[ingest.TagRequest]()
	decs["TagResponse"] = reflect.TypeFor[ingest.TagResponse]()
	decs["Target"] = reflect.TypeFor[ingest.Target]()
	decs["TargetError"] = reflect.TypeFor[ingest.TargetError]()
	decs["UniformMuxerConfig"] = reflect.TypeFor[ingest.UniformMuxerConfig]()
	decs["VERSION"] = ingest.VERSION
	decs["VerifyResponse"] = ingest.VerifyResponse
	decs["WRITE_BUFFER_SIZE"] = ingest.WRITE_BUFFER_SIZE
	decs["Y"] = native.UntypedNumericConst("1000000000000000.0")
	decs["YB"] = ingest.YB
	packages["github.com/gravwell/gravwell/v4/ingest"] = native.Package{
		Name:         "ingest",
		Declarations: decs,
	}

	// "github.com/gravwell/gravwell/v4/ingest/config"
	decs = make(native.Declarations, 41)
	decs["AppendDefaultPort"] = config.AppendDefaultPort
	decs["CACHE_DEPTH_DEFAULT"] = native.UntypedNumericConst("128")
	decs["CACHE_MODE_DEFAULT"] = native.UntypedStringConst("always")
	decs["CACHE_SIZE_DEFAULT"] = native.UntypedNumericConst("1000")
	decs["CustomTimeFormat"] = reflect.TypeFor[config.CustomTimeFormat]()
	decs["DefaultCleartextPort"] = config.DefaultCleartextPort
	decs["DefaultTLSPort"] = config.DefaultTLSPort
	decs["ErrBadMap"] = &config.ErrBadMap
	decs["ErrBadValue"] = &config.ErrBadValue
	decs["ErrConfigFileTooLarge"] = &config.ErrConfigFileTooLarge
	decs["ErrConfigNotOpen"] = &config.ErrConfigNotOpen
	decs["ErrEmptyEnvFile"] = &config.ErrEmptyEnvFile
	decs["ErrFailedFileRead"] = &config.ErrFailedFileRead
	decs["ErrGlobalSectionNotFound"] = &config.ErrGlobalSectionNotFound
	decs["ErrInvalidArg"] = &config.ErrInvalidArg
	decs["ErrInvalidArgument"] = &config.ErrInvalidArgument
	decs["ErrInvalidConnectionTimeout"] = &config.ErrInvalidConnectionTimeout
	decs["ErrInvalidImportInterface"] = &config.ErrInvalidImportInterface
	decs["ErrInvalidImportParameter"] = &config.ErrInvalidImportParameter
	decs["ErrInvalidLineLocation"] = &config.ErrInvalidLineLocation
	decs["ErrInvalidLogLevel"] = &config.ErrInvalidLogLevel
	decs["ErrInvalidMapKeyType"] = &config.ErrInvalidMapKeyType
	decs["ErrInvalidMapValueType"] = &config.ErrInvalidMapValueType
	decs["ErrInvalidUpdateLineParameter"] = &config.ErrInvalidUpdateLineParameter
	decs["ErrIsNotDirectory"] = &config.ErrIsNotDirectory
	decs["ErrMissingIngestSecret"] = &config.ErrMissingIngestSecret
	decs["ErrNoConnections"] = &config.ErrNoConnections
	decs["ErrNotFound"] = &config.ErrNotFound
	decs["IngestConfig"] = reflect.TypeFor[config.IngestConfig]()
	decs["IngestStreamConfig"] = reflect.TypeFor[config.IngestStreamConfig]()
	decs["LoadConfigBytes"] = config.LoadConfigBytes
	decs["LoadConfigFile"] = config.LoadConfigFile
	decs["LoadConfigOverlays"] = config.LoadConfigOverlays
	decs["LoadEnvVar"] = config.LoadEnvVar
	decs["ParseBool"] = config.ParseBool
	decs["ParseInt64"] = config.ParseInt64
	decs["ParseRate"] = config.ParseRate
	decs["ParseSource"] = config.ParseSource
	decs["ParseUint64"] = config.ParseUint64
	decs["TimeFormat"] = reflect.TypeFor[config.TimeFormat]()
	decs["VariableConfig"] = reflect.TypeFor[config.VariableConfig]()
	packages["github.com/gravwell/gravwell/v4/ingest/config"] = native.Package{
		Name:         "config",
		Declarations: decs,
	}
	// "github.com/gravwell/gravwell/v4/ingest/entry"
	decs = make(native.Declarations, 89)
	decs["BoolEnumData"] = entry.BoolEnumData
	decs["ByteEnumData"] = entry.ByteEnumData
	decs["DecodeEVBlockHeader"] = entry.DecodeEVBlockHeader
	decs["DecodeHeader"] = entry.DecodeHeader
	decs["DecodeHeaderTagSec"] = entry.DecodeHeaderTagSec
	decs["DecodePartialHeader"] = entry.DecodePartialHeader
	decs["DefaultTagId"] = entry.DefaultTagId
	decs["DefaultTagName"] = &entry.DefaultTagName
	decs["DurationEnumData"] = entry.DurationEnumData
	decs["ENTRY_HEADER_SIZE"] = entry.ENTRY_HEADER_SIZE
	decs["EVBlock"] = reflect.TypeFor[entry.EVBlock]()
	decs["EVBlockHeader"] = reflect.TypeFor[entry.EVBlockHeader]()
	decs["EVBlockHeaderLen"] = native.UntypedNumericConst("8")
	decs["Entry"] = reflect.TypeFor[entry.Entry]()
	decs["EntryBlock"] = reflect.TypeFor[entry.EntryBlock]()
	decs["EntryBlockHeaderSize"] = native.UntypedNumericConst("16")
	decs["EntryKey"] = reflect.TypeFor[entry.EntryKey]()
	decs["EntrySize"] = entry.EntrySize
	decs["EntrySlice"] = reflect.TypeFor[entry.EntrySlice]()
	decs["EntryTag"] = reflect.TypeFor[entry.EntryTag]()
	decs["EnumeratedData"] = reflect.TypeFor[entry.EnumeratedData]()
	decs["EnumeratedValue"] = reflect.TypeFor[entry.EnumeratedValue]()
	decs["ErrBadKey"] = &entry.ErrBadKey
	decs["ErrBlockTooLarge"] = &entry.ErrBlockTooLarge
	decs["ErrCorruptedEnumeratedValue"] = &entry.ErrCorruptedEnumeratedValue
	decs["ErrDataSizeTooLarge"] = &entry.ErrDataSizeTooLarge
	decs["ErrEnumeratedValueBlockCorrupt"] = &entry.ErrEnumeratedValueBlockCorrupt
	decs["ErrEnumeratedValueBlockInvalidCount"] = &entry.ErrEnumeratedValueBlockInvalidCount
	decs["ErrEnumeratedValueBlockInvalidSize"] = &entry.ErrEnumeratedValueBlockInvalidSize
	decs["ErrFailedBodyRead"] = &entry.ErrFailedBodyRead
	decs["ErrFailedBodyWrite"] = &entry.ErrFailedBodyWrite
	decs["ErrFailedHeaderWrite"] = &entry.ErrFailedHeaderWrite
	decs["ErrInvalid"] = &entry.ErrInvalid
	decs["ErrInvalidBufferSize"] = &entry.ErrInvalidBufferSize
	decs["ErrInvalidDestBuff"] = &entry.ErrInvalidDestBuff
	decs["ErrInvalidEntryBlock"] = &entry.ErrInvalidEntryBlock
	decs["ErrInvalidEnumeratedData"] = &entry.ErrInvalidEnumeratedData
	decs["ErrInvalidHeader"] = &entry.ErrInvalidHeader
	decs["ErrInvalidKey"] = &entry.ErrInvalidKey
	decs["ErrInvalidName"] = &entry.ErrInvalidName
	decs["ErrInvalidSrcBuff"] = &entry.ErrInvalidSrcBuff
	decs["ErrKeyAlreadySet"] = &entry.ErrKeyAlreadySet
	decs["ErrNilEntry"] = &entry.ErrNilEntry
	decs["ErrPartialDecode"] = &entry.ErrPartialDecode
	decs["ErrSliceLenTooLarge"] = &entry.ErrSliceLenTooLarge
	decs["ErrSliceSizeTooLarge"] = &entry.ErrSliceSizeTooLarge
	decs["ErrTSDataSizeInvalid"] = &entry.ErrTSDataSizeInvalid
	decs["ErrTruncatedEnumeratedValue"] = &entry.ErrTruncatedEnumeratedValue
	decs["ErrUnknownType"] = &entry.ErrUnknownType
	decs["Float32EnumData"] = entry.Float32EnumData
	decs["Float64EnumData"] = entry.Float64EnumData
	decs["FromStandard"] = entry.FromStandard
	decs["GravwellTagId"] = entry.GravwellTagId
	decs["GravwellTagName"] = &entry.GravwellTagName
	decs["IPEnumData"] = entry.IPEnumData
	decs["IPV4_SRC_SIZE"] = entry.IPV4_SRC_SIZE
	decs["InferEnumeratedData"] = entry.InferEnumeratedData
	decs["Int16EnumData"] = entry.Int16EnumData
	decs["Int32EnumData"] = entry.Int32EnumData
	decs["Int64EnumData"] = entry.Int64EnumData
	decs["Int8EnumData"] = entry.Int8EnumData
	decs["IntEnumData"] = entry.IntEnumData
	decs["MACEnumData"] = entry.MACEnumData
	decs["MaxDataSize"] = entry.MaxDataSize
	decs["MaxEvBlockCount"] = native.UntypedNumericConst("65535")
	decs["MaxEvBlockSize"] = native.UntypedNumericConst("33554432")
	decs["MaxEvDataLength"] = native.UntypedNumericConst("64503")
	decs["MaxEvNameLength"] = native.UntypedNumericConst("1024")
	decs["MaxEvSize"] = native.UntypedNumericConst("65535")
	decs["MaxSliceCount"] = entry.MaxSliceCount
	decs["NewDeepCopyEntryBlock"] = entry.NewDeepCopyEntryBlock
	decs["NewEntryBlock"] = entry.NewEntryBlock
	decs["NewEntryBlockNP"] = entry.NewEntryBlockNP
	decs["NewEnumeratedData"] = entry.NewEnumeratedData
	decs["NewEnumeratedValue"] = entry.NewEnumeratedValue
	decs["Now"] = entry.Now
	decs["RawEnumeratedData"] = entry.RawEnumeratedData
	decs["SRC_SIZE"] = entry.SRC_SIZE
	decs["Since"] = entry.Since
	decs["SliceEnumData"] = entry.SliceEnumData
	decs["StringEnumData"] = entry.StringEnumData
	decs["StringEnumDataTail"] = entry.StringEnumDataTail
	decs["TSEnumData"] = entry.TSEnumData
	decs["TS_SIZE"] = entry.TS_SIZE
	decs["Timestamp"] = reflect.TypeFor[entry.Timestamp]()
	decs["Uint16EnumData"] = entry.Uint16EnumData
	decs["Uint32EnumData"] = entry.Uint32EnumData
	decs["Uint64EnumData"] = entry.Uint64EnumData
	decs["UintEnumData"] = entry.UintEnumData
	decs["UnixTime"] = entry.UnixTime
	packages["github.com/gravwell/gravwell/v4/ingest/entry"] = native.Package{
		Name:         "entry",
		Declarations: decs,
	}
	// "github.com/gravwell/ipfix"
	decs = make(native.Declarations, 61)
	decs["Boolean"] = ipfix.Boolean
	decs["DataRecord"] = reflect.TypeFor[ipfix.DataRecord]()
	decs["DateTimeMicroseconds"] = ipfix.DateTimeMicroseconds
	decs["DateTimeMilliseconds"] = ipfix.DateTimeMilliseconds
	decs["DateTimeNanoseconds"] = ipfix.DateTimeNanoseconds
	decs["DateTimeSeconds"] = ipfix.DateTimeSeconds
	decs["DictionaryEntry"] = reflect.TypeFor[ipfix.DictionaryEntry]()
	decs["ErrFieldOverflow"] = &ipfix.ErrFieldOverflow
	decs["ErrNilCallback"] = &ipfix.ErrNilCallback
	decs["ErrProtocol"] = &ipfix.ErrProtocol
	decs["ErrRead"] = &ipfix.ErrRead
	decs["ErrTooManyTemplates"] = &ipfix.ErrTooManyTemplates
	decs["ErrUnknownTemplate"] = &ipfix.ErrUnknownTemplate
	decs["ErrVersion"] = &ipfix.ErrVersion
	decs["FieldType"] = reflect.TypeFor[ipfix.FieldType]()
	decs["FieldTypes"] = &ipfix.FieldTypes
	decs["Filter"] = reflect.TypeFor[ipfix.Filter]()
	decs["Float32"] = ipfix.Float32
	decs["Float64"] = ipfix.Float64
	decs["HeaderFilter"] = reflect.TypeFor[ipfix.HeaderFilter]()
	decs["IPfixIDTypeLookup"] = ipfix.IPfixIDTypeLookup
	decs["Int16"] = ipfix.Int16
	decs["Int32"] = ipfix.Int32
	decs["Int64"] = ipfix.Int64
	decs["Int8"] = ipfix.Int8
	decs["InterpretedField"] = reflect.TypeFor[ipfix.InterpretedField]()
	decs["InterpretedTemplateFieldSpecifier"] = reflect.TypeFor[ipfix.InterpretedTemplateFieldSpecifier]()
	decs["Interpreter"] = reflect.TypeFor[ipfix.Interpreter]()
	decs["IpfixIDLookup"] = ipfix.IpfixIDLookup
	decs["IpfixNameLookup"] = ipfix.IpfixNameLookup
	decs["Ipv4Address"] = ipfix.Ipv4Address
	decs["Ipv6Address"] = ipfix.Ipv6Address
	decs["LookupAndIdentify"] = ipfix.LookupAndIdentify
	decs["MacAddress"] = ipfix.MacAddress
	decs["Message"] = reflect.TypeFor[ipfix.Message]()
	decs["MessageHeader"] = reflect.TypeFor[ipfix.MessageHeader]()
	decs["NetflowV9IDLookup"] = ipfix.NetflowV9IDLookup
	decs["NetflowV9IDTypeLookup"] = ipfix.NetflowV9IDTypeLookup
	decs["NetflowV9NameLookup"] = ipfix.NetflowV9NameLookup
	decs["NewInterpreter"] = ipfix.NewInterpreter
	decs["NewInterpreterVersion"] = ipfix.NewInterpreterVersion
	decs["NewSession"] = ipfix.NewSession
	decs["NewWalker"] = ipfix.NewWalker
	decs["OctetArray"] = ipfix.OctetArray
	decs["Option"] = reflect.TypeFor[ipfix.Option]()
	decs["Read"] = ipfix.Read
	decs["Record"] = reflect.TypeFor[ipfix.Record]()
	decs["RecordCallback"] = reflect.TypeFor[ipfix.RecordCallback]()
	decs["Session"] = reflect.TypeFor[ipfix.Session]()
	decs["String"] = ipfix.String
	decs["TemplateFieldSpecifier"] = reflect.TypeFor[ipfix.TemplateFieldSpecifier]()
	decs["TemplateRecord"] = reflect.TypeFor[ipfix.TemplateRecord]()
	decs["Uint16"] = ipfix.Uint16
	decs["Uint24"] = ipfix.Uint24
	decs["Uint32"] = ipfix.Uint32
	decs["Uint64"] = ipfix.Uint64
	decs["Uint8"] = ipfix.Uint8
	decs["Unknown"] = ipfix.Unknown
	decs["VarInt"] = ipfix.VarInt
	decs["Walker"] = reflect.TypeFor[ipfix.Walker]()
	decs["WithIDAliasing"] = ipfix.WithIDAliasing
	packages["github.com/gravwell/ipfix"] = native.Package{
		Name:         "ipfix",
		Declarations: decs,
	}
	// "github.com/gravwell/jsonparser"
	decs = make(native.Declarations, 51)
	decs["Array"] = jsonparser_2.Array
	decs["ArrayEach"] = jsonparser_2.ArrayEach
	decs["Boolean"] = jsonparser_2.Boolean
	decs["Delete"] = jsonparser_2.Delete
	decs["EachKey"] = jsonparser_2.EachKey
	decs["FuzzDelete"] = jsonparser_2.FuzzDelete
	decs["FuzzEachKey"] = jsonparser_2.FuzzEachKey
	decs["FuzzGetBoolean"] = jsonparser_2.FuzzGetBoolean
	decs["FuzzGetFloat"] = jsonparser_2.FuzzGetFloat
	decs["FuzzGetInt"] = jsonparser_2.FuzzGetInt
	decs["FuzzGetString"] = jsonparser_2.FuzzGetString
	decs["FuzzGetUnsafeString"] = jsonparser_2.FuzzGetUnsafeString
	decs["FuzzObjectEach"] = jsonparser_2.FuzzObjectEach
	decs["FuzzParseBool"] = jsonparser_2.FuzzParseBool
	decs["FuzzParseFloat"] = jsonparser_2.FuzzParseFloat
	decs["FuzzParseInt"] = jsonparser_2.FuzzParseInt
	decs["FuzzParseString"] = jsonparser_2.FuzzParseString
	decs["FuzzSet"] = jsonparser_2.FuzzSet
	decs["FuzzTokenStart"] = jsonparser_2.FuzzTokenStart
	decs["Get"] = jsonparser_2.Get
	decs["GetBoolean"] = jsonparser_2.GetBoolean
	decs["GetFloat"] = jsonparser_2.GetFloat
	decs["GetInt"] = jsonparser_2.GetInt
	decs["GetString"] = jsonparser_2.GetString
	decs["GetUnsafeString"] = jsonparser_2.GetUnsafeString
	decs["KeyPathNotFoundError"] = &jsonparser_2.KeyPathNotFoundError
	decs["MalformedArrayError"] = &jsonparser_2.MalformedArrayError
	decs["MalformedJsonError"] = &jsonparser_2.MalformedJsonError
	decs["MalformedObjectError"] = &jsonparser_2.MalformedObjectError
	decs["MalformedStringError"] = &jsonparser_2.MalformedStringError
	decs["MalformedStringEscapeError"] = &jsonparser_2.MalformedStringEscapeError
	decs["MalformedValueError"] = &jsonparser_2.MalformedValueError
	decs["NotExist"] = jsonparser_2.NotExist
	decs["Null"] = jsonparser_2.Null
	decs["NullValueError"] = &jsonparser_2.NullValueError
	decs["Number"] = jsonparser_2.Number
	decs["Object"] = jsonparser_2.Object
	decs["ObjectEach"] = jsonparser_2.ObjectEach
	decs["OverflowIntegerError"] = &jsonparser_2.OverflowIntegerError
	decs["ParseBoolean"] = jsonparser_2.ParseBoolean
	decs["ParseFloat"] = jsonparser_2.ParseFloat
	decs["ParseInt"] = jsonparser_2.ParseInt
	decs["ParseString"] = jsonparser_2.ParseString
	decs["Set"] = jsonparser_2.Set
	decs["String"] = jsonparser_2.String
	decs["StringToBytes"] = jsonparser_2.StringToBytes
	decs["Unescape"] = jsonparser_2.Unescape
	decs["Unknown"] = jsonparser_2.Unknown
	decs["UnknownValueTypeError"] = &jsonparser_2.UnknownValueTypeError
	decs["ValueType"] = reflect.TypeFor[jsonparser_2.ValueType]()
	decs["WriteToBuffer"] = jsonparser_2.WriteToBuffer
	packages["github.com/gravwell/jsonparser"] = native.Package{
		Name:         "jsonparser",
		Declarations: decs,
	}
	// "github.com/buger/jsonparser" just aliases to our internal one
	packages["github.com/buger/jsonparser"] = packages["github.com/gravwell/jsonparser"]

	// "github.com/h2non/filetype"
	decs = make(native.Declarations, 37)
	decs["AddMatcher"] = filetype.AddMatcher
	decs["AddType"] = filetype.AddType
	decs["Archive"] = filetype.Archive
	decs["Audio"] = filetype.Audio
	decs["Document"] = filetype.Document
	decs["ErrEmptyBuffer"] = &filetype.ErrEmptyBuffer
	decs["ErrUnknownBuffer"] = &filetype.ErrUnknownBuffer
	decs["Font"] = filetype.Font
	decs["Get"] = filetype.Get
	decs["GetType"] = filetype.GetType
	decs["Image"] = filetype.Image
	decs["Is"] = filetype.Is
	decs["IsArchive"] = filetype.IsArchive
	decs["IsAudio"] = filetype.IsAudio
	decs["IsDocument"] = filetype.IsDocument
	decs["IsExtension"] = filetype.IsExtension
	decs["IsFont"] = filetype.IsFont
	decs["IsImage"] = filetype.IsImage
	decs["IsMIME"] = filetype.IsMIME
	decs["IsMIMESupported"] = filetype.IsMIMESupported
	decs["IsSupported"] = filetype.IsSupported
	decs["IsType"] = filetype.IsType
	decs["IsVideo"] = filetype.IsVideo
	decs["Match"] = filetype.Match
	decs["MatchFile"] = filetype.MatchFile
	decs["MatchMap"] = filetype.MatchMap
	decs["MatchReader"] = filetype.MatchReader
	decs["MatcherKeys"] = &filetype.MatcherKeys
	decs["Matchers"] = &filetype.Matchers
	decs["Matches"] = filetype.Matches
	decs["MatchesMap"] = filetype.MatchesMap
	decs["NewMatcher"] = &filetype.NewMatcher
	decs["NewType"] = &filetype.NewType
	decs["Types"] = &filetype.Types
	decs["Unknown"] = &filetype.Unknown
	decs["Version"] = native.UntypedStringConst("1.0.10")
	decs["Video"] = filetype.Video
	packages["github.com/h2non/filetype"] = native.Package{
		Name:         "filetype",
		Declarations: decs,
	}
	// "github.com/klauspost/compress"
	decs = make(native.Declarations, 2)
	decs["Estimate"] = compress.Estimate
	decs["ShannonEntropyBits"] = compress.ShannonEntropyBits
	packages["github.com/klauspost/compress"] = native.Package{
		Name:         "compress",
		Declarations: decs,
	}
	// "github.com/miekg/dns"
	decs = make(native.Declarations, 468)
	decs["A"] = reflect.TypeFor[dns.A]()
	decs["AAAA"] = reflect.TypeFor[dns.AAAA]()
	decs["AFSDB"] = reflect.TypeFor[dns.AFSDB]()
	decs["AMTRELAY"] = reflect.TypeFor[dns.AMTRELAY]()
	decs["AMTRELAYHost"] = dns.AMTRELAYHost
	decs["AMTRELAYIPv4"] = dns.AMTRELAYIPv4
	decs["AMTRELAYIPv6"] = dns.AMTRELAYIPv6
	decs["AMTRELAYNone"] = dns.AMTRELAYNone
	decs["ANY"] = reflect.TypeFor[dns.ANY]()
	decs["APL"] = reflect.TypeFor[dns.APL]()
	decs["APLPrefix"] = reflect.TypeFor[dns.APLPrefix]()
	decs["AVC"] = reflect.TypeFor[dns.AVC]()
	decs["ActivateAndServe"] = dns.ActivateAndServe
	decs["AlgorithmToHash"] = &dns.AlgorithmToHash
	decs["AlgorithmToString"] = &dns.AlgorithmToString
	decs["CAA"] = reflect.TypeFor[dns.CAA]()
	decs["CDNSKEY"] = reflect.TypeFor[dns.CDNSKEY]()
	decs["CDS"] = reflect.TypeFor[dns.CDS]()
	decs["CERT"] = reflect.TypeFor[dns.CERT]()
	decs["CNAME"] = reflect.TypeFor[dns.CNAME]()
	decs["CSYNC"] = reflect.TypeFor[dns.CSYNC]()
	decs["CanonicalName"] = dns.CanonicalName
	decs["CertACPKIX"] = native.UntypedNumericConst("7")
	decs["CertIACPKIX"] = native.UntypedNumericConst("8")
	decs["CertIPGP"] = native.UntypedNumericConst("6")
	decs["CertIPIX"] = native.UntypedNumericConst("4")
	decs["CertISPKI"] = native.UntypedNumericConst("5")
	decs["CertOID"] = native.UntypedNumericConst("254")
	decs["CertPGP"] = native.UntypedNumericConst("3")
	decs["CertPKIX"] = native.UntypedNumericConst("1")
	decs["CertSPKI"] = native.UntypedNumericConst("2")
	decs["CertTypeToString"] = &dns.CertTypeToString
	decs["CertURI"] = native.UntypedNumericConst("253")
	decs["CertificateToDANE"] = dns.CertificateToDANE
	decs["Class"] = reflect.TypeFor[dns.Class]()
	decs["ClassANY"] = native.UntypedNumericConst("255")
	decs["ClassCHAOS"] = native.UntypedNumericConst("3")
	decs["ClassCSNET"] = native.UntypedNumericConst("2")
	decs["ClassHESIOD"] = native.UntypedNumericConst("4")
	decs["ClassINET"] = native.UntypedNumericConst("1")
	decs["ClassNONE"] = native.UntypedNumericConst("254")
	decs["ClassToString"] = &dns.ClassToString
	decs["Client"] = reflect.TypeFor[dns.Client]()
	decs["ClientConfig"] = reflect.TypeFor[dns.ClientConfig]()
	decs["ClientConfigFromFile"] = dns.ClientConfigFromFile
	decs["ClientConfigFromReader"] = dns.ClientConfigFromReader
	decs["CompareDomainName"] = dns.CompareDomainName
	decs["Conn"] = reflect.TypeFor[dns.Conn]()
	decs["ConnectionStater"] = reflect.TypeFor[dns.ConnectionStater]()
	decs["Copy"] = dns.Copy
	decs["CountLabel"] = dns.CountLabel
	decs["DH"] = dns.DH
	decs["DHCID"] = reflect.TypeFor[dns.DHCID]()
	decs["DLV"] = reflect.TypeFor[dns.DLV]()
	decs["DNAME"] = reflect.TypeFor[dns.DNAME]()
	decs["DNSKEY"] = reflect.TypeFor[dns.DNSKEY]()
	decs["DS"] = reflect.TypeFor[dns.DS]()
	decs["DSA"] = dns.DSA
	decs["DSANSEC3SHA1"] = dns.DSANSEC3SHA1
	decs["DecorateReader"] = reflect.TypeFor[dns.DecorateReader]()
	decs["DecorateWriter"] = reflect.TypeFor[dns.DecorateWriter]()
	decs["Dedup"] = dns.Dedup
	decs["DefaultMsgAcceptFunc"] = &dns.DefaultMsgAcceptFunc
	decs["DefaultMsgSize"] = native.UntypedNumericConst("4096")
	decs["DefaultServeMux"] = &dns.DefaultServeMux
	decs["Dial"] = dns.Dial
	decs["DialTimeout"] = dns.DialTimeout
	decs["DialTimeoutWithTLS"] = dns.DialTimeoutWithTLS
	decs["DialWithTLS"] = dns.DialWithTLS
	decs["ECCGOST"] = dns.ECCGOST
	decs["ECDSAP256SHA256"] = dns.ECDSAP256SHA256
	decs["ECDSAP384SHA384"] = dns.ECDSAP384SHA384
	decs["ED25519"] = dns.ED25519
	decs["ED448"] = dns.ED448
	decs["EDNS0"] = reflect.TypeFor[dns.EDNS0]()
	decs["EDNS0COOKIE"] = native.UntypedNumericConst("10")
	decs["EDNS0DAU"] = native.UntypedNumericConst("5")
	decs["EDNS0DHU"] = native.UntypedNumericConst("6")
	decs["EDNS0EDE"] = native.UntypedNumericConst("15")
	decs["EDNS0ESU"] = native.UntypedNumericConst("4")
	decs["EDNS0EXPIRE"] = native.UntypedNumericConst("9")
	decs["EDNS0LLQ"] = native.UntypedNumericConst("1")
	decs["EDNS0LOCALEND"] = native.UntypedNumericConst("65534")
	decs["EDNS0LOCALSTART"] = native.UntypedNumericConst("65001")
	decs["EDNS0N3U"] = native.UntypedNumericConst("7")
	decs["EDNS0NSID"] = native.UntypedNumericConst("3")
	decs["EDNS0PADDING"] = native.UntypedNumericConst("12")
	decs["EDNS0SUBNET"] = native.UntypedNumericConst("8")
	decs["EDNS0TCPKEEPALIVE"] = native.UntypedNumericConst("11")
	decs["EDNS0UL"] = native.UntypedNumericConst("2")
	decs["EDNS0_COOKIE"] = reflect.TypeFor[dns.EDNS0_COOKIE]()
	decs["EDNS0_DAU"] = reflect.TypeFor[dns.EDNS0_DAU]()
	decs["EDNS0_DHU"] = reflect.TypeFor[dns.EDNS0_DHU]()
	decs["EDNS0_EDE"] = reflect.TypeFor[dns.EDNS0_EDE]()
	decs["EDNS0_ESU"] = reflect.TypeFor[dns.EDNS0_ESU]()
	decs["EDNS0_EXPIRE"] = reflect.TypeFor[dns.EDNS0_EXPIRE]()
	decs["EDNS0_LLQ"] = reflect.TypeFor[dns.EDNS0_LLQ]()
	decs["EDNS0_LOCAL"] = reflect.TypeFor[dns.EDNS0_LOCAL]()
	decs["EDNS0_N3U"] = reflect.TypeFor[dns.EDNS0_N3U]()
	decs["EDNS0_NSID"] = reflect.TypeFor[dns.EDNS0_NSID]()
	decs["EDNS0_PADDING"] = reflect.TypeFor[dns.EDNS0_PADDING]()
	decs["EDNS0_SUBNET"] = reflect.TypeFor[dns.EDNS0_SUBNET]()
	decs["EDNS0_TCP_KEEPALIVE"] = reflect.TypeFor[dns.EDNS0_TCP_KEEPALIVE]()
	decs["EDNS0_UL"] = reflect.TypeFor[dns.EDNS0_UL]()
	decs["EID"] = reflect.TypeFor[dns.EID]()
	decs["EUI48"] = reflect.TypeFor[dns.EUI48]()
	decs["EUI64"] = reflect.TypeFor[dns.EUI64]()
	decs["Envelope"] = reflect.TypeFor[dns.Envelope]()
	decs["ErrAlg"] = &dns.ErrAlg
	decs["ErrAuth"] = &dns.ErrAuth
	decs["ErrBuf"] = &dns.ErrBuf
	decs["ErrConnEmpty"] = &dns.ErrConnEmpty
	decs["ErrExtendedRcode"] = &dns.ErrExtendedRcode
	decs["ErrFqdn"] = &dns.ErrFqdn
	decs["ErrId"] = &dns.ErrId
	decs["ErrKey"] = &dns.ErrKey
	decs["ErrKeyAlg"] = &dns.ErrKeyAlg
	decs["ErrKeySize"] = &dns.ErrKeySize
	decs["ErrLongDomain"] = &dns.ErrLongDomain
	decs["ErrNoSig"] = &dns.ErrNoSig
	decs["ErrPrivKey"] = &dns.ErrPrivKey
	decs["ErrRRset"] = &dns.ErrRRset
	decs["ErrRcode"] = &dns.ErrRcode
	decs["ErrRdata"] = &dns.ErrRdata
	decs["ErrSecret"] = &dns.ErrSecret
	decs["ErrShortRead"] = &dns.ErrShortRead
	decs["ErrSig"] = &dns.ErrSig
	decs["ErrSoa"] = &dns.ErrSoa
	decs["ErrTime"] = &dns.ErrTime
	decs["Error"] = reflect.TypeFor[dns.Error]()
	decs["Exchange"] = dns.Exchange
	decs["ExchangeConn"] = dns.ExchangeConn
	decs["ExchangeContext"] = dns.ExchangeContext
	decs["ExtendedErrorCodeBlocked"] = dns.ExtendedErrorCodeBlocked
	decs["ExtendedErrorCodeCachedError"] = dns.ExtendedErrorCodeCachedError
	decs["ExtendedErrorCodeCensored"] = dns.ExtendedErrorCodeCensored
	decs["ExtendedErrorCodeDNSBogus"] = dns.ExtendedErrorCodeDNSBogus
	decs["ExtendedErrorCodeDNSKEYMissing"] = dns.ExtendedErrorCodeDNSKEYMissing
	decs["ExtendedErrorCodeDNSSECIndeterminate"] = dns.ExtendedErrorCodeDNSSECIndeterminate
	decs["ExtendedErrorCodeFiltered"] = dns.ExtendedErrorCodeFiltered
	decs["ExtendedErrorCodeForgedAnswer"] = dns.ExtendedErrorCodeForgedAnswer
	decs["ExtendedErrorCodeInvalidData"] = dns.ExtendedErrorCodeInvalidData
	decs["ExtendedErrorCodeNSECMissing"] = dns.ExtendedErrorCodeNSECMissing
	decs["ExtendedErrorCodeNetworkError"] = dns.ExtendedErrorCodeNetworkError
	decs["ExtendedErrorCodeNoReachableAuthority"] = dns.ExtendedErrorCodeNoReachableAuthority
	decs["ExtendedErrorCodeNoZoneKeyBitSet"] = dns.ExtendedErrorCodeNoZoneKeyBitSet
	decs["ExtendedErrorCodeNotAuthoritative"] = dns.ExtendedErrorCodeNotAuthoritative
	decs["ExtendedErrorCodeNotReady"] = dns.ExtendedErrorCodeNotReady
	decs["ExtendedErrorCodeNotSupported"] = dns.ExtendedErrorCodeNotSupported
	decs["ExtendedErrorCodeOther"] = dns.ExtendedErrorCodeOther
	decs["ExtendedErrorCodeProhibited"] = dns.ExtendedErrorCodeProhibited
	decs["ExtendedErrorCodeRRSIGsMissing"] = dns.ExtendedErrorCodeRRSIGsMissing
	decs["ExtendedErrorCodeSignatureExpired"] = dns.ExtendedErrorCodeSignatureExpired
	decs["ExtendedErrorCodeSignatureNotYetValid"] = dns.ExtendedErrorCodeSignatureNotYetValid
	decs["ExtendedErrorCodeStaleAnswer"] = dns.ExtendedErrorCodeStaleAnswer
	decs["ExtendedErrorCodeStaleNXDOMAINAnswer"] = dns.ExtendedErrorCodeStaleNXDOMAINAnswer
	decs["ExtendedErrorCodeToString"] = &dns.ExtendedErrorCodeToString
	decs["ExtendedErrorCodeUnsupportedDNSKEYAlgorithm"] = dns.ExtendedErrorCodeUnsupportedDNSKEYAlgorithm
	decs["ExtendedErrorCodeUnsupportedDSDigestType"] = dns.ExtendedErrorCodeUnsupportedDSDigestType
	decs["Field"] = dns.Field
	decs["Fqdn"] = dns.Fqdn
	decs["GID"] = reflect.TypeFor[dns.GID]()
	decs["GOST94"] = dns.GOST94
	decs["GPOS"] = reflect.TypeFor[dns.GPOS]()
	decs["HINFO"] = reflect.TypeFor[dns.HINFO]()
	decs["HIP"] = reflect.TypeFor[dns.HIP]()
	decs["HTTPS"] = reflect.TypeFor[dns.HTTPS]()
	decs["Handle"] = dns.Handle
	decs["HandleFailed"] = dns.HandleFailed
	decs["HandleFunc"] = dns.HandleFunc
	decs["HandleRemove"] = dns.HandleRemove
	decs["Handler"] = reflect.TypeFor[dns.Handler]()
	decs["HandlerFunc"] = reflect.TypeFor[dns.HandlerFunc]()
	decs["HashName"] = dns.HashName
	decs["HashToString"] = &dns.HashToString
	decs["Header"] = reflect.TypeFor[dns.Header]()
	decs["HmacMD5"] = native.UntypedStringConst("hmac-md5.sig-alg.reg.int.")
	decs["HmacSHA1"] = native.UntypedStringConst("hmac-sha1.")
	decs["HmacSHA224"] = native.UntypedStringConst("hmac-sha224.")
	decs["HmacSHA256"] = native.UntypedStringConst("hmac-sha256.")
	decs["HmacSHA384"] = native.UntypedStringConst("hmac-sha384.")
	decs["HmacSHA512"] = native.UntypedStringConst("hmac-sha512.")
	decs["INDIRECT"] = dns.INDIRECT
	decs["IPSECGatewayHost"] = dns.IPSECGatewayHost
	decs["IPSECGatewayIPv4"] = dns.IPSECGatewayIPv4
	decs["IPSECGatewayIPv6"] = dns.IPSECGatewayIPv6
	decs["IPSECGatewayNone"] = dns.IPSECGatewayNone
	decs["IPSECKEY"] = reflect.TypeFor[dns.IPSECKEY]()
	decs["Id"] = &dns.Id
	decs["IsDomainName"] = dns.IsDomainName
	decs["IsDuplicate"] = dns.IsDuplicate
	decs["IsFqdn"] = dns.IsFqdn
	decs["IsMsg"] = dns.IsMsg
	decs["IsRRset"] = dns.IsRRset
	decs["IsSubDomain"] = dns.IsSubDomain
	decs["KEY"] = reflect.TypeFor[dns.KEY]()
	decs["KX"] = reflect.TypeFor[dns.KX]()
	decs["L32"] = reflect.TypeFor[dns.L32]()
	decs["L64"] = reflect.TypeFor[dns.L64]()
	decs["LOC"] = reflect.TypeFor[dns.LOC]()
	decs["LOC_ALTITUDEBASE"] = native.UntypedNumericConst("100000")
	decs["LOC_DEGREES"] = native.UntypedNumericConst("3600000")
	decs["LOC_EQUATOR"] = native.UntypedNumericConst("2147483648")
	decs["LOC_HOURS"] = native.UntypedNumericConst("60000")
	decs["LOC_PRIMEMERIDIAN"] = native.UntypedNumericConst("2147483648")
	decs["LP"] = reflect.TypeFor[dns.LP]()
	decs["Len"] = dns.Len
	decs["ListenAndServe"] = dns.ListenAndServe
	decs["ListenAndServeTLS"] = dns.ListenAndServeTLS
	decs["MB"] = reflect.TypeFor[dns.MB]()
	decs["MD"] = reflect.TypeFor[dns.MD]()
	decs["MF"] = reflect.TypeFor[dns.MF]()
	decs["MG"] = reflect.TypeFor[dns.MG]()
	decs["MINFO"] = reflect.TypeFor[dns.MINFO]()
	decs["MR"] = reflect.TypeFor[dns.MR]()
	decs["MX"] = reflect.TypeFor[dns.MX]()
	decs["MaxMsgSize"] = native.UntypedNumericConst("65535")
	decs["MinMsgSize"] = native.UntypedNumericConst("512")
	decs["Msg"] = reflect.TypeFor[dns.Msg]()
	decs["MsgAccept"] = dns.MsgAccept
	decs["MsgAcceptAction"] = reflect.TypeFor[dns.MsgAcceptAction]()
	decs["MsgAcceptFunc"] = reflect.TypeFor[dns.MsgAcceptFunc]()
	decs["MsgHdr"] = reflect.TypeFor[dns.MsgHdr]()
	decs["MsgIgnore"] = dns.MsgIgnore
	decs["MsgReject"] = dns.MsgReject
	decs["MsgRejectNotImplemented"] = dns.MsgRejectNotImplemented
	decs["NAPTR"] = reflect.TypeFor[dns.NAPTR]()
	decs["NID"] = reflect.TypeFor[dns.NID]()
	decs["NIMLOC"] = reflect.TypeFor[dns.NIMLOC]()
	decs["NINFO"] = reflect.TypeFor[dns.NINFO]()
	decs["NS"] = reflect.TypeFor[dns.NS]()
	decs["NSAPPTR"] = reflect.TypeFor[dns.NSAPPTR]()
	decs["NSEC"] = reflect.TypeFor[dns.NSEC]()
	decs["NSEC3"] = reflect.TypeFor[dns.NSEC3]()
	decs["NSEC3PARAM"] = reflect.TypeFor[dns.NSEC3PARAM]()
	decs["NULL"] = reflect.TypeFor[dns.NULL]()
	decs["Name"] = reflect.TypeFor[dns.Name]()
	decs["NewRR"] = dns.NewRR
	decs["NewServeMux"] = dns.NewServeMux
	decs["NewZoneParser"] = dns.NewZoneParser
	decs["NextLabel"] = dns.NextLabel
	decs["NumField"] = dns.NumField
	decs["OPENPGPKEY"] = reflect.TypeFor[dns.OPENPGPKEY]()
	decs["OPT"] = reflect.TypeFor[dns.OPT]()
	decs["OpcodeIQuery"] = native.UntypedNumericConst("1")
	decs["OpcodeNotify"] = native.UntypedNumericConst("4")
	decs["OpcodeQuery"] = native.UntypedNumericConst("0")
	decs["OpcodeStatus"] = native.UntypedNumericConst("2")
	decs["OpcodeToString"] = &dns.OpcodeToString
	decs["OpcodeUpdate"] = native.UntypedNumericConst("5")
	decs["PRIVATEDNS"] = dns.PRIVATEDNS
	decs["PRIVATEOID"] = dns.PRIVATEOID
	decs["PTR"] = reflect.TypeFor[dns.PTR]()
	decs["PX"] = reflect.TypeFor[dns.PX]()
	decs["PackDomainName"] = dns.PackDomainName
	decs["PackRR"] = dns.PackRR
	decs["PacketConnReader"] = reflect.TypeFor[dns.PacketConnReader]()
	decs["ParseError"] = reflect.TypeFor[dns.ParseError]()
	decs["PrevLabel"] = dns.PrevLabel
	decs["PrivateHandle"] = dns.PrivateHandle
	decs["PrivateHandleRemove"] = dns.PrivateHandleRemove
	decs["PrivateRR"] = reflect.TypeFor[dns.PrivateRR]()
	decs["PrivateRdata"] = reflect.TypeFor[dns.PrivateRdata]()
	decs["Question"] = reflect.TypeFor[dns.Question]()
	decs["REVOKE"] = native.UntypedNumericConst("128")
	decs["RFC3597"] = reflect.TypeFor[dns.RFC3597]()
	decs["RKEY"] = reflect.TypeFor[dns.RKEY]()
	decs["RP"] = reflect.TypeFor[dns.RP]()
	decs["RR"] = reflect.TypeFor[dns.RR]()
	decs["RRSIG"] = reflect.TypeFor[dns.RRSIG]()
	decs["RR_Header"] = reflect.TypeFor[dns.RR_Header]()
	decs["RSAMD5"] = dns.RSAMD5
	decs["RSASHA1"] = dns.RSASHA1
	decs["RSASHA1NSEC3SHA1"] = dns.RSASHA1NSEC3SHA1
	decs["RSASHA256"] = dns.RSASHA256
	decs["RSASHA512"] = dns.RSASHA512
	decs["RT"] = reflect.TypeFor[dns.RT]()
	decs["RcodeBadAlg"] = native.UntypedNumericConst("21")
	decs["RcodeBadCookie"] = native.UntypedNumericConst("23")
	decs["RcodeBadKey"] = native.UntypedNumericConst("17")
	decs["RcodeBadMode"] = native.UntypedNumericConst("19")
	decs["RcodeBadName"] = native.UntypedNumericConst("20")
	decs["RcodeBadSig"] = native.UntypedNumericConst("16")
	decs["RcodeBadTime"] = native.UntypedNumericConst("18")
	decs["RcodeBadTrunc"] = native.UntypedNumericConst("22")
	decs["RcodeBadVers"] = native.UntypedNumericConst("16")
	decs["RcodeFormatError"] = native.UntypedNumericConst("1")
	decs["RcodeNXRrset"] = native.UntypedNumericConst("8")
	decs["RcodeNameError"] = native.UntypedNumericConst("3")
	decs["RcodeNotAuth"] = native.UntypedNumericConst("9")
	decs["RcodeNotImplemented"] = native.UntypedNumericConst("4")
	decs["RcodeNotZone"] = native.UntypedNumericConst("10")
	decs["RcodeRefused"] = native.UntypedNumericConst("5")
	decs["RcodeServerFailure"] = native.UntypedNumericConst("2")
	decs["RcodeSuccess"] = native.UntypedNumericConst("0")
	decs["RcodeToString"] = &dns.RcodeToString
	decs["RcodeYXDomain"] = native.UntypedNumericConst("6")
	decs["RcodeYXRrset"] = native.UntypedNumericConst("7")
	decs["ReadFromSessionUDP"] = dns.ReadFromSessionUDP
	decs["ReadRR"] = dns.ReadRR
	decs["Reader"] = reflect.TypeFor[dns.Reader]()
	decs["ResponseWriter"] = reflect.TypeFor[dns.ResponseWriter]()
	decs["ReverseAddr"] = dns.ReverseAddr
	decs["SEP"] = native.UntypedNumericConst("1")
	decs["SHA1"] = dns.SHA1
	decs["SHA256"] = dns.SHA256
	decs["SHA384"] = dns.SHA384
	decs["SHA512"] = dns.SHA512
	decs["SIG"] = reflect.TypeFor[dns.SIG]()
	decs["SMIMEA"] = reflect.TypeFor[dns.SMIMEA]()
	decs["SMIMEAName"] = dns.SMIMEAName
	decs["SOA"] = reflect.TypeFor[dns.SOA]()
	decs["SPF"] = reflect.TypeFor[dns.SPF]()
	decs["SRV"] = reflect.TypeFor[dns.SRV]()
	decs["SSHFP"] = reflect.TypeFor[dns.SSHFP]()
	decs["SVCB"] = reflect.TypeFor[dns.SVCB]()
	decs["SVCBAlpn"] = reflect.TypeFor[dns.SVCBAlpn]()
	decs["SVCBDoHPath"] = reflect.TypeFor[dns.SVCBDoHPath]()
	decs["SVCBECHConfig"] = reflect.TypeFor[dns.SVCBECHConfig]()
	decs["SVCBIPv4Hint"] = reflect.TypeFor[dns.SVCBIPv4Hint]()
	decs["SVCBIPv6Hint"] = reflect.TypeFor[dns.SVCBIPv6Hint]()
	decs["SVCBKey"] = reflect.TypeFor[dns.SVCBKey]()
	decs["SVCBKeyValue"] = reflect.TypeFor[dns.SVCBKeyValue]()
	decs["SVCBLocal"] = reflect.TypeFor[dns.SVCBLocal]()
	decs["SVCBMandatory"] = reflect.TypeFor[dns.SVCBMandatory]()
	decs["SVCBNoDefaultAlpn"] = reflect.TypeFor[dns.SVCBNoDefaultAlpn]()
	decs["SVCBPort"] = reflect.TypeFor[dns.SVCBPort]()
	decs["SVCB_ALPN"] = dns.SVCB_ALPN
	decs["SVCB_DOHPATH"] = dns.SVCB_DOHPATH
	decs["SVCB_ECHCONFIG"] = dns.SVCB_ECHCONFIG
	decs["SVCB_IPV4HINT"] = dns.SVCB_IPV4HINT
	decs["SVCB_IPV6HINT"] = dns.SVCB_IPV6HINT
	decs["SVCB_MANDATORY"] = dns.SVCB_MANDATORY
	decs["SVCB_NO_DEFAULT_ALPN"] = dns.SVCB_NO_DEFAULT_ALPN
	decs["SVCB_PORT"] = dns.SVCB_PORT
	decs["ServeMux"] = reflect.TypeFor[dns.ServeMux]()
	decs["Server"] = reflect.TypeFor[dns.Server]()
	decs["SessionUDP"] = reflect.TypeFor[dns.SessionUDP]()
	decs["Split"] = dns.Split
	decs["SplitDomainName"] = dns.SplitDomainName
	decs["StringToAlgorithm"] = &dns.StringToAlgorithm
	decs["StringToCertType"] = &dns.StringToCertType
	decs["StringToClass"] = &dns.StringToClass
	decs["StringToExtendedErrorCode"] = &dns.StringToExtendedErrorCode
	decs["StringToHash"] = &dns.StringToHash
	decs["StringToOpcode"] = &dns.StringToOpcode
	decs["StringToRcode"] = &dns.StringToRcode
	decs["StringToTime"] = dns.StringToTime
	decs["StringToType"] = &dns.StringToType
	decs["TA"] = reflect.TypeFor[dns.TA]()
	decs["TALINK"] = reflect.TypeFor[dns.TALINK]()
	decs["TKEY"] = reflect.TypeFor[dns.TKEY]()
	decs["TLSA"] = reflect.TypeFor[dns.TLSA]()
	decs["TLSAName"] = dns.TLSAName
	decs["TSIG"] = reflect.TypeFor[dns.TSIG]()
	decs["TXT"] = reflect.TypeFor[dns.TXT]()
	decs["TimeToString"] = dns.TimeToString
	decs["Transfer"] = reflect.TypeFor[dns.Transfer]()
	decs["TsigGenerate"] = dns.TsigGenerate
	decs["TsigGenerateWithProvider"] = dns.TsigGenerateWithProvider
	decs["TsigProvider"] = reflect.TypeFor[dns.TsigProvider]()
	decs["TsigVerify"] = dns.TsigVerify
	decs["TsigVerifyWithProvider"] = dns.TsigVerifyWithProvider
	decs["Type"] = reflect.TypeFor[dns.Type]()
	decs["TypeA"] = dns.TypeA
	decs["TypeAAAA"] = dns.TypeAAAA
	decs["TypeAFSDB"] = dns.TypeAFSDB
	decs["TypeAMTRELAY"] = dns.TypeAMTRELAY
	decs["TypeANY"] = dns.TypeANY
	decs["TypeAPL"] = dns.TypeAPL
	decs["TypeATMA"] = dns.TypeATMA
	decs["TypeAVC"] = dns.TypeAVC
	decs["TypeAXFR"] = dns.TypeAXFR
	decs["TypeCAA"] = dns.TypeCAA
	decs["TypeCDNSKEY"] = dns.TypeCDNSKEY
	decs["TypeCDS"] = dns.TypeCDS
	decs["TypeCERT"] = dns.TypeCERT
	decs["TypeCNAME"] = dns.TypeCNAME
	decs["TypeCSYNC"] = dns.TypeCSYNC
	decs["TypeDHCID"] = dns.TypeDHCID
	decs["TypeDLV"] = dns.TypeDLV
	decs["TypeDNAME"] = dns.TypeDNAME
	decs["TypeDNSKEY"] = dns.TypeDNSKEY
	decs["TypeDS"] = dns.TypeDS
	decs["TypeEID"] = dns.TypeEID
	decs["TypeEUI48"] = dns.TypeEUI48
	decs["TypeEUI64"] = dns.TypeEUI64
	decs["TypeGID"] = dns.TypeGID
	decs["TypeGPOS"] = dns.TypeGPOS
	decs["TypeHINFO"] = dns.TypeHINFO
	decs["TypeHIP"] = dns.TypeHIP
	decs["TypeHTTPS"] = dns.TypeHTTPS
	decs["TypeIPSECKEY"] = dns.TypeIPSECKEY
	decs["TypeISDN"] = dns.TypeISDN
	decs["TypeIXFR"] = dns.TypeIXFR
	decs["TypeKEY"] = dns.TypeKEY
	decs["TypeKX"] = dns.TypeKX
	decs["TypeL32"] = dns.TypeL32
	decs["TypeL64"] = dns.TypeL64
	decs["TypeLOC"] = dns.TypeLOC
	decs["TypeLP"] = dns.TypeLP
	decs["TypeMAILA"] = dns.TypeMAILA
	decs["TypeMAILB"] = dns.TypeMAILB
	decs["TypeMB"] = dns.TypeMB
	decs["TypeMD"] = dns.TypeMD
	decs["TypeMF"] = dns.TypeMF
	decs["TypeMG"] = dns.TypeMG
	decs["TypeMINFO"] = dns.TypeMINFO
	decs["TypeMR"] = dns.TypeMR
	decs["TypeMX"] = dns.TypeMX
	decs["TypeNAPTR"] = dns.TypeNAPTR
	decs["TypeNID"] = dns.TypeNID
	decs["TypeNIMLOC"] = dns.TypeNIMLOC
	decs["TypeNINFO"] = dns.TypeNINFO
	decs["TypeNS"] = dns.TypeNS
	decs["TypeNSAPPTR"] = dns.TypeNSAPPTR
	decs["TypeNSEC"] = dns.TypeNSEC
	decs["TypeNSEC3"] = dns.TypeNSEC3
	decs["TypeNSEC3PARAM"] = dns.TypeNSEC3PARAM
	decs["TypeNULL"] = dns.TypeNULL
	decs["TypeNXT"] = dns.TypeNXT
	decs["TypeNone"] = dns.TypeNone
	decs["TypeOPENPGPKEY"] = dns.TypeOPENPGPKEY
	decs["TypeOPT"] = dns.TypeOPT
	decs["TypePTR"] = dns.TypePTR
	decs["TypePX"] = dns.TypePX
	decs["TypeRKEY"] = dns.TypeRKEY
	decs["TypeRP"] = dns.TypeRP
	decs["TypeRRSIG"] = dns.TypeRRSIG
	decs["TypeRT"] = dns.TypeRT
	decs["TypeReserved"] = dns.TypeReserved
	decs["TypeSIG"] = dns.TypeSIG
	decs["TypeSMIMEA"] = dns.TypeSMIMEA
	decs["TypeSOA"] = dns.TypeSOA
	decs["TypeSPF"] = dns.TypeSPF
	decs["TypeSRV"] = dns.TypeSRV
	decs["TypeSSHFP"] = dns.TypeSSHFP
	decs["TypeSVCB"] = dns.TypeSVCB
	decs["TypeTA"] = dns.TypeTA
	decs["TypeTALINK"] = dns.TypeTALINK
	decs["TypeTKEY"] = dns.TypeTKEY
	decs["TypeTLSA"] = dns.TypeTLSA
	decs["TypeTSIG"] = dns.TypeTSIG
	decs["TypeTXT"] = dns.TypeTXT
	decs["TypeToRR"] = &dns.TypeToRR
	decs["TypeToString"] = &dns.TypeToString
	decs["TypeUID"] = dns.TypeUID
	decs["TypeUINFO"] = dns.TypeUINFO
	decs["TypeUNSPEC"] = dns.TypeUNSPEC
	decs["TypeURI"] = dns.TypeURI
	decs["TypeX25"] = dns.TypeX25
	decs["TypeZONEMD"] = dns.TypeZONEMD
	decs["UID"] = reflect.TypeFor[dns.UID]()
	decs["UINFO"] = reflect.TypeFor[dns.UINFO]()
	decs["URI"] = reflect.TypeFor[dns.URI]()
	decs["UnpackDomainName"] = dns.UnpackDomainName
	decs["UnpackRR"] = dns.UnpackRR
	decs["UnpackRRWithHeader"] = dns.UnpackRRWithHeader
	decs["Version"] = &dns.Version
	decs["WriteToSessionUDP"] = dns.WriteToSessionUDP
	decs["Writer"] = reflect.TypeFor[dns.Writer]()
	decs["X25"] = reflect.TypeFor[dns.X25]()
	decs["ZONE"] = native.UntypedNumericConst("256")
	decs["ZONEMD"] = reflect.TypeFor[dns.ZONEMD]()
	decs["ZoneMDHashAlgSHA384"] = native.UntypedNumericConst("1")
	decs["ZoneMDHashAlgSHA512"] = native.UntypedNumericConst("2")
	decs["ZoneMDSchemeSimple"] = native.UntypedNumericConst("1")
	decs["ZoneParser"] = reflect.TypeFor[dns.ZoneParser]()
	packages["github.com/miekg/dns"] = native.Package{
		Name:         "dns",
		Declarations: decs,
	}
	// "go/ast"
	decs = make(native.Declarations, 102)
	decs["ArrayType"] = reflect.TypeFor[ast.ArrayType]()
	decs["AssignStmt"] = reflect.TypeFor[ast.AssignStmt]()
	decs["Bad"] = ast.Bad
	decs["BadDecl"] = reflect.TypeFor[ast.BadDecl]()
	decs["BadExpr"] = reflect.TypeFor[ast.BadExpr]()
	decs["BadStmt"] = reflect.TypeFor[ast.BadStmt]()
	decs["BasicLit"] = reflect.TypeFor[ast.BasicLit]()
	decs["BinaryExpr"] = reflect.TypeFor[ast.BinaryExpr]()
	decs["BlockStmt"] = reflect.TypeFor[ast.BlockStmt]()
	decs["BranchStmt"] = reflect.TypeFor[ast.BranchStmt]()
	decs["CallExpr"] = reflect.TypeFor[ast.CallExpr]()
	decs["CaseClause"] = reflect.TypeFor[ast.CaseClause]()
	decs["ChanDir"] = reflect.TypeFor[ast.ChanDir]()
	decs["ChanType"] = reflect.TypeFor[ast.ChanType]()
	decs["CommClause"] = reflect.TypeFor[ast.CommClause]()
	decs["Comment"] = reflect.TypeFor[ast.Comment]()
	decs["CommentGroup"] = reflect.TypeFor[ast.CommentGroup]()
	decs["CommentMap"] = reflect.TypeFor[ast.CommentMap]()
	decs["CompositeLit"] = reflect.TypeFor[ast.CompositeLit]()
	decs["Con"] = ast.Con
	decs["Decl"] = reflect.TypeFor[ast.Decl]()
	decs["DeclStmt"] = reflect.TypeFor[ast.DeclStmt]()
	decs["DeferStmt"] = reflect.TypeFor[ast.DeferStmt]()
	decs["Ellipsis"] = reflect.TypeFor[ast.Ellipsis]()
	decs["EmptyStmt"] = reflect.TypeFor[ast.EmptyStmt]()
	decs["Expr"] = reflect.TypeFor[ast.Expr]()
	decs["ExprStmt"] = reflect.TypeFor[ast.ExprStmt]()
	decs["Field"] = reflect.TypeFor[ast.Field]()
	decs["FieldFilter"] = reflect.TypeFor[ast.FieldFilter]()
	decs["FieldList"] = reflect.TypeFor[ast.FieldList]()
	decs["File"] = reflect.TypeFor[ast.File]()
	decs["FileExports"] = ast.FileExports
	decs["Filter"] = reflect.TypeFor[ast.Filter]()
	decs["FilterDecl"] = ast.FilterDecl
	decs["FilterFile"] = ast.FilterFile
	decs["FilterFuncDuplicates"] = ast.FilterFuncDuplicates
	decs["FilterImportDuplicates"] = ast.FilterImportDuplicates
	decs["FilterPackage"] = ast.FilterPackage
	decs["FilterUnassociatedComments"] = ast.FilterUnassociatedComments
	decs["ForStmt"] = reflect.TypeFor[ast.ForStmt]()
	decs["Fprint"] = ast.Fprint
	decs["Fun"] = ast.Fun
	decs["FuncDecl"] = reflect.TypeFor[ast.FuncDecl]()
	decs["FuncLit"] = reflect.TypeFor[ast.FuncLit]()
	decs["FuncType"] = reflect.TypeFor[ast.FuncType]()
	decs["GenDecl"] = reflect.TypeFor[ast.GenDecl]()
	decs["GoStmt"] = reflect.TypeFor[ast.GoStmt]()
	decs["Ident"] = reflect.TypeFor[ast.Ident]()
	decs["IfStmt"] = reflect.TypeFor[ast.IfStmt]()
	decs["ImportSpec"] = reflect.TypeFor[ast.ImportSpec]()
	decs["Importer"] = reflect.TypeFor[ast.Importer]()
	decs["IncDecStmt"] = reflect.TypeFor[ast.IncDecStmt]()
	decs["IndexExpr"] = reflect.TypeFor[ast.IndexExpr]()
	decs["IndexListExpr"] = reflect.TypeFor[ast.IndexListExpr]()
	decs["Inspect"] = ast.Inspect
	decs["InterfaceType"] = reflect.TypeFor[ast.InterfaceType]()
	decs["IsExported"] = ast.IsExported
	decs["IsGenerated"] = ast.IsGenerated
	decs["KeyValueExpr"] = reflect.TypeFor[ast.KeyValueExpr]()
	decs["LabeledStmt"] = reflect.TypeFor[ast.LabeledStmt]()
	decs["Lbl"] = ast.Lbl
	decs["MapType"] = reflect.TypeFor[ast.MapType]()
	decs["MergeMode"] = reflect.TypeFor[ast.MergeMode]()
	decs["MergePackageFiles"] = ast.MergePackageFiles
	decs["NewCommentMap"] = ast.NewCommentMap
	decs["NewIdent"] = ast.NewIdent
	decs["NewObj"] = ast.NewObj
	decs["NewPackage"] = ast.NewPackage
	decs["NewScope"] = ast.NewScope
	decs["Node"] = reflect.TypeFor[ast.Node]()
	decs["NotNilFilter"] = ast.NotNilFilter
	decs["ObjKind"] = reflect.TypeFor[ast.ObjKind]()
	decs["Object"] = reflect.TypeFor[ast.Object]()
	decs["Package"] = reflect.TypeFor[ast.Package]()
	decs["PackageExports"] = ast.PackageExports
	decs["ParenExpr"] = reflect.TypeFor[ast.ParenExpr]()
	decs["Pkg"] = ast.Pkg
	decs["Print"] = ast.Print
	decs["RECV"] = ast.RECV
	decs["RangeStmt"] = reflect.TypeFor[ast.RangeStmt]()
	decs["ReturnStmt"] = reflect.TypeFor[ast.ReturnStmt]()
	decs["SEND"] = ast.SEND
	decs["Scope"] = reflect.TypeFor[ast.Scope]()
	decs["SelectStmt"] = reflect.TypeFor[ast.SelectStmt]()
	decs["SelectorExpr"] = reflect.TypeFor[ast.SelectorExpr]()
	decs["SendStmt"] = reflect.TypeFor[ast.SendStmt]()
	decs["SliceExpr"] = reflect.TypeFor[ast.SliceExpr]()
	decs["SortImports"] = ast.SortImports
	decs["Spec"] = reflect.TypeFor[ast.Spec]()
	decs["StarExpr"] = reflect.TypeFor[ast.StarExpr]()
	decs["Stmt"] = reflect.TypeFor[ast.Stmt]()
	decs["StructType"] = reflect.TypeFor[ast.StructType]()
	decs["SwitchStmt"] = reflect.TypeFor[ast.SwitchStmt]()
	decs["Typ"] = ast.Typ
	decs["TypeAssertExpr"] = reflect.TypeFor[ast.TypeAssertExpr]()
	decs["TypeSpec"] = reflect.TypeFor[ast.TypeSpec]()
	decs["TypeSwitchStmt"] = reflect.TypeFor[ast.TypeSwitchStmt]()
	decs["UnaryExpr"] = reflect.TypeFor[ast.UnaryExpr]()
	decs["ValueSpec"] = reflect.TypeFor[ast.ValueSpec]()
	decs["Var"] = ast.Var
	decs["Visitor"] = reflect.TypeFor[ast.Visitor]()
	decs["Walk"] = ast.Walk
	packages["go/ast"] = native.Package{
		Name:         "ast",
		Declarations: decs,
	}
	// "go/build"
	decs = make(native.Declarations, 16)
	decs["AllowBinary"] = build.AllowBinary
	decs["ArchChar"] = build.ArchChar
	decs["Context"] = reflect.TypeFor[build.Context]()
	decs["Default"] = &build.Default
	decs["Directive"] = reflect.TypeFor[build.Directive]()
	decs["FindOnly"] = build.FindOnly
	decs["IgnoreVendor"] = build.IgnoreVendor
	decs["Import"] = build.Import
	decs["ImportComment"] = build.ImportComment
	decs["ImportDir"] = build.ImportDir
	decs["ImportMode"] = reflect.TypeFor[build.ImportMode]()
	decs["IsLocalImport"] = build.IsLocalImport
	decs["MultiplePackageError"] = reflect.TypeFor[build.MultiplePackageError]()
	decs["NoGoError"] = reflect.TypeFor[build.NoGoError]()
	decs["Package"] = reflect.TypeFor[build.Package]()
	decs["ToolDir"] = &build.ToolDir
	packages["go/build"] = native.Package{
		Name:         "build",
		Declarations: decs,
	}
	// "go/constant"
	decs = make(native.Declarations, 39)
	decs["BinaryOp"] = constant.BinaryOp
	decs["BitLen"] = constant.BitLen
	decs["Bool"] = constant.Bool
	decs["BoolVal"] = constant.BoolVal
	decs["Bytes"] = constant.Bytes
	decs["Compare"] = constant.Compare
	decs["Complex"] = constant.Complex
	decs["Denom"] = constant.Denom
	decs["Float"] = constant.Float
	decs["Float32Val"] = constant.Float32Val
	decs["Float64Val"] = constant.Float64Val
	decs["Imag"] = constant.Imag
	decs["Int"] = constant.Int
	decs["Int64Val"] = constant.Int64Val
	decs["Kind"] = reflect.TypeFor[constant.Kind]()
	decs["Make"] = constant.Make
	decs["MakeBool"] = constant.MakeBool
	decs["MakeFloat64"] = constant.MakeFloat64
	decs["MakeFromBytes"] = constant.MakeFromBytes
	decs["MakeFromLiteral"] = constant.MakeFromLiteral
	decs["MakeImag"] = constant.MakeImag
	decs["MakeInt64"] = constant.MakeInt64
	decs["MakeString"] = constant.MakeString
	decs["MakeUint64"] = constant.MakeUint64
	decs["MakeUnknown"] = constant.MakeUnknown
	decs["Num"] = constant.Num
	decs["Real"] = constant.Real
	decs["Shift"] = constant.Shift
	decs["Sign"] = constant.Sign
	decs["String"] = constant.String
	decs["StringVal"] = constant.StringVal
	decs["ToComplex"] = constant.ToComplex
	decs["ToFloat"] = constant.ToFloat
	decs["ToInt"] = constant.ToInt
	decs["Uint64Val"] = constant.Uint64Val
	decs["UnaryOp"] = constant.UnaryOp
	decs["Unknown"] = constant.Unknown
	decs["Val"] = constant.Val
	decs["Value"] = reflect.TypeFor[constant.Value]()
	packages["go/constant"] = native.Package{
		Name:         "constant",
		Declarations: decs,
	}
	// "go/doc"
	decs = make(native.Declarations, 19)
	decs["AllDecls"] = doc.AllDecls
	decs["AllMethods"] = doc.AllMethods
	decs["Example"] = reflect.TypeFor[doc.Example]()
	decs["Examples"] = doc.Examples
	decs["Filter"] = reflect.TypeFor[doc.Filter]()
	decs["Func"] = reflect.TypeFor[doc.Func]()
	decs["IllegalPrefixes"] = &doc.IllegalPrefixes
	decs["IsPredeclared"] = doc.IsPredeclared
	decs["Mode"] = reflect.TypeFor[doc.Mode]()
	decs["New"] = doc.New
	decs["NewFromFiles"] = doc.NewFromFiles
	decs["Note"] = reflect.TypeFor[doc.Note]()
	decs["Package"] = reflect.TypeFor[doc.Package]()
	decs["PreserveAST"] = doc.PreserveAST
	decs["Synopsis"] = doc.Synopsis
	decs["ToHTML"] = doc.ToHTML
	decs["ToText"] = doc.ToText
	decs["Type"] = reflect.TypeFor[doc.Type]()
	decs["Value"] = reflect.TypeFor[doc.Value]()
	packages["go/doc"] = native.Package{
		Name:         "doc",
		Declarations: decs,
	}
	// "go/format"
	decs = make(native.Declarations, 2)
	decs["Node"] = format.Node
	decs["Source"] = format.Source
	packages["go/format"] = native.Package{
		Name:         "format",
		Declarations: decs,
	}
	// "go/importer"
	decs = make(native.Declarations, 4)
	decs["Default"] = importer.Default
	decs["For"] = importer.For
	decs["ForCompiler"] = importer.ForCompiler
	decs["Lookup"] = reflect.TypeFor[importer.Lookup]()
	packages["go/importer"] = native.Package{
		Name:         "importer",
		Declarations: decs,
	}
	// "go/parser"
	decs = make(native.Declarations, 13)
	decs["AllErrors"] = parser.AllErrors
	decs["DeclarationErrors"] = parser.DeclarationErrors
	decs["ImportsOnly"] = parser.ImportsOnly
	decs["Mode"] = reflect.TypeFor[parser.Mode]()
	decs["PackageClauseOnly"] = parser.PackageClauseOnly
	decs["ParseComments"] = parser.ParseComments
	decs["ParseDir"] = parser.ParseDir
	decs["ParseExpr"] = parser.ParseExpr
	decs["ParseExprFrom"] = parser.ParseExprFrom
	decs["ParseFile"] = parser.ParseFile
	decs["SkipObjectResolution"] = parser.SkipObjectResolution
	decs["SpuriousErrors"] = parser.SpuriousErrors
	decs["Trace"] = parser.Trace
	packages["go/parser"] = native.Package{
		Name:         "parser",
		Declarations: decs,
	}
	// "go/printer"
	decs = make(native.Declarations, 8)
	decs["CommentedNode"] = reflect.TypeFor[printer.CommentedNode]()
	decs["Config"] = reflect.TypeFor[printer.Config]()
	decs["Fprint"] = printer.Fprint
	decs["Mode"] = reflect.TypeFor[printer.Mode]()
	decs["RawFormat"] = printer.RawFormat
	decs["SourcePos"] = printer.SourcePos
	decs["TabIndent"] = printer.TabIndent
	decs["UseSpaces"] = printer.UseSpaces
	packages["go/printer"] = native.Package{
		Name:         "printer",
		Declarations: decs,
	}
	// "go/scanner"
	decs = make(native.Declarations, 7)
	decs["Error"] = reflect.TypeFor[scanner.Error]()
	decs["ErrorHandler"] = reflect.TypeFor[scanner.ErrorHandler]()
	decs["ErrorList"] = reflect.TypeFor[scanner.ErrorList]()
	decs["Mode"] = reflect.TypeFor[scanner.Mode]()
	decs["PrintError"] = scanner.PrintError
	decs["ScanComments"] = scanner.ScanComments
	decs["Scanner"] = reflect.TypeFor[scanner.Scanner]()
	packages["go/scanner"] = native.Package{
		Name:         "scanner",
		Declarations: decs,
	}
	// "go/token"
	decs = make(native.Declarations, 96)
	decs["ADD"] = token.ADD
	decs["ADD_ASSIGN"] = token.ADD_ASSIGN
	decs["AND"] = token.AND
	decs["AND_ASSIGN"] = token.AND_ASSIGN
	decs["AND_NOT"] = token.AND_NOT
	decs["AND_NOT_ASSIGN"] = token.AND_NOT_ASSIGN
	decs["ARROW"] = token.ARROW
	decs["ASSIGN"] = token.ASSIGN
	decs["BREAK"] = token.BREAK
	decs["CASE"] = token.CASE
	decs["CHAN"] = token.CHAN
	decs["CHAR"] = token.CHAR
	decs["COLON"] = token.COLON
	decs["COMMA"] = token.COMMA
	decs["COMMENT"] = token.COMMENT
	decs["CONST"] = token.CONST
	decs["CONTINUE"] = token.CONTINUE
	decs["DEC"] = token.DEC
	decs["DEFAULT"] = token.DEFAULT
	decs["DEFER"] = token.DEFER
	decs["DEFINE"] = token.DEFINE
	decs["ELLIPSIS"] = token.ELLIPSIS
	decs["ELSE"] = token.ELSE
	decs["EOF"] = token.EOF
	decs["EQL"] = token.EQL
	decs["FALLTHROUGH"] = token.FALLTHROUGH
	decs["FLOAT"] = token.FLOAT
	decs["FOR"] = token.FOR
	decs["FUNC"] = token.FUNC
	decs["File"] = reflect.TypeFor[token.File]()
	decs["FileSet"] = reflect.TypeFor[token.FileSet]()
	decs["GEQ"] = token.GEQ
	decs["GO"] = token.GO
	decs["GOTO"] = token.GOTO
	decs["GTR"] = token.GTR
	decs["HighestPrec"] = native.UntypedNumericConst("7")
	decs["IDENT"] = token.IDENT
	decs["IF"] = token.IF
	decs["ILLEGAL"] = token.ILLEGAL
	decs["IMAG"] = token.IMAG
	decs["IMPORT"] = token.IMPORT
	decs["INC"] = token.INC
	decs["INT"] = token.INT
	decs["INTERFACE"] = token.INTERFACE
	decs["IsExported"] = token.IsExported
	decs["IsIdentifier"] = token.IsIdentifier
	decs["IsKeyword"] = token.IsKeyword
	decs["LAND"] = token.LAND
	decs["LBRACE"] = token.LBRACE
	decs["LBRACK"] = token.LBRACK
	decs["LEQ"] = token.LEQ
	decs["LOR"] = token.LOR
	decs["LPAREN"] = token.LPAREN
	decs["LSS"] = token.LSS
	decs["Lookup"] = token.Lookup
	decs["LowestPrec"] = native.UntypedNumericConst("0")
	decs["MAP"] = token.MAP
	decs["MUL"] = token.MUL
	decs["MUL_ASSIGN"] = token.MUL_ASSIGN
	decs["NEQ"] = token.NEQ
	decs["NOT"] = token.NOT
	decs["NewFileSet"] = token.NewFileSet
	decs["NoPos"] = token.NoPos
	decs["OR"] = token.OR
	decs["OR_ASSIGN"] = token.OR_ASSIGN
	decs["PACKAGE"] = token.PACKAGE
	decs["PERIOD"] = token.PERIOD
	decs["Pos"] = reflect.TypeFor[token.Pos]()
	decs["Position"] = reflect.TypeFor[token.Position]()
	decs["QUO"] = token.QUO
	decs["QUO_ASSIGN"] = token.QUO_ASSIGN
	decs["RANGE"] = token.RANGE
	decs["RBRACE"] = token.RBRACE
	decs["RBRACK"] = token.RBRACK
	decs["REM"] = token.REM
	decs["REM_ASSIGN"] = token.REM_ASSIGN
	decs["RETURN"] = token.RETURN
	decs["RPAREN"] = token.RPAREN
	decs["SELECT"] = token.SELECT
	decs["SEMICOLON"] = token.SEMICOLON
	decs["SHL"] = token.SHL
	decs["SHL_ASSIGN"] = token.SHL_ASSIGN
	decs["SHR"] = token.SHR
	decs["SHR_ASSIGN"] = token.SHR_ASSIGN
	decs["STRING"] = token.STRING
	decs["STRUCT"] = token.STRUCT
	decs["SUB"] = token.SUB
	decs["SUB_ASSIGN"] = token.SUB_ASSIGN
	decs["SWITCH"] = token.SWITCH
	decs["TILDE"] = token.TILDE
	decs["TYPE"] = token.TYPE
	decs["Token"] = reflect.TypeFor[token.Token]()
	decs["UnaryPrec"] = native.UntypedNumericConst("6")
	decs["VAR"] = token.VAR
	decs["XOR"] = token.XOR
	decs["XOR_ASSIGN"] = token.XOR_ASSIGN
	packages["go/token"] = native.Package{
		Name:         "token",
		Declarations: decs,
	}
	// "go/types"
	decs = make(native.Declarations, 150)
	decs["ArgumentError"] = reflect.TypeFor[types.ArgumentError]()
	decs["Array"] = reflect.TypeFor[types.Array]()
	decs["AssertableTo"] = types.AssertableTo
	decs["AssignableTo"] = types.AssignableTo
	decs["Basic"] = reflect.TypeFor[types.Basic]()
	decs["BasicInfo"] = reflect.TypeFor[types.BasicInfo]()
	decs["BasicKind"] = reflect.TypeFor[types.BasicKind]()
	decs["Bool"] = types.Bool
	decs["Builtin"] = reflect.TypeFor[types.Builtin]()
	decs["Byte"] = types.Byte
	decs["Chan"] = reflect.TypeFor[types.Chan]()
	decs["ChanDir"] = reflect.TypeFor[types.ChanDir]()
	decs["CheckExpr"] = types.CheckExpr
	decs["Checker"] = reflect.TypeFor[types.Checker]()
	decs["Comparable"] = types.Comparable
	decs["Complex128"] = types.Complex128
	decs["Complex64"] = types.Complex64
	decs["Config"] = reflect.TypeFor[types.Config]()
	decs["Const"] = reflect.TypeFor[types.Const]()
	decs["Context"] = reflect.TypeFor[types.Context]()
	decs["ConvertibleTo"] = types.ConvertibleTo
	decs["DefPredeclaredTestFuncs"] = types.DefPredeclaredTestFuncs
	decs["Default"] = types.Default
	decs["Error"] = reflect.TypeFor[types.Error]()
	decs["Eval"] = types.Eval
	decs["ExprString"] = types.ExprString
	decs["FieldVal"] = types.FieldVal
	decs["Float32"] = types.Float32
	decs["Float64"] = types.Float64
	decs["Func"] = reflect.TypeFor[types.Func]()
	decs["Id"] = types.Id
	decs["Identical"] = types.Identical
	decs["IdenticalIgnoreTags"] = types.IdenticalIgnoreTags
	decs["Implements"] = types.Implements
	decs["ImportMode"] = reflect.TypeFor[types.ImportMode]()
	decs["Importer"] = reflect.TypeFor[types.Importer]()
	decs["ImporterFrom"] = reflect.TypeFor[types.ImporterFrom]()
	decs["Info"] = reflect.TypeFor[types.Info]()
	decs["Initializer"] = reflect.TypeFor[types.Initializer]()
	decs["Instance"] = reflect.TypeFor[types.Instance]()
	decs["Instantiate"] = types.Instantiate
	decs["Int"] = types.Int
	decs["Int16"] = types.Int16
	decs["Int32"] = types.Int32
	decs["Int64"] = types.Int64
	decs["Int8"] = types.Int8
	decs["Interface"] = reflect.TypeFor[types.Interface]()
	decs["Invalid"] = types.Invalid
	decs["IsBoolean"] = types.IsBoolean
	decs["IsComplex"] = types.IsComplex
	decs["IsConstType"] = types.IsConstType
	decs["IsFloat"] = types.IsFloat
	decs["IsInteger"] = types.IsInteger
	decs["IsInterface"] = types.IsInterface
	decs["IsNumeric"] = types.IsNumeric
	decs["IsOrdered"] = types.IsOrdered
	decs["IsString"] = types.IsString
	decs["IsUnsigned"] = types.IsUnsigned
	decs["IsUntyped"] = types.IsUntyped
	decs["Label"] = reflect.TypeFor[types.Label]()
	decs["LookupFieldOrMethod"] = types.LookupFieldOrMethod
	decs["Map"] = reflect.TypeFor[types.Map]()
	decs["MethodExpr"] = types.MethodExpr
	decs["MethodSet"] = reflect.TypeFor[types.MethodSet]()
	decs["MethodVal"] = types.MethodVal
	decs["MissingMethod"] = types.MissingMethod
	decs["Named"] = reflect.TypeFor[types.Named]()
	decs["NewArray"] = types.NewArray
	decs["NewChan"] = types.NewChan
	decs["NewChecker"] = types.NewChecker
	decs["NewConst"] = types.NewConst
	decs["NewContext"] = types.NewContext
	decs["NewField"] = types.NewField
	decs["NewFunc"] = types.NewFunc
	decs["NewInterface"] = types.NewInterface
	decs["NewInterfaceType"] = types.NewInterfaceType
	decs["NewLabel"] = types.NewLabel
	decs["NewMap"] = types.NewMap
	decs["NewMethodSet"] = types.NewMethodSet
	decs["NewNamed"] = types.NewNamed
	decs["NewPackage"] = types.NewPackage
	decs["NewParam"] = types.NewParam
	decs["NewPkgName"] = types.NewPkgName
	decs["NewPointer"] = types.NewPointer
	decs["NewScope"] = types.NewScope
	decs["NewSignature"] = types.NewSignature
	decs["NewSignatureType"] = types.NewSignatureType
	decs["NewSlice"] = types.NewSlice
	decs["NewStruct"] = types.NewStruct
	decs["NewTerm"] = types.NewTerm
	decs["NewTuple"] = types.NewTuple
	decs["NewTypeName"] = types.NewTypeName
	decs["NewTypeParam"] = types.NewTypeParam
	decs["NewUnion"] = types.NewUnion
	decs["NewVar"] = types.NewVar
	decs["Nil"] = reflect.TypeFor[types.Nil]()
	decs["Object"] = reflect.TypeFor[types.Object]()
	decs["ObjectString"] = types.ObjectString
	decs["Package"] = reflect.TypeFor[types.Package]()
	decs["PkgName"] = reflect.TypeFor[types.PkgName]()
	decs["Pointer"] = reflect.TypeFor[types.Pointer]()
	decs["Qualifier"] = reflect.TypeFor[types.Qualifier]()
	decs["RecvOnly"] = types.RecvOnly
	decs["RelativeTo"] = types.RelativeTo
	decs["Rune"] = types.Rune
	decs["Satisfies"] = types.Satisfies
	decs["Scope"] = reflect.TypeFor[types.Scope]()
	decs["Selection"] = reflect.TypeFor[types.Selection]()
	decs["SelectionKind"] = reflect.TypeFor[types.SelectionKind]()
	decs["SelectionString"] = types.SelectionString
	decs["SendOnly"] = types.SendOnly
	decs["SendRecv"] = types.SendRecv
	decs["Signature"] = reflect.TypeFor[types.Signature]()
	decs["Sizes"] = reflect.TypeFor[types.Sizes]()
	decs["SizesFor"] = types.SizesFor
	decs["Slice"] = reflect.TypeFor[types.Slice]()
	decs["StdSizes"] = reflect.TypeFor[types.StdSizes]()
	decs["String"] = types.String
	decs["Struct"] = reflect.TypeFor[types.Struct]()
	decs["Term"] = reflect.TypeFor[types.Term]()
	decs["Tuple"] = reflect.TypeFor[types.Tuple]()
	decs["Typ"] = &types.Typ
	decs["Type"] = reflect.TypeFor[types.Type]()
	decs["TypeAndValue"] = reflect.TypeFor[types.TypeAndValue]()
	decs["TypeList"] = reflect.TypeFor[types.TypeList]()
	decs["TypeName"] = reflect.TypeFor[types.TypeName]()
	decs["TypeParam"] = reflect.TypeFor[types.TypeParam]()
	decs["TypeParamList"] = reflect.TypeFor[types.TypeParamList]()
	decs["TypeString"] = types.TypeString
	decs["Uint"] = types.Uint
	decs["Uint16"] = types.Uint16
	decs["Uint32"] = types.Uint32
	decs["Uint64"] = types.Uint64
	decs["Uint8"] = types.Uint8
	decs["Uintptr"] = types.Uintptr
	decs["Union"] = reflect.TypeFor[types.Union]()
	decs["Universe"] = &types.Universe
	decs["Unsafe"] = &types.Unsafe
	decs["UnsafePointer"] = types.UnsafePointer
	decs["UntypedBool"] = types.UntypedBool
	decs["UntypedComplex"] = types.UntypedComplex
	decs["UntypedFloat"] = types.UntypedFloat
	decs["UntypedInt"] = types.UntypedInt
	decs["UntypedNil"] = types.UntypedNil
	decs["UntypedRune"] = types.UntypedRune
	decs["UntypedString"] = types.UntypedString
	decs["Var"] = reflect.TypeFor[types.Var]()
	decs["WriteExpr"] = types.WriteExpr
	decs["WriteSignature"] = types.WriteSignature
	decs["WriteType"] = types.WriteType
	packages["go/types"] = native.Package{
		Name:         "types",
		Declarations: decs,
	}
	// "golang.org/x/text/encoding/japanese"
	decs = make(native.Declarations, 4)
	decs["All"] = &japanese.All
	decs["EUCJP"] = &japanese.EUCJP
	decs["ISO2022JP"] = &japanese.ISO2022JP
	decs["ShiftJIS"] = &japanese.ShiftJIS
	packages["golang.org/x/text/encoding/japanese"] = native.Package{
		Name:         "japanese",
		Declarations: decs,
	}
	// "hash"
	decs = make(native.Declarations, 3)
	decs["Hash"] = reflect.TypeFor[hash.Hash]()
	decs["Hash32"] = reflect.TypeFor[hash.Hash32]()
	decs["Hash64"] = reflect.TypeFor[hash.Hash64]()
	packages["hash"] = native.Package{
		Name:         "hash",
		Declarations: decs,
	}
	// "hash/adler32"
	decs = make(native.Declarations, 3)
	decs["Checksum"] = adler32.Checksum
	decs["New"] = adler32.New
	decs["Size"] = native.UntypedNumericConst("4")
	packages["hash/adler32"] = native.Package{
		Name:         "adler32",
		Declarations: decs,
	}
	// "hash/crc32"
	decs = make(native.Declarations, 12)
	decs["Castagnoli"] = native.UntypedNumericConst("2197175160")
	decs["Checksum"] = crc32.Checksum
	decs["ChecksumIEEE"] = crc32.ChecksumIEEE
	decs["IEEE"] = native.UntypedNumericConst("3988292384")
	decs["IEEETable"] = &crc32.IEEETable
	decs["Koopman"] = native.UntypedNumericConst("3945912366")
	decs["MakeTable"] = crc32.MakeTable
	decs["New"] = crc32.New
	decs["NewIEEE"] = crc32.NewIEEE
	decs["Size"] = native.UntypedNumericConst("4")
	decs["Table"] = reflect.TypeFor[crc32.Table]()
	decs["Update"] = crc32.Update
	packages["hash/crc32"] = native.Package{
		Name:         "crc32",
		Declarations: decs,
	}
	// "hash/crc64"
	decs = make(native.Declarations, 8)
	decs["Checksum"] = crc64.Checksum
	decs["ECMA"] = native.UntypedNumericConst("14514072000185962306")
	decs["ISO"] = native.UntypedNumericConst("15564440312192434176")
	decs["MakeTable"] = crc64.MakeTable
	decs["New"] = crc64.New
	decs["Size"] = native.UntypedNumericConst("8")
	decs["Table"] = reflect.TypeFor[crc64.Table]()
	decs["Update"] = crc64.Update
	packages["hash/crc64"] = native.Package{
		Name:         "crc64",
		Declarations: decs,
	}
	// "hash/fnv"
	decs = make(native.Declarations, 6)
	decs["New128"] = fnv.New128
	decs["New128a"] = fnv.New128a
	decs["New32"] = fnv.New32
	decs["New32a"] = fnv.New32a
	decs["New64"] = fnv.New64
	decs["New64a"] = fnv.New64a
	packages["hash/fnv"] = native.Package{
		Name:         "fnv",
		Declarations: decs,
	}
	// "hash/maphash"
	decs = make(native.Declarations, 5)
	decs["Bytes"] = maphash.Bytes
	decs["Hash"] = reflect.TypeFor[maphash.Hash]()
	decs["MakeSeed"] = maphash.MakeSeed
	decs["Seed"] = reflect.TypeFor[maphash.Seed]()
	decs["String"] = maphash.String
	packages["hash/maphash"] = native.Package{
		Name:         "maphash",
		Declarations: decs,
	}
	// "html"
	decs = make(native.Declarations, 2)
	decs["EscapeString"] = html.EscapeString
	decs["UnescapeString"] = html.UnescapeString
	packages["html"] = native.Package{
		Name:         "html",
		Declarations: decs,
	}
	// "html/template"
	decs = make(native.Declarations, 37)
	decs["CSS"] = reflect.TypeFor[template.CSS]()
	decs["ErrAmbigContext"] = template.ErrAmbigContext
	decs["ErrBadHTML"] = template.ErrBadHTML
	decs["ErrBranchEnd"] = template.ErrBranchEnd
	decs["ErrEndContext"] = template.ErrEndContext
	decs["ErrJSTemplate"] = template.ErrJSTemplate
	decs["ErrNoSuchTemplate"] = template.ErrNoSuchTemplate
	decs["ErrOutputContext"] = template.ErrOutputContext
	decs["ErrPartialCharset"] = template.ErrPartialCharset
	decs["ErrPartialEscape"] = template.ErrPartialEscape
	decs["ErrPredefinedEscaper"] = template.ErrPredefinedEscaper
	decs["ErrRangeLoopReentry"] = template.ErrRangeLoopReentry
	decs["ErrSlashAmbig"] = template.ErrSlashAmbig
	decs["Error"] = reflect.TypeFor[template.Error]()
	decs["ErrorCode"] = reflect.TypeFor[template.ErrorCode]()
	decs["FuncMap"] = reflect.TypeFor[template.FuncMap]()
	decs["HTML"] = reflect.TypeFor[template.HTML]()
	decs["HTMLAttr"] = reflect.TypeFor[template.HTMLAttr]()
	decs["HTMLEscape"] = template.HTMLEscape
	decs["HTMLEscapeString"] = template.HTMLEscapeString
	decs["HTMLEscaper"] = template.HTMLEscaper
	decs["IsTrue"] = template.IsTrue
	decs["JS"] = reflect.TypeFor[template.JS]()
	decs["JSEscape"] = template.JSEscape
	decs["JSEscapeString"] = template.JSEscapeString
	decs["JSEscaper"] = template.JSEscaper
	decs["JSStr"] = reflect.TypeFor[template.JSStr]()
	decs["Must"] = template.Must
	decs["New"] = template.New
	decs["OK"] = template.OK
	decs["ParseFS"] = template.ParseFS
	decs["ParseFiles"] = template.ParseFiles
	decs["ParseGlob"] = template.ParseGlob
	decs["Srcset"] = reflect.TypeFor[template.Srcset]()
	decs["Template"] = reflect.TypeFor[template.Template]()
	decs["URL"] = reflect.TypeFor[template.URL]()
	decs["URLQueryEscaper"] = template.URLQueryEscaper
	packages["html/template"] = native.Package{
		Name:         "template",
		Declarations: decs,
	}
	// "image"
	decs = make(native.Declarations, 51)
	decs["Alpha"] = reflect.TypeFor[image.Alpha]()
	decs["Alpha16"] = reflect.TypeFor[image.Alpha16]()
	decs["Black"] = &image.Black
	decs["CMYK"] = reflect.TypeFor[image.CMYK]()
	decs["Config"] = reflect.TypeFor[image.Config]()
	decs["Decode"] = image.Decode
	decs["DecodeConfig"] = image.DecodeConfig
	decs["ErrFormat"] = &image.ErrFormat
	decs["Gray"] = reflect.TypeFor[image.Gray]()
	decs["Gray16"] = reflect.TypeFor[image.Gray16]()
	decs["Image"] = reflect.TypeFor[image.Image]()
	decs["NRGBA"] = reflect.TypeFor[image.NRGBA]()
	decs["NRGBA64"] = reflect.TypeFor[image.NRGBA64]()
	decs["NYCbCrA"] = reflect.TypeFor[image.NYCbCrA]()
	decs["NewAlpha"] = image.NewAlpha
	decs["NewAlpha16"] = image.NewAlpha16
	decs["NewCMYK"] = image.NewCMYK
	decs["NewGray"] = image.NewGray
	decs["NewGray16"] = image.NewGray16
	decs["NewNRGBA"] = image.NewNRGBA
	decs["NewNRGBA64"] = image.NewNRGBA64
	decs["NewNYCbCrA"] = image.NewNYCbCrA
	decs["NewPaletted"] = image.NewPaletted
	decs["NewRGBA"] = image.NewRGBA
	decs["NewRGBA64"] = image.NewRGBA64
	decs["NewUniform"] = image.NewUniform
	decs["NewYCbCr"] = image.NewYCbCr
	decs["Opaque"] = &image.Opaque
	decs["Paletted"] = reflect.TypeFor[image.Paletted]()
	decs["PalettedImage"] = reflect.TypeFor[image.PalettedImage]()
	decs["Point"] = reflect.TypeFor[image.Point]()
	decs["Pt"] = image.Pt
	decs["RGBA"] = reflect.TypeFor[image.RGBA]()
	decs["RGBA64"] = reflect.TypeFor[image.RGBA64]()
	decs["RGBA64Image"] = reflect.TypeFor[image.RGBA64Image]()
	decs["Rect"] = image.Rect
	decs["Rectangle"] = reflect.TypeFor[image.Rectangle]()
	decs["RegisterFormat"] = image.RegisterFormat
	decs["Transparent"] = &image.Transparent
	decs["Uniform"] = reflect.TypeFor[image.Uniform]()
	decs["White"] = &image.White
	decs["YCbCr"] = reflect.TypeFor[image.YCbCr]()
	decs["YCbCrSubsampleRatio"] = reflect.TypeFor[image.YCbCrSubsampleRatio]()
	decs["YCbCrSubsampleRatio410"] = image.YCbCrSubsampleRatio410
	decs["YCbCrSubsampleRatio411"] = image.YCbCrSubsampleRatio411
	decs["YCbCrSubsampleRatio420"] = image.YCbCrSubsampleRatio420
	decs["YCbCrSubsampleRatio422"] = image.YCbCrSubsampleRatio422
	decs["YCbCrSubsampleRatio440"] = image.YCbCrSubsampleRatio440
	decs["YCbCrSubsampleRatio444"] = image.YCbCrSubsampleRatio444
	decs["ZP"] = &image.ZP
	decs["ZR"] = &image.ZR
	packages["image"] = native.Package{
		Name:         "image",
		Declarations: decs,
	}
	// "image/color"
	decs = make(native.Declarations, 34)
	decs["Alpha"] = reflect.TypeFor[color.Alpha]()
	decs["Alpha16"] = reflect.TypeFor[color.Alpha16]()
	decs["Alpha16Model"] = &color.Alpha16Model
	decs["AlphaModel"] = &color.AlphaModel
	decs["Black"] = &color.Black
	decs["CMYK"] = reflect.TypeFor[color.CMYK]()
	decs["CMYKModel"] = &color.CMYKModel
	decs["CMYKToRGB"] = color.CMYKToRGB
	decs["Color"] = reflect.TypeFor[color.Color]()
	decs["Gray"] = reflect.TypeFor[color.Gray]()
	decs["Gray16"] = reflect.TypeFor[color.Gray16]()
	decs["Gray16Model"] = &color.Gray16Model
	decs["GrayModel"] = &color.GrayModel
	decs["Model"] = reflect.TypeFor[color.Model]()
	decs["ModelFunc"] = color.ModelFunc
	decs["NRGBA"] = reflect.TypeFor[color.NRGBA]()
	decs["NRGBA64"] = reflect.TypeFor[color.NRGBA64]()
	decs["NRGBA64Model"] = &color.NRGBA64Model
	decs["NRGBAModel"] = &color.NRGBAModel
	decs["NYCbCrA"] = reflect.TypeFor[color.NYCbCrA]()
	decs["NYCbCrAModel"] = &color.NYCbCrAModel
	decs["Opaque"] = &color.Opaque
	decs["Palette"] = reflect.TypeFor[color.Palette]()
	decs["RGBA"] = reflect.TypeFor[color.RGBA]()
	decs["RGBA64"] = reflect.TypeFor[color.RGBA64]()
	decs["RGBA64Model"] = &color.RGBA64Model
	decs["RGBAModel"] = &color.RGBAModel
	decs["RGBToCMYK"] = color.RGBToCMYK
	decs["RGBToYCbCr"] = color.RGBToYCbCr
	decs["Transparent"] = &color.Transparent
	decs["White"] = &color.White
	decs["YCbCr"] = reflect.TypeFor[color.YCbCr]()
	decs["YCbCrModel"] = &color.YCbCrModel
	decs["YCbCrToRGB"] = color.YCbCrToRGB
	packages["image/color"] = native.Package{
		Name:         "color",
		Declarations: decs,
	}
	// "image/color/palette"
	decs = make(native.Declarations, 2)
	decs["Plan9"] = &palette.Plan9
	decs["WebSafe"] = &palette.WebSafe
	packages["image/color/palette"] = native.Package{
		Name:         "palette",
		Declarations: decs,
	}
	// "image/draw"
	decs = make(native.Declarations, 10)
	decs["Draw"] = draw.Draw
	decs["DrawMask"] = draw.DrawMask
	decs["Drawer"] = reflect.TypeFor[draw.Drawer]()
	decs["FloydSteinberg"] = &draw.FloydSteinberg
	decs["Image"] = reflect.TypeFor[draw.Image]()
	decs["Op"] = reflect.TypeFor[draw.Op]()
	decs["Over"] = draw.Over
	decs["Quantizer"] = reflect.TypeFor[draw.Quantizer]()
	decs["RGBA64Image"] = reflect.TypeFor[draw.RGBA64Image]()
	decs["Src"] = draw.Src
	packages["image/draw"] = native.Package{
		Name:         "draw",
		Declarations: decs,
	}
	// "image/gif"
	decs = make(native.Declarations, 10)
	decs["Decode"] = gif.Decode
	decs["DecodeAll"] = gif.DecodeAll
	decs["DecodeConfig"] = gif.DecodeConfig
	decs["DisposalBackground"] = native.UntypedNumericConst("2")
	decs["DisposalNone"] = native.UntypedNumericConst("1")
	decs["DisposalPrevious"] = native.UntypedNumericConst("3")
	decs["Encode"] = gif.Encode
	decs["EncodeAll"] = gif.EncodeAll
	decs["GIF"] = reflect.TypeFor[gif.GIF]()
	decs["Options"] = reflect.TypeFor[gif.Options]()
	packages["image/gif"] = native.Package{
		Name:         "gif",
		Declarations: decs,
	}
	// "image/jpeg"
	decs = make(native.Declarations, 8)
	decs["Decode"] = jpeg.Decode
	decs["DecodeConfig"] = jpeg.DecodeConfig
	decs["DefaultQuality"] = native.UntypedNumericConst("75")
	decs["Encode"] = jpeg.Encode
	decs["FormatError"] = reflect.TypeFor[jpeg.FormatError]()
	decs["Options"] = reflect.TypeFor[jpeg.Options]()
	decs["Reader"] = reflect.TypeFor[jpeg.Reader]()
	decs["UnsupportedError"] = reflect.TypeFor[jpeg.UnsupportedError]()
	packages["image/jpeg"] = native.Package{
		Name:         "jpeg",
		Declarations: decs,
	}
	// "image/png"
	decs = make(native.Declarations, 13)
	decs["BestCompression"] = png.BestCompression
	decs["BestSpeed"] = png.BestSpeed
	decs["CompressionLevel"] = reflect.TypeFor[png.CompressionLevel]()
	decs["Decode"] = png.Decode
	decs["DecodeConfig"] = png.DecodeConfig
	decs["DefaultCompression"] = png.DefaultCompression
	decs["Encode"] = png.Encode
	decs["Encoder"] = reflect.TypeFor[png.Encoder]()
	decs["EncoderBuffer"] = reflect.TypeFor[png.EncoderBuffer]()
	decs["EncoderBufferPool"] = reflect.TypeFor[png.EncoderBufferPool]()
	decs["FormatError"] = reflect.TypeFor[png.FormatError]()
	decs["NoCompression"] = png.NoCompression
	decs["UnsupportedError"] = reflect.TypeFor[png.UnsupportedError]()
	packages["image/png"] = native.Package{
		Name:         "png",
		Declarations: decs,
	}
	// "index/suffixarray"
	decs = make(native.Declarations, 2)
	decs["Index"] = reflect.TypeFor[suffixarray.Index]()
	decs["New"] = suffixarray.New
	packages["index/suffixarray"] = native.Package{
		Name:         "suffixarray",
		Declarations: decs,
	}
	// "io"
	decs = make(native.Declarations, 52)
	decs["ByteReader"] = reflect.TypeFor[io.ByteReader]()
	decs["ByteScanner"] = reflect.TypeFor[io.ByteScanner]()
	decs["ByteWriter"] = reflect.TypeFor[io.ByteWriter]()
	decs["Closer"] = reflect.TypeFor[io.Closer]()
	decs["Copy"] = io.Copy
	decs["CopyBuffer"] = io.CopyBuffer
	decs["CopyN"] = io.CopyN
	decs["Discard"] = &io.Discard
	decs["EOF"] = &io.EOF
	decs["ErrClosedPipe"] = &io.ErrClosedPipe
	decs["ErrNoProgress"] = &io.ErrNoProgress
	decs["ErrShortBuffer"] = &io.ErrShortBuffer
	decs["ErrShortWrite"] = &io.ErrShortWrite
	decs["ErrUnexpectedEOF"] = &io.ErrUnexpectedEOF
	decs["LimitReader"] = io.LimitReader
	decs["LimitedReader"] = reflect.TypeFor[io.LimitedReader]()
	decs["MultiReader"] = io.MultiReader
	decs["MultiWriter"] = io.MultiWriter
	decs["NewOffsetWriter"] = io.NewOffsetWriter
	decs["NewSectionReader"] = io.NewSectionReader
	decs["NopCloser"] = io.NopCloser
	decs["OffsetWriter"] = reflect.TypeFor[io.OffsetWriter]()
	decs["Pipe"] = io.Pipe
	decs["PipeReader"] = reflect.TypeFor[io.PipeReader]()
	decs["PipeWriter"] = reflect.TypeFor[io.PipeWriter]()
	decs["ReadAll"] = io.ReadAll
	decs["ReadAtLeast"] = io.ReadAtLeast
	decs["ReadCloser"] = reflect.TypeFor[io.ReadCloser]()
	decs["ReadFull"] = io.ReadFull
	decs["ReadSeekCloser"] = reflect.TypeFor[io.ReadSeekCloser]()
	decs["ReadSeeker"] = reflect.TypeFor[io.ReadSeeker]()
	decs["ReadWriteCloser"] = reflect.TypeFor[io.ReadWriteCloser]()
	decs["ReadWriteSeeker"] = reflect.TypeFor[io.ReadWriteSeeker]()
	decs["ReadWriter"] = reflect.TypeFor[io.ReadWriter]()
	decs["Reader"] = reflect.TypeFor[io.Reader]()
	decs["ReaderAt"] = reflect.TypeFor[io.ReaderAt]()
	decs["ReaderFrom"] = reflect.TypeFor[io.ReaderFrom]()
	decs["RuneReader"] = reflect.TypeFor[io.RuneReader]()
	decs["RuneScanner"] = reflect.TypeFor[io.RuneScanner]()
	decs["SectionReader"] = reflect.TypeFor[io.SectionReader]()
	decs["SeekCurrent"] = native.UntypedNumericConst("1")
	decs["SeekEnd"] = native.UntypedNumericConst("2")
	decs["SeekStart"] = native.UntypedNumericConst("0")
	decs["Seeker"] = reflect.TypeFor[io.Seeker]()
	decs["StringWriter"] = reflect.TypeFor[io.StringWriter]()
	decs["TeeReader"] = io.TeeReader
	decs["WriteCloser"] = reflect.TypeFor[io.WriteCloser]()
	decs["WriteSeeker"] = reflect.TypeFor[io.WriteSeeker]()
	decs["WriteString"] = io.WriteString
	decs["Writer"] = reflect.TypeFor[io.Writer]()
	decs["WriterAt"] = reflect.TypeFor[io.WriterAt]()
	decs["WriterTo"] = reflect.TypeFor[io.WriterTo]()
	packages["io"] = native.Package{
		Name:         "io",
		Declarations: decs,
	}
	// "io/fs"
	decs = make(native.Declarations, 45)
	decs["DirEntry"] = reflect.TypeFor[fs.DirEntry]()
	decs["ErrClosed"] = &fs.ErrClosed
	decs["ErrExist"] = &fs.ErrExist
	decs["ErrInvalid"] = &fs.ErrInvalid
	decs["ErrNotExist"] = &fs.ErrNotExist
	decs["ErrPermission"] = &fs.ErrPermission
	decs["FS"] = reflect.TypeFor[fs.FS]()
	decs["File"] = reflect.TypeFor[fs.File]()
	decs["FileInfo"] = reflect.TypeFor[fs.FileInfo]()
	decs["FileInfoToDirEntry"] = fs.FileInfoToDirEntry
	decs["FileMode"] = reflect.TypeFor[fs.FileMode]()
	decs["FormatDirEntry"] = fs.FormatDirEntry
	decs["FormatFileInfo"] = fs.FormatFileInfo
	decs["Glob"] = fs.Glob
	decs["GlobFS"] = reflect.TypeFor[fs.GlobFS]()
	decs["ModeAppend"] = fs.ModeAppend
	decs["ModeCharDevice"] = fs.ModeCharDevice
	decs["ModeDevice"] = fs.ModeDevice
	decs["ModeDir"] = fs.ModeDir
	decs["ModeExclusive"] = fs.ModeExclusive
	decs["ModeIrregular"] = fs.ModeIrregular
	decs["ModeNamedPipe"] = fs.ModeNamedPipe
	decs["ModePerm"] = fs.ModePerm
	decs["ModeSetgid"] = fs.ModeSetgid
	decs["ModeSetuid"] = fs.ModeSetuid
	decs["ModeSocket"] = fs.ModeSocket
	decs["ModeSticky"] = fs.ModeSticky
	decs["ModeSymlink"] = fs.ModeSymlink
	decs["ModeTemporary"] = fs.ModeTemporary
	decs["ModeType"] = fs.ModeType
	decs["PathError"] = reflect.TypeFor[fs.PathError]()
	decs["ReadDir"] = fs.ReadDir
	decs["ReadDirFS"] = reflect.TypeFor[fs.ReadDirFS]()
	decs["ReadDirFile"] = reflect.TypeFor[fs.ReadDirFile]()
	decs["ReadFile"] = fs.ReadFile
	decs["ReadFileFS"] = reflect.TypeFor[fs.ReadFileFS]()
	decs["SkipAll"] = &fs.SkipAll
	decs["SkipDir"] = &fs.SkipDir
	decs["Stat"] = fs.Stat
	decs["StatFS"] = reflect.TypeFor[fs.StatFS]()
	decs["Sub"] = fs.Sub
	decs["SubFS"] = reflect.TypeFor[fs.SubFS]()
	decs["ValidPath"] = fs.ValidPath
	decs["WalkDir"] = fs.WalkDir
	decs["WalkDirFunc"] = reflect.TypeFor[fs.WalkDirFunc]()
	packages["io/fs"] = native.Package{
		Name:         "fs",
		Declarations: decs,
	}
	// "io/ioutil"
	decs = make(native.Declarations, 8)
	decs["Discard"] = &ioutil.Discard
	decs["NopCloser"] = ioutil.NopCloser
	decs["ReadAll"] = ioutil.ReadAll
	decs["ReadDir"] = ioutil.ReadDir
	decs["ReadFile"] = ioutil.ReadFile
	decs["TempDir"] = ioutil.TempDir
	decs["TempFile"] = ioutil.TempFile
	decs["WriteFile"] = ioutil.WriteFile
	packages["io/ioutil"] = native.Package{
		Name:         "ioutil",
		Declarations: decs,
	}
	// "log"
	decs = make(native.Declarations, 27)
	decs["Default"] = log.Default
	decs["Fatal"] = log.Fatal
	decs["Fatalf"] = log.Fatalf
	decs["Fatalln"] = log.Fatalln
	decs["Flags"] = log.Flags
	decs["LUTC"] = native.UntypedNumericConst("32")
	decs["Ldate"] = native.UntypedNumericConst("1")
	decs["Llongfile"] = native.UntypedNumericConst("8")
	decs["Lmicroseconds"] = native.UntypedNumericConst("4")
	decs["Lmsgprefix"] = native.UntypedNumericConst("64")
	decs["Logger"] = reflect.TypeFor[log.Logger]()
	decs["Lshortfile"] = native.UntypedNumericConst("16")
	decs["LstdFlags"] = native.UntypedNumericConst("3")
	decs["Ltime"] = native.UntypedNumericConst("2")
	decs["New"] = log.New
	decs["Output"] = log.Output
	decs["Panic"] = log.Panic
	decs["Panicf"] = log.Panicf
	decs["Panicln"] = log.Panicln
	decs["Prefix"] = log.Prefix
	decs["Print"] = log.Print
	decs["Printf"] = log.Printf
	decs["Println"] = log.Println
	decs["SetFlags"] = log.SetFlags
	decs["SetOutput"] = log.SetOutput
	decs["SetPrefix"] = log.SetPrefix
	decs["Writer"] = log.Writer
	packages["log"] = native.Package{
		Name:         "log",
		Declarations: decs,
	}
	// "math"
	decs = make(native.Declarations, 97)
	decs["Abs"] = math.Abs
	decs["Acos"] = math.Acos
	decs["Acosh"] = math.Acosh
	decs["Asin"] = math.Asin
	decs["Asinh"] = math.Asinh
	decs["Atan"] = math.Atan
	decs["Atan2"] = math.Atan2
	decs["Atanh"] = math.Atanh
	decs["Cbrt"] = math.Cbrt
	decs["Ceil"] = math.Ceil
	decs["Copysign"] = math.Copysign
	decs["Cos"] = math.Cos
	decs["Cosh"] = math.Cosh
	decs["Dim"] = math.Dim
	decs["E"] = native.UntypedNumericConst("2.71828182845904523536028747135266249775724709369995957496696763")
	decs["Erf"] = math.Erf
	decs["Erfc"] = math.Erfc
	decs["Erfcinv"] = math.Erfcinv
	decs["Erfinv"] = math.Erfinv
	decs["Exp"] = math.Exp
	decs["Exp2"] = math.Exp2
	decs["Expm1"] = math.Expm1
	decs["FMA"] = math.FMA
	decs["Float32bits"] = math.Float32bits
	decs["Float32frombits"] = math.Float32frombits
	decs["Float64bits"] = math.Float64bits
	decs["Float64frombits"] = math.Float64frombits
	decs["Floor"] = math.Floor
	decs["Frexp"] = math.Frexp
	decs["Gamma"] = math.Gamma
	decs["Hypot"] = math.Hypot
	decs["Ilogb"] = math.Ilogb
	decs["Inf"] = math.Inf
	decs["IsInf"] = math.IsInf
	decs["IsNaN"] = math.IsNaN
	decs["J0"] = math.J0
	decs["J1"] = math.J1
	decs["Jn"] = math.Jn
	decs["Ldexp"] = math.Ldexp
	decs["Lgamma"] = math.Lgamma
	decs["Ln10"] = native.UntypedNumericConst("2.3025850929940456840179914546843642076011014886287729760333279")
	decs["Ln2"] = native.UntypedNumericConst("0.693147180559945309417232121458176568075500134360255254120680009")
	decs["Log"] = math.Log
	decs["Log10"] = math.Log10
	decs["Log10E"] = native.UntypedNumericConst("10000000000000000000000000000000000000000000000000000000000000/23025850929940456840179914546843642076011014886287729760333279")
	decs["Log1p"] = math.Log1p
	decs["Log2"] = math.Log2
	decs["Log2E"] = native.UntypedNumericConst("1000000000000000000000000000000000000000000000000000000000000000/693147180559945309417232121458176568075500134360255254120680009")
	decs["Logb"] = math.Logb
	decs["Max"] = math.Max
	decs["MaxFloat32"] = native.UntypedNumericConst("340282346638528859811704183484516925440.0")
	decs["MaxFloat64"] = native.UntypedNumericConst("179769313486231570814527423731704356798070567525844996598917476803157260780028538760589558632766878171540458953514382464234321326889464182768467546703537516986049910576551282076245490090389328944075868508455133942304583236903222948165808559332123348274797826204144723168738177180919299881250404026184124858368.0")
	decs["MaxInt"] = native.UntypedNumericConst("9223372036854775807")
	decs["MaxInt16"] = native.UntypedNumericConst("32767")
	decs["MaxInt32"] = native.UntypedNumericConst("2147483647")
	decs["MaxInt64"] = native.UntypedNumericConst("9223372036854775807")
	decs["MaxInt8"] = native.UntypedNumericConst("127")
	decs["MaxUint"] = native.UntypedNumericConst("18446744073709551615")
	decs["MaxUint16"] = native.UntypedNumericConst("65535")
	decs["MaxUint32"] = native.UntypedNumericConst("4294967295")
	decs["MaxUint64"] = native.UntypedNumericConst("18446744073709551615")
	decs["MaxUint8"] = native.UntypedNumericConst("255")
	decs["Min"] = math.Min
	decs["MinInt"] = native.UntypedNumericConst("-9223372036854775808")
	decs["MinInt16"] = native.UntypedNumericConst("-32768")
	decs["MinInt32"] = native.UntypedNumericConst("-2147483648")
	decs["MinInt64"] = native.UntypedNumericConst("-9223372036854775808")
	decs["MinInt8"] = native.UntypedNumericConst("-128")
	decs["Mod"] = math.Mod
	decs["Modf"] = math.Modf
	decs["NaN"] = math.NaN
	decs["Nextafter"] = math.Nextafter
	decs["Nextafter32"] = math.Nextafter32
	decs["Phi"] = native.UntypedNumericConst("1.61803398874989484820458683436563811772030917980576286213544862")
	decs["Pi"] = native.UntypedNumericConst("3.14159265358979323846264338327950288419716939937510582097494459")
	decs["Pow"] = math.Pow
	decs["Pow10"] = math.Pow10
	decs["Remainder"] = math.Remainder
	decs["Round"] = math.Round
	decs["RoundToEven"] = math.RoundToEven
	decs["Signbit"] = math.Signbit
	decs["Sin"] = math.Sin
	decs["Sincos"] = math.Sincos
	decs["Sinh"] = math.Sinh
	decs["SmallestNonzeroFloat32"] = native.UntypedNumericConst("1.40129846432481707092372958328991613128026194187651577175706828388979108268586060148663818836212158203125e-45")
	decs["SmallestNonzeroFloat64"] = native.UntypedNumericConst("1/202402253307310618352495346718917307049556649764142118356901358027430339567995346891960383701437124495187077864316811911389808737385793476867013399940738509921517424276566361364466907742093216341239767678472745068562007483424692698618103355649159556340810056512358769552333414615230502532186327508646006263307707741093494784")
	decs["Sqrt"] = math.Sqrt
	decs["Sqrt2"] = native.UntypedNumericConst("1.41421356237309504880168872420969807856967187537694807317667974")
	decs["SqrtE"] = native.UntypedNumericConst("1.64872127070012814684865078781416357165377610071014801157507931")
	decs["SqrtPhi"] = native.UntypedNumericConst("1.27201964951406896425242246173749149171560804184009624861664038")
	decs["SqrtPi"] = native.UntypedNumericConst("1.77245385090551602729816748334114518279754945612238712821380779")
	decs["Tan"] = math.Tan
	decs["Tanh"] = math.Tanh
	decs["Trunc"] = math.Trunc
	decs["Y0"] = math.Y0
	decs["Y1"] = math.Y1
	decs["Yn"] = math.Yn
	packages["math"] = native.Package{
		Name:         "math",
		Declarations: decs,
	}
	// "math/big"
	decs = make(native.Declarations, 25)
	decs["Above"] = big.Above
	decs["Accuracy"] = reflect.TypeFor[big.Accuracy]()
	decs["AwayFromZero"] = big.AwayFromZero
	decs["Below"] = big.Below
	decs["ErrNaN"] = reflect.TypeFor[big.ErrNaN]()
	decs["Exact"] = big.Exact
	decs["Float"] = reflect.TypeFor[big.Float]()
	decs["Int"] = reflect.TypeFor[big.Int]()
	decs["Jacobi"] = big.Jacobi
	decs["MaxBase"] = native.UntypedNumericConst("62")
	decs["MaxExp"] = native.UntypedNumericConst("2147483647")
	decs["MaxPrec"] = native.UntypedNumericConst("4294967295")
	decs["MinExp"] = native.UntypedNumericConst("-2147483648")
	decs["NewFloat"] = big.NewFloat
	decs["NewInt"] = big.NewInt
	decs["NewRat"] = big.NewRat
	decs["ParseFloat"] = big.ParseFloat
	decs["Rat"] = reflect.TypeFor[big.Rat]()
	decs["RoundingMode"] = reflect.TypeFor[big.RoundingMode]()
	decs["ToNearestAway"] = big.ToNearestAway
	decs["ToNearestEven"] = big.ToNearestEven
	decs["ToNegativeInf"] = big.ToNegativeInf
	decs["ToPositiveInf"] = big.ToPositiveInf
	decs["ToZero"] = big.ToZero
	decs["Word"] = reflect.TypeFor[big.Word]()
	packages["math/big"] = native.Package{
		Name:         "big",
		Declarations: decs,
	}
	// "math/bits"
	decs = make(native.Declarations, 50)
	decs["Add"] = bits.Add
	decs["Add32"] = bits.Add32
	decs["Add64"] = bits.Add64
	decs["Div"] = bits.Div
	decs["Div32"] = bits.Div32
	decs["Div64"] = bits.Div64
	decs["LeadingZeros"] = bits.LeadingZeros
	decs["LeadingZeros16"] = bits.LeadingZeros16
	decs["LeadingZeros32"] = bits.LeadingZeros32
	decs["LeadingZeros64"] = bits.LeadingZeros64
	decs["LeadingZeros8"] = bits.LeadingZeros8
	decs["Len"] = bits.Len
	decs["Len16"] = bits.Len16
	decs["Len32"] = bits.Len32
	decs["Len64"] = bits.Len64
	decs["Len8"] = bits.Len8
	decs["Mul"] = bits.Mul
	decs["Mul32"] = bits.Mul32
	decs["Mul64"] = bits.Mul64
	decs["OnesCount"] = bits.OnesCount
	decs["OnesCount16"] = bits.OnesCount16
	decs["OnesCount32"] = bits.OnesCount32
	decs["OnesCount64"] = bits.OnesCount64
	decs["OnesCount8"] = bits.OnesCount8
	decs["Rem"] = bits.Rem
	decs["Rem32"] = bits.Rem32
	decs["Rem64"] = bits.Rem64
	decs["Reverse"] = bits.Reverse
	decs["Reverse16"] = bits.Reverse16
	decs["Reverse32"] = bits.Reverse32
	decs["Reverse64"] = bits.Reverse64
	decs["Reverse8"] = bits.Reverse8
	decs["ReverseBytes"] = bits.ReverseBytes
	decs["ReverseBytes16"] = bits.ReverseBytes16
	decs["ReverseBytes32"] = bits.ReverseBytes32
	decs["ReverseBytes64"] = bits.ReverseBytes64
	decs["RotateLeft"] = bits.RotateLeft
	decs["RotateLeft16"] = bits.RotateLeft16
	decs["RotateLeft32"] = bits.RotateLeft32
	decs["RotateLeft64"] = bits.RotateLeft64
	decs["RotateLeft8"] = bits.RotateLeft8
	decs["Sub"] = bits.Sub
	decs["Sub32"] = bits.Sub32
	decs["Sub64"] = bits.Sub64
	decs["TrailingZeros"] = bits.TrailingZeros
	decs["TrailingZeros16"] = bits.TrailingZeros16
	decs["TrailingZeros32"] = bits.TrailingZeros32
	decs["TrailingZeros64"] = bits.TrailingZeros64
	decs["TrailingZeros8"] = bits.TrailingZeros8
	decs["UintSize"] = native.UntypedNumericConst("64")
	packages["math/bits"] = native.Package{
		Name:         "bits",
		Declarations: decs,
	}
	// "math/cmplx"
	decs = make(native.Declarations, 27)
	decs["Abs"] = cmplx.Abs
	decs["Acos"] = cmplx.Acos
	decs["Acosh"] = cmplx.Acosh
	decs["Asin"] = cmplx.Asin
	decs["Asinh"] = cmplx.Asinh
	decs["Atan"] = cmplx.Atan
	decs["Atanh"] = cmplx.Atanh
	decs["Conj"] = cmplx.Conj
	decs["Cos"] = cmplx.Cos
	decs["Cosh"] = cmplx.Cosh
	decs["Cot"] = cmplx.Cot
	decs["Exp"] = cmplx.Exp
	decs["Inf"] = cmplx.Inf
	decs["IsInf"] = cmplx.IsInf
	decs["IsNaN"] = cmplx.IsNaN
	decs["Log"] = cmplx.Log
	decs["Log10"] = cmplx.Log10
	decs["NaN"] = cmplx.NaN
	decs["Phase"] = cmplx.Phase
	decs["Polar"] = cmplx.Polar
	decs["Pow"] = cmplx.Pow
	decs["Rect"] = cmplx.Rect
	decs["Sin"] = cmplx.Sin
	decs["Sinh"] = cmplx.Sinh
	decs["Sqrt"] = cmplx.Sqrt
	decs["Tan"] = cmplx.Tan
	decs["Tanh"] = cmplx.Tanh
	packages["math/cmplx"] = native.Package{
		Name:         "cmplx",
		Declarations: decs,
	}
	// "math/rand"
	decs = make(native.Declarations, 23)
	decs["ExpFloat64"] = rand_2.ExpFloat64
	decs["Float32"] = rand_2.Float32
	decs["Float64"] = rand_2.Float64
	decs["Int"] = rand_2.Int
	decs["Int31"] = rand_2.Int31
	decs["Int31n"] = rand_2.Int31n
	decs["Int63"] = rand_2.Int63
	decs["Int63n"] = rand_2.Int63n
	decs["Intn"] = rand_2.Intn
	decs["New"] = rand_2.New
	decs["NewSource"] = rand_2.NewSource
	decs["NewZipf"] = rand_2.NewZipf
	decs["NormFloat64"] = rand_2.NormFloat64
	decs["Perm"] = rand_2.Perm
	decs["Rand"] = reflect.TypeFor[rand_2.Rand]()
	decs["Read"] = rand_2.Read
	decs["Seed"] = rand_2.Seed
	decs["Shuffle"] = rand_2.Shuffle
	decs["Source"] = reflect.TypeFor[rand_2.Source]()
	decs["Source64"] = reflect.TypeFor[rand_2.Source64]()
	decs["Uint32"] = rand_2.Uint32
	decs["Uint64"] = rand_2.Uint64
	decs["Zipf"] = reflect.TypeFor[rand_2.Zipf]()
	packages["math/rand"] = native.Package{
		Name:         "rand",
		Declarations: decs,
	}
	// "mime"
	decs = make(native.Declarations, 10)
	decs["AddExtensionType"] = mime.AddExtensionType
	decs["BEncoding"] = mime.BEncoding
	decs["ErrInvalidMediaParameter"] = &mime.ErrInvalidMediaParameter
	decs["ExtensionsByType"] = mime.ExtensionsByType
	decs["FormatMediaType"] = mime.FormatMediaType
	decs["ParseMediaType"] = mime.ParseMediaType
	decs["QEncoding"] = mime.QEncoding
	decs["TypeByExtension"] = mime.TypeByExtension
	decs["WordDecoder"] = reflect.TypeFor[mime.WordDecoder]()
	decs["WordEncoder"] = reflect.TypeFor[mime.WordEncoder]()
	packages["mime"] = native.Package{
		Name:         "mime",
		Declarations: decs,
	}
	// "mime/multipart"
	decs = make(native.Declarations, 9)
	decs["ErrMessageTooLarge"] = &multipart.ErrMessageTooLarge
	decs["File"] = reflect.TypeFor[multipart.File]()
	decs["FileHeader"] = reflect.TypeFor[multipart.FileHeader]()
	decs["Form"] = reflect.TypeFor[multipart.Form]()
	decs["NewReader"] = multipart.NewReader
	decs["NewWriter"] = multipart.NewWriter
	decs["Part"] = reflect.TypeFor[multipart.Part]()
	decs["Reader"] = reflect.TypeFor[multipart.Reader]()
	decs["Writer"] = reflect.TypeFor[multipart.Writer]()
	packages["mime/multipart"] = native.Package{
		Name:         "multipart",
		Declarations: decs,
	}
	// "mime/quotedprintable"
	decs = make(native.Declarations, 4)
	decs["NewReader"] = quotedprintable.NewReader
	decs["NewWriter"] = quotedprintable.NewWriter
	decs["Reader"] = reflect.TypeFor[quotedprintable.Reader]()
	decs["Writer"] = reflect.TypeFor[quotedprintable.Writer]()
	packages["mime/quotedprintable"] = native.Package{
		Name:         "quotedprintable",
		Declarations: decs,
	}
	// "net"
	decs = make(native.Declarations, 101)
	decs["Addr"] = reflect.TypeFor[net.Addr]()
	decs["AddrError"] = reflect.TypeFor[net.AddrError]()
	decs["Buffers"] = reflect.TypeFor[net.Buffers]()
	decs["CIDRMask"] = net.CIDRMask
	decs["Conn"] = reflect.TypeFor[net.Conn]()
	decs["DNSConfigError"] = reflect.TypeFor[net.DNSConfigError]()
	decs["DNSError"] = reflect.TypeFor[net.DNSError]()
	decs["DefaultResolver"] = &net.DefaultResolver
	decs["Dial"] = net.Dial
	decs["DialIP"] = net.DialIP
	decs["DialTCP"] = net.DialTCP
	decs["DialTimeout"] = net.DialTimeout
	decs["DialUDP"] = net.DialUDP
	decs["DialUnix"] = net.DialUnix
	decs["Dialer"] = reflect.TypeFor[net.Dialer]()
	decs["ErrClosed"] = &net.ErrClosed
	decs["ErrWriteToConnected"] = &net.ErrWriteToConnected
	decs["Error"] = reflect.TypeFor[net.Error]()
	decs["FileConn"] = net.FileConn
	decs["FileListener"] = net.FileListener
	decs["FilePacketConn"] = net.FilePacketConn
	decs["FlagBroadcast"] = net.FlagBroadcast
	decs["FlagLoopback"] = net.FlagLoopback
	decs["FlagMulticast"] = net.FlagMulticast
	decs["FlagPointToPoint"] = net.FlagPointToPoint
	decs["FlagRunning"] = net.FlagRunning
	decs["FlagUp"] = net.FlagUp
	decs["Flags"] = reflect.TypeFor[net.Flags]()
	decs["HardwareAddr"] = reflect.TypeFor[net.HardwareAddr]()
	decs["IP"] = reflect.TypeFor[net.IP]()
	decs["IPAddr"] = reflect.TypeFor[net.IPAddr]()
	decs["IPConn"] = reflect.TypeFor[net.IPConn]()
	decs["IPMask"] = reflect.TypeFor[net.IPMask]()
	decs["IPNet"] = reflect.TypeFor[net.IPNet]()
	decs["IPv4"] = net.IPv4
	decs["IPv4Mask"] = net.IPv4Mask
	decs["IPv4allrouter"] = &net.IPv4allrouter
	decs["IPv4allsys"] = &net.IPv4allsys
	decs["IPv4bcast"] = &net.IPv4bcast
	decs["IPv4len"] = native.UntypedNumericConst("4")
	decs["IPv4zero"] = &net.IPv4zero
	decs["IPv6interfacelocalallnodes"] = &net.IPv6interfacelocalallnodes
	decs["IPv6len"] = native.UntypedNumericConst("16")
	decs["IPv6linklocalallnodes"] = &net.IPv6linklocalallnodes
	decs["IPv6linklocalallrouters"] = &net.IPv6linklocalallrouters
	decs["IPv6loopback"] = &net.IPv6loopback
	decs["IPv6unspecified"] = &net.IPv6unspecified
	decs["IPv6zero"] = &net.IPv6zero
	decs["Interface"] = reflect.TypeFor[net.Interface]()
	decs["InterfaceAddrs"] = net.InterfaceAddrs
	decs["InterfaceByIndex"] = net.InterfaceByIndex
	decs["InterfaceByName"] = net.InterfaceByName
	decs["Interfaces"] = net.Interfaces
	decs["InvalidAddrError"] = reflect.TypeFor[net.InvalidAddrError]()
	decs["JoinHostPort"] = net.JoinHostPort
	decs["Listen"] = net.Listen
	decs["ListenConfig"] = reflect.TypeFor[net.ListenConfig]()
	decs["ListenIP"] = net.ListenIP
	decs["ListenMulticastUDP"] = net.ListenMulticastUDP
	decs["ListenPacket"] = net.ListenPacket
	decs["ListenTCP"] = net.ListenTCP
	decs["ListenUDP"] = net.ListenUDP
	decs["ListenUnix"] = net.ListenUnix
	decs["ListenUnixgram"] = net.ListenUnixgram
	decs["Listener"] = reflect.TypeFor[net.Listener]()
	decs["LookupAddr"] = net.LookupAddr
	decs["LookupCNAME"] = net.LookupCNAME
	decs["LookupHost"] = net.LookupHost
	decs["LookupIP"] = net.LookupIP
	decs["LookupMX"] = net.LookupMX
	decs["LookupNS"] = net.LookupNS
	decs["LookupPort"] = net.LookupPort
	decs["LookupSRV"] = net.LookupSRV
	decs["LookupTXT"] = net.LookupTXT
	decs["MX"] = reflect.TypeFor[net.MX]()
	decs["NS"] = reflect.TypeFor[net.NS]()
	decs["OpError"] = reflect.TypeFor[net.OpError]()
	decs["PacketConn"] = reflect.TypeFor[net.PacketConn]()
	decs["ParseCIDR"] = net.ParseCIDR
	decs["ParseError"] = reflect.TypeFor[net.ParseError]()
	decs["ParseIP"] = net.ParseIP
	decs["ParseMAC"] = net.ParseMAC
	decs["Pipe"] = net.Pipe
	decs["ResolveIPAddr"] = net.ResolveIPAddr
	decs["ResolveTCPAddr"] = net.ResolveTCPAddr
	decs["ResolveUDPAddr"] = net.ResolveUDPAddr
	decs["ResolveUnixAddr"] = net.ResolveUnixAddr
	decs["Resolver"] = reflect.TypeFor[net.Resolver]()
	decs["SRV"] = reflect.TypeFor[net.SRV]()
	decs["SplitHostPort"] = net.SplitHostPort
	decs["TCPAddr"] = reflect.TypeFor[net.TCPAddr]()
	decs["TCPAddrFromAddrPort"] = net.TCPAddrFromAddrPort
	decs["TCPConn"] = reflect.TypeFor[net.TCPConn]()
	decs["TCPListener"] = reflect.TypeFor[net.TCPListener]()
	decs["UDPAddr"] = reflect.TypeFor[net.UDPAddr]()
	decs["UDPAddrFromAddrPort"] = net.UDPAddrFromAddrPort
	decs["UDPConn"] = reflect.TypeFor[net.UDPConn]()
	decs["UnixAddr"] = reflect.TypeFor[net.UnixAddr]()
	decs["UnixConn"] = reflect.TypeFor[net.UnixConn]()
	decs["UnixListener"] = reflect.TypeFor[net.UnixListener]()
	decs["UnknownNetworkError"] = reflect.TypeFor[net.UnknownNetworkError]()
	packages["net"] = native.Package{
		Name:         "net",
		Declarations: decs,
	}
	// "net/http"
	decs = make(native.Declarations, 177)
	decs["AllowQuerySemicolons"] = http.AllowQuerySemicolons
	decs["CanonicalHeaderKey"] = http.CanonicalHeaderKey
	decs["Client"] = reflect.TypeFor[http.Client]()
	decs["CloseNotifier"] = reflect.TypeFor[http.CloseNotifier]()
	decs["ConnState"] = reflect.TypeFor[http.ConnState]()
	decs["Cookie"] = reflect.TypeFor[http.Cookie]()
	decs["CookieJar"] = reflect.TypeFor[http.CookieJar]()
	decs["DefaultClient"] = &http.DefaultClient
	decs["DefaultMaxHeaderBytes"] = native.UntypedNumericConst("1048576")
	decs["DefaultMaxIdleConnsPerHost"] = native.UntypedNumericConst("2")
	decs["DefaultServeMux"] = &http.DefaultServeMux
	decs["DefaultTransport"] = &http.DefaultTransport
	decs["DetectContentType"] = http.DetectContentType
	decs["Dir"] = reflect.TypeFor[http.Dir]()
	decs["ErrAbortHandler"] = &http.ErrAbortHandler
	decs["ErrBodyNotAllowed"] = &http.ErrBodyNotAllowed
	decs["ErrBodyReadAfterClose"] = &http.ErrBodyReadAfterClose
	decs["ErrContentLength"] = &http.ErrContentLength
	decs["ErrHandlerTimeout"] = &http.ErrHandlerTimeout
	decs["ErrHeaderTooLong"] = &http.ErrHeaderTooLong
	decs["ErrHijacked"] = &http.ErrHijacked
	decs["ErrLineTooLong"] = &http.ErrLineTooLong
	decs["ErrMissingBoundary"] = &http.ErrMissingBoundary
	decs["ErrMissingContentLength"] = &http.ErrMissingContentLength
	decs["ErrMissingFile"] = &http.ErrMissingFile
	decs["ErrNoCookie"] = &http.ErrNoCookie
	decs["ErrNoLocation"] = &http.ErrNoLocation
	decs["ErrNotMultipart"] = &http.ErrNotMultipart
	decs["ErrNotSupported"] = &http.ErrNotSupported
	decs["ErrSchemeMismatch"] = &http.ErrSchemeMismatch
	decs["ErrServerClosed"] = &http.ErrServerClosed
	decs["ErrShortBody"] = &http.ErrShortBody
	decs["ErrSkipAltProtocol"] = &http.ErrSkipAltProtocol
	decs["ErrUnexpectedTrailer"] = &http.ErrUnexpectedTrailer
	decs["ErrUseLastResponse"] = &http.ErrUseLastResponse
	decs["ErrWriteAfterFlush"] = &http.ErrWriteAfterFlush
	decs["Error"] = http.Error
	decs["FS"] = http.FS
	decs["File"] = reflect.TypeFor[http.File]()
	decs["FileServer"] = http.FileServer
	decs["FileSystem"] = reflect.TypeFor[http.FileSystem]()
	decs["Flusher"] = reflect.TypeFor[http.Flusher]()
	decs["Get"] = http.Get
	decs["Handle"] = http.Handle
	decs["HandleFunc"] = http.HandleFunc
	decs["Handler"] = reflect.TypeFor[http.Handler]()
	decs["HandlerFunc"] = reflect.TypeFor[http.HandlerFunc]()
	decs["Head"] = http.Head
	decs["Header"] = reflect.TypeFor[http.Header]()
	decs["Hijacker"] = reflect.TypeFor[http.Hijacker]()
	decs["ListenAndServe"] = http.ListenAndServe
	decs["ListenAndServeTLS"] = http.ListenAndServeTLS
	decs["LocalAddrContextKey"] = &http.LocalAddrContextKey
	decs["MaxBytesError"] = reflect.TypeFor[http.MaxBytesError]()
	decs["MaxBytesHandler"] = http.MaxBytesHandler
	decs["MaxBytesReader"] = http.MaxBytesReader
	decs["MethodConnect"] = native.UntypedStringConst("CONNECT")
	decs["MethodDelete"] = native.UntypedStringConst("DELETE")
	decs["MethodGet"] = native.UntypedStringConst("GET")
	decs["MethodHead"] = native.UntypedStringConst("HEAD")
	decs["MethodOptions"] = native.UntypedStringConst("OPTIONS")
	decs["MethodPatch"] = native.UntypedStringConst("PATCH")
	decs["MethodPost"] = native.UntypedStringConst("POST")
	decs["MethodPut"] = native.UntypedStringConst("PUT")
	decs["MethodTrace"] = native.UntypedStringConst("TRACE")
	decs["NewFileTransport"] = http.NewFileTransport
	decs["NewRequest"] = http.NewRequest
	decs["NewRequestWithContext"] = http.NewRequestWithContext
	decs["NewResponseController"] = http.NewResponseController
	decs["NewServeMux"] = http.NewServeMux
	decs["NoBody"] = &http.NoBody
	decs["NotFound"] = http.NotFound
	decs["NotFoundHandler"] = http.NotFoundHandler
	decs["ParseHTTPVersion"] = http.ParseHTTPVersion
	decs["ParseTime"] = http.ParseTime
	decs["Post"] = http.Post
	decs["PostForm"] = http.PostForm
	decs["ProtocolError"] = reflect.TypeFor[http.ProtocolError]()
	decs["ProxyFromEnvironment"] = http.ProxyFromEnvironment
	decs["ProxyURL"] = http.ProxyURL
	decs["PushOptions"] = reflect.TypeFor[http.PushOptions]()
	decs["Pusher"] = reflect.TypeFor[http.Pusher]()
	decs["ReadRequest"] = http.ReadRequest
	decs["ReadResponse"] = http.ReadResponse
	decs["Redirect"] = http.Redirect
	decs["RedirectHandler"] = http.RedirectHandler
	decs["Request"] = reflect.TypeFor[http.Request]()
	decs["Response"] = reflect.TypeFor[http.Response]()
	decs["ResponseController"] = reflect.TypeFor[http.ResponseController]()
	decs["ResponseWriter"] = reflect.TypeFor[http.ResponseWriter]()
	decs["RoundTripper"] = reflect.TypeFor[http.RoundTripper]()
	decs["SameSite"] = reflect.TypeFor[http.SameSite]()
	decs["SameSiteDefaultMode"] = http.SameSiteDefaultMode
	decs["SameSiteLaxMode"] = http.SameSiteLaxMode
	decs["SameSiteNoneMode"] = http.SameSiteNoneMode
	decs["SameSiteStrictMode"] = http.SameSiteStrictMode
	decs["Serve"] = http.Serve
	decs["ServeContent"] = http.ServeContent
	decs["ServeFile"] = http.ServeFile
	decs["ServeMux"] = reflect.TypeFor[http.ServeMux]()
	decs["ServeTLS"] = http.ServeTLS
	decs["Server"] = reflect.TypeFor[http.Server]()
	decs["ServerContextKey"] = &http.ServerContextKey
	decs["SetCookie"] = http.SetCookie
	decs["StateActive"] = http.StateActive
	decs["StateClosed"] = http.StateClosed
	decs["StateHijacked"] = http.StateHijacked
	decs["StateIdle"] = http.StateIdle
	decs["StateNew"] = http.StateNew
	decs["StatusAccepted"] = native.UntypedNumericConst("202")
	decs["StatusAlreadyReported"] = native.UntypedNumericConst("208")
	decs["StatusBadGateway"] = native.UntypedNumericConst("502")
	decs["StatusBadRequest"] = native.UntypedNumericConst("400")
	decs["StatusConflict"] = native.UntypedNumericConst("409")
	decs["StatusContinue"] = native.UntypedNumericConst("100")
	decs["StatusCreated"] = native.UntypedNumericConst("201")
	decs["StatusEarlyHints"] = native.UntypedNumericConst("103")
	decs["StatusExpectationFailed"] = native.UntypedNumericConst("417")
	decs["StatusFailedDependency"] = native.UntypedNumericConst("424")
	decs["StatusForbidden"] = native.UntypedNumericConst("403")
	decs["StatusFound"] = native.UntypedNumericConst("302")
	decs["StatusGatewayTimeout"] = native.UntypedNumericConst("504")
	decs["StatusGone"] = native.UntypedNumericConst("410")
	decs["StatusHTTPVersionNotSupported"] = native.UntypedNumericConst("505")
	decs["StatusIMUsed"] = native.UntypedNumericConst("226")
	decs["StatusInsufficientStorage"] = native.UntypedNumericConst("507")
	decs["StatusInternalServerError"] = native.UntypedNumericConst("500")
	decs["StatusLengthRequired"] = native.UntypedNumericConst("411")
	decs["StatusLocked"] = native.UntypedNumericConst("423")
	decs["StatusLoopDetected"] = native.UntypedNumericConst("508")
	decs["StatusMethodNotAllowed"] = native.UntypedNumericConst("405")
	decs["StatusMisdirectedRequest"] = native.UntypedNumericConst("421")
	decs["StatusMovedPermanently"] = native.UntypedNumericConst("301")
	decs["StatusMultiStatus"] = native.UntypedNumericConst("207")
	decs["StatusMultipleChoices"] = native.UntypedNumericConst("300")
	decs["StatusNetworkAuthenticationRequired"] = native.UntypedNumericConst("511")
	decs["StatusNoContent"] = native.UntypedNumericConst("204")
	decs["StatusNonAuthoritativeInfo"] = native.UntypedNumericConst("203")
	decs["StatusNotAcceptable"] = native.UntypedNumericConst("406")
	decs["StatusNotExtended"] = native.UntypedNumericConst("510")
	decs["StatusNotFound"] = native.UntypedNumericConst("404")
	decs["StatusNotImplemented"] = native.UntypedNumericConst("501")
	decs["StatusNotModified"] = native.UntypedNumericConst("304")
	decs["StatusOK"] = native.UntypedNumericConst("200")
	decs["StatusPartialContent"] = native.UntypedNumericConst("206")
	decs["StatusPaymentRequired"] = native.UntypedNumericConst("402")
	decs["StatusPermanentRedirect"] = native.UntypedNumericConst("308")
	decs["StatusPreconditionFailed"] = native.UntypedNumericConst("412")
	decs["StatusPreconditionRequired"] = native.UntypedNumericConst("428")
	decs["StatusProcessing"] = native.UntypedNumericConst("102")
	decs["StatusProxyAuthRequired"] = native.UntypedNumericConst("407")
	decs["StatusRequestEntityTooLarge"] = native.UntypedNumericConst("413")
	decs["StatusRequestHeaderFieldsTooLarge"] = native.UntypedNumericConst("431")
	decs["StatusRequestTimeout"] = native.UntypedNumericConst("408")
	decs["StatusRequestURITooLong"] = native.UntypedNumericConst("414")
	decs["StatusRequestedRangeNotSatisfiable"] = native.UntypedNumericConst("416")
	decs["StatusResetContent"] = native.UntypedNumericConst("205")
	decs["StatusSeeOther"] = native.UntypedNumericConst("303")
	decs["StatusServiceUnavailable"] = native.UntypedNumericConst("503")
	decs["StatusSwitchingProtocols"] = native.UntypedNumericConst("101")
	decs["StatusTeapot"] = native.UntypedNumericConst("418")
	decs["StatusTemporaryRedirect"] = native.UntypedNumericConst("307")
	decs["StatusText"] = http.StatusText
	decs["StatusTooEarly"] = native.UntypedNumericConst("425")
	decs["StatusTooManyRequests"] = native.UntypedNumericConst("429")
	decs["StatusUnauthorized"] = native.UntypedNumericConst("401")
	decs["StatusUnavailableForLegalReasons"] = native.UntypedNumericConst("451")
	decs["StatusUnprocessableEntity"] = native.UntypedNumericConst("422")
	decs["StatusUnsupportedMediaType"] = native.UntypedNumericConst("415")
	decs["StatusUpgradeRequired"] = native.UntypedNumericConst("426")
	decs["StatusUseProxy"] = native.UntypedNumericConst("305")
	decs["StatusVariantAlsoNegotiates"] = native.UntypedNumericConst("506")
	decs["StripPrefix"] = http.StripPrefix
	decs["TimeFormat"] = native.UntypedStringConst("Mon, 02 Jan 2006 15:04:05 GMT")
	decs["TimeoutHandler"] = http.TimeoutHandler
	decs["TrailerPrefix"] = native.UntypedStringConst("Trailer:")
	decs["Transport"] = reflect.TypeFor[http.Transport]()
	packages["net/http"] = native.Package{
		Name:         "http",
		Declarations: decs,
	}
	// "net/http/cgi"
	decs = make(native.Declarations, 4)
	decs["Handler"] = reflect.TypeFor[cgi.Handler]()
	decs["Request"] = cgi.Request
	decs["RequestFromMap"] = cgi.RequestFromMap
	decs["Serve"] = cgi.Serve
	packages["net/http/cgi"] = native.Package{
		Name:         "cgi",
		Declarations: decs,
	}
	// "net/http/cookiejar"
	decs = make(native.Declarations, 4)
	decs["Jar"] = reflect.TypeFor[cookiejar.Jar]()
	decs["New"] = cookiejar.New
	decs["Options"] = reflect.TypeFor[cookiejar.Options]()
	decs["PublicSuffixList"] = reflect.TypeFor[cookiejar.PublicSuffixList]()
	packages["net/http/cookiejar"] = native.Package{
		Name:         "cookiejar",
		Declarations: decs,
	}
	// "net/http/fcgi"
	decs = make(native.Declarations, 4)
	decs["ErrConnClosed"] = &fcgi.ErrConnClosed
	decs["ErrRequestAborted"] = &fcgi.ErrRequestAborted
	decs["ProcessEnv"] = fcgi.ProcessEnv
	decs["Serve"] = fcgi.Serve
	packages["net/http/fcgi"] = native.Package{
		Name:         "fcgi",
		Declarations: decs,
	}
	// "net/http/httptest"
	decs = make(native.Declarations, 8)
	decs["DefaultRemoteAddr"] = native.UntypedStringConst("1.2.3.4")
	decs["NewRecorder"] = httptest.NewRecorder
	decs["NewRequest"] = httptest.NewRequest
	decs["NewServer"] = httptest.NewServer
	decs["NewTLSServer"] = httptest.NewTLSServer
	decs["NewUnstartedServer"] = httptest.NewUnstartedServer
	decs["ResponseRecorder"] = reflect.TypeFor[httptest.ResponseRecorder]()
	decs["Server"] = reflect.TypeFor[httptest.Server]()
	packages["net/http/httptest"] = native.Package{
		Name:         "httptest",
		Declarations: decs,
	}
	// "net/http/httptrace"
	decs = make(native.Declarations, 7)
	decs["ClientTrace"] = reflect.TypeFor[httptrace.ClientTrace]()
	decs["ContextClientTrace"] = httptrace.ContextClientTrace
	decs["DNSDoneInfo"] = reflect.TypeFor[httptrace.DNSDoneInfo]()
	decs["DNSStartInfo"] = reflect.TypeFor[httptrace.DNSStartInfo]()
	decs["GotConnInfo"] = reflect.TypeFor[httptrace.GotConnInfo]()
	decs["WithClientTrace"] = httptrace.WithClientTrace
	decs["WroteRequestInfo"] = reflect.TypeFor[httptrace.WroteRequestInfo]()
	packages["net/http/httptrace"] = native.Package{
		Name:         "httptrace",
		Declarations: decs,
	}
	// "net/http/httputil"
	decs = make(native.Declarations, 18)
	decs["BufferPool"] = reflect.TypeFor[httputil.BufferPool]()
	decs["ClientConn"] = reflect.TypeFor[httputil.ClientConn]()
	decs["DumpRequest"] = httputil.DumpRequest
	decs["DumpRequestOut"] = httputil.DumpRequestOut
	decs["DumpResponse"] = httputil.DumpResponse
	decs["ErrClosed"] = &httputil.ErrClosed
	decs["ErrLineTooLong"] = &httputil.ErrLineTooLong
	decs["ErrPersistEOF"] = &httputil.ErrPersistEOF
	decs["ErrPipeline"] = &httputil.ErrPipeline
	decs["NewChunkedReader"] = httputil.NewChunkedReader
	decs["NewChunkedWriter"] = httputil.NewChunkedWriter
	decs["NewClientConn"] = httputil.NewClientConn
	decs["NewProxyClientConn"] = httputil.NewProxyClientConn
	decs["NewServerConn"] = httputil.NewServerConn
	decs["NewSingleHostReverseProxy"] = httputil.NewSingleHostReverseProxy
	decs["ProxyRequest"] = reflect.TypeFor[httputil.ProxyRequest]()
	decs["ReverseProxy"] = reflect.TypeFor[httputil.ReverseProxy]()
	decs["ServerConn"] = reflect.TypeFor[httputil.ServerConn]()
	packages["net/http/httputil"] = native.Package{
		Name:         "httputil",
		Declarations: decs,
	}
	// "net/http/pprof"
	decs = make(native.Declarations, 6)
	decs["Cmdline"] = pprof.Cmdline
	decs["Handler"] = pprof.Handler
	decs["Index"] = pprof.Index
	decs["Profile"] = pprof.Profile
	decs["Symbol"] = pprof.Symbol
	decs["Trace"] = pprof.Trace
	packages["net/http/pprof"] = native.Package{
		Name:         "pprof",
		Declarations: decs,
	}
	// "net/mail"
	decs = make(native.Declarations, 9)
	decs["Address"] = reflect.TypeFor[mail.Address]()
	decs["AddressParser"] = reflect.TypeFor[mail.AddressParser]()
	decs["ErrHeaderNotPresent"] = &mail.ErrHeaderNotPresent
	decs["Header"] = reflect.TypeFor[mail.Header]()
	decs["Message"] = reflect.TypeFor[mail.Message]()
	decs["ParseAddress"] = mail.ParseAddress
	decs["ParseAddressList"] = mail.ParseAddressList
	decs["ParseDate"] = mail.ParseDate
	decs["ReadMessage"] = mail.ReadMessage
	packages["net/mail"] = native.Package{
		Name:         "mail",
		Declarations: decs,
	}
	// "net/rpc"
	decs = make(native.Declarations, 25)
	decs["Accept"] = rpc.Accept
	decs["Call"] = reflect.TypeFor[rpc.Call]()
	decs["Client"] = reflect.TypeFor[rpc.Client]()
	decs["ClientCodec"] = reflect.TypeFor[rpc.ClientCodec]()
	decs["DefaultDebugPath"] = native.UntypedStringConst("/debug/rpc")
	decs["DefaultRPCPath"] = native.UntypedStringConst("/_goRPC_")
	decs["DefaultServer"] = &rpc.DefaultServer
	decs["Dial"] = rpc.Dial
	decs["DialHTTP"] = rpc.DialHTTP
	decs["DialHTTPPath"] = rpc.DialHTTPPath
	decs["ErrShutdown"] = &rpc.ErrShutdown
	decs["HandleHTTP"] = rpc.HandleHTTP
	decs["NewClient"] = rpc.NewClient
	decs["NewClientWithCodec"] = rpc.NewClientWithCodec
	decs["NewServer"] = rpc.NewServer
	decs["Register"] = rpc.Register
	decs["RegisterName"] = rpc.RegisterName
	decs["Request"] = reflect.TypeFor[rpc.Request]()
	decs["Response"] = reflect.TypeFor[rpc.Response]()
	decs["ServeCodec"] = rpc.ServeCodec
	decs["ServeConn"] = rpc.ServeConn
	decs["ServeRequest"] = rpc.ServeRequest
	decs["Server"] = reflect.TypeFor[rpc.Server]()
	decs["ServerCodec"] = reflect.TypeFor[rpc.ServerCodec]()
	decs["ServerError"] = reflect.TypeFor[rpc.ServerError]()
	packages["net/rpc"] = native.Package{
		Name:         "rpc",
		Declarations: decs,
	}
	// "net/rpc/jsonrpc"
	decs = make(native.Declarations, 5)
	decs["Dial"] = jsonrpc.Dial
	decs["NewClient"] = jsonrpc.NewClient
	decs["NewClientCodec"] = jsonrpc.NewClientCodec
	decs["NewServerCodec"] = jsonrpc.NewServerCodec
	decs["ServeConn"] = jsonrpc.ServeConn
	packages["net/rpc/jsonrpc"] = native.Package{
		Name:         "jsonrpc",
		Declarations: decs,
	}
	// "net/smtp"
	decs = make(native.Declarations, 8)
	decs["Auth"] = reflect.TypeFor[smtp.Auth]()
	decs["CRAMMD5Auth"] = smtp.CRAMMD5Auth
	decs["Client"] = reflect.TypeFor[smtp.Client]()
	decs["Dial"] = smtp.Dial
	decs["NewClient"] = smtp.NewClient
	decs["PlainAuth"] = smtp.PlainAuth
	decs["SendMail"] = smtp.SendMail
	decs["ServerInfo"] = reflect.TypeFor[smtp.ServerInfo]()
	packages["net/smtp"] = native.Package{
		Name:         "smtp",
		Declarations: decs,
	}
	// "net/textproto"
	decs = make(native.Declarations, 14)
	decs["CanonicalMIMEHeaderKey"] = textproto.CanonicalMIMEHeaderKey
	decs["Conn"] = reflect.TypeFor[textproto.Conn]()
	decs["Dial"] = textproto.Dial
	decs["Error"] = reflect.TypeFor[textproto.Error]()
	decs["MIMEHeader"] = reflect.TypeFor[textproto.MIMEHeader]()
	decs["NewConn"] = textproto.NewConn
	decs["NewReader"] = textproto.NewReader
	decs["NewWriter"] = textproto.NewWriter
	decs["Pipeline"] = reflect.TypeFor[textproto.Pipeline]()
	decs["ProtocolError"] = reflect.TypeFor[textproto.ProtocolError]()
	decs["Reader"] = reflect.TypeFor[textproto.Reader]()
	decs["TrimBytes"] = textproto.TrimBytes
	decs["TrimString"] = textproto.TrimString
	decs["Writer"] = reflect.TypeFor[textproto.Writer]()
	packages["net/textproto"] = native.Package{
		Name:         "textproto",
		Declarations: decs,
	}
	// "net/url"
	decs = make(native.Declarations, 16)
	decs["Error"] = reflect.TypeFor[url.Error]()
	decs["EscapeError"] = reflect.TypeFor[url.EscapeError]()
	decs["InvalidHostError"] = reflect.TypeFor[url.InvalidHostError]()
	decs["JoinPath"] = url.JoinPath
	decs["Parse"] = url.Parse
	decs["ParseQuery"] = url.ParseQuery
	decs["ParseRequestURI"] = url.ParseRequestURI
	decs["PathEscape"] = url.PathEscape
	decs["PathUnescape"] = url.PathUnescape
	decs["QueryEscape"] = url.QueryEscape
	decs["QueryUnescape"] = url.QueryUnescape
	decs["URL"] = reflect.TypeFor[url.URL]()
	decs["User"] = url.User
	decs["UserPassword"] = url.UserPassword
	decs["Userinfo"] = reflect.TypeFor[url.Userinfo]()
	decs["Values"] = reflect.TypeFor[url.Values]()
	packages["net/url"] = native.Package{
		Name:         "url",
		Declarations: decs,
	}
	// "os"
	decs = make(native.Declarations, 114)
	decs["Args"] = &os.Args
	decs["Chdir"] = os.Chdir
	decs["Chmod"] = os.Chmod
	decs["Chown"] = os.Chown
	decs["Chtimes"] = os.Chtimes
	decs["Clearenv"] = os.Clearenv
	decs["Create"] = os.Create
	decs["CreateTemp"] = os.CreateTemp
	decs["DevNull"] = native.UntypedStringConst("/dev/null")
	decs["DirEntry"] = reflect.TypeFor[os.DirEntry]()
	decs["DirFS"] = os.DirFS
	decs["Environ"] = os.Environ
	decs["ErrClosed"] = &os.ErrClosed
	decs["ErrDeadlineExceeded"] = &os.ErrDeadlineExceeded
	decs["ErrExist"] = &os.ErrExist
	decs["ErrInvalid"] = &os.ErrInvalid
	decs["ErrNoDeadline"] = &os.ErrNoDeadline
	decs["ErrNotExist"] = &os.ErrNotExist
	decs["ErrPermission"] = &os.ErrPermission
	decs["ErrProcessDone"] = &os.ErrProcessDone
	decs["Executable"] = os.Executable
	decs["Exit"] = os.Exit
	decs["Expand"] = os.Expand
	decs["ExpandEnv"] = os.ExpandEnv
	decs["File"] = reflect.TypeFor[os.File]()
	decs["FileInfo"] = reflect.TypeFor[os.FileInfo]()
	decs["FileMode"] = reflect.TypeFor[os.FileMode]()
	decs["FindProcess"] = os.FindProcess
	decs["Getegid"] = os.Getegid
	decs["Getenv"] = os.Getenv
	decs["Geteuid"] = os.Geteuid
	decs["Getgid"] = os.Getgid
	decs["Getgroups"] = os.Getgroups
	decs["Getpagesize"] = os.Getpagesize
	decs["Getpid"] = os.Getpid
	decs["Getppid"] = os.Getppid
	decs["Getuid"] = os.Getuid
	decs["Getwd"] = os.Getwd
	decs["Hostname"] = os.Hostname
	decs["Interrupt"] = &os.Interrupt
	decs["IsExist"] = os.IsExist
	decs["IsNotExist"] = os.IsNotExist
	decs["IsPathSeparator"] = os.IsPathSeparator
	decs["IsPermission"] = os.IsPermission
	decs["IsTimeout"] = os.IsTimeout
	decs["Kill"] = &os.Kill
	decs["Lchown"] = os.Lchown
	decs["Link"] = os.Link
	decs["LinkError"] = reflect.TypeFor[os.LinkError]()
	decs["LookupEnv"] = os.LookupEnv
	decs["Lstat"] = os.Lstat
	decs["Mkdir"] = os.Mkdir
	decs["MkdirAll"] = os.MkdirAll
	decs["MkdirTemp"] = os.MkdirTemp
	decs["ModeAppend"] = os.ModeAppend
	decs["ModeCharDevice"] = os.ModeCharDevice
	decs["ModeDevice"] = os.ModeDevice
	decs["ModeDir"] = os.ModeDir
	decs["ModeExclusive"] = os.ModeExclusive
	decs["ModeIrregular"] = os.ModeIrregular
	decs["ModeNamedPipe"] = os.ModeNamedPipe
	decs["ModePerm"] = os.ModePerm
	decs["ModeSetgid"] = os.ModeSetgid
	decs["ModeSetuid"] = os.ModeSetuid
	decs["ModeSocket"] = os.ModeSocket
	decs["ModeSticky"] = os.ModeSticky
	decs["ModeSymlink"] = os.ModeSymlink
	decs["ModeTemporary"] = os.ModeTemporary
	decs["ModeType"] = os.ModeType
	decs["NewFile"] = os.NewFile
	decs["NewSyscallError"] = os.NewSyscallError
	decs["O_APPEND"] = os.O_APPEND
	decs["O_CREATE"] = os.O_CREATE
	decs["O_EXCL"] = os.O_EXCL
	decs["O_RDONLY"] = os.O_RDONLY
	decs["O_RDWR"] = os.O_RDWR
	decs["O_SYNC"] = os.O_SYNC
	decs["O_TRUNC"] = os.O_TRUNC
	decs["O_WRONLY"] = os.O_WRONLY
	decs["Open"] = os.Open
	decs["OpenFile"] = os.OpenFile
	decs["PathError"] = reflect.TypeFor[os.PathError]()
	decs["PathListSeparator"] = native.UntypedNumericConst("58")
	decs["PathSeparator"] = native.UntypedNumericConst("47")
	decs["Pipe"] = os.Pipe
	decs["ProcAttr"] = reflect.TypeFor[os.ProcAttr]()
	decs["Process"] = reflect.TypeFor[os.Process]()
	decs["ProcessState"] = reflect.TypeFor[os.ProcessState]()
	decs["ReadDir"] = os.ReadDir
	decs["ReadFile"] = os.ReadFile
	decs["Readlink"] = os.Readlink
	decs["Remove"] = os.Remove
	decs["RemoveAll"] = os.RemoveAll
	decs["Rename"] = os.Rename
	decs["SEEK_CUR"] = os.SEEK_CUR
	decs["SEEK_END"] = os.SEEK_END
	decs["SEEK_SET"] = os.SEEK_SET
	decs["SameFile"] = os.SameFile
	decs["Setenv"] = os.Setenv
	decs["Signal"] = reflect.TypeFor[os.Signal]()
	decs["StartProcess"] = os.StartProcess
	decs["Stat"] = os.Stat
	decs["Stderr"] = &os.Stderr
	decs["Stdin"] = &os.Stdin
	decs["Stdout"] = &os.Stdout
	decs["Symlink"] = os.Symlink
	decs["SyscallError"] = reflect.TypeFor[os.SyscallError]()
	decs["TempDir"] = os.TempDir
	decs["Truncate"] = os.Truncate
	decs["Unsetenv"] = os.Unsetenv
	decs["UserCacheDir"] = os.UserCacheDir
	decs["UserConfigDir"] = os.UserConfigDir
	decs["UserHomeDir"] = os.UserHomeDir
	decs["WriteFile"] = os.WriteFile
	packages["os"] = native.Package{
		Name:         "os",
		Declarations: decs,
	}
	// "os/exec"
	decs = make(native.Declarations, 9)
	decs["Cmd"] = reflect.TypeFor[exec.Cmd]()
	decs["Command"] = exec.Command
	decs["CommandContext"] = exec.CommandContext
	decs["ErrDot"] = &exec.ErrDot
	decs["ErrNotFound"] = &exec.ErrNotFound
	decs["ErrWaitDelay"] = &exec.ErrWaitDelay
	decs["Error"] = reflect.TypeFor[exec.Error]()
	decs["ExitError"] = reflect.TypeFor[exec.ExitError]()
	decs["LookPath"] = exec.LookPath
	packages["os/exec"] = native.Package{
		Name:         "exec",
		Declarations: decs,
	}
	// "os/user"
	decs = make(native.Declarations, 11)
	decs["Current"] = user.Current
	decs["Group"] = reflect.TypeFor[user.Group]()
	decs["Lookup"] = user.Lookup
	decs["LookupGroup"] = user.LookupGroup
	decs["LookupGroupId"] = user.LookupGroupId
	decs["LookupId"] = user.LookupId
	decs["UnknownGroupError"] = reflect.TypeFor[user.UnknownGroupError]()
	decs["UnknownGroupIdError"] = reflect.TypeFor[user.UnknownGroupIdError]()
	decs["UnknownUserError"] = reflect.TypeFor[user.UnknownUserError]()
	decs["UnknownUserIdError"] = reflect.TypeFor[user.UnknownUserIdError]()
	decs["User"] = reflect.TypeFor[user.User]()
	packages["os/user"] = native.Package{
		Name:         "user",
		Declarations: decs,
	}
	// "path"
	decs = make(native.Declarations, 9)
	decs["Base"] = path.Base
	decs["Clean"] = path.Clean
	decs["Dir"] = path.Dir
	decs["ErrBadPattern"] = &path.ErrBadPattern
	decs["Ext"] = path.Ext
	decs["IsAbs"] = path.IsAbs
	decs["Join"] = path.Join
	decs["Match"] = path.Match
	decs["Split"] = path.Split
	packages["path"] = native.Package{
		Name:         "path",
		Declarations: decs,
	}
	// "path/filepath"
	decs = make(native.Declarations, 26)
	decs["Abs"] = filepath.Abs
	decs["Base"] = filepath.Base
	decs["Clean"] = filepath.Clean
	decs["Dir"] = filepath.Dir
	decs["ErrBadPattern"] = &filepath.ErrBadPattern
	decs["EvalSymlinks"] = filepath.EvalSymlinks
	decs["Ext"] = filepath.Ext
	decs["FromSlash"] = filepath.FromSlash
	decs["Glob"] = filepath.Glob
	decs["HasPrefix"] = filepath.HasPrefix
	decs["IsAbs"] = filepath.IsAbs
	decs["IsLocal"] = filepath.IsLocal
	decs["Join"] = filepath.Join
	decs["ListSeparator"] = native.UntypedNumericConst("58")
	decs["Match"] = filepath.Match
	decs["Rel"] = filepath.Rel
	decs["Separator"] = native.UntypedNumericConst("47")
	decs["SkipAll"] = &filepath.SkipAll
	decs["SkipDir"] = &filepath.SkipDir
	decs["Split"] = filepath.Split
	decs["SplitList"] = filepath.SplitList
	decs["ToSlash"] = filepath.ToSlash
	decs["VolumeName"] = filepath.VolumeName
	decs["Walk"] = filepath.Walk
	decs["WalkDir"] = filepath.WalkDir
	decs["WalkFunc"] = reflect.TypeFor[filepath.WalkFunc]()
	packages["path/filepath"] = native.Package{
		Name:         "filepath",
		Declarations: decs,
	}
	// "reflect"
	decs = make(native.Declarations, 73)
	decs["Append"] = reflect.Append
	decs["AppendSlice"] = reflect.AppendSlice
	decs["Array"] = reflect.Array
	decs["ArrayOf"] = reflect.ArrayOf
	decs["Bool"] = reflect.Bool
	decs["BothDir"] = reflect.BothDir
	decs["Chan"] = reflect.Chan
	decs["ChanDir"] = reflect.TypeFor[reflect.ChanDir]()
	decs["ChanOf"] = reflect.ChanOf
	decs["Complex128"] = reflect.Complex128
	decs["Complex64"] = reflect.Complex64
	decs["Copy"] = reflect.Copy
	decs["DeepEqual"] = reflect.DeepEqual
	decs["Float32"] = reflect.Float32
	decs["Float64"] = reflect.Float64
	decs["Func"] = reflect.Func
	decs["FuncOf"] = reflect.FuncOf
	decs["Indirect"] = reflect.Indirect
	decs["Int"] = reflect.Int
	decs["Int16"] = reflect.Int16
	decs["Int32"] = reflect.Int32
	decs["Int64"] = reflect.Int64
	decs["Int8"] = reflect.Int8
	decs["Interface"] = reflect.Interface
	decs["Invalid"] = reflect.Invalid
	decs["Kind"] = reflect.TypeFor[reflect.Kind]()
	decs["MakeChan"] = reflect.MakeChan
	decs["MakeFunc"] = reflect.MakeFunc
	decs["MakeMap"] = reflect.MakeMap
	decs["MakeMapWithSize"] = reflect.MakeMapWithSize
	decs["MakeSlice"] = reflect.MakeSlice
	decs["Map"] = reflect.Map
	decs["MapIter"] = reflect.TypeFor[reflect.MapIter]()
	decs["MapOf"] = reflect.MapOf
	decs["Method"] = reflect.TypeFor[reflect.Method]()
	decs["New"] = reflect.New
	decs["NewAt"] = reflect.NewAt
	decs["Pointer"] = reflect.Pointer
	decs["PointerTo"] = reflect.PointerTo
	decs["Ptr"] = reflect.Pointer
	decs["PtrTo"] = reflect.PtrTo
	decs["RecvDir"] = reflect.RecvDir
	decs["Select"] = reflect.Select
	decs["SelectCase"] = reflect.TypeFor[reflect.SelectCase]()
	decs["SelectDefault"] = reflect.SelectDefault
	decs["SelectDir"] = reflect.TypeFor[reflect.SelectDir]()
	decs["SelectRecv"] = reflect.SelectRecv
	decs["SelectSend"] = reflect.SelectSend
	decs["SendDir"] = reflect.SendDir
	decs["Slice"] = reflect.Slice
	decs["SliceHeader"] = reflect.TypeFor[reflect.SliceHeader]()
	decs["SliceOf"] = reflect.SliceOf
	decs["String"] = reflect.String
	decs["StringHeader"] = reflect.TypeFor[reflect.StringHeader]()
	decs["Struct"] = reflect.Struct
	decs["StructField"] = reflect.TypeFor[reflect.StructField]()
	decs["StructOf"] = reflect.StructOf
	decs["StructTag"] = reflect.TypeFor[reflect.StructTag]()
	decs["Swapper"] = reflect.Swapper
	decs["Type"] = reflect.TypeFor[reflect.Type]()
	decs["TypeOf"] = reflect.TypeOf
	decs["Uint"] = reflect.Uint
	decs["Uint16"] = reflect.Uint16
	decs["Uint32"] = reflect.Uint32
	decs["Uint64"] = reflect.Uint64
	decs["Uint8"] = reflect.Uint8
	decs["Uintptr"] = reflect.Uintptr
	decs["UnsafePointer"] = reflect.UnsafePointer
	decs["Value"] = reflect.TypeFor[reflect.Value]()
	decs["ValueError"] = reflect.TypeFor[reflect.ValueError]()
	decs["ValueOf"] = reflect.ValueOf
	decs["VisibleFields"] = reflect.VisibleFields
	decs["Zero"] = reflect.Zero
	packages["reflect"] = native.Package{
		Name:         "reflect",
		Declarations: decs,
	}
	// "regexp"
	decs = make(native.Declarations, 9)
	decs["Compile"] = regexp.Compile
	decs["CompilePOSIX"] = regexp.CompilePOSIX
	decs["Match"] = regexp.Match
	decs["MatchReader"] = regexp.MatchReader
	decs["MatchString"] = regexp.MatchString
	decs["MustCompile"] = regexp.MustCompile
	decs["MustCompilePOSIX"] = regexp.MustCompilePOSIX
	decs["QuoteMeta"] = regexp.QuoteMeta
	decs["Regexp"] = reflect.TypeFor[regexp.Regexp]()
	packages["regexp"] = native.Package{
		Name:         "regexp",
		Declarations: decs,
	}
	// "regexp/syntax"
	decs = make(native.Declarations, 78)
	decs["ClassNL"] = syntax.ClassNL
	decs["Compile"] = syntax.Compile
	decs["DotNL"] = syntax.DotNL
	decs["EmptyBeginLine"] = syntax.EmptyBeginLine
	decs["EmptyBeginText"] = syntax.EmptyBeginText
	decs["EmptyEndLine"] = syntax.EmptyEndLine
	decs["EmptyEndText"] = syntax.EmptyEndText
	decs["EmptyNoWordBoundary"] = syntax.EmptyNoWordBoundary
	decs["EmptyOp"] = reflect.TypeFor[syntax.EmptyOp]()
	decs["EmptyOpContext"] = syntax.EmptyOpContext
	decs["EmptyWordBoundary"] = syntax.EmptyWordBoundary
	decs["ErrInternalError"] = syntax.ErrInternalError
	decs["ErrInvalidCharClass"] = syntax.ErrInvalidCharClass
	decs["ErrInvalidCharRange"] = syntax.ErrInvalidCharRange
	decs["ErrInvalidEscape"] = syntax.ErrInvalidEscape
	decs["ErrInvalidNamedCapture"] = syntax.ErrInvalidNamedCapture
	decs["ErrInvalidPerlOp"] = syntax.ErrInvalidPerlOp
	decs["ErrInvalidRepeatOp"] = syntax.ErrInvalidRepeatOp
	decs["ErrInvalidRepeatSize"] = syntax.ErrInvalidRepeatSize
	decs["ErrInvalidUTF8"] = syntax.ErrInvalidUTF8
	decs["ErrLarge"] = syntax.ErrLarge
	decs["ErrMissingBracket"] = syntax.ErrMissingBracket
	decs["ErrMissingParen"] = syntax.ErrMissingParen
	decs["ErrMissingRepeatArgument"] = syntax.ErrMissingRepeatArgument
	decs["ErrNestingDepth"] = syntax.ErrNestingDepth
	decs["ErrTrailingBackslash"] = syntax.ErrTrailingBackslash
	decs["ErrUnexpectedParen"] = syntax.ErrUnexpectedParen
	decs["Error"] = reflect.TypeFor[syntax.Error]()
	decs["ErrorCode"] = reflect.TypeFor[syntax.ErrorCode]()
	decs["Flags"] = reflect.TypeFor[syntax.Flags]()
	decs["FoldCase"] = syntax.FoldCase
	decs["Inst"] = reflect.TypeFor[syntax.Inst]()
	decs["InstAlt"] = syntax.InstAlt
	decs["InstAltMatch"] = syntax.InstAltMatch
	decs["InstCapture"] = syntax.InstCapture
	decs["InstEmptyWidth"] = syntax.InstEmptyWidth
	decs["InstFail"] = syntax.InstFail
	decs["InstMatch"] = syntax.InstMatch
	decs["InstNop"] = syntax.InstNop
	decs["InstOp"] = reflect.TypeFor[syntax.InstOp]()
	decs["InstRune"] = syntax.InstRune
	decs["InstRune1"] = syntax.InstRune1
	decs["InstRuneAny"] = syntax.InstRuneAny
	decs["InstRuneAnyNotNL"] = syntax.InstRuneAnyNotNL
	decs["IsWordChar"] = syntax.IsWordChar
	decs["Literal"] = syntax.Literal
	decs["MatchNL"] = syntax.MatchNL
	decs["NonGreedy"] = syntax.NonGreedy
	decs["OneLine"] = syntax.OneLine
	decs["Op"] = reflect.TypeFor[syntax.Op]()
	decs["OpAlternate"] = syntax.OpAlternate
	decs["OpAnyChar"] = syntax.OpAnyChar
	decs["OpAnyCharNotNL"] = syntax.OpAnyCharNotNL
	decs["OpBeginLine"] = syntax.OpBeginLine
	decs["OpBeginText"] = syntax.OpBeginText
	decs["OpCapture"] = syntax.OpCapture
	decs["OpCharClass"] = syntax.OpCharClass
	decs["OpConcat"] = syntax.OpConcat
	decs["OpEmptyMatch"] = syntax.OpEmptyMatch
	decs["OpEndLine"] = syntax.OpEndLine
	decs["OpEndText"] = syntax.OpEndText
	decs["OpLiteral"] = syntax.OpLiteral
	decs["OpNoMatch"] = syntax.OpNoMatch
	decs["OpNoWordBoundary"] = syntax.OpNoWordBoundary
	decs["OpPlus"] = syntax.OpPlus
	decs["OpQuest"] = syntax.OpQuest
	decs["OpRepeat"] = syntax.OpRepeat
	decs["OpStar"] = syntax.OpStar
	decs["OpWordBoundary"] = syntax.OpWordBoundary
	decs["POSIX"] = syntax.POSIX
	decs["Parse"] = syntax.Parse
	decs["Perl"] = syntax.Perl
	decs["PerlX"] = syntax.PerlX
	decs["Prog"] = reflect.TypeFor[syntax.Prog]()
	decs["Regexp"] = reflect.TypeFor[syntax.Regexp]()
	decs["Simple"] = syntax.Simple
	decs["UnicodeGroups"] = syntax.UnicodeGroups
	decs["WasDollar"] = syntax.WasDollar
	packages["regexp/syntax"] = native.Package{
		Name:         "syntax",
		Declarations: decs,
	}
	// "runtime/debug"
	decs = make(native.Declarations, 17)
	decs["BuildInfo"] = reflect.TypeFor[debug.BuildInfo]()
	decs["BuildSetting"] = reflect.TypeFor[debug.BuildSetting]()
	decs["FreeOSMemory"] = debug.FreeOSMemory
	decs["GCStats"] = reflect.TypeFor[debug.GCStats]()
	decs["Module"] = reflect.TypeFor[debug.Module]()
	decs["ParseBuildInfo"] = debug.ParseBuildInfo
	decs["PrintStack"] = debug.PrintStack
	decs["ReadBuildInfo"] = debug.ReadBuildInfo
	decs["ReadGCStats"] = debug.ReadGCStats
	decs["SetGCPercent"] = debug.SetGCPercent
	decs["SetMaxStack"] = debug.SetMaxStack
	decs["SetMaxThreads"] = debug.SetMaxThreads
	decs["SetMemoryLimit"] = debug.SetMemoryLimit
	decs["SetPanicOnFault"] = debug.SetPanicOnFault
	decs["SetTraceback"] = debug.SetTraceback
	decs["Stack"] = debug.Stack
	decs["WriteHeapDump"] = debug.WriteHeapDump
	packages["runtime/debug"] = native.Package{
		Name:         "debug",
		Declarations: decs,
	}
	// "sort"
	decs = make(native.Declarations, 22)
	decs["Find"] = sort.Find
	decs["Float64Slice"] = reflect.TypeFor[sort.Float64Slice]()
	decs["Float64s"] = sort.Float64s
	decs["Float64sAreSorted"] = sort.Float64sAreSorted
	decs["IntSlice"] = reflect.TypeFor[sort.IntSlice]()
	decs["Interface"] = reflect.TypeFor[sort.Interface]()
	decs["Ints"] = sort.Ints
	decs["IntsAreSorted"] = sort.IntsAreSorted
	decs["IsSorted"] = sort.IsSorted
	decs["Reverse"] = sort.Reverse
	decs["Search"] = sort.Search
	decs["SearchFloat64s"] = sort.SearchFloat64s
	decs["SearchInts"] = sort.SearchInts
	decs["SearchStrings"] = sort.SearchStrings
	decs["Slice"] = sort.Slice
	decs["SliceIsSorted"] = sort.SliceIsSorted
	decs["SliceStable"] = sort.SliceStable
	decs["Sort"] = sort.Sort
	decs["Stable"] = sort.Stable
	decs["StringSlice"] = reflect.TypeFor[sort.StringSlice]()
	decs["Strings"] = sort.Strings
	decs["StringsAreSorted"] = sort.StringsAreSorted
	packages["sort"] = native.Package{
		Name:         "sort",
		Declarations: decs,
	}
	// "strconv"
	decs = make(native.Declarations, 38)
	decs["AppendBool"] = strconv.AppendBool
	decs["AppendFloat"] = strconv.AppendFloat
	decs["AppendInt"] = strconv.AppendInt
	decs["AppendQuote"] = strconv.AppendQuote
	decs["AppendQuoteRune"] = strconv.AppendQuoteRune
	decs["AppendQuoteRuneToASCII"] = strconv.AppendQuoteRuneToASCII
	decs["AppendQuoteRuneToGraphic"] = strconv.AppendQuoteRuneToGraphic
	decs["AppendQuoteToASCII"] = strconv.AppendQuoteToASCII
	decs["AppendQuoteToGraphic"] = strconv.AppendQuoteToGraphic
	decs["AppendUint"] = strconv.AppendUint
	decs["Atoi"] = strconv.Atoi
	decs["CanBackquote"] = strconv.CanBackquote
	decs["ErrRange"] = &strconv.ErrRange
	decs["ErrSyntax"] = &strconv.ErrSyntax
	decs["FormatBool"] = strconv.FormatBool
	decs["FormatComplex"] = strconv.FormatComplex
	decs["FormatFloat"] = strconv.FormatFloat
	decs["FormatInt"] = strconv.FormatInt
	decs["FormatUint"] = strconv.FormatUint
	decs["IntSize"] = native.UntypedNumericConst("64")
	decs["IsGraphic"] = strconv.IsGraphic
	decs["IsPrint"] = strconv.IsPrint
	decs["Itoa"] = strconv.Itoa
	decs["NumError"] = reflect.TypeFor[strconv.NumError]()
	decs["ParseBool"] = strconv.ParseBool
	decs["ParseComplex"] = strconv.ParseComplex
	decs["ParseFloat"] = strconv.ParseFloat
	decs["ParseInt"] = strconv.ParseInt
	decs["ParseUint"] = strconv.ParseUint
	decs["Quote"] = strconv.Quote
	decs["QuoteRune"] = strconv.QuoteRune
	decs["QuoteRuneToASCII"] = strconv.QuoteRuneToASCII
	decs["QuoteRuneToGraphic"] = strconv.QuoteRuneToGraphic
	decs["QuoteToASCII"] = strconv.QuoteToASCII
	decs["QuoteToGraphic"] = strconv.QuoteToGraphic
	decs["QuotedPrefix"] = strconv.QuotedPrefix
	decs["Unquote"] = strconv.Unquote
	decs["UnquoteChar"] = strconv.UnquoteChar
	packages["strconv"] = native.Package{
		Name:         "strconv",
		Declarations: decs,
	}
	// "strings"
	decs = make(native.Declarations, 55)
	decs["Builder"] = reflect.TypeFor[strings.Builder]()
	decs["Clone"] = strings.Clone
	decs["Compare"] = strings.Compare
	decs["Contains"] = strings.Contains
	decs["ContainsAny"] = strings.ContainsAny
	decs["ContainsFunc"] = strings.ContainsFunc
	decs["ContainsRune"] = strings.ContainsRune
	decs["Count"] = strings.Count
	decs["Cut"] = strings.Cut
	decs["CutPrefix"] = strings.CutPrefix
	decs["CutSuffix"] = strings.CutSuffix
	decs["EqualFold"] = strings.EqualFold
	decs["Fields"] = strings.Fields
	decs["FieldsFunc"] = strings.FieldsFunc
	decs["HasPrefix"] = strings.HasPrefix
	decs["HasSuffix"] = strings.HasSuffix
	decs["Index"] = strings.Index
	decs["IndexAny"] = strings.IndexAny
	decs["IndexByte"] = strings.IndexByte
	decs["IndexFunc"] = strings.IndexFunc
	decs["IndexRune"] = strings.IndexRune
	decs["Join"] = strings.Join
	decs["LastIndex"] = strings.LastIndex
	decs["LastIndexAny"] = strings.LastIndexAny
	decs["LastIndexByte"] = strings.LastIndexByte
	decs["LastIndexFunc"] = strings.LastIndexFunc
	decs["Map"] = strings.Map
	decs["NewReader"] = strings.NewReader
	decs["NewReplacer"] = strings.NewReplacer
	decs["Reader"] = reflect.TypeFor[strings.Reader]()
	decs["Repeat"] = strings.Repeat
	decs["Replace"] = strings.Replace
	decs["ReplaceAll"] = strings.ReplaceAll
	decs["Replacer"] = reflect.TypeFor[strings.Replacer]()
	decs["Split"] = strings.Split
	decs["SplitAfter"] = strings.SplitAfter
	decs["SplitAfterN"] = strings.SplitAfterN
	decs["SplitN"] = strings.SplitN
	decs["Title"] = strings.Title
	decs["ToLower"] = strings.ToLower
	decs["ToLowerSpecial"] = strings.ToLowerSpecial
	decs["ToTitle"] = strings.ToTitle
	decs["ToTitleSpecial"] = strings.ToTitleSpecial
	decs["ToUpper"] = strings.ToUpper
	decs["ToUpperSpecial"] = strings.ToUpperSpecial
	decs["ToValidUTF8"] = strings.ToValidUTF8
	decs["Trim"] = strings.Trim
	decs["TrimFunc"] = strings.TrimFunc
	decs["TrimLeft"] = strings.TrimLeft
	decs["TrimLeftFunc"] = strings.TrimLeftFunc
	decs["TrimPrefix"] = strings.TrimPrefix
	decs["TrimRight"] = strings.TrimRight
	decs["TrimRightFunc"] = strings.TrimRightFunc
	decs["TrimSpace"] = strings.TrimSpace
	decs["TrimSuffix"] = strings.TrimSuffix
	packages["strings"] = native.Package{
		Name:         "strings",
		Declarations: decs,
	}
	// "sync"
	decs = make(native.Declarations, 12)
	decs["Cond"] = reflect.TypeFor[sync.Cond]()
	decs["Locker"] = reflect.TypeFor[sync.Locker]()
	decs["Map"] = reflect.TypeFor[sync.Map]()
	decs["Mutex"] = reflect.TypeFor[sync.Mutex]()
	decs["NewCond"] = sync.NewCond
	decs["Once"] = reflect.TypeFor[sync.Once]()
	decs["OnceFunc"] = sync.OnceFunc
	decs["Pool"] = reflect.TypeFor[sync.Pool]()
	decs["RWMutex"] = reflect.TypeFor[sync.RWMutex]()
	decs["WaitGroup"] = reflect.TypeFor[sync.WaitGroup]()
	packages["sync"] = native.Package{
		Name:         "sync",
		Declarations: decs,
	}
	// "sync/atomic"
	decs = make(native.Declarations, 37)
	decs["AddInt32"] = atomic.AddInt32
	decs["AddInt64"] = atomic.AddInt64
	decs["AddUint32"] = atomic.AddUint32
	decs["AddUint64"] = atomic.AddUint64
	decs["AddUintptr"] = atomic.AddUintptr
	decs["Bool"] = reflect.TypeFor[atomic.Bool]()
	decs["CompareAndSwapInt32"] = atomic.CompareAndSwapInt32
	decs["CompareAndSwapInt64"] = atomic.CompareAndSwapInt64
	decs["CompareAndSwapPointer"] = atomic.CompareAndSwapPointer
	decs["CompareAndSwapUint32"] = atomic.CompareAndSwapUint32
	decs["CompareAndSwapUint64"] = atomic.CompareAndSwapUint64
	decs["CompareAndSwapUintptr"] = atomic.CompareAndSwapUintptr
	decs["Int32"] = reflect.TypeFor[atomic.Int32]()
	decs["Int64"] = reflect.TypeFor[atomic.Int64]()
	decs["LoadInt32"] = atomic.LoadInt32
	decs["LoadInt64"] = atomic.LoadInt64
	decs["LoadPointer"] = atomic.LoadPointer
	decs["LoadUint32"] = atomic.LoadUint32
	decs["LoadUint64"] = atomic.LoadUint64
	decs["LoadUintptr"] = atomic.LoadUintptr
	decs["StoreInt32"] = atomic.StoreInt32
	decs["StoreInt64"] = atomic.StoreInt64
	decs["StorePointer"] = atomic.StorePointer
	decs["StoreUint32"] = atomic.StoreUint32
	decs["StoreUint64"] = atomic.StoreUint64
	decs["StoreUintptr"] = atomic.StoreUintptr
	decs["SwapInt32"] = atomic.SwapInt32
	decs["SwapInt64"] = atomic.SwapInt64
	decs["SwapPointer"] = atomic.SwapPointer
	decs["SwapUint32"] = atomic.SwapUint32
	decs["SwapUint64"] = atomic.SwapUint64
	decs["SwapUintptr"] = atomic.SwapUintptr
	decs["Uint32"] = reflect.TypeFor[atomic.Uint32]()
	decs["Uint64"] = reflect.TypeFor[atomic.Uint64]()
	decs["Uintptr"] = reflect.TypeFor[atomic.Uintptr]()
	decs["Value"] = reflect.TypeFor[atomic.Value]()
	packages["sync/atomic"] = native.Package{
		Name:         "atomic",
		Declarations: decs,
	}
	// "text/scanner"
	decs = make(native.Declarations, 21)
	decs["Char"] = native.UntypedNumericConst("-5")
	decs["Comment"] = native.UntypedNumericConst("-8")
	decs["EOF"] = native.UntypedNumericConst("-1")
	decs["Float"] = native.UntypedNumericConst("-4")
	decs["GoTokens"] = native.UntypedNumericConst("1012")
	decs["GoWhitespace"] = native.UntypedNumericConst("4294977024")
	decs["Ident"] = native.UntypedNumericConst("-2")
	decs["Int"] = native.UntypedNumericConst("-3")
	decs["Position"] = reflect.TypeFor[scanner_2.Position]()
	decs["RawString"] = native.UntypedNumericConst("-7")
	decs["ScanChars"] = native.UntypedNumericConst("32")
	decs["ScanComments"] = native.UntypedNumericConst("256")
	decs["ScanFloats"] = native.UntypedNumericConst("16")
	decs["ScanIdents"] = native.UntypedNumericConst("4")
	decs["ScanInts"] = native.UntypedNumericConst("8")
	decs["ScanRawStrings"] = native.UntypedNumericConst("128")
	decs["ScanStrings"] = native.UntypedNumericConst("64")
	decs["Scanner"] = reflect.TypeFor[scanner_2.Scanner]()
	decs["SkipComments"] = native.UntypedNumericConst("512")
	decs["String"] = native.UntypedNumericConst("-6")
	decs["TokenString"] = scanner_2.TokenString
	packages["text/scanner"] = native.Package{
		Name:         "scanner",
		Declarations: decs,
	}
	// "text/tabwriter"
	decs = make(native.Declarations, 9)
	decs["AlignRight"] = tabwriter.AlignRight
	decs["Debug"] = tabwriter.Debug
	decs["DiscardEmptyColumns"] = tabwriter.DiscardEmptyColumns
	decs["Escape"] = native.UntypedNumericConst("255")
	decs["FilterHTML"] = tabwriter.FilterHTML
	decs["NewWriter"] = tabwriter.NewWriter
	decs["StripEscape"] = tabwriter.StripEscape
	decs["TabIndent"] = tabwriter.TabIndent
	decs["Writer"] = reflect.TypeFor[tabwriter.Writer]()
	packages["text/tabwriter"] = native.Package{
		Name:         "tabwriter",
		Declarations: decs,
	}
	// "text/template"
	decs = make(native.Declarations, 16)
	decs["ExecError"] = reflect.TypeFor[template_2.ExecError]()
	decs["FuncMap"] = reflect.TypeFor[template_2.FuncMap]()
	decs["HTMLEscape"] = template_2.HTMLEscape
	decs["HTMLEscapeString"] = template_2.HTMLEscapeString
	decs["HTMLEscaper"] = template_2.HTMLEscaper
	decs["IsTrue"] = template_2.IsTrue
	decs["JSEscape"] = template_2.JSEscape
	decs["JSEscapeString"] = template_2.JSEscapeString
	decs["JSEscaper"] = template_2.JSEscaper
	decs["Must"] = template_2.Must
	decs["New"] = template_2.New
	decs["ParseFS"] = template_2.ParseFS
	decs["ParseFiles"] = template_2.ParseFiles
	decs["ParseGlob"] = template_2.ParseGlob
	decs["Template"] = reflect.TypeFor[template_2.Template]()
	decs["URLQueryEscaper"] = template_2.URLQueryEscaper
	packages["text/template"] = native.Package{
		Name:         "template",
		Declarations: decs,
	}
	// "text/template/parse"
	decs = make(native.Declarations, 54)
	decs["ActionNode"] = reflect.TypeFor[parse.ActionNode]()
	decs["BoolNode"] = reflect.TypeFor[parse.BoolNode]()
	decs["BranchNode"] = reflect.TypeFor[parse.BranchNode]()
	decs["BreakNode"] = reflect.TypeFor[parse.BreakNode]()
	decs["ChainNode"] = reflect.TypeFor[parse.ChainNode]()
	decs["CommandNode"] = reflect.TypeFor[parse.CommandNode]()
	decs["CommentNode"] = reflect.TypeFor[parse.CommentNode]()
	decs["ContinueNode"] = reflect.TypeFor[parse.ContinueNode]()
	decs["DotNode"] = reflect.TypeFor[parse.DotNode]()
	decs["FieldNode"] = reflect.TypeFor[parse.FieldNode]()
	decs["IdentifierNode"] = reflect.TypeFor[parse.IdentifierNode]()
	decs["IfNode"] = reflect.TypeFor[parse.IfNode]()
	decs["IsEmptyTree"] = parse.IsEmptyTree
	decs["ListNode"] = reflect.TypeFor[parse.ListNode]()
	decs["Mode"] = reflect.TypeFor[parse.Mode]()
	decs["New"] = parse.New
	decs["NewIdentifier"] = parse.NewIdentifier
	decs["NilNode"] = reflect.TypeFor[parse.NilNode]()
	decs["Node"] = reflect.TypeFor[parse.Node]()
	decs["NodeAction"] = parse.NodeAction
	decs["NodeBool"] = parse.NodeBool
	decs["NodeBreak"] = parse.NodeBreak
	decs["NodeChain"] = parse.NodeChain
	decs["NodeCommand"] = parse.NodeCommand
	decs["NodeComment"] = parse.NodeComment
	decs["NodeContinue"] = parse.NodeContinue
	decs["NodeDot"] = parse.NodeDot
	decs["NodeField"] = parse.NodeField
	decs["NodeIdentifier"] = parse.NodeIdentifier
	decs["NodeIf"] = parse.NodeIf
	decs["NodeList"] = parse.NodeList
	decs["NodeNil"] = parse.NodeNil
	decs["NodeNumber"] = parse.NodeNumber
	decs["NodePipe"] = parse.NodePipe
	decs["NodeRange"] = parse.NodeRange
	decs["NodeString"] = parse.NodeString
	decs["NodeTemplate"] = parse.NodeTemplate
	decs["NodeText"] = parse.NodeText
	decs["NodeType"] = reflect.TypeFor[parse.NodeType]()
	decs["NodeVariable"] = parse.NodeVariable
	decs["NodeWith"] = parse.NodeWith
	decs["NumberNode"] = reflect.TypeFor[parse.NumberNode]()
	decs["Parse"] = parse.Parse
	decs["ParseComments"] = parse.ParseComments
	decs["PipeNode"] = reflect.TypeFor[parse.PipeNode]()
	decs["Pos"] = reflect.TypeFor[parse.Pos]()
	decs["RangeNode"] = reflect.TypeFor[parse.RangeNode]()
	decs["SkipFuncCheck"] = parse.SkipFuncCheck
	decs["StringNode"] = reflect.TypeFor[parse.StringNode]()
	decs["TemplateNode"] = reflect.TypeFor[parse.TemplateNode]()
	decs["TextNode"] = reflect.TypeFor[parse.TextNode]()
	decs["Tree"] = reflect.TypeFor[parse.Tree]()
	decs["VariableNode"] = reflect.TypeFor[parse.VariableNode]()
	decs["WithNode"] = reflect.TypeFor[parse.WithNode]()
	packages["text/template/parse"] = native.Package{
		Name:         "parse",
		Declarations: decs,
	}
	// "time"
	decs = make(native.Declarations, 73)
	decs["ANSIC"] = native.UntypedStringConst("Mon Jan _2 15:04:05 2006")
	decs["After"] = time.After
	decs["AfterFunc"] = time.AfterFunc
	decs["April"] = time.April
	decs["August"] = time.August
	decs["Date"] = time.Date
	decs["DateOnly"] = native.UntypedStringConst("2006-01-02")
	decs["DateTime"] = native.UntypedStringConst("2006-01-02 15:04:05")
	decs["December"] = time.December
	decs["Duration"] = reflect.TypeFor[time.Duration]()
	decs["February"] = time.February
	decs["FixedZone"] = time.FixedZone
	decs["Friday"] = time.Friday
	decs["Hour"] = time.Hour
	decs["January"] = time.January
	decs["July"] = time.July
	decs["June"] = time.June
	decs["Kitchen"] = native.UntypedStringConst("3:04PM")
	decs["Layout"] = native.UntypedStringConst("01/02 03:04:05PM '06 -0700")
	decs["LoadLocation"] = time.LoadLocation
	decs["LoadLocationFromTZData"] = time.LoadLocationFromTZData
	decs["Local"] = &time.Local
	decs["Location"] = reflect.TypeFor[time.Location]()
	decs["March"] = time.March
	decs["May"] = time.May
	decs["Microsecond"] = time.Microsecond
	decs["Millisecond"] = time.Millisecond
	decs["Minute"] = time.Minute
	decs["Monday"] = time.Monday
	decs["Month"] = reflect.TypeFor[time.Month]()
	decs["Nanosecond"] = time.Nanosecond
	decs["NewTicker"] = time.NewTicker
	decs["NewTimer"] = time.NewTimer
	decs["November"] = time.November
	decs["Now"] = time.Now
	decs["October"] = time.October
	decs["Parse"] = time.Parse
	decs["ParseDuration"] = time.ParseDuration
	decs["ParseError"] = reflect.TypeFor[time.ParseError]()
	decs["ParseInLocation"] = time.ParseInLocation
	decs["RFC1123"] = native.UntypedStringConst("Mon, 02 Jan 2006 15:04:05 MST")
	decs["RFC1123Z"] = native.UntypedStringConst("Mon, 02 Jan 2006 15:04:05 -0700")
	decs["RFC3339"] = native.UntypedStringConst("2006-01-02T15:04:05Z07:00")
	decs["RFC3339Nano"] = native.UntypedStringConst("2006-01-02T15:04:05.999999999Z07:00")
	decs["RFC822"] = native.UntypedStringConst("02 Jan 06 15:04 MST")
	decs["RFC822Z"] = native.UntypedStringConst("02 Jan 06 15:04 -0700")
	decs["RFC850"] = native.UntypedStringConst("Monday, 02-Jan-06 15:04:05 MST")
	decs["RubyDate"] = native.UntypedStringConst("Mon Jan 02 15:04:05 -0700 2006")
	decs["Saturday"] = time.Saturday
	decs["Second"] = time.Second
	decs["September"] = time.September
	decs["Since"] = time.Since
	decs["Sleep"] = time.Sleep
	decs["Stamp"] = native.UntypedStringConst("Jan _2 15:04:05")
	decs["StampMicro"] = native.UntypedStringConst("Jan _2 15:04:05.000000")
	decs["StampMilli"] = native.UntypedStringConst("Jan _2 15:04:05.000")
	decs["StampNano"] = native.UntypedStringConst("Jan _2 15:04:05.000000000")
	decs["Sunday"] = time.Sunday
	decs["Thursday"] = time.Thursday
	decs["Tick"] = time.Tick
	decs["Ticker"] = reflect.TypeFor[time.Ticker]()
	decs["Time"] = reflect.TypeFor[time.Time]()
	decs["TimeOnly"] = native.UntypedStringConst("15:04:05")
	decs["Timer"] = reflect.TypeFor[time.Timer]()
	decs["Tuesday"] = time.Tuesday
	decs["UTC"] = &time.UTC
	decs["Unix"] = time.Unix
	decs["UnixDate"] = native.UntypedStringConst("Mon Jan _2 15:04:05 MST 2006")
	decs["UnixMicro"] = time.UnixMicro
	decs["UnixMilli"] = time.UnixMilli
	decs["Until"] = time.Until
	decs["Wednesday"] = time.Wednesday
	decs["Weekday"] = reflect.TypeFor[time.Weekday]()
	packages["time"] = native.Package{
		Name:         "time",
		Declarations: decs,
	}
	// "unicode"
	decs = make(native.Declarations, 291)
	decs["ASCII_Hex_Digit"] = &unicode.ASCII_Hex_Digit
	decs["Adlam"] = &unicode.Adlam
	decs["Ahom"] = &unicode.Ahom
	decs["Anatolian_Hieroglyphs"] = &unicode.Anatolian_Hieroglyphs
	decs["Arabic"] = &unicode.Arabic
	decs["Armenian"] = &unicode.Armenian
	decs["Avestan"] = &unicode.Avestan
	decs["AzeriCase"] = &unicode.AzeriCase
	decs["Balinese"] = &unicode.Balinese
	decs["Bamum"] = &unicode.Bamum
	decs["Bassa_Vah"] = &unicode.Bassa_Vah
	decs["Batak"] = &unicode.Batak
	decs["Bengali"] = &unicode.Bengali
	decs["Bhaiksuki"] = &unicode.Bhaiksuki
	decs["Bidi_Control"] = &unicode.Bidi_Control
	decs["Bopomofo"] = &unicode.Bopomofo
	decs["Brahmi"] = &unicode.Brahmi
	decs["Braille"] = &unicode.Braille
	decs["Buginese"] = &unicode.Buginese
	decs["Buhid"] = &unicode.Buhid
	decs["C"] = &unicode.C
	decs["Canadian_Aboriginal"] = &unicode.Canadian_Aboriginal
	decs["Carian"] = &unicode.Carian
	decs["CaseRange"] = reflect.TypeFor[unicode.CaseRange]()
	decs["CaseRanges"] = &unicode.CaseRanges
	decs["Categories"] = &unicode.Categories
	decs["Caucasian_Albanian"] = &unicode.Caucasian_Albanian
	decs["Cc"] = &unicode.Cc
	decs["Cf"] = &unicode.Cf
	decs["Chakma"] = &unicode.Chakma
	decs["Cham"] = &unicode.Cham
	decs["Cherokee"] = &unicode.Cherokee
	decs["Chorasmian"] = &unicode.Chorasmian
	decs["Co"] = &unicode.Co
	decs["Common"] = &unicode.Common
	decs["Coptic"] = &unicode.Coptic
	decs["Cs"] = &unicode.Cs
	decs["Cuneiform"] = &unicode.Cuneiform
	decs["Cypriot"] = &unicode.Cypriot
	decs["Cypro_Minoan"] = &unicode.Cypro_Minoan
	decs["Cyrillic"] = &unicode.Cyrillic
	decs["Dash"] = &unicode.Dash
	decs["Deprecated"] = &unicode.Deprecated
	decs["Deseret"] = &unicode.Deseret
	decs["Devanagari"] = &unicode.Devanagari
	decs["Diacritic"] = &unicode.Diacritic
	decs["Digit"] = &unicode.Digit
	decs["Dives_Akuru"] = &unicode.Dives_Akuru
	decs["Dogra"] = &unicode.Dogra
	decs["Duployan"] = &unicode.Duployan
	decs["Egyptian_Hieroglyphs"] = &unicode.Egyptian_Hieroglyphs
	decs["Elbasan"] = &unicode.Elbasan
	decs["Elymaic"] = &unicode.Elymaic
	decs["Ethiopic"] = &unicode.Ethiopic
	decs["Extender"] = &unicode.Extender
	decs["FoldCategory"] = &unicode.FoldCategory
	decs["FoldScript"] = &unicode.FoldScript
	decs["Georgian"] = &unicode.Georgian
	decs["Glagolitic"] = &unicode.Glagolitic
	decs["Gothic"] = &unicode.Gothic
	decs["Grantha"] = &unicode.Grantha
	decs["GraphicRanges"] = &unicode.GraphicRanges
	decs["Greek"] = &unicode.Greek
	decs["Gujarati"] = &unicode.Gujarati
	decs["Gunjala_Gondi"] = &unicode.Gunjala_Gondi
	decs["Gurmukhi"] = &unicode.Gurmukhi
	decs["Han"] = &unicode.Han
	decs["Hangul"] = &unicode.Hangul
	decs["Hanifi_Rohingya"] = &unicode.Hanifi_Rohingya
	decs["Hanunoo"] = &unicode.Hanunoo
	decs["Hatran"] = &unicode.Hatran
	decs["Hebrew"] = &unicode.Hebrew
	decs["Hex_Digit"] = &unicode.Hex_Digit
	decs["Hiragana"] = &unicode.Hiragana
	decs["Hyphen"] = &unicode.Hyphen
	decs["IDS_Binary_Operator"] = &unicode.IDS_Binary_Operator
	decs["IDS_Trinary_Operator"] = &unicode.IDS_Trinary_Operator
	decs["Ideographic"] = &unicode.Ideographic
	decs["Imperial_Aramaic"] = &unicode.Imperial_Aramaic
	decs["In"] = unicode.In
	decs["Inherited"] = &unicode.Inherited
	decs["Inscriptional_Pahlavi"] = &unicode.Inscriptional_Pahlavi
	decs["Inscriptional_Parthian"] = &unicode.Inscriptional_Parthian
	decs["Is"] = unicode.Is
	decs["IsControl"] = unicode.IsControl
	decs["IsDigit"] = unicode.IsDigit
	decs["IsGraphic"] = unicode.IsGraphic
	decs["IsLetter"] = unicode.IsLetter
	decs["IsLower"] = unicode.IsLower
	decs["IsMark"] = unicode.IsMark
	decs["IsNumber"] = unicode.IsNumber
	decs["IsOneOf"] = unicode.IsOneOf
	decs["IsPrint"] = unicode.IsPrint
	decs["IsPunct"] = unicode.IsPunct
	decs["IsSpace"] = unicode.IsSpace
	decs["IsSymbol"] = unicode.IsSymbol
	decs["IsTitle"] = unicode.IsTitle
	decs["IsUpper"] = unicode.IsUpper
	decs["Javanese"] = &unicode.Javanese
	decs["Join_Control"] = &unicode.Join_Control
	decs["Kaithi"] = &unicode.Kaithi
	decs["Kannada"] = &unicode.Kannada
	decs["Katakana"] = &unicode.Katakana
	decs["Kawi"] = &unicode.Kawi
	decs["Kayah_Li"] = &unicode.Kayah_Li
	decs["Kharoshthi"] = &unicode.Kharoshthi
	decs["Khitan_Small_Script"] = &unicode.Khitan_Small_Script
	decs["Khmer"] = &unicode.Khmer
	decs["Khojki"] = &unicode.Khojki
	decs["Khudawadi"] = &unicode.Khudawadi
	decs["L"] = &unicode.L
	decs["Lao"] = &unicode.Lao
	decs["Latin"] = &unicode.Latin
	decs["Lepcha"] = &unicode.Lepcha
	decs["Letter"] = &unicode.Letter
	decs["Limbu"] = &unicode.Limbu
	decs["Linear_A"] = &unicode.Linear_A
	decs["Linear_B"] = &unicode.Linear_B
	decs["Lisu"] = &unicode.Lisu
	decs["Ll"] = &unicode.Ll
	decs["Lm"] = &unicode.Lm
	decs["Lo"] = &unicode.Lo
	decs["Logical_Order_Exception"] = &unicode.Logical_Order_Exception
	decs["Lower"] = &unicode.Lower
	decs["LowerCase"] = native.UntypedNumericConst("1")
	decs["Lt"] = &unicode.Lt
	decs["Lu"] = &unicode.Lu
	decs["Lycian"] = &unicode.Lycian
	decs["Lydian"] = &unicode.Lydian
	decs["M"] = &unicode.M
	decs["Mahajani"] = &unicode.Mahajani
	decs["Makasar"] = &unicode.Makasar
	decs["Malayalam"] = &unicode.Malayalam
	decs["Mandaic"] = &unicode.Mandaic
	decs["Manichaean"] = &unicode.Manichaean
	decs["Marchen"] = &unicode.Marchen
	decs["Mark"] = &unicode.Mark
	decs["Masaram_Gondi"] = &unicode.Masaram_Gondi
	decs["MaxASCII"] = native.UntypedNumericConst("127")
	decs["MaxCase"] = native.UntypedNumericConst("3")
	decs["MaxLatin1"] = native.UntypedNumericConst("255")
	decs["MaxRune"] = native.UntypedNumericConst("1114111")
	decs["Mc"] = &unicode.Mc
	decs["Me"] = &unicode.Me
	decs["Medefaidrin"] = &unicode.Medefaidrin
	decs["Meetei_Mayek"] = &unicode.Meetei_Mayek
	decs["Mende_Kikakui"] = &unicode.Mende_Kikakui
	decs["Meroitic_Cursive"] = &unicode.Meroitic_Cursive
	decs["Meroitic_Hieroglyphs"] = &unicode.Meroitic_Hieroglyphs
	decs["Miao"] = &unicode.Miao
	decs["Mn"] = &unicode.Mn
	decs["Modi"] = &unicode.Modi
	decs["Mongolian"] = &unicode.Mongolian
	decs["Mro"] = &unicode.Mro
	decs["Multani"] = &unicode.Multani
	decs["Myanmar"] = &unicode.Myanmar
	decs["N"] = &unicode.N
	decs["Nabataean"] = &unicode.Nabataean
	decs["Nag_Mundari"] = &unicode.Nag_Mundari
	decs["Nandinagari"] = &unicode.Nandinagari
	decs["Nd"] = &unicode.Nd
	decs["New_Tai_Lue"] = &unicode.New_Tai_Lue
	decs["Newa"] = &unicode.Newa
	decs["Nko"] = &unicode.Nko
	decs["Nl"] = &unicode.Nl
	decs["No"] = &unicode.No
	decs["Noncharacter_Code_Point"] = &unicode.Noncharacter_Code_Point
	decs["Number"] = &unicode.Number
	decs["Nushu"] = &unicode.Nushu
	decs["Nyiakeng_Puachue_Hmong"] = &unicode.Nyiakeng_Puachue_Hmong
	decs["Ogham"] = &unicode.Ogham
	decs["Ol_Chiki"] = &unicode.Ol_Chiki
	decs["Old_Hungarian"] = &unicode.Old_Hungarian
	decs["Old_Italic"] = &unicode.Old_Italic
	decs["Old_North_Arabian"] = &unicode.Old_North_Arabian
	decs["Old_Permic"] = &unicode.Old_Permic
	decs["Old_Persian"] = &unicode.Old_Persian
	decs["Old_Sogdian"] = &unicode.Old_Sogdian
	decs["Old_South_Arabian"] = &unicode.Old_South_Arabian
	decs["Old_Turkic"] = &unicode.Old_Turkic
	decs["Old_Uyghur"] = &unicode.Old_Uyghur
	decs["Oriya"] = &unicode.Oriya
	decs["Osage"] = &unicode.Osage
	decs["Osmanya"] = &unicode.Osmanya
	decs["Other"] = &unicode.Other
	decs["Other_Alphabetic"] = &unicode.Other_Alphabetic
	decs["Other_Default_Ignorable_Code_Point"] = &unicode.Other_Default_Ignorable_Code_Point
	decs["Other_Grapheme_Extend"] = &unicode.Other_Grapheme_Extend
	decs["Other_ID_Continue"] = &unicode.Other_ID_Continue
	decs["Other_ID_Start"] = &unicode.Other_ID_Start
	decs["Other_Lowercase"] = &unicode.Other_Lowercase
	decs["Other_Math"] = &unicode.Other_Math
	decs["Other_Uppercase"] = &unicode.Other_Uppercase
	decs["P"] = &unicode.P
	decs["Pahawh_Hmong"] = &unicode.Pahawh_Hmong
	decs["Palmyrene"] = &unicode.Palmyrene
	decs["Pattern_Syntax"] = &unicode.Pattern_Syntax
	decs["Pattern_White_Space"] = &unicode.Pattern_White_Space
	decs["Pau_Cin_Hau"] = &unicode.Pau_Cin_Hau
	decs["Pc"] = &unicode.Pc
	decs["Pd"] = &unicode.Pd
	decs["Pe"] = &unicode.Pe
	decs["Pf"] = &unicode.Pf
	decs["Phags_Pa"] = &unicode.Phags_Pa
	decs["Phoenician"] = &unicode.Phoenician
	decs["Pi"] = &unicode.Pi
	decs["Po"] = &unicode.Po
	decs["Prepended_Concatenation_Mark"] = &unicode.Prepended_Concatenation_Mark
	decs["PrintRanges"] = &unicode.PrintRanges
	decs["Properties"] = &unicode.Properties
	decs["Ps"] = &unicode.Ps
	decs["Psalter_Pahlavi"] = &unicode.Psalter_Pahlavi
	decs["Punct"] = &unicode.Punct
	decs["Quotation_Mark"] = &unicode.Quotation_Mark
	decs["Radical"] = &unicode.Radical
	decs["Range16"] = reflect.TypeFor[unicode.Range16]()
	decs["Range32"] = reflect.TypeFor[unicode.Range32]()
	decs["RangeTable"] = reflect.TypeFor[unicode.RangeTable]()
	decs["Regional_Indicator"] = &unicode.Regional_Indicator
	decs["Rejang"] = &unicode.Rejang
	decs["ReplacementChar"] = native.UntypedNumericConst("65533")
	decs["Runic"] = &unicode.Runic
	decs["S"] = &unicode.S
	decs["STerm"] = &unicode.STerm
	decs["Samaritan"] = &unicode.Samaritan
	decs["Saurashtra"] = &unicode.Saurashtra
	decs["Sc"] = &unicode.Sc
	decs["Scripts"] = &unicode.Scripts
	decs["Sentence_Terminal"] = &unicode.Sentence_Terminal
	decs["Sharada"] = &unicode.Sharada
	decs["Shavian"] = &unicode.Shavian
	decs["Siddham"] = &unicode.Siddham
	decs["SignWriting"] = &unicode.SignWriting
	decs["SimpleFold"] = unicode.SimpleFold
	decs["Sinhala"] = &unicode.Sinhala
	decs["Sk"] = &unicode.Sk
	decs["Sm"] = &unicode.Sm
	decs["So"] = &unicode.So
	decs["Soft_Dotted"] = &unicode.Soft_Dotted
	decs["Sogdian"] = &unicode.Sogdian
	decs["Sora_Sompeng"] = &unicode.Sora_Sompeng
	decs["Soyombo"] = &unicode.Soyombo
	decs["Space"] = &unicode.Space
	decs["SpecialCase"] = reflect.TypeFor[unicode.SpecialCase]()
	decs["Sundanese"] = &unicode.Sundanese
	decs["Syloti_Nagri"] = &unicode.Syloti_Nagri
	decs["Symbol"] = &unicode.Symbol
	decs["Syriac"] = &unicode.Syriac
	decs["Tagalog"] = &unicode.Tagalog
	decs["Tagbanwa"] = &unicode.Tagbanwa
	decs["Tai_Le"] = &unicode.Tai_Le
	decs["Tai_Tham"] = &unicode.Tai_Tham
	decs["Tai_Viet"] = &unicode.Tai_Viet
	decs["Takri"] = &unicode.Takri
	decs["Tamil"] = &unicode.Tamil
	decs["Tangsa"] = &unicode.Tangsa
	decs["Tangut"] = &unicode.Tangut
	decs["Telugu"] = &unicode.Telugu
	decs["Terminal_Punctuation"] = &unicode.Terminal_Punctuation
	decs["Thaana"] = &unicode.Thaana
	decs["Thai"] = &unicode.Thai
	decs["Tibetan"] = &unicode.Tibetan
	decs["Tifinagh"] = &unicode.Tifinagh
	decs["Tirhuta"] = &unicode.Tirhuta
	decs["Title"] = &unicode.Title
	decs["TitleCase"] = native.UntypedNumericConst("2")
	decs["To"] = unicode.To
	decs["ToLower"] = unicode.ToLower
	decs["ToTitle"] = unicode.ToTitle
	decs["ToUpper"] = unicode.ToUpper
	decs["Toto"] = &unicode.Toto
	decs["TurkishCase"] = &unicode.TurkishCase
	decs["Ugaritic"] = &unicode.Ugaritic
	decs["Unified_Ideograph"] = &unicode.Unified_Ideograph
	decs["Upper"] = &unicode.Upper
	decs["UpperCase"] = native.UntypedNumericConst("0")
	decs["UpperLower"] = native.UntypedNumericConst("1114112")
	decs["Vai"] = &unicode.Vai
	decs["Variation_Selector"] = &unicode.Variation_Selector
	decs["Version"] = native.UntypedStringConst("15.0.0")
	decs["Vithkuqi"] = &unicode.Vithkuqi
	decs["Wancho"] = &unicode.Wancho
	decs["Warang_Citi"] = &unicode.Warang_Citi
	decs["White_Space"] = &unicode.White_Space
	decs["Yezidi"] = &unicode.Yezidi
	decs["Yi"] = &unicode.Yi
	decs["Z"] = &unicode.Z
	decs["Zanabazar_Square"] = &unicode.Zanabazar_Square
	decs["Zl"] = &unicode.Zl
	decs["Zp"] = &unicode.Zp
	decs["Zs"] = &unicode.Zs
	packages["unicode"] = native.Package{
		Name:         "unicode",
		Declarations: decs,
	}
	// "unicode/utf16"
	decs = make(native.Declarations, 6)
	decs["AppendRune"] = utf16.AppendRune
	decs["Decode"] = utf16.Decode
	decs["DecodeRune"] = utf16.DecodeRune
	decs["Encode"] = utf16.Encode
	decs["EncodeRune"] = utf16.EncodeRune
	decs["IsSurrogate"] = utf16.IsSurrogate
	packages["unicode/utf16"] = native.Package{
		Name:         "utf16",
		Declarations: decs,
	}
	// "unicode/utf8"
	decs = make(native.Declarations, 19)
	decs["AppendRune"] = utf8.AppendRune
	decs["DecodeLastRune"] = utf8.DecodeLastRune
	decs["DecodeLastRuneInString"] = utf8.DecodeLastRuneInString
	decs["DecodeRune"] = utf8.DecodeRune
	decs["DecodeRuneInString"] = utf8.DecodeRuneInString
	decs["EncodeRune"] = utf8.EncodeRune
	decs["FullRune"] = utf8.FullRune
	decs["FullRuneInString"] = utf8.FullRuneInString
	decs["MaxRune"] = native.UntypedNumericConst("1114111")
	decs["RuneCount"] = utf8.RuneCount
	decs["RuneCountInString"] = utf8.RuneCountInString
	decs["RuneError"] = native.UntypedNumericConst("65533")
	decs["RuneLen"] = utf8.RuneLen
	decs["RuneSelf"] = native.UntypedNumericConst("128")
	decs["RuneStart"] = utf8.RuneStart
	decs["UTFMax"] = native.UntypedNumericConst("4")
	decs["Valid"] = utf8.Valid
	decs["ValidRune"] = utf8.ValidRune
	decs["ValidString"] = utf8.ValidString
	packages["unicode/utf8"] = native.Package{
		Name:         "utf8",
		Declarations: decs,
	}
	initLegacy(packages) // bring in the old v3 packages
}
