# Build the manager binary
FROM quay.io/projectquay/golang:1.23 as builder

WORKDIR /workspace

# Copy the Go Modules manifests
COPY go.mod go.sum ./
COPY vendor/ vendor/

# Copy the go source
COPY main.go main.go
COPY pkg/api/ pkg/api/
COPY controllers/ controllers/

# Build
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GO111MODULE=on go build -mod=vendor -a -o manager main.go

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static:nonroot

WORKDIR /

COPY --from=builder /workspace/manager .

USER 65532:65532

ENTRYPOINT ["/manager"]
