.PHONY: all build server scn scnd web demo test deploy-render deploy-vercel clean

all: build web-build

build:
	@mkdir -p bin
	go build -ldflags="-s -w" -o bin/aether-server ./cmd/aether-server
	go build -ldflags="-s -w" -o bin/scn ./cmd/scn
	go build -ldflags="-s -w" -o bin/scnd ./cmd/scnd
	@echo "\033[1;32m✔ Built all binaries into ./bin\033[0m"

server:
	./bin/aether-server --port 8080 --data-dir ./data/server

web:
	cd web && npm run dev

web-build:
	cd web && npm run build

demo:
	./scripts/demo.sh

deploy-render:
	./scripts/deploy-render.sh

deploy-vercel:
	./scripts/deploy-vercel.sh

clean:
	rm -rf bin/ dist/ data/ /tmp/aether-demo web/dist
