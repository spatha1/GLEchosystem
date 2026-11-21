// Entry point for the gl module's gRPC server. All plumbing — listening, opening the
// datastore, running migrations, the per-request transaction and the ModuleHandler
// contract — lives in github.com/mycelpf/mycel_module_sdk/server. This file only declares
// the module's identity, migrations and routes. The pure handler lives in ./logic.
package main

import (
	"log/slog"
	"os"

	"gl/ai"
	"gl/logic"

	modulesdk "github.com/mycelpf/mycel_module_sdk"
	"github.com/mycelpf/mycel_module_sdk/server"
)

const contractVersion = "0.2.0"

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, nil)))
	// AI drafting reads its provider settings from this file on each request; without it the studio says AI is not set up.
	logic.AIDrafter = func() ai.Drafter { return ai.Load(os.Getenv("GL_AI_SETTINGS")) }
	server.Serve(descriptor())
}

func descriptor() modulesdk.Descriptor {
	return modulesdk.Descriptor{
		Token:           logic.Token,
		ContractVersion: contractVersion,
		MenuGroup:       "GL",
		Nav:             []modulesdk.NavEntry{{Label: "Configuration", Path: "/configs"}},
		Migrations:      logic.Migrations(),
		Route:           logic.Route,
		OpenAPI:         []byte(logic.OpenAPIFragment),
	}
}
