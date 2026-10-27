package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
)

func TestStreamUsesGatewayDeltas(t *testing.T){
	var request map[string]any
	gateway:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
		if r.Header.Get("Authorization")!="Bearer local-test"{t.Error("missing bearer")}
		if err:=json.NewDecoder(r.Body).Decode(&request);err!=nil{t.Error(err)}
		w.Header().Set("Content-Type","text/event-stream")
		fmt.Fprint(w,"data: {\"choices\":[{\"delta\":{\"content\":\"第一\"}}]}\n\n")
		fmt.Fprint(w,"data: {\"choices\":[{\"delta\":{\"content\":\"第二\"}}]}\n\n")
		fmt.Fprint(w,"data: [DONE]\n\n")
	}));defer gateway.Close()
	c:=New(gateway.URL,"local-test","test-model",2*time.Second)
	parts:=[]string{}
	err:=c.Stream(context.Background(),[]Message{{Role:"system",Content:"role"},{Role:"user",Content:"question"}},func(delta string)error{parts=append(parts,delta);return nil})
	if err!=nil{t.Fatal(err)}
	if !reflect.DeepEqual(parts,[]string{"第一","第二"}){t.Fatalf("not incremental: %v",parts)}
	if request["stream"]!=true||request["model"]!="test-model"{t.Fatalf("unexpected gateway request: %+v",request)}
	if len(request["messages"].([]any))!=2{t.Fatalf("messages omitted: %+v",request)}
}

func TestStreamCollectsToolCallFragments(t *testing.T){
	var captured map[string]any
	gateway:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
		_ = json.NewDecoder(r.Body).Decode(&captured)
		w.Header().Set("Content-Type","text/event-stream")
		fmt.Fprint(w,"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"mcp_tool_\",\"arguments\":\"{\\\"topic\\\":\"}}]}}]}\n\n")
		fmt.Fprint(w,"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"name\":\"7\",\"arguments\":\"\\\"area\\\"}\"}}]}}]}\n\n")
		fmt.Fprint(w,"data: [DONE]\n\n")
	}));defer gateway.Close()
	c:=New(gateway.URL,"test","test-model",2*time.Second)
	tool:=functionSpecForTest()
	calls,err:=c.StreamWithTools(context.Background(),[]Message{{Role:"user",Content:"help"}},[]FunctionTool{tool},func(string)error{return nil})
	if err!=nil{t.Fatal(err)}
	if len(calls)!=1||calls[0].Function.Name!="mcp_tool_7"||calls[0].Function.Arguments!=`{"topic":"area"}`{t.Fatalf("fragments: %+v",calls)}
	if captured["tool_choice"]!="auto"||len(captured["tools"].([]any))!=1{t.Fatalf("tool declarations missing: %+v",captured)}
}

func functionSpecForTest()FunctionTool{
	var tool FunctionTool
	tool.Type="function";tool.Function.Name="mcp_tool_7";tool.Function.Description="hint";tool.Function.Parameters=json.RawMessage(`{"type":"object"}`)
	return tool
}
