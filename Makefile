# Stays on Make, no value in moving to Just.
#
# A public repo also pays twice: contributors need a new tool, and CI,
# which pins every action to a SHA, gains unpinned supply-chain surface.

all: lint build

GO       := go
COVERDIR := coverage
PACKAGES := ./...

# If _RAW_GOBIN is empty, take the first element in _RAW_GOPATH
# Replace Windows/UNIX directory separators to create a "make" list.
# Then take the first word and append /bin
_RAW_GOBIN  := $(shell $(GO) env GOBIN)
_RAW_GOPATH := $(shell $(GO) env GOPATH)
GOBIN := $(if $(_RAW_GOBIN),$(_RAW_GOBIN),$(firstword $(subst :, ,$(subst ;, ,$(_RAW_GOPATH))))/bin)

demo:
	LOCAL=demo $(GO) run . -f .env.demo env | sort

.PHONY: build clean cover install lint modernize test

lint: modernize
	$(GO) tool staticcheck -checks=all $(PACKAGES)

# go fix exits non-zero when fixes are pending, and prints them as a patch,
# so a failure here already says what to apply.
modernize:
	$(GO) fix -diff $(PACKAGES)

cover:
	mkdir -p $(COVERDIR)
	# This runs the benchmarks just once, as unit tests, for coverage reporting only.
	# It does not replace running "make bench".
	$(GO) test -v -race -run=. -coverprofile=$(COVERDIR)/cover.out -covermode=atomic $(PACKAGES)
	$(GO) tool cover -html=$(COVERDIR)/cover.out

test:
	# This includes the fuzz tests in unit test mode
	$(GO) test -race $(PACKAGES)

build: test
	$(GO) build $(PACKAGES)

install: build
	$(GO) install .

clean:
	@echo GOBIN: $(GOBIN)
	rm -fr $(COVERDIR) envrun $(GOBIN)/envrun
