VERSION ?= dev
PYTHON ?= python3
DOCKER_PLATFORM ?=
.PHONY: build test vet run docker release
build:
	go build -trimpath -ldflags '-s -w -X main.version=$(VERSION)' -o bin/subconfig ./cmd/subconfig
test:
	go test -race ./...
vet:
	go vet ./...
run:
	go run ./cmd/subconfig $(ARGS)
docker:
	docker build $(if $(DOCKER_PLATFORM),--platform $(DOCKER_PLATFORM)) --build-arg VERSION=$(VERSION) -t subconfig:local .
release:
	$(PYTHON) scripts/package.py '$(VERSION)'
