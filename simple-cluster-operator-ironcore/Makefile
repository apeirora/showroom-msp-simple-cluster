IMG ?= simple-cluster-operator-ironcore:dev

CONTROLLER_GEN ?= /home/marek-sap/SAP/workspace/showroom-msp-dati/operator/bin/controller-gen-v0.19.0

.PHONY: build
build:
	go build -o bin/manager cmd/main.go

.PHONY: fmt
fmt:
	go fmt ./...

.PHONY: vet
vet:
	go vet ./...

.PHONY: manifests
manifests:
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases output:rbac:artifacts:config=config/rbac

.PHONY: generate
generate:
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

.PHONY: docker-build
docker-build:
	docker build -t ${IMG} .

.PHONY: install
install:
	kubectl apply -f config/crd/bases/

.PHONY: uninstall
uninstall:
	kubectl delete --ignore-not-found -f config/crd/bases/

.PHONY: deploy
deploy:
	kubectl apply -k config/default

.PHONY: undeploy
undeploy:
	kubectl delete --ignore-not-found -k config/default
