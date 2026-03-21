IMAGE := nobuomiura/porthole
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")

.PHONY: run build docker-build docker-push docker-up docker-down lint release

run:
	go run .

build:
	CGO_ENABLED=0 go build -ldflags="-s -w -X main.version=$(VERSION)" -o porthole .

docker-build:
	docker buildx build \
		--platform linux/amd64,linux/arm64 \
		--build-arg VERSION=$(VERSION) \
		-f docker/Dockerfile \
		-t $(IMAGE):$(VERSION) \
		-t $(IMAGE):latest \
		.

docker-push:
	docker buildx build \
		--platform linux/amd64,linux/arm64 \
		--build-arg VERSION=$(VERSION) \
		-f docker/Dockerfile \
		-t $(IMAGE):$(VERSION) \
		-t $(IMAGE):latest \
		--push \
		.

docker-up:
	docker compose up --build

docker-down:
	docker compose down

lint:
	go vet ./...

# Create and push a release tag  (e.g. make release VERSION=v1.0.0)
release:
	@test -n "$(VERSION)" || (echo "VERSION is required. e.g. make release VERSION=v1.0.0" && exit 1)
	git tag $(VERSION)
	git push origin $(VERSION)
