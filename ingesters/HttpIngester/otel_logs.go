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
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/crewjam/rfc5424"
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

type otelLogsHandler struct {
	name         string
	encodeAsJSON bool
	disableEVs   bool
	lgr          *log.Logger
	timeWindow   timegrinder.TimestampWindow
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

	if len(body) == 0 || lr.N == 0 {
		ll.Error("request body empty or too large", log.KV("max-body", maxBody))
		w.WriteHeader(http.StatusBadRequest)
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
				TS:  ts,
				SRC: ip,
				Tag: cfg.tag,
			}
			if !oh.disableEVs {
				if err := oh.encodeLogEVs(logRecord, rl.Resource, sl.Scope, &e); err != nil {
					oh.lgr.Warn("failed to encode log EVs", log.KVErr(err))
				}
			}

			if oh.encodeAsJSON {
				var err error
				if e.Data, err = oh.convertLogToJSON(logRecord, rl.Resource, sl.Scope); err != nil {
					oh.lgr.Warn("failed to convert log to JSON", log.KVErr(err))
					continue
				}
			} else {
				e.Data = oh.extractLogBody(logRecord)
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
	if logRecord.TraceId != nil && len(logRecord.TraceId) > 0 {
		e.AddEnumeratedValue(entry.EnumeratedValue{
			Name:  "trace_id",
			Value: entry.SliceEnumData(logRecord.TraceId),
		})
	}
	if logRecord.SpanId != nil && len(logRecord.SpanId) > 0 {
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

func (oh *otelLogsHandler) convertLogToJSON(logRecord *lpb.LogRecord, resource *rpb.Resource, scope *cpb.InstrumentationScope) ([]byte, error) {
	logData := map[string]interface{}{
		"timestamp": oh.nanoToTime(logRecord.TimeUnixNano).Format(time.RFC3339Nano),
	}

	if logRecord.ObservedTimeUnixNano != 0 {
		logData["observed_timestamp"] = oh.nanoToTime(logRecord.ObservedTimeUnixNano).Format(time.RFC3339Nano)
	}

	if logRecord.SeverityNumber != lpb.SeverityNumber_SEVERITY_NUMBER_UNSPECIFIED {
		logData["severity_number"] = logRecord.SeverityNumber.String()
	}

	if logRecord.SeverityText != "" {
		logData["severity_text"] = logRecord.SeverityText
	}

	if logRecord.Body != nil {
		logData["body"] = oh.convertAttributeValue(logRecord.Body)
	}

	if len(logRecord.Attributes) > 0 {
		attrs := make(map[string]interface{})
		for _, attr := range logRecord.Attributes {
			attrs[attr.Key] = oh.convertAttributeValue(attr.Value)
		}
		logData["attributes"] = attrs
	}

	if logRecord.DroppedAttributesCount > 0 {
		logData["dropped_attributes_count"] = logRecord.DroppedAttributesCount
	}

	if logRecord.Flags != 0 {
		logData["flags"] = logRecord.Flags
	}

	if len(logRecord.TraceId) > 0 {
		logData["trace_id"] = fmt.Sprintf("%x", logRecord.TraceId)
	}

	if len(logRecord.SpanId) > 0 {
		logData["span_id"] = fmt.Sprintf("%x", logRecord.SpanId)
	}

	if resource != nil {
		resAttrs := make(map[string]interface{})
		for _, attr := range resource.Attributes {
			resAttrs[attr.Key] = oh.convertAttributeValue(attr.Value)
		}
		if len(resAttrs) > 0 {
			logData["resource"] = resAttrs
		}
	}

	if scope != nil {
		scopeData := map[string]interface{}{
			"name":    scope.Name,
			"version": scope.Version,
		}
		logData["scope"] = scopeData
	}

	return json.Marshal(logData)
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
