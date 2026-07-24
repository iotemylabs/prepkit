# Build a single static binary; the final image contains nothing else.
FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /prepkit ./cmd/prepkit

FROM scratch
COPY --from=build /prepkit /prepkit
VOLUME /data
EXPOSE 8214

# Inside a container, 0.0.0.0 is required for port publishing to work, so
# -allow-public-bind is set here and the startup warning will print. Actual
# exposure is controlled by which interface you publish the port on:
#   docker run -p 127.0.0.1:8214:8214 -e PREPKIT_ADMIN_SECRET=... prepkit
ENTRYPOINT ["/prepkit"]
CMD ["serve", "-listen", "0.0.0.0:8214", "-allow-public-bind", "-data-dir", "/data"]
