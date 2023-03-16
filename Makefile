

default:
	go mod tidy
	gofmt -d -l -e -w .
	go build .
