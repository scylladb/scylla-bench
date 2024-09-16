# Makefile

# Default variables
DOCKERFILE_PATH ?= Dockerfile
SCYLLA_BENCH_VERSION ?= tags/v0.1.22
GOCQL_REPO ?= github.com/scylladb/gocql

build-image-with-custom-gocql-commit:
	@echo "Replacing gocql with commit ID ${GOCQL_COMMIT_ID}"
	go mod edit -replace github.com/gocql/gocql=${GOCQL_REPO}@${GOCQL_COMMIT_ID}
	go mod tidy

	@echo "Building Docker image with tag ${IMAGE_TAG}"
	docker build -f ${DOCKERFILE_PATH} . -t ${IMAGE_TAG} --build-arg version=${SCYLLA_BENCH_VERSION}
