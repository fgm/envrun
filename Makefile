# Stays on Make, no value in moving to Just.
#
# A public repo also pays twice: contributors need a new tool, and CI,
# which pins every action to a SHA, gains unpinned supply-chain surface.

all: lint test build

BINDIR   := bin
COVERDIR := coverage
GO       := go
PACKAGES := ./...

# If _RAW_GOBIN is empty, take the first element in _RAW_GOPATH
# Replace Windows/UNIX directory separators to create a "make" list.
# Then take the first word and append /bin
_RAW_GOBIN  := $(shell $(GO) env GOBIN)
_RAW_GOPATH := $(shell $(GO) env GOPATH)
GOBIN := $(if $(_RAW_GOBIN),$(_RAW_GOBIN),$(firstword $(subst :, ,$(subst ;, ,$(_RAW_GOPATH))))/bin)

# The demo file is generated from the fixtures the tests assert against,
# so the two cannot drift: what the demo shows is what the suite pins down.
# Globbing rather than listing them means a new fixture needs no edit here,
# and the test suite checks that every fixture is named for the outcome it expects.
DEMO_ENV := .env.demo

# Only the demo's own variables are shown: "env" prints the whole environment,
# so an unfiltered demo would leak whatever secrets the caller happens to export.
demo: $(DEMO_ENV)
	@sed -nE 's/^[[:space:]]*([^#=[:space:]][^=]*[^=[:space:]]|[^#=[:space:]])[[:space:]]*=.*/^\1=/p' $(DEMO_ENV) > $(DEMO_ENV).names
	@LOCAL=demo $(GO) run . -f $(DEMO_ENV) env | grep -f $(DEMO_ENV).names | sort
	@rm -f $(DEMO_ENV).names

$(DEMO_ENV): $(wildcard env/testdata/pass-*.env)
	cat $^ > $@

.PHONY: all build clean cover demo install lint modernize test

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
	$(GO) test -v -race -coverprofile=$(COVERDIR)/cover.out -covermode=atomic $(PACKAGES)
	$(GO) tool cover -html=$(COVERDIR)/cover.out

test:
	# This includes the fuzz tests in unit test mode
	$(GO) test -race $(PACKAGES)

# go build discards its result when handed more than one package,
# so $(PACKAGES) would check that the command builds without ever producing it.
build:
	$(GO) build -o $(BINDIR)/ .

install: test build
	$(GO) install .

clean:
	@echo GOBIN: $(GOBIN)
	rm -fr $(COVERDIR) $(DEMO_ENV) $(BINDIR)/envrun $(GOBIN)/envrun
