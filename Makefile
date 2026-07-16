.PHONY: build install tidy clean

BIN    := agentsb
PREFIX ?= /usr/local/bin

build: tidy
	go build -o $(BIN) .

# リポジトリ内のビルド成果へ PREFIX からシンボリックリンクする。
install: build
	ln -sfn "$(CURDIR)/$(BIN)" "$(PREFIX)/$(BIN)"

tidy:
	go mod tidy

clean:
	rm -f $(BIN)
