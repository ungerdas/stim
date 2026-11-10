VERSION ?= dev

.PHONY: build web go test dev-api dev-web docker

# Full build: web UI first, then the binary that embeds it.
build: web go

web:
	cd web && npm ci && npm run build

go:
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o bin/stim ./cmd/stim

test:
	go vet ./cmd/... ./internal/...
	go test ./internal/...

# Development: run these two in separate terminals, then open http://localhost:5173
dev-api:
	go run ./cmd/stim

dev-web:
	cd web && npm install && npm run dev

docker:
	docker build --build-arg VERSION=$(VERSION) -t stim:$(VERSION) .
