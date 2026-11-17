# make install: build and install clindar (to $GOBIN, else $GOPATH/bin), then restart its
# daemon, if installed, so it runs the new version. After pulling changes: git pull && make install

BIN := $(or $(shell go env GOBIN),$(shell go env GOPATH)/bin)

.PHONY: install test

install:
	go install .
	"$(BIN)/clindar" daemon restart

test:
	go vet ./...
	go test -race ./...
