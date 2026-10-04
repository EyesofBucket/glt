// Package gitlab is a small, purpose-built GitLab API client. It only
// implements what the TUI needs and keeps connections warm for low latency.
package gitlab

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Client struct {
	base  string // https://host/api/v4
	token string
	http  *http.Client
}

// TLSOptions configure trust for self-hosted instances.
type TLSOptions struct {
	CACert     string
	SkipVerify bool
}

func New(apiBase, token string, opts ...TLSOptions) *Client {
	tr := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          32,
		MaxIdleConnsPerHost:   16,
		IdleConnTimeout:       5 * time.Minute,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
	for _, o := range opts {
		if o.CACert == "" && !o.SkipVerify {
			continue
		}
		tc := &tls.Config{InsecureSkipVerify: o.SkipVerify}
		if o.CACert != "" {
			pool, err := x509.SystemCertPool()
			if err != nil {
				pool = x509.NewCertPool()
			}
			if pem, err := os.ReadFile(o.CACert); err == nil {
				pool.AppendCertsFromPEM(pem)
			}
			tc.RootCAs = pool
		}
		tr.TLSClientConfig = tc
	}
	return &Client{
		base:  strings.TrimSuffix(apiBase, "/"),
		token: token,
		http:  &http.Client{Transport: tr, Timeout: 30 * time.Second},
	}
}

type APIError struct {
	Status     int
	Message    string
	RetryAfter time.Duration
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("%d: %s", e.Status, e.Message)
	}
	return fmt.Sprintf("HTTP %d", e.Status)
}

func IsRateLimited(err error) (time.Duration, bool) {
	var ae *APIError
	if errors.As(err, &ae) && ae.Status == http.StatusTooManyRequests {
		return ae.RetryAfter, true
	}
	return 0, false
}

func (c *Client) newRequest(ctx context.Context, method, path string, q url.Values, body any) (*http.Request, error) {
	u := c.base + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("User-Agent", "glt")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

func readError(resp *http.Response) error {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	ae := &APIError{Status: resp.StatusCode}
	var m struct {
		Message any    `json:"message"`
		Error   string `json:"error"`
	}
	if json.Unmarshal(b, &m) == nil {
		switch v := m.Message.(type) {
		case string:
			ae.Message = v
		case nil:
			ae.Message = m.Error
		default:
			mb, _ := json.Marshal(v)
			ae.Message = string(mb)
		}
	} else {
		ae.Message = strings.TrimSpace(string(b))
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		ae.Message = "rate limited by GitLab"
		if s, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil {
			ae.RetryAfter = time.Duration(s) * time.Second
		}
	}
	return ae
}

// do performs a request and decodes a JSON response into out (if non-nil).
// It returns the response headers for pagination.
func (c *Client) do(ctx context.Context, method, path string, q url.Values, body, out any) (http.Header, error) {
	req, err := c.newRequest(ctx, method, path, q, body)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return resp.Header, readError(resp)
	}
	if out == nil {
		io.Copy(io.Discard, resp.Body)
		return resp.Header, nil
	}
	return resp.Header, json.NewDecoder(resp.Body).Decode(out)
}

func (c *Client) get(ctx context.Context, path string, q url.Values, out any) error {
	_, err := c.do(ctx, http.MethodGet, path, q, nil, out)
	return err
}

// getAll fetches up to maxPages pages. Once the first page reports the
// total page count the rest are fetched in parallel.
func getAll[T any](ctx context.Context, c *Client, path string, q url.Values, maxPages int) ([]T, error) {
	if q == nil {
		q = url.Values{}
	}
	q.Set("per_page", "100")
	q.Set("page", "1")
	var first []T
	h, err := c.do(ctx, http.MethodGet, path, q, nil, &first)
	if err != nil {
		return nil, err
	}
	pages, _ := strconv.Atoi(h.Get("X-Total-Pages"))
	if pages <= 1 {
		if h.Get("X-Next-Page") == "" || maxPages == 1 {
			return first, nil
		}
		// no total (very large collections): fall back to sequential
		all := first
		for page := 2; page <= maxPages; page++ {
			q.Set("page", strconv.Itoa(page))
			var batch []T
			h, err := c.do(ctx, http.MethodGet, path, q, nil, &batch)
			if err != nil {
				return all, err
			}
			all = append(all, batch...)
			if h.Get("X-Next-Page") == "" {
				break
			}
		}
		return all, nil
	}
	pages = min(pages, maxPages)
	results := make([][]T, pages)
	errs := make([]error, pages)
	results[0] = first
	var wg sync.WaitGroup
	for page := 2; page <= pages; page++ {
		pq := url.Values{}
		for k, v := range q {
			pq[k] = v
		}
		pq.Set("page", strconv.Itoa(page))
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = c.do(ctx, http.MethodGet, path, pq, nil, &results[i])
		}(page - 1)
	}
	wg.Wait()
	var all []T
	for i, r := range results {
		if errs[i] != nil {
			return all, errs[i]
		}
		all = append(all, r...)
	}
	return all, nil
}

func (c *Client) graphql(ctx context.Context, query string, vars map[string]any, out any) error {
	gqlURL := strings.TrimSuffix(c.base, "/v4")
	gqlURL = strings.TrimSuffix(gqlURL, "/api") + "/api/graphql"
	b, _ := json.Marshal(map[string]any{"query": query, "variables": vars})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, gqlURL, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "glt")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return readError(resp)
	}
	var env struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return err
	}
	if len(env.Errors) > 0 {
		return &APIError{Status: resp.StatusCode, Message: env.Errors[0].Message}
	}
	return json.Unmarshal(env.Data, out)
}

func pid(project string) string {
	return url.PathEscape(project)
}
