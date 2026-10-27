package mcpbridge

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestValidateBaseURL(t *testing.T){
	for _,raw:=range []string{"http://mcp-server:8091","https://tools.example.org"}{if err:=ValidateBaseURL(raw);err!=nil{t.Fatalf("rejected %s: %v",raw,err)}}
	for _,raw:=range []string{"http://127.0.0.1:8091","http://169.254.169.254","https://127.0.0.1","https://tools.example.org@localhost","https://tools.example.org:8443","https://tools.example.org/path"}{if !errors.Is(ValidateBaseURL(raw),ErrUnsafeURL){t.Fatalf("accepted unsafe URL %s",raw)}}
}

func TestValidateArguments(t *testing.T){
	schema:=json.RawMessage(`{"type":"object","properties":{"topic":{"type":"string","enum":["area"]},"count":{"type":"integer"}},"required":["topic"],"additionalProperties":false}`)
	if err:=ValidateArguments(schema,json.RawMessage(`{"topic":"area","count":2}`));err!=nil{t.Fatal(err)}
	for _,raw:=range []string{`{}`,`{"topic":"other"}`,`{"topic":"area","count":1.5}`,`{"topic":"area","extra":1}`,`{"topic":"area"}{}`}{
		if err:=ValidateArguments(schema,json.RawMessage(raw));err==nil{t.Fatalf("accepted %s",raw)}
	}
}
