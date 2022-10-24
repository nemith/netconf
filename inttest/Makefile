.ONESHELL:warning
.PHONY: all inttest inttest-csrx

.DEFAULT: 

all: csrx ceos

CSRX_IMAGE ?= "csrx:20.3R1.8"
csrx:
	CSRX_IMAGE=$(CSRX_IMAGE) docker-compose -f docker-compose.csrx.yml up \
		--build \
		--remove-orphans \
		--abort-on-container-exit \
		--exit-code-from inttest

CEOS_IMAGE ?= "ceos64-lab:4.28.3M"
ceos:
	CEOS_IMAGE=$(CEOS_IMAGE) docker-compose -f docker-compose.ceos.yml up \
		--build \
		--remove-orphans \
		--abort-on-container-exit \
		--exit-code-from inttest
