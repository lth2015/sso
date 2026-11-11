// Local acceptance helper: secret input only on stdin; fixed authority, no
// command-line credentials and only a controlled result on stdout.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/lth2015/sso/pkg/machineidentity"
	"io"
	"os"
	"time"
)

func main() {
	result := map[string]any{"ok": false, "category": "invalid_input"}
	defer func() { json.NewEncoder(os.Stdout).Encode(result) }()
	var input struct {
		Tenant      string `json:"tenant"`
		Application string `json:"application"`
		Token       string `json:"token"`
	}
	decoder := json.NewDecoder(io.LimitReader(os.Stdin, 32769))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil {
		return
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return
	}
	verifier, err := machineidentity.New(machineidentity.Config{Origin: "http://api.localhost:8083", AllowLocalHTTP: true})
	if err != nil {
		result["category"] = "invalid_configuration"
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	identity, err := verifier.Verify(ctx, input.Tenant, input.Application, input.Token)
	if err == nil {
		result = map[string]any{"ok": true, "identity": identity}
		return
	}
	switch {
	case errors.Is(err, machineidentity.ErrUnauthenticated):
		result["category"] = "unauthenticated"
	case errors.Is(err, machineidentity.ErrForbidden):
		result["category"] = "forbidden"
	case errors.Is(err, machineidentity.ErrInvalidRequest):
		result["category"] = "invalid_request"
	case errors.Is(err, machineidentity.ErrInvalidResponse):
		result["category"] = "authority_response_invalid"
	default:
		result["category"] = "unavailable"
	}
}
