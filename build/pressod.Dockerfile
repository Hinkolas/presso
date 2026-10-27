# The engine image: one binary carrying the API, the workers and the
# embedded web UI. Alpine (not scratch) on purpose so an install script can
# pipe a generated password into `pressod user create --password-stdin`
# through a shell. CGO is off: modernc.org/sqlite is pure Go.
#
# The container needs the Docker socket (-v /var/run/docker.sock:/var/run/docker.sock)
# and, because it publishes the shared services to the host's loopback, the
# host network (--network host) so it can reach them the same way a host
# install does.
FROM node:22-alpine AS web
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ .
RUN npm run build

FROM golang:1.26.10-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /web/build ./internal/webui/dist
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -ldflags "-X github.com/Hinkolas/presso/internal/version.Version=${VERSION}" -o /pressod ./cmd/pressod

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=builder /pressod /usr/local/bin/pressod
COPY LICENSE NOTICE /usr/share/licenses/presso/
COPY docs/licensing.md /usr/share/licenses/presso/docs/licensing.md
ENV DATA_DIR=/data
# Tells the engine it cannot replace its own binary (self-update is refused;
# pull a newer image instead).
ENV PRESSO_IN_CONTAINER=1
VOLUME ["/data"]
EXPOSE 7080
ENTRYPOINT ["pressod"]
