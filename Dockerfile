FROM golang:1.26-alpine AS builder
COPY core/ /build/core/
COPY logging-file/ /build/logging-file/
WORKDIR /build/logging-file
RUN go mod download
RUN CGO_ENABLED=0 go build -o /logging-file ./cmd/module
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /logging-file /
ENTRYPOINT ["/logging-file"]
