/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"time"

	"github.com/crewjam/rfc5424"
	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/timegrinder"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	collogs "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	cpb "go.opentelemetry.io/proto/otlp/common/v1"
	lpb "go.opentelemetry.io/proto/otlp/logs/v1"
	rpb "go.opentelemetry.io/proto/otlp/resource/v1"
)

const (
	defaultOtelLogsURL = `/v1/logs`
)

// otelLogsListener defines the configuration for an OpenTelemetry logs listener
type otelLogsListener struct {
	auth              //authentication information
	URL               string
	Tag_Name          string
	Ignore_Timestamps bool
	Debug_Posts       bool
	Disable_EVs       bool
	Preprocessor      []string
}
type otelLogsHandler struct {
	name       string
	disableEVs bool
	lgr        *log.Logger
	timeWindow timegrinder.TimestampWindow
}

func (oh *otelLogsHandler) handle(h *handler, cfg routeHandler, w http.ResponseWriter, r *http.Request, rdr io.Reader, ip net.IP) {
	var now time.Time
	if cfg.debugPosts {
		now = time.Now()
	}

	ll := log.NewLoggerWithKV(oh.lgr,
		log.KV("otel-logs-listener", oh.name),
		log.KV("remoteaddress", ip),
	)

	bodyReadLimit := int64(maxBody + 256)
	lr := io.LimitedReader{R: rdr, N: bodyReadLimit}
	body, err := io.ReadAll(&lr)
	if err != nil {
		ll.Error("failed to read request body", log.KVErr(err))
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if len(body) == 0 {
		ll.Error("request body empty")
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if lr.N == 0 {
		ll.Error("request body too large", log.KV("max-body", maxBody))
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		return
	}

	var req collogs.ExportLogsServiceRequest
	contentType := r.Header.Get("Content-Type")

	switch contentType {
	case "application/x-protobuf", "application/protobuf":
		if err = proto.Unmarshal(body, &req); err != nil {
			ll.Error("failed to unmarshal protobuf", log.KVErr(err))
			w.WriteHeader(http.StatusBadRequest)
			return
		}
	case "application/json":
		if err = protojson.Unmarshal(body, &req); err != nil {
			ll.Error("failed to unmarshal JSON", log.KVErr(err))
			w.WriteHeader(http.StatusBadRequest)
			return
		}
	default:
		if err = proto.Unmarshal(body, &req); err != nil {
			if err = protojson.Unmarshal(body, &req); err != nil {
				ll.Error("failed to unmarshal request", log.KVErr(err))
				w.WriteHeader(http.StatusBadRequest)
				return
			}
		}
	}

	var entriesCount int
	var byteCount int64

	for _, rl := range req.ResourceLogs {
		if err := oh.processResourceLogs(h, cfg, rl, ip, &entriesCount, &byteCount); err != nil {
			ll.Error("failed to process resource logs", log.KVErr(err))
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	}

	resp := &collogs.ExportLogsServiceResponse{}
	respBytes, err := proto.Marshal(resp)
	if err != nil {
		ll.Error("failed to marshal response", log.KVErr(err))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/x-protobuf")
	w.WriteHeader(http.StatusOK)
	w.Write(respBytes)

	if cfg.debugPosts {
		kvs := []rfc5424.SDParam{
			log.KV("bytes", byteCount), log.KV("entries", entriesCount),
			log.KV("ms", time.Since(now).Milliseconds()),
		}
		h.igst.Info("OpenTelemetry logs request", kvs...)
	}
}

func (oh *otelLogsHandler) processResourceLogs(h *handler, cfg routeHandler, rl *lpb.ResourceLogs, ip net.IP, entriesCount *int, byteCount *int64) error {
	if rl == nil {
		return nil
	}
	for _, sl := range rl.ScopeLogs {
		if sl == nil {
			continue
		}
		for _, logRecord := range sl.LogRecords {
			if logRecord == nil {
				continue
			}
			var ts entry.Timestamp
			if cfg.ignoreTs {
				ts = entry.Now()
			} else {
				ts = oh.extractTimestamp(logRecord)
			}
			e := entry.Entry{
				TS:   ts,
				SRC:  ip,
				Tag:  cfg.tag,
				Data: oh.extractLogBody(logRecord),
			}
			if !oh.disableEVs {
				if err := oh.encodeLogEVs(logRecord, rl.Resource, sl.Scope, &e); err != nil {
					oh.lgr.Warn("failed to encode log EVs", log.KVErr(err))
				}
			}

			*byteCount += int64(e.Size())

			if rl.Resource != nil {
				for _, attr := range rl.Resource.Attributes {
					oh.addAttributeToEntry(&e, attr)
				}
			}

			if err := cfg.pproc.ProcessContext(&e, exitCtx); err != nil {
				oh.lgr.Error("failed to send entry", log.KVErr(err))
				return err
			}

			h.entSI.Add(1)
			h.bytesSI.Add(uint64(len(e.Data)))
			*entriesCount++
		}
	}
	return nil
}

func (oh *otelLogsHandler) encodeLogEVs(logRecord *lpb.LogRecord, resource *rpb.Resource, scope *cpb.InstrumentationScope, e *entry.Entry) error {
	if logRecord.SeverityNumber != lpb.SeverityNumber_SEVERITY_NUMBER_UNSPECIFIED {
		e.AddEnumeratedValue(entry.EnumeratedValue{
			Name:  "severity_number",
			Value: entry.Int32EnumData(int32(logRecord.SeverityNumber)),
		})
	}
	if logRecord.SeverityText != "" {
		e.AddEnumeratedValue(entry.EnumeratedValue{
			Name:  "severity_text",
			Value: entry.StringEnumData(logRecord.SeverityText),
		})
	}
	if logRecord.Flags != 0 {
		e.AddEnumeratedValue(entry.EnumeratedValue{
			Name:  "flags",
			Value: entry.Uint32EnumData(logRecord.Flags),
		})
	}
	if len(logRecord.TraceId) > 0 {
		e.AddEnumeratedValue(entry.EnumeratedValue{
			Name:  "trace_id",
			Value: entry.SliceEnumData(logRecord.TraceId),
		})
	}
	if len(logRecord.SpanId) > 0 {
		e.AddEnumeratedValue(entry.EnumeratedValue{
			Name:  "span_id",
			Value: entry.SliceEnumData(logRecord.SpanId),
		})
	}
	for _, attr := range logRecord.Attributes {
		if attr != nil {
			oh.addAttributeToEntry(e, attr)
		}
	}
	return nil
}

func (oh *otelLogsHandler) extractLogBody(logRecord *lpb.LogRecord) []byte {
	if logRecord.Body == nil {
		return []byte{}
	}
	val := oh.convertAttributeValue(logRecord.Body)
	switch v := val.(type) {
	case string:
		return []byte(v)
	case []byte:
		return v
	default:
		b, _ := json.Marshal(val)
		return b
	}
}

func (oh *otelLogsHandler) convertAttributeValue(v *cpb.AnyValue) interface{} {
	if v == nil {
		return nil
	}
	switch value := v.Value.(type) {
	case *cpb.AnyValue_StringValue:
		return value.StringValue
	case *cpb.AnyValue_IntValue:
		return value.IntValue
	case *cpb.AnyValue_DoubleValue:
		return value.DoubleValue
	case *cpb.AnyValue_BoolValue:
		return value.BoolValue
	case *cpb.AnyValue_BytesValue:
		return value.BytesValue
	case *cpb.AnyValue_ArrayValue:
		arr := make([]interface{}, 0, len(value.ArrayValue.Values))
		for _, av := range value.ArrayValue.Values {
			arr = append(arr, oh.convertAttributeValue(av))
		}
		return arr
	case *cpb.AnyValue_KvlistValue:
		kvMap := make(map[string]interface{})
		for _, kv := range value.KvlistValue.Values {
			kvMap[kv.Key] = oh.convertAttributeValue(kv.Value)
		}
		return kvMap
	}
	return nil
}

func (oh *otelLogsHandler) addAttributeToEntry(e *entry.Entry, attr *cpb.KeyValue) {
	val := oh.convertAttributeValue(attr.Value)
	if ed, err := entry.InferEnumeratedData(val); err == nil {
		e.AddEnumeratedValue(entry.EnumeratedValue{Name: attr.Key, Value: ed})
	} else if err == entry.ErrUnknownType {
		if ed, err = entry.InferEnumeratedData(fmt.Sprintf("%v", val)); err == nil {
			e.AddEnumeratedValue(entry.EnumeratedValue{Name: attr.Key, Value: ed})
		}
	}
}

func (oh *otelLogsHandler) extractTimestamp(logRecord *lpb.LogRecord) entry.Timestamp {
	var ts time.Time

	if logRecord.TimeUnixNano != 0 {
		ts = oh.nanoToTime(logRecord.TimeUnixNano)
	} else if logRecord.ObservedTimeUnixNano != 0 {
		ts = oh.nanoToTime(logRecord.ObservedTimeUnixNano)
	}

	if ts.IsZero() {
		ts = time.Now()
	} else {
		ts = oh.timeWindow.Override(ts)
	}

	return entry.FromStandard(ts)
}

func (oh *otelLogsHandler) nanoToTime(nano uint64) time.Time {
	if nano == 0 {
		return time.Time{}
	}
	sec := int64(nano / 1e9)
	nsec := int64(nano % 1e9)
	return time.Unix(sec, nsec)
}

func (o *otelLogsListener) validate(name string) (string, error) {
	if _, err := o.auth.Validate(); err != nil {
		return ``, fmt.Errorf("Authentication configuration error for %s %w", name, err)
	}
	if len(o.URL) == 0 {
		o.URL = defaultOtelLogsURL
	}
	p, err := url.Parse(o.URL)
	if err != nil {
		return ``, fmt.Errorf("URL structure is invalid: %v", err)
	}
	if p.Scheme != `` {
		return ``, errors.New("May not specify scheme in listening URL")
	} else if p.Host != `` {
		return ``, errors.New("May not specify host in listening URL")
	}
	pth := path.Clean(p.Path)
	if len(o.Tag_Name) == 0 {
		o.Tag_Name = entry.DefaultTagName
	}
	if ingest.CheckTag(o.Tag_Name) != nil {
		return ``, errors.New("Invalid characters in the \"" + o.Tag_Name + "\"Tag-Name for " + name)
	}
	o.URL = pth
	return pth, nil
}

func (o *otelLogsListener) tags() ([]string, error) {
	if len(o.Tag_Name) == 0 {
		return nil, errors.New("No tags specified")
	}
	return []string{o.Tag_Name}, nil
}

func includeOtelLogsListeners(hnd *handler, igst *ingest.IngestMuxer, cfg *cfgType) (err error) {
	for k, v := range cfg.OtelLogsListener {
		oh := &otelLogsHandler{
			name:       k,
			lgr:        hnd.lgr,
			disableEVs: v.Disable_EVs,
		}
		if oh.timeWindow, err = cfg.GlobalTimestampWindow(); err != nil {
			return fmt.Errorf("TimestampWindow is invalid %w", err)
		}

		hcfg := routeHandler{
			handler:    oh.handle,
			debugPosts: v.Debug_Posts,
		}

		if hcfg.tag, err = igst.NegotiateTag(v.Tag_Name); err != nil {
			return fmt.Errorf("failed to negotiate tag %s %w", v.Tag_Name, err)
		}

		if v.Ignore_Timestamps {
			hcfg.ignoreTs = true
		} else {
			var window timegrinder.TimestampWindow
			window, err = cfg.GlobalTimestampWindow()
			if err != nil {
				return fmt.Errorf("Failed to get global timestamp window %w", err)
			}
			if hcfg.tg, err = timegrinder.New(timegrinder.Config{TSWindow: window}); err != nil {
				return fmt.Errorf("Failed to create timegrinder %w", err)
			} else if err = cfg.TimeFormat.LoadFormats(hcfg.tg); err != nil {
				return fmt.Errorf("failed to load custom time formats %w", err)
			}
		}

		if hcfg.pproc, err = cfg.Preprocessor.ProcessorSet(igst, v.Preprocessor); err != nil {
			return fmt.Errorf("preprocessor construction error %w", err)
		}

		//check if authentication is enabled for this URL
		if pth, ah, err := v.NewAuthHandler(hnd.lgr); err != nil {
			return fmt.Errorf("failed to get a new authentication handler %w", err)
		} else if hnd != nil {
			if pth != `` {
				if err = hnd.addAuthHandler(http.MethodPost, pth, ah); err != nil {
					return fmt.Errorf("failed to add auth handler url %q %w", pth, err)
				}
			}
			hcfg.auth = ah
		}

		if err = hnd.addHandler(http.MethodPost, v.URL, hcfg); err != nil {
			return fmt.Errorf("failed to add OpenTelemetry logs handler %w", err)
		}
		debugout("Added OpenTelemetry logs listener %s %s\n", k, v.URL)
	}
	return nil
}
