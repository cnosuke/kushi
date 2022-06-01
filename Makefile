NAME     := kushi
VERSION  := $(shell git describe --tags)
REVISION := $(shell git rev-parse --short HEAD)

SRCS    := $(shell find . -type f -name '*.go')
LDFLAGS := -ldflags="-s -w -X \"main.Name=$(NAME)\" -X \"main.Version=$(VERSION)\" -X \"main.Revision=$(REVISION)\" -extldflags \"-static\""

bin/$(NAME): $(SRCS)
	go build -a -tags netgo -installsuffix netgo $(LDFLAGS) -o bin/$(NAME)

.PHONY: clean
clean:
	rm -rf bin/* dist/*

.PHONY: cross-build
cross-build:
	for os in darwin linux; do \
		for arch in amd64 arm64; do \
			GOOS=$$os GOARCH=$$arch CGO_ENABLED=0 go build -a -tags netgo -installsuffix netgo $(LDFLAGS) -o dist/$$os-$$arch/$(NAME) \
			&& zip -j dist/kushi-$$os-$$arch.zip dist/$$os-$$arch/$(NAME); \
		done; \
	done

.PHONY: release-pack
release-pack: cross-build
	for os in darwin linux; do \
		for arch in amd64 386; do \
			zip -j dist/$(NAME)-$$os-$$arch.zip dist/$$os-$$arch/$(NAME); \
		done; \
	done

.PHONY: install
install: bin/$(NAME)
	cp bin/$(NAME) $$GOPATH/bin/$(NAME)
