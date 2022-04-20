package tchannel_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uber/tchannel-go"
	"golang.org/x/net/context"
)

type tlsScenario struct {
	CAs        *x509.CertPool
	ServerCert *x509.Certificate
	ServerKey  *ecdsa.PrivateKey
	ClientCert *x509.Certificate
	ClientKey  *ecdsa.PrivateKey
}

func createTLSScenario(t *testing.T, clientValidity time.Duration, serverValidity time.Duration) tlsScenario {
	now := time.Now()

	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	caBytes, err := x509.CreateCertificate(
		rand.Reader,
		&x509.Certificate{
			Subject: pkix.Name{
				CommonName: "test ca",
			},
			SerialNumber:          big.NewInt(1),
			BasicConstraintsValid: true,
			IsCA:                  true,
			KeyUsage:              x509.KeyUsageCertSign,
			NotBefore:             now,
			NotAfter:              now.Add(10 * time.Minute),
		},
		&x509.Certificate{},
		caKey.Public(),
		caKey,
	)
	require.NoError(t, err)
	ca, err := x509.ParseCertificate(caBytes)
	require.NoError(t, err)

	serverKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	serverCertBytes, err := x509.CreateCertificate(
		rand.Reader,
		&x509.Certificate{
			Subject: pkix.Name{
				CommonName: "server",
			},
			NotAfter:     now.Add(serverValidity),
			SerialNumber: big.NewInt(2),
			IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
			KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyAgreement,
		},
		ca,
		serverKey.Public(),
		caKey,
	)
	require.NoError(t, err)
	serverCert, err := x509.ParseCertificate(serverCertBytes)
	require.NoError(t, err)

	clientKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	clientCertBytes, err := x509.CreateCertificate(
		rand.Reader,
		&x509.Certificate{
			Subject: pkix.Name{
				CommonName: "client",
			},
			NotAfter:     now.Add(clientValidity),
			SerialNumber: big.NewInt(3),
			KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyAgreement,
		},
		ca,
		clientKey.Public(),
		caKey,
	)
	require.NoError(t, err)
	clientCert, err := x509.ParseCertificate(clientCertBytes)
	require.NoError(t, err)

	pool := x509.NewCertPool()
	pool.AddCert(ca)

	return tlsScenario{
		CAs:        pool,
		ServerCert: serverCert,
		ServerKey:  serverKey,
		ClientCert: clientCert,
		ClientKey:  clientKey,
	}
}

func TestTLSConnectionState(t *testing.T) {
	tlsScenario := createTLSScenario(t, time.Minute, time.Minute)
	serverTlsConfig := &tls.Config{
		GetCertificate: func(_ *tls.ClientHelloInfo) (*tls.Certificate, error) {
			return &tls.Certificate{
				Certificate: [][]byte{tlsScenario.ServerCert.Raw},
				Leaf:        tlsScenario.ServerCert,
				PrivateKey:  tlsScenario.ServerKey,
			}, nil
		},
		ClientAuth: tls.RequireAndVerifyClientCert,
		ClientCAs:  tlsScenario.CAs,
	}
	clientTlsConfig := &tls.Config{
		GetClientCertificate: func(_ *tls.CertificateRequestInfo) (*tls.Certificate, error) {
			return &tls.Certificate{
				Certificate: [][]byte{tlsScenario.ClientCert.Raw},
				Leaf:        tlsScenario.ClientCert,
				PrivateKey:  tlsScenario.ClientKey,
			}, nil
		},
		RootCAs: tlsScenario.CAs,
	}

	ctx, cancel := tchannel.NewContext(time.Second)
	defer cancel()

	ch, err := tchannel.NewChannel("server", &tchannel.ChannelOptions{
		Dialer: func(ctx context.Context, network, hostPort string) (net.Conn, error) {
			return tls.Dial(network, hostPort, clientTlsConfig)
		},
	})
	require.NoError(t, err)
	defer ch.Close()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	require.NoError(t, ch.Serve(tls.NewListener(listener, serverTlsConfig)))

	handler := tchannel.HandlerFunc(func(ctx context.Context, call *tchannel.InboundCall) {
		tlsConnState := call.TLSConnectionState()
		require.NotNil(t, tlsConnState)
		require.Len(t, tlsConnState.PeerCertificates, 1)
		assert.Equal(t, tlsScenario.ClientCert, tlsConnState.PeerCertificates[0])

		buf := make([]byte, 0, 256)
		tchannel.NewArgReader(call.Arg2Reader()).Read(&buf)
		tchannel.NewArgReader(call.Arg3Reader()).Read(&buf)

		tchannel.NewArgWriter(call.Response().Arg2Writer()).Write(nil)
		tchannel.NewArgWriter(call.Response().Arg3Writer()).Write(buf)
	})
	ch.Register(handler, "handle")

	call, err := ch.BeginCall(ctx, ch.PeerInfo().HostPort, "server", "handle", &tchannel.CallOptions{})
	require.NoError(t, err)

	expectedStr := "test-message"
	require.NoError(t, tchannel.NewArgWriter(call.Arg2Writer()).Write(nil))
	require.NoError(t, tchannel.NewArgWriter(call.Arg3Writer()).Write([]byte(expectedStr)))

	resp := call.Response()
	data := make([]byte, 0, 256)
	require.NoError(t, tchannel.NewArgReader(resp.Arg2Reader()).Read(&data))
	require.NoError(t, tchannel.NewArgReader(resp.Arg3Reader()).Read(&data))
	assert.Equal(t, []byte(expectedStr), data, "result does not match arg")
}
