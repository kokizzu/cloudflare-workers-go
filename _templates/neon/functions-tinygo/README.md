# functions-template-tinygo

- A template for starting a Neon Functions project with TinyGo.
- This template uses the [`workers-go`](https://github.com/syumai/workers-go) package to run an HTTP server.

## Usage

- `main.go` includes a simple HTTP server implementation. Feel free to edit this code and implement your own HTTP server.

## Requirements

- Node.js
- TinyGo 0.42.0 or later

## Getting Started

- Create a new Functions project using this template.

```console
npx degit github:syumai/workers-go/_templates/neon/functions-tinygo my-app
```

- Initialize a project.

```console
cd my-app # A directory of the project created by the above command
go mod init
go mod tidy
npm install
npm run dev # start running dev server
curl http://localhost:8787/hello # outputs "Hello!"
```

## Development

### Commands

```
npm run dev    # run dev server
npm run build  # build Go Wasm binary
npx neon auth  # authenticate with Neon
npx neon link --project-name <PROJECT_NAME> --region-id <REGION_ID> # create and link a Neon project
npm run deploy # deploy function
```

### Testing dev server

- Just send HTTP requests using some tools like curl.

```
$ curl http://localhost:8787/hello
Hello!
```

```
$ curl -X POST -d "test message" http://localhost:8787/echo
test message
```
