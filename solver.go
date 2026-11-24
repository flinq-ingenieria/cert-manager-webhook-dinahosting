package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	extapi "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"

	"github.com/cert-manager/cert-manager/pkg/acme/webhook"
	"github.com/cert-manager/cert-manager/pkg/acme/webhook/apis/acme/v1alpha1"
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"

	"github.com/flinq-ingenieria/cert-manager-webhook-dinahosting/dinahosting"
)

// SolverName is the name referenced in the issuer's webhook stanza.
const SolverName = "dinahosting"

// apiTimeout bounds every Present/CleanUp call to the Dinahosting API.
const apiTimeout = 60 * time.Second

// dnsAPI is the subset of the Dinahosting client used by the solver.
type dnsAPI interface {
	AddTXTRecord(ctx context.Context, domain, hostname, text string) error
	DeleteTXTRecord(ctx context.Context, domain, hostname, value string) error
	GetTXTRecords(ctx context.Context, domain string) ([]dinahosting.TXTRecord, error)
}

// dinahostingSolver implements webhook.Solver for Dinahosting DNS.
type dinahostingSolver struct {
	client kubernetes.Interface
	// newAPI builds an API client; overridable in tests.
	newAPI func(cfg solverConfig, username, password string) dnsAPI
}

var _ webhook.Solver = (*dinahostingSolver)(nil)

// solverConfig is decoded from issuer.spec.acme.solvers[].dns01.webhook.config.
//
//	config:
//	  usernameSecretRef: {name: dinahosting-credentials, key: username}
//	  passwordSecretRef: {name: dinahosting-credentials, key: password}
//
// The Secret is read from the namespace of the Issuer, or from cert-manager's
// cluster resource namespace for a ClusterIssuer.
type solverConfig struct {
	UsernameSecretRef cmmeta.SecretKeySelector `json:"usernameSecretRef"`
	PasswordSecretRef cmmeta.SecretKeySelector `json:"passwordSecretRef"`
	// Endpoint overrides the API URL. Only meant for testing.
	Endpoint string `json:"endpoint,omitempty"`
}

func newSolver() *dinahostingSolver {
	return &dinahostingSolver{newAPI: defaultAPI}
}

func defaultAPI(cfg solverConfig, username, password string) dnsAPI {
	c := dinahosting.NewClient(username, password)
	if cfg.Endpoint != "" {
		c.Endpoint = cfg.Endpoint
	}
	return c
}

func (s *dinahostingSolver) Name() string { return SolverName }

func (s *dinahostingSolver) Initialize(kubeClientConfig *rest.Config, _ <-chan struct{}) error {
	cl, err := kubernetes.NewForConfig(kubeClientConfig)
	if err != nil {
		return err
	}
	s.client = cl
	return nil
}

// Present creates the challenge TXT record. It is idempotent: if a record
// with the same hostname and value already exists, nothing is done.
func (s *dinahostingSolver) Present(ch *v1alpha1.ChallengeRequest) error {
	ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
	defer cancel()

	api, domain, host, err := s.prepare(ctx, ch)
	if err != nil {
		return err
	}

	// The pre-check only provides idempotency; if listing fails (e.g. an
	// unexpected response shape) still try to add the record.
	exists, err := hasRecord(ctx, api, domain, host, ch.Key)
	if err != nil {
		klog.InfoS("could not list TXT records, adding anyway", "domain", domain, "hostname", host, "err", err)
	}
	if exists {
		klog.InfoS("TXT record already present", "domain", domain, "hostname", host)
		return nil
	}

	err = api.AddTXTRecord(ctx, domain, host, ch.Key)
	if dinahosting.IsCode(err, dinahosting.CodeObjectExists) {
		// Tolerate a concurrent or previous identical add, but do not hide
		// a conflict with a different value.
		if ok, verr := hasRecord(ctx, api, domain, host, ch.Key); verr == nil && ok {
			return nil
		}
		return fmt.Errorf("adding TXT %s.%s: %w", host, domain, err)
	}
	if err != nil {
		return fmt.Errorf("adding TXT %s.%s: %w", host, domain, err)
	}
	klog.InfoS("TXT record created", "domain", domain, "hostname", host)
	return nil
}

// CleanUp deletes only the TXT record matching the challenge key, so that
// concurrent validations for the same name (e.g. example.com and
// *.example.com) do not interfere with each other.
func (s *dinahostingSolver) CleanUp(ch *v1alpha1.ChallengeRequest) error {
	ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
	defer cancel()

	api, domain, host, err := s.prepare(ctx, ch)
	if err != nil {
		return err
	}
	err = api.DeleteTXTRecord(ctx, domain, host, ch.Key)
	if dinahosting.IsCode(err, dinahosting.CodeObjectNotExists) {
		klog.InfoS("TXT record already absent", "domain", domain, "hostname", host)
		return nil
	}
	if err != nil {
		return fmt.Errorf("deleting TXT %s.%s: %w", host, domain, err)
	}
	klog.InfoS("TXT record deleted", "domain", domain, "hostname", host)
	return nil
}

func (s *dinahostingSolver) prepare(ctx context.Context, ch *v1alpha1.ChallengeRequest) (dnsAPI, string, string, error) {
	cfg, err := loadConfig(ch.Config)
	if err != nil {
		return nil, "", "", err
	}
	domain, host, err := splitFQDN(ch.ResolvedFQDN, ch.ResolvedZone)
	if err != nil {
		return nil, "", "", err
	}
	username, err := s.secretValue(ctx, ch.ResourceNamespace, cfg.UsernameSecretRef)
	if err != nil {
		return nil, "", "", err
	}
	password, err := s.secretValue(ctx, ch.ResourceNamespace, cfg.PasswordSecretRef)
	if err != nil {
		return nil, "", "", err
	}
	return s.newAPI(cfg, username, password), domain, host, nil
}

func (s *dinahostingSolver) secretValue(ctx context.Context, namespace string, ref cmmeta.SecretKeySelector) (string, error) {
	if ref.Name == "" || ref.Key == "" {
		return "", fmt.Errorf("secret reference must set both name and key")
	}
	if s.client == nil {
		return "", fmt.Errorf("kubernetes client not initialized")
	}
	secret, err := s.client.CoreV1().Secrets(namespace).Get(ctx, ref.Name, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("reading secret %s/%s: %w", namespace, ref.Name, err)
	}
	value, ok := secret.Data[ref.Key]
	if !ok || len(value) == 0 {
		return "", fmt.Errorf("secret %s/%s has no key %q", namespace, ref.Name, ref.Key)
	}
	return strings.TrimSpace(string(value)), nil
}

func hasRecord(ctx context.Context, api dnsAPI, domain, host, value string) (bool, error) {
	records, err := api.GetTXTRecords(ctx, domain)
	if err != nil {
		return false, fmt.Errorf("listing TXT records of %s: %w", domain, err)
	}
	for _, r := range records {
		if strings.EqualFold(r.Hostname, host) && strings.Trim(r.Text, `"`) == value {
			return true, nil
		}
	}
	return false, nil
}

// splitFQDN turns ("_acme-challenge.runboat.zverp.com.", "zverp.com.") into
// ("zverp.com", "_acme-challenge.runboat").
func splitFQDN(fqdn, zone string) (string, string, error) {
	f := strings.ToLower(strings.TrimSuffix(fqdn, "."))
	z := strings.ToLower(strings.TrimSuffix(zone, "."))
	if z == "" {
		return "", "", fmt.Errorf("empty resolved zone for %q", fqdn)
	}
	if f == z {
		return z, "", nil
	}
	if !strings.HasSuffix(f, "."+z) {
		return "", "", fmt.Errorf("fqdn %q is not inside zone %q", fqdn, zone)
	}
	return z, strings.TrimSuffix(f, "."+z), nil
}

func loadConfig(cfgJSON *extapi.JSON) (solverConfig, error) {
	cfg := solverConfig{}
	if cfgJSON == nil {
		return cfg, fmt.Errorf("missing webhook config: usernameSecretRef and passwordSecretRef are required")
	}
	if err := json.Unmarshal(cfgJSON.Raw, &cfg); err != nil {
		return cfg, fmt.Errorf("error decoding solver config: %v", err)
	}
	return cfg, nil
}
