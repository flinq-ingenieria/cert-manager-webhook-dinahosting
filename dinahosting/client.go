// Package dinahosting implements the small subset of the Dinahosting API
// (https://dinahosting.com/api) needed to manage TXT records for ACME DNS-01
// challenges.
//
// It uses the documented "Simple" request type: an HTTP POST with
// URL-encoded key=value pairs and responseType=Json. Credentials are sent
// both as AUTH_USER/AUTH_PWD body parameters and in an "Authorization: Basic"
// header (both documented), and never in the URL.
package dinahosting

import (
	"bytes"
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

// DefaultEndpoint is the production Dinahosting API endpoint.
const DefaultEndpoint = "https://dinahosting.com/special/api.php"

// Response codes documented in the "Errors" section of the API reference.
const (
	CodeSuccess         = 1000
	CodeSuccessPending  = 1001
	CodeObjectExists    = 2302
	CodeObjectNotExists = 2303
)

// maxResponseBytes bounds how much of a response body is read.
const maxResponseBytes = 4 << 20

// Client talks to the Dinahosting API.
type Client struct {
	Endpoint   string
	Username   string
	Password   string
	HTTPClient *http.Client
}

// NewClient returns a Client for the production endpoint.
func NewClient(username, password string) *Client {
	return &Client{
		Endpoint:   DefaultEndpoint,
		Username:   username,
		Password:   password,
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// APIError is returned when the API answers with a non-success response code.
type APIError struct {
	Command string
	Code    int
	Message string
	Details string
}

func (e *APIError) Error() string {
	msg := fmt.Sprintf("dinahosting %s: code %d: %s", e.Command, e.Code, e.Message)
	if e.Details != "" {
		msg += " (" + e.Details + ")"
	}
	return msg
}

// IsCode reports whether err is an *APIError with the given response code.
func IsCode(err error, code int) bool {
	apiErr, ok := err.(*APIError)
	return ok && apiErr.Code == code
}

// TXTRecord is a TXT record as returned by Domain_Zone_GetTypeTXT.
// Hostname is relative to the domain (e.g. "_acme-challenge.runboat").
type TXTRecord struct {
	Hostname string
	Text     string
}

// AddTXTRecord creates a TXT record (Domain_Zone_AddTypeTXT).
func (c *Client) AddTXTRecord(ctx context.Context, domain, hostname, text string) error {
	_, err := c.call(ctx, "Domain_Zone_AddTypeTXT", url.Values{
		"domain":   {domain},
		"hostname": {hostname},
		"text":     {text},
	})
	return err
}

// DeleteTXTRecord deletes the TXT record with the given hostname and value
// (Domain_Zone_DeleteTypeTXT). Passing the value makes the API delete only
// that record when several TXT records share the same hostname.
func (c *Client) DeleteTXTRecord(ctx context.Context, domain, hostname, value string) error {
	_, err := c.call(ctx, "Domain_Zone_DeleteTypeTXT", url.Values{
		"domain":   {domain},
		"hostname": {hostname},
		"value":    {value},
	})
	return err
}

// GetTXTRecords lists the TXT records of a domain (Domain_Zone_GetTypeTXT).
func (c *Client) GetTXTRecords(ctx context.Context, domain string) ([]TXTRecord, error) {
	data, err := c.call(ctx, "Domain_Zone_GetTypeTXT", url.Values{"domain": {domain}})
	if err != nil {
		return nil, err
	}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return nil, nil
	}
	var raw []struct {
		Host     string `json:"host"`
		Hostname string `json:"hostname"`
		Text     string `json:"text"`
	}
	if err := json.Unmarshal(trimmed, &raw); err != nil {
		return nil, fmt.Errorf("dinahosting Domain_Zone_GetTypeTXT: decoding data: %w", err)
	}
	records := make([]TXTRecord, 0, len(raw))
	for _, r := range raw {
		name := r.Host
		if name == "" {
			name = r.Hostname
		}
		records = append(records, TXTRecord{Hostname: name, Text: r.Text})
	}
	return records, nil
}

// apiResponse is the envelope of a Json response.
type apiResponse struct {
	ResponseCode flexInt         `json:"responseCode"`
	Message      string          `json:"message"`
	Data         json.RawMessage `json:"data"`
	Errors       json.RawMessage `json:"errors"`
}

// flexInt accepts both JSON numbers and numeric strings.
type flexInt int

func (f *flexInt) UnmarshalJSON(b []byte) error {
	s := strings.Trim(strings.TrimSpace(string(b)), `"`)
	if s == "" || s == "null" {
		*f = 0
		return nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return fmt.Errorf("invalid response code %q", s)
	}
	*f = flexInt(n)
	return nil
}

func (c *Client) call(ctx context.Context, command string, params url.Values) (json.RawMessage, error) {
	form := url.Values{}
	for k, v := range params {
		form[k] = v
	}
	form.Set("command", command)
	form.Set("responseType", "Json")
	// Both documented auth methods: AUTH_USER/AUTH_PWD request parameters
	// (sent in the POST body, never in the URL) and the Basic header below.
	form.Set("AUTH_USER", c.Username)
	form.Set("AUTH_PWD", c.Password)

	endpoint := c.Endpoint
	if endpoint == "" {
		endpoint = DefaultEndpoint
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("dinahosting %s: building request: %w", command, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.SetBasicAuth(c.Username, c.Password)

	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("dinahosting %s: %w", command, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("dinahosting %s: reading response: %w", command, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("dinahosting %s: HTTP %d: %s", command, resp.StatusCode, snippet(body))
	}

	var out apiResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("dinahosting %s: invalid JSON response: %s", command, snippet(body))
	}
	code := int(out.ResponseCode)
	if code != CodeSuccess && code != CodeSuccessPending {
		details := ""
		if e := bytes.TrimSpace(out.Errors); len(e) > 0 && string(e) != "null" && string(e) != "[]" {
			details = snippet(e)
		}
		return nil, &APIError{Command: command, Code: code, Message: out.Message, Details: details}
	}
	return out.Data, nil
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	return s
}
