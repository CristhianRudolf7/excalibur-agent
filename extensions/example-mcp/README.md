# example-mcp (route 1: MCP server)

A stdio MCP server with one pure tool, `cube_volume`. Its Excalibur
execution fields travel in `_meta` under the `excalibur/` key; fields
the server omits derive from the MCP annotations with the most
conservative value.

Build the server next to the manifest, then test:

```sh
go build -o server ./server
excalibur ext test .
```
