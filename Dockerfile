FROM golang:1.26-alpine AS builder
WORKDIR /src

COPY go.mod go.sum* ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /bin/cobalt-dingo ./cmd/server

FROM gcr.io/distroless/static-debian12 AS runtime
COPY --from=builder /bin/cobalt-dingo /cobalt-dingo
ENTRYPOINT ["/cobalt-dingo"]
