package mcpserver

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func request(t *testing.T,h http.Handler,token,method string,params any)(int,map[string]any){
	t.Helper()
	body,_:=json.Marshal(map[string]any{"jsonrpc":"2.0","id":1,"method":method,"params":params})
	r:=httptest.NewRequest(http.MethodPost,"/mcp",bytes.NewReader(body))
	if token!=""{r.Header.Set("Authorization","Bearer "+token)}
	w:=httptest.NewRecorder();h.ServeHTTP(w,r)
	var out map[string]any;_ = json.Unmarshal(w.Body.Bytes(),&out)
	return w.Code,out
}

func TestProtocolAndAuthentication(t *testing.T){
	h:=Handler("test-token-at-least-24-chars")
	if status,_:=request(t,h,"","tools/list",nil);status!=http.StatusUnauthorized{t.Fatalf("missing token status %d",status)}
	if status,_:=request(t,h,"wrong","tools/list",nil);status!=http.StatusUnauthorized{t.Fatalf("wrong token status %d",status)}
	status,init:=request(t,h,"test-token-at-least-24-chars","initialize",map[string]any{})
	if status!=200||init["result"].(map[string]any)["protocolVersion"]==""{t.Fatalf("initialize: %+v",init)}
	status,list:=request(t,h,"test-token-at-least-24-chars","tools/list",nil)
	if status!=200||len(list["result"].(map[string]any)["tools"].([]any))!=2{t.Fatalf("tools/list: %+v",list)}
	status,called:=request(t,h,"test-token-at-least-24-chars","tools/call",map[string]any{"name":"formula_hint","arguments":map[string]any{"topic":"linear_equation"}})
	if status!=200||called["result"].(map[string]any)["isError"]!=false{t.Fatalf("tools/call: %+v",called)}
	_,rejected:=request(t,h,"test-token-at-least-24-chars","tools/call",map[string]any{"name":"unit_convert","arguments":map[string]any{"value":2,"from":"kg","to":"cm"}})
	if rejected["error"].(map[string]any)["code"]!=float64(-32602){t.Fatalf("cross-dimension conversion accepted: %+v",rejected)}
	_,unknown:=request(t,h,"test-token-at-least-24-chars","tools/call",map[string]any{"name":"shell","arguments":map[string]any{}})
	if unknown["error"].(map[string]any)["code"]!=float64(-32602){t.Fatalf("unknown tool accepted: %+v",unknown)}
}
