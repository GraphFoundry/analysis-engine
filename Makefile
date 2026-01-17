.PHONY: build run test test-parity docker-build docker-run clean

BINARY_NAME=predictive-analysis-engine
DOCKER_IMAGE=predictive-analysis-engine-go
PORT=5000

build:
	go build -o $(BINARY_NAME) ./cmd/server

run: build
	./$(BINARY_NAME)

test:
	go test -v ./pkg/... ./cmd/...

test-parity: build
	# Ensure the binary is built as 'predictive-analysis-engine' because the test runner might expect it 
	# or the test runner starts it using 'go run'.
	# Let's check runner_test.go. It builds "server_bin".
	go test -v ./tests/parity/...

docker-build:
	docker build -t $(DOCKER_IMAGE) .

docker-run:
	docker run -p $(PORT):$(PORT) --env-file .env --name $(DOCKER_IMAGE) --rm $(DOCKER_IMAGE)

clean:
	go clean
	rm -f $(BINARY_NAME) server_bin
