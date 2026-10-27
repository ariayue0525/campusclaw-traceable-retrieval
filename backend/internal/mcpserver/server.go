package mcpserver

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
)

type Tool struct {
	Name string `json:"name"`
	Description string `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

func Tools()[]Tool{return []Tool{
	{Name:"formula_hint",Description:"给出常见公式的概念性提示，不直接代入得数。",InputSchema:map[string]any{"type":"object","properties":map[string]any{"topic":map[string]any{"type":"string","enum":[]string{"area_rectangle","speed_distance_time","linear_equation"}}},"required":[]string{"topic"},"additionalProperties":false}},
	{Name:"unit_convert",Description:"在受支持的长度或质量单位之间换算，结果仅供核对。",InputSchema:map[string]any{"type":"object","properties":map[string]any{"value":map[string]any{"type":"number"},"from":map[string]any{"type":"string","enum":[]string{"mm","cm","m","km","g","kg"}},"to":map[string]any{"type":"string","enum":[]string{"mm","cm","m","km","g","kg"}}},"required":[]string{"value","from","to"},"additionalProperties":false}},
}}

type rpcRequest struct{JSONRPC string `json:"jsonrpc"`;ID json.RawMessage `json:"id"`;Method string `json:"method"`;Params json.RawMessage `json:"params"`}

func Handler(token string)http.Handler{
	mux:=http.NewServeMux()
	mux.HandleFunc("GET /health",func(w http.ResponseWriter,_ *http.Request){write(w,http.StatusOK,map[string]string{"status":"ok"})})
	mux.HandleFunc("POST /mcp",func(w http.ResponseWriter,r *http.Request){
		provided:=strings.TrimPrefix(r.Header.Get("Authorization"),"Bearer ")
		if token==""||!strings.HasPrefix(r.Header.Get("Authorization"),"Bearer ")||subtle.ConstantTimeCompare([]byte(provided),[]byte(token))!=1{write(w,http.StatusUnauthorized,map[string]string{"error":"unauthorized"});return}
		r.Body=http.MaxBytesReader(w,r.Body,16<<10)
		var req rpcRequest
		decoder:=json.NewDecoder(r.Body);decoder.DisallowUnknownFields()
		if err:=decoder.Decode(&req);err!=nil||req.JSONRPC!="2.0"||len(req.ID)==0||req.Method==""{rpcError(w,req.ID,-32600,"invalid request");return}
		var trailing any
		if err:=decoder.Decode(&trailing);err!=io.EOF{rpcError(w,req.ID,-32600,"invalid request");return}
		switch req.Method{
		case "initialize":
			write(w,http.StatusOK,map[string]any{"jsonrpc":"2.0","id":req.ID,"result":map[string]any{"protocolVersion":"2025-03-26","capabilities":map[string]any{"tools":map[string]any{}},"serverInfo":map[string]string{"name":"campusclaw-teaching-tools","version":"1.0.0"}}})
		case "tools/list":
			write(w,http.StatusOK,map[string]any{"jsonrpc":"2.0","id":req.ID,"result":map[string]any{"tools":Tools()}})
		case "tools/call":
			var in struct{Name string `json:"name"`;Arguments json.RawMessage `json:"arguments"`}
			if err:=json.Unmarshal(req.Params,&in);err!=nil{rpcError(w,req.ID,-32602,"invalid params");return}
			answer,err:=Call(in.Name,in.Arguments)
			if err!=nil{rpcError(w,req.ID,-32602,"invalid params");return}
			write(w,http.StatusOK,map[string]any{"jsonrpc":"2.0","id":req.ID,"result":map[string]any{"content":[]map[string]string{{"type":"text","text":answer}},"isError":false}})
		default:rpcError(w,req.ID,-32601,"method not found")
		}
	})
	return mux
}

func Call(name string,raw json.RawMessage)(string,error){
	switch name{
	case "formula_hint":
		var in struct{Topic string `json:"topic"`}
		if err:=strictArgs(raw,&in);err!=nil{return "",err}
		switch in.Topic{
		case "area_rectangle":return "先确认长与宽的单位一致，再回想长方形面积与两条相邻边的关系。",nil
		case "speed_distance_time":return "先分清路程、速度和时间三个量，再想其中哪两个量已知。",nil
		case "linear_equation":return "先找未知数和等量关系，再检查等号两侧做了什么相同操作。",nil
		default:return "",fmt.Errorf("unknown topic")
		}
	case "unit_convert":
		var in struct{Value *float64 `json:"value"`;From string `json:"from"`;To string `json:"to"`}
		if err:=strictArgs(raw,&in);err!=nil{return "",err}
		if in.Value==nil||in.From==""||in.To==""{return "",fmt.Errorf("missing conversion field")}
		length:=map[string]float64{"mm":0.001,"cm":0.01,"m":1,"km":1000}
		mass:=map[string]float64{"g":0.001,"kg":1}
		factorFrom,okFrom:=length[in.From];factorTo,okTo:=length[in.To]
		if !okFrom||!okTo{factorFrom,okFrom=mass[in.From];factorTo,okTo=mass[in.To]}
		if !okFrom||!okTo||math.IsInf(*in.Value,0)||math.IsNaN(*in.Value)||math.Abs(*in.Value)>1e12{return "",fmt.Errorf("invalid conversion")}
		return fmt.Sprintf("%g %s = %g %s",*in.Value,in.From,*in.Value*factorFrom/factorTo,in.To),nil
	default:return "",fmt.Errorf("unknown tool")
	}
}

func strictArgs(raw json.RawMessage,out any)error{
	if len(raw)==0{return fmt.Errorf("missing arguments")}
	d:=json.NewDecoder(strings.NewReader(string(raw)));d.DisallowUnknownFields()
	if err:=d.Decode(out);err!=nil{return err}
	var trailing any
	if err:=d.Decode(&trailing);err!=io.EOF{return fmt.Errorf("trailing JSON")}
	return nil
}

func rpcError(w http.ResponseWriter,id json.RawMessage,code int,message string){
	if len(id)==0{id=json.RawMessage("null")}
	write(w,http.StatusOK,map[string]any{"jsonrpc":"2.0","id":id,"error":map[string]any{"code":code,"message":message}})
}

func write(w http.ResponseWriter,status int,value any){w.Header().Set("Content-Type","application/json");w.WriteHeader(status);_ = json.NewEncoder(w).Encode(value)}
