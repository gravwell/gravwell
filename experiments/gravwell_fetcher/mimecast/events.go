package mimecast

import "time"

type EventType string

const (
	DeliveryEvent             EventType = "delivery"
	ReceiptEvent              EventType = "receipt"
	ProcessEvent              EventType = "process"
	AVEvent                   EventType = "av"
	SpamEvent                 EventType = "spam"
	InternalEmailProtectEvent EventType = "internal email protect"
	ImpersonationProtectEvent EventType = "impersonation protect"
	UrlProtectEvent           EventType = "url protect"
	AttachmentProtectEvent    EventType = "attachment protect"
	JournalEvent              EventType = "journal"
)

type Api string

const (
	AuditApi            = "audit"
	MtaDeliveryApi      = "mta-delivery"
	MtaReceiptApi       = "mta-receipt"
	MtaProcessApi       = "mta-process"
	MtaAvApi            = "mta-av"
	MtaSpamApi          = "mta-spam"
	MtaInternalApi      = "mta-internal"
	MtaImpersonationApi = "mta-impersonation"
	MtaUrlApi           = "mta-url"
	MtaAttachmentApi    = "mta-attachment"
	MtaJournal          = "mta-journal"
)

var ApiEvents = map[Api]EventType{
	MtaDeliveryApi:      DeliveryEvent,
	MtaReceiptApi:       ReceiptEvent,
	MtaProcessApi:       ProcessEvent,
	MtaAvApi:            AVEvent,
	MtaSpamApi:          SpamEvent,
	MtaInternalApi:      InternalEmailProtectEvent,
	MtaImpersonationApi: ImpersonationProtectEvent,
	MtaUrlApi:           UrlProtectEvent,
	MtaAttachmentApi:    AttachmentProtectEvent,
	MtaJournal:          JournalEvent,
}

type SIEMBatchEventResponse struct {
	Value []struct {
		URL    string    `json:"url"`
		Expiry time.Time `json:"expiry,omitempty"`
		Size   int       `json:"size"`
	} `json:"value"`
	NextPage   string `json:"@nextPage"`
	IsCaughtUp bool   `json:"isCaughtUp"`
}

// MtaEventData is the minimum representation of all events returned.
// We only need to extract the timestamp for creting the entry.
// Everything else we pass along as the original byte slice, unchanged.
type MtaEventData struct {
	Timestamp int64 `json:"timestamp"`
}

type AuditData struct {
	EventTime string `json:"eventTime"`
}
