# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM golang:1.24 AS builder

WORKDIR /workspace

ADD go.mod .
ADD go.sum .
RUN go mod download

ADD . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -ldflags="-w -s" .

FROM gcr.io/distroless/static-debian11:latest

COPY --from=builder /workspace/prometheus-trusted-advisor-exporter .

CMD ["/prometheus-trusted-advisor-exporter"]
