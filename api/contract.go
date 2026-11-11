// Package api embeds the external API contract served by the platform service.
package api

import _ "embed"

//go:embed openapi.yaml
var PublicSpec []byte

//go:embed openapi.json
var PublicJSON []byte

//go:embed console.openapi.yaml
var ConsoleSpec []byte

//go:embed security.openapi.yaml
var SecuritySpec []byte

//go:embed machine.openapi.yaml
var MachineSpec []byte

//go:embed machine.openapi.json
var MachineJSON []byte

//go:embed machine-resource-example.openapi.yaml
var MachineResourceExampleSpec []byte

//go:embed machine-resource-example.openapi.json
var MachineResourceExampleJSON []byte
