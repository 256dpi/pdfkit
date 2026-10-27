all:
	go fmt .
	go vet .
	golint .

ALPINE ?= 3.19

test-linux:
	docker run --rm -v "$(CURDIR)":/src -v pdfkit-go:/go/pkg/mod -w /src golang:alpine$(ALPINE) sh -c "apk add --no-cache chromium >/dev/null && chromium --version && go test -count=1 -test.noSandbox ./..."
