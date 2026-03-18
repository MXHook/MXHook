.PHONY: build test lint dev docker clean

build:
	CGO_ENABLED=0 go build -o mxhook .

test:
	go test ./...

lint:
	golangci-lint run
	gofumpt -l -d .

dev:
	docker compose up --build

docker:
	docker build -t mxhook .

clean:
	rm -f mxhook
	docker compose down -v
