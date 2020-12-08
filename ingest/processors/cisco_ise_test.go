/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package processors

import (
	"testing"
	"time"
)

func TestParseRemoteHeader(t *testing.T) {
	//just test that we can parse each
	for _, tv := range testdata {
		var rih remoteISE
		if err := rih.Parse(tv); err != nil {
			t.Fatal(err)
		}
		if rih.id != 983328 {
			t.Fatalf("bad message ID: %d", rih.id)
		}
	}
	var rih remoteISE
	if err := rih.Parse(strayData); err != nil {
		t.Fatal(err)
	}
	if rih.id != 983331 {
		t.Fatalf("Bad message ID: %d", rih.id)
	}
}

func TestRemoteAssembler(t *testing.T) {
	total := append(testdata, strayData)
	ejectOn := len(testdata) - 1

	mpa := newMultipartAssembler(1024*1024, time.Second)
	for i, v := range total {
		var rih remoteISE
		if err := rih.Parse(v); err != nil {
			t.Fatal(err)
		}
		res, ejected, bad := mpa.add(rih, float64(32.0))
		if bad {
			t.Fatal("Bad value", v)
		} else if ejected {
			//make sure its the right eject ID
			if i != ejectOn {
				t.Fatal("Invalid eject sequence", i, ejectOn)
			}
			//check that we got the right thing out
			if res.output != mergedData {
				t.Fatalf("Merged data is invalid:\n\t%s\n\t%s\n", res.output, mergedData)
			} else if vf, ok := res.meta.(float64); !ok || vf != 32.0 {
				t.Fatal("Metadata object is bad")
			}
		} else if res.output != `` {
			t.Fatal("got output when we didn't want any")
		} else if res.meta != nil {
			t.Fatal("Metadata object is bad")
		}
	}

	//check that there is exactly one item left in the reassembler
	if len(mpa.tracker) != 1 {
		t.Fatal("invalid residual items")
	}

	//check that purging isn't set
	if mpa.shouldFlush() {
		t.Fatal("Flush is set when it should not be")
	}

	purgeSet := mpa.flush(false) //do not force a flush
	if len(purgeSet) != 0 {
		t.Fatal("invalid result on a flush")
	}

	//lets artificially force a purge condition and then check on the purges
	mpa.oldest = time.Now().Add(-1 * time.Minute)
	if !mpa.shouldFlush() {
		t.Fatal("Flush condition isn't set")
	}

	//should still miss
	if purgeSet = mpa.flush(false); len(purgeSet) != 0 {
		t.Fatalf("invalid number of flushed values: %d != 0", len(purgeSet))
	}

	//manually force all existing to an old value (should only be one)
	//this is a hack
	for _, v := range mpa.tracker {
		v.last = v.last.Add(-10 * time.Minute)
	}
	if purgeSet = mpa.flush(false); len(purgeSet) != 1 {
		t.Fatalf("invalid number of flushed values: %d != 1", len(purgeSet))
	}

	//check that what we got out matches the stray
	if purgeSet[0].output != strayMerged {
		t.Fatalf("Merged data is invalid:\n\t%s\n\t%s\n", purgeSet[0], strayMerged)
	} else if purgeSet[0].meta == nil {
		t.Fatalf("Merged meta is invalid")
	} else if vf, ok := purgeSet[0].meta.(float64); !ok || vf != 32.0 {
		t.Fatal("Metadata object is bad")
	}

	//force a purge
	if purgeSet = mpa.flush(true); purgeSet != nil {
		t.Fatal("got values out after a forced purge on empty")
	}
}

type testoffset struct {
	value  []byte
	offset int
}

func TestEscapedCommaReader(t *testing.T) {
	set := []testoffset{
		testoffset{offset: -1, value: []byte(``)}, //empty
		testoffset{offset: 0, value: []byte(`, this is a test,`)},
		testoffset{offset: 1, value: []byte(`h, ello`)},
		testoffset{offset: 2, value: []byte(`\,,`)},                        //skip and escaped
		testoffset{offset: -1, value: []byte(`this is a test\, no comma`)}, //
		testoffset{offset: 13, value: []byte(`hello\, world, `)},
		testoffset{offset: 23, value: []byte(`hello\, world\, testing, `)},
	}

	for _, s := range set {
		if r := indexOfNonEscapedComma(s.value); r != s.offset {
			t.Fatalf("Missed offset on %q: %d != %d", string(s.value), r, s.offset)
		}
	}
}

func TestParseISEMEssage(t *testing.T) {
	ts1, err := time.Parse(iseTimestampFormat, `2020-11-23 12:50:16.963 -05:00`)
	if err != nil {
		t.Fatal(err)
	}
	ts2, err := time.Parse(iseTimestampFormat, `2020-11-23 12:50:01.926 -05:00`)
	if err != nil {
		t.Fatal(err)
	}
	outputs := []iseMessage{
		iseMessage{
			ts:    ts1,
			seq:   1706721103,
			ode:   `5205`,
			sev:   `NOTICE`,
			class: `Dynamic-Authorization`,
			text:  `Dynamic Authorization succeeded`,
			attrs: strayMergedValues,
		},
		iseMessage{
			ts:    ts2,
			seq:   1706719405,
			ode:   `5200`,
			sev:   `NOTICE`,
			class: `Passed-Authentication`,
			text:  `Authentication succeeded`,
			attrs: mergedDataValues,
		},
	}
	inputs := []string{strayMerged, mergedData}

	for i, inp := range inputs {
		var m iseMessage
		if err := m.Parse(inp); err != nil {
			t.Fatalf("Failed to parse %q: %v", inp, err)
		} else if !m.equal(&outputs[i]) {
			t.Fatalf("input %d does not match output\n%+v\n%+v", i, m, outputs[i])
		}
	}
}

// some nasty test data
var (
	testdata = []string{
		`Nov 23 12:50:01 ISE_DEVICE CISE_Passed_Authentications 0000983328 5 0 2020-11-23 12:50:01.926 -05:00 1706719405 5200 NOTICE Passed-Authentication: Authentication succeeded, ConfigVersionId=44, Device IP Address=14.14.14.25, DestinationIPAddress=50.50.50.252, DestinationPort=1645, UserName=iamfromit@company.com, Protocol=Radius, RequestLatency=10301, NetworkDeviceName=APC-EDGVPN, User-Name=iamfromit@company.com, NAS-IP-Address=14.14.14.25, NAS-Port=486502400, Called-Station-ID=80.80.80.36, Calling-Station-ID=1.2.3.4, NAS-Port-Type=Virtual, Tunnel-Client-Endpoint=(tag=0) 1.2.3.4, cisco-av-pair=mdm-tlv=device-platform=win, cisco-av-pair=mdm-tlv=device-mac=00-0c-29-74-9d-e8, cisco-av-pair=mdm-tlv=device-platform-version=10.0.17134 , cisco-av-pair=mdm-tlv=device-public-mac=00-0c-29-74-9d-e8, cisco-av-pair=mdm-tlv=ac-user-agent=AnyConnect Windows 4.8.03052, cisco-av-pair=mdm-tlv=device-type=VMware\, Inc. VMware Virtual Platform,`,
		`Nov 23 12:50:01 -05:00 ISE_DEVICE CISE_Passed_Authentications 0000983328 5 1  cisco-av-pair=mdm-tlv=device-uid-global=2C3336E73736D3A9E146404971480D085118BBA1, cisco-av-pair=mdm-tlv=device-uid=7CBA86CABADBEDA399BF816AA27901B7E634810DD29CDC6EAE8EBDEEC583CE79, cisco-av-pair=audit-session-id=0a700e191cff70005fbbf63f, cisco-av-pair=ip:source-ip=1.2.3.4, cisco-av-pair=coa-push=true, CVPN3000/ASA/PIX7x-Tunnel-Group-Name=APC-VPN-PROFILE-POSTURE, OriginalUserName=iamfromit@company.com, NetworkDeviceProfileName=Cisco, NetworkDeviceProfileId=b0699505-3150-4215-a80e-6753d45bf56c, IsThirdPartyDeviceFlow=false, SSID=80.80.80.36, CVPN3000/ASA/PIX7x-Client-Type=2, AcsSessionID=ISE_DEVICE/384429556/212087299, AuthenticationIdentityStore=AzureBackup, AuthenticationMethod=PAP_ASCII, SelectedAccessService=Default Network Access, SelectedAuthorizationProfiles=EMPLOYEE_CORP_POSTURE_AGENT, IdentityGroup=Endpoint Identity Groups:Profiled:Workstation, Step=11001, Step=11017, Step=15049, Step=15008,`,
		`Nov 23 12:50:01 ISE_DEVICE CISE_Passed_Authentications 0000983328 5 2  Step=15048, Step=15041, Step=15048, Step=15013, Step=24638, Step=24609, Step=11100, Step=11101, Step=24612, Step=24623, Step=24638, Step=22037, Step=24715, Step=15036, Step=24432, Step=24325, Step=24313, Step=24319, Step=24323, Step=24325, Step=24313, Step=24318, Step=24315, Step=24323, Step=24355, Step=24416, Step=15048, Step=15048, Step=15048, Step=15016, Step=22081, Step=22080, Step=11002, SelectedAuthenticationIdentityStores=AzureBackup, AuthenticationStatus=AuthenticationPassed, NetworkDeviceGroups=IPSEC#Is IPSEC Device#No, NetworkDeviceGroups=Location#All Locations, NetworkDeviceGroups=Device Type#All Device Types, IdentityPolicyMatchedRule=APC-VPN-Authentication-Policy, AuthorizationPolicyMatchedRule=APC-VPN-POSTURE-WINDOWS-UNKNOWN, CPMSessionID=0a700e191cff70005fbbf63f, PostureAssessmentStatus=NotApplicable, EndPointMatchedProfile=Windows10-Workstation, ISEPolicySetName=APC-VPN-POLICY-POSTURE,`,
		`Nov 23 12:50:01 +05:00 ISE_DEVICE CISE_Passed_Authentications 0000983328 5 3  IdentitySelectionMatchedRule=APC-VPN-Authentication-Policy, StepLatency=11=9383, StepData=4= Cisco-VPN3000.CVPN3000/ASA/PIX7x-Tunnel-Group-Name, StepData=6= Cisco-VPN3000.CVPN3000/ASA/PIX7x-Tunnel-Group-Name, StepData=7=AzureBackup, StepData=8=AzureBackup, StepData=9=AzureBackup, StepData=10=( port = 1812 ), StepData=0=contoso.com, StepData=1=iamfromit@company.com, StepData=2=contoso.com, StepData=3=contoso.com, StepData=5=jmiller@contoso.com, StepData=6=contoso.com, StepData=7=contoso.com, StepData=8=contoso.com, StepData=10=contoso.com, StepData=11=contoso.com, StepData=30= contoso.com.ExternalGroups, StepData=31= EndPoints.LogicalProfile, StepData=32= Session.PostureStatus, allowEasyWiredSession=false, DTLSSupport=Unknown, HostIdentityGroup=Endpoint Identity Groups:Profiled:Workstation, Network Device Profile=Cisco, Location=Location#All Locations, Device Type=Device Type#All Device Types, IPSEC=IPSEC#Is IPSEC Device#No,`,
		`Nov 23 12:50:01 -05:30 ISE_DEVICE CISE_Passed_Authentications 0000983328 5 4  LogicalProfile=5bd60f30-bbc8-11ea-af79-069071986653, PostureStatus=Unknown, ExternalGroups=S-1-5-21-790525478-842925246-1060284298-275342, ExternalGroups=S-1-5-21-790525478-842925246-1060284298-275383, ExternalGroups=S-1-5-21-790525478-842925246-1060284298-275344, ExternalGroups=S-1-5-21-790525478-842925246-1060284298-275343, ExternalGroups=S-1-5-21-790525478-842925246-1060284298-513, Response={Class=EMPLOYEE-POSTURE-AGENT; Class=CACS:0a700e191cff70005fbbf63f:ISE_DEVICE/384429556/212087299; cisco-av-pair=profile-name=Windows10-Workstation; LicenseTypes=2051; },`,
	}
	strayData   = `Nov 23 12:50:16 ISE_DEVICE CISE_Passed_Authentications 0000983331 2 0 2020-11-23 12:50:16.963 -05:00 1706721103 5205 NOTICE Dynamic-Authorization: Dynamic Authorization succeeded, ConfigVersionId=44, Device IP Address=14.14.14.25, DestinationIPAddress=50.50.50.252, RequestLatency=78, NetworkDeviceName=APC-EDGVPN, NAS-IP-Address=14.14.14.25, Class=EMPLOYEE-POSTURE-SUCCESS, Calling-Station-ID=1.2.3.4, Acct-Session-Id=63815AAF, Event-Timestamp=1606153816, cisco-av-pair=audit-session-id=0a700e191cff70005fbbf63f, NetworkDeviceProfileName=Cisco, Device CoA type=Cisco CoA, Device CoA port=1700, NetworkDeviceProfileId=b0699505-3150-4215-a80e-6753d45bf56c, IsThirdPartyDeviceFlow=false, PostureStatus=Compliant, SelectedAuthorizationProfiles=EMPLOYEE_CORP_POSTURE_SUCCESS, Step=11100, Step=11101, NetworkDeviceGroups=IPSEC#Is IPSEC Device#No, NetworkDeviceGroups=Location#All Locations, NetworkDeviceGroups=Device Type#All Device Types, AuthorizationPolicyMatchedRule=APC-VPN-POSTURE-SUCCESS,`
	strayMerged = `2020-11-23 12:50:16.963 -05:00 1706721103 5205 NOTICE Dynamic-Authorization: Dynamic Authorization succeeded, ConfigVersionId=44, Device IP Address=14.14.14.25, DestinationIPAddress=50.50.50.252, RequestLatency=78, NetworkDeviceName=APC-EDGVPN, NAS-IP-Address=14.14.14.25, Class=EMPLOYEE-POSTURE-SUCCESS, Calling-Station-ID=1.2.3.4, Acct-Session-Id=63815AAF, Event-Timestamp=1606153816, cisco-av-pair=audit-session-id=0a700e191cff70005fbbf63f, NetworkDeviceProfileName=Cisco, Device CoA type=Cisco CoA, Device CoA port=1700, NetworkDeviceProfileId=b0699505-3150-4215-a80e-6753d45bf56c, IsThirdPartyDeviceFlow=false, PostureStatus=Compliant, SelectedAuthorizationProfiles=EMPLOYEE_CORP_POSTURE_SUCCESS, Step=11100, Step=11101, NetworkDeviceGroups=IPSEC#Is IPSEC Device#No, NetworkDeviceGroups=Location#All Locations, NetworkDeviceGroups=Device Type#All Device Types, AuthorizationPolicyMatchedRule=APC-VPN-POSTURE-SUCCESS,`

	mergedData = `2020-11-23 12:50:01.926 -05:00 1706719405 5200 NOTICE Passed-Authentication: Authentication succeeded, ConfigVersionId=44, Device IP Address=14.14.14.25, DestinationIPAddress=50.50.50.252, DestinationPort=1645, UserName=iamfromit@company.com, Protocol=Radius, RequestLatency=10301, NetworkDeviceName=APC-EDGVPN, User-Name=iamfromit@company.com, NAS-IP-Address=14.14.14.25, NAS-Port=486502400, Called-Station-ID=80.80.80.36, Calling-Station-ID=1.2.3.4, NAS-Port-Type=Virtual, Tunnel-Client-Endpoint=(tag=0) 1.2.3.4, cisco-av-pair=mdm-tlv=device-platform=win, cisco-av-pair=mdm-tlv=device-mac=00-0c-29-74-9d-e8, cisco-av-pair=mdm-tlv=device-platform-version=10.0.17134 , cisco-av-pair=mdm-tlv=device-public-mac=00-0c-29-74-9d-e8, cisco-av-pair=mdm-tlv=ac-user-agent=AnyConnect Windows 4.8.03052, cisco-av-pair=mdm-tlv=device-type=VMware\, Inc. VMware Virtual Platform, cisco-av-pair=mdm-tlv=device-uid-global=2C3336E73736D3A9E146404971480D085118BBA1, cisco-av-pair=mdm-tlv=device-uid=7CBA86CABADBEDA399BF816AA27901B7E634810DD29CDC6EAE8EBDEEC583CE79, cisco-av-pair=audit-session-id=0a700e191cff70005fbbf63f, cisco-av-pair=ip:source-ip=1.2.3.4, cisco-av-pair=coa-push=true, CVPN3000/ASA/PIX7x-Tunnel-Group-Name=APC-VPN-PROFILE-POSTURE, OriginalUserName=iamfromit@company.com, NetworkDeviceProfileName=Cisco, NetworkDeviceProfileId=b0699505-3150-4215-a80e-6753d45bf56c, IsThirdPartyDeviceFlow=false, SSID=80.80.80.36, CVPN3000/ASA/PIX7x-Client-Type=2, AcsSessionID=ISE_DEVICE/384429556/212087299, AuthenticationIdentityStore=AzureBackup, AuthenticationMethod=PAP_ASCII, SelectedAccessService=Default Network Access, SelectedAuthorizationProfiles=EMPLOYEE_CORP_POSTURE_AGENT, IdentityGroup=Endpoint Identity Groups:Profiled:Workstation, Step=11001, Step=11017, Step=15049, Step=15008, Step=15048, Step=15041, Step=15048, Step=15013, Step=24638, Step=24609, Step=11100, Step=11101, Step=24612, Step=24623, Step=24638, Step=22037, Step=24715, Step=15036, Step=24432, Step=24325, Step=24313, Step=24319, Step=24323, Step=24325, Step=24313, Step=24318, Step=24315, Step=24323, Step=24355, Step=24416, Step=15048, Step=15048, Step=15048, Step=15016, Step=22081, Step=22080, Step=11002, SelectedAuthenticationIdentityStores=AzureBackup, AuthenticationStatus=AuthenticationPassed, NetworkDeviceGroups=IPSEC#Is IPSEC Device#No, NetworkDeviceGroups=Location#All Locations, NetworkDeviceGroups=Device Type#All Device Types, IdentityPolicyMatchedRule=APC-VPN-Authentication-Policy, AuthorizationPolicyMatchedRule=APC-VPN-POSTURE-WINDOWS-UNKNOWN, CPMSessionID=0a700e191cff70005fbbf63f, PostureAssessmentStatus=NotApplicable, EndPointMatchedProfile=Windows10-Workstation, ISEPolicySetName=APC-VPN-POLICY-POSTURE, IdentitySelectionMatchedRule=APC-VPN-Authentication-Policy, StepLatency=11=9383, StepData=4= Cisco-VPN3000.CVPN3000/ASA/PIX7x-Tunnel-Group-Name, StepData=6= Cisco-VPN3000.CVPN3000/ASA/PIX7x-Tunnel-Group-Name, StepData=7=AzureBackup, StepData=8=AzureBackup, StepData=9=AzureBackup, StepData=10=( port = 1812 ), StepData=0=contoso.com, StepData=1=iamfromit@company.com, StepData=2=contoso.com, StepData=3=contoso.com, StepData=5=jmiller@contoso.com, StepData=6=contoso.com, StepData=7=contoso.com, StepData=8=contoso.com, StepData=10=contoso.com, StepData=11=contoso.com, StepData=30= contoso.com.ExternalGroups, StepData=31= EndPoints.LogicalProfile, StepData=32= Session.PostureStatus, allowEasyWiredSession=false, DTLSSupport=Unknown, HostIdentityGroup=Endpoint Identity Groups:Profiled:Workstation, Network Device Profile=Cisco, Location=Location#All Locations, Device Type=Device Type#All Device Types, IPSEC=IPSEC#Is IPSEC Device#No, LogicalProfile=5bd60f30-bbc8-11ea-af79-069071986653, PostureStatus=Unknown, ExternalGroups=S-1-5-21-790525478-842925246-1060284298-275342, ExternalGroups=S-1-5-21-790525478-842925246-1060284298-275383, ExternalGroups=S-1-5-21-790525478-842925246-1060284298-275344, ExternalGroups=S-1-5-21-790525478-842925246-1060284298-275343, ExternalGroups=S-1-5-21-790525478-842925246-1060284298-513, Response={Class=EMPLOYEE-POSTURE-AGENT; Class=CACS:0a700e191cff70005fbbf63f:ISE_DEVICE/384429556/212087299; cisco-av-pair=profile-name=Windows10-Workstation; LicenseTypes=2051; },`
)

var strayMergedValues = []iseKV{
	iseKV{key: `ConfigVersionId`, value: `44`},
	iseKV{key: `Device IP Address`, value: `14.14.14.25`},
	iseKV{key: `DestinationIPAddress`, value: `50.50.50.252`},
	iseKV{key: `RequestLatency`, value: `78`},
	iseKV{key: `NetworkDeviceName`, value: `APC-EDGVPN`},
	iseKV{key: `NAS-IP-Address`, value: `14.14.14.25`},
	iseKV{key: `Class`, value: `EMPLOYEE-POSTURE-SUCCESS`},
	iseKV{key: `Calling-Station-ID`, value: `1.2.3.4`},
	iseKV{key: `Acct-Session-Id`, value: `63815AAF`},
	iseKV{key: `Event-Timestamp`, value: `1606153816`},
	iseKV{key: `cisco-av-pair`, value: `audit-session-id=0a700e191cff70005fbbf63f`},
	iseKV{key: `NetworkDeviceProfileName`, value: `Cisco`},
	iseKV{key: `Device CoA type`, value: `Cisco CoA`},
	iseKV{key: `Device CoA port`, value: `1700`},
	iseKV{key: `NetworkDeviceProfileId`, value: `b0699505-3150-4215-a80e-6753d45bf56c`},
	iseKV{key: `IsThirdPartyDeviceFlow`, value: `false`},
	iseKV{key: `PostureStatus`, value: `Compliant`},
	iseKV{key: `SelectedAuthorizationProfiles`, value: `EMPLOYEE_CORP_POSTURE_SUCCESS`},
	iseKV{key: `Step`, value: `11100`},
	iseKV{key: `Step`, value: `11101`},
	iseKV{key: `NetworkDeviceGroups`, value: `IPSEC#Is IPSEC Device#No`},
	iseKV{key: `NetworkDeviceGroups`, value: `Location#All Locations`},
	iseKV{key: `NetworkDeviceGroups`, value: `Device Type#All Device Types`},
	iseKV{key: `AuthorizationPolicyMatchedRule`, value: `APC-VPN-POSTURE-SUCCESS`},
}

var mergedDataValues = []iseKV{
	iseKV{key: `ConfigVersionId`, value: `44`},
	iseKV{key: `Device IP Address`, value: `14.14.14.25`},
	iseKV{key: `DestinationIPAddress`, value: `50.50.50.252`},
	iseKV{key: `DestinationPort`, value: `1645`},
	iseKV{key: `UserName`, value: `iamfromit@company.com`},
	iseKV{key: `Protocol`, value: `Radius`},
	iseKV{key: `RequestLatency`, value: `10301`},
	iseKV{key: `NetworkDeviceName`, value: `APC-EDGVPN`},
	iseKV{key: `User-Name`, value: `iamfromit@company.com`},
	iseKV{key: `NAS-IP-Address`, value: `14.14.14.25`},
	iseKV{key: `NAS-Port`, value: `486502400`},
	iseKV{key: `Called-Station-ID`, value: `80.80.80.36`},
	iseKV{key: `Calling-Station-ID`, value: `1.2.3.4`},
	iseKV{key: `NAS-Port-Type`, value: `Virtual`},
	iseKV{key: `Tunnel-Client-Endpoint`, value: `(tag=0) 1.2.3.4`},
	iseKV{key: `cisco-av-pair`, value: `mdm-tlv=device-platform=win`},
	iseKV{key: `cisco-av-pair`, value: `mdm-tlv=device-mac=00-0c-29-74-9d-e8`},
	iseKV{key: `cisco-av-pair`, value: `mdm-tlv=device-platform-version=10.0.17134`},
	iseKV{key: `cisco-av-pair`, value: `mdm-tlv=device-public-mac=00-0c-29-74-9d-e8`},
	iseKV{key: `cisco-av-pair`, value: `mdm-tlv=ac-user-agent=AnyConnect Windows 4.8.03052`},
	iseKV{key: `cisco-av-pair`, value: `mdm-tlv=device-type=VMware\, Inc. VMware Virtual Platform`},
	iseKV{key: `cisco-av-pair`, value: `mdm-tlv=device-uid-global=2C3336E73736D3A9E146404971480D085118BBA1`},
	iseKV{key: `cisco-av-pair`, value: `mdm-tlv=device-uid=7CBA86CABADBEDA399BF816AA27901B7E634810DD29CDC6EAE8EBDEEC583CE79`},
	iseKV{key: `cisco-av-pair`, value: `audit-session-id=0a700e191cff70005fbbf63f`},
	iseKV{key: `cisco-av-pair`, value: `ip:source-ip=1.2.3.4`},
	iseKV{key: `cisco-av-pair`, value: `coa-push=true`},
	iseKV{key: `CVPN3000/ASA/PIX7x-Tunnel-Group-Name`, value: `APC-VPN-PROFILE-POSTURE`},
	iseKV{key: `OriginalUserName`, value: `iamfromit@company.com`},
	iseKV{key: `NetworkDeviceProfileName`, value: `Cisco`},
	iseKV{key: `NetworkDeviceProfileId`, value: `b0699505-3150-4215-a80e-6753d45bf56c`},
	iseKV{key: `IsThirdPartyDeviceFlow`, value: `false`},
	iseKV{key: `SSID`, value: `80.80.80.36`},
	iseKV{key: `CVPN3000/ASA/PIX7x-Client-Type`, value: `2`},
	iseKV{key: `AcsSessionID`, value: `ISE_DEVICE/384429556/212087299`},
	iseKV{key: `AuthenticationIdentityStore`, value: `AzureBackup`},
	iseKV{key: `AuthenticationMethod`, value: `PAP_ASCII`},
	iseKV{key: `SelectedAccessService`, value: `Default Network Access`},
	iseKV{key: `SelectedAuthorizationProfiles`, value: `EMPLOYEE_CORP_POSTURE_AGENT`},
	iseKV{key: `IdentityGroup`, value: `Endpoint Identity Groups:Profiled:Workstation`},
	iseKV{key: `Step`, value: `11001`},
	iseKV{key: `Step`, value: `11017`},
	iseKV{key: `Step`, value: `15049`},
	iseKV{key: `Step`, value: `15008`},
	iseKV{key: `Step`, value: `15048`},
	iseKV{key: `Step`, value: `15041`},
	iseKV{key: `Step`, value: `15048`},
	iseKV{key: `Step`, value: `15013`},
	iseKV{key: `Step`, value: `24638`},
	iseKV{key: `Step`, value: `24609`},
	iseKV{key: `Step`, value: `11100`},
	iseKV{key: `Step`, value: `11101`},
	iseKV{key: `Step`, value: `24612`},
	iseKV{key: `Step`, value: `24623`},
	iseKV{key: `Step`, value: `24638`},
	iseKV{key: `Step`, value: `22037`},
	iseKV{key: `Step`, value: `24715`},
	iseKV{key: `Step`, value: `15036`},
	iseKV{key: `Step`, value: `24432`},
	iseKV{key: `Step`, value: `24325`},
	iseKV{key: `Step`, value: `24313`},
	iseKV{key: `Step`, value: `24319`},
	iseKV{key: `Step`, value: `24323`},
	iseKV{key: `Step`, value: `24325`},
	iseKV{key: `Step`, value: `24313`},
	iseKV{key: `Step`, value: `24318`},
	iseKV{key: `Step`, value: `24315`},
	iseKV{key: `Step`, value: `24323`},
	iseKV{key: `Step`, value: `24355`},
	iseKV{key: `Step`, value: `24416`},
	iseKV{key: `Step`, value: `15048`},
	iseKV{key: `Step`, value: `15048`},
	iseKV{key: `Step`, value: `15048`},
	iseKV{key: `Step`, value: `15016`},
	iseKV{key: `Step`, value: `22081`},
	iseKV{key: `Step`, value: `22080`},
	iseKV{key: `Step`, value: `11002`},
	iseKV{key: `SelectedAuthenticationIdentityStores`, value: `AzureBackup`},
	iseKV{key: `AuthenticationStatus`, value: `AuthenticationPassed`},
	iseKV{key: `NetworkDeviceGroups`, value: `IPSEC#Is IPSEC Device#No`},
	iseKV{key: `NetworkDeviceGroups`, value: `Location#All Locations`},
	iseKV{key: `NetworkDeviceGroups`, value: `Device Type#All Device Types`},
	iseKV{key: `IdentityPolicyMatchedRule`, value: `APC-VPN-Authentication-Policy`},
	iseKV{key: `AuthorizationPolicyMatchedRule`, value: `APC-VPN-POSTURE-WINDOWS-UNKNOWN`},
	iseKV{key: `CPMSessionID`, value: `0a700e191cff70005fbbf63f`},
	iseKV{key: `PostureAssessmentStatus`, value: `NotApplicable`},
	iseKV{key: `EndPointMatchedProfile`, value: `Windows10-Workstation`},
	iseKV{key: `ISEPolicySetName`, value: `APC-VPN-POLICY-POSTURE`},
	iseKV{key: `IdentitySelectionMatchedRule`, value: `APC-VPN-Authentication-Policy`},
	iseKV{key: `StepLatency`, value: `11=9383`},
	iseKV{key: `StepData`, value: `4= Cisco-VPN3000.CVPN3000/ASA/PIX7x-Tunnel-Group-Name`},
	iseKV{key: `StepData`, value: `6= Cisco-VPN3000.CVPN3000/ASA/PIX7x-Tunnel-Group-Name`},
	iseKV{key: `StepData`, value: `7=AzureBackup`},
	iseKV{key: `StepData`, value: `8=AzureBackup`},
	iseKV{key: `StepData`, value: `9=AzureBackup`},
	iseKV{key: `StepData`, value: `10=( port = 1812 )`},
	iseKV{key: `StepData`, value: `0=contoso.com`},
	iseKV{key: `StepData`, value: `1=iamfromit@company.com`},
	iseKV{key: `StepData`, value: `2=contoso.com`},
	iseKV{key: `StepData`, value: `3=contoso.com`},
	iseKV{key: `StepData`, value: `5=jmiller@contoso.com`},
	iseKV{key: `StepData`, value: `6=contoso.com`},
	iseKV{key: `StepData`, value: `7=contoso.com`},
	iseKV{key: `StepData`, value: `8=contoso.com`},
	iseKV{key: `StepData`, value: `10=contoso.com`},
	iseKV{key: `StepData`, value: `11=contoso.com`},
	iseKV{key: `StepData`, value: `30= contoso.com.ExternalGroups`},
	iseKV{key: `StepData`, value: `31= EndPoints.LogicalProfile`},
	iseKV{key: `StepData`, value: `32= Session.PostureStatus`},
	iseKV{key: `allowEasyWiredSession`, value: `false`},
	iseKV{key: `DTLSSupport`, value: `Unknown`},
	iseKV{key: `HostIdentityGroup`, value: `Endpoint Identity Groups:Profiled:Workstation`},
	iseKV{key: `Network Device Profile`, value: `Cisco`},
	iseKV{key: `Location`, value: `Location#All Locations`},
	iseKV{key: `Device Type`, value: `Device Type#All Device Types`},
	iseKV{key: `IPSEC`, value: `IPSEC#Is IPSEC Device#No`},
	iseKV{key: `LogicalProfile`, value: `5bd60f30-bbc8-11ea-af79-069071986653`},
	iseKV{key: `PostureStatus`, value: `Unknown`},
	iseKV{key: `ExternalGroups`, value: `S-1-5-21-790525478-842925246-1060284298-275342`},
	iseKV{key: `ExternalGroups`, value: `S-1-5-21-790525478-842925246-1060284298-275383`},
	iseKV{key: `ExternalGroups`, value: `S-1-5-21-790525478-842925246-1060284298-275344`},
	iseKV{key: `ExternalGroups`, value: `S-1-5-21-790525478-842925246-1060284298-275343`},
	iseKV{key: `ExternalGroups`, value: `S-1-5-21-790525478-842925246-1060284298-513`},
	iseKV{key: `Response`, value: `{Class=EMPLOYEE-POSTURE-AGENT; Class=CACS:0a700e191cff70005fbbf63f:ISE_DEVICE/384429556/212087299; cisco-av-pair=profile-name=Windows10-Workstation; LicenseTypes=2051; }`},
}
