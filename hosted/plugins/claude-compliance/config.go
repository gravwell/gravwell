package claudecompliance

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v3/hosted"
	"github.com/gravwell/gravwell/v3/ingest"
	"net/url"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	Name    = "claudecompliance"
	ID      = "claudecompliance.ingesters.gravwell.io"
	Version = "1.0.0"
)

var _ hosted.Config = (*Config)(nil)

type lookbackHours int64

func (h *lookbackHours) UnmarshalText(text []byte) error {
	s := strings.TrimSpace(string(text))
	const invalid = "Lookback must use whole hours and days, for example 24, 24h, 1d, or 1d12h"
	maxHours := uint64((1<<63)-1) / uint64(time.Hour)
	if s == "" || strings.ContainsAny(s, "+-.") {
		return errors.New(invalid)
	}
	if !strings.ContainsAny(s, "dh") {
		v, err := strconv.ParseUint(s, 10, 64)
		if err != nil || v > maxHours {
			return errors.New("Lookback must be a positive, representable number of hours")
		}
		*h = lookbackHours(v)
		return nil
	}
	m := lookbackDuration.FindStringSubmatch(s)
	if m == nil || (m[1] == "" && m[2] == "") {
		return errors.New(invalid)
	}
	var days, hours uint64
	var err error
	if m[1] != "" {
		days, err = strconv.ParseUint(m[1], 10, 64)
		if err != nil || days > maxHours/24 {
			return errors.New("Lookback must be a positive, representable number of hours")
		}
	}
	if m[2] != "" {
		hours, err = strconv.ParseUint(m[2], 10, 64)
		if err != nil || hours > maxHours-days*24 {
			return errors.New("Lookback must be a positive, representable number of hours")
		}
	}
	*h = lookbackHours(days*24 + hours)
	return nil
}

type Config struct {
	hosted.BaseConfig
	hosted.PollingConfig
	Lookback                                                                                lookbackHours
	Dataset                                                                                 string
	Credential_File                                                                         string `json:"-"`
	Scope_Identity                                                                          string
	Parameter                                                                               []string
	Tag_Name                                                                                string
	Page_Size, Max_Pages, Max_Response_Bytes, Max_Entry_Bytes, Max_Retries, Overlap_Seconds int
	Start_Time                                                                              string
	Follow_Children                                                                         string
	Max_Children, Max_Pending                                                               int
	dataset                                                                                 Dataset
	path                                                                                    string
	discovered                                                                              bool
}

var (
	placeholder      = regexp.MustCompile(`\{([a-z_]+)\}`)
	lookbackDuration = regexp.MustCompile(`^(?:([0-9]+)d)?(?:([0-9]+)h)?$`)
)

func (c *Config) Verify() error {
	if c.Lookback == 0 {
		c.Lookback = 24
	}
	if c.Lookback < 1 || uint64(c.Lookback) > uint64((1<<63-1)/time.Hour) {
		return errors.New("Lookback must be a positive, representable number of hours")
	}
	c.PollingConfig.Lookback = int(c.Lookback)
	c.PollingConfig.ApplyDefaults(int(c.Lookback), 60, 300)
	if c.UUID() == uuid.Nil {
		return errors.New("Ingester-UUID must be nonzero and persistent")
	}
	d, ok := lookup(c.Dataset)
	if !ok {
		return errors.New("unknown Compliance JSON dataset")
	}
	c.dataset = d
	if c.Follow_Children == "" {
		c.Follow_Children = "enabled"
	}
	if c.Follow_Children != "enabled" && c.Follow_Children != "disabled" {
		return errors.New("Follow-Children must be enabled or disabled")
	}
	if c.Max_Children == 0 {
		c.Max_Children = 100
	}
	if c.Max_Pending == 0 {
		c.Max_Pending = 10000
	}
	if c.Max_Children < 1 || c.Max_Children > 1000 || c.Max_Pending < c.Max_Children || c.Max_Pending > 100000 {
		return errors.New("invalid child-work bounds")
	}
	if c.Scope_Identity == "" {
		return errors.New("Scope-Identity must identify the configured key scope without a secret")
	}
	if _, e := credential(c.Credential_File); e != nil {
		return e
	}
	params := map[string]string{}
	for _, s := range c.Parameter {
		k, v, ok := strings.Cut(s, ":")
		if !ok || k == "" || v == "" || strings.ContainsAny(v, "/\\?#") {
			return errors.New("Parameter must contain name:opaque-id")
		}
		if _, exists := params[k]; exists {
			return errors.New("duplicate Parameter")
		}
		params[k] = v
	}
	p := d.Path
	for _, m := range placeholder.FindAllStringSubmatch(p, -1) {
		v, ok := params[m[1]]
		if !ok {
			return fmt.Errorf("missing Parameter %s", m[1])
		}
		p = strings.ReplaceAll(p, m[0], url.PathEscape(v))
		delete(params, m[1])
	}
	if len(params) > 0 {
		return errors.New("unused Parameter")
	}
	c.path = "/v1/compliance" + p
	if c.Tag_Name == "" {
		c.Tag_Name = "claude-compliance-" + d.Tag
	}
	if e := ingest.CheckTag(c.Tag_Name); e != nil {
		return e
	}
	if c.Page_Size == 0 {
		c.Page_Size = 100
	}
	if c.Page_Size < 1 || (d.Limit > 0 && c.Page_Size > d.Limit) {
		return errors.New("Page-Size outside endpoint limit")
	}
	if c.Max_Pages == 0 {
		c.Max_Pages = 1000
	}
	if c.Max_Pages < 1 || c.Max_Pages > 10000 {
		return errors.New("Max-Pages outside 1..10000")
	}
	if c.Max_Response_Bytes == 0 {
		c.Max_Response_Bytes = 16 << 20
	}
	if c.Max_Entry_Bytes == 0 {
		c.Max_Entry_Bytes = 4 << 20
	}
	if c.Max_Entry_Bytes < 1 || c.Max_Response_Bytes < c.Max_Entry_Bytes || c.Max_Response_Bytes > 64<<20 {
		return errors.New("invalid response/entry bounds")
	}
	if c.Max_Retries == 0 {
		c.Max_Retries = 4
	}
	if c.Max_Retries < 0 || c.Max_Retries > 10 {
		return errors.New("invalid Max-Retries")
	}
	if c.Overlap_Seconds == 0 {
		c.Overlap_Seconds = 300
	}
	if c.Overlap_Seconds < 1 || c.Overlap_Seconds > 86400 {
		return errors.New("invalid overlap")
	}
	if c.Requests_Per_Minute < 1 || c.Requests_Per_Minute > 600 || c.Request_Interval < 60 {
		return errors.New("invalid rate or interval")
	}
	if c.Start_Time != "" {
		if _, e := time.Parse(time.RFC3339, c.Start_Time); e != nil {
			return errors.New("Start-Time requires RFC3339 offset")
		}
	}
	return nil
}
func (c *Config) Tags() []string { return []string{c.Tag_Name} }
func (c *Config) Equal(other any) bool {
	n, ok := hosted.EqualTarget[Config](other)
	return c != nil && ok && reflect.DeepEqual(c, n)
}
func (c *Config) SanitizedConfig() any { return struct{ Dataset, Tag string }{c.Dataset, c.Tag_Name} }
func (c *Config) key() string {
	s := sha256.Sum256([]byte(c.Scope_Identity + "\x00" + c.path + "\x00" + c.Start_Time + "\x00" + c.Tag_Name))
	return "claude-compliance-v1/" + hex.EncodeToString(s[:])
}
func credential(p string) (string, error) {
	st, e := os.Stat(p)
	if e != nil || !st.Mode().IsRegular() || st.Size() > 16384 {
		return "", errors.New("invalid Compliance credential file")
	}
	b, e := os.ReadFile(p)
	if e != nil {
		return "", errors.New("cannot read Compliance credential file")
	}
	if len(b) > 16384 {
		return "", errors.New("invalid Compliance credential file")
	}
	v := strings.TrimSpace(string(b))
	if v == "" || strings.ContainsAny(v, "\r\n") {
		return "", errors.New("invalid Compliance credential")
	}
	return v, nil
}
