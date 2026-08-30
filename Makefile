.PHONY: test cover check fmt

test:
	go test ./... -count=1

# -count=1 is required, not optional: cached test results do not re-emit
# coverage data, so a cached run reports a profile that is missing entries
# while still counting their statements. It reads ~15 points low.
cover:
	go test ./... -count=1 -coverpkg=./... -coverprofile=cover.out -covermode=count
	@go tool cover -func=cover.out | tail -1

fmt:
	gofmt -l .
	go vet ./...

check: fmt test
