package email

import (
	"context"
	"fmt"
	"io"
	"net/http"
)

// newJSONRequest creates an HTTP request with JSON content type.
func newJSONRequest(ctx context.Context, method, url string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

// doRequest executes the request and checks for the expected status code.
// Reads and discards the response body to allow connection reuse.
func doRequest(req *http.Request, wantStatus int) error {
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != wantStatus {
		return fmt.Errorf("unexpected status %d (want %d)", resp.StatusCode, wantStatus)
	}
	return nil
}
