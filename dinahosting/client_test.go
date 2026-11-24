package dinahosting

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestServer(t *testing.T, handler func(w http.ResponseWriter, r *http.Request)) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(handler))
	t.Cleanup(srv.Close)
	c := NewClient("user", "s3cret")
	c.Endpoint = srv.URL
	return c
}

func TestAddTXTRecordSendsDocumentedRequest(t *testing.T) {
	c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/x-www-form-urlencoded" {
			t.Errorf("content-type = %q", ct)
		}
		u, p, ok := r.BasicAuth()
		if !ok || u != "user" || p != "s3cret" {
			t.Errorf("basic auth = %q/%q/%v", u, p, ok)
		}
		if strings.Contains(r.URL.RawQuery, "s3cret") {
			t.Errorf("password leaked into URL")
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		want := map[string]string{
			"command":      "Domain_Zone_AddTypeTXT",
			"responseType": "Json",
			"domain":       "zverp.com",
			"hostname":     "_acme-challenge.runboat",
			"text":         "token-value",
			"AUTH_USER":    "user",
			"AUTH_PWD":     "s3cret",
		}
		if r.URL.RawQuery != "" {
			t.Errorf("unexpected query string %q", r.URL.RawQuery)
		}
		for k, v := range want {
			if got := r.PostForm.Get(k); got != v {
				t.Errorf("%s = %q, want %q", k, got, v)
			}
		}
		_, _ = w.Write([]byte(`{"trId":"x","responseCode":1000,"message":"Success.","data":null}`))
	})
	if err := c.AddTXTRecord(context.Background(), "zverp.com", "_acme-challenge.runboat", "token-value"); err != nil {
		t.Fatalf("AddTXTRecord: %v", err)
	}
}

func TestDeleteTXTRecordSendsValue(t *testing.T) {
	c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.PostForm.Get("command") != "Domain_Zone_DeleteTypeTXT" || r.PostForm.Get("value") != "v1" ||
			r.PostForm.Get("hostname") != "_acme-challenge" || r.PostForm.Get("domain") != "zverp.com" {
			t.Errorf("unexpected form: %v", r.PostForm)
		}
		_, _ = w.Write([]byte(`{"responseCode":"1000","message":"Success."}`))
	})
	if err := c.DeleteTXTRecord(context.Background(), "zverp.com", "_acme-challenge", "v1"); err != nil {
		t.Fatalf("DeleteTXTRecord: %v", err)
	}
}

func TestAPIErrorCodes(t *testing.T) {
	c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"responseCode":2303,"message":"Object does not exist.","errors":[{"code":2303}]}`))
	})
	err := c.DeleteTXTRecord(context.Background(), "zverp.com", "_acme-challenge", "v1")
	if !IsCode(err, CodeObjectNotExists) {
		t.Fatalf("err = %v, want code %d", err, CodeObjectNotExists)
	}
	if !strings.Contains(err.Error(), "Object does not exist") {
		t.Errorf("error message lost: %v", err)
	}
}

func TestHTTPErrorAndInvalidJSON(t *testing.T) {
	c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusBadGateway)
	})
	if err := c.AddTXTRecord(context.Background(), "d", "h", "t"); err == nil || !strings.Contains(err.Error(), "HTTP 502") {
		t.Errorf("err = %v, want HTTP 502", err)
	}

	c = newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html>not json</html>`))
	})
	if err := c.AddTXTRecord(context.Background(), "d", "h", "t"); err == nil || !strings.Contains(err.Error(), "invalid JSON") {
		t.Errorf("err = %v, want invalid JSON", err)
	}
}

func TestGetTXTRecords(t *testing.T) {
	c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.PostForm.Get("command") != "Domain_Zone_GetTypeTXT" {
			t.Errorf("command = %q", r.PostForm.Get("command"))
		}
		_, _ = w.Write([]byte(`{"responseCode":1000,"message":"Success.","data":[
			{"host":"key_9344._domainkey","text":"v=DKIM1"},
			{"hostname":"_acme-challenge.runboat","text":"abc"}]}`))
	})
	recs, err := c.GetTXTRecords(context.Background(), "zverp.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 || recs[0].Hostname != "key_9344._domainkey" || recs[1].Hostname != "_acme-challenge.runboat" || recs[1].Text != "abc" {
		t.Errorf("records = %+v", recs)
	}
}

func TestGetTXTRecordsEmpty(t *testing.T) {
	for _, body := range []string{
		`{"responseCode":1000,"message":"Success.","data":[]}`,
		`{"responseCode":1000,"message":"Success.","data":null}`,
		`{"responseCode":1000,"message":"Success."}`,
	} {
		c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) })
		recs, err := c.GetTXTRecords(context.Background(), "zverp.com")
		if err != nil || len(recs) != 0 {
			t.Errorf("body %s: recs=%v err=%v", body, recs, err)
		}
	}
}
