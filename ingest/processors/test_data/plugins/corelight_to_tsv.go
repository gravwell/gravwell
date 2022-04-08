package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"gravwell" //package expose the builtin plugin funcs

	"github.com/gravwell/gravwell/v3/ingest/entry"
)

const (
	PluginName = "corelight"
	defaultTag = ``
)

var (
	tg gravwell.Tagger
)

func main() {
	makeTagFields()
	if err := gravwell.Execute(PluginName, Config, nop, nop, Process, Flush); err != nil {
		panic(fmt.Sprintf("Failed to execute dynamic plugin %s - %v\n", PluginName, err))
	}
}

func nop() error {
	return nil //this is a synchronous plugin, so no "start" or "close"
}

func Config(cm gravwell.ConfigMap, tgr gravwell.Tagger) (err error) {
	if cm == nil || tgr == nil {
		err = errors.New("bad parameters")
	}
	tg = tgr
	return
}

func Flush() []*entry.Entry {
	return nil //we don't hold on to anything
}

func Process(ents []*entry.Entry) ([]*entry.Entry, error) {
	for _, e := range ents {
		if e == nil || len(e.Data) == 0 {
			continue
		} else if tag, line := processLine(e.Data); tag != defaultTag {
			//reroute
			if tv, err := tg.NegotiateTag(tag); err == nil {
				e.Tag = tv
				e.Data = line
				println("FIXING")
			} else {
				println("NO NEGOTIATE", tag, err)
			}
		}
	}
	return ents, nil
}

func processLine(s []byte) (tag string, line []byte) {
	mp := map[string]interface{}{}
	line = s
	if idx := bytes.IndexByte(line, '{'); idx == -1 {
		println("\t\tNO JSON")
		tag = defaultTag
		return
	} else {
		line = line[idx:]
	}
	if err := json.Unmarshal(line, &mp); err != nil {
		tag = defaultTag
		println("\t\tNO UNMARSHAL", err)
		return
	}
	tag, line = process(mp, line)
	return
}

func clean(ff map[string]interface{}) {
	for k, v := range ff {
		if len(k) == 0 {
			continue
		}
		if bits := strings.Split(k, "."); len(bits) > 0 {
			ff[bits[len(bits)-1]] = v
		}
	}
	return
}

func process(mp map[string]interface{}, og []byte) (tag string, line []byte) {
	clean(mp)
	var ok bool
	var headers []string
	var ts time.Time
	if len(mp) == 0 {
		println("bad mp")
		tag = defaultTag
		line = og
	} else if tag, ts, ok = getTagTs(mp); !ok {
		println("could not get tag or timestamp")
		tag = defaultTag
		line = og
	} else if headers, ok = tagFields[tag]; !ok {
		println("no headers for", tag)
		tag = defaultTag
		line = og
	} else if line, ok = emitLine(ts, headers, mp); !ok {
		println("emit failed")
		tag = defaultTag
		line = og
	}
	println("good to go")
	return
}

func getTagTs(mp map[string]interface{}) (tag string, ts time.Time, ok bool) {
	var tagv interface{}
	var tsv interface{}
	var tss string
	var tagval string
	var err error
	if tagv, ok = mp["_path"]; !ok {
		println("no path", mp)
		return
	} else if tsv, ok = mp["ts"]; !ok {
		println("no ts")
		return
	} else if tagval, ok = tagv.(string); !ok {
		println(tagv, "not a string")
		return
	} else if tss, ok = tsv.(string); !ok {
		println(tsv, "not a string")
		return
	} else if ts, err = time.Parse(time.RFC3339, tss); err != nil {
		println("parse fail", tss, err)
		ok = false
	} else {
		tag = "zeek" + tagval
	}
	println("TAG TS", tag, ts)
	return
}

func emitLine(ts time.Time, headers []string, mp map[string]interface{}) (line []byte, ok bool) {
	bb := bytes.NewBuffer(nil)
	var f64 float64
	fmt.Fprintf(bb, "%.3f", float64(ts.UnixNano())/1000000000.0)
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
			fmt.Fprintf(bb, "\t")
		}
	}
	line, ok = bb.Bytes(), true
	return
}

func makeTagFields() {
	tagFields = make(map[string][]string, len(tagHeaders))
	var k, v string
	for k, v = range tagHeaders {
		tagFields[k] = strings.Split(v, ",")
	}
}

var tagFields map[string][]string

var tagHeaders = map[string]string{
	"zeekconn":        "ts,uid,orig_h,orig_p,resp_h,resp_p,proto,service,duration,orig_ip_bytes,resp_ip_bytes,conn_state,local_orig,local_resp,missed_bytes,history,orig_pkts,orig_ip_bytes,resp_pkts,resp_ip_bytes,tunnel_parents,vlan",
	"zeekdhcp":        "ts,uids,client_addr,server_addr,mac,host_name,client_fqdn,domain,requested_addr,assigned_addr,lease_time,client_message,server_message,msg_types,duration",
	"zeekdns":         "ts,uid,orig_h,orig_p,resp_h,resp_p,proto,trans_id,rtt,query,qclass,qclass_name,qtype,qtype_name,rcode,rcode_name,AA,TC,RD,RA,Z,answers,TTLs,rejected",
	"zeekfiles":       "ts,fuid,tx_hosts,rx_hosts,conn_uids,source,depth,analyzers,mime_type,filename,duration,local_orig,is_orig,seen_bytes,total_bytes,missing_bytes,overflow_bytes,timedout,parent_fuid,md5,sha1,sha256,extracted,extracted_cutoff,extracted_size",
	"zeekhttp":        "ts,uid,orig_h,orig_p,resp_h,resp_p,trans_depth,method,host,uri,referrer,version,user_agent,origin,request_body_len,response_body_len,status_code,status_msg,info_code,info_msg,tags,username,password,proxied,orig_fuids,orig_filenames,orig_mime_types,resp_fuids,resp_filenames,resp_mime_types",
	"zeekssl":         "ts,uid,orig_h,orig_p,resp_h,resp_p,version,cipher,curve,server_name,resumed,last_alert,next_protocol,established,cert_chain_fuids,client_cert_chain_fuids,subject,issuer,client_subject,client_issuer,validation_status",
	"zeekweird":       "ts,uid,orig_h,orig_p,resp_h,resp_p,name,addl,notice,peer",
	"zeekx509":        "ts,uid,version,serial,subject,issuer,not_valid_before,not_valid_after,key_alg,sig_alg,key_type,key_length,exponent,curve,dns,uri,email,ip,ca,path_len",
	"zeekssh":         "ts,uid,orig_h,orig_p,resp_h,resp_p,version,auth_success,auth_attempts,direction,client,server,cipher_alg,mac_alg,compression_alg,kex_alg,host_key_alg,host_key",
	"zeeksip":         "ts,uid,orig_h,orig_p,resp_h,resp_p,trans_depth,method,uri,date,request_fromrequest_to,response_from,response_to,reply_to,call_id,seq,subject,request_path,response_path,user_agent,status_code,status_msg,warning,request_body_len,response_body_len,content_type",
	"zeekdpd":         "ts,uid,orig_h,orig_p,resp_h,resp_p,proto,analyzer,failure_reason,packet_segment",
	"zeeksnmp":        "ts,uid,orig_h,orig_p,resp_h,resp_p,duration,version,community,get_requests,get_bulk_requests,get_responses,set_requests,display_string,up_since",
	"zeeksmtp":        "ts,uid,orig_h,orig_p,resp_h,resp_p,trans_depth,helo,mailfrom",
	"zeekpe":          "ts,uid,machine,compile_ts,os,subsystem,is_exe,is_64bit,uses_aslr,uses_dep",
	"zeektunnel":      "ts,uid,orig_h,orig_p,resp_h,resp_p,tunnel_type,action",
	"zeeksocks":       "ts,uid,orig_h,orig_p,resp_h,resp_p,version,user,password,status,request,request_host,request_name,request_port,bound_host,bound_name",
	"zeeksoftware":    "ts,host,host_port,software_type,name,major,minor,minor2,minor3,addl,unparsed_version",
	"zeeksyslog":      "ts,uid,orig_h,orig_p,resp_h,resp_p,proto,facility,severity,message",
	"zeekrfb":         "ts,uid,orig_h,orig_p,resp_h,resp_p,client_major_version,client_minor_version,server_major_version,server_minor_version,authentication_method,auth,share_flag,desktop_name,width,height",
	"zeekradius":      "ts,uid,orig_h,orig_p,resp_h,resp_p,username,mac,remote_ip,connect_info,result,logged",
	"zeekrdp":         "ts,uid,orig_h,orig_p,resp_h,resp_p,cookie,result,security_protocol,client_build,client_name,client_dig_product_id,desktop_width,desktop_height,requested_color_depth,cert_type,cert_count,cert_permanent,encryption_level,encryption_method",
	"zeekftp":         "ts,uid,orig_h,orig_p,resp_h,resp_p,user,password,command,arg,mime_type,file_size,reply_code,reply_msg,data_channel_passive,data_channel_source_ip,data_channel_destination_ip,data_channel_destination_port",
	"zeekintel":       "ts,uid,orig_h,orig_p,resp_h,resp_p,indicator,indicator_type,seen_where,seen_node,matched,sources,fuid,file_mime_type,file_desc",
	"zeekirc":         "ts,uid,orig_h,orig_p,resp_h,resp_p,nick,user,command,value,additional_info,dcc_file_name,dcc_file_size,dcc_mime_type,fuid",
	"zeekkerberos":    "ts,uid,orig_h,orig_p,resp_h,resp_p,request_type,client,service,success,error_msg,from,till,cipher,forwardable,renewable,client_cert,client_cert_fuid,server_cert_subject,server_cert_fuid",
	"zeekmysql":       "ts,uid,orig_h,orig_p,resp_h,resp_p,cmd,arg,success,rows,response",
	"zeekmodbus":      "ts,uid,orig_h,orig_p,resp_h,resp_p,func,exception",
	"zeeknotice":      "ts,uid,orig_h,orig_p,resp_h,resp_p,fuid,mime,desc,proto,note,msg,sub,src,dst,p,n,peer_descr,actions,suppress_for,dropped,destination_country_code,destination_region,destination_city,destination_latitude,destination_longitude",
	"zeeksignature":   "ts,uid,orig_h,orig_p,resp_h,resp_p,note,sig_id,event_msg,sub_msg,sig_count,host_count",
	"zeeksmb_mapping": "ts,uid,orig_h,orig_p,resp_h,resp_p,path,service,native_file_system,share_type",
	"zeeksmb_files":   "ts,uid,orig_h,orig_p,resp_h,resp_p,fuid,action,path,name,size,prev_name,modified,accessed,created,changed",
}
