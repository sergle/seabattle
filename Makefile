.PHONY: build test clean

build:
	go build -o seabattle .

test:
	go test ./... -race

clean:
	rm -f seabattle
