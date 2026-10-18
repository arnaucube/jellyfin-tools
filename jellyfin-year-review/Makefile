.PHONY: check build test e2e
check:
	test -z "$$(gofmt -l cmd internal scripts)"
	go vet ./...
	go test -race ./...
	python3 -m unittest discover -s scripts -p 'test_*.py'
	npm --prefix web run check
build:
	npm --prefix web run build
	go build -trimpath -o review ./cmd/review
test:
	go test ./...
e2e: build
	npm --prefix web run test:e2e
	REEL_E2E_AUTH_MODE=offline npm --prefix web run test:e2e
