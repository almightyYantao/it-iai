FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download || true
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/control-plane ./cmd/control-plane

FROM alpine:3.19
RUN apk add --no-cache ca-certificates curl
COPY --from=build /out/control-plane /control-plane
COPY migrations /migrations
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=3s CMD curl -fsS http://localhost:8080/healthz || exit 1
ENTRYPOINT ["/control-plane"]
