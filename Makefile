GOCQL_REPO ?= github.com/scylladb/gocql
DOCKER_IMAGE_TAG ?= scylla-bench
GOOS ?= $(shell uname | tr '[:upper:]' '[:lower:]')
GOARCH ?= $(shell go env GOARCH)

VERSION ?= $(shell git describe --tags 2>/dev/null || echo "dev")
COMMIT ?= $(shell git rev-parse HEAD 2>/dev/null || echo "unknown")
BUILD_DATE ?= $(shell git log -1 --format=%cd --date=format:%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS_VERSION := -X github.com/scylladb/scylla-bench/internal/version.version=$(VERSION) -X github.com/scylladb/scylla-bench/internal/version.commit=$(COMMIT) -X github.com/scylladb/scylla-bench/internal/version.date=$(BUILD_DATE)

_prepare_build_dir:
	@mkdir build >/dev/null 2>&1 || true

_use-custom-gocql-version:
	@{\
  	if [ -z "${GOCQL_VERSION}" ]; then \
  	  echo "GOCQL_VERSION is not set";\
  	  exit 1;\
  	fi;\
  	echo "Using custom gocql commit \"${GOCQL_VERSION}\"";\
	go mod edit -replace "github.com/gocql/gocql=${GOCQL_REPO}@${GOCQL_VERSION}";\
	go mod tidy \
	}

build: _prepare_build_dir
	@echo "Building static scylla-bench"
	@CGO_ENABLED=0 go build -ldflags="-s -w $(LDFLAGS_VERSION)" -o ./build/scylla-bench .

build-debug: _prepare_build_dir
	@echo "Building debug version of static scylla-bench"
	@CGO_ENABLED=0 go build -gcflags "all=-N -l" -ldflags="$(LDFLAGS_VERSION)" -o ./build/scylla-bench .

.PHONY: build-with-custom-gocql-version
build-with-custom-gocql-version: _use-custom-gocql-version build

.PHONY: build-debug-with-custom-gocql-version
build-debug-with-custom-gocql-version: _use-custom-gocql-version build-debug

.PHONY: build-docker-image
build-docker-image:
ifdef DOCKER_IMAGE_LABELS
	@echo 'Building docker image "${DOCKER_IMAGE_TAG}" with custom labels "${DOCKER_IMAGE_LABELS}"'
	@docker build --target production -t ${DOCKER_IMAGE_TAG} --label "${DOCKER_IMAGE_LABELS}" .
else
	@echo 'Building docker image "${DOCKER_IMAGE_TAG}"'
	@docker build --target production -t ${DOCKER_IMAGE_TAG} .
endif

.PHONY: build-sct-docker-image
build-sct-docker-image: build
ifdef DOCKER_IMAGE_LABELS
	@echo 'Building sct docker image "${DOCKER_IMAGE_TAG}" with custom labels "${DOCKER_IMAGE_LABELS}"'
	@docker build --target production-sct -t ${DOCKER_IMAGE_TAG} --label "${DOCKER_IMAGE_LABELS}" -f ./Dockerfile build/
else
	@echo 'Building sct docker image "${DOCKER_IMAGE_TAG}"'
	@docker build --target production-sct -t ${DOCKER_IMAGE_TAG} -f ./Dockerfile build/
endif

.PHONY: fmt
fmt:
	@go tool golangci-lint fmt

.PHONY: test
test:
	@go test -covermode=atomic -race -coverprofile=coverage.txt -timeout 5m -json -v ./... 2>&1 | go tool gotestfmt -showteststatus

.PHONY: check
check:
	@go tool golangci-lint run

.PHONY: fieldalign
fieldalign:
	@go tool fieldalignment -fix github.com/scylladb/scylla-bench
	@go tool fieldalignment -fix github.com/scylladb/scylla-bench/internal/version
	@go tool fieldalignment -fix github.com/scylladb/scylla-bench/random
	@go tool fieldalignment -fix github.com/scylladb/scylla-bench/pkg/workloads
	@go tool fieldalignment -fix github.com/scylladb/scylla-bench/pkg/results

.PHONY: clean-bin
clean-bin:
	@rm -rf build

.PHONY: clean-results
clean-results:
	@rm -rf coverage.txt
	@rm -rf dist

.PHONY: clean
clean: clean-bin clean-results
