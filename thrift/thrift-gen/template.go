package main

var serviceTmpl = `
// Package {{ .Package }} is generated code used to make or handle TChannel calls using Thrift.
package {{ .Package }}

import (
"fmt"

athrift "{{ .ThriftImport }}"
"{{ .TChannelImport }}"
)

// Interfaces for the service and client for the services defined in the IDL.

{{ range .Services }}
// {{ .Interface }} is the interface that defines the server handler and client interface.
type {{ .Interface }} interface {
	{{ if .HasExtends }}
		{{ .ExtendsService.Interface }}

	{{ end }}
	{{ range .Methods }}
		{{ .Name }}({{ .ArgList }}) {{ .RetType }}
	{{ end }}
}
{{ end }}

// Implementation of a client and service handler.

{{/* Generate client and service implementations for the above interfaces. */}}
{{ range $svc := .Services }}
type {{ .ClientStruct }} struct {
	{{ if .HasExtends }}
		{{ .ExtendsService.ClientStruct }}

	{{ end }}
	thriftService string
	client        thrift.TChanClient
}


func {{ .InternalClientConstructor }}(thriftService string, client thrift.TChanClient) *{{ .ClientStruct }} {
	return &{{ .ClientStruct }}{
		{{ if .HasExtends }}
			*{{ .ExtendsService.InternalClientConstructor }}(thriftService, client),
		{{ end }}
		thriftService,
		client,
	}
}

// {{ .ClientConstructor }} creates a client that can be used to make remote calls.
func {{ .ClientConstructor }}(client thrift.TChanClient) {{ .Interface }} {
	return {{ .InternalClientConstructor }}("{{ .ThriftName }}", client)
}

{{ range .Methods }}
	func (c *{{ $svc.ClientStruct }}) {{ .Name }}({{ .ArgList }}) {{ .RetType }} {
		var resp {{ .ResultType }}
		args := {{ .ArgsType }}{
			{{ range .Arguments }}
				{{ .ArgStructName }}: {{ .Name }},
			{{ end }}
		}
		success, err := c.client.Call(ctx, c.thriftService, "{{ .ThriftName }}", &args, &resp)
		if err == nil && !success {
			{{ range .Exceptions }}
				if e := resp.{{ .ArgStructName }}; e != nil {
					err = e
				}
			{{ end }}
		}

		{{ if .HasReturn }}
			return resp.GetSuccess(), err
		{{ else }}
			return err
		{{ end }}
	}
{{ end }}

type {{ .ServerStruct }} struct {
	{{ if .HasExtends }}
		{{ .ExtendsService.ServerStruct }}

	{{ end }}
	handler {{ .Interface }}
}

func {{ .InternalServerConstructor }}(handler {{ .Interface }}) *{{ .ServerStruct }} {
	return &{{ .ServerStruct }}{
		{{ if .HasExtends }}
			*{{ .ExtendsService.InternalServerConstructor }}(handler),
		{{ end }}
		handler,
	}
}

// {{ .ServerConstructor }} wraps a handler for {{ .Interface }} so it can be
// registered with a thrift.Server.
func {{ .ServerConstructor }}(handler {{ .Interface }}) thrift.TChanServer {
	return {{ .InternalServerConstructor }}(handler)
}

func (s *{{ .ServerStruct }}) Service() string {
	return "{{ .ThriftName }}"
}

func (s *{{ .ServerStruct }}) Methods() []string {
	return []string{
		{{ range .Methods }}
			"{{ .ThriftName }}",
		{{ end }}
		{{ if .HasExtends }}
			{{ range .ExtendsService.Methods }}
				"{{ .ThriftName }}",
			{{ end }}
		{{ end }}
	}
}

func (s *{{ .ServerStruct }}) Handle(ctx {{ contextType }}, methodName string, protocol athrift.TProtocol) (bool, athrift.TStruct, error) {
	switch methodName {
		{{ range .Methods }}
			case "{{ .ThriftName }}":
				return s.{{ .HandleFunc }}(ctx, protocol)
		{{ end }}
		{{ if .HasExtends }}
			{{ range .ExtendsService.Methods }}
				case "{{ .ThriftName }}":
					return s.{{ .HandleFunc }}(ctx, protocol)
			{{ end }}
		{{ end }}
		default:
			return false, nil, fmt.Errorf("method %v not found in service %v", methodName, s.Service())
	}
}

{{ range .Methods }}
	func (s *{{ $svc.ServerStruct }}) {{ .HandleFunc }}(ctx {{ contextType }}, protocol athrift.TProtocol) (bool, athrift.TStruct, error) {
		var req {{ .ArgsType }}
		var res {{ .ResultType }}

		if err := req.Read(protocol); err != nil {
			return false, nil, err
		}

		{{ if .HasReturn }}
			r, err :=
		{{ else }}
			err :=
		{{ end }}
				s.handler.{{ .Name }}({{ .CallList "req" }})

		if err != nil {
			{{ if .HasExceptions }}
			switch v := err.(type) {
				{{ range .Exceptions }}
					case {{ .ArgType }}:
						res.{{ .ArgStructName }} = v
				{{ end }}
					default:
						return false, nil, err
			}
			{{ else }}
				return false, nil, err
			{{ end }}
		} else {
    {{ if .HasReturn }}
		  res.Success = {{ .WrapResult "r" }}
		{{ end }}
    }

		return err == nil, &res, nil
	}

{{ end }}

{{ end }}
`
