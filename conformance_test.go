//go:build conformance

package main

import (
	"os"
	"testing"
	"time"

	acmetest "github.com/cert-manager/cert-manager/test/acme"
)

// TestRunsSuite runs cert-manager's DNS01 webhook conformance suite against
// the real Dinahosting API and real DNS. It creates and deletes TXT records
// named cert-manager-dns01-tests.<zone>.
//
// Built only with -tags conformance (cert-manager's test apiserver package
// panics at init without the envtest TEST_ASSET_* variables). It is skipped
// unless TEST_ZONE_NAME is set (e.g. "zverp.com."), and needs
// testdata/dinahosting/secret.yaml with real credentials (see
// secret.yaml.example) plus the envtest binaries (see `make test-conformance`).
func TestRunsSuite(t *testing.T) {
	zone := os.Getenv("TEST_ZONE_NAME")
	if zone == "" {
		t.Skip("TEST_ZONE_NAME not set; skipping conformance tests against the real Dinahosting API")
	}
	if _, err := os.Stat("testdata/dinahosting/secret.yaml"); err != nil {
		t.Fatalf("testdata/dinahosting/secret.yaml is required: %v", err)
	}

	fixture := acmetest.NewFixture(newSolver(),
		acmetest.SetResolvedZone(zone),
		acmetest.SetAllowAmbientCredentials(false),
		acmetest.SetManifestPath("testdata/dinahosting"),
		acmetest.SetStrict(true),
		// Dinahosting can take several minutes to publish zone changes.
		acmetest.SetPollInterval(15*time.Second),
		acmetest.SetPropagationLimit(15*time.Minute),
	)
	fixture.RunConformance(t)
}
