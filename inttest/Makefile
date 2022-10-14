.PHONY: inttest-csrx

all: inttest-csrx

inttest-csrx:
	docker network rm -f inttest_default
	docker-compose -f docker-compose.csrx.yml up --remove-orphans --exit-code-from inttest