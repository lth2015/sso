// Local single-batch CLI. Private JSON arrives on stdin, never argv/env/files.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/lth2015/sso/pkg/useraudit"
)

var errInput = errors.New("invalid_input")

type input struct {
	AccessToken string
	Tenant      string
	RoleID      string
	Kind        string
	Limit       int
	After       string
}

func readInput(reader io.Reader) (v input, err error) {
	body, e := io.ReadAll(io.LimitReader(reader, 32769))
	if e != nil || len(body) > 32768 {
		return v, errInput
	}
	d := json.NewDecoder(bytes.NewReader(body))
	start, e := d.Token()
	if e != nil || start != json.Delim('{') {
		return v, errInput
	}
	fields := map[string]any{"access_token": &v.AccessToken, "tenant_id": &v.Tenant, "kind": &v.Kind, "role_id": &v.RoleID, "limit": &v.Limit, "after": &v.After}
	seen := map[string]bool{}
	for d.More() {
		key, e := d.Token()
		name, ok := key.(string)
		if e != nil || !ok || seen[name] || fields[name] == nil {
			return input{}, errInput
		}
		seen[name] = true
		var raw json.RawMessage
		if d.Decode(&raw) != nil || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, fields[name]) != nil {
			return input{}, errInput
		}
	}
	end, e := d.Token()
	if e != nil || end != json.Delim('}') || !seen["access_token"] || !seen["tenant_id"] || !seen["kind"] || v.AccessToken == "" || v.Tenant == "" || (v.Kind != "management" && v.Kind != "machine" && v.Kind != "policy_history") {
		return input{}, errInput
	}
	if (v.Kind == "policy_history" && (!seen["role_id"] || v.RoleID == "")) || (v.Kind != "policy_history" && seen["role_id"]) {
		return input{}, errInput
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return input{}, errInput
	}
	return v, nil
}
func category(err error) string {
	switch {
	case errors.Is(err, errInput), errors.Is(err, useraudit.ErrInvalidRequest):
		return "invalid_input"
	case errors.Is(err, useraudit.ErrUnauthenticated):
		return "unauthenticated"
	case errors.Is(err, useraudit.ErrForbidden):
		return "forbidden"
	case errors.Is(err, useraudit.ErrNotFound):
		return "not_found"
	case errors.Is(err, useraudit.ErrInvalidResponse):
		return "invalid_response"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	default:
		return "unavailable"
	}
}
func run(ctx context.Context, reader io.Reader, writer io.Writer) error {
	v, err := readInput(reader)
	if err != nil {
		return err
	}
	client, err := useraudit.New(useraudit.Config{Origin: "http://api.localhost:8083", AllowLocalHTTP: true})
	if err != nil {
		return err
	}
	page := useraudit.Page{Limit: v.Limit, After: v.After}
	var result any
	if v.Kind == "management" {
		result, err = client.Management(ctx, v.AccessToken, v.Tenant, page)
	} else if v.Kind == "policy_history" {
		result, err = client.PolicyHistory(ctx, v.AccessToken, v.Tenant, v.RoleID, page)
	} else {
		result, err = client.Machine(ctx, v.AccessToken, v.Tenant, page)
	}
	if err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return json.NewEncoder(writer).Encode(result)
}
func main() {
	if len(os.Args) != 1 {
		os.Stderr.WriteString("audit batch failed: invalid_input\n")
		os.Exit(1)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, os.Stdin, os.Stdout); err != nil {
		os.Stderr.WriteString("audit batch failed: " + category(err) + "\n")
		os.Exit(1)
	}
}
