package mcpbridge

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"reflect"
)

var ErrInvalidArguments=errors.New("tool arguments do not match schema")

// ValidateArguments supports the bounded object/primitive subset accepted by
// Discover. Unknown keys are rejected even if a remote schema permits them.
func ValidateArguments(schemaRaw,argsRaw json.RawMessage)error{
	var schema struct{Type string `json:"type"`;Properties map[string]struct{Type string `json:"type"`;Enum []any `json:"enum"`} `json:"properties"`;Required []string `json:"required"`}
	if err:=json.Unmarshal(schemaRaw,&schema);err!=nil||schema.Type!="object"{return ErrInvalidArguments}
	decoder:=json.NewDecoder(bytes.NewReader(argsRaw));decoder.UseNumber()
	var args map[string]any
	if err:=decoder.Decode(&args);err!=nil||args==nil{return ErrInvalidArguments}
	var trailing any
	if err:=decoder.Decode(&trailing);err!=io.EOF{return ErrInvalidArguments}
	for _,key:=range schema.Required{if _,ok:=args[key];!ok{return ErrInvalidArguments}}
	for key,value:=range args{
		property,ok:=schema.Properties[key];if !ok{return ErrInvalidArguments}
		switch property.Type{
		case "string":if _,ok:=value.(string);!ok{return ErrInvalidArguments}
		case "number","integer":
			n,ok:=value.(json.Number);if !ok{return ErrInvalidArguments}
			f,err:=n.Float64();if err!=nil||math.IsInf(f,0)||math.IsNaN(f){return ErrInvalidArguments}
			if property.Type=="integer"&&math.Trunc(f)!=f{return ErrInvalidArguments}
		case "boolean":if _,ok:=value.(bool);!ok{return ErrInvalidArguments}
		default:return ErrInvalidArguments
		}
		if len(property.Enum)>0{
			matched:=false
			for _,option:=range property.Enum{if reflect.DeepEqual(value,option)||reflect.DeepEqual(value,json.Number(toJSONNumber(option))){matched=true;break}}
			if !matched{return ErrInvalidArguments}
		}
	}
	return nil
}

func toJSONNumber(value any)string{if f,ok:=value.(float64);ok{raw,_:=json.Marshal(f);return string(raw)};return ""}
