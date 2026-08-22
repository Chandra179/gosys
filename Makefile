.PHONY: build vet test dashboard

build:
	go build ./...

vet:
	go vet ./...

test:
	go test ./...

dashboard:
	-fuser -k $(subst :,,$(or $(ADDR),:8083))/tcp
	go run ./cmd/dashboard -addr $(or $(ADDR),:8083)
