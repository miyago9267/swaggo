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
	Closure  *ast.FuncLit // return 的閉包
}

// collectClosureFactories 收集所有返回 gin.HandlerFunc 的工廠函數
func (p *Parser) collectClosureFactories() map[string]*ClosureFactory {
	factories := make(map[string]*ClosureFactory)

	for _, file := range p.files {
		pkgName := file.Name.Name

		ast.Inspect(file, func(n ast.Node) bool {
			fn, ok := n.(*ast.FuncDecl)
			if !ok || fn.Type.Results == nil {
				return true
			}

			// 檢查返回型別是否為 gin.HandlerFunc
			if !p.returnsGinHandlerFunc(fn.Type.Results) {
				return true
			}

			// 找到 return 的閉包
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

	return factories
}

// returnsGinHandlerFunc 檢查返回型別是否為 gin.HandlerFunc
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

// isGinHandlerFunc 檢查型別是否為 gin.HandlerFunc
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

// findReturnedClosure 找到函數中 return 的閉包
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

// analyzeClosureAsHandler 分析閉包，提取 handler 資訊
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

	// 從工廠函數的 doc comment 取得 summary/description
	if factory.FuncDecl.Doc != nil {
		lines := strings.Split(strings.TrimSpace(factory.FuncDecl.Doc.Text()), "\n")
		if len(lines) > 0 {
			handler.Summary = strings.TrimSpace(lines[0])
		}
		if len(lines) > 1 {
			handler.Description = strings.TrimSpace(strings.Join(lines[1:], "\n"))
		}
	}

	// 分析閉包體（和一般 handler 一樣的邏輯）
	p.analyzeClosureBody(factory.Closure, handler)

	return handler
}

// analyzeClosureBody 分析閉包體，提取參數和回應
func (p *Parser) analyzeClosureBody(closure *ast.FuncLit, handler *HandlerInfo) {
	if closure.Body == nil {
		return
	}

	localVarTypes := make(map[string]string)

	// 先收集 local variable 型別
	for _, stmt := range closure.Body.List {
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

	ast.Inspect(closure.Body, func(n ast.Node) bool {
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

		case "ShouldBindJSON", "BindJSON", "ShouldBind", "Bind":
			if len(call.Args) > 0 {
				typeName := p.extractTypeFromBindArgWithLocals(call.Args[0], localVarTypes)
				if typeName != "" {
					handler.RequestBody = p.findType(typeName)
				}
			}

		case "JSON":
			if len(call.Args) >= 2 {
				statusCode := p.extractStatusCode(call.Args[0])
				if statusCode > 0 {
					typeInfo, isArray := p.extractResponseTypeWithLocals(call.Args[1], localVarTypes)
					handler.Responses[statusCode] = &ResponseInfo{
						StatusCode: statusCode,
						Type:       typeInfo,
						IsArray:    isArray,
					}
				}
			}
		}

		return true
	})
}

// registerClosureHandlers 註冊閉包 handler 到 Handlers map
func (p *Parser) registerClosureHandlers(factories map[string]*ClosureFactory) {
	for fullName, factory := range factories {
		// 如果已經有同名 handler，跳過（可能是普通函數）
		if _, exists := p.Handlers[fullName]; exists {
			continue
		}

		handler := p.analyzeClosureAsHandler(factory)
		if handler != nil {
			p.Handlers[fullName] = handler
		}
	}
}
