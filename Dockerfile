FROM golang:1.26-alpine@sha256:ecb67f987aa1a57794c91d19ade7ae0209f2f0f5496aaef9e6ae85ddf92d02ab AS build

WORKDIR /workspace
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/webhook .

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab

LABEL org.opencontainers.image.source="https://github.com/flinq-ingenieria/cert-manager-webhook-dinahosting" \
      org.opencontainers.image.description="cert-manager DNS01 webhook solver for Dinahosting" \
      org.opencontainers.image.licenses="Apache-2.0"

COPY --from=build /out/webhook /usr/local/bin/webhook
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/webhook"]
