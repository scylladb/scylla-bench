GOCQL_REPO?=github.com/scylladb/gocql
DOCKER_IMAGE_TAG?=scylla-bench

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
	go mod tidy;\
	}

build: _prepare_build_dir
	@echo "Building static scylla-bench"
	@CGO_ENABLED=0 go build -ldflags="-s -w" -o ./build/scylla-bench .

build-debug: _prepare_build_dir
	@echo "Building debug version of static scylla-bench"
	@CGO_ENABLED=0 go build -gcflags "all=-N -l" -o ./build/scylla-bench .

build-with-custom-gocql-version: _use-custom-gocql-version build

build-debug-with-custom-gocql-version: _use-custom-gocql-version build-debug

build-docker-image:
ifdef DOCKER_IMAGE_LABELS
	@echo 'Building docker image "${DOCKER_IMAGE_TAG}" with custom labels "${DOCKER_IMAGE_LABELS}"'
	@docker build -t ${DOCKER_IMAGE_TAG} --label "${DOCKER_IMAGE_LABELS}" -f ./Dockerfile build/
else
	@echo 'Building docker image "${DOCKER_IMAGE_TAG}"'
	@docker build -t ${DOCKER_IMAGE_TAG} -f ./Dockerfile build/
endif

build-sct-docker-image:
ifdef DOCKER_IMAGE_LABELS
	@echo 'Building sct docker image "${DOCKER_IMAGE_TAG}" with custom labels "${DOCKER_IMAGE_LABELS}"'
	@docker build -t ${DOCKER_IMAGE_TAG} --label "${DOCKER_IMAGE_LABELS}" -f ./Dockerfile.sct build/
else
	@echo 'Building sct docker image "${DOCKER_IMAGE_TAG}"'
	@docker build -t ${DOCKER_IMAGE_TAG} -f ./Dockerfile.sct build/
endif
