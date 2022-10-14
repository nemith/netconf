.PHONY: all inttest inttest-csrx

all: inttest 

inttest: inttest-csrx

inttest-csrx:
	docker network rm -f inttest_default
	docker-compose -f docker-compose.csrx.yml up --remove-orphans --exit-code-from inttest