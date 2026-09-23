# deno-worker-template-go

- A template for starting a Deno / Deno Deploy project with Go.
- This template uses [`workers-go`](https://github.com/syumai/workers-go) package to run an HTTP server on `Deno.serve`.

## Usage

- `main.go` includes simple HTTP server implementation. Feel free to edit this code and implement your own HTTP server.

## Requirements

- Deno
- Go 1.24.0 or later
- `deployctl` (only for deployment to Deno Deploy)

## Getting Started

- Initialize a project.

```console
cd my-app
go mod init
go mod tidy
deno task build # build Go Wasm binary and generate JS assets
deno task start # start running dev server
curl http://localhost:8000/hello # outputs "Hello!"
```

## Development

### Commands

```
deno task dev     # run dev server
deno task build   # build Go Wasm binary
deno task deploy  # deploy to Deno Deploy (requires deployctl)
```

### Testing dev server

- Just send HTTP request using some tools like curl.

```
$ curl http://localhost:8000/hello
Hello!
```

```
$ curl -X POST -d "test message" http://localhost:8000/echo
test message
```
