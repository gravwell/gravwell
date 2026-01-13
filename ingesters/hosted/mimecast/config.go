package mimecast

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

const (
	defaultBaseDomain = "https://api.services.mimecast.com"
	defaultLookback   = 24 * time.Hour
)

type LegacyConfig struct {
	Ingester_UUID string
	StartTime     time.Time
	ClientID      string `json:"-"`
	ClientSecret  string `json:"-"`
	MimecastAPI   Api
	Tag_Name      string
	Preprocessor  []string
	RateLimit     int // requests per minute
}

func (l *LegacyConfig) Verify() error {
	if l.Tag_Name == "" {
		return errors.New("Tag-Name not specified")
	}
	if l.ClientID == "" {
		return errors.New("ClientID not specified")
	}
	if l.ClientSecret == "" {
		return errors.New("ClientSecret not specified")
	}
	if l.StartTime.IsZero() {
		l.StartTime = time.Now()
	}
	if _, ok := SIEMApiEvents[l.MimecastAPI]; !ok && l.MimecastAPI != AuditApi {
		return errors.New("Mimecast API is not valid")
	}

	return nil
}

func (l *LegacyConfig) UUID() uuid.UUID {
	if l.Ingester_UUID != "" {
		if r, err := uuid.Parse(l.Ingester_UUID); err == nil {
			return r
		}
	}
	return uuid.Nil
}

func (l *LegacyConfig) Tags() []string {
	return []string{l.Tag_Name}
}

type Config struct {
	Ingester_UUID string
	Lookback      time.Duration
	Client_Id     string `json:"-"`
	Client_Secret string `json:"-"`
	Api           []Api
	Host          string
	Tag_Prefix    string
	Preprocessor  []string
	Rate_Limit    int // Request per minute
}

func (c *Config) Verify() error {
	if c.Host == "" {
		c.Host = defaultBaseDomain
	}
	if c.Lookback == 0 {
		c.Lookback = defaultLookback
	}
	if c.Client_Id == "" {
		return errors.New("Client-Id not specified")
	}
	if c.Client_Secret == "" {
		return errors.New("Client-Secret not specified")
	}
	for _, api := range c.Api {
		if _, supported := SIEMApiEvents[api]; !supported && api != AuditApi {
			return fmt.Errorf("API '%s' is not supported", api)
		}
	}
	return nil
}

func (c *Config) UUID() uuid.UUID {
	if c.Ingester_UUID != "" {
		if r, err := uuid.Parse(c.Ingester_UUID); err == nil {
			return r
		}
	}
	return uuid.Nil
}

func (c *Config) Tags() (tags []string) {
	for _, api := range c.Api {
		tags = append(tags, c.Tag_Prefix+string(api))
	}
	return
}
