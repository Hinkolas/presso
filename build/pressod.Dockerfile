# The engine image. Alpine (not scratch) on purpose so an install script can
# pipe a generated password into `pressod user create --password-stdin`
# through a shell. CGO is off: modernc.org/sqlite is pure Go.
FROM golang:1.26-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /pressod ./cmd/pressod

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=builder /pressod /usr/local/bin/pressod
ENV DATA_DIR=/data
VOLUME ["/data"]
EXPOSE 7080
ENTRYPOINT ["pressod"]
