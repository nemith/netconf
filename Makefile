.PHONY: lint
lint:
	docker run -v $(shell pwd):/src:ro -w /src golangci/golangci-lint:v1.50 golangci-lint run


GO_PACKAGES := . ./transport/...
.PHONY: test 
test:
	docker run -v $(shell pwd):/src:ro -w /src -e CGO_ENABLED=0 golang:1.19-alpine  go test -v ${GO_PACKAGES} 

.PHONY: inttest
inttest:
	$(MAKE) -C inttest all 