.PHONY: build vet test analyze dashboard

build:
	go build ./...

vet:
	go vet ./...

test:
	go test ./...

# usage: make analyze PPROF=heap.pprof REPO=./path TOP=20
analyze:
	go run ./cmd/analyze -pprof $(PPROF) -repo $(REPO) -top $(or $(TOP),10)

dashboard:
	go run ./cmd/dashboard -addr $(or $(ADDR),:8080)
