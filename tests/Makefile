.PHONY: test test-e2e test-coverage clean

test:
	go test ./... -v

test-e2e:
	go test ./tests/e2e/... -v -tags=e2e

test-coverage:
	go test ./... -coverprofile=coverage.out
	go tool cover -html=coverage.out -o coverage.html

test-race:
	go test ./... -race

clean:
	rm -f coverage.out coverage.html