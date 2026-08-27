BINARY := bin/server

.PHONY: run build test race lint fmt docker deploy-check tidy

run:
	go run ./cmd/server

build:
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o $(BINARY) ./cmd/server

test:
	go test ./...

race:
	go test -race ./...

cover:
	go test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out | tail -1

lint:
	gofmt -l . | tee /dev/stderr | (! read)
	go vet ./...

fmt:
	gofmt -w .

tidy:
	go mod tidy

docker:
	docker build -t linkedin-profile-api . && docker run --rm -p 8000:8000 --env-file .env linkedin-profile-api

deploy-check:
	@git check-ignore -q .env && echo ".env is ignored (good)" || echo "DANGER: .env is NOT ignored"
