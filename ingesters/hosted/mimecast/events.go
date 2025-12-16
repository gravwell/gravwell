package mimecast

import (
	"encoding/json"
	"time"
)

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

var SIEMApiEvents = map[Api]EventType{
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

type RequestData struct {
	EndDateTime   string `json:"endDateTime,omitempty"`
	StartDateTime string `json:"startDateTime,omitempty"`
}

type RequestMeta struct {
	Pagination struct {
		PageSize  int    `json:"pageSize,omitempty"`
		PageToken string `json:"pageToken,omitempty"`
	} `json:"pagination,omitzero"`
}

type Request struct {
	Meta RequestMeta   `json:"meta,omitzero"`
	Data []RequestData `json:"data,omitempty"`
}

type ResponseError struct {
	Code      string `json:"code,omitempty"`
	Message   string `json:"message,omitempty"`
	Retryable bool   `json:"retryable,omitempty"`
}

type ResponseFailure struct {
	Errors []ResponseError `json:"errors,omitempty"`
}

type ResponseMeta struct {
	Pagination struct {
		PageSize int    `json:"pageSize,omitempty"`
		Next     string `json:"next,omitempty"`
	} `json:"pagination,omitzero"`
}

type Response struct {
	Meta ResponseMeta      `json:"meta,omitzero"`
	Data []json.RawMessage `json:"data,omitempty"`
	Fail []ResponseFailure `json:"fail,omitempty"`
}
