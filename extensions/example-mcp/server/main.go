// Command server is the route-1 example extension (Section 14.12):
// a stdio MCP server whose Excalibur execution fields travel in
// _meta under the "excalibur/" key. Build it next to the manifest
// (go build -o server ./server) so the loader dials ./server.
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type volumeArgs struct {
	Size float64 `json:"size" jsonschema:"the edge length of the cube"`
}

type volumeOut struct {
	Volume float64 `json:"volume"`
}

func main() {
	server := mcp.NewServer(&mcp.Implementation{Name: "example-mcp", Version: "0.1.0"}, nil)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "cube_volume",
		Description: "Compute the volume of a cube from its edge length",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true},
		Meta: mcp.Meta{
			"excalibur/": map[string]any{
				"effect":      "read_only",
				"idempotency": "pure",
				"cancel":      "immediate",
				"group":       "shapes",
				"tags":        []string{"shapes", "math"},
				"keywords":    []string{"cube", "volume", "geometry"},
			},
		},
	}, func(_ context.Context, _ *mcp.CallToolRequest, args volumeArgs) (*mcp.CallToolResult, volumeOut, error) {
		volume := args.Size * args.Size * args.Size
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("volume %g", volume)}},
		}, volumeOut{Volume: volume}, nil
	})
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}
