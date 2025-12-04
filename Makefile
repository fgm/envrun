all: lint build

demo:
	LOCAL=demo go run . -f .env.demo env | sort

.PHONY: lint test build
lint:
	go tool staticcheck ./...

.PHONY: cover
cover:
	# This runs the benchmarks just once, as unit tests, for coverage reporting only.
	# It does not replace running "make bench".
	go test -v -race -run=. -coverprofile=coverage/cover.out -covermode=atomic ./...

.PHONY: test
test:
	# This includes the fuzz tests in unit test mode
	go test -race ./...

.PHONY: build
build: test
	go build ./...
