package validatoridentityenricher

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const maxRadarResponseBytes = 1 << 20

// RadarOptions configure the HTTP identity adapter.
type RadarOptions struct {
	Client         *http.Client
	BaseURL        string
	MainnetBaseURL string
	TestnetBaseURL string
	At             *time.Time
	Retries        int
	RetryDelay     time.Duration
	UserAgent      string
}

// RadarResolver resolves validator identities through Radar's node endpoint.
type RadarResolver struct {
	client         *http.Client
	baseURL        string
	mainnetBaseURL string
	testnetBaseURL string
	at             *time.Time
	retries        int
	retryDelay     time.Duration
	userAgent      string
}

// NewRadarResolver validates and constructs a Radar HTTP adapter.
func NewRadarResolver(opts RadarOptions) (*RadarResolver, error) {
	if opts.Client == nil {
		opts.Client = http.DefaultClient
	}
	if opts.MainnetBaseURL == "" {
		opts.MainnetBaseURL = "https://radar.withobsrvr.com/api"
	}
	if opts.TestnetBaseURL == "" {
		opts.TestnetBaseURL = "https://radar.withobsrvr.com/testnet-api"
	}
	for _, baseURL := range []string{opts.BaseURL, opts.MainnetBaseURL, opts.TestnetBaseURL} {
		if baseURL == "" {
			continue
		}
		parsed, err := url.Parse(baseURL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return nil, fmt.Errorf("invalid Radar base URL %q", baseURL)
		}
	}
	if opts.Retries < 0 {
		return nil, fmt.Errorf("retries cannot be negative")
	}
	if opts.RetryDelay < 0 {
		return nil, fmt.Errorf("retry delay cannot be negative")
	}
	if opts.UserAgent == "" {
		opts.UserAgent = "validator-identity-enricher/0.1.0"
	}
	return &RadarResolver{
		client:         opts.Client,
		baseURL:        strings.TrimRight(opts.BaseURL, "/"),
		mainnetBaseURL: strings.TrimRight(opts.MainnetBaseURL, "/"),
		testnetBaseURL: strings.TrimRight(opts.TestnetBaseURL, "/"),
		at:             opts.At,
		retries:        opts.Retries,
		retryDelay:     opts.RetryDelay,
		userAgent:      opts.UserAgent,
	}, nil
}

func (r *RadarResolver) Source() string { return "radar" }

func (r *RadarResolver) TemporalBasis() string {
	if r.at != nil {
		return "at"
	}
	return "current"
}

// Resolve performs a bounded Radar lookup. A 404 is a stable not-found result;
// transport, decoding, and server failures remain soft unavailable results.
func (r *RadarResolver) Resolve(ctx context.Context, networkName, publicKey string) LookupResult {
	baseURL, ok := r.urlForNetwork(networkName)
	if !ok {
		return LookupResult{Status: StatusUnavailable, Reason: "radar_network_unsupported"}
	}

	endpoint := baseURL + "/v1/node/" + url.PathEscape(publicKey)
	if r.at != nil {
		parsed, err := url.Parse(endpoint)
		if err != nil {
			return LookupResult{Status: StatusUnavailable, Reason: "radar_url_invalid"}
		}
		query := parsed.Query()
		query.Set("at", r.at.UTC().Format(time.RFC3339))
		parsed.RawQuery = query.Encode()
		endpoint = parsed.String()
	}

	var last LookupResult
	for attempt := 0; attempt <= r.retries; attempt++ {
		last = r.resolveOnce(ctx, endpoint, publicKey)
		if last.Status != StatusUnavailable || !isRetryableReason(last.Reason) || attempt == r.retries {
			return last
		}
		if !waitForRetry(ctx, r.retryDelay) {
			return LookupResult{Status: StatusUnavailable, Reason: "radar_request_timeout"}
		}
	}
	return last
}

func (r *RadarResolver) resolveOnce(ctx context.Context, endpoint, publicKey string) LookupResult {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return LookupResult{Status: StatusUnavailable, Reason: "radar_request_invalid"}
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", r.userAgent)

	resp, err := r.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return LookupResult{Status: StatusUnavailable, Reason: "radar_request_timeout"}
		}
		return LookupResult{Status: StatusUnavailable, Reason: "radar_transport_error"}
	}
	defer func() {
		// Drain within the size limit so the connection can be reused.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxRadarResponseBytes))
		_ = resp.Body.Close()
	}()

	switch resp.StatusCode {
	case http.StatusOK:
		var node NodeIdentity
		decoder := json.NewDecoder(io.LimitReader(resp.Body, maxRadarResponseBytes+1))
		if err := decoder.Decode(&node); err != nil {
			return LookupResult{Status: StatusUnavailable, Reason: "radar_invalid_response"}
		}
		if decoder.Decode(&json.RawMessage{}) != io.EOF {
			return LookupResult{Status: StatusUnavailable, Reason: "radar_invalid_response"}
		}
		if node.PublicKey != publicKey {
			return LookupResult{Status: StatusUnavailable, Reason: "radar_public_key_mismatch"}
		}
		return LookupResult{Status: StatusResolved, Node: node}
	case http.StatusNotFound:
		return LookupResult{Status: StatusNotFound, Reason: "radar_not_found"}
	default:
		return LookupResult{Status: StatusUnavailable, Reason: "radar_http_" + strconv.Itoa(resp.StatusCode)}
	}
}

func (r *RadarResolver) urlForNetwork(networkName string) (string, bool) {
	if r.baseURL != "" {
		return r.baseURL, true
	}
	switch networkName {
	case "mainnet":
		return r.mainnetBaseURL, true
	case "testnet":
		return r.testnetBaseURL, true
	default:
		return "", false
	}
}

func isRetryableReason(reason string) bool {
	if reason == "radar_transport_error" || reason == "radar_request_timeout" {
		return true
	}
	if !strings.HasPrefix(reason, "radar_http_") {
		return false
	}
	status, err := strconv.Atoi(strings.TrimPrefix(reason, "radar_http_"))
	return err == nil && (status == http.StatusTooManyRequests || status >= 500)
}

func waitForRetry(ctx context.Context, delay time.Duration) bool {
	if delay == 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
