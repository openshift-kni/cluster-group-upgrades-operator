# Builder image requires golang to compile
ARG BUILDER_IMAGE=quay.io/projectquay/golang:1.19

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
ARG RUNTIME_IMAGE=gcr.io/distroless/static:nonroot

# Build the manager binary
FROM ${BUILDER_IMAGE} AS builder

# Default Konflux to false
ARG KONFLUX="false"

# Asssume x86 unless otherwise specified
ARG GOARCH="amd64"

WORKDIR /workspace

# Bring in the go dependencies before anything else so we can take
# advantage of caching these layers in future builds.
COPY go.mod go.sum ./
COPY vendor/ vendor/

# Copy the go source
COPY main.go main.go
COPY api/ api/
COPY controllers/ controllers/

# Fips is not required or supported on releases this old
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${GOARCH} GO111MODULE=on go build -mod=vendor -a -o manager main.go

# Create the runtime image
FROM ${RUNTIME_IMAGE}

WORKDIR /

COPY --from=builder /workspace/manager .

USER 65532:65532

ENTRYPOINT ["/manager"]
