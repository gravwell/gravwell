package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/kinesis"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

func main() {
	cfg := &Config{}
	RegisterFlags(flag.CommandLine, cfg)
	flag.Parse()

	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		flag.Usage()
		os.Exit(1)
	}

	ctx := context.Background()
	logf := log.Printf
	summary := &Summary{}

	var wg sync.WaitGroup

	if cfg.S3Enabled() {
		pub, err := buildS3Publisher(ctx, cfg, logf)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error creating S3 client: %v\n", err)
			os.Exit(1)
		}
		wg.Go(func() {
			runPublisher(ctx, pub, cfg.Buckets, cfg.NumEvents, summary, logf)
		})
	}

	if cfg.SQSEnabled() {
		pub, err := buildSQSPublisher(ctx, cfg, logf)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error creating SQS client: %v\n", err)
			os.Exit(1)
		}
		wg.Go(func() {
			runPublisher(ctx, pub, cfg.SQSQueues, cfg.NumEvents, summary, logf)
		})
	}

	if cfg.KinesisEnabled() {
		pub, err := buildKinesisPublisher(ctx, cfg, logf)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error creating Kinesis client: %v\n", err)
			os.Exit(1)
		}
		wg.Go(func() {
			runPublisher(ctx, pub, cfg.KinesisStreams, cfg.NumEvents, summary, logf)
		})
	}

	wg.Wait()
	fmt.Print(summary.String())
}

// runPublisher runs a publisher against each resource and collects results.
func runPublisher(ctx context.Context, pub Publisher, resources []string, numEvents int, summary *Summary, logf func(string, ...any)) {
	for _, resource := range resources {
		logf("[%s] publishing %d events to %s", pub.ServiceName(), numEvents, resource)
		events := GenerateEvents(numEvents)
		result, err := pub.Publish(ctx, resource, events)
		if err != nil {
			logf("[%s] error: %v", pub.ServiceName(), err)
		}
		summary.Add(result)
	}
}

func buildS3Publisher(ctx context.Context, cfg *Config, logf func(string, ...any)) (*S3Publisher, error) {
	awsCfg, err := loadAWSConfig(ctx, cfg.S3Profile, cfg.S3Endpoint)
	if err != nil {
		return nil, err
	}
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if cfg.S3Endpoint != "" {
			o.BaseEndpoint = &cfg.S3Endpoint
			o.UsePathStyle = true
		}
	})
	return NewS3Publisher(client, cfg.Verbose, logf), nil
}

func buildSQSPublisher(ctx context.Context, cfg *Config, logf func(string, ...any)) (*SQSPublisher, error) {
	awsCfg, err := loadAWSConfig(ctx, cfg.SQSProfile, cfg.SQSEndpoint)
	if err != nil {
		return nil, err
	}
	client := sqs.NewFromConfig(awsCfg, func(o *sqs.Options) {
		if cfg.SQSEndpoint != "" {
			o.BaseEndpoint = &cfg.SQSEndpoint
		}
	})
	return NewSQSPublisher(client, cfg.Verbose, logf), nil
}

func buildKinesisPublisher(ctx context.Context, cfg *Config, logf func(string, ...any)) (*KinesisPublisher, error) {
	awsCfg, err := loadAWSConfig(ctx, cfg.KinesisProfile, cfg.KinesisEndpoint)
	if err != nil {
		return nil, err
	}
	client := kinesis.NewFromConfig(awsCfg, func(o *kinesis.Options) {
		if cfg.KinesisEndpoint != "" {
			o.BaseEndpoint = &cfg.KinesisEndpoint
		}
	})
	return NewKinesisPublisher(client, cfg.Verbose, logf), nil
}

func loadAWSConfig(ctx context.Context, profile, endpoint string) (aws.Config, error) {
	opts := []func(*config.LoadOptions) error{}
	if profile != "" {
		opts = append(opts, config.WithSharedConfigProfile(profile))
	}
	return config.LoadDefaultConfig(ctx, opts...)
}
