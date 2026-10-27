package mcpbridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"campusclaw/backend/internal/model"
)

var ErrUnsafeURL=errors.New("unsafe MCP URL")
var toolName=regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

func ValidateBaseURL(raw string)error{
	u,err:=url.Parse(raw)
	if err!=nil||u.User!=nil||u.RawQuery!=""||u.Fragment!=""||u.Path!=""||u.Hostname()==""{return ErrUnsafeURL}
	if raw=="http://mcp-server:8091"{return nil}
	if u.Scheme!="https"||u.Port()!=""||net.ParseIP(u.Hostname())!=nil||strings.HasSuffix(strings.ToLower(u.Hostname()),".local"){return ErrUnsafeURL}
	return nil
}

func publicIP(ip net.IP)bool{return !ip.IsLoopback()&&!ip.IsPrivate()&&!ip.IsLinkLocalUnicast()&&!ip.IsLinkLocalMulticast()&&!ip.IsUnspecified()&&!ip.IsMulticast()}

func newHTTPClient(baseURL string)*http.Client{
	transport:=&http.Transport{Proxy:nil,ResponseHeaderTimeout:8*time.Second}
	if baseURL!="http://mcp-server:8091"{
		transport.DialContext=func(ctx context.Context,network,address string)(net.Conn,error){
			host,port,err:=net.SplitHostPort(address);if err!=nil{return nil,err}
			ips,err:=net.DefaultResolver.LookupIPAddr(ctx,host);if err!=nil{return nil,err}
			for _,item:=range ips{if publicIP(item.IP){return (&net.Dialer{Timeout:5*time.Second}).DialContext(ctx,network,net.JoinHostPort(item.IP.String(),port))}}
			return nil,ErrUnsafeURL
		}
	}
	return &http.Client{Transport:transport,Timeout:15*time.Second,CheckRedirect:func(*http.Request,[]*http.Request)error{return ErrUnsafeURL}}
}

type Client struct{server model.MCPServer;http *http.Client}

func New(server model.MCPServer)(*Client,error){
	if err:=ValidateBaseURL(server.BaseURL);err!=nil{return nil,err}
	if server.TokenEnvKey==""||os.Getenv(server.TokenEnvKey)==""{return nil,errors.New("MCP token unavailable")}
	return &Client{server:server,http:newHTTPClient(server.BaseURL)},nil
}

func (c *Client) request(ctx context.Context,method string,params any)(json.RawMessage,error){
	input:=map[string]any{"jsonrpc":"2.0","id":1,"method":method}
	if params!=nil{input["params"]=params}
	body,err:=json.Marshal(input);if err!=nil{return nil,err}
	req,err:=http.NewRequestWithContext(ctx,http.MethodPost,c.server.BaseURL+"/mcp",bytes.NewReader(body));if err!=nil{return nil,err}
	req.Header.Set("Content-Type","application/json")
	req.Header.Set("Authorization","Bearer "+os.Getenv(c.server.TokenEnvKey))
	resp,err:=c.http.Do(req);if err!=nil{return nil,fmt.Errorf("MCP unavailable: %w",err)}
	defer resp.Body.Close()
	if resp.StatusCode!=http.StatusOK{return nil,fmt.Errorf("MCP HTTP %d",resp.StatusCode)}
	raw,err:=io.ReadAll(io.LimitReader(resp.Body,64<<10));if err!=nil{return nil,err}
	var parsed struct{JSONRPC string `json:"jsonrpc"`;ID json.RawMessage `json:"id"`;Result json.RawMessage `json:"result"`;Error *struct{Code int `json:"code"`;Message string `json:"message"`} `json:"error"`}
	if err:=json.Unmarshal(raw,&parsed);err!=nil{return nil,err}
	if parsed.JSONRPC!="2.0"||string(parsed.ID)!="1"{return nil,errors.New("invalid MCP response")}
	if parsed.Error!=nil{return nil,fmt.Errorf("MCP error %d",parsed.Error.Code)}
	if len(parsed.Result)==0{return nil,errors.New("empty MCP response")}
	return parsed.Result,nil
}

func (c *Client) Discover(ctx context.Context)([]model.MCPTool,error){
	if _,err:=c.request(ctx,"initialize",map[string]any{"protocolVersion":"2025-03-26","capabilities":map[string]any{},"clientInfo":map[string]string{"name":"campusclaw","version":"1.0.0"}});err!=nil{return nil,err}
	raw,err:=c.request(ctx,"tools/list",map[string]any{});if err!=nil{return nil,err}
	var parsed struct{Tools []struct{Name string `json:"name"`;Description string `json:"description"`;InputSchema json.RawMessage `json:"inputSchema"`} `json:"tools"`}
	if err:=json.Unmarshal(raw,&parsed);err!=nil{return nil,err}
	if len(parsed.Tools)>100{return nil,errors.New("too many tools")}
	seen:=map[string]bool{}
	out:=make([]model.MCPTool,0,len(parsed.Tools))
	for _,tool:=range parsed.Tools{
		if !toolName.MatchString(tool.Name)||seen[tool.Name]||len(tool.Description)>2000||len(tool.InputSchema)>16000{return nil,errors.New("invalid tool declaration")}
		var schema map[string]any
		if err:=json.Unmarshal(tool.InputSchema,&schema);err!=nil||schema["type"]!="object"{return nil,errors.New("invalid tool schema")}
		seen[tool.Name]=true
		out=append(out,model.MCPTool{Name:tool.Name,Description:tool.Description,InputSchema:tool.InputSchema})
	}
	return out,nil
}

func (c *Client) Call(ctx context.Context,name string,args json.RawMessage)(string,error){
	if !toolName.MatchString(name)||len(args)>16000{return "",errors.New("invalid tool call")}
	var arguments map[string]any
	if err:=json.Unmarshal(args,&arguments);err!=nil{return "",err}
	raw,err:=c.request(ctx,"tools/call",map[string]any{"name":name,"arguments":arguments});if err!=nil{return "",err}
	var parsed struct{Content []struct{Type string `json:"type"`;Text string `json:"text"`} `json:"content"`;IsError bool `json:"isError"`}
	if err:=json.Unmarshal(raw,&parsed);err!=nil{return "",err}
	if parsed.IsError||len(parsed.Content)==0{return "",errors.New("tool returned error")}
	var out strings.Builder
	for _,part:=range parsed.Content{if part.Type=="text"{out.WriteString(part.Text)}}
	result:=out.String()
	if len(result)>4000{result=result[:4000]}
	return result,nil
}
