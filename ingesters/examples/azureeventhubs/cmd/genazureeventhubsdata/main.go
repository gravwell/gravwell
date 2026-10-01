package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	eventhubs "github.com/Azure/azure-sdk-for-go/sdk/messaging/azeventhubs/v2"
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
	for _, hub := range cfg.EventHubs {
		client, err := buildProducerClient(cfg, hub)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error creating Event Hubs client for hub %s: %v\n", hub, err)
			os.Exit(1)
		}

		wg.Go(func() {
			defer closeClient(client, hub, logf)

			pub := NewEventHubsPublisher(&producerClientAdapter{client: client}, cfg.Verbose, logf)
			logf("publishing %d events to hub %s", cfg.NumEvents, hub)
			events := GenerateEvents(cfg.NumEvents)
			result, err := pub.Publish(ctx, hub, events)
			if err != nil {
				logf("[%s] error: %v", hub, err)
			}
			summary.Add(result)
		})
	}

	wg.Wait()
	fmt.Print(summary.String())
}

// buildProducerClient connects to the local Event Hubs emulator using the
// same UseDevelopmentEmulator=true connection string shape the emulator
// requires (no EntityPath, since the emulator's SAS key is per-namespace
// rather than per-hub).
func buildProducerClient(cfg *Config, hub string) (*eventhubs.ProducerClient, error) {
	connStr := buildConnectionString(cfg)
	return eventhubs.NewProducerClientFromConnectionString(connStr, hub, nil)
}

func buildConnectionString(cfg *Config) string {
	return fmt.Sprintf(
		"Endpoint=sb://%s;SharedAccessKeyName=%s;SharedAccessKey=%s;UseDevelopmentEmulator=true;",
		cfg.Endpoint, cfg.TokenName, cfg.TokenKey,
	)
}

func closeClient(client *eventhubs.ProducerClient, hub string, logf func(string, ...any)) {
	cctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Close(cctx); err != nil {
		logf("[%s] error closing client: %v", hub, err)
	}
}
