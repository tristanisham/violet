// Package client provides remote implementations of protocol.Client.
package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/tristanisham/violet/protocol"
)

const (
	submitTimeout = 15 * time.Second
	headerTimeout = 15 * time.Second
	eventBuffer   = 64
	// Event JSON can expand up to 6x when the server HTML-escapes RawMessage data.
	maxSSETokenBytes   = 64 << 20
	sseIdleTimeout     = 45 * time.Second
	maxErrorBodyBytes  = 4 << 10
	maxDrainBodyBytes  = 64 << 10
	clientIDHeader     = "X-Violet-Client-ID"
	eventStreamMIME    = "text/event-stream"
	messagesPath       = "/api/messages"
	eventsPath         = "/api/events"
	authorizationValue = "Bearer "
)

type httpClient struct {
	base   string
	token  string
	id     string
	http   *http.Client
	ctx    context.Context // cancelled by Close
	cancel context.CancelFunc

	mu       sync.Mutex
	closed   bool
	streamOn bool
	streamWG sync.WaitGroup
}

var _ protocol.Client = (*httpClient)(nil)

// NewHTTP returns a protocol.Client that talks to a Violet server over HTTP.
// baseURL must be https, or http on a loopback host. The token, when non-empty,
// is sent as a bearer credential and is never included in errors.
func NewHTTP(baseURL, token string) (protocol.Client, error) {
	base, err := validateBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = headerTimeout
	ctx, cancel := context.WithCancel(context.Background())
	return &httpClient{
		base: base, token: token, id: uuid.NewString(), ctx: ctx, cancel: cancel,
		http: &http.Client{
			Transport: transport,
			// Never follow redirects: they could forward credentials elsewhere.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}, nil
}

func validateBaseURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", errors.New("invalid server URL")
	}
	if u.Opaque != "" || u.Host == "" || u.Hostname() == "" {
		return "", errors.New("server URL must be absolute with a host")
	}
	if u.User != nil {
		return "", errors.New("server URL must not contain credentials")
	}
	if u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(raw, "#") {
		return "", errors.New("server URL must not contain a query or fragment")
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
	case "http":
		if !isLoopbackHost(u.Hostname()) {
			return "", errors.New("plain http is only allowed for loopback hosts; use https")
		}
	default:
		return "", errors.New("server URL scheme must be http or https")
	}
	path := strings.TrimRight(u.EscapedPath(), "/")
	return strings.ToLower(u.Scheme) + "://" + u.Host + path, nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// bind derives a context that is also cancelled when the client is closed.
func (c *httpClient) bind(ctx context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(c.ctx, cancel)
	return ctx, func() { stop(); cancel() }
}

func (c *httpClient) newRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return nil, errors.New("build server request")
	}
	req.Header.Set(clientIDHeader, c.id)
	if c.token != "" {
		req.Header.Set("Authorization", authorizationValue+c.token)
	}
	return req, nil
}

func (c *httpClient) isClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

func (c *httpClient) Submit(ctx context.Context, request protocol.Request) error {
	if c.isClosed() {
		return protocol.ErrDisconnected
	}
	body, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("encode request: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, submitTimeout)
	defer cancel()
	ctx, unbind := c.bind(ctx)
	defer unbind()
	req, err := c.newRequest(ctx, http.MethodPost, messagesPath, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		if c.isClosed() {
			return protocol.ErrDisconnected
		}
		return transportError("submit", err)
	}
	defer closeBody(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return statusError("submit", resp)
	}
	return nil
}

func (c *httpClient) Subscribe(ctx context.Context) (<-chan protocol.Event, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, protocol.ErrDisconnected
	}
	if c.streamOn {
		c.mu.Unlock()
		return nil, errors.New("client already subscribed")
	}
	c.streamOn = true
	c.streamWG.Add(1)
	c.mu.Unlock()
	// streamOn clears before the channel closes so a consumer can resubscribe as
	// soon as it observes the close; streamWG completes only after the close.
	clearStream := func() {
		c.mu.Lock()
		c.streamOn = false
		c.mu.Unlock()
	}
	release := func() {
		clearStream()
		c.streamWG.Done()
	}

	streamCtx, cancel := c.bind(ctx)
	req, err := c.newRequest(streamCtx, http.MethodGet, eventsPath, nil)
	if err != nil {
		cancel()
		release()
		return nil, err
	}
	req.Header.Set("Accept", eventStreamMIME)
	resp, err := c.http.Do(req)
	if err != nil {
		cancel()
		release()
		if c.isClosed() {
			return nil, protocol.ErrDisconnected
		}
		return nil, transportError("subscribe", err)
	}
	if resp.StatusCode != http.StatusOK {
		err := statusError("subscribe", resp)
		closeBody(resp.Body)
		cancel()
		release()
		return nil, err
	}
	if mediaType := strings.TrimSpace(strings.Split(resp.Header.Get("Content-Type"), ";")[0]); !strings.EqualFold(mediaType, eventStreamMIME) {
		closeBody(resp.Body)
		cancel()
		release()
		return nil, errors.New("subscribe: server did not return an event stream")
	}

	events := make(chan protocol.Event, eventBuffer)
	go func() {
		defer c.streamWG.Done()
		// The server sends keepalives; silence beyond sseIdleTimeout means a dead
		// connection, so cancel the stream instead of blocking forever.
		idle := time.AfterFunc(sseIdleTimeout, cancel)
		readEvents(streamCtx, resp.Body, events, func() { idle.Reset(sseIdleTimeout) })
		idle.Stop()
		cancel()
		resp.Body.Close()
		clearStream()
		close(events)
	}()
	return events, nil
}

// readEvents parses an SSE stream until it ends, is malformed, or ctx is done.
func readEvents(ctx context.Context, body io.Reader, events chan<- protocol.Event, alive func()) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64<<10), maxSSETokenBytes)
	var data []string
	for scanner.Scan() {
		alive()
		line := strings.TrimSuffix(scanner.Text(), "\r")
		switch {
		case line == "":
			if len(data) == 0 {
				continue
			}
			var event protocol.Event
			err := json.Unmarshal([]byte(strings.Join(data, "\n")), &event)
			data = data[:0]
			if err != nil {
				// A malformed frame ends the stream rather than silently dropping events.
				return
			}
			select {
			case events <- event:
			case <-ctx.Done():
				return
			}
		case strings.HasPrefix(line, ":"):
			// Comment, e.g. keepalive.
		default:
			field, value, _ := strings.Cut(line, ":")
			if field == "data" {
				data = append(data, strings.TrimPrefix(value, " "))
			}
			// Other fields (event, id, retry) are not used by Violet.
		}
	}
}

// Close is idempotent. It cancels any active stream, waits for its channel to
// close, and makes later Submit/Subscribe calls return protocol.ErrDisconnected.
func (c *httpClient) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.mu.Unlock()
	c.cancel()
	c.streamWG.Wait()
	c.http.CloseIdleConnections()
	return nil
}

func closeBody(body io.ReadCloser) {
	_, _ = io.Copy(io.Discard, io.LimitReader(body, maxDrainBodyBytes))
	_ = body.Close()
}

// transportError avoids echoing request details; url.Error includes the URL,
// which never carries the token, but we unwrap to keep messages short.
func transportError(op string, err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		err = urlErr.Err
	}
	return fmt.Errorf("%s: %w", op, err)
}

func statusError(op string, resp *http.Response) error {
	text, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
	// Strip control characters so a hostile server cannot inject terminal escapes.
	message := strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, strings.ToValidUTF8(string(text), "")))
	var sentinel error
	switch resp.StatusCode {
	case http.StatusTooManyRequests:
		sentinel = protocol.ErrBusy
	case http.StatusServiceUnavailable:
		sentinel = protocol.ErrStopped
	case http.StatusConflict:
		sentinel = protocol.ErrDisconnected
	}
	if sentinel != nil {
		return fmt.Errorf("%s: %w (HTTP %d)", op, sentinel, resp.StatusCode)
	}
	if message == "" {
		return fmt.Errorf("%s: HTTP %d", op, resp.StatusCode)
	}
	return fmt.Errorf("%s: HTTP %d: %s", op, resp.StatusCode, message)
}
