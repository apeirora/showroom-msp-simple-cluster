IMG ?= ubuntu-machines:dev

.PHONY: build
build:
	go build -o bin/manager cmd/main.go

.PHONY: fmt
fmt:
	go fmt ./...

.PHONY: vet
vet:
	go vet ./...

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
