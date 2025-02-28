# Runtime image is used to overlay clusterserviceversion.yaml for Konflux
ARG RUNTIME_IMAGE=registry.access.redhat.com/ubi9-minimal:9.4

# By default, do not do the konflux overlay
ARG KONFLUX=false

# Run the overlay in a container
FROM ${RUNTIME_IMAGE} AS overlay

# Set KONFLUX to env from args
# It will checked by the overlay script
ENV KONFLUX=${KONFLUX}

# Copy manifests into the container
COPY bundle/manifests /manifests/

# Check if this is a Konflux build to overlay the clusterserviceversion
COPY konflux_clusterserviceversion_overlay.sh /
COPY konflux_clusterserviceversion_overlay.data /
RUN /konflux_clusterserviceversion_overlay.sh

FROM scratch

# Core bundle labels.
LABEL operators.operatorframework.io.bundle.mediatype.v1=registry+v1
LABEL operators.operatorframework.io.bundle.manifests.v1=manifests/
LABEL operators.operatorframework.io.bundle.metadata.v1=metadata/
LABEL operators.operatorframework.io.bundle.package.v1=cluster-group-upgrades-operator
LABEL operators.operatorframework.io.bundle.channels.v1=alpha
LABEL operators.operatorframework.io.metrics.builder=operator-sdk-v1.28.0-ocp
LABEL operators.operatorframework.io.metrics.mediatype.v1=metrics+v1
LABEL operators.operatorframework.io.metrics.project_layout=go.kubebuilder.io/v3

# Labels for testing.
LABEL operators.operatorframework.io.test.mediatype.v1=scorecard+v1
LABEL operators.operatorframework.io.test.config.v1=tests/scorecard/

# Copy files to locations specified by labels.
COPY --from=overlay /manifests /manifests/
COPY bundle/metadata /metadata/
COPY bundle/tests/scorecard /tests/scorecard/
