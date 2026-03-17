# KubeWA Operator Makefile
SHELL := /bin/bash

# Version info
VERSION ?= 0.1.0
IMG ?= ghcr.io/takumi-software/kubewa-operator:$(VERSION)
PLATFORMS ?= linux/arm64,linux/amd64

# Go
GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)

# Tool paths
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
KUSTOMIZE ?= $(shell which kustomize)
HELMIFY ?= $(LOCALBIN)/helmify

## Tool versions
CONTROLLER_TOOLS_VERSION ?= v0.17.2

.PHONY: all
all: build

##@ General

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: test
test: fmt vet ## Run unit tests.
	go test ./... -v -count=1

.PHONY: test-coverage
test-coverage: ## Run tests with coverage report.
	go test ./... -coverprofile=cover.out -covermode=atomic
	go tool cover -html=cover.out -o cover.html
	@echo "Coverage report: cover.html"

##@ Build

.PHONY: build
build: generate fmt vet ## Build the operator binary.
	go build -o bin/manager ./cmd/main.go

.PHONY: run
run: generate fmt vet ## Run the operator locally (requires kubeconfig).
	go run ./cmd/main.go

##@ Code generation

.PHONY: generate
generate: controller-gen ## Generate code (deepcopy, RBAC markers, etc.).
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

.PHONY: manifests
manifests: controller-gen ## Generate CRD and RBAC manifests.
	$(CONTROLLER_GEN) crd:generateEmbeddedObjectMeta=true paths="./api/..." output:crd:artifacts:config=config/crd/bases
	$(CONTROLLER_GEN) rbac:roleName=kubewa-operator-manager-role paths="./..." output:rbac:artifacts:config=config/rbac
	cp config/crd/bases/*.yaml helm/kubewa-operator/templates/

##@ Deployment

.PHONY: install
install: manifests ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | kubectl apply -f -

.PHONY: uninstall
uninstall: manifests ## Uninstall CRDs from the K8s cluster.
	$(KUSTOMIZE) build config/crd | kubectl delete --ignore-not-found=true -f -

.PHONY: deploy
deploy: manifests ## Deploy controller to the K8s cluster.
	cd config/manager && $(KUSTOMIZE) edit set image controller=$(IMG)
	$(KUSTOMIZE) build config/default | kubectl apply -f -

.PHONY: undeploy
undeploy: ## Undeploy controller from the K8s cluster.
	$(KUSTOMIZE) build config/default | kubectl delete --ignore-not-found=true -f -

##@ Helm

.PHONY: helm-install
helm-install: ## Install via Helm (set TWILIO_ACCOUNT_SID, TWILIO_AUTH_TOKEN, TWILIO_FROM_NUMBER).
	helm upgrade --install kube-wa-operator ./helm/kubewa-operator \
		--namespace kubewa-system \
		--create-namespace \
		--set twilio.accountSID=$(TWILIO_ACCOUNT_SID) \
		--set twilio.authToken=$(TWILIO_AUTH_TOKEN) \
		--set twilio.fromNumber=$(TWILIO_FROM_NUMBER) \
		--set webhook.publicURL=$(WEBHOOK_PUBLIC_URL)

.PHONY: helm-uninstall
helm-uninstall: ## Uninstall via Helm.
	helm uninstall kube-wa-operator --namespace kubewa-system

.PHONY: helm-lint
helm-lint: ## Lint the Helm chart.
	helm lint ./helm/kubewa-operator

##@ Container image

.PHONY: docker-build
docker-build: ## Build the Docker image.
	docker build -t $(IMG) .

.PHONY: docker-push
docker-push: ## Push the Docker image.
	docker push $(IMG)

.PHONY: docker-buildx
docker-buildx: ## Build and push multi-arch Docker image.
	docker buildx build --platform=$(PLATFORMS) --push -t $(IMG) .

##@ Tools

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary.
$(CONTROLLER_GEN): $(LOCALBIN)
	test -s $(LOCALBIN)/controller-gen || GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_TOOLS_VERSION)
