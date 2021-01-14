# compile to wasm
build:
	@ GOOS=js GOARCH=wasm go build -o main.wasm
	@ echo build complete

# build then run local file server, visit localhost:8080
run: build
	@ go run cmd/fileserver/main.go;

# rebuild on changes to main.go
reflex-build:
	@ reflex -g 'main.go' $(MAKE) build

# can't use tinygo with encoding/xml and net/http
# https://tinygo.org/lang-support/stdlib/
# could refactor & just parse from <puzzle> to </puzzle> in a raw string...
# and use browser's fetch
build-tiny:
	@ tinygo build -o tiny.wasm ./main.go
