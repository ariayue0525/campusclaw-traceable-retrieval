package server

import (
  "encoding/json"
  "strings"
  "testing"
)

func TestSwaggerDocument(t *testing.T){
  var doc struct{OpenAPI string `json:"openapi"`;Paths map[string]map[string]any `json:"paths"`;Components map[string]any `json:"components"`}
  if err:=json.Unmarshal(openAPIDocument,&doc);err!=nil{t.Fatal(err)}
  if doc.OpenAPI!="3.0.3"||len(doc.Paths)<25{t.Fatalf("incomplete OpenAPI: %s %d",doc.OpenAPI,len(doc.Paths))}
  for _,path:=range []string{"/api/login","/api/search","/api/agents","/api/skills","/api/conversations/{id}/messages/stream","/api/mcp/servers/{id}/connect"}{if len(doc.Paths[path])==0{t.Errorf("missing path %s",path)}}
  if !strings.Contains(swaggerPage,"SwaggerUIBundle")||!strings.Contains(swaggerPage,"openapi.json"){t.Fatal("Swagger UI is not configured")}
}
