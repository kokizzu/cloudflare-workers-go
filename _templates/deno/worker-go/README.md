# deno-worker-template-go

- A template for starting a Deno / Deno Deploy project with Go.
- This template uses [`workers-go`](https://github.com/syumai/workers-go) package to run an HTTP server on `Deno.serve`.

## Usage

- `main.go` includes simple HTTP server implementation. Feel free to edit this code and implement your own HTTP server.

## Requirements

- Deno 2.4.2 or later (required for `deno deploy`)
- Go 1.24.0 or later

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
deno task deploy  # deploy to Deno Deploy (runs `deno deploy`)
```

### Deploying to Deno Deploy

- Replace `"org": "<TBD>"` in `deno.json` with your Deno Deploy organization slug (or leave it as-is and let `deno deploy` prompt you).
- Then run:

```console
deno task build
deno task deploy
```

- On the first run, `deno deploy` interactively lets you select or create an app and saves `deploy.app` back into `deno.json`.
- Use `deno deploy --prod` to deploy to production.

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
