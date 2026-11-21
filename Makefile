# `make help` lists the targets.
BIN := reader
PREFIX ?= $(HOME)/.local
DESTDIR ?=

UNAME := $(shell uname -s)
OS := $(if $(filter Darwin,$(UNAME)),macos,$(if $(filter Linux,$(UNAME)),linux,windows))
only-on = @[ "$(OS)" = "$(1)" ] || { echo "$@ needs $(1) (this is $(OS))"; exit 1; }
EXE := $(if $(filter windows,$(OS)),.exe)

.DEFAULT_GOAL := help
.PHONY: help build run test fmt vet install uninstall clean \
        linux-install macos-install windows-install

help:
	@awk -F':.*## ' '/^[a-z0-9-]+:.*## /{printf "  %-16s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## build ./reader
	go build -o $(BIN)$(EXE)

run: build ## run it; DIR=~/Books or FILE=book.epub to start there
	./$(BIN)$(EXE) $(or $(FILE),$(DIR))

test: ## run the tests
	go test ./...

fmt: ## format the code
	gofmt -w .

vet: ## go vet
	go vet ./...

install: $(OS)-install ## build and install for this OS

linux-install: build ## install to $(PREFIX)/bin
	$(call only-on,linux)
	install -Dm755 $(BIN) $(DESTDIR)$(PREFIX)/bin/$(BIN)

macos-install: build ## install to $(PREFIX)/bin
	$(call only-on,macos)
	mkdir -p $(DESTDIR)$(PREFIX)/bin
	install -m755 $(BIN) $(DESTDIR)$(PREFIX)/bin/$(BIN)

windows-install: build ## install to $(PREFIX)/bin
	$(call only-on,windows)
	mkdir -p "$(DESTDIR)$(PREFIX)/bin"
	cp $(BIN)$(EXE) "$(DESTDIR)$(PREFIX)/bin/"

uninstall: ## remove the installed binary
	rm -f $(DESTDIR)$(PREFIX)/bin/$(BIN)$(EXE)

clean: ## remove the build
	rm -f $(BIN)$(EXE)
