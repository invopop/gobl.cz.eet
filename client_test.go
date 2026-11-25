package eet

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/invopop/gobl"
	"github.com/invopop/gobl.cz.eet/addon"
	"github.com/invopop/gobl/head"
	"github.com/invopop/gobl/note"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"software.sslmate.com/src/go-pkcs12"
)

// testService is a TLS server that also offers HTTP/2, and a client whose
// own transport is pointed at it: only the server's certificate is trusted
// and connections go to its address, the rest of the transport is the
// module's.
type testService struct {
	server *httptest.Server
	client *Client
	calls  atomic.Int32
}

func newTestService(t *testing.T, handler http.HandlerFunc, opts ...Option) *testService {
	t.Helper()
	s := &testService{}
	s.server = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.calls.Add(1)
		handler(w, r)
	}))
	s.server.EnableHTTP2 = true
	s.server.StartTLS()
	t.Cleanup(s.server.Close)

	opts = append([]Option{InPlayground(), WithCertificate(newTestXMLCertificate(t))}, opts...)
	client, err := New(opts...)
	require.NoError(t, err)
	transport := client.httpClient.Transport.(*http.Transport)
	roots := x509.NewCertPool()
	roots.AddCert(s.server.Certificate())
	transport.TLSClientConfig.RootCAs = roots
	transport.TLSClientConfig.ServerName = "example.com"
	addr := s.server.Listener.Addr().String()
	transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}
	s.client = client
	return s
}

// errorSale is a sale whose message UUID the error-schema reply answers.
func errorSale() *Sale {
	return &Sale{
		Header: &Header{UUID: errorSchemaUUID, SentAt: "2026-11-06T12:18:27+01:00", FirstAttempt: true},
		Data: &Data{
			TaxID: "CZ00000019", UnitID: "303", DeviceID: "POS-1", Number: "PRG/000135",
			SoldAt: "2026-11-06T12:18:26+01:00", Total: "205.00",
		},
	}
}

// The request is a SOAP 1.1 POST over HTTP/1.1 (interface description,
// 2.3.2) with the content type and the quoted SOAPAction of the WSDL, and
// the reply comes back with the exchange's X-Global-Transaction-Id (5.4).
func TestSendRequest(t *testing.T) {
	var proto, method, contentType, soapAction string
	s := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		proto, method = r.Proto, r.Method
		contentType, soapAction = r.Header.Get("Content-Type"), r.Header.Get("SOAPAction")
		w.Header().Set("X-Global-Transaction-Id", "a1b2c3")
		_, _ = w.Write(readResponse(t, "verification"))
	})
	sale := errorSale()
	sale.Header.UUID = verificationUUID
	sale.Header.Verification = ptr(true)
	res, err := s.client.Send(context.Background(), sale)
	require.NoError(t, err)

	assert.Equal(t, "HTTP/1.1", proto)
	assert.Equal(t, http.MethodPost, method)
	assert.Equal(t, "text/xml; charset=utf-8", contentType)
	assert.Equal(t, `"http://fs.gov.cz/eet/OdeslaniTrzby"`, soapAction)
	assert.Equal(t, ErrorCodeVerified, res.Error.Code)
	assert.Equal(t, "a1b2c3", res.TransactionID)
}

// Error code 0 to a message not in verification mode leaves the sale
// without a POK, so it is no answer.
func TestSendVerificationCodeOutsideVerification(t *testing.T) {
	s := newTestService(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(readResponse(t, "verification"))
	})
	sale := errorSale()
	sale.Header.UUID = verificationUUID
	_, err := s.client.Send(context.Background(), sale)
	assert.ErrorIs(t, err, ErrConnection)
	assert.ErrorContains(t, err, "error code 0 answers a message not in verification mode")
}

// An error (Chyba) comes back as a structured error with its code: one to
// send again later is ErrServer, any other ErrValidation.
func TestSendResponseError(t *testing.T) {
	s := newTestService(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Global-Transaction-Id", "a1b2c3")
		_, _ = w.Write(readResponse(t, "error-schema"))
	})
	_, err := s.client.Send(context.Background(), errorSale())
	assert.ErrorIs(t, err, ErrValidation)
	assert.EqualError(t, err, "validation: 3: XML zprava nevyhovela kontrole XML schematu (X-Global-Transaction-Id a1b2c3)")
	var e *Error
	require.ErrorAs(t, err, &e)
	assert.Equal(t, "3", e.Code())
}

// The server's certificate is verified: a server the system doesn't trust
// is refused (interface description, 5.1).
func TestSendVerifiesTheServer(t *testing.T) {
	s := newTestService(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(readResponse(t, "error-schema"))
	})
	s.client.httpClient.Transport.(*http.Transport).TLSClientConfig.RootCAs = x509.NewCertPool()
	_, err := s.client.Send(context.Background(), errorSale())
	assert.ErrorIs(t, err, ErrConnection)
	assert.ErrorContains(t, err, "certificate signed by unknown authority")
	assert.Zero(t, s.calls.Load())
}

func TestSendRequestSize(t *testing.T) {
	assert.NoError(t, checkRequestSize(MaxRequestSize))
	err := checkRequestSize(MaxRequestSize + 1)
	assert.ErrorIs(t, err, ErrValidation)
	assert.EqualError(t, err, "validation: request too large: 12001 bytes, the limit is 12000")

	t.Run("not sent", func(t *testing.T) {
		s := newTestService(t, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write(readResponse(t, "error-schema"))
		})
		sale := errorSale()
		sale.Data.Number = strings.Repeat("9", MaxRequestSize)
		_, err := s.client.Send(context.Background(), sale)
		assert.ErrorIs(t, err, ErrValidation)
		assert.ErrorContains(t, err, "request too large")
		assert.Zero(t, s.calls.Load())
	})
	t.Run("refused by Sign", func(t *testing.T) {
		c, err := New(WithCertificate(newTestXMLCertificate(t)))
		require.NoError(t, err)
		sale := errorSale()
		sale.Data.Number = strings.Repeat("9", MaxRequestSize)
		_, err = c.Sign(sale)
		assert.ErrorContains(t, err, "validation: request too large")
	})
}

func TestSendTimeout(t *testing.T) {
	release := make(chan struct{})
	s := newTestService(t, func(w http.ResponseWriter, _ *http.Request) {
		<-release
		_, _ = w.Write(readResponse(t, "error-schema"))
	}, WithTimeout(MinTimeout))
	defer close(release)

	start := time.Now()
	_, err := s.client.Send(context.Background(), errorSale())
	assert.ErrorIs(t, err, ErrConnection)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.WithinDuration(t, start.Add(MinTimeout), time.Now(), time.Second)
}

// A SOAP envelope is read as the reply whatever the HTTP status, as SOAP 1.1
// (section 6.2) sends faults with 500. The Playground answers its errors
// with 200, but the interface description doesn't say which status they
// come with.
func TestSendEnvelopeWithAnyStatus(t *testing.T) {
	t.Run("error with 500", func(t *testing.T) {
		s := newTestService(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write(readResponse(t, "error-schema"))
		})
		_, err := s.client.Send(context.Background(), errorSale())
		assert.ErrorIs(t, err, ErrValidation)
		assert.ErrorContains(t, err, "validation: 3: XML zprava nevyhovela")
	})
	t.Run("fault with 500", func(t *testing.T) {
		s := newTestService(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("X-Global-Transaction-Id", "j1k2l3")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`<soapenv:Envelope xmlns:soapenv="http://schemas.xmlsoap.org/soap/envelope/"><soapenv:Body><soapenv:Fault>` +
				`<faultcode>soapenv:Client</faultcode><faultstring>Bad request</faultstring></soapenv:Fault></soapenv:Body></soapenv:Envelope>`))
		})
		_, err := s.client.Send(context.Background(), errorSale())
		assert.ErrorIs(t, err, ErrValidation)
		assert.EqualError(t, err, "validation: soapenv:Client: Bad request (X-Global-Transaction-Id j1k2l3)")
	})
	t.Run("server fault with 500", func(t *testing.T) {
		s := newTestService(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`<soapenv:Envelope xmlns:soapenv="http://schemas.xmlsoap.org/soap/envelope/"><soapenv:Body><soapenv:Fault>` +
				`<faultcode>soapenv:Server</faultcode><faultstring>Internal Error</faultstring></soapenv:Fault></soapenv:Body></soapenv:Envelope>`))
		})
		_, err := s.client.Send(context.Background(), errorSale())
		assert.ErrorIs(t, err, ErrServer)
	})
}

// A status other than 2xx with a body that isn't a SOAP envelope is no
// answer, 4xx included: the service reports problems with a message with an
// error code or a fault, so the sale is sent again.
func TestSendHTTPStatus(t *testing.T) {
	tests := []struct {
		status int
		body   string
		err    error
	}{
		{http.StatusServiceUnavailable, "<html><body>Service Unavailable</body></html>", ErrServer},
		{http.StatusInternalServerError, "", ErrServer},
		{http.StatusBadGateway, strings.Repeat(" ", maxResponseSize+1), ErrServer},
		{http.StatusRequestTimeout, "", ErrServer},
		{http.StatusTooManyRequests, "<html><body>Too Many Requests</body></html>", ErrServer},
		// The Playground's firewall answers a request that isn't well-formed
		// XML with this page.
		{http.StatusBadRequest, "<!DOCTYPE html>\n<html lang=\"cs\"><body><h2>Váš požadavek byl z bezpečnostních důvodů zablokován.</h2></body></html>", ErrServer},
		{http.StatusNotFound, "", ErrServer},
		{http.StatusForbidden, "<html><body>Forbidden</body></html>", ErrServer},
	}
	for _, tt := range tests {
		t.Run(http.StatusText(tt.status), func(t *testing.T) {
			s := newTestService(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("X-Global-Transaction-Id", "d4e5f6")
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			})
			_, err := s.client.Send(context.Background(), errorSale())
			assert.ErrorIs(t, err, tt.err)
			var e *Error
			require.ErrorAs(t, err, &e)
			assert.Equal(t, strconv.Itoa(tt.status), e.Code())
			assert.Equal(t, http.StatusText(tt.status)+" (X-Global-Transaction-Id d4e5f6)", e.Message())
		})
	}
}

func TestSendUnreadableReplies(t *testing.T) {
	tests := map[string]struct {
		body string
		err  string
	}{
		"empty":     {body: "", err: "reading message:"},
		"HTML page": {body: "<html><body>Service Unavailable</body></html>", err: "response is not a SOAP envelope"},
		"too large": {body: strings.Repeat(" ", maxResponseSize+1), err: "reply over 1048576 bytes"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			s := newTestService(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("X-Global-Transaction-Id", "g7h8i9")
				_, _ = w.Write([]byte(tt.body))
			})
			_, err := s.client.Send(context.Background(), errorSale())
			assert.ErrorContains(t, err, tt.err)
			assert.ErrorContains(t, err, "X-Global-Transaction-Id g7h8i9")
			assert.ErrorIs(t, err, ErrConnection)
		})
	}
}

func TestNew(t *testing.T) {
	c, err := New()
	require.NoError(t, err)
	assert.Equal(t, EnvironmentPlayground, c.Environment(), "the Playground by default")
	assert.Equal(t, DefaultTimeout, c.timeout)

	c, err = New(InProduction())
	require.NoError(t, err)
	assert.Equal(t, EnvironmentProduction, c.Environment())

	_, err = New(WithTimeout(MinTimeout - time.Millisecond))
	assert.EqualError(t, err, "validation: timeout 1.999s is under the 2s the EET Act asks for")
	_, err = New(WithTimeout(MinTimeout))
	assert.NoError(t, err)

	now := time.Date(2027, 1, 9, 16, 45, 37, 0, time.UTC)
	c, err = New(WithCurrentTime(now))
	require.NoError(t, err)
	assert.Equal(t, now, c.CurrentTime())

	_, err = c.Send(context.Background(), errorSale())
	assert.EqualError(t, err, "validation: a certificate is needed to sign a sale")
}

// A payment whose envelope has a POK was recorded: it is not sent again,
// but it can still be checked in verification mode, which records nothing.
func TestConfirmedPaymentNotSentAgain(t *testing.T) {
	s := newTestService(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(readResponse(t, "verification"))
	})
	env := loadExampleEnvelope(t, "receipt")
	stampPOK(env, "3d3a5319-e700-4a9c-a7bb-986a62bcbf3a-ff")

	t.Run("NewSale", func(t *testing.T) {
		_, err := s.client.NewSale(env, WithResend())
		assert.ErrorIs(t, err, ErrDuplicate)
		assert.EqualError(t, err, "duplicate: already has an eet-pok stamp")
	})
	t.Run("verification", func(t *testing.T) {
		sale, err := s.client.NewSale(env, WithVerification())
		require.NoError(t, err)
		sale.Header.UUID = verificationUUID
		res, err := s.client.Send(context.Background(), sale)
		require.NoError(t, err)
		assert.Equal(t, ErrorCodeVerified, res.Error.Code)
		assert.Equal(t, "3d3a5319-e700-4a9c-a7bb-986a62bcbf3a-ff", env.Head.Stamp(addon.StampPOK).Value)
	})
}

func TestNewSaleFromEnvelope(t *testing.T) {
	now := time.Date(2027, 1, 9, 15, 45, 37, 0, time.UTC)
	c, err := New(WithCurrentTime(now))
	require.NoError(t, err)

	sale, err := c.NewSale(loadExampleEnvelope(t, "receipt"), WithResend())
	require.NoError(t, err)
	assert.Empty(t, sale.Header.SentAt, "the send time is set by Sign")
	assert.False(t, sale.Header.FirstAttempt)

	_, err = c.NewSale(nil)
	assert.ErrorIs(t, err, ErrValidation)
	inv, err := gobl.Envelop(&note.Message{Content: "not a payment"})
	require.NoError(t, err)
	_, err = c.NewSale(inv)
	assert.ErrorIs(t, err, ErrOnlyPayments)
}

// The default transport speaks HTTP/1.1 only, over TLS 1.2 or higher.
func TestDefaultTransport(t *testing.T) {
	transport := newTransport()
	assert.False(t, transport.ForceAttemptHTTP2)
	assert.True(t, transport.Protocols.HTTP1())
	assert.False(t, transport.Protocols.HTTP2())
	assert.False(t, transport.TLSClientConfig.InsecureSkipVerify)
	assert.EqualValues(t, 0x0303, transport.TLSClientConfig.MinVersion)
}

// Send posts to the environment's own URL. The test dialer reaches the test
// server whatever the host, so the host and path are checked as sent.
func TestSendToEnvironmentURL(t *testing.T) {
	tests := map[string]struct {
		opt  Option
		host string
	}{
		"Playground": {InPlayground(), "pg.trzbyeet.gov.cz"},
		"production": {InProduction(), "trzbyeet.gov.cz"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var host, path string
			s := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
				host, path = r.Host, r.URL.Path
				_, _ = w.Write(readResponse(t, "error-schema"))
			}, tt.opt)
			_, _ = s.client.Send(context.Background(), errorSale())
			assert.Equal(t, tt.host, host)
			assert.Equal(t, "/eet/services/EETServiceSOAP/v4", path)
		})
	}
}

// A reply cut short is no answer.
func TestSendReplyCutShort(t *testing.T) {
	s := newTestService(t, func(w http.ResponseWriter, _ *http.Request) {
		conn, buf, err := http.NewResponseController(w).Hijack()
		require.NoError(t, err)
		defer conn.Close() //nolint:errcheck
		_, _ = buf.WriteString("HTTP/1.1 200 OK\r\nX-Global-Transaction-Id: p7q8r9\r\nContent-Length: 1000\r\n\r\n<soapenv:Envelope")
		_ = buf.Flush()
	})
	res, err := s.client.Send(context.Background(), errorSale())
	assert.Nil(t, res)
	assert.ErrorIs(t, err, ErrConnection)
	assert.ErrorIs(t, err, io.ErrUnexpectedEOF)
	assert.ErrorContains(t, err, "X-Global-Transaction-Id p7q8r9")
}

// The transport's own errors stay reachable through the cause.
func TestSendTransportErrorCause(t *testing.T) {
	s := newTestService(t, func(http.ResponseWriter, *http.Request) {})
	s.client.httpClient.Transport.(*http.Transport).TLSClientConfig.RootCAs = x509.NewCertPool()
	_, err := s.client.Send(context.Background(), errorSale())
	var urlErr *url.Error
	assert.ErrorAs(t, err, &urlErr)
	var verifyErr *tls.CertificateVerificationError
	assert.ErrorAs(t, err, &verifyErr)
}

func TestNewWithMismatchedCertificate(t *testing.T) {
	key, _ := newTestCertificate(t)
	_, cert := newTestCertificate(t)
	_, err := New(WithCertificate(loadTestCertificate(t, pkcs12.LegacyRC2, key, cert)))
	assert.ErrorIs(t, err, ErrValidation)
}

// A sale built by hand, with no envelope, gets its confirmation back.
func TestSendHandBuiltSaleConfirmed(t *testing.T) {
	s := newTestService(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(readResponse(t, "confirmation"))
	})
	s.client.trust = playgroundTrust()
	sale := errorSale()
	sale.Header.UUID = confirmationUUID
	res, err := s.client.Send(context.Background(), sale)
	require.NoError(t, err)
	assert.NotEmpty(t, res.Confirmation.POK)
}

// A message in verification mode is never recorded, so a confirmation for
// one is refused.
func TestSendVerificationWithConfirmation(t *testing.T) {
	s := newTestService(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(readResponse(t, "confirmation"))
	})
	s.client.trust = playgroundTrust()
	env := loadExampleEnvelope(t, "receipt")
	sale, err := s.client.NewSale(env, WithVerification())
	require.NoError(t, err)
	sale.Header.UUID = confirmationUUID
	_, err = s.client.Send(context.Background(), sale)
	assert.ErrorIs(t, err, ErrConnection)
	assert.ErrorContains(t, err, "confirmation answers a message in verification mode")
}

func TestSendResponseEdges(t *testing.T) {
	t.Run("body of exactly maxResponseSize", func(t *testing.T) {
		s := newTestService(t, func(w http.ResponseWriter, _ *http.Request) {
			reply := readResponse(t, "error-schema")
			_, _ = w.Write(append(reply, bytes.Repeat([]byte(" "), maxResponseSize-len(reply))...))
		})
		_, err := s.client.Send(context.Background(), errorSale())
		assert.ErrorIs(t, err, ErrValidation.WithCode("3"))
	})
	t.Run("2xx other than 200", func(t *testing.T) {
		s := newTestService(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write(readResponse(t, "error-schema"))
		})
		_, err := s.client.Send(context.Background(), errorSale())
		assert.ErrorIs(t, err, ErrValidation.WithCode("3"))
	})
	t.Run("204 with no body", func(t *testing.T) {
		s := newTestService(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})
		_, err := s.client.Send(context.Background(), errorSale())
		assert.ErrorIs(t, err, ErrConnection)
	})
	t.Run("an Envelope outside the SOAP namespace with 503", func(t *testing.T) {
		s := newTestService(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("<Envelope/>"))
		})
		_, err := s.client.Send(context.Background(), errorSale())
		assert.ErrorIs(t, err, ErrServer.WithCode("503"))
	})
}

// Without the header, the error message names no transaction ID.
func TestSendWithoutTransactionID(t *testing.T) {
	for status, msg := range map[int]string{http.StatusForbidden: "server: 403: Forbidden", 599: "server: 599"} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			s := newTestService(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
			})
			_, err := s.client.Send(context.Background(), errorSale())
			assert.EqualError(t, err, msg)
		})
	}
}

// A redirect is not followed, so the signed request goes nowhere else: it
// is no reply, and ErrServer with its status.
func TestSendDoesNotFollowRedirects(t *testing.T) {
	var elsewhere atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		elsewhere.Add(1)
	}))
	t.Cleanup(target.Close)
	tests := map[int]string{
		http.StatusTemporaryRedirect: "https://elsewhere.example/eet/services/EETServiceSOAP/v4",
		http.StatusPermanentRedirect: target.URL + "/eet/services/EETServiceSOAP/v4",
	}
	for status, location := range tests {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			s := newTestService(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Location", location)
				w.WriteHeader(status)
			})
			_, err := s.client.Send(context.Background(), errorSale())
			assert.ErrorIs(t, err, ErrServer.WithCode(strconv.Itoa(status)))
			assert.EqualValues(t, 1, s.calls.Load())
			assert.Zero(t, elsewhere.Load())
		})
	}
}

// A Playground POK ends in "-ff" and isn't valid under the EET Act
// (interface description, 3.4.2.3): it only blocks a resend in the
// Playground, while a production POK blocks it in both environments.
func TestConfirmedDependsOnEnvironment(t *testing.T) {
	const (
		playgroundPOK = "3d3a5319-e700-4a9c-a7bb-986a62bcbf3a-ff"
		productionPOK = "b3a09b52-7c87-4014-a496-4c7a53cf9125-03"
	)
	stamped := func(t *testing.T, pok string) *gobl.Envelope {
		env := loadExampleEnvelope(t, "receipt")
		stampPOK(env, pok)
		return env
	}
	for _, tt := range []struct {
		name    string
		opt     Option
		pok     string
		blocked bool
	}{
		{"Playground POK in production", InProduction(), playgroundPOK, false},
		{"Playground POK in the Playground", InPlayground(), playgroundPOK, true},
		{"production POK in production", InProduction(), productionPOK, true},
		{"production POK in the Playground", InPlayground(), productionPOK, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c, err := New(tt.opt)
			require.NoError(t, err)
			_, err = c.NewSale(stamped(t, tt.pok))
			if tt.blocked {
				assert.ErrorIs(t, err, ErrDuplicate)
			} else {
				assert.NoError(t, err)
			}
		})
	}

	t.Run("two POK stamps", func(t *testing.T) {
		c, err := New(InProduction())
		require.NoError(t, err)
		env := loadExampleEnvelope(t, "receipt")
		env.Head.Stamps = append(env.Head.Stamps,
			&head.Stamp{Provider: addon.StampPOK, Value: playgroundPOK},
			&head.Stamp{Provider: addon.StampPOK, Value: productionPOK})
		_, err = c.NewSale(env)
		assert.EqualError(t, err, "validation: envelope has more than one eet-pok stamp")
	})

}

// Sign sets the send time (interface description, 3.3.3.2) and signs a sale
// only once, as every attempt is a new message (3.3.3.1).
func TestSignOnce(t *testing.T) {
	now := time.Date(2027, 1, 9, 15, 45, 37, 0, time.UTC)
	c, err := New(WithCertificate(newTestXMLCertificate(t)), WithCurrentTime(now))
	require.NoError(t, err)
	sale, err := c.NewSale(loadExampleEnvelope(t, "receipt"))
	require.NoError(t, err)

	_, err = c.Sign(sale)
	require.NoError(t, err)
	assert.Equal(t, "2027-01-09T16:45:37+01:00", sale.Header.SentAt)
	_, err = c.Sign(sale)
	assert.EqualError(t, err, "validation: sale already signed: build a new sale for each attempt")

	handBuilt := errorSale()
	_, err = c.Sign(handBuilt)
	require.NoError(t, err)
	_, err = c.Sign(handBuilt)
	assert.ErrorIs(t, err, ErrValidation)
}

// Sending the same sale twice would repeat its UUID, so the second is
// refused before anything is posted.
func TestSendSameSaleTwice(t *testing.T) {
	s := newTestService(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(readResponse(t, "error-schema"))
	})
	sale := errorSale()
	_, _ = s.client.Send(context.Background(), sale)
	_, err := s.client.Send(context.Background(), sale)
	assert.EqualError(t, err, "validation: sale already signed: build a new sale for each attempt")
	assert.EqualValues(t, 1, s.calls.Load())
}

// An empty eet-pok stamp is refused, not read as a POK.
func TestEmptyPOKStamp(t *testing.T) {
	c, err := New()
	require.NoError(t, err)
	env := loadExampleEnvelope(t, "receipt")
	env.Head.AddStamp(&head.Stamp{Provider: addon.StampPOK, Value: ""})
	_, err = c.NewSale(env)
	assert.EqualError(t, err, "validation: eet-pok stamp is empty")
	_, err = c.NewSale(env, WithVerification())
	assert.EqualError(t, err, "validation: eet-pok stamp is empty")
}

// stampPOK keeps a POK on the envelope, as the app does after a confirmation.
func stampPOK(env *gobl.Envelope, pok string) {
	env.Head.AddStamp(&head.Stamp{Provider: addon.StampPOK, Value: pok})
}

// Send, and readReply as ParseResponse uses it, return the POK and leave the
// caller's envelope as it was.
func TestEnvelopeLeftAlone(t *testing.T) {
	s := newTestService(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(readResponse(t, "confirmation"))
	})
	s.client.trust = playgroundTrust()
	env := loadExampleEnvelope(t, "receipt")
	before, err := json.Marshal(env)
	require.NoError(t, err)

	sale, err := s.client.NewSale(env)
	require.NoError(t, err)
	sale.Header.UUID = confirmationUUID
	res, err := s.client.Send(context.Background(), sale)
	require.NoError(t, err)
	assert.NotEmpty(t, res.Confirmation.POK)

	sale, err = s.client.NewSale(env)
	require.NoError(t, err)
	sale.Header.UUID = confirmationUUID
	res, err = readReply(readResponse(t, "confirmation"), sale, playgroundTrust())
	require.NoError(t, err)
	assert.NotEmpty(t, res.Confirmation.POK)

	after, err := json.Marshal(env)
	require.NoError(t, err)
	assert.Equal(t, string(before), string(after), "the envelope is byte for byte the same")
}

// Of several Signs racing on the same sale, exactly one signs it.
func TestSignOnceConcurrently(t *testing.T) {
	c, err := New(WithCertificate(newTestXMLCertificate(t)))
	require.NoError(t, err)
	sale, err := c.NewSale(loadExampleEnvelope(t, "receipt"))
	require.NoError(t, err)

	const n = 16
	var signed, refused atomic.Int32
	var wg sync.WaitGroup
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.Sign(sale); err == nil {
				signed.Add(1)
			} else if errors.Is(err, ErrValidation) {
				refused.Add(1)
			}
		}()
	}
	wg.Wait()
	assert.EqualValues(t, 1, signed.Load())
	assert.EqualValues(t, n-1, refused.Load())
}

// An app that replaced the default transport, with a tracing one, say, still
// gets a client that speaks HTTP/1.1.
func TestNewWithReplacedDefaultTransport(t *testing.T) {
	original := http.DefaultTransport
	http.DefaultTransport = afterRoundTrip{original, func() {}}
	t.Cleanup(func() { http.DefaultTransport = original })

	var c *Client
	var err error
	require.NotPanics(t, func() { c, err = New() })
	require.NoError(t, err)
	transport, ok := c.httpClient.Transport.(*http.Transport)
	require.True(t, ok)
	assert.False(t, transport.Protocols.HTTP2())
	assert.EqualValues(t, tls.VersionTLS12, transport.TLSClientConfig.MinVersion)
}

// afterRoundTrip runs a function once the request has had its response.
type afterRoundTrip struct {
	base http.RoundTripper
	then func()
}

func (a afterRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) {
	res, err := a.base.RoundTrip(r)
	a.then()
	return res, err
}
