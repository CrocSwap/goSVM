.PHONY: build test verify bench build-tokens verify-tokens bench-tokens scale verify-scale protocol-prepare bench-protocol verify-protocol clean clean-deep

build:
	bash scripts/build.sh

test:
	@if [ "$$(uname -s)" = Darwin ]; then go test -ldflags=-linkmode=external ./...; else go test ./...; fi

verify:
	bash scripts/verify.sh

bench:
	python3 scripts/bench.py

build-tokens:
	bash scripts/build-tokens.sh

verify-tokens:
	bash scripts/verify.sh tokens

bench-tokens:
	python3 scripts/bench.py --tokens

scale:
	python3 scripts/scale.py

verify-scale:
	bash scripts/verify.sh scale

protocol-prepare:
	bash scripts/protocol_prepare.sh

bench-protocol: protocol-prepare
	python3 scripts/protocol_bench.py

verify-protocol:
	bash scripts/verify.sh protocol

clean:
	python3 scripts/clean_build.py --apply

clean-deep:
	python3 scripts/clean_build.py --deep --apply
