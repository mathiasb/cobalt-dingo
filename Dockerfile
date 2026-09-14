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
# fortnox-check ships for the same reason as migrate: it reads the token the
# cluster already holds, so checking a live connection needs no credential on a
# laptop. It is read-only — every client it builds passes readOnly=true — and
# it is the only way to inspect a connection made through the web UI, whose
# token never touches a file.
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /bin/fortnox-check ./cmd/fortnox-check
# overview ships for the same reason again, plus one of its own: the voucher
# cache it reads and fills lives in the cluster's postgres, so running it
# anywhere else would either fill a throwaway cache or need the production DSN
# on a laptop. Read-only — every client is read-only and the voucher source
# takes no readOnly flag to get wrong.
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /bin/overview ./cmd/overview
# fortnox-shape reports what the live API actually returns versus what our
# structs read (#92). It needs the stored token, so it ships with the rest.
# Read-only, and prints field names and types only — never values.
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /bin/fortnox-shape ./cmd/fortnox-shape

FROM gcr.io/distroless/static-debian12 AS runtime
COPY --from=builder /bin/cobalt-dingo /cobalt-dingo
COPY --from=builder /bin/migrate /migrate
COPY --from=builder /bin/fortnox-check /fortnox-check
COPY --from=builder /bin/overview /overview
COPY --from=builder /bin/fortnox-shape /fortnox-shape
# golang-migrate reads the .sql files at runtime, so they have to be in the
# image. MIGRATIONS_DIR points the runner at this path.
COPY --from=builder /src/migrations /migrations
ENTRYPOINT ["/cobalt-dingo"]
