FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download || true
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/build-service ./cmd/build-service

# Final image needs docker CLI (to push to registry) and nixpacks.
FROM alpine:3.19
RUN apk add --no-cache ca-certificates curl tar zstd docker-cli git bash \
 && curl -sSL https://nixpacks.com/install.sh | bash
COPY --from=build /out/build-service /build-service
ENTRYPOINT ["/build-service"]
