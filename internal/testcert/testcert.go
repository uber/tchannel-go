// Copyright (c) 2022 Uber Technologies, Inc.

// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

package testcert

// TestCert is a PEM-encoded TLS cert with SAN IPs
// "127.0.0.1" and "[::1]", expiring at Jan 29 16:00:00 2084 GMT.
// generated from src/crypto/tls:
// go run "$(go env GOROOT)/src/crypto/tls/generate_cert.go"  --rsa-bits 2048 --host 127.0.0.1,::1,example.com --ca --start-date "Jan 1 00:00:00 1970" --duration=1000000h
var TestCert = []byte(`-----BEGIN CERTIFICATE-----
MIIDOTCCAiGgAwIBAgIQD2X8uKDzMVRc0crgmNX/0zANBgkqhkiG9w0BAQsFADAS
MRAwDgYDVQQKEwdBY21lIENvMCAXDTcwMDEwMTAwMDAwMFoYDzIwODQwMTI5MTYw
MDAwWjASMRAwDgYDVQQKEwdBY21lIENvMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8A
MIIBCgKCAQEAqvPofDY9ItZCO7TWb/Symnb38SuuJt4o6iTNlsE0wFPfWdYlE760
PRW2rUqE7t0M2AQwHD3OWPpzLZcqZA2aSKEyx/GmQuNUYN87idYW1JhbxD3zn14P
fflcf9s3PiWscnOM9xmPOkSvCptG9IdOs2l1TqmM91+z6AIS/M1yJvETcLJjZqTE
v5YK8RuSdTk1prgKA25HLSnwn8JFkG3L9lc0y96W2gwcW5j3+RmVie+k57pa67LD
aD2cMBDXcI+OFlDxecjtuaKJBZtbU/0QS0ehc9XXCgRvwUlg1T/MDb5Oi5z+rhuK
CP2aLd7QvTYiSgw3J0f/g52QWdBzkBaZFQIDAQABo4GIMIGFMA4GA1UdDwEB/wQE
AwICpDATBgNVHSUEDDAKBggrBgEFBQcDATAPBgNVHRMBAf8EBTADAQH/MB0GA1Ud
DgQWBBQqXSCk6h8ksO7U+3NH2nsM0GPkRjAuBgNVHREEJzAlggtleGFtcGxlLmNv
bYcEfwAAAYcQAAAAAAAAAAAAAAAAAAAAATANBgkqhkiG9w0BAQsFAAOCAQEAf4DP
yoGZ26s5IkBK5iJBpIFtIWnejBSPc7gdFmQsFb9qjRt7kQf7bKLkER0FLFmq3I0f
lsmWcYwvuLZSCQppxNB1lzcWqiE9LkHrO1wNJqcipPtOwhg9VYLgwi2BJd6mMr++
EHJntBgGpsvM4nqSanjjMlaE1ZPP2flt8/xSnikY78P7aYmHPL4xY5Al8zI09H1o
pc96r62fgMPMSDibhF5tqz5nK7Olt2Jd/alHd7LMzVOQw2DfCaBrj8OPO2J4ppvu
rqJ+Izqv7kZpwU1Ye6dFG/F8TOp1iWhkCoVR17FP6dqY1BZLfxiz3YsoS+2XVh3z
CTWY1J1Aj1WiEVBTfg==
-----END CERTIFICATE-----`)

// TestKey is the private key for TestCert.
var TestKey = []byte(`-----BEGIN PRIVATE KEY-----
MIIEvgIBADANBgkqhkiG9w0BAQEFAASCBKgwggSkAgEAAoIBAQCq8+h8Nj0i1kI7
tNZv9LKadvfxK64m3ijqJM2WwTTAU99Z1iUTvrQ9FbatSoTu3QzYBDAcPc5Y+nMt
lypkDZpIoTLH8aZC41Rg3zuJ1hbUmFvEPfOfXg99+Vx/2zc+Jaxyc4z3GY86RK8K
m0b0h06zaXVOqYz3X7PoAhL8zXIm8RNwsmNmpMS/lgrxG5J1OTWmuAoDbkctKfCf
wkWQbcv2VzTL3pbaDBxbmPf5GZWJ76TnulrrssNoPZwwENdwj44WUPF5yO25ookF
m1tT/RBLR6Fz1dcKBG/BSWDVP8wNvk6LnP6uG4oI/Zot3tC9NiJKDDcnR/+DnZBZ
0HOQFpkVAgMBAAECggEATDuyW9mwD53uMUPmMEy1bK5KyNBKu+hr5GX/DBAiXvXH
7v7Qz+pF48uQB9zoRMBsXtQXRDDHmOQugpEbhTyPpX3E8GaxVribQwupOEExMyKy
IWPjBRlj3TBa8GUoUF1qditTHEnYlgpU6GzwClFgZh9MAYUYaKPTzU1HfFZ9ZiF2
jZB841HorsAJzbTnKXpHSK51GZ0ecOPGhRMkImsAskuI/EY5RBUZJmI9vVrs0pIu
OO9TcAvSs9tNXfM8YrJwZVMG11qiCcvfHD3VuYhsYEOvCsjxSmRp4DCYlISTlUr+
LXv7VdhGMoeSdQVQqpqPF9kqkghfOzQFQ9ppzw6iDQKBgQDSmPNIY0f7nZH4diir
A0WUl7QzzUyf2qX4UrYzgGHufEfanTlrS3sTAdEkK85oxfNygLBXYmxtrzcQWVFD
gx5cXDHaH6ZVoZxSRrDyO37vrVv76NSrOH3yqq9j8gytf3M74dTcunMVOGGdx1Zi
D/AQ05KpjdKmhBDyCdGcHvXAqwKBgQDPzu8YdP56w3VNkPAlXRLZu9g5eZHj4uPF
NRexV8BdbQ8EVu3KnIjzCSUSjPdGDN18ycgTrU0AzQ8MxQE8rqebs/otPTKsYJt4
SwR/Ol+lDC+lGdSTREUu677MPE0buAce0UBQ9RtWoYUEsNEI6sFqReaCqmri55tm
ioM4T3qNPwKBgQCQU8YXDANfC2PodYH1gW6EIVucTMyAmSY5guXfcdKr0Hyl9C5P
vBECu7ILKgJxh4gKJuuzV36bxQLlr3Cj5g4+meiIZjxmXzV0pYHK4L9jntl1UOG+
3h5i2lsNEetiVAAzP9fT1evc1SEBMoWe+vE5duYCUXHWMJg0aEpAxm8BtQKBgQCX
BYBlecDnXt0E/exIexeT/RvqyRrpTp7RVwBc9bTrMLLVKIev04sDdQXoMWITGo5s
fghVpIBtsJjbYuC/RP6x/V43Ol51P9A83+fovnd77xtBFUCTte3BZ7pFmx0+o8Mo
9lGThE3V65RMEGQZ4uGlZh9bnpYHSOJ65vbuGXSq6QKBgHthfDeAsW7V4JIm0IG+
sEkFjGvYhyngDbOKMSf9YN3YuuuLPawHQJYe7gmH4p/Wry+oUcF8t5ddhwLd63xz
q4LAT9EgEvfLEbMnxjvLHUG/eeRx6zqCf54+KHfGCcooOI4kbI7lkQglLq5DWDe2
4n6AEKY0aVWJ1zN9B/vaJMZM
-----END PRIVATE KEY-----`)
