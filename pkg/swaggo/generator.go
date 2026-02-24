package swaggo

import (
	"path/filepath"
	"strings"
)

// Generator 產生器設定
type Generator struct {
	Title       string
	Description string
	Version     string
	BasePath    string
	Host        string
	Schemes     []string

	projectRoot     string
	entryFile       string
	excludeDirs     []string
	parseVendor     bool
	parseDependency bool

	parser *Parser
}

// Stats 統計資訊
type Stats struct {
	Routes   int
	Handlers int
	Types    int
}

func New() *Generator {
	return &Generator{
		Title:    "API Documentation",
		Version:  "1.0.0",
		BasePath: "/",
		parser:   NewParser(),
	}
}

func (g *Generator) WithTitle(title string) *Generator {
	g.Title = title
	return g
}

func (g *Generator) WithDescription(desc string) *Generator {
	g.Description = desc
	return g
}

func (g *Generator) WithVersion(version string) *Generator {
	g.Version = version
	return g
}

func (g *Generator) WithBasePath(basePath string) *Generator {
	g.BasePath = basePath
	return g
}

func (g *Generator) WithHost(host string) *Generator {
	g.Host = host
	return g
}

func (g *Generator) WithSchemes(schemes ...string) *Generator {
	g.Schemes = schemes
	return g
}

func (g *Generator) WithExclude(dirs ...string) *Generator {
	g.excludeDirs = dirs
	g.parser.excludeDirs = dirs
	return g
}

// WithEntry 設定入口檔案，只解析從入口 import 的 package
func (g *Generator) WithEntry(entryFile string) *Generator {
	g.entryFile = entryFile
	return g
}

// WithProjectRoot 設定專案根目錄（搭配 WithEntry 使用）
func (g *Generator) WithProjectRoot(root string) *Generator {
	g.projectRoot = root
	return g
}

func (g *Generator) SetParseVendor(v bool) {
	g.parseVendor = v
	g.parser.parseVendor = v
}

func (g *Generator) SetParseDependency(v bool) {
	g.parseDependency = v
	g.parser.parseDependency = v
}

func (g *Generator) Stats() Stats {
	return Stats{
		Routes:   len(g.parser.Routes),
		Handlers: len(g.parser.Handlers),
		Types:    len(g.parser.Types),
	}
}

// Parse 解析原始碼（自動判斷全掃或入口模式）
func (g *Generator) Parse() error {
	if g.entryFile != "" {
		root := g.projectRoot
		if root == "" {
			root = "."
		}
		absRoot, _ := filepath.Abs(root)
		entryPath := g.entryFile
		if !filepath.IsAbs(entryPath) {
			entryPath = filepath.Join(absRoot, entryPath)
		}
		return g.ParseFromEntry(entryPath, absRoot)
	}

	root := g.projectRoot
	if root == "" {
		root = "."
	}
	return g.ParseSource(root)
}

func (g *Generator) ParseSource(paths ...string) error {
	for _, path := range paths {
		if err := g.parser.ParseDir(path); err != nil {
			return err
		}
	}
	return g.parser.Analyze()
}

// ParseFromEntry 從指定入口檔案解析，只追蹤被 import 的 package
func (g *Generator) ParseFromEntry(entryFile string, projectRoot string) error {
	if err := g.parser.ParseFromEntry(entryFile, projectRoot); err != nil {
		return err
	}
	return g.parser.Analyze()
}

func (g *Generator) Generate() (*OpenAPI, error) {
	spec := &OpenAPI{
		OpenAPI: "3.0.3",
		Info: Info{
			Title:       g.Title,
			Description: g.Description,
			Version:     g.Version,
		},
		Paths: make(map[string]PathItem),
		Components: &Components{
			Schemas: make(map[string]*Schema),
		},
	}

	if g.Host != "" {
		scheme := "http"
		if len(g.Schemes) > 0 {
			scheme = g.Schemes[0]
		}
		spec.Servers = append(spec.Servers, Server{
			URL: scheme + "://" + g.Host + g.BasePath,
		})
	}

	for _, route := range g.parser.Routes {
		path := convertGinPathToOpenAPI(route.Path)

		pathItem, exists := spec.Paths[path]
		if !exists {
			pathItem = PathItem{}
		}

		op := g.routeToOperation(route)

		switch strings.ToUpper(route.Method) {
		case "GET":
			pathItem.Get = op
		case "POST":
			pathItem.Post = op
		case "PUT":
			pathItem.Put = op
		case "DELETE":
			pathItem.Delete = op
		case "PATCH":
			pathItem.Patch = op
		case "OPTIONS":
			pathItem.Options = op
		case "HEAD":
			pathItem.Head = op
		}

		spec.Paths[path] = pathItem
	}

	registeredSchemas := make(map[string]bool)
	for _, typeInfo := range g.parser.Types {
		name := typeInfo.Name
		if registeredSchemas[name] {
			continue
		}
		if g.isTypeReferenced(name) {
			spec.Components.Schemas[name] = g.typeToSchema(typeInfo)
			registeredSchemas[name] = true
		}
	}

	return spec, nil
}

func (g *Generator) routeToOperation(route *RouteInfo) *Operation {
	op := &Operation{
		Responses: make(map[string]Response),
	}

	if route.Handler != nil {
		op.Summary = route.Handler.Summary
		op.Description = route.Handler.Description
		op.OperationID = g.generateOperationID(route.Handler)

		for _, param := range route.Handler.Parameters {
			op.Parameters = append(op.Parameters, g.paramToOpenAPI(param))
		}

		if route.Handler.RequestBody != nil {
			op.RequestBody = &RequestBody{
				Required: true,
				Content: map[string]MediaType{
					"application/json": {
						Schema: g.typeRefOrInline(route.Handler.RequestBody),
					},
				},
			}
		}

		for code, resp := range route.Handler.Responses {
			op.Responses[statusCodeToString(code)] = g.responseToOpenAPI(resp)
		}
	}

	if len(op.Responses) == 0 {
		op.Responses["200"] = Response{
			Description: "Successful response",
		}
	}

	if route.Group != "" {
		tag := strings.Trim(route.Group, "/")
		if tag != "" {
			parts := strings.Split(tag, "/")
			tag = parts[len(parts)-1]
			op.Tags = []string{tag}
		}
	}

	return op
}

func (g *Generator) paramToOpenAPI(param *ParameterInfo) Parameter {
	p := Parameter{
		Name:        param.Name,
		In:          param.In,
		Description: param.Comment,
		Required:    param.Required || param.In == "path",
		Schema:      g.primitiveSchema(param.Type),
	}

	if param.Default != "" {
		p.Schema.Example = param.Default
	}

	return p
}

func (g *Generator) generateOperationID(handler *HandlerInfo) string {
	if handler.Receiver != "" {
		return handler.Receiver + "_" + handler.Name
	}
	return handler.Name
}

func (g *Generator) responseToOpenAPI(resp *ResponseInfo) Response {
	r := Response{
		Description: resp.Description,
	}

	if r.Description == "" {
		r.Description = statusCodeDescription(resp.StatusCode)
	}

	if resp.Type != nil {
		schema := g.typeRefOrInline(resp.Type)
		if resp.IsArray {
			schema = &Schema{
				Type:  "array",
				Items: schema,
			}
		}
		r.Content = map[string]MediaType{
			"application/json": {Schema: schema},
		}
	}

	return r
}

func (g *Generator) typeRefOrInline(ti *TypeInfo) *Schema {
	if ti == nil {
		return &Schema{Type: "object"}
	}

	switch ti.Kind {
	case "struct":
		if ti.Name != "" && ti.Name != "object" {
			return &Schema{Ref: "#/components/schemas/" + ti.Name}
		}
		return g.typeToSchema(ti)
	case "map":
		return &Schema{Type: "object"}
	case "primitive":
		return g.primitiveSchema(ti.Name)
	default:
		return &Schema{Type: "object"}
	}
}

func (g *Generator) typeToSchema(ti *TypeInfo) *Schema {
	if ti == nil {
		return &Schema{Type: "object"}
	}

	schema := &Schema{
		Type:        "object",
		Description: ti.Comment,
		Properties:  make(map[string]*Schema),
	}

	for _, field := range ti.Fields {
		fieldSchema := g.fieldToSchema(field)
		schema.Properties[field.JSONName] = fieldSchema

		if field.Required {
			schema.Required = append(schema.Required, field.JSONName)
		}
	}

	return schema
}

func (g *Generator) fieldToSchema(field *FieldInfo) *Schema {
	schema := g.goTypeToSchema(field.Type)
	schema.Description = field.Comment

	if field.Example != "" {
		schema.Example = field.Example
	}

	return schema
}

func (g *Generator) goTypeToSchema(goType string) *Schema {
	goType = strings.TrimPrefix(goType, "*")

	if strings.HasPrefix(goType, "[]") {
		elemType := strings.TrimPrefix(goType, "[]")
		return &Schema{
			Type:  "array",
			Items: g.goTypeToSchema(elemType),
		}
	}

	if strings.HasPrefix(goType, "map[") {
		return &Schema{Type: "object"}
	}

	if _, ok := g.parser.Types[goType]; ok {
		return &Schema{Ref: "#/components/schemas/" + goType}
	}

	return g.primitiveSchema(goType)
}

func (g *Generator) primitiveSchema(goType string) *Schema {
	switch goType {
	case "string":
		return &Schema{Type: "string"}
	case "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64",
		"integer":
		return &Schema{Type: "integer"}
	case "float32", "float64":
		return &Schema{Type: "number"}
	case "bool", "boolean":
		return &Schema{Type: "boolean"}
	case "time.Time":
		return &Schema{Type: "string", Format: "date-time"}
	case "interface{}", "any":
		return &Schema{}
	default:
		if _, ok := g.parser.Types[goType]; ok {
			return &Schema{Ref: "#/components/schemas/" + goType}
		}
		return &Schema{Type: "string"}
	}
}

func (g *Generator) isTypeReferenced(name string) bool {
	for _, handler := range g.parser.Handlers {
		if handler.RequestBody != nil {
			if handler.RequestBody.Name == name || handler.RequestBody.FullName == name {
				return true
			}
		}
		for _, resp := range handler.Responses {
			if resp.Type != nil {
				if resp.Type.Name == name || resp.Type.FullName == name {
					return true
				}
			}
		}
	}
	for _, ti := range g.parser.Types {
		for _, field := range ti.Fields {
			fieldType := strings.TrimPrefix(field.Type, "*")
			fieldType = strings.TrimPrefix(fieldType, "[]")
			parts := strings.Split(fieldType, ".")
			simpleName := parts[len(parts)-1]
			if fieldType == name || simpleName == name {
				return true
			}
		}
	}
	return false
}

func convertGinPathToOpenAPI(path string) string {
	result := strings.ReplaceAll(path, ":", "{")

	var output strings.Builder
	inParam := false
	for i, c := range result {
		if c == '{' {
			inParam = true
			output.WriteRune(c)
		} else if inParam && (c == '/' || i == len(result)-1) {
			if i == len(result)-1 && c != '/' {
				output.WriteRune(c)
			}
			output.WriteRune('}')
			if c == '/' {
				output.WriteRune(c)
			}
			inParam = false
		} else {
			output.WriteRune(c)
		}
	}

	res := output.String()
	res = strings.ReplaceAll(res, "*", "{")

	return res
}

func statusCodeToString(code int) string {
	switch code {
	case 200:
		return "200"
	case 201:
		return "201"
	case 204:
		return "204"
	case 400:
		return "400"
	case 401:
		return "401"
	case 403:
		return "403"
	case 404:
		return "404"
	case 500:
		return "500"
	default:
		return "default"
	}
}

func statusCodeDescription(code int) string {
	descriptions := map[int]string{
		200: "Successful response",
		201: "Created",
		204: "No content",
		400: "Bad request",
		401: "Unauthorized",
		403: "Forbidden",
		404: "Not found",
		500: "Internal server error",
	}
	if desc, ok := descriptions[code]; ok {
		return desc
	}
	return "Response"
}
