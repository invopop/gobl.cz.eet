package eet

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/invopop/gobl"
	"github.com/invopop/gobl.cz.eet/addon"
	"github.com/invopop/gobl/bill"
	"github.com/invopop/gobl/uuid"
	"github.com/invopop/xmldsig"
)

// MaxRequestSize is the largest signed request the service accepts, the
// smaller reading of the "12 kB" of error 7 (interface description, 3.5.4).
const MaxRequestSize = 12000

// maxResponseSize caps how much of a reply is read; replies are a few kB.
const maxResponseSize = 1 << 20

// MinTimeout is the shortest wait for a reply the EET Act allows (§ 19; item
// description, chapter 6), and DefaultTimeout the one used when none is set.
const (
	MinTimeout     = 2 * time.Second
	DefaultTimeout = 10 * time.Second
)

// SOAPAction is the action of the service's one operation, OdeslaniTrzby,
// which SOAP 1.1 sends in quotes in the SOAPAction header.
const SOAPAction = "http://fs.gov.cz/eet/OdeslaniTrzby"

// transactionIDHeader tags each exchange for tracing it (interface
// description, 5.4).
const transactionIDHeader = "X-Global-Transaction-Id"

// Client builds sale messages from GOBL payments and sends them to one
// environment of the EET service.
type Client struct {
	env        Environment
	cert       *xmldsig.Certificate
	signer     *signer
	curTime    time.Time
	timeout    time.Duration
	httpClient *http.Client
	conn       *resty.Client

	// trust replaces the environment's checks on the replies, for tests.
	trust *trust
}

// Option is used to configure the client.
type Option func(*Client)

// WithCertificate sets the merchant's CA EET certificate, whose tax ID is
// its CN, to sign the sales with.
func WithCertificate(cert *xmldsig.Certificate) Option {
	return func(c *Client) {
		c.cert = cert
	}
}

// WithCurrentTime fixes the send time of the sale messages.
func WithCurrentTime(curTime time.Time) Option {
	return func(c *Client) {
		c.curTime = curTime
	}
}

// InProduction sends to the production environment, where sales are
// recorded.
func InProduction() Option {
	return func(c *Client) {
		c.env = EnvironmentProduction
	}
}

// InPlayground sends to the Playground, the EET service's public test
// environment.
func InPlayground() Option {
	return func(c *Client) {
		c.env = EnvironmentPlayground
	}
}

// WithTimeout sets how long to wait for a reply, at least MinTimeout.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) {
		c.timeout = d
	}
}

// New creates a client for the Playground, or production with InProduction,
// which without a certificate builds sales but doesn't send them.
func New(opts ...Option) (*Client, error) {
	c := &Client{env: EnvironmentPlayground, timeout: DefaultTimeout}
	for _, opt := range opts {
		if opt != nil {
			opt(c)
		}
	}
	if c.timeout < MinTimeout {
		return nil, ErrValidation.WithMessage(fmt.Sprintf("timeout %s is under the %s the EET Act asks for", c.timeout, MinTimeout))
	}
	if c.cert != nil {
		var err error
		if c.signer, err = signerFor(c.cert); err != nil {
			return nil, err
		}
	}
	c.httpClient = &http.Client{Transport: newTransport(), CheckRedirect: noRedirects}
	c.conn = resty.NewWithClient(c.httpClient).
		SetDoNotParseResponse(true).
		SetDebug(os.Getenv("DEBUG") == "true")
	return c, nil
}

// newTransport speaks HTTP/1.1 only (interface description, 2.3.2) over TLS
// 1.2 or higher (2.3.1), verifying the server's certificate (5.1).
func newTransport() *http.Transport {
	t, ok := http.DefaultTransport.(*http.Transport)
	if ok {
		t = t.Clone()
	} else {
		// The app replaced the default transport, with a tracing one, say.
		t = &http.Transport{Proxy: http.ProxyFromEnvironment}
	}
	t.ForceAttemptHTTP2 = false
	t.Protocols = new(http.Protocols)
	t.Protocols.SetHTTP1(true)
	t.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	return t
}

// CurrentTime returns the send time for the sale messages.
func (c *Client) CurrentTime() time.Time {
	if c != nil && !c.curTime.IsZero() {
		return c.curTime
	}
	return time.Now()
}

// Environment returns the environment the client sends to.
func (c *Client) Environment() Environment {
	if c == nil {
		return ""
	}
	return c.env
}

// NewSale builds the message for the calculated payment in the envelope, one
// for every attempt, refusing it with the rules' faults, or with
// ErrDuplicate for an envelope whose POK records the sale in the client's
// environment (any in the Playground, a production one in production)
// unless WithVerification is given.
func (c *Client) NewSale(env *gobl.Envelope, opts ...GenerateOption) (*Sale, error) {
	if env == nil {
		return nil, ErrValidation.WithMessage("no envelope")
	}
	pmt, ok := env.Extract().(*bill.Payment)
	if !ok {
		return nil, ErrOnlyPayments
	}
	o := c.generateOptions(opts)
	if err := c.checkStamp(env, o.verification); err != nil {
		return nil, err
	}
	return newSale(pmt, o)
}

func (c *Client) generateOptions(opts []GenerateOption) generateOptions {
	o := generateOptions{uuid: uuid.V4().String()}
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}
	return o
}

// checkStamp refuses an empty eet-pok stamp, and one that records the sale
// for the client's environment unless in verification mode: a POK ending in
// "-ff" is the Playground's and not valid under the EET Act (interface
// description, 3.4.2.3), so it only counts in the Playground, while a
// production POK counts in both.
func (c *Client) checkStamp(env *gobl.Envelope, verification bool) error {
	if env.Head == nil {
		return nil
	}
	stamp := env.Head.GetStamp(addon.StampPOK)
	switch {
	case stamp == nil:
		return nil
	case stamp.Value == "":
		return ErrValidation.WithMessage("eet-pok stamp is empty")
	case verification:
		return nil
	}
	playground := c != nil && c.env == EnvironmentPlayground
	if playground || !strings.HasSuffix(strings.ToLower(stamp.Value), "-ff") {
		return ErrDuplicate.WithMessage("already has an eet-pok stamp")
	}
	return nil
}

// Sign returns the SOAP request carrying the sale, signed with the client's
// certificate as the interface description asks (5.2), with the send time
// (dat_odesl, 3.3.3.2) set now; a sale is used once (3.3.3.1), so Sign
// followed by Send fails, and an app that posts the request itself uses Sign
// and ParseResponse while any other uses only Send.
func (c *Client) Sign(sale *Sale) ([]byte, error) {
	if c == nil || c.signer == nil {
		return nil, ErrValidation.WithMessage("a certificate is needed to sign a sale")
	}
	if sale == nil || sale.Header == nil || sale.Data == nil {
		return nil, ErrValidation.WithMessage("sale needs a header and data")
	}
	// Only the first Sign claims the sale, and a failed signing still uses
	// it up, so a new sale is built.
	if !sale.signed.CompareAndSwap(false, true) {
		return nil, ErrValidation.WithMessage("sale already signed: build a new sale for each attempt")
	}
	sale.Header.SentAt = sentAt(c.CurrentTime())
	request, err := c.signer.sign(sale)
	if err != nil {
		return nil, err
	}
	if err := checkRequestSize(len(request)); err != nil {
		return nil, err
	}
	return request, nil
}

// Send signs and posts the sale, which can't have been signed before, and
// reads the reply as ParseResponse does; a context deadline under MinTimeout
// cuts the wait short, and errors about a reply carry its transaction ID, when
// the service sent one.
func (c *Client) Send(ctx context.Context, sale *Sale) (*Response, error) {
	if c == nil || c.conn == nil {
		return nil, ErrValidation.WithMessage("client must be created with New")
	}
	if ctx == nil {
		return nil, ErrValidation.WithMessage("no context")
	}
	request, err := c.Sign(sale)
	if err != nil {
		return nil, asError(err, ErrValidation)
	}

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	res, err := c.conn.R().
		SetContext(ctx).
		SetHeader("Content-Type", "text/xml; charset=utf-8").
		SetHeader("SOAPAction", `"`+SOAPAction+`"`).
		SetBody(request).
		Post(c.env.URL())
	if err != nil {
		return nil, ErrConnection.withCause(err)
	}
	body := res.RawBody()
	defer body.Close() //nolint:errcheck
	transactionID := res.Header().Get(transactionIDHeader)
	data, err := io.ReadAll(io.LimitReader(body, maxResponseSize+1))
	if err != nil {
		return nil, withTransaction(ErrConnection.withCause(err), transactionID)
	}
	tooLarge := len(data) > maxResponseSize
	// SOAP 1.1 sends faults with 500 (section 6.2), so an envelope is read
	// whatever the status.
	if !res.IsSuccess() && (tooLarge || !isSOAPEnvelope(data)) {
		return nil, withTransaction(statusError(res.StatusCode()), transactionID)
	}
	if tooLarge {
		return nil, withTransaction(ErrConnection.WithMessage(fmt.Sprintf("reply over %d bytes", maxResponseSize)), transactionID)
	}
	tr := c.trust
	if tr == nil {
		tr = c.env.trust()
	}
	reply, err := readReply(data, sale, tr)
	if reply != nil {
		reply.TransactionID = transactionID
	}
	if err != nil {
		return reply, withTransaction(err, transactionID)
	}
	return reply, nil
}

// readReply reads the reply to the sale, and returns an error code other
// than 0 as an error alongside it.
func readReply(data []byte, sale *Sale, tr *trust) (*Response, error) {
	res, err := parseResponse(data, sale, tr)
	if err != nil {
		return nil, asError(err, ErrConnection)
	}
	return res, answerError(res)
}

// noRedirects leaves a redirect as the reply: the service's URLs are fixed
// ("Přístupové a provozní informace" v1.2, chapter 2.1), and following a 307
// or 308 would send the signed request on to another host.
func noRedirects(*http.Request, []*http.Request) error {
	return http.ErrUseLastResponse
}

func checkRequestSize(n int) error {
	if n > MaxRequestSize {
		return ErrValidation.WithMessage(fmt.Sprintf("request too large: %d bytes, the limit is %d", n, MaxRequestSize))
	}
	return nil
}

// statusError is the error for a status other than 2xx without a SOAP
// envelope: ErrServer, 4xx included, as the service reports a faulty message
// with an error code or a fault, never a bare status.
func statusError(status int) *Error {
	return ErrServer.WithCode(strconv.Itoa(status)).WithMessage(http.StatusText(status))
}

// withTransaction adds the X-Global-Transaction-Id to the error's message.
func withTransaction(err error, transactionID string) *Error {
	e := asError(err, ErrConnection)
	if transactionID == "" {
		return e
	}
	msg := transactionIDHeader + " " + transactionID
	if e.message != "" {
		msg = e.message + " (" + msg + ")"
	}
	return e.WithMessage(msg)
}

// isSOAPEnvelope says whether data is a SOAP envelope.
func isSOAPEnvelope(data []byte) bool {
	doc, err := parseXML(data)
	if err != nil {
		return false
	}
	root := doc.DocumentElement()
	return root != nil && root.LocalName() == "Envelope" && root.URI() == nsSOAP
}
