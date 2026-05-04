# MetalLB

METALLB_VERSION = v0.15.2

.PHONY: metallb-install
metallb-install: $(YQ) # Install MetalLB load balancer
	kubectl apply -f https://raw.githubusercontent.com/metallb/metallb/$(METALLB_VERSION)/config/manifests/metallb-native.yaml
	kubectl -n metallb-system create secret generic memberlist --from-literal=secretkey="$$(openssl rand -base64 128)" --dry-run=client -o yaml | kubectl apply -f -
	kubectl -n metallb-system wait --for=condition=Available deployments controller --timeout=300s
	kubectl -n metallb-system wait --for=condition=ready pod --selector=app=metallb --timeout=120s
	./utils/docker-network-ipaddresspool.sh kind $(YQ) | kubectl apply -n metallb-system -f -

.PHONY: metallb-uninstall
metallb-uninstall: # Uninstall MetalLB
	kubectl delete -f https://raw.githubusercontent.com/metallb/metallb/$(METALLB_VERSION)/config/manifests/metallb-native.yaml
