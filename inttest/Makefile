.PHONY: all inttest inttest-csrx

all: inttest 

inttest: inttest-csrx

inttest-csrx:
	docker-compose -f docker-compose.csrx.yml up --remove-orphans --exit-code-from inttest