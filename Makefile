# rain-net 构建与分发
.PHONY: build test vet fmt dist clean

BINARY_VERSION ?= dev
GOFILES := ./cmd/... ./internal/... ./pkg/... ./protocol/...

build:
	go build -o bin/raind ./cmd/raind
	go build -o bin/rainc ./cmd/rainc

test:
	go test $(GOFILES) ./test/... ./config/

vet:
	go vet $(GOFILES)

fmt:
	gofmt -w ./cmd ./internal ./pkg ./protocol ./test

# 交叉编译分发(单二进制,linux/amd64+arm64、darwin、windows)
dist: clean
	mkdir -p dist
	for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64; do \
		os=$${target%/*}; arch=$${target#*/}; \
		ext=$$([ "$$os" = "windows" ] && echo .exe); \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath \
			-ldflags "-s -w -X main.version=$(BINARY_VERSION)" \
			-o dist/raind-$$os-$$arch$$ext ./cmd/raind; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath \
			-ldflags "-s -w -X main.version=$(BINARY_VERSION)" \
			-o dist/rainc-$$os-$$arch$$ext ./cmd/rainc; \
	done
	@echo "dist/ 产物就绪"

clean:
	rm -rf bin dist

test-ci: vet
	go test ./...
