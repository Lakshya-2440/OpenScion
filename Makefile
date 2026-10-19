.PHONY: all build install server demo clean

all: build install

build:
	@mkdir -p bin
	go build -ldflags="-s -w" -o bin/scion ./cmd/scion
	go build -ldflags="-s -w" -o bin/aether-server ./cmd/aether-server
	go build -ldflags="-s -w" -o bin/scnd ./cmd/scnd
	@echo "\033[1;32m✔ Built OpenScion binaries into ./bin\033[0m"

install: build
	@mkdir -p ~/.local/bin
	@cp bin/scion ~/.local/bin/scion
	@cp bin/scion ~/.local/bin/scn
	@echo "\033[1;32m✔ Installed 'scion' to ~/.local/bin/scion (available globally in your terminal!)\033[0m"

server:
	./bin/aether-server --port 8080 --data-dir ./data/server

demo:
	./scripts/demo.sh

clean:
	rm -rf bin/ dist/ data/ /tmp/openscion-demo /tmp/test-scion-terminal
