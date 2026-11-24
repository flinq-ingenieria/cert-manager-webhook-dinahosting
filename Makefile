GO ?= go
IMAGE ?= ghcr.io/flinq-ingenieria/cert-manager-webhook-dinahosting
TAG ?= dev
CHART := deploy/cert-manager-webhook-dinahosting

# Kubernetes version of the envtest binaries (kube-apiserver, etcd) used by
# the conformance suite.
ENVTEST_K8S_VERSION ?= 1.33.0
LOCALBIN ?= $(shell pwd)/bin
ENVTEST ?= $(LOCALBIN)/setup-envtest
ENVTEST_VERSION ?= release-0.21

# Docker-based variants for machines without a Go toolchain: every $(GO)
# call runs in a golang container. The source is mounted at the same path so
# that envtest asset paths are valid both inside and outside the container.
GO_IMAGE ?= golang:1.26-alpine
DOCKER_GOPATH ?= $(HOME)/.cache/go-docker
DOCKER_GO = docker run --rm -u $$(id -u):$$(id -g) -e HOME=/tmp -e GOPATH=/tmp/go \
	-e GOCACHE=/tmp/go/cache -e GOFLAGS=-modcacherw \
	-e TEST_ZONE_NAME -e TEST_ASSET_ETCD -e TEST_ASSET_KUBE_APISERVER -e TEST_ASSET_KUBECTL \
	-v $(DOCKER_GOPATH):/tmp/go -v $(CURDIR):$(CURDIR) -w $(CURDIR) $(GO_IMAGE) go

.PHONY: test
test: ## Unit tests (no network, no credentials).
	$(GO) vet ./...
	$(GO) vet -tags conformance ./...
	$(GO) test -count=1 -cover ./...

.PHONY: test-conformance
test-conformance: setup-envtest ## Conformance suite against the real Dinahosting API. Needs TEST_ZONE_NAME and testdata/dinahosting/secret.yaml.
	@test -n "$(TEST_ZONE_NAME)" || { echo "TEST_ZONE_NAME is required, e.g. TEST_ZONE_NAME=zverp.com."; exit 1; }
	ASSETS=$$("$(ENVTEST)" use $(ENVTEST_K8S_VERSION) --bin-dir "$(LOCALBIN)" -p path) && \
	TEST_ASSET_ETCD=$$ASSETS/etcd \
	TEST_ASSET_KUBE_APISERVER=$$ASSETS/kube-apiserver \
	TEST_ASSET_KUBECTL=$$ASSETS/kubectl \
	$(GO) test -tags conformance -count=1 -v -run TestRunsSuite -timeout 40m .

.PHONY: docker-test docker-test-conformance
docker-test:
	mkdir -p $(DOCKER_GOPATH)
	$(MAKE) test GO="$(DOCKER_GO)"
docker-test-conformance:
	mkdir -p $(DOCKER_GOPATH)
	$(MAKE) test-conformance GO="$(DOCKER_GO)"

.PHONY: setup-envtest
setup-envtest: $(ENVTEST)
$(ENVTEST):
	mkdir -p $(LOCALBIN)
	GOBIN=$(LOCALBIN) $(GO) install sigs.k8s.io/controller-runtime/tools/setup-envtest@$(ENVTEST_VERSION)

.PHONY: image
image:
	docker build -t $(IMAGE):$(TAG) .

.PHONY: chart-lint chart-template
chart-lint:
	helm lint $(CHART)
chart-template:
	helm template dinahosting $(CHART) --namespace cert-manager

.PHONY: clean
clean:
	chmod -R u+w $(LOCALBIN) 2>/dev/null || true
	rm -rf $(LOCALBIN) _out
