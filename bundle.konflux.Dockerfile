# Runtime image is used to overlay clusterserviceversion.yaml for Konflux

# yq is required for merging the yaml files
# Run the overlay in a container
ARG YQ_IMAGE=quay.io/konflux-ci/yq:latest
FROM ${YQ_IMAGE} AS overlay

# Set work dir
WORKDIR /tmp

# Copy manifests into the container
COPY --chown=yq:yq bundle/manifests /tmp/manifests

# Check if this is a Konflux build to overlay the clusterserviceversion
COPY konflux_clusterserviceversion_overlay.sh .
COPY konflux_clusterserviceversion_overlay.data .
RUN /tmp/konflux_clusterserviceversion_overlay.sh

# From here downwards this should mostly match the non-konflux bundle, i.e., `bundle.Dockerfile`
# However there are a few exceptions:
# 1. The label 'operators.operatorframework.io.bundle.channels.v1'
# 2. The label 'operators.operatorframework.io.bundle.channels.default.v1'
# 3. The copy of the manifests (copy from the overlay instead of from the git repo)
FROM scratch

# Core bundle labels.
LABEL operators.operatorframework.io.bundle.mediatype.v1=registry+v1
LABEL operators.operatorframework.io.bundle.manifests.v1=manifests/
LABEL operators.operatorframework.io.bundle.metadata.v1=metadata/
LABEL operators.operatorframework.io.bundle.package.v1=cluster-group-upgrades-operator
LABEL operators.operatorframework.io.bundle.channels.v1=stable,4.19
LABEL operators.operatorframework.io.bundle.channels.default.v1=stable
LABEL operators.operatorframework.io.metrics.builder=operator-sdk-v1.28.0-ocp
LABEL operators.operatorframework.io.metrics.mediatype.v1=metrics+v1
LABEL operators.operatorframework.io.metrics.project_layout=go.kubebuilder.io/v3

# Labels for testing.
LABEL operators.operatorframework.io.test.mediatype.v1=scorecard+v1
LABEL operators.operatorframework.io.test.config.v1=tests/scorecard/

# Copy files to locations specified by labels.
COPY --from=overlay /tmp/manifests /manifests/
COPY bundle/metadata /metadata/
COPY bundle/tests/scorecard /tests/scorecard/
