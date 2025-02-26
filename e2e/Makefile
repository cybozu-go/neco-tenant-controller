KUBERNETES_VERSION = v1.31.6 # renovate: kindest/node

KIND_CONFIG = kind-config.yaml
export KUBECONFIG

.PHONY: help
help:
	@echo "Choose one of the following target"
	@echo
	@echo "start      Start kind cluster and install accurate"
	@echo "test       Run e2e tests"
	@echo "logs       Save logs as logs.tar.gz"
	@echo "stop       Stop the kind cluster"

.PHONY: start
start:
	kind create cluster --name=cattage --config=$(KIND_CONFIG) --image=kindest/node:$(KUBERNETES_VERSION) --wait 1m
	cd ..; CGO_ENABLED=0 go build -o cattage-controller ./cmd/cattage-controller/main.go
	cd ..; docker build --no-cache -t cattage:dev .
	kind load docker-image cattage:dev --name=cattage
	$(MAKE) prepare
	kubectl apply -k ../config/default
	kubectl -n cattage wait --for=condition=available --timeout=180s --all deployments

.PHONY: prepare
prepare:
	# Setup cert-manager
	kubectl apply -f https://github.com/jetstack/cert-manager/releases/latest/download/cert-manager.yaml
	kubectl -n cert-manager wait --for=condition=available --timeout=180s --all deployments

	# Setup stakater/Reloader
	helm repo add stakater https://stakater.github.io/stakater-charts
	helm repo update
	helm install --create-namespace --namespace reloader reloader -f manifests/reloader-values.yaml stakater/reloader
	kubectl -n reloader wait --for=condition=available --timeout=180s --all deployments

	# Setup Argo CD
	kubectl create namespace argocd
	kustomize build --enable-helm ./manifests | kubectl apply -f -
	kubectl -n argocd wait --for=condition=available --timeout=180s --all deployments

	# Setup accurate
	helm repo add accurate https://cybozu-go.github.io/accurate
	helm repo update
	helm install --create-namespace --namespace accurate accurate -f manifests/accurate-values.yaml accurate/accurate
	kubectl -n accurate wait --for=condition=available --timeout=180s --all deployments

.PHONY: generate-second-controller
generate-second-controller:
	kustomize build --enable-helm ./manifests | yq ea '. as $$i ireduce ([]; . + $$i) | .[] | select(.kind=="StatefulSet") | .metadata.name="second-application-controller"' > ./manifests/second-application-controller.yaml

.PHONY: test
test:
	env RUN_E2E=1 \
		go test -v -race . -ginkgo.progress -ginkgo.v -ginkgo.failFast

.PHONY: logs
logs:
	rm -rf logs.tar.gz logs
	kind export logs --name=cattage ./logs
	tar czf logs.tar.gz logs
	rm -rf logs

.PHONY: stop
stop:
	kind delete cluster --name=cattage
	-docker image rm cattage:dev
	-docker image prune -f
