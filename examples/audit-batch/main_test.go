package main

import (
	"strings"
	"testing"
)

func TestPrivateInputIsBoundedExactAndSingleDocument(t *testing.T) {
	valid := `{"access_token":"private-test-input","tenant_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","kind":"management"}`
	if _, e := readInput(strings.NewReader(valid)); e != nil {
		t.Fatal("valid private input refused")
	}
	for _, body := range []string{`{}`, `[]`, valid + valid, strings.Replace(valid, `"kind":"management"`, `"kind":"unknown"`, 1), strings.Replace(valid, `"kind":"management"`, `"kind":"management","kind":"machine"`, 1), strings.Replace(valid, `"kind":"management"`, `"kind":"management","Cookie":"private"`, 1), strings.Replace(valid, `"kind":"management"`, `"kind":"management","limit":null`, 1), strings.Repeat(" ", 32769)} {
		if _, e := readInput(strings.NewReader(body)); e == nil {
			t.Fatal("ambiguous or unbounded private input accepted")
		}
	}
}
