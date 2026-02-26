package swaggo

import (
	"go/ast"
	"strings"
)

// ClosureFactory 閉包工廠函數資訊
type ClosureFactory struct {
	Package  string
	Name     string
	FullName string
	FuncDecl *ast.FuncDecl
	Closure  *ast.FuncLit
}

// collectClosureFactories 收集所有返回 gin.HandlerFunc 的工廠函數
func (p *Parser) collectClosureFactories() map[string]*ClosureFactory {
	factories := make(map[string]*ClosureFactory)

	for _, file := range p.files {
		pkgName := file.Name.Name
		p.collectFactoriesFromFile(file, pkgName, factories)
	}

	return factories
}

func (p *Parser) collectFactoriesFromFile(file *ast.File, pkgName string, factories map[string]*ClosureFactory) {
	ast.Inspect(file, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Type.Results == nil {
			return true
		}

		if !p.returnsGinHandlerFunc(fn.Type.Results) {
			return true
		}

		closure := p.findReturnedClosure(fn)
		if closure == nil {
			return true
		}

		fullName := pkgName + "." + fn.Name.Name
		factories[fullName] = &ClosureFactory{
			Package:  pkgName,
			Name:     fn.Name.Name,
			FullName: fullName,
			FuncDecl: fn,
			Closure:  closure,
		}

		return true
	})
}

func (p *Parser) returnsGinHandlerFunc(results *ast.FieldList) bool {
	if results == nil || len(results.List) == 0 {
		return false
	}

	for _, result := range results.List {
		if p.isGinHandlerFunc(result.Type) {
			return true
		}
	}
	return false
}

func (p *Parser) isGinHandlerFunc(expr ast.Expr) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return false
	}

	ident, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}

	return ident.Name == "gin" && sel.Sel.Name == "HandlerFunc"
}

func (p *Parser) findReturnedClosure(fn *ast.FuncDecl) *ast.FuncLit {
	if fn.Body == nil {
		return nil
	}

	var closure *ast.FuncLit

	ast.Inspect(fn.Body, func(n ast.Node) bool {
		ret, ok := n.(*ast.ReturnStmt)
		if !ok {
			return true
		}

		for _, result := range ret.Results {
			if funcLit, ok := result.(*ast.FuncLit); ok {
				closure = funcLit
				return false
			}
		}
		return true
	})

	return closure
}

func (p *Parser) analyzeClosureAsHandler(factory *ClosureFactory) *HandlerInfo {
	if factory.Closure == nil {
		return nil
	}

	handler := &HandlerInfo{
		Name:      factory.Name,
		FullName:  factory.FullName,
		Package:   factory.Package,
		Responses: make(map[int]*ResponseInfo),
	}

	p.extractDocComment(factory.FuncDecl.Doc, handler)
	p.analyzeClosureBody(factory.Closure, handler)

	return handler
}

func (p *Parser) extractDocComment(doc *ast.CommentGroup, handler *HandlerInfo) {
	if doc == nil {
		return
	}

	lines := strings.Split(strings.TrimSpace(doc.Text()), "\n")
	if len(lines) > 0 {
		handler.Summary = strings.TrimSpace(lines[0])
	}
	if len(lines) > 1 {
		handler.Description = strings.TrimSpace(strings.Join(lines[1:], "\n"))
	}
}

func (p *Parser) analyzeClosureBody(closure *ast.FuncLit, handler *HandlerInfo) {
	if closure.Body == nil {
		return
	}

	localVarTypes := p.collectLocalVarTypes(closure.Body.List)

	ast.Inspect(closure.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		p.analyzeGinCall(call, handler, localVarTypes)
		return true
	})
}

func (p *Parser) collectLocalVarTypes(stmts []ast.Stmt) map[string]string {
	types := make(map[string]string)

	for _, stmt := range stmts {
		switch s := stmt.(type) {
		case *ast.DeclStmt:
			p.collectDeclTypes(s, types)
		case *ast.AssignStmt:
			p.collectAssignTypes(s, types)
		}
	}

	return types
}

func (p *Parser) collectDeclTypes(decl *ast.DeclStmt, types map[string]string) {
	gd, ok := decl.Decl.(*ast.GenDecl)
	if !ok {
		return
	}

	for _, spec := range gd.Specs {
		vs, ok := spec.(*ast.ValueSpec)
		if !ok || vs.Type == nil {
			continue
		}
		for _, n := range vs.Names {
			types[n.Name] = p.typeToString(vs.Type)
		}
	}
}

func (p *Parser) collectAssignTypes(assign *ast.AssignStmt, types map[string]string) {
	for i, lhs := range assign.Lhs {
		id, ok := lhs.(*ast.Ident)
		if !ok || i >= len(assign.Rhs) {
			continue
		}
		if cl, ok := assign.Rhs[i].(*ast.CompositeLit); ok {
			types[id.Name] = p.typeToString(cl.Type)
		}
	}
}

func (p *Parser) analyzeGinCall(call *ast.CallExpr, handler *HandlerInfo, localVarTypes map[string]string) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return
	}

	switch sel.Sel.Name {
	case "Param":
		p.addParamFromCall(call, handler, "path", true)
	case "Query":
		p.addQueryParam(call, handler)
	case "DefaultQuery":
		p.addDefaultQueryParam(call, handler)
	case "GetHeader":
		p.addParamFromCall(call, handler, "header", false)
	case "ShouldBindJSON", "BindJSON", "ShouldBind", "Bind":
		p.addRequestBody(call, handler, localVarTypes)
	case "JSON":
		p.addJSONResponse(call, handler, localVarTypes)
	}
}

func (p *Parser) addParamFromCall(call *ast.CallExpr, handler *HandlerInfo, in string, required bool) {
	if len(call.Args) == 0 {
		return
	}

	name := p.extractStringArg(call.Args[0])
	if name == "" {
		return
	}

	handler.Parameters = append(handler.Parameters, &ParameterInfo{
		Name:     name,
		Type:     "string",
		In:       in,
		Required: required,
	})
}

func (p *Parser) addQueryParam(call *ast.CallExpr, handler *HandlerInfo) {
	if len(call.Args) == 0 {
		return
	}

	name := p.extractStringArg(call.Args[0])
	if name == "" {
		return
	}

	handler.Parameters = append(handler.Parameters, &ParameterInfo{
		Name: name,
		Type: inferQueryParamType(name),
		In:   "query",
	})
}

func (p *Parser) addDefaultQueryParam(call *ast.CallExpr, handler *HandlerInfo) {
	if len(call.Args) < 2 {
		return
	}

	name := p.extractStringArg(call.Args[0])
	if name == "" {
		return
	}

	handler.Parameters = append(handler.Parameters, &ParameterInfo{
		Name:    name,
		Type:    inferQueryParamType(name),
		In:      "query",
		Default: p.extractStringArg(call.Args[1]),
	})
}

func (p *Parser) addRequestBody(call *ast.CallExpr, handler *HandlerInfo, localVarTypes map[string]string) {
	if len(call.Args) == 0 {
		return
	}

	typeName := p.extractTypeFromBindArgWithLocals(call.Args[0], localVarTypes)
	if typeName != "" {
		handler.RequestBody = p.findType(typeName)
	}
}

func (p *Parser) addJSONResponse(call *ast.CallExpr, handler *HandlerInfo, localVarTypes map[string]string) {
	if len(call.Args) < 2 {
		return
	}

	statusCode := p.extractStatusCode(call.Args[0])
	if statusCode <= 0 {
		return
	}

	typeInfo, isArray := p.extractResponseTypeWithLocals(call.Args[1], localVarTypes)
	handler.Responses[statusCode] = &ResponseInfo{
		StatusCode: statusCode,
		Type:       typeInfo,
		IsArray:    isArray,
	}
}

func (p *Parser) registerClosureHandlers(factories map[string]*ClosureFactory) {
	for fullName, factory := range factories {
		if _, exists := p.Handlers[fullName]; exists {
			continue
		}

		handler := p.analyzeClosureAsHandler(factory)
		if handler != nil {
			p.Handlers[fullName] = handler
		}
	}
}
