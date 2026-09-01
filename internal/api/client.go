// Package api talks to a server's deploy agent.
//
// The agent's API lives under a random path and is guarded by a bearer token;
// this package holds the one place either value is put on the wire, so there is
// exactly one place to audit for leaks. Tokens are never logged, never included
// in an error message, and never sent to a redirect target.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// Client is a connection to one server's deploy agent.
type Client struct {
	endpoint string
	token    string
	http     *http.Client
	agent    string
}

// New builds a client for an endpoint URL (including its API path).
func New(endpoint, token, userAgent string) (*Client, error) {
	u, err := url.Parse(strings.TrimRight(endpoint, "/"))
	if err != nil {
		return nil, fmt.Errorf("endpoint is not a URL: %w", err)
	}
	return &Client{
		endpoint: u.String(),
		token:    token,
		agent:    userAgent,
		http: &http.Client{
			// Uploads can legitimately take minutes; the per-request context
			// carries the real deadline.
			Timeout: 0,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				// A redirect could point anywhere, and the Authorization header
				// would follow it. Refuse instead.
				return errors.New("the server redirected the request; check the endpoint URL")
			},
		},
	}, nil
}

// Error is a failure reported by the agent, with its HTTP status.
type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string { return e.Message }

// Hint returns advice for the common failure modes, so the caller does not have
// to interpret status codes.
func (e *Error) Hint() string {
	switch e.Status {
	case http.StatusUnauthorized:
		return "The deploy token was rejected. Run `token rotate` in the server console, then `sorahost link` again."
	case http.StatusTooManyRequests:
		return "Too many failed attempts were made from this address; wait a minute and retry."
	case http.StatusNotFound:
		// The agent names the missing endpoint itself; only a wholesale 404,
		// where the path is wrong, needs explaining.
		if strings.Contains(e.Message, "no such endpoint") {
			return ""
		}
		return "The endpoint path does not exist. Run `url` in the server console to see the current one."
	case http.StatusConflict:
		if strings.Contains(e.Message, "in progress") {
			return "Wait for the running deployment to finish, then retry."
		}
	}
	return ""
}

func (c *Client) do(ctx context.Context, method, path string, body io.Reader, headers map[string]string, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, c.endpoint+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("User-Agent", c.agent)
	req.Header.Set("Accept", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	res, err := c.http.Do(req)
	if err != nil {
		return friendlyTransportError(err, c.endpoint)
	}
	defer res.Body.Close()

	// Bound the response: a misrouted request could return anything at all.
	raw, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("could not read the server's response: %w", err)
	}

	if res.StatusCode >= 400 {
		return &Error{Status: res.StatusCode, Message: extractMessage(raw, res.StatusCode)}
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("the server returned a response this CLI does not understand (HTTP %d)", res.StatusCode)
	}
	return nil
}

// PingResult confirms an endpoint and token work together.
type PingResult struct {
	OK    bool   `json:"ok"`
	Agent string `json:"agent"`
}

// Ping verifies credentials without changing anything.
func (c *Client) Ping(ctx context.Context) (*PingResult, error) {
	var out PingResult
	if err := c.do(ctx, http.MethodGet, "/ping", nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Status is the deployment state reported by the agent.
type Status struct {
	OK        bool     `json:"ok"`
	Running   bool     `json:"running"`
	Release   string   `json:"release"`
	Mode      string   `json:"mode"`
	Framework string   `json:"framework"`
	StartedAt string   `json:"startedAt"`
	Releases  []string `json:"releases"`
	History   []struct {
		ID    string `json:"id"`
		At    string `json:"at"`
		Mode  string `json:"mode"`
		Bytes int64  `json:"bytes"`
	} `json:"history"`
}

// Status fetches the current deployment state.
func (c *Client) Status(ctx context.Context) (*Status, error) {
	var out Status
	if err := c.do(ctx, http.MethodGet, "/status", nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// LogLine is one line from the server's log buffer.
type LogLine struct {
	TS      string `json:"ts"`
	Tag     string `json:"tag"`
	Message string `json:"message"`
}

// Logs fetches the last `tail` log lines.
func (c *Client) Logs(ctx context.Context, tail int) ([]LogLine, error) {
	var out struct {
		Lines []LogLine `json:"lines"`
	}
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/logs?tail=%d", tail), nil, nil, &out); err != nil {
		return nil, err
	}
	return out.Lines, nil
}

// DeployResult identifies the release that went live.
type DeployResult struct {
	Release   string `json:"release"`
	Mode      string `json:"mode"`
	Framework string `json:"framework"`
}

// Deploy uploads an artifact and waits for the server to activate it.
//
// `progress` is called as bytes leave this machine; it may be nil.
func (c *Client) Deploy(ctx context.Context, path, sha256hex string, progress func(sent, total int64)) (*DeployResult, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}

	body := io.Reader(f)
	if progress != nil {
		body = &progressReader{r: f, total: info.Size(), report: progress}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"/deploy", body)
	if err != nil {
		return nil, err
	}
	req.ContentLength = info.Size()
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("User-Agent", c.agent)
	req.Header.Set("Content-Type", "application/gzip")
	req.Header.Set("X-Artifact-Sha256", sha256hex)

	res, err := c.http.Do(req)
	if err != nil {
		return nil, friendlyTransportError(err, c.endpoint)
	}
	defer res.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode >= 400 {
		return nil, &Error{Status: res.StatusCode, Message: extractMessage(raw, res.StatusCode)}
	}
	var out DeployResult
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("the deployment was accepted but the response could not be read")
	}
	return &out, nil
}

// Rollback activates a previous release; an empty id means "the one before
// the current release".
func (c *Client) Rollback(ctx context.Context, id string) (string, error) {
	payload, err := json.Marshal(map[string]string{"id": id})
	if err != nil {
		return "", err
	}
	var out struct {
		Release string `json:"release"`
	}
	headers := map[string]string{"Content-Type": "application/json"}
	if err := c.do(ctx, http.MethodPost, "/rollback", bytes.NewReader(payload), headers, &out); err != nil {
		return "", err
	}
	return out.Release, nil
}

// extractMessage pulls the agent's error text out of a JSON body, falling back
// to the status line when the response came from something else entirely.
func extractMessage(raw []byte, status int) string {
	var payload struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(raw, &payload) == nil && payload.Error != "" {
		return payload.Error
	}
	text := strings.TrimSpace(string(raw))
	if len(text) > 200 {
		text = text[:200] + "..."
	}
	if text == "" {
		return fmt.Sprintf("the server returned HTTP %d", status)
	}
	return fmt.Sprintf("the server returned HTTP %d: %s", status, text)
}

// friendlyTransportError rewrites Go's transport errors into something a user
// can act on, without echoing the request URL (which carries the API path).
func friendlyTransportError(err error, endpoint string) error {
	host := endpoint
	if u, perr := url.Parse(endpoint); perr == nil {
		host = u.Host
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("timed out talking to %s", host)
	case errors.Is(err, context.Canceled):
		return errors.New("cancelled")
	case strings.Contains(err.Error(), "certificate"):
		return fmt.Errorf("the TLS certificate for %s could not be verified", host)
	case strings.Contains(err.Error(), "no such host"):
		return fmt.Errorf("%s could not be resolved", host)
	case strings.Contains(err.Error(), "connection refused"):
		return fmt.Errorf("%s refused the connection - is the server running?", host)
	}
	return fmt.Errorf("could not reach %s: %w", host, redactURL(err, endpoint))
}

// redactURL strips the endpoint (which contains the secret API path) out of an
// error before it is printed or copied into a bug report.
func redactURL(err error, endpoint string) error {
	msg := err.Error()
	if strings.Contains(msg, endpoint) {
		return errors.New(strings.ReplaceAll(msg, endpoint, "<endpoint>"))
	}
	return err
}

type progressReader struct {
	r      io.Reader
	sent   int64
	total  int64
	last   time.Time
	report func(sent, total int64)
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.sent += int64(n)
	// Report at most ten times a second; a progress line is for reassurance,
	// not telemetry, and CI logs should not fill up with it.
	if now := time.Now(); err == io.EOF || now.Sub(p.last) > 100*time.Millisecond {
		p.last = now
		p.report(p.sent, p.total)
	}
	return n, err
}
