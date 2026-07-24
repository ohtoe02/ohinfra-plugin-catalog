.PHONY: test validate materialize

test:
	go test -race ./...

validate:
	go run ./cmd/catalogctl validate --plugins plugins

materialize:
	go run ./cmd/catalogctl materialize --plugins plugins --output verification
