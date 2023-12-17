package main

import "testing"

var snsWithBucket = []byte(`{
  "Type" : "Notification",
  "MessageId" : "908d49d7-cfdb-50fc-9463-1421206c9674",
  "TopicArn" : "arn:aws:sns:us-west-2:005292802539:aws-cloudtrail-logs-005292802539-c48c67bc",
  "Message" : "{\"s3Bucket\":\"aws-cloudtrail-logs-005292802539-91be7236\",\"s3ObjectKey\":[\"AWSLogs/005292802539/CloudTrail/us-west-2/2023/12/16/005292802539_CloudTrail_us-west-2_20231216T0020Z_7PliVTBQKBEdK2Bd.json.gz\"]}",
  "Timestamp" : "2023-12-16T00:21:11.830Z",
  "SignatureVersion" : "1",
  "Signature" : "WnFwMXQWNiwhJtBbff9I5S0UE1Jcnv2V0oeXRPlylS3eB4hO4K50ZXnZSLJePE8zZOXq1y7aIKaIVOs1EPIk7tv8kBwC5r5dAgqc7x4Kf0OI+4DDBwjKBC9LAeUB6b5A8BAiLna+qzzZ0nsRoelVSRDqo+goKfRT+oIP+gX6fQJ84WoLMNDtrZgbI+qevlFcXw7lH/3f34LWpLnLwARXnevEpPeaR45NtYzzvhtIf1qHw6ySdbQ/T8QhaKNu2BHinoqmtNYBMUO7nqyU5Ako/Wgx/R2z2JyjXmzK3DBQNb2+AsDxLCZ5zgBCVZzeHirjtpl64zw5yTRHko2TddbPeg==",
  "SigningCertURL" : "https://sns.us-west-2.amazonaws.com/SimpleNotificationService-ABCDABCDABCDABCDABCDABCDABCDABCD.pem",
  "UnsubscribeURL" : "https://sns.us-west-2.amazonaws.com/?Action=Unsubscribe&SubscriptionArn=arn:aws:sns:us-west-2:123456789012:aws-cloudtrail-logs-123456789012-123456789:09c307ae-9d04-11ee-849c-2feff9d344f5"
}`)

var snsWithRecords = []byte(`
{
  "Type" : "Notification",
  "MessageId" : "25c7149a-9d04-11ee-bbdf-0781f22bc150",
  "TopicArn" : "arn:aws:sns:us-east-1:000:security-cloudtrail",
  "Subject" : "Amazon S3 Notification",
  "Message" : "{\"Records\":[{\"eventVersion\":\"2.1\",\"eventSource\":\"aws:s3\",\"awsRegion\":\"us-east-1\",\"eventTime\":\"2023-12-16T00:12:50.875Z\",\"eventName\":\"ObjectCreated:Put\",\"userIdentity\":{\"principalId\":\"AWS:foo:regionalDeliverySession\"},\"requestParameters\":{\"sourceIPAddress\":\"1.1.1.1\"},\"responseElements\":{\"x-amz-request-id\":\"foo\",\"x-amz-id-2\":\"foofoo\"},\"s3\":{\"s3SchemaVersion\":\"1.0\",\"configurationId\":\"64fcd58c-9d04-11ee-a6cc-ebc3a9cee567\",\"bucket\":{\"name\":\"cloudtrail\",\"ownerIdentity\":{\"principalId\":\"foo\"},\"arn\":\"arn:aws:s3:::cloudtrail\"},\"object\":{\"key\":\"foo.json.gz\",\"size\":18394,\"eTag\":\"foo\",\"versionId\":\"x\",\"sequencer\":\"012345678901234567\"}}}]}",
  "Timestamp" : "2023-12-16T00:12:52.342Z",
  "SignatureVersion" : "1",
  "Signature" : "foo",
  "SigningCertURL" : "https://sns.us-east-1.amazonaws.com/SimpleNotificationService-.pem",
  "UnsubscribeURL" : "https://sns.us-east-1.amazonaws.com/?Action=Unsubscribe&SubscriptionArn=arn:aws:sns:us-east-1"
}
`)

var s3Record = []byte(`
  {"Records":[{"eventVersion":"2.1","eventSource":"aws:s3","awsRegion":"us-east-1","eventTime":"2023-12-16T00:12:50.875Z","eventName":"ObjectCreated:Put","userIdentity":{"principalId":"AWS:foo:regionalDeliverySession"},"requestParameters":{"sourceIPAddress":"1.1.1.1"},"responseElements":{"x-amz-request-id":"foo","x-amz-id-2":"foofoo"},"s3":{"s3SchemaVersion":"1.0","configurationId":"64fcd58c-9d04-11ee-a6cc-ebc3a9cee567","bucket":{"name":"cloudtrail","ownerIdentity":{"principalId":"foo"},"arn":"arn:aws:s3:::cloudtrail"},"object":{"key":"foo.json.gz","size":18394,"eTag":"foo","versionId":"x","sequencer":"012345678901234567"}}}]}
`)

func TestDecodeSNS(t *testing.T) {
	b, k, err := snsDecode(snsWithBucket)
	if err != nil {
		t.Fatal(err)
	}
	if b[0] != "aws-cloudtrail-logs-005292802539-91be7236" || k[0] != "AWSLogs/005292802539/CloudTrail/us-west-2/2023/12/16/005292802539_CloudTrail_us-west-2_20231216T0020Z_7PliVTBQKBEdK2Bd.json.gz" {
		t.Fatal("invalid bucket/key")
	}
}

func TestDecodeSNS2(t *testing.T) {
	b, k, err := snsDecode(snsWithRecords)
	if err != nil {
		t.Fatal(err)
	}
	if b[0] != "cloudtrail" || k[0] != "foo.json.gz" {
		t.Fatal("invalid bucket/key")
	}
}

func TestDecodeS3(t *testing.T) {
	b, k, err := s3Decode(s3Record)
	if err != nil {
		t.Fatal(err)
	}
	if b[0] != "cloudtrail" || k[0] != "foo.json.gz" {
		t.Fatal("invalid bucket/key")
	}
}
