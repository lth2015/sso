# syntax=docker/dockerfile:1
# Includes system CA certificates for HTTPS; no shell or package manager.
FROM gcr.io/distroless/static-debian12:nonroot
ARG BINARY
COPY --chmod=0555 .local/${BINARY} /app
USER 65532:65532
ENTRYPOINT ["/app"]
