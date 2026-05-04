# Istio

ARCH ?= $(shell uname -m)
SAIL_VERSION ?= 1.27.0
ISTIO_NAMESPACE ?= istio-system
ISTIO_VERSION ?= 1.26.3

TIMEOUT ?= 900s

# istioctl tool
ISTIOCTL = bin/istioctl
ISTIO_SRC_DIR = /tmp/istio-src-$(ISTIO_VERSION)


ISTIO_MANIFEST ?= config/istio/istio.yaml

ifeq ($(ARCH),ppc64le)
ISTIO_MANIFEST := config/istio/istio.ppc64le.yaml
endif


$(ISTIOCTL):
	mkdir -p bin
	@if [ "$(ARCH)" = "ppc64le" ]; then \
		echo "Building istioctl $(ISTIO_VERSION) for $(ARCH)..."; \
          	rm -rf "$(ISTIO_SRC_DIR)"; \
        	git clone --depth 1 --branch "$(ISTIO_VERSION)" https://github.com/istio/istio.git "$(ISTIO_SRC_DIR)"; \
        	cd "$(ISTIO_SRC_DIR)" && \
            	GOOS=linux GOARCH=$(ARCH) CGO_ENABLED=0 go build -o "$(CURDIR)/$(ISTIOCTL)" ./istioctl/cmd/istioctl; \
        	rm -rf "$(ISTIO_SRC_DIR)"; \
   	 else \
        	echo "Downloading istioctl $(ISTIO_VERSION) for $(ARCH)..."; \
        	curl -sL https://istio.io/downloadIstio | ISTIO_VERSION=$(ISTIO_VERSION) TARGET_ARCH=$(ARCH) sh -; \
       		mv "istio-$(ISTIO_VERSION)/bin/istioctl" bin/; \
       		rm -rf "istio-$(ISTIO_VERSION)"; \
   	 fi

.PHONY: istioctl-impl
istioctl-impl: $(ISTIOCTL)
	@echo "istioctl installed at: $(ISTIOCTL)"
	@echo "Version: $$($(ISTIOCTL) version --remote=false)"

.PHONY: istio-install
istio-install: $(HELM) ## Install Istio using Sail operator
	$(HELM) upgrade --install sail-operator \
		--create-namespace \
        	--namespace $(ISTIO_NAMESPACE) \
        	--wait \
        	--timeout=$(TIMEOUT) \
        	https://github.com/istio-ecosystem/sail-operator/releases/download/$(SAIL_VERSION)/sail-operator-$(SAIL_VERSION).tgz
	kubectl apply -f $(ISTIO_MANIFEST)
	kubectl -n $(ISTIO_NAMESPACE) wait --for=condition=Ready istio/default --timeout=$(TIMEOUT)

.PHONY: istio-uninstall
istio-uninstall: $(HELM) ## Uninstall Istio and Sail operator
	- kubectl delete -f $(ISTIO_MANIFEST)
	$(HELM) uninstall sail-operator -n $(ISTIO_NAMESPACE)
	- kubectl delete namespace $(ISTIO_NAMESPACE)

