GO	= go

.PHONY: all build gen test clean

all:	test

gen:
	$(RM) go.mod go.sum
	$(GO) mod init github.com/ricardobranco777/httpseek
	$(GO) mod tidy

test:
	$(GO) test -v ./...
	$(GO) vet ./...
	staticcheck ./...
	gofmt -s -l .

clean:
	$(GO) clean -a
