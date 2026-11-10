package collector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ErrPrometheus is returned when Prometheus cannot be queried or answers
// with an error. The snapshot command fails with it (exit code 2).
var ErrPrometheus = errors.New("prometheus query failed")

// promClient runs instant queries against the HTTP API of Prometheus
// (GET /api/v1/query). It only reads, so it needs no more than read access
// to the query API (NFR1). It uses net/http and encoding/json instead of the
// Prometheus client module, so no new dependency is needed.
type promClient struct {
	baseURL string // e.g. "http://prometheus.monitoring:9090"
	http    *http.Client
}

// promSample is one series of an instant vector: its labels and its value.
type promSample struct {
	Labels map[string]string
	Value  float64
}

// promResponse is the envelope of every Prometheus API response.
type promResponse struct {
	Status    string `json:"status"` // "success" or "error"
	ErrorType string `json:"errorType"`
	Error     string `json:"error"`
	Data      struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Metric map[string]string `json:"metric"`
			Value  [2]any            `json:"value"` // [unix time, "value"]
		} `json:"result"`
	} `json:"data"`
}

// query runs an instant query evaluated at time at and returns the samples
// of the resulting vector. Any transport error, non-success status or result
// that is not a vector is an error wrapping ErrPrometheus.
func (c *promClient) query(ctx context.Context, q string, at time.Time) ([]promSample, error) {
	params := url.Values{}
	params.Set("query", q)
	params.Set("time", strconv.FormatInt(at.Unix(), 10))
	endpoint := strings.TrimSuffix(c.baseURL, "/") + "/api/v1/query?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrPrometheus, err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrPrometheus, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("%w: reading response: %w", ErrPrometheus, err)
	}

	// Prometheus also answers errors (400, 422, 503) with a JSON body, so
	// the body is decoded before the status code is checked.
	var r promResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("%w: HTTP %d: decoding response: %w", ErrPrometheus, resp.StatusCode, err)
	}
	if r.Status != "success" {
		return nil, fmt.Errorf("%w: HTTP %d: %s: %s", ErrPrometheus, resp.StatusCode, r.ErrorType, r.Error)
	}
	if r.Data.ResultType != "vector" {
		return nil, fmt.Errorf("%w: result type %q, want vector", ErrPrometheus, r.Data.ResultType)
	}

	samples := make([]promSample, 0, len(r.Data.Result))
	for _, s := range r.Data.Result {
		text, ok := s.Value[1].(string)
		if !ok {
			return nil, fmt.Errorf("%w: sample value %v is not a string", ErrPrometheus, s.Value[1])
		}
		v, err := strconv.ParseFloat(text, 64) // also parses "NaN" and "+Inf"
		if err != nil {
			return nil, fmt.Errorf("%w: sample value %q: %w", ErrPrometheus, text, err)
		}
		samples = append(samples, promSample{Labels: s.Metric, Value: v})
	}
	return samples, nil
}
