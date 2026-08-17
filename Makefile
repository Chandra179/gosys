.PHONY: build vet test dashboard

build:
	go build ./...

vet:
	go vet ./...

test:
	go test ./...

dashboard:
	go run ./cmd/dashboard -addr $(or $(ADDR),:8080)
