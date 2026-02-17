package mimecast

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
)

const (
	defaultBaseDomain        = "https://api.services.mimecast.com"
	defaultLookback          = 24
	defaultRequestsPerMinute = 5
	defaultInterval          = 5
)

type Config struct {
	Ingester_UUID       string
	Lookback            int    // in hours
	Client_Id           string `json:"-"`
	Client_Secret       string `json:"-"`
	Api                 []Api
	Host                string
	Tag_Name            string
	Tag_Prefix          string
	Preprocessor        []string
	Requests_Per_Minute int
	Interval            int // in seconds
}

func (c *Config) Verify() error {
	if c.Host == "" {
		c.Host = defaultBaseDomain
	}
	if c.Lookback <= 0 {
		c.Lookback = defaultLookback
	}
	if c.Requests_Per_Minute <= 0 {
		c.Requests_Per_Minute = defaultRequestsPerMinute
	}
	if c.Interval <= 0 {
		c.Interval = defaultInterval
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
	if c.Tag_Name != "" && len(c.Api) > 1 {
		return fmt.Errorf("Tag-Name '%s' is only supported when specifying a single API", c.Tag_Name)
	}
	if c.Tag_Prefix != "" && c.Tag_Name != "" {
		return fmt.Errorf("Tag-Prefix cannot be used with Tag-Name")
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
		tags = append(tags, api.Tag(c.Tag_Name, c.Tag_Prefix))
	}
	return
}
