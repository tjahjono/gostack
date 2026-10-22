.PHONY: fmt vet test race integration build run migrate-up docker-up docker-test docker-down

fmt:
	go fmt ./...
vet:
	go vet ./...
test:
	go test ./...
race:
	go test -race ./...
integration:
	GOSTACK_REQUIRE_INTEGRATION=true go test -race -count=1 ./...
build:
	go build -trimpath -o bin/gostack-example ./examples/minimal
run:
	go run ./examples/minimal serve
migrate-up:
	go run ./examples/minimal migrate up
docker-up:
	docker compose up --build -d api
docker-test:
	docker compose run --build --rm tests
docker-down:
	docker compose down
