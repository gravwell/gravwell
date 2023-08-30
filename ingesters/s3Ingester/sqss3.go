package main

import (
	"fmt"
	"net"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingest/processors"
	"github.com/gravwell/gravwell/v3/sqs_common"
	"github.com/gravwell/gravwell/v3/timegrinder"
)

type SQSS3Config struct {
	TimeConfig
	Verbose        bool
	MaxLineSize    int
	Name           string
	Tag            entry.EntryTag
	TagName        string
	SourceOverride string
	Proc           *processors.ProcessorSet
	TG             *timegrinder.TimeGrinder
	Logger         *log.Logger
	Region         string
	AKID           string
	Secret         string
	Queue          string
	Reader         string
}

type SQSS3Listener struct {
	SQSS3Config
	sqs     *sqs_common.SQS
	session *session.Session
	svc     *s3.S3
	tg      timegrinder.TimeGrinder
	src     net.IP
	rdr     reader
}

func NewSQSS3Listener(cfg SQSS3Config) (s *SQSS3Listener, err error) {
	var rdr reader
	var sess *session.Session
	if err = cfg.validate(); err != nil {
		return
	}

	if rdr, err = parseReader(cfg.Reader); err != nil {
		return
	}

	sess, err = session.NewSession(&aws.Config{
		Region:      aws.String(cfg.Region),
		Credentials: credentials.NewStaticCredentials(cfg.AKID, cfg.Secret, ""),
	})
	if err != nil {
		return nil, err
	}

	sqs, err := sqs_common.SQSListener(&sqs_common.Config{
		Queue:  cfg.Queue,
		Region: cfg.Region,
		AKID:   cfg.AKID,
		Secret: cfg.Secret,
	})
	if err != nil {
		return nil, err
	}

	s = &SQSS3Listener{
		SQSS3Config: cfg,
		session:     sess,
		sqs:         sqs,
		svc:         s3.New(sess),
		src:         cfg.srcOverride(),
		rdr:         rdr,
	}
	return
}

func (s *SQSS3Config) srcOverride() net.IP {
	if s.SourceOverride == `` {
		return nil
	}
	return net.ParseIP(s.SourceOverride)
}

// Process reads the object in and processes its contents
func (s *SQSS3Listener) Process(obj *s3.Object) (err error) {
	//return s.ProcessContext(obj, nil)
	return nil
}

func (s SQSS3Config) Log(vals ...interface{}) {
	if s.Logger == nil || len(vals) == 0 {
		return
	}
	s.Logger.Info(fmt.Sprint(vals...))
}
