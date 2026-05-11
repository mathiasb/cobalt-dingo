FROM golang:1.25-alpine AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux \
    go build -trimpath -ldflags="-s -w" -o /bin/coo-agent-server ./cmd/server

FROM gcr.io/distroless/static-debian12 AS runtime
COPY --from=builder /bin/coo-agent-server /coo-agent-server
EXPOSE 8080
ENTRYPOINT ["/coo-agent-server"]
