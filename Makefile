# Relaycheck Makefile
#
# 常用：
#   make            # 编译当前系统架构，输出 build/Relaycheck-<os>-<arch>
#   make all        # 编译全部平台（发布用）
#   make test       # go test ./...
#   make check      # gofmt 检查 + go vet + 测试
#   make clean      # 删除 build 下的产物
#
# 变量：
#   make VERSION=v2.0.2
#   make build/Relaycheck-linux-amd64   # 单独编译某个平台

MODULE     := github.com/qist/relaycheck
BIN        := Relaycheck
OUT_DIR    := build
VERSION    ?= $(shell cat config/version 2>/dev/null || echo latest)

LDFLAGS    := -s -w -X '$(MODULE)/config.Version=$(VERSION)'
BUILD_ENV  := CGO_ENABLED=0

# 源码或依赖变化都会触发重新编译（原 Makefile 没有依赖，改代码后仍提示 up to date）
SOURCES    := $(shell find . -name '*.go' -not -path './$(OUT_DIR)/*')
DEPS       := go.mod go.sum $(SOURCES)

HOST_OS    := $(shell go env GOOS)
HOST_ARCH  := $(shell go env GOARCH)
HOST_BIN   := $(OUT_DIR)/$(BIN)-$(HOST_OS)-$(HOST_ARCH)

TARGETS := \
	$(OUT_DIR)/$(BIN)-linux-amd64 \
	$(OUT_DIR)/$(BIN)-linux-arm64 \
	$(OUT_DIR)/$(BIN)-linux-armv7 \
	$(OUT_DIR)/$(BIN)-linux-386 \
	$(OUT_DIR)/$(BIN)-linux-ppc64 \
	$(OUT_DIR)/$(BIN)-linux-ppc64le \
	$(OUT_DIR)/$(BIN)-linux-s390x \
	$(OUT_DIR)/$(BIN)-windows-amd64.exe \
	$(OUT_DIR)/$(BIN)-windows-arm64.exe \
	$(OUT_DIR)/$(BIN)-windows-386.exe \
	$(OUT_DIR)/$(BIN)-darwin-amd64 \
	$(OUT_DIR)/$(BIN)-darwin-arm64 \
	$(OUT_DIR)/$(BIN)-android-arm64

.DEFAULT_GOAL := build
.PHONY: build all test vet fmt fmt-check check tidy clean help

# 用法：$(call gobuild,<goos>,<goarch>,<额外 env>)
define gobuild
	$(BUILD_ENV) GOOS=$(1) GOARCH=$(2) $(3) go build -trimpath -buildvcs=false \
		-ldflags "$(LDFLAGS)" -o $@ .
endef

$(OUT_DIR)/.dirstamp:
	@mkdir -p $(OUT_DIR)
	@touch $@

build: $(HOST_BIN)
	@echo "编译完成: $(HOST_BIN) (版本 $(VERSION))"

all: $(TARGETS)
	@echo "全部平台编译完成，版本 $(VERSION)，产物在 $(OUT_DIR)/"

$(OUT_DIR)/$(BIN)-linux-amd64: $(DEPS) | $(OUT_DIR)/.dirstamp
	$(call gobuild,linux,amd64,)
$(OUT_DIR)/$(BIN)-linux-arm64: $(DEPS) | $(OUT_DIR)/.dirstamp
	$(call gobuild,linux,arm64,)
$(OUT_DIR)/$(BIN)-linux-armv7: $(DEPS) | $(OUT_DIR)/.dirstamp
	$(call gobuild,linux,arm,GOARM=7)
$(OUT_DIR)/$(BIN)-linux-386: $(DEPS) | $(OUT_DIR)/.dirstamp
	$(call gobuild,linux,386,)
$(OUT_DIR)/$(BIN)-linux-ppc64: $(DEPS) | $(OUT_DIR)/.dirstamp
	$(call gobuild,linux,ppc64,)
$(OUT_DIR)/$(BIN)-linux-ppc64le: $(DEPS) | $(OUT_DIR)/.dirstamp
	$(call gobuild,linux,ppc64le,)
$(OUT_DIR)/$(BIN)-linux-s390x: $(DEPS) | $(OUT_DIR)/.dirstamp
	$(call gobuild,linux,s390x,)
$(OUT_DIR)/$(BIN)-windows-amd64.exe: $(DEPS) | $(OUT_DIR)/.dirstamp
	$(call gobuild,windows,amd64,)
$(OUT_DIR)/$(BIN)-windows-arm64.exe: $(DEPS) | $(OUT_DIR)/.dirstamp
	$(call gobuild,windows,arm64,)
$(OUT_DIR)/$(BIN)-windows-386.exe: $(DEPS) | $(OUT_DIR)/.dirstamp
	$(call gobuild,windows,386,)
$(OUT_DIR)/$(BIN)-darwin-amd64: $(DEPS) | $(OUT_DIR)/.dirstamp
	$(call gobuild,darwin,amd64,)
$(OUT_DIR)/$(BIN)-darwin-arm64: $(DEPS) | $(OUT_DIR)/.dirstamp
	$(call gobuild,darwin,arm64,)
$(OUT_DIR)/$(BIN)-android-arm64: $(DEPS) | $(OUT_DIR)/.dirstamp
	$(call gobuild,android,arm64,)

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w $(SOURCES)

fmt-check:
	@out="$$(gofmt -l $(SOURCES))"; \
	if [ -n "$$out" ]; then echo "需要 gofmt 的文件:"; echo "$$out"; exit 1; fi

check: fmt-check vet test

tidy:
	go mod tidy

clean:
	rm -rf $(OUT_DIR)

help:
	@echo "make            编译当前平台 ($(HOST_OS)/$(HOST_ARCH))"
	@echo "make all        编译全部平台"
	@echo "make test       go test ./..."
	@echo "make check      gofmt + vet + test"
	@echo "make clean      清理 $(OUT_DIR)/"
