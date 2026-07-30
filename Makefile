IMAGE := nobuomiura/porthole
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")

TAILWIND_VERSION := 3.4.19

.PHONY: run build test cover fmt fmt-check vet lint tidy-check tailwind-check check check-all \
	tailwind tailwind-watch docker-build docker-push docker-up docker-down release

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

test:
	go test -race ./...

cover:
	go test -race -coverprofile=coverage.out -covermode=atomic ./...
	go tool cover -func=coverage.out | tail -1

# 整形を書き換える。検証だけしたい場合は fmt-check を使う。
fmt:
	gofmt -w .

# CI と同じく、整形されていなければ失敗する（書き換えない）。
fmt-check:
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "not gofmt'd:"; echo "$$unformatted"; exit 1; \
	fi

vet:
	go vet ./...

# 要 golangci-lint: https://golangci-lint.run/docs/welcome/install/local/
lint:
	golangci-lint run ./...

# go.mod / go.sum が tidy かを、書き換えずに確認する。
tidy-check:
	go mod tidy -diff

# web/tailwind.css がマークアップと同期しているかを確認する。要 Node。
tailwind-check: tailwind
	@if ! git diff --exit-code --quiet web/tailwind.css; then \
		echo "web/tailwind.css is stale. Commit the regenerated file."; \
		git diff --stat web/tailwind.css; exit 1; \
	fi

# Go 側の CI ゲートをローカルで再現する。
# CI にはこのほかに CSS 再生成差分（tailwind-check）と
# Docker ビルド + スモークテストのジョブがあり、check には含まれない。
check: fmt-check tidy-check build vet lint test

# CSS の同期確認まで含める。Docker ジョブは CI のみ。
check-all: check tailwind-check

# web/tailwind.css を再生成する。要 Node（実行時・Dockerビルド時には不要）。
# tailwind/input.css か web/ のマークアップを編集したら実行すること。
tailwind:
	npx --yes tailwindcss@$(TAILWIND_VERSION) \
		-c tailwind/tailwind.config.js \
		-i tailwind/input.css \
		-o web/tailwind.css \
		--minify

tailwind-watch:
	npx --yes tailwindcss@$(TAILWIND_VERSION) \
		-c tailwind/tailwind.config.js \
		-i tailwind/input.css \
		-o web/tailwind.css \
		--watch

# Create and push a release tag  (e.g. make release VERSION=v1.0.0)
release:
	@test -n "$(VERSION)" || (echo "VERSION is required. e.g. make release VERSION=v1.0.0" && exit 1)
	git tag $(VERSION)
	git push origin $(VERSION)
