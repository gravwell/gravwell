package mimecast

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

const (
	defaultBaseDomain = "https://api.services.mimecast.com"
)

type LegacyConfig struct {
	Ingester_UUID string
	StartTime     time.Time
	ClientID      string
	ClientSecret  string
	MimecastAPI   Api
	Tag_Name      string
	Preprocessor  []string
	RateLimit     int
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

type Config struct {
	Ingester_UUID string
	Lookback      time.Duration
	Client_Id     string
	Client_Secret string
	Api           []Api
	Host          string
	Tag_Name      string
	Preprocessor  []string
	Interval      int
}

func (c *Config) Verify() error {
	if c.Host == "" {
		c.Host = defaultBaseDomain
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

func (c *Config) Tags() []string {
	return []string{c.Tag_Name}
}
