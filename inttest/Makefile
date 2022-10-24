.ONESHELL:warning
.PHONY: all inttest inttest-csrx

.DEFAULT: inttest 

inttest: inttest-csrx


# This should warn and exit possibly?
check-docker-image = \
	$(if $(shell docker image ls -q $1),, \
	    $(error docker image $1 is missing))


CSRX_IMAGE ?= "csrx:20.3R1.8"
inttest-csrx:
	$(call check-docker-image,$(CSRX_IMAGE))
	CSRX_IMAGE=$(CSRX_IMAGE) docker-compose -f docker-compose.csrx.yml up \
		--build \
		--remove-orphans \
		--abort-on-container-exit \
		--exit-code-from inttest