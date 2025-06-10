# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM golang:1.24 AS builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace

ADD go.mod .
ADD go.sum .
RUN go mod download

ADD . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -ldflags="-w -s" .

FROM --platform=$BUILDPLATFORM gcr.io/distroless/static-debian12:latest

COPY --from=builder /workspace/prometheus-trusted-advisor-exporter .

CMD ["/prometheus-trusted-advisor-exporter"]
