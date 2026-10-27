package server

import (
  _ "embed"
  "net/http"
)

//go:embed docs/openapi.json
var openAPIDocument []byte

const swaggerPage=`<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><title>CampusClaw API · Swagger</title><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui.css"></head><body><div id="swagger-ui"></div><script src="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui-bundle.js"></script><script>window.onload=function(){SwaggerUIBundle({url:(location.pathname.startsWith('/week04/')?'/week04':'')+'/openapi.json',dom_id:'#swagger-ui',persistAuthorization:true,deepLinking:true})}</script></body></html>`

func(s *Server) swaggerRoutes(){
  s.mux.HandleFunc("GET /openapi.json",func(w http.ResponseWriter,_ *http.Request){w.Header().Set("Content-Type","application/json; charset=utf-8");_,_=w.Write(openAPIDocument)})
  s.mux.HandleFunc("GET /swagger",func(w http.ResponseWriter,_ *http.Request){w.Header().Set("Content-Type","text/html; charset=utf-8");_,_=w.Write([]byte(swaggerPage))})
}
