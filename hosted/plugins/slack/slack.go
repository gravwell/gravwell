// Package slack implements polling for the Slack Audit Logs API.
package slack

import (
	"github.com/gravwell/gravwell/v3/hosted"
	"github.com/gravwell/gravwell/v3/hosted/plugins/slack/internal/restpoller"
)

const (
	Name    = "slack"
	ID      = "slack.ingesters.gravwell.io"
	Version = "1.0.0"
)

type Config struct{ restpoller.Config }

func (c *Config) bind()         { c.SetProduct("slack") }
func (c *Config) Verify() error { c.bind(); return c.Config.Verify() }
func (c *Config) Tags() []string {
	c.bind()
	return c.Config.Tags()
}
func New(c *Config) hosted.Job { c.bind(); return restpoller.New(&c.Config) }
