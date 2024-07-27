FROM golang:1.22 AS build
ARG TARGETOS
ARG TARGETARCH


WORKDIR /workspace
# Copy the Go Modules manifests
COPY . /workspace/
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o solver main.go

FROM alpine:3.20

RUN apk add --no-cache ca-certificates

COPY --from=build /workspace/solver /usr/local/bin/solver

ENTRYPOINT ["solver"]