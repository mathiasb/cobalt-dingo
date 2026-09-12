FROM golang:1.26-alpine AS builder
WORKDIR /src

COPY go.mod go.sum* ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /bin/cobalt-dingo ./cmd/server
# The migration runner ships too, so migrations run as a k8s Job against the
# secret the cluster already holds. The alternative is an operator decrypting
# the production DSN onto a laptop to run them by hand, which is both a
# credential-handling step and unrepeatable.
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /bin/migrate ./cmd/migrate

FROM gcr.io/distroless/static-debian12 AS runtime
COPY --from=builder /bin/cobalt-dingo /cobalt-dingo
COPY --from=builder /bin/migrate /migrate
# golang-migrate reads the .sql files at runtime, so they have to be in the
# image. MIGRATIONS_DIR points the runner at this path.
COPY --from=builder /src/migrations /migrations
ENTRYPOINT ["/cobalt-dingo"]
