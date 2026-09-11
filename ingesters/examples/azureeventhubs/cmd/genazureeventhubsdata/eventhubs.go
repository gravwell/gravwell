package main

import (
	"context"
	"errors"
	"fmt"

	eventhubs "github.com/Azure/azure-sdk-for-go/sdk/messaging/azeventhubs/v2"
)

// EventBatch is the minimal batch surface used by EventHubsPublisher. It is
// satisfied directly by *eventhubs.EventDataBatch.
type EventBatch interface {
	AddEventData(ed *eventhubs.EventData, options *eventhubs.AddEventDataOptions) error
	NumEvents() int32
}

// EventHubsAPI defines the subset of *eventhubs.ProducerClient used by
// EventHubsPublisher. It returns EventBatch rather than
// *eventhubs.EventDataBatch directly so tests can exercise the batching/retry
// logic in Publish without a live emulator connection: EventDataBatch can
// only be constructed by the real SDK client, which requires a network round
// trip to negotiate the link's max message size.
type EventHubsAPI interface {
	NewEventDataBatch(ctx context.Context, options *eventhubs.EventDataBatchOptions) (EventBatch, error)
	SendEventDataBatch(ctx context.Context, batch EventBatch, options *eventhubs.SendEventDataBatchOptions) error
}

// producerClientAdapter adapts a real *eventhubs.ProducerClient to the
// EventHubsAPI interface.
type producerClientAdapter struct {
	client *eventhubs.ProducerClient
}

func (a *producerClientAdapter) NewEventDataBatch(ctx context.Context, options *eventhubs.EventDataBatchOptions) (EventBatch, error) {
	return a.client.NewEventDataBatch(ctx, options)
}

func (a *producerClientAdapter) SendEventDataBatch(ctx context.Context, batch EventBatch, options *eventhubs.SendEventDataBatchOptions) error {
	realBatch, ok := batch.(*eventhubs.EventDataBatch)
	if !ok {
		return fmt.Errorf("unexpected batch type %T", batch)
	}
	return a.client.SendEventDataBatch(ctx, realBatch, options)
}

// EventHubsPublisher publishes events as EventData batches to an Event Hub.
type EventHubsPublisher struct {
	client  EventHubsAPI
	verbose bool
	logf    func(string, ...any)
}

// NewEventHubsPublisher creates a new EventHubsPublisher.
func NewEventHubsPublisher(client EventHubsAPI, verbose bool, logf func(string, ...any)) *EventHubsPublisher {
	return &EventHubsPublisher{
		client:  client,
		verbose: verbose,
		logf:    logf,
	}
}

// Publish batches and sends events to the given event hub. Because a single
// [eventhubs.EventDataBatch] has a maximum size, events are packed into as
// few batches as will fit and each full batch is sent as soon as another
// event no longer fits.
func (p *EventHubsPublisher) Publish(ctx context.Context, hub string, events []Event) (Result, error) {
	result := Result{Resource: hub}

	if len(events) == 0 {
		return result, nil
	}

	batch, err := p.client.NewEventDataBatch(ctx, nil)
	if err != nil {
		result.Err = fmt.Errorf("failed to create batch for hub %s: %w", hub, err)
		return result, result.Err
	}

	send := func(b EventBatch) {
		n := b.NumEvents()
		if n == 0 {
			return
		}
		if p.verbose {
			p.logf("  sending batch of %d events to hub %s", n, hub)
		}
		if err := p.client.SendEventDataBatch(ctx, b, nil); err != nil {
			result.Failed += int(n)
			if result.Err == nil {
				result.Err = fmt.Errorf("failed to send batch to hub %s: %w", hub, err)
			}
			return
		}
		result.Sent += int(n)
	}

	for _, e := range events {
		ed := &eventhubs.EventData{Body: []byte(e.String())}

		if err := batch.AddEventData(ed, nil); err != nil {
			if !errors.Is(err, eventhubs.ErrEventDataTooLarge) {
				result.Failed++
				if result.Err == nil {
					result.Err = fmt.Errorf("failed to add event to batch for hub %s: %w", hub, err)
				}
				continue
			}

			// Batch is full: send what we have and start a fresh one for this event.
			send(batch)
			newBatch, err := p.client.NewEventDataBatch(ctx, nil)
			if err != nil {
				result.Failed++
				if result.Err == nil {
					result.Err = fmt.Errorf("failed to create batch for hub %s: %w", hub, err)
				}
				continue
			}
			batch = newBatch

			if err := batch.AddEventData(ed, nil); err != nil {
				result.Failed++
				if result.Err == nil {
					result.Err = fmt.Errorf("event too large to fit in an empty batch for hub %s: %w", hub, err)
				}
			}
		}
	}

	send(batch)

	if result.Failed > 0 && result.Sent > 0 {
		// Partial failure — don't return error so we can still report the result.
		return result, nil
	}
	return result, result.Err
}
