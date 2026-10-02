export CGO_ENABLED := 0

GO ?= go
LDFLAGS := -w -s
# Platforms supported by the pure-Go SQLite dependency (modernc.org/sqlite).
PLATFORMS := darwin/amd64 darwin/arm64 \
	freebsd/386 freebsd/amd64 freebsd/arm freebsd/arm64 \
	linux/386 linux/amd64 linux/arm linux/arm64 linux/loong64 \
	linux/ppc64le linux/riscv64 linux/s390x \
	netbsd/amd64 openbsd/amd64 openbsd/arm64 \
	windows/386 windows/amd64 windows/arm64

.PHONY: build build-all test $(addprefix build-,$(PLATFORMS))

build:
	$(GO) build -ldflags="$(LDFLAGS)" -o .cache/bin/violet .

build-all: $(addprefix build-,$(PLATFORMS))

$(addprefix build-,$(PLATFORMS)): build-%:
	GOOS=$(word 1,$(subst /, ,$*)) GOARCH=$(word 2,$(subst /, ,$*)) $(GO) build -ldflags="$(LDFLAGS)" -o .cache/bin/violet-$(subst /,-,$*)$(if $(filter windows/%,$*),.exe) .

test:
	$(GO) test ./...
