# Build a single static binary; the final image contains nothing else.
# Go cross-compiles, so a multi-arch build needs no emulation.
FROM --platform=$BUILDPLATFORM golang:1.26 AS build
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath \
    -ldflags="-s -w -X main.version=$VERSION" -o /prepkit ./cmd/prepkit
RUN mkdir /data

FROM scratch
COPY --from=build /prepkit /prepkit
COPY --from=build --chown=65532:65532 /data /data

# Run unprivileged. A named volume inherits /data's ownership; a bind-mounted
# host directory must be chowned to 65532 first.
USER 65532:65532

# Keep the working directory writable: a scratch image has no /tmp, and /
# belongs to root.
WORKDIR /data
VOLUME /data
EXPOSE 8214
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD ["/prepkit", "healthcheck"]

# Inside a container, 0.0.0.0 is required for port publishing to work, so
# -allow-public-bind is set here and the startup warning will print. Actual
# exposure is controlled by which interface you publish the port on:
#   docker run -p 127.0.0.1:8214:8214 -e PREPKIT_ADMIN_SECRET=... prepkit
ENTRYPOINT ["/prepkit"]
CMD ["serve", "-listen", "0.0.0.0:8214", "-allow-public-bind", "-data-dir", "/data"]
