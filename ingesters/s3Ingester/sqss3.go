package main

import (
	"context"
	"fmt"
	"net"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingest/processors"
	"github.com/gravwell/gravwell/v3/sqs_common"
	"github.com/gravwell/gravwell/v3/timegrinder"
)

type SQSS3Config struct {
	TimeConfig
	Verbose          bool
	MaxLineSize      int
	Name             string
	Tag              entry.EntryTag
	TagName          string
	SourceOverride   string
	Proc             *processors.ProcessorSet
	TG               *timegrinder.TimeGrinder
	Logger           *log.Logger
	FileFilters      []string
	Region           string
	Queue            string
	Endpoint         string
	Reader           string
	ID               string `json:"-"` //do not ship this as part of a config report
	Secret           string `json:"-"` //do not ship this as part of a config report
	Credentials_Type string
	AttachMetadata   bool

	// Credentials-Type=assumerole only
	Role_ARN                string
	External_ID             string `json:"-"` //do not ship this as part of a config report
	Source_Credentials_Type string
}

func (s *SQSS3Config) credentialsConfig() sqs_common.CredentialsConfig {
	return sqs_common.CredentialsConfig{
		Type:        s.Credentials_Type,
		ID:          s.ID,
		Secret:      s.Secret,
		RoleARN:     s.Role_ARN,
		ExternalID:  s.External_ID,
		SourceType:  s.Source_Credentials_Type,
		Region:      s.Region,
		SessionName: roleSessionName,
		Logger:      s.Logger,
	}
}

type SQSS3Listener struct {
	SQSS3Config
	sqs    *sqs_common.SQS
	svc    s3Handler
	tg     timegrinder.TimeGrinder
	src    net.IP
	rdr    reader
	filter *matcher
	creds  aws.CredentialsProvider
}

// checkCredentials reports an assumerole misconfiguration; it does nothing for other credential types.
func (s *SQSS3Listener) checkCredentials(ctx context.Context) error {
	return s.credentialsConfig().CheckAssumeRole(ctx, s.creds)
}

func NewSQSS3Listener(ctx context.Context, cfg SQSS3Config) (s *SQSS3Listener, err error) {
	var rdr reader
	if err = cfg.validate(); err != nil {
		return
	}

	if cfg.MaxLineSize <= 0 {
		cfg.MaxLineSize = defaultMaxLineSize
	}

	var filter *matcher
	if filter, err = newMatcher(cfg.FileFilters); err != nil {
		return
	}

	if rdr, err = parseReader(cfg.Reader); err != nil {
		return
	}

	c, err := cfg.credentialsConfig().Provider(ctx)
	if err != nil {
		return nil, err
	}

	loadOpts := []func(*config.LoadOptions) error{
		config.WithRegion(cfg.Region),
	}
	if c != nil {
		loadOpts = append(loadOpts, config.WithCredentialsProvider(c))
	}
	awsCfg, err := config.LoadDefaultConfig(ctx, loadOpts...)
	if err != nil {
		return nil, fmt.Errorf("load default aws config: %w", err)
	}

	var s3Opts []func(*s3.Options)
	if cfg.Endpoint != "" {
		s3Opts = append(s3Opts, func(o *s3.Options) {
			o.BaseEndpoint = new(cfg.Endpoint)
		})
	}

	sqs, err := sqs_common.SQSListener(&sqs_common.Config{
		Queue:       cfg.Queue,
		Region:      cfg.Region,
		Endpoint:    cfg.Endpoint,
		Credentials: c,
	})
	if err != nil {
		return nil, err
	}

	s3Svc := s3.NewFromConfig(awsCfg, s3Opts...)

	s = &SQSS3Listener{
		SQSS3Config: cfg,
		sqs:         sqs,
		svc:         s3Svc,
		src:         cfg.srcOverride(),
		rdr:         rdr,
		filter:      filter,
		creds:       c,
	}
	return
}

func (s *SQSS3Config) srcOverride() net.IP {
	if s.SourceOverride == `` {
		return nil
	}
	return net.ParseIP(s.SourceOverride)
}

func (s SQSS3Config) Log(vals ...any) {
	if s.Logger == nil || len(vals) == 0 {
		return
	}
	s.Logger.Info(fmt.Sprint(vals...))
}
