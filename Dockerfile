FROM golang:1.26-alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /logging-file ./cmd/module

FROM gcr.io/distroless/static-debian12:nonroot
ENV LOG_FILE_PATH=/tmp/logging-file/module.log
COPY --from=builder /logging-file /
ENTRYPOINT ["/logging-file"]
