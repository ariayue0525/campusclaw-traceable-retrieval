package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"campusclaw/backend/internal/mcpserver"
)

func main(){
	token:=os.Getenv("MCP_AUTH_TOKEN")
	if len(token)<24{log.Fatal("MCP_AUTH_TOKEN must contain at least 24 characters")}
	addr:=os.Getenv("MCP_ADDR");if addr==""{addr=":8091"}
	srv:=&http.Server{Addr:addr,Handler:mcpserver.Handler(token),ReadHeaderTimeout:5*time.Second,ReadTimeout:10*time.Second,WriteTimeout:10*time.Second}
	log.Printf("CampusClaw MCP service listening on %s",addr)
	log.Fatal(srv.ListenAndServe())
}
