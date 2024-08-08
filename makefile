.PHONY: server

server: cmd/manyboards/server/*.go
	go build -o bin/manyboards-server $<
