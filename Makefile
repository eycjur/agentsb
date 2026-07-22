.PHONY: build install tidy clean

BIN    := agentsb
PREFIX ?= /usr/local/bin

build: tidy
	go build -o $(BIN) .

# PREFIX へ直接ビルドして配置する。
install: tidy
	go build -o "$(PREFIX)/$(BIN)" .

tidy:
	go mod tidy

clean:
	rm -f $(BIN)
