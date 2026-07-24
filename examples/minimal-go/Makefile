BINARY := example-plugin_linux_amd64

.PHONY: test vet build manifest clean

test:
	go test ./...

vet:
	go vet ./...

build:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
		-buildvcs=false \
		-trimpath \
		-ldflags "-s -w -X main.version=$${VERSION:-1.0.0}" \
		-o dist/$(BINARY) .

manifest:
	go run . manifest --protocol=1

clean:
	rm -rf dist
