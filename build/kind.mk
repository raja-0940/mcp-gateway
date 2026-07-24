# Kind

ARCH ?= $(shell uname -m)
KIND_CLUSTER_NAME ?= mcp-gateway
CONTAINER_ENGINE ?= podman

# node image for CI clusters; the baked CI node image overrides this on a
# bake hit. defaults to the pinned KIND_NODE_IMAGE so it always matches the
# bin/kind the load targets use.
KIND_CLUSTER_IMAGE ?= $(KIND_NODE_IMAGE)

ISTIO_CUSTOM_HUB ?= quay.io/raja0940/istio-release
ISTIO_CUSTOM_TAG ?= 1.26.3-ppc64le

.PHONY: kind-load-custom-istio-images
kind-load-custom-istio-images: ## Load custom ppc64le Istio pilot/proxyv2 images into Kind
	@if [ "$$(uname -m)" = "ppc64le" ]; then \
        echo "Loading custom Istio images into Kind cluster $(KIND_CLUSTER_NAME)..."; \
        tmpdir=$$(mktemp -d); \
        echo "Saving $(ISTIO_CUSTOM_HUB)/pilot:$(ISTIO_CUSTOM_TAG)..."; \
        $(CONTAINER_ENGINE) save -o $$tmpdir/pilot.tar $(ISTIO_CUSTOM_HUB)/pilot:$(ISTIO_CUSTOM_TAG); \
        KIND_EXPERIMENTAL_PROVIDER=$(KIND_EXPERIMENTAL_PROVIDER) ./bin/kind load image-archive $$tmpdir/pilot.tar --name $(KIND_CLUSTER_NAME); \
        echo "Saving $(ISTIO_CUSTOM_HUB)/proxyv2:$(ISTIO_CUSTOM_TAG)..."; \
        $(CONTAINER_ENGINE) save -o $$tmpdir/proxyv2.tar $(ISTIO_CUSTOM_HUB)/proxyv2:$(ISTIO_CUSTOM_TAG); \
        KIND_EXPERIMENTAL_PROVIDER=$(KIND_EXPERIMENTAL_PROVIDER) ./bin/kind load image-archive $$tmpdir/proxyv2.tar --name $(KIND_CLUSTER_NAME); \
        rm -rf $$tmpdir; \
        echo "[OK] Custom Istio images loaded into Kind."; \
    else \
        echo "[INFO] Non-ppc64le architecture detected. Skipping custom Istio image load."; \
    fi

# CI cluster creation uses the same pinned kind binary and node image as the
# make load targets. creating with the runner-preinstalled kind broke when
# its default node image moved to a containerd config version the pinned
# bin/kind cannot load into (kindest/node v1.36.1 ships containerd config
# version 4; kind v0.29.0 supports 2 and 3).
.PHONY: kind-create-cluster-ci
kind-create-cluster-ci: kind # Create the CI kind cluster with the pinned kind binary and node image
	$(KIND) create cluster --name $(KIND_CLUSTER_NAME) --config config/kind/cluster-ci.yaml --image "$(KIND_CLUSTER_IMAGE)"
# Match the Kubernetes version your repo expects for kind
KIND_K8S_VERSION ?= v1.33.1

# Default upstream kind node image name for supported arches
KIND_NODE_IMAGE ?= kindest/node:$(KIND_K8S_VERSION)

# Custom node image name for ppc64le
ifeq ($(ARCH),ppc64le)
	KIND_NODE_IMAGE_PPC64LE := quay.io/powercloud/kind-node:$(KIND_K8S_VERSION)
endif

.PHONY: kind-create-cluster
kind-create-cluster: kind ## Create the "mcp-gateway" kind cluster.
	@./utils/generate-placeholder-ca.sh
	@# Set KIND provider for podman
	@if echo "$(CONTAINER_ENGINE)" | grep -q "podman"; then \
		export KIND_EXPERIMENTAL_PROVIDER=podman; \
	fi; \
	if $(KIND) get clusters | grep -q "^$(KIND_CLUSTER_NAME)$$"; then \
		echo "Kind cluster '$(KIND_CLUSTER_NAME)' already exists, skipping creation"; \
	else \
		echo "Creating Kind cluster '$(KIND_CLUSTER_NAME)' with MCP_GATEWAY port $(KIND_HOST_PORT_MCP_GATEWAY) and KEYCLOAK port $(KIND_HOST_PORT_KEYCLOAK)..."; \
		cat config/kind/cluster.yaml | sed \
			-e 's/hostPort: 8001/hostPort: $(KIND_HOST_PORT_MCP_GATEWAY)/' \
			-e 's/hostPort: 8002/hostPort: $(KIND_HOST_PORT_KEYCLOAK)/' | \
		$(KIND) create cluster --name $(KIND_CLUSTER_NAME) --image "$$KIND_NODE_IMAGE" --config -; \
	fi	
	@"$(MAKE)" -s -f build/kind.mk kind-load-custom-istio-images

.PHONY: kind-delete-cluster
kind-delete-cluster: kind # Delete the "mcp-gateway" kind cluster.
	@# Set KIND provider for podman
	@if echo "$(CONTAINER_ENGINE)" | grep -q "podman"; then \
		export KIND_EXPERIMENTAL_PROVIDER=podman; \
	fi; \
	$(KIND) delete cluster --name $(KIND_CLUSTER_NAME)
