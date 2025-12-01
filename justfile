export DOCKER_BUILDKIT := "1"

pwd := justfile_directory()

@list:
    just --list

test *args:
    go test -v -race ./... {{ args }} 

lint *args:
    golangci-lint run --timeout 10m0s ./... {{ args }}

inttest:
    just inttest/all

clean:
    @rm -rf ./out
