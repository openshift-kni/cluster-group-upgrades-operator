# Build the manager binary
FROM registry.hub.docker.com/library/golang:1.20 as builder

WORKDIR /workspace

# Copy the Go Modules manifests
COPY go.mod go.sum ./
COPY vendor/ vendor/

# Copy the go source
COPY main.go main.go
COPY pkg/api/ pkg/api/
COPY controllers/ controllers/
COPY pkg/aztp/ pkg/aztp/

# Build
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GO111MODULE=on go build -mod=vendor -a -o manager main.go
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GO111MODULE=on go build -mod=vendor -a -o aztp pkg/aztp/aztp.go

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static:nonroot

WORKDIR /

COPY --from=builder /workspace/manager /workspace/aztp .

USER 65532:65532

ENTRYPOINT ["/manager"]
