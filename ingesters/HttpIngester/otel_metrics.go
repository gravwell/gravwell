/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"bytes"
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
	"github.com/gravwell/gravwell/v3/ingest/processors"
	"github.com/gravwell/gravwell/v3/timegrinder"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	colmetrics "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	cpb "go.opentelemetry.io/proto/otlp/common/v1"
	mpb "go.opentelemetry.io/proto/otlp/metrics/v1"
	rpb "go.opentelemetry.io/proto/otlp/resource/v1"
)

const (
	defaultOtelMetricsURL = `/v1/metrics`
)

var (
	evGaugeType        = entry.EnumeratedValue{Name: "type", Value: entry.StringEnumData(`gauge`)}
	evSumType          = entry.EnumeratedValue{Name: "type", Value: entry.StringEnumData(`sum`)}
	evHistogramType    = entry.EnumeratedValue{Name: "type", Value: entry.StringEnumData(`histogram`)}
	evExpHistogramType = entry.EnumeratedValue{Name: "type", Value: entry.StringEnumData(`exponential_histogram`)}
	evSummaryType      = entry.EnumeratedValue{Name: "type", Value: entry.StringEnumData(`summary`)}
)

// otelMetricsListener is the config type
type otelMetricsListener struct {
	auth              //authentication information
	URL               string
	Tag_Name          string
	Ignore_Timestamps bool
	Debug_Posts       bool
	Encode_As_JSON    bool // Encode the entire metric post as JSON and assign to the body
	Preprocessor      []string
}

type otelHandler struct {
	name           string
	encodeJSONBody bool
	lgr            *log.Logger
	timeWindow     timegrinder.TimestampWindow
}

func (oh *otelHandler) handle(h *handler, cfg routeHandler, w http.ResponseWriter, r *http.Request, rdr io.Reader, ip net.IP) {
	var now time.Time
	if cfg.debugPosts {
		now = time.Now()
	}

	ll := log.NewLoggerWithKV(oh.lgr,
		log.KV("otel-listener", oh.name),
		log.KV("remoteaddress", ip),
	)

	bodyReadLimit := int64(maxBody + 256) // add a few bytes of leeway for headers or other overhead
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

	var req colmetrics.ExportMetricsServiceRequest
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

	for _, rm := range req.ResourceMetrics {
		if err := oh.processResourceMetrics(h, cfg, rm, ip, &entriesCount, &byteCount); err != nil {
			ll.Error("failed to process resource metrics", log.KVErr(err))
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	}

	resp := &colmetrics.ExportMetricsServiceResponse{}
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
		h.igst.Info("OpenTelemetry metrics request", kvs...)
	}
}

func (oh *otelHandler) processResourceMetrics(h *handler, cfg routeHandler, rm *mpb.ResourceMetrics, ip net.IP, entriesCount *int, byteCount *int64) error {
	if rm == nil {
		// if we get a nil metric just exit cleanly
		return nil
	}
	for _, sm := range rm.ScopeMetrics {
		if sm == nil {
			continue
		}
		for _, metric := range sm.Metrics {
			if metric == nil {
				continue
			}
			var ts entry.Timestamp
			if cfg.ignoreTs {
				ts = entry.Now()
			} else {
				ts = oh.extractTimestamp(metric)
			}
			e := entry.Entry{
				TS:  ts,
				SRC: ip,
				Tag: cfg.tag,
			}
			if oh.encodeJSONBody {
				var err error
				if e.Data, err = oh.convertMetricToJSON(metric, rm.Resource, sm.Scope); err != nil {
					oh.lgr.Warn("failed to convert metric to JSON", log.KVErr(err))
					continue
				}
			}
			if metric.Name != "" {
				e.AddEnumeratedValueEx("metric", metric.Name)
			}
			if metric.Unit != "" {
				e.AddEnumeratedValueEx("unit", metric.Unit)
			}
			if metric.Description != "" {
				e.AddEnumeratedValueEx("description", metric.Description)
			}

			if rm.Resource != nil {
				for _, attr := range rm.Resource.Attributes {
					oh.addAttributeToEntry(&e, attr)
				}
			}
			cnt, bytes, err := oh.processMetricDatapoints(cfg.pproc, e, metric.Data)
			if err != nil {
				oh.lgr.Error("failed to send entry", log.KVErr(err))
				return err
			}
			h.entSI.Add(cnt)
			h.bytesSI.Add(bytes)
			*entriesCount += int(cnt)
			*byteCount += int64(bytes)
		}
	}
	return nil
}

func (oh *otelHandler) addMetricAttributes(e *entry.Entry, attribs []*cpb.KeyValue) {
	if len(attribs) > 0 {
		for _, attr := range attribs {
			if attr != nil && attr.Value != nil {
				switch v := attr.Value.Value.(type) {
				case *cpb.AnyValue_StringValue:
					e.AddEnumeratedValueEx(attr.Key, v.StringValue)
				case *cpb.AnyValue_IntValue:
					e.AddEnumeratedValueEx(attr.Key, v.IntValue)
				case *cpb.AnyValue_DoubleValue:
					e.AddEnumeratedValueEx(attr.Key, v.DoubleValue)
				case *cpb.AnyValue_BoolValue:
					e.AddEnumeratedValueEx(attr.Key, v.BoolValue)
				case *cpb.AnyValue_BytesValue:
					e.AddEnumeratedValueEx(attr.Key, v.BytesValue)
				}
			}
		}
	}
}

func (oh *otelHandler) processNumberDataPoint(p *processors.ProcessorSet, e entry.Entry, dp *mpb.NumberDataPoint) (sz uint64, err error) {
	if dp == nil {
		return
	}
	oh.overrideTimestampFromDatapoint(&e, dp.TimeUnixNano)

	oh.addMetricAttributes(&e, dp.Attributes)
	switch v := dp.Value.(type) {
	case *mpb.NumberDataPoint_AsInt:
		e.AddEnumeratedValueEx("value", v.AsInt)
	case *mpb.NumberDataPoint_AsDouble:
		e.AddEnumeratedValueEx("value", v.AsDouble)
	}

	if err = p.ProcessContext(&e, exitCtx); err == nil {
		sz = uint64(e.Size())
	}
	return
}

func (oh *otelHandler) processHistogramDataPoint(p *processors.ProcessorSet, e entry.Entry, dp *mpb.HistogramDataPoint) error {
	if dp == nil {
		return nil
	}
	oh.overrideTimestampFromDatapoint(&e, dp.TimeUnixNano)

	oh.addMetricAttributes(&e, dp.Attributes)
	e.AddEnumeratedValueEx("count", dp.Count)
	e.AddEnumeratedValueEx("sum", dp.Sum)
	if dp.Min != nil {
		e.AddEnumeratedValueEx("min", *dp.Min)
	}
	if dp.Max != nil {
		e.AddEnumeratedValueEx("max", *dp.Max)
	}
	return nil
}

func (oh *otelHandler) processExponentialHistogramDataPoint(p *processors.ProcessorSet, e entry.Entry, dp *mpb.ExponentialHistogramDataPoint) error {
	if dp == nil {
		return nil
	}
	oh.overrideTimestampFromDatapoint(&e, dp.TimeUnixNano)

	oh.addMetricAttributes(&e, dp.Attributes)
	e.AddEnumeratedValueEx("count", dp.Count)
	if dp.Sum != nil {
		e.AddEnumeratedValueEx("sum", *dp.Sum)
	}
	if dp.Min != nil {
		e.AddEnumeratedValueEx("min", *dp.Min)
	}
	if dp.Max != nil {
		e.AddEnumeratedValueEx("max", *dp.Max)
	}
	return nil
}

func (oh *otelHandler) processSummaryDataPoint(p *processors.ProcessorSet, e entry.Entry, dp *mpb.SummaryDataPoint) error {
	if dp == nil {
		return nil
	}
	oh.overrideTimestampFromDatapoint(&e, dp.TimeUnixNano)

	oh.addMetricAttributes(&e, dp.Attributes)
	e.AddEnumeratedValueEx("count", dp.Count)
	e.AddEnumeratedValueEx("sum", dp.Sum)
	return nil
}

func (oh *otelHandler) processMetricDatapoints(p *processors.ProcessorSet, e entry.Entry, d interface{}) (cnt uint64, bts uint64, err error) {
	if p == nil || d == nil {
		return // just do nothing
	}
	switch data := d.(type) {
	case *mpb.Metric_Gauge:
		e.AddEnumeratedValue(evGaugeType)
		for _, dp := range data.Gauge.DataPoints {
			var sz uint64
			if sz, err = oh.processNumberDataPoint(p, e, dp); err != nil {
				return
			}
			cnt++
			bts += sz
		}
	case *mpb.Metric_Sum:
		e.AddEnumeratedValue(evSumType)
		//e.AddEnumeratedValueEx("aggregation_temporality", data.Sum.AggregationTemporality.String())
		e.AddEnumeratedValueEx("monotonic", data.Sum.IsMonotonic)
		for _, dp := range data.Sum.DataPoints {
			var sz uint64
			if sz, err = oh.processNumberDataPoint(p, e, dp); err != nil {
				return
			}
			cnt++
			bts += sz
		}
	case *mpb.Metric_Histogram:
		e.AddEnumeratedValue(evHistogramType)
		//e.AddEnumeratedValueEx("aggregation_temporality", data.Histogram.AggregationTemporality.String())
		for _, dp := range data.Histogram.DataPoints {
			if err = oh.processHistogramDataPoint(p, e, dp); err != nil {
				return
			}
		}
		//metricData["data_points"] = oh.convertHistogramDataPoints(data.Histogram.DataPoints)
	case *mpb.Metric_ExponentialHistogram:
		e.AddEnumeratedValue(evExpHistogramType)
		for _, dp := range data.ExponentialHistogram.DataPoints {
			if err = oh.processExponentialHistogramDataPoint(p, e, dp); err != nil {
				return
			}
		}
	case *mpb.Metric_Summary:
		e.AddEnumeratedValue(evSummaryType)
		for _, dp := range data.Summary.DataPoints {
			if err = oh.processSummaryDataPoint(p, e, dp); err != nil {
				return
			}
		}
		//metricData["data_points"] = oh.convertSummaryDataPoints(data.Summary.DataPoints)
	}
	return
}

func (oh *otelHandler) convertMetricToJSON(metric *mpb.Metric, resource *rpb.Resource, scope *cpb.InstrumentationScope) ([]byte, error) {
	metricData := map[string]interface{}{
		"name":        metric.Name,
		"description": metric.Description,
		"unit":        metric.Unit,
	}

	if resource != nil {
		resAttrs := make(map[string]interface{})
		for _, attr := range resource.Attributes {
			resAttrs[attr.Key] = oh.convertAttributeValue(attr.Value)
		}
		if len(resAttrs) > 0 {
			metricData["resource"] = resAttrs
		}
	}

	if scope != nil {
		scopeData := map[string]interface{}{
			"name":    scope.Name,
			"version": scope.Version,
		}
		metricData["scope"] = scopeData
	}

	switch data := metric.Data.(type) {
	case *mpb.Metric_Gauge:
		metricData["type"] = "gauge"
		metricData["data_points"] = oh.convertNumberDataPoints(data.Gauge.DataPoints)
	case *mpb.Metric_Sum:
		metricData["type"] = "sum"
		metricData["aggregation_temporality"] = data.Sum.AggregationTemporality.String()
		metricData["is_monotonic"] = data.Sum.IsMonotonic
		metricData["data_points"] = oh.convertNumberDataPoints(data.Sum.DataPoints)
	case *mpb.Metric_Histogram:
		metricData["type"] = "histogram"
		metricData["aggregation_temporality"] = data.Histogram.AggregationTemporality.String()
		metricData["data_points"] = oh.convertHistogramDataPoints(data.Histogram.DataPoints)
	case *mpb.Metric_ExponentialHistogram:
		metricData["type"] = "exponential_histogram"
		metricData["aggregation_temporality"] = data.ExponentialHistogram.AggregationTemporality.String()
		metricData["data_points"] = oh.convertExponentialHistogramDataPoints(data.ExponentialHistogram.DataPoints)
	case *mpb.Metric_Summary:
		metricData["type"] = "summary"
		metricData["data_points"] = oh.convertSummaryDataPoints(data.Summary.DataPoints)
	}

	return json.Marshal(metricData)
}

func (oh *otelHandler) convertNumberDataPoints(dps []*mpb.NumberDataPoint) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(dps))
	for _, dp := range dps {
		point := map[string]interface{}{
			"start_time_unix_nano": dp.StartTimeUnixNano,
			"time_unix_nano":       dp.TimeUnixNano,
			"attributes":           oh.convertAttributes(dp.Attributes),
		}
		switch v := dp.Value.(type) {
		case *mpb.NumberDataPoint_AsInt:
			point["value"] = v.AsInt
			point["value_type"] = "int"
		case *mpb.NumberDataPoint_AsDouble:
			point["value"] = v.AsDouble
			point["value_type"] = "double"
		}
		result = append(result, point)
	}
	return result
}

func (oh *otelHandler) convertHistogramDataPoints(dps []*mpb.HistogramDataPoint) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(dps))
	for _, dp := range dps {
		point := map[string]interface{}{
			"start_time_unix_nano": dp.StartTimeUnixNano,
			"time_unix_nano":       dp.TimeUnixNano,
			"count":                dp.Count,
			"sum":                  dp.Sum,
			"bucket_counts":        dp.BucketCounts,
			"explicit_bounds":      dp.ExplicitBounds,
			"attributes":           oh.convertAttributes(dp.Attributes),
		}
		if dp.Min != nil {
			point["min"] = *dp.Min
		}
		if dp.Max != nil {
			point["max"] = *dp.Max
		}
		result = append(result, point)
	}
	return result
}

func (oh *otelHandler) convertExponentialHistogramDataPoints(dps []*mpb.ExponentialHistogramDataPoint) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(dps))
	for _, dp := range dps {
		point := map[string]interface{}{
			"start_time_unix_nano": dp.StartTimeUnixNano,
			"time_unix_nano":       dp.TimeUnixNano,
			"count":                dp.Count,
			"sum":                  dp.Sum,
			"scale":                dp.Scale,
			"zero_count":           dp.ZeroCount,
			"attributes":           oh.convertAttributes(dp.Attributes),
		}
		if dp.Min != nil {
			point["min"] = *dp.Min
		}
		if dp.Max != nil {
			point["max"] = *dp.Max
		}
		if dp.Positive != nil {
			point["positive"] = map[string]interface{}{
				"offset":        dp.Positive.Offset,
				"bucket_counts": dp.Positive.BucketCounts,
			}
		}
		if dp.Negative != nil {
			point["negative"] = map[string]interface{}{
				"offset":        dp.Negative.Offset,
				"bucket_counts": dp.Negative.BucketCounts,
			}
		}
		result = append(result, point)
	}
	return result
}

func (oh *otelHandler) convertSummaryDataPoints(dps []*mpb.SummaryDataPoint) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(dps))
	for _, dp := range dps {
		point := map[string]interface{}{
			"start_time_unix_nano": dp.StartTimeUnixNano,
			"time_unix_nano":       dp.TimeUnixNano,
			"count":                dp.Count,
			"sum":                  dp.Sum,
			"attributes":           oh.convertAttributes(dp.Attributes),
		}
		quantiles := make([]map[string]interface{}, 0, len(dp.QuantileValues))
		for _, qv := range dp.QuantileValues {
			quantiles = append(quantiles, map[string]interface{}{
				"quantile": qv.Quantile,
				"value":    qv.Value,
			})
		}
		point["quantile_values"] = quantiles
		result = append(result, point)
	}
	return result
}

func (oh *otelHandler) convertAttributes(attrs []*cpb.KeyValue) map[string]interface{} {
	result := make(map[string]interface{})
	for _, attr := range attrs {
		result[attr.Key] = oh.convertAttributeValue(attr.Value)
	}
	return result
}

func (oh *otelHandler) convertAttributeValue(v *cpb.AnyValue) interface{} {
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

func (oh *otelHandler) addAttributeToEntry(e *entry.Entry, attr *cpb.KeyValue) {
	val := oh.convertAttributeValue(attr.Value)
	if ed, err := entry.InferEnumeratedData(val); err == nil {
		e.AddEnumeratedValue(entry.EnumeratedValue{Name: attr.Key, Value: ed})
	} else if err == entry.ErrUnknownType {
		if ed, err = entry.InferEnumeratedData(fmt.Sprintf("%v", val)); err == nil {
			e.AddEnumeratedValue(entry.EnumeratedValue{Name: attr.Key, Value: ed})
		}
	}
}

func (oh *otelHandler) overrideTimestampFromDatapoint(e *entry.Entry, ts uint64) {
	if ts == 0 {
		return
	}
	e.TS = entry.FromStandard(oh.timeWindow.Override(oh.nanoToTime(ts)))
}

func (oh *otelHandler) extractTimestamp(metric *mpb.Metric) entry.Timestamp {
	var ts time.Time

	switch data := metric.Data.(type) {
	case *mpb.Metric_Gauge:
		if len(data.Gauge.DataPoints) > 0 {
			ts = oh.nanoToTime(data.Gauge.DataPoints[0].TimeUnixNano)
		}
	case *mpb.Metric_Sum:
		if len(data.Sum.DataPoints) > 0 {
			ts = oh.nanoToTime(data.Sum.DataPoints[0].TimeUnixNano)
		}
	case *mpb.Metric_Histogram:
		if len(data.Histogram.DataPoints) > 0 {
			ts = oh.nanoToTime(data.Histogram.DataPoints[0].TimeUnixNano)
		}
	case *mpb.Metric_ExponentialHistogram:
		if len(data.ExponentialHistogram.DataPoints) > 0 {
			ts = oh.nanoToTime(data.ExponentialHistogram.DataPoints[0].TimeUnixNano)
		}
	case *mpb.Metric_Summary:
		if len(data.Summary.DataPoints) > 0 {
			ts = oh.nanoToTime(data.Summary.DataPoints[0].TimeUnixNano)
		}
	}

	if ts.IsZero() {
		ts = time.Now()
	} else {
		ts = oh.timeWindow.Override(ts)
	}

	return entry.FromStandard(ts)
}

func (oh *otelHandler) nanoToTime(nano uint64) time.Time {
	if nano == 0 {
		return time.Time{}
	}
	sec := int64(nano / 1e9)
	nsec := int64(nano % 1e9)
	return time.Unix(sec, nsec)
}

type metricsEntry struct {
	MetricName   string                 `json:"metric_name"`
	Description  string                 `json:"description,omitempty"`
	Unit         string                 `json:"unit,omitempty"`
	Type         string                 `json:"type"`
	Value        interface{}            `json:"value"`
	Attributes   map[string]interface{} `json:"attributes,omitempty"`
	Resource     map[string]interface{} `json:"resource,omitempty"`
	Timestamp    time.Time              `json:"timestamp"`
	StartTime    time.Time              `json:"start_time,omitempty"`
	ScopeName    string                 `json:"scope_name,omitempty"`
	ScopeVersion string                 `json:"scope_version,omitempty"`
}

func formatMetricAsString(me *metricsEntry) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString(fmt.Sprintf("metric=%s type=%s", me.MetricName, me.Type))
	if me.Description != "" {
		buf.WriteString(fmt.Sprintf(" description=%q", me.Description))
	}
	if me.Unit != "" {
		buf.WriteString(fmt.Sprintf(" unit=%s", me.Unit))
	}
	buf.WriteString(fmt.Sprintf(" value=%v", me.Value))
	buf.WriteString(fmt.Sprintf(" timestamp=%s", me.Timestamp.Format(time.RFC3339Nano)))
	if !me.StartTime.IsZero() {
		buf.WriteString(fmt.Sprintf(" start_time=%s", me.StartTime.Format(time.RFC3339Nano)))
	}
	if len(me.Attributes) > 0 {
		attrJSON, _ := json.Marshal(me.Attributes)
		buf.WriteString(fmt.Sprintf(" attributes=%s", attrJSON))
	}
	if len(me.Resource) > 0 {
		resJSON, _ := json.Marshal(me.Resource)
		buf.WriteString(fmt.Sprintf(" resource=%s", resJSON))
	}
	return buf.Bytes(), nil
}

func (o *otelMetricsListener) validate(name string) (string, error) {
	if _, err := o.auth.Validate(); err != nil {
		return ``, fmt.Errorf("Authentication configuration error for %s %w", name, err)
	}
	if len(o.URL) == 0 {
		o.URL = defaultOtelMetricsURL
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

func (o *otelMetricsListener) tags() ([]string, error) {
	if len(o.Tag_Name) == 0 {
		return nil, errors.New("No tags specified")
	}
	return []string{o.Tag_Name}, nil
}

func includeOtelMetricsListeners(hnd *handler, igst *ingest.IngestMuxer, cfg *cfgType) (err error) {
	for k, v := range cfg.OtelListener {
		oh := &otelHandler{
			name:           k,
			encodeJSONBody: v.Encode_As_JSON,
			lgr:            hnd.lgr,
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
			return fmt.Errorf("failed to add OpenTelemetry handler %w", err)
		}
		debugout("Added OpenTelemetry metrics listener %s %s\n", k, v.URL)
	}
	return nil
}
