/*************************************************************************
 * Copyright 2019 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

var (
	protos   = []string{"icmp", "tcp", "udp"}
	alphabet = []byte("0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
	services = map[string][]string{
		"icmp": []string{"-"},
		"tcp":  []string{"-", "http", "ssl", "ssh"},
		"udp":  []string{"-", "dns", "dhcp", "krb", "dtls"},
	}
	states = []string{
		"OTH",
		"SF",
		"S0",
		"SH",
		"SHR",
		"RSTR",
		"S1",
		"RSTO",
		"RSTRH",
		"S3",
	}

	histories = []string{
		"Dc",
		"ShADadFf",
		"-",
		"Dd",
		"C",
		"D",
		"HcADF",
		"ShADadfF",
		"CC",
		"S",
		"ShADadFRfR",
		"DadA",
		"^dDa",
		"ShADad",
		"ShADadfFr",
		"ShADadR",
		"HcAD",
		"ShADadFfR",
		"Cd",
		"^d",
		"ShADdaFf",
		"DFdfR",
		"ShR",
		"^dADa",
		"Sh",
		"ShADadfR",
		"^dDaA",
		"ShADadtFf",
		"ShADadf",
		"ShADadfFR",
		"ShAFf",
		"ShADadFfrrr",
		"DFafA",
		"ShADadtfFrr",
		"ShADadFRf",
		"ShADadr",
		"^dfADFr",
		"ShADdaFfR",
		"DFdfrR",
		"ShADdFaf",
		"ShADadtFfR",
		"ShADadfr",
		"ShADadttttFf",
		"ShADadFfrr",
		"ShADadtttFf",
		"ShADaFdRfR",
		"ShADadFfT",
		"ShADadFfRRR",
		"ShADFadRfR",
		"ShADadtfRrr",
		"ShADadtfF",
		"ShADFadfR",
		"DFr",
		"DadAt",
		"DFdrrR",
		"DadAf",
		"^dfA",
		"ShADFadRf",
		"Fr",
		"ShADadtR",
		"ShADadFfRR",
		"ShADdfFa",
		"ShADadtfFR",
		"ShADadftR",
		"DFdrR",
		"DFadfR",
		"ShADadttf",
		"SW",
		"ShADadTtfFrr",
		"^r",
		"ShADadFTfR",
	}
)
