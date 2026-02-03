package swaggo

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Parser 解析 Go 原始碼，提取 API 資訊
type Parser struct {
	fset     *token.FileSet
	packages map[string]*ast.Package
	files    []*ast.File

	Routes   []*RouteInfo
	Handlers map[string]*HandlerInfo
	Types    map[string]*TypeInfo
}

// RouteInfo 路由資訊
type RouteInfo struct {
	Method      string
	Path        string
	HandlerName string
	Handler     *HandlerInfo
	Group       string
}

// HandlerInfo Handler 函數資訊
type HandlerInfo struct {
	Name        string
	Package     string
	FilePath    string
	Summary     string
	Description string
	Parameters  []*ParameterInfo
	RequestBody *TypeInfo
	Responses   map[int]*ResponseInfo
}

// ParameterInfo 參數資訊
type ParameterInfo struct {
	Name     string
	Type     string
	In       string // path, query, header, cookie
	Required bool
	Default  string
	Comment  string
}

// ResponseInfo 回應資訊
type ResponseInfo struct {
	StatusCode  int
	Type        *TypeInfo
	IsArray     bool
	Description string
}

// TypeInfo 型別資訊
type TypeInfo struct {
	Name    string
	Package string
	Kind    string // struct, primitive, map, slice
	Fields  []*FieldInfo
	Element *TypeInfo
	Comment string
}

// FieldInfo 欄位資訊
type FieldInfo struct {
	Name     string
	Type     string
	TypeInfo *TypeInfo
	JSONName string
	Comment  string
	Required bool
	Example  string
	Tags     map[string]string
}

func NewParser() *Parser {
	return &Parser{
		fset:     token.NewFileSet(),
		packages: make(map[string]*ast.Package),
		Handlers: make(map[string]*HandlerInfo),
		Types:    make(map[string]*TypeInfo),
	}
}

func (p *Parser) ParseDir(dir string) error {
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if info.Name() == "vendor" || info.Name() == ".git" {
				return filepath.SkipDir
			}
			pkgs, err := parser.ParseDir(p.fset, path, func(fi os.FileInfo) bool {
				return !strings.HasSuffix(fi.Name(), "_test.go")
			}, parser.ParseComments)
			if err != nil {
				return nil
			}
			for name, pkg := range pkgs {
				p.packages[name] = pkg
				for _, file := range pkg.Files {
					p.files = append(p.files, file)
				}
			}
		}
		return nil
	})
}

func (p *Parser) ParseFile(filename string) error {
	file, err := parser.ParseFile(p.fset, filename, nil, parser.ParseComments)
	if err != nil {
		return err
	}
	p.files = append(p.files, file)
	return nil
}

func (p *Parser) Analyze() error {
	for _, file := range p.files {
		p.extractTypes(file)
	}
	for _, file := range p.files {
		p.extractHandlers(file)
		p.extractRoutes(file)
	}
	for _, route := range p.Routes {
		if handler, ok := p.Handlers[route.HandlerName]; ok {
			route.Handler = handler
			p.addPathParams(route)
		}
	}
	return nil
}

func (p *Parser) extractTypes(file *ast.File) {
	ast.Inspect(file, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSpec)
		if !ok {
			return true
		}

		st, ok := ts.Type.(*ast.StructType)
		if !ok {
			return true
		}

		typeInfo := &TypeInfo{
			Name:    ts.Name.Name,
			Package: file.Name.Name,
			Kind:    "struct",
		}

		if ts.Doc != nil {
			typeInfo.Comment = strings.TrimSpace(ts.Doc.Text())
		}

		for _, field := range st.Fields.List {
			if len(field.Names) == 0 {
				continue
			}

			fi := &FieldInfo{
				Name: field.Names[0].Name,
				Type: p.typeToString(field.Type),
				Tags: make(map[string]string),
			}

			if field.Tag != nil {
				tag := strings.Trim(field.Tag.Value, "`")
				fi.Tags = parseStructTags(tag)

				if jsonTag, ok := fi.Tags["json"]; ok {
					parts := strings.Split(jsonTag, ",")
					if parts[0] != "-" {
						fi.JSONName = parts[0]
					}
				}
				if fi.JSONName == "" {
					fi.JSONName = fi.Name
				}

				if bindTag, ok := fi.Tags["binding"]; ok {
					fi.Required = strings.Contains(bindTag, "required")
				}

				if example, ok := fi.Tags["example"]; ok {
					fi.Example = example
				}
			} else {
				fi.JSONName = fi.Name
			}

			if field.Comment != nil {
				fi.Comment = strings.TrimSpace(field.Comment.Text())
			} else if field.Doc != nil {
				fi.Comment = strings.TrimSpace(field.Doc.Text())
			}

			typeInfo.Fields = append(typeInfo.Fields, fi)
		}

		p.Types[typeInfo.Name] = typeInfo
		return true
	})
}

func (p *Parser) extractHandlers(file *ast.File) {
	ast.Inspect(file, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok {
			return true
		}

		if !p.isGinHandler(fn) {
			return true
		}

		handler := &HandlerInfo{
			Name:      fn.Name.Name,
			Package:   file.Name.Name,
			FilePath:  p.fset.Position(fn.Pos()).Filename,
			Responses: make(map[int]*ResponseInfo),
		}

		if fn.Doc != nil {
			lines := strings.Split(strings.TrimSpace(fn.Doc.Text()), "\n")
			if len(lines) > 0 {
				handler.Summary = strings.TrimSpace(lines[0])
			}
			if len(lines) > 1 {
				handler.Description = strings.TrimSpace(strings.Join(lines[1:], "\n"))
			}
		}

		p.analyzeHandlerBody(fn, handler)
		p.Handlers[handler.Name] = handler
		return true
	})
}

func (p *Parser) isGinHandler(fn *ast.FuncDecl) bool {
	if fn.Type.Params == nil || len(fn.Type.Params.List) == 0 {
		return false
	}
	for _, param := range fn.Type.Params.List {
		typeStr := p.typeToString(param.Type)
		if strings.Contains(typeStr, "gin.Context") || strings.Contains(typeStr, "Context") {
			return true
		}
	}
	return false
}

func (p *Parser) analyzeHandlerBody(fn *ast.FuncDecl, handler *HandlerInfo) {
	if fn.Body == nil {
		return
	}

	localVarTypes := make(map[string]string)

	for _, stmt := range fn.Body.List {
		switch s := stmt.(type) {
		case *ast.DeclStmt:
			if gd, ok := s.Decl.(*ast.GenDecl); ok {
				for _, spec := range gd.Specs {
					if vs, ok := spec.(*ast.ValueSpec); ok {
						for _, n := range vs.Names {
							if vs.Type != nil {
								localVarTypes[n.Name] = p.typeToString(vs.Type)
							}
						}
					}
				}
			}
		case *ast.AssignStmt:
			for i, lhs := range s.Lhs {
				if id, ok := lhs.(*ast.Ident); ok {
					if i < len(s.Rhs) {
						if cl, ok := s.Rhs[i].(*ast.CompositeLit); ok {
							localVarTypes[id.Name] = p.typeToString(cl.Type)
						}
					}
				}
			}
		}
	}

	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}

		method := sel.Sel.Name

		switch method {
		case "Param":
			if len(call.Args) > 0 {
				if name := p.extractStringArg(call.Args[0]); name != "" {
					handler.Parameters = append(handler.Parameters, &ParameterInfo{
						Name:     name,
						Type:     "string",
						In:       "path",
						Required: true,
					})
				}
			}

		case "Query":
			if len(call.Args) > 0 {
				if name := p.extractStringArg(call.Args[0]); name != "" {
					handler.Parameters = append(handler.Parameters, &ParameterInfo{
						Name: name,
						Type: inferQueryParamType(name),
						In:   "query",
					})
				}
			}

		case "DefaultQuery":
			if len(call.Args) >= 2 {
				if name := p.extractStringArg(call.Args[0]); name != "" {
					handler.Parameters = append(handler.Parameters, &ParameterInfo{
						Name:    name,
						Type:    inferQueryParamType(name),
						In:      "query",
						Default: p.extractStringArg(call.Args[1]),
					})
				}
			}

		case "GetHeader":
			if len(call.Args) > 0 {
				if name := p.extractStringArg(call.Args[0]); name != "" {
					handler.Parameters = append(handler.Parameters, &ParameterInfo{
						Name: name,
						Type: "string",
						In:   "header",
					})
				}
			}

		case "ShouldBindQuery", "BindQuery":
			if len(call.Args) > 0 {
				typeName := p.extractTypeFromBindArgWithLocals(call.Args[0], localVarTypes)
				if typeName != "" {
					if typeInfo, ok := p.Types[typeName]; ok {
						for _, field := range typeInfo.Fields {
							paramName := field.JSONName
							if formTag, ok := field.Tags["form"]; ok {
								parts := strings.Split(formTag, ",")
								if parts[0] != "" && parts[0] != "-" {
									paramName = parts[0]
								}
							}
							handler.Parameters = append(handler.Parameters, &ParameterInfo{
								Name:     paramName,
								Type:     field.Type,
								In:       "query",
								Required: field.Required,
							})
						}
					}
				}
			}

		case "ShouldBindUri", "BindUri":
			if len(call.Args) > 0 {
				typeName := p.extractTypeFromBindArgWithLocals(call.Args[0], localVarTypes)
				if typeName != "" {
					if typeInfo, ok := p.Types[typeName]; ok {
						for _, field := range typeInfo.Fields {
							paramName := field.JSONName
							if uriTag, ok := field.Tags["uri"]; ok {
								parts := strings.Split(uriTag, ",")
								if parts[0] != "" && parts[0] != "-" {
									paramName = parts[0]
								}
							}
							handler.Parameters = append(handler.Parameters, &ParameterInfo{
								Name:     paramName,
								Type:     field.Type,
								In:       "path",
								Required: true,
							})
						}
					}
				}
			}
		case "ShouldBindJSON", "BindJSON", "ShouldBind", "Bind":
			if len(call.Args) > 0 {
				typeName := p.extractTypeFromBindArgWithLocals(call.Args[0], localVarTypes)
				if typeName != "" {
					if typeInfo, ok := p.Types[typeName]; ok {
						handler.RequestBody = typeInfo
					} else {
						handler.RequestBody = &TypeInfo{Name: typeName, Kind: "struct"}
					}
				}
			}

		case "JSON":
			if len(call.Args) >= 2 {
				statusCode := p.extractStatusCode(call.Args[0])
				if statusCode > 0 {
					resp := &ResponseInfo{StatusCode: statusCode}
					resp.Type, resp.IsArray = p.extractResponseTypeWithLocals(call.Args[1], localVarTypes)
					handler.Responses[statusCode] = resp
				}
			}

		case "String":
			if len(call.Args) >= 1 {
				statusCode := p.extractStatusCode(call.Args[0])
				if statusCode > 0 {
					handler.Responses[statusCode] = &ResponseInfo{
						StatusCode: statusCode,
						Type:       &TypeInfo{Kind: "primitive", Name: "string"},
					}
				}
			}
		}

		return true
	})
}

// extractRoutes 透過追蹤 router 變數來正確處理 route group
func (p *Parser) extractRoutes(file *ast.File) {
	groupPrefixes := make(map[string]string)

	ast.Inspect(file, func(n ast.Node) bool {
		if assign, ok := n.(*ast.AssignStmt); ok {
			for i, rhs := range assign.Rhs {
				if call, ok := rhs.(*ast.CallExpr); ok {
					if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
						if sel.Sel.Name == "Group" && len(call.Args) > 0 {
							if prefix := p.extractStringArg(call.Args[0]); prefix != "" {
								parentPrefix := ""
								if parentIdent, ok := sel.X.(*ast.Ident); ok {
									parentPrefix = groupPrefixes[parentIdent.Name]
								}
								if i < len(assign.Lhs) {
									if ident, ok := assign.Lhs[i].(*ast.Ident); ok {
										groupPrefixes[ident.Name] = parentPrefix + prefix
									}
								}
							}
						}
					}
				}
			}
			return true
		}

		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}

		method := sel.Sel.Name

		httpMethods := map[string]bool{
			"GET": true, "POST": true, "PUT": true, "DELETE": true,
			"PATCH": true, "OPTIONS": true, "HEAD": true,
			"Any": true, "Handle": true,
		}

		if !httpMethods[method] {
			return true
		}

		if len(call.Args) < 2 {
			return true
		}

		path := p.extractStringArg(call.Args[0])

		groupPrefix := ""
		if receiverIdent, ok := sel.X.(*ast.Ident); ok {
			groupPrefix = groupPrefixes[receiverIdent.Name]
		}

		if path == "" && groupPrefix == "" {
			return true
		}

		handlerArg := call.Args[len(call.Args)-1]
		handlerName := ""

		switch h := handlerArg.(type) {
		case *ast.Ident:
			handlerName = h.Name
		case *ast.SelectorExpr:
			handlerName = h.Sel.Name
		}

		if handlerName == "" || shouldSkipHandler(handlerName) {
			return true
		}

		route := &RouteInfo{
			Method:      method,
			Path:        groupPrefix + path,
			HandlerName: handlerName,
			Group:       groupPrefix,
		}

		p.Routes = append(p.Routes, route)
		return true
	})
}

func (p *Parser) addPathParams(route *RouteInfo) {
	if route.Handler == nil {
		return
	}

	re := regexp.MustCompile(`[:\*](\w+)`)
	matches := re.FindAllStringSubmatch(route.Path, -1)

	existingParams := make(map[string]bool)
	for _, param := range route.Handler.Parameters {
		if param.In == "path" {
			existingParams[param.Name] = true
		}
	}

	for _, match := range matches {
		if len(match) > 1 && !existingParams[match[1]] {
			route.Handler.Parameters = append(route.Handler.Parameters, &ParameterInfo{
				Name:     match[1],
				Type:     "string",
				In:       "path",
				Required: true,
			})
		}
	}
}

func (p *Parser) typeToString(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return p.typeToString(t.X) + "." + t.Sel.Name
	case *ast.StarExpr:
		return "*" + p.typeToString(t.X)
	case *ast.ArrayType:
		return "[]" + p.typeToString(t.Elt)
	case *ast.MapType:
		return "map[" + p.typeToString(t.Key) + "]" + p.typeToString(t.Value)
	case *ast.InterfaceType:
		return "interface{}"
	default:
		return "any"
	}
}

func (p *Parser) extractStringArg(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.BasicLit:
		if e.Kind == token.STRING {
			s, _ := strconv.Unquote(e.Value)
			return s
		}
	}
	return ""
}

func (p *Parser) extractTypeFromBindArgWithLocals(expr ast.Expr, localVars map[string]string) string {
	switch e := expr.(type) {
	case *ast.UnaryExpr:
		if e.Op == token.AND {
			return p.extractTypeFromBindArgWithLocals(e.X, localVars)
		}
	case *ast.Ident:
		if localVars != nil {
			if typeName, ok := localVars[e.Name]; ok {
				return strings.TrimPrefix(typeName, "*")
			}
		}
		return p.findVariableType(e.Name)
	case *ast.CompositeLit:
		return p.typeToString(e.Type)
	}
	return ""
}

func (p *Parser) findVariableType(name string) string {
	for _, file := range p.files {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}

			for _, stmt := range fn.Body.List {
				switch s := stmt.(type) {
				case *ast.DeclStmt:
					if gd, ok := s.Decl.(*ast.GenDecl); ok {
						for _, spec := range gd.Specs {
							if vs, ok := spec.(*ast.ValueSpec); ok {
								for _, n := range vs.Names {
									if n.Name == name && vs.Type != nil {
										return p.typeToString(vs.Type)
									}
								}
							}
						}
					}
				case *ast.AssignStmt:
					for i, lhs := range s.Lhs {
						if id, ok := lhs.(*ast.Ident); ok && id.Name == name {
							if i < len(s.Rhs) {
								if cl, ok := s.Rhs[i].(*ast.CompositeLit); ok {
									return p.typeToString(cl.Type)
								}
							}
						}
					}
				}
			}
		}
	}
	return name
}

func (p *Parser) extractStatusCode(expr ast.Expr) int {
	switch e := expr.(type) {
	case *ast.BasicLit:
		if e.Kind == token.INT {
			code, _ := strconv.Atoi(e.Value)
			return code
		}
	case *ast.SelectorExpr:
		return httpStatusCode(e.Sel.Name)
	case *ast.Ident:
		return httpStatusCode(e.Name)
	}
	return 0
}

func (p *Parser) extractResponseTypeWithLocals(expr ast.Expr, localVars map[string]string) (*TypeInfo, bool) {
	switch e := expr.(type) {
	case *ast.Ident:
		typeName := e.Name

		if typeName == "nil" {
			return nil, false
		}

		if typeInfo, ok := p.Types[typeName]; ok {
			return typeInfo, false
		}

		varType := ""
		if localVars != nil {
			varType = localVars[typeName]
		}
		if varType == "" {
			varType = p.findVariableType(typeName)
		}

		varType = strings.TrimPrefix(varType, "*")

		if strings.HasPrefix(varType, "[]") {
			elemType := strings.TrimPrefix(varType, "[]")
			if typeInfo, ok := p.Types[elemType]; ok {
				return typeInfo, true
			}
			return &TypeInfo{Name: elemType, Kind: "struct"}, true
		}
		if typeInfo, ok := p.Types[varType]; ok {
			return typeInfo, false
		}
		return &TypeInfo{Name: varType, Kind: "struct"}, false

	case *ast.CompositeLit:
		switch t := e.Type.(type) {
		case *ast.ArrayType:
			elemType := p.typeToString(t.Elt)
			if typeInfo, ok := p.Types[elemType]; ok {
				return typeInfo, true
			}
			return &TypeInfo{Name: elemType, Kind: "struct"}, true
		case *ast.MapType:
			return &TypeInfo{Kind: "map", Name: "object"}, false
		case *ast.SelectorExpr:
			if t.Sel.Name == "H" {
				return &TypeInfo{Kind: "map", Name: "object"}, false
			}
		case *ast.Ident:
			if t.Name == "H" {
				return &TypeInfo{Kind: "map", Name: "object"}, false
			}
			if typeInfo, ok := p.Types[t.Name]; ok {
				return typeInfo, false
			}
		}

	case *ast.CallExpr:
		return &TypeInfo{Kind: "any", Name: "object"}, false

	case *ast.UnaryExpr:
		return p.extractResponseTypeWithLocals(e.X, localVars)
	}

	return &TypeInfo{Kind: "any", Name: "object"}, false
}

func parseStructTags(tag string) map[string]string {
	tags := make(map[string]string)

	for tag != "" {
		i := 0
		for i < len(tag) && tag[i] == ' ' {
			i++
		}
		tag = tag[i:]
		if tag == "" {
			break
		}

		i = 0
		for i < len(tag) && tag[i] != ':' && tag[i] != '"' && tag[i] != ' ' {
			i++
		}
		if i >= len(tag) {
			break
		}
		key := tag[:i]
		tag = tag[i:]

		if tag == "" || tag[0] != ':' {
			break
		}
		tag = tag[1:]

		if tag == "" || tag[0] != '"' {
			break
		}
		tag = tag[1:]

		i = 0
		for i < len(tag) && tag[i] != '"' {
			if tag[i] == '\\' {
				i++
			}
			i++
		}
		if i >= len(tag) {
			break
		}

		value := tag[:i]
		tags[key] = value
		tag = tag[i+1:]
	}

	return tags
}

func httpStatusCode(name string) int {
	codes := map[string]int{
		"StatusContinue":            100,
		"StatusOK":                  200,
		"StatusCreated":             201,
		"StatusAccepted":            202,
		"StatusNoContent":           204,
		"StatusMovedPermanently":    301,
		"StatusFound":               302,
		"StatusBadRequest":          400,
		"StatusUnauthorized":        401,
		"StatusForbidden":           403,
		"StatusNotFound":            404,
		"StatusMethodNotAllowed":    405,
		"StatusConflict":            409,
		"StatusUnprocessableEntity": 422,
		"StatusTooManyRequests":     429,
		"StatusInternalServerError": 500,
		"StatusBadGateway":          502,
		"StatusServiceUnavailable":  503,
	}
	return codes[name]
}

// inferQueryParamType 根據參數名稱啟發式推斷型別
func inferQueryParamType(name string) string {
	integerParams := map[string]bool{
		"page": true, "limit": true, "offset": true, "size": true,
		"per_page": true, "page_size": true, "count": true,
		"id": true, "user_id": true, "order_id": true, "product_id": true,
	}
	boolParams := map[string]bool{
		"active": true, "enabled": true, "deleted": true, "archived": true,
		"is_active": true, "is_deleted": true, "published": true,
	}

	lower := strings.ToLower(name)
	if integerParams[lower] {
		return "integer"
	}
	if boolParams[lower] {
		return "boolean"
	}
	return "string"
}

// shouldSkipHandler 判斷是否應該跳過這個 handler（靜態檔案、redirect 等）
func shouldSkipHandler(name string) bool {
	skipPatterns := []string{
		"StaticFile", "Static", "StaticFS",
		"func1", "func2", "func3", "func4", "func5", // anonymous handlers
	}
	for _, pattern := range skipPatterns {
		if strings.Contains(name, pattern) {
			return true
		}
	}
	return false
}

