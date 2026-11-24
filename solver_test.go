package main

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	extapi "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/cert-manager/cert-manager/pkg/acme/webhook/apis/acme/v1alpha1"

	"github.com/flinq-ingenieria/cert-manager-webhook-dinahosting/dinahosting"
)

// fakeAPI is an in-memory Dinahosting zone that mimics the documented
// error codes.
type fakeAPI struct {
	records  map[string][]dinahosting.TXTRecord // domain -> records
	adds     int
	username string
	password string
}

func (f *fakeAPI) AddTXTRecord(_ context.Context, domain, host, text string) error {
	f.adds++
	for _, r := range f.records[domain] {
		if r.Hostname == host && r.Text == text {
			return &dinahosting.APIError{Code: dinahosting.CodeObjectExists, Message: "exists"}
		}
	}
	f.records[domain] = append(f.records[domain], dinahosting.TXTRecord{Hostname: host, Text: text})
	return nil
}

func (f *fakeAPI) DeleteTXTRecord(_ context.Context, domain, host, value string) error {
	recs := f.records[domain]
	for i, r := range recs {
		if r.Hostname == host && r.Text == value {
			f.records[domain] = append(recs[:i], recs[i+1:]...)
			return nil
		}
	}
	return &dinahosting.APIError{Code: dinahosting.CodeObjectNotExists, Message: "not exists"}
}

func (f *fakeAPI) GetTXTRecords(_ context.Context, domain string) ([]dinahosting.TXTRecord, error) {
	return append([]dinahosting.TXTRecord(nil), f.records[domain]...), nil
}

func newTestSolver(t *testing.T) (*dinahostingSolver, *fakeAPI) {
	t.Helper()
	api := &fakeAPI{records: map[string][]dinahosting.TXTRecord{}}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "dinahosting-credentials", Namespace: "cert-manager"},
		Data:       map[string][]byte{"username": []byte("user\n"), "password": []byte("pass")},
	}
	s := &dinahostingSolver{
		client: fake.NewSimpleClientset(secret),
		newAPI: func(_ solverConfig, u, p string) dnsAPI {
			api.username, api.password = u, p
			return api
		},
	}
	return s, api
}

const validConfig = `{"usernameSecretRef":{"name":"dinahosting-credentials","key":"username"},
	"passwordSecretRef":{"name":"dinahosting-credentials","key":"password"}}`

func challenge(fqdn, key, cfg string) *v1alpha1.ChallengeRequest {
	return &v1alpha1.ChallengeRequest{
		ResolvedFQDN:      fqdn,
		ResolvedZone:      "zverp.com.",
		Key:               key,
		ResourceNamespace: "cert-manager",
		Config:            &extapi.JSON{Raw: []byte(cfg)},
	}
}

func TestPresentAndCleanUp(t *testing.T) {
	s, api := newTestSolver(t)
	ch := challenge("_acme-challenge.runboat.zverp.com.", "k1", validConfig)

	if err := s.Present(ch); err != nil {
		t.Fatalf("Present: %v", err)
	}
	if api.username != "user" || api.password != "pass" {
		t.Errorf("credentials = %q/%q (username must be trimmed)", api.username, api.password)
	}
	want := dinahosting.TXTRecord{Hostname: "_acme-challenge.runboat", Text: "k1"}
	if got := api.records["zverp.com"]; len(got) != 1 || got[0] != want {
		t.Fatalf("records = %+v", got)
	}

	// Idempotent: a second Present must not add again.
	if err := s.Present(ch); err != nil {
		t.Fatalf("second Present: %v", err)
	}
	if api.adds != 1 {
		t.Errorf("adds = %d, want 1", api.adds)
	}

	if err := s.CleanUp(ch); err != nil {
		t.Fatalf("CleanUp: %v", err)
	}
	if len(api.records["zverp.com"]) != 0 {
		t.Errorf("record not deleted: %+v", api.records)
	}
	// CleanUp of an absent record is not an error.
	if err := s.CleanUp(ch); err != nil {
		t.Errorf("second CleanUp: %v", err)
	}
}

// listFailingAPI fails GetTXTRecords but otherwise behaves like fakeAPI.
type listFailingAPI struct{ *fakeAPI }

func (l listFailingAPI) GetTXTRecords(context.Context, string) ([]dinahosting.TXTRecord, error) {
	return nil, &dinahosting.APIError{Code: 2400, Message: "boom"}
}

func TestPresentAddsEvenIfListingFails(t *testing.T) {
	s, api := newTestSolver(t)
	s.newAPI = func(solverConfig, string, string) dnsAPI { return listFailingAPI{api} }
	if err := s.Present(challenge("_acme-challenge.zverp.com.", "k", validConfig)); err != nil {
		t.Fatalf("Present: %v", err)
	}
	if api.adds != 1 || len(api.records["zverp.com"]) != 1 {
		t.Errorf("adds=%d records=%+v", api.adds, api.records)
	}
}

func TestCleanUpOnlyRemovesMatchingKey(t *testing.T) {
	s, api := newTestSolver(t)
	apex := challenge("_acme-challenge.zverp.com.", "apex", validConfig)
	wild := challenge("_acme-challenge.zverp.com.", "wildcard", validConfig)
	for _, ch := range []*v1alpha1.ChallengeRequest{apex, wild} {
		if err := s.Present(ch); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.CleanUp(apex); err != nil {
		t.Fatal(err)
	}
	got := api.records["zverp.com"]
	if len(got) != 1 || got[0].Text != "wildcard" || got[0].Hostname != "_acme-challenge" {
		t.Errorf("records = %+v", got)
	}
}

func TestConfigErrors(t *testing.T) {
	s, _ := newTestSolver(t)
	cases := map[string]*v1alpha1.ChallengeRequest{
		"missing config": {ResolvedFQDN: "_acme-challenge.zverp.com.", ResolvedZone: "zverp.com.", ResourceNamespace: "cert-manager"},
		"missing key":    challenge("_acme-challenge.zverp.com.", "k", `{"usernameSecretRef":{"name":"dinahosting-credentials"},"passwordSecretRef":{"name":"dinahosting-credentials","key":"password"}}`),
		"absent secret":  challenge("_acme-challenge.zverp.com.", "k", strings.ReplaceAll(validConfig, "dinahosting-credentials", "nope")),
		"absent key":     challenge("_acme-challenge.zverp.com.", "k", strings.ReplaceAll(validConfig, `"key":"password"`, `"key":"other"`)),
		"outside zone":   challenge("_acme-challenge.example.org.", "k", validConfig),
	}
	for name, ch := range cases {
		if err := s.Present(ch); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestSplitFQDN(t *testing.T) {
	cases := []struct{ fqdn, zone, domain, host string }{
		{"_acme-challenge.runboat.zverp.com.", "zverp.com.", "zverp.com", "_acme-challenge.runboat"},
		{"_acme-challenge.zverp.com.", "zverp.com.", "zverp.com", "_acme-challenge"},
		{"_ACME-Challenge.Zverp.com", "zverp.com", "zverp.com", "_acme-challenge"},
		{"zverp.com.", "zverp.com.", "zverp.com", ""},
	}
	for _, c := range cases {
		d, h, err := splitFQDN(c.fqdn, c.zone)
		if err != nil || d != c.domain || h != c.host {
			t.Errorf("splitFQDN(%q,%q) = %q,%q,%v", c.fqdn, c.zone, d, h, err)
		}
	}
	if _, _, err := splitFQDN("_acme-challenge.notzverp.com.", "zverp.com."); err == nil {
		t.Error("expected error for suffix that is not a label boundary")
	}
}
