/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package processors

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/timegrinder"
)

const (
	CorelightProcessor = `corelight`
)

var (
	defaultTag    string
	defaultPrefix = "zeek"
)

type CorelightConfig struct {
	// Prefix specifies the prefix for corelight logs. Each log type name will
	// be appended to the prefix to create a tag; thus if Prefix="zeek",
	// conn logs will be ingested to the 'zeekconn' tag, dhcp logs to 'zeekdhcp',
	// and so on.
	Prefix string

	// Custom_Format specifies a custom override for a path value and headers, there can be many
	Custom_Format []string
}

// A Corelight processor takes JSON-formatted Corelight logs and reformats
// them as TSV, matching the standard Zeek log types.
type Corelight struct {
	nocloser
	timegrind *timegrinder.TimeGrinder
	tg        Tagger
	tagFields map[string][]string
	tags      map[string]entry.EntryTag
	CorelightConfig
}

func CorelightLoadConfig(vc *config.VariableConfig) (c CorelightConfig, err error) {
	if err = vc.MapTo(&c); err != nil {
		return
	}
	err = c.Validate()
	return
}

func NewCorelight(cfg CorelightConfig, tagger Tagger) (*Corelight, error) {
	tcfg := timegrinder.Config{}
	timegrind, err := timegrinder.NewTimeGrinder(tcfg)
	if err != nil {
		return nil, err
	}
	rr := &Corelight{
		timegrind:       timegrind,
		CorelightConfig: cfg,
		tg:              tagger,
	}
	if err := rr.init(cfg, tagger); err != nil {
		return nil, err
	}
	return rr, nil
}

func (c *Corelight) Config(v interface{}, tagger Tagger) (err error) {
	if v == nil {
		err = ErrNilConfig
	} else if cfg, ok := v.(CorelightConfig); ok {
		err = c.init(cfg, tagger)
	} else {
		err = fmt.Errorf("Invalid configuration, unknown type type %T", v)
	}
	return
}

func cleanHeaders(hdrs []string) []string {
	for i := range hdrs {
		hdrs[i] = strings.TrimSpace(hdrs[i])
	}
	return hdrs
}

func (c *Corelight) init(cfg CorelightConfig, tagger Tagger) (err error) {
	// First we read the default specs, *then* we read the custom specs
	// This allows the user to override one of the predefined specs with their own, e.g.:
	//	Custom-Format="x509:ts,certificate.version,certificate.subject"
	specs := make([]corelightSpec, 0, len(tagHeaders)+len(cfg.Custom_Format))
	specs = append(specs, defaultSpecs()...)
	if err = cfg.Validate(); err != nil {
		return
	}
	if s, err := loadCustomFormats(cfg.Custom_Format); err != nil {
		return err
	} else {
		specs = append(specs, s...)
	}
	c.tagFields = make(map[string][]string, len(tagHeaders))
	c.tags = make(map[string]entry.EntryTag)
	for _, spec := range specs {
		tagName := c.Prefix + spec.prefix
		var tv entry.EntryTag
		if tv, err = c.tg.NegotiateTag(tagName); err != nil {
			return
		}
		c.tags[tagName] = tv
		c.tagFields[tagName] = spec.headers
	}

	return
}

func (c *Corelight) Process(ents []*entry.Entry) ([]*entry.Entry, error) {
	if len(ents) == 0 {
		return ents, nil
	}
	for _, ent := range ents {
		if ent == nil || len(ent.Data) == 0 {
			continue
		} else if tag, ts, line := c.processLine(ent.Data); tag != defaultTag {
			// If processLine comes up with a different tag, it means it parsed JSON into
			// TSV, so let's rewrite the entry.
			if tv, ok := c.tags[tag]; ok {
				ent.Tag = tv
				ent.TS = entry.FromStandard(ts)
				ent.Data = line
			}
		}
	}
	return ents, nil
}

// processLine attempts to parse out the corelight JSON, figure out
// the log type (conn, dns, dhcp, weird, etc.), and convert the entry to TSV format.
// If it succeeds, it returns the destination tag, a new timestamp, and the log entry in TSV format
func (c *Corelight) processLine(s []byte) (tag string, ts time.Time, line []byte) {
	mp := map[string]interface{}{}
	line = s
	if idx := bytes.IndexByte(line, '{'); idx == -1 {
		tag = defaultTag
		return
	} else {
		line = line[idx:]
	}
	if err := json.Unmarshal(line, &mp); err != nil {
		tag = defaultTag
		return
	}
	tag, ts, line = c.process(mp, line)
	return
}

func (c *Corelight) process(mp map[string]interface{}, og []byte) (tag string, ts time.Time, line []byte) {
	var ok bool
	var headers []string
	if len(mp) == 0 {
		tag = defaultTag
		line = og
	} else if tag, ts, ok = c.getTagTs(mp); !ok {
		tag = defaultTag
		line = og
	} else if headers, ok = c.tagFields[tag]; !ok {
		tag = defaultTag
		line = og
	} else if line, ok = emitLine(ts, headers, mp); !ok {
		tag = defaultTag
		line = og
	}

	return
}

func (c *Corelight) getTagTs(mp map[string]interface{}) (tag string, ts time.Time, ok bool) {
	var tagv interface{}
	var tsv interface{}
	var tss string
	var tagval string
	var err error
	if tagv, ok = mp["_path"]; !ok {
		return
	} else if tsv, ok = mp["ts"]; !ok {
		return
	} else if tagval, ok = tagv.(string); !ok {
		return
	} else if tss, ok = tsv.(string); !ok {
		return
	} else if ts, ok, err = c.timegrind.Extract([]byte(tss)); err != nil {
		ok = false
	} else {
		tag = c.Prefix + tagval
	}
	return
}

func emitLine(ts time.Time, headers []string, mp map[string]interface{}) (line []byte, ok bool) {
	bb := bytes.NewBuffer(nil)
	var f64 float64
	fmt.Fprintf(bb, "%.6f", float64(ts.UnixNano())/1000000000.0)
	for _, h := range headers[1:] { //always skip the TS
		if v, ok := mp[h]; ok {
			if f64, ok = v.(float64); ok {
				if _, fractional := math.Modf(f64); fractional == 0 {
					fmt.Fprintf(bb, "\t%d", int(f64))
				} else {
					fmt.Fprintf(bb, "\t%.5f", f64)
				}
			} else {
				fmt.Fprintf(bb, "\t%v", v)
			}
		} else {
			fmt.Fprintf(bb, "\t-")
		}
	}
	line, ok = bb.Bytes(), true
	return
}

func (cl *CorelightConfig) Validate() (err error) {
	if cl.Prefix == `` {
		cl.Prefix = defaultPrefix
	}
	if err = ingest.CheckTag(cl.Prefix); err != nil {
		err = fmt.Errorf("prefix %q is invalid %w", cl.Prefix, err)
		return
	}
	_, err = loadCustomFormats(cl.Custom_Format)
	return
}

type corelightSpec struct {
	prefix  string
	headers []string
}

func defaultSpecs() (specs []corelightSpec) {
	specs = make([]corelightSpec, 0, len(tagHeaders))
	for k, v := range tagHeaders {
		spec := corelightSpec{
			prefix: k,
		}
		spec.headers, _ = loadHeaders(v)
		specs = append(specs, spec)
	}
	return
}

func loadCustomFormats(strs []string) (specs []corelightSpec, err error) {
	for _, v := range strs {
		v = strings.TrimSpace(v)
		if bits := strings.SplitN(v, ":", 2); len(bits) != 2 {
			err = fmt.Errorf("%q custom format is invalid", v)
			return
		} else {
			var spec corelightSpec

			//grab and check the prefix
			if spec.prefix = strings.TrimSpace(bits[0]); len(spec.prefix) == 0 {
				err = fmt.Errorf("%q custom format is invalid", v)
				return
			} else if err = ingest.CheckTag(spec.prefix); err != nil {
				err = fmt.Errorf("%q custom format is invalid %w", v, err)
				return
			}

			//parse out the headers
			if spec.headers, err = loadHeaders(bits[1]); err != nil {
				err = fmt.Errorf("%q custom format is invalid, missing headers", v)
				return
			}
			specs = append(specs, spec)
		}
	}
	return
}

func loadHeaders(v string) (hdrs []string, err error) {
	v = strings.TrimSpace(v)
	if hdrs = cleanHeaders(strings.Split(v, ",")); len(hdrs) == 0 {
		err = errors.New("missing headers")
	}
	return
}

var tagHeaders = map[string]string{
	"conn":   "ts,uid,id.orig_h,id.orig_p,id.resp_h,id.resp_p,proto,service,duration,orig_bytes,resp_bytes,conn_state,local_orig,local_resp,missed_bytes,history,orig_pkts,orig_ip_bytes,resp_pkts,resp_ip_bytes,tunnel_parents,vlan",
	"dns":    "ts,uid,id.orig_h,id.orig_p,id.resp_h,id.resp_p,proto,trans_id,rtt,query,qclass,qclass_name,qtype,qtype_name,rcode,rcode_name,AA,TC,RD,RA,Z,answers,TTLs,rejected",
	"dhcp":   "ts,uids,client_addr,server_addr,mac,host_name,client_fqdn,domain,requested_addr,assigned_addr,lease_time,client_message,server_message,msg_types,duration",
	"ssh":    "ts,uid,id.orig_h,id.orig_p,id.resp_h,id.resp_p,version,auth_success,auth_attempts,direction,client,server,cipher_alg,mac_alg,compression_alg,kex_alg,host_key_alg,host_key,inferences",
	"ftp":    "ts,uid,id.orig_h,id.orig_p,id.resp_h,id.resp_p,user,password,command,arg,mime_type,file_size,reply_code,reply_msg,data_channel.passive,data_channel.orig_h,data_channel.resp_h,data_channel.resp_p,fuid",
	"http":   "ts,uid,id.orig_h,id.orig_p,id.resp_h,id.resp_p,trans_depth,method,host,uri,referrer,version,user_agent,origin,request_body_len,response_body_len,status_code,status_msg,info_code,info_msg,tags,username,password,proxied,orig_fuids,orig_filenames,orig_mime_types,resp_fuids,resp_filenames,resp_mime_types",
	"files":  "ts,fuid,tx_hosts,rx_hosts,conn_uids,source,depth,analyzers,mime_type,filename,duration,local_orig,is_orig,seen_bytes,total_bytes,missing_bytes,overflow_bytes,timedout,parent_fuid,md5,sha1,sha256,extracted,extracted_cutoff,extracted_size",
	"ssl":    "ts,uid,id.orig_h,id.orig_p,id.resp_h,id.resp_p,version,cipher,curve,server_name,resumed,last_alert,next_protocol,established,cert_chain_fuids,client_cert_chain_fuids,subject,issuer,client_subject,client_issuer,validation_status",
	"x509":   "ts,id,certificate.version,certificate.serial,certificate.subject,certificate.issuer,certificate.not_valid_before,certificate.not_valid_after,certificate.key_alg,certificate.sig_alg,certificate.key_type,certificate.key_length,certificate.exponent,certificate.curve,san.dns,san.uri,san.email,san.ip,basic_constraints.ca,basic_constraintspath_len",
	"smtp":   "ts,uid,id.orig_h,id.orig_p,id.resp_h,id.resp_p,trans_depth,helo,mailfrom,rcptto,date,from,to,cc,reply_to,msg_id,in_reply_to,subject,x_originating_ip,first_received,second_received,last_reply,path,user_agent,tls",
	"pe":     "ts,id,machine,compile_ts,os,subsystem,is_exe,is_64bit,uses_aslr,uses_dep,uses_code_integrity,uses_seh,has_import_table,has_cert_table,has_debug_data,section_names",
	"ntp":    "ts,uid,id.orig_h,id.orig_p,id.resp_h,id.resp_p,version,mode,stratum,poll,precision,root_delay,root_disp,ref_id,ref_time,org_time,rec_time,xmt_time,num_exts",
	"notice": "ts,uid,id.orig_h,id.orig_p,id.resp_h,id.resp_p,fuid,file_mime_type,file_desc,proto,note,msg,sub,src,dst,p,n,peer_descr,actions,suppress_for,remote_location.destination_country_code,remote_location.destination_region,remote_location.destination_city,remote_location.destination_latitude,remote_location.destination_longitude,dropped",
	"weird":  "ts,uid,id.orig_h,id.orig_p,id.resp_h,id.resp_p,name,addl,notice,peer,source",
	"dpd":    "ts,uid,id.orig_h,id.orig_p,id.resp_h,id.resp_p,proto,analyzer,failure_reason,packet_segment",
	"irc":    "ts,uid,id.orig_h,id.orig_p,id.resp_h,id.resp_p,nick,user,command,value,addl,dcc_file_name,dcc_file_size,dcc_mime_type,fuid",
	"rdp":    "ts,uid,id.orig_h,id.orig_p,id.resp_h,id.resp_p,cookie,result,security_protocol,client_channels,keyboard_layout,client_build,client_name,client_dig_product_id,desktop_width,desktop_height,requested_color_depth,cert_type,cert_count,cert_permanent,encryption_level,encryption_method",

	"sip":         "ts,uid,id.orig_h,id.orig_p,id.resp_h,id.resp_p,trans_depth,method,uri,date,request_fromrequest_to,response_from,response_to,reply_to,call_id,seq,subject,request_path,response_path,user_agent,status_code,status_msg,warning,request_body_len,response_body_len,content_type",
	"snmp":        "ts,uid,id.orig_h,id.orig_p,id.resp_h,id.resp_p,duration,version,community,get_requests,get_bulk_requests,get_responses,set_requests,display_string,up_since",
	"tunnel":      "ts,uid,id.orig_h,id.orig_p,id.resp_h,id.resp_p,tunnel_type,action",
	"socks":       "ts,uid,id.orig_h,id.orig_p,id.resp_h,id.resp_p,version,user,password,status,request,request_host,request_name,request_port,bound_host,bound_name",
	"software":    "ts,host,host_port,software_type,name,major,minor,minor2,minor3,addl,unparsed_version",
	"syslog":      "ts,uid,id.orig_h,id.orig_p,id.resp_h,id.resp_p,proto,facility,severity,message",
	"rfb":         "ts,uid,id.orig_h,id.orig_p,id.resp_h,id.resp_p,client_major_version,client_minor_version,server_major_version,server_minor_version,authentication_method,auth,share_flag,desktop_name,width,height",
	"radius":      "ts,uid,id.orig_h,id.orig_p,id.resp_h,id.resp_p,username,mac,remote_ip,connect_info,result,logged",
	"intel":       "ts,uid,id.orig_h,id.orig_p,id.resp_h,id.resp_p,indicator,indicator_type,seen_where,seen_node,matched,sources,fuid,file_mime_type,file_desc",
	"kerberos":    "ts,uid,id.orig_h,id.orig_p,id.resp_h,id.resp_p,request_type,client,service,success,error_msg,from,till,cipher,forwardable,renewable,client_cert,client_cert_fuid,server_cert_subject,server_cert_fuid",
	"mysql":       "ts,uid,id.orig_h,id.orig_p,id.resp_h,id.resp_p,cmd,arg,success,rows,response",
	"modbus":      "ts,uid,id.orig_h,id.orig_p,id.resp_h,id.resp_p,func,exception",
	"signature":   "ts,uid,id.orig_h,id.orig_p,id.resp_h,id.resp_p,note,sig_id,event_msg,sub_msg,sig_count,host_count",
	"smb_mapping": "ts,uid,id.orig_h,id.orig_p,id.resp_h,id.resp_p,path,service,native_file_system,share_type",
	"smb_files":   "ts,uid,id.orig_h,id.orig_p,id.resp_h,id.resp_p,fuid,action,path,name,size,prev_name,modified,accessed,created,changed",
	"zeekdnp3":    "ts,uid,id,fc_request,fc_reply,iin",
}
