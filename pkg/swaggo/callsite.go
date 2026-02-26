package swaggo

import (
	"go/ast"
	"strings"
)

// RouteRegistrar 路由註冊函數資訊
type RouteRegistrar struct {
	Package   string
	Name      string
	FullName  string
	ParamName string
	ParamType string
	File      *ast.File
	FuncDecl  *ast.FuncDecl
}

// CallSite 呼叫點資訊
type CallSite struct {
	Registrar   *RouteRegistrar
	GroupPrefix string
}

// collectRouteRegistrars 收集所有接受 *gin.RouterGroup 或 *gin.Engine 的函數
func (p *Parser) collectRouteRegistrars() map[string]*RouteRegistrar {
	registrars := make(map[string]*RouteRegistrar)

	for _, file := range p.files {
		pkgName := file.Name.Name

		ast.Inspect(file, func(n ast.Node) bool {
			fn, ok := n.(*ast.FuncDecl)
			if !ok || fn.Type.Params == nil {
				return true
			}

			for _, param := range fn.Type.Params.List {
				paramType := p.getGinParamType(param.Type)
				if paramType == "" {
					continue
				}

				paramName := ""
				if len(param.Names) > 0 {
					paramName = param.Names[0].Name
				}

				fullName := pkgName + "." + fn.Name.Name
				if fn.Recv != nil && len(fn.Recv.List) > 0 {
					recvType := p.extractReceiverType(fn.Recv.List[0].Type)
					fullName = pkgName + "." + recvType + "." + fn.Name.Name
				}

				registrars[fullName] = &RouteRegistrar{
					Package:   pkgName,
					Name:      fn.Name.Name,
					FullName:  fullName,
					ParamName: paramName,
					ParamType: paramType,
					File:      file,
					FuncDecl:  fn,
				}
				break
			}

			return true
		})
	}

	return registrars
}

// getGinParamType 檢查參數是否為 *gin.RouterGroup 或 *gin.Engine
func (p *Parser) getGinParamType(expr ast.Expr) string {
	star, ok := expr.(*ast.StarExpr)
	if !ok {
		return ""
	}

	sel, ok := star.X.(*ast.SelectorExpr)
	if !ok {
		return ""
	}

	ident, ok := sel.X.(*ast.Ident)
	if !ok || ident.Name != "gin" {
		return ""
	}

	switch sel.Sel.Name {
	case "RouterGroup":
		return "RouterGroup"
	case "Engine":
		return "Engine"
	default:
		return ""
	}
}

// findCallSites 找出路由註冊函數的呼叫點，追蹤傳入的 group prefix
func (p *Parser) findCallSites(registrars map[string]*RouteRegistrar) []CallSite {
	var callSites []CallSite

	for _, file := range p.files {
		pkgName := file.Name.Name
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
			}
			return true
		})

		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}

			var funcName string
			var groupArg ast.Expr

			switch fn := call.Fun.(type) {
			case *ast.Ident:
				funcName = pkgName + "." + fn.Name
				if len(call.Args) > 0 {
					groupArg = call.Args[0]
				}

			case *ast.SelectorExpr:
				if ident, ok := fn.X.(*ast.Ident); ok {
					funcName = ident.Name + "." + fn.Sel.Name
					if len(call.Args) > 0 {
						groupArg = call.Args[0]
					}
				}
			}

			if funcName == "" {
				return true
			}

			var matchedRegistrar *RouteRegistrar
			for fullName, reg := range registrars {
				if fullName == funcName || strings.HasSuffix(fullName, "."+p.getSimpleName(funcName)) {
					matchedRegistrar = reg
					break
				}
				if reg.Name == p.getSimpleName(funcName) {
					matchedRegistrar = reg
					break
				}
			}

			if matchedRegistrar == nil {
				return true
			}

			prefix := ""
			if groupArg != nil {
				if ident, ok := groupArg.(*ast.Ident); ok {
					prefix = groupPrefixes[ident.Name]
				}
			}

			callSites = append(callSites, CallSite{
				Registrar:   matchedRegistrar,
				GroupPrefix: prefix,
			})

			return true
		})
	}

	return callSites
}

// extractRoutesWithPrefix 在指定的 prefix 下解析路由註冊函數
func (p *Parser) extractRoutesWithPrefix(registrar *RouteRegistrar, basePrefix string) {
	if registrar.FuncDecl == nil || registrar.FuncDecl.Body == nil {
		return
	}

	pkgName := registrar.Package
	groupPrefixes := make(map[string]string)

	if registrar.ParamName != "" {
		groupPrefixes[registrar.ParamName] = basePrefix
	}

	if registrar.FuncDecl.Recv != nil && len(registrar.FuncDecl.Recv.List) > 0 {
		recv := registrar.FuncDecl.Recv.List[0]
		if len(recv.Names) > 0 {
			recvVarName := recv.Names[0].Name
			recvType := p.extractReceiverType(recv.Type)
			if recvType != "" {
				p.controllerInstances[recvVarName] = pkgName + "." + recvType
			}
		}
	}

	ast.Inspect(registrar.FuncDecl.Body, func(n ast.Node) bool {
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

		fullPath := groupPrefix + path

		handlerArg := call.Args[len(call.Args)-1]
		handlerName := p.resolveHandlerName(handlerArg, pkgName)

		if handlerName == "" || shouldSkipHandler(handlerName) {
			return true
		}

		for _, existing := range p.Routes {
			if existing.Method == method && existing.Path == fullPath {
				return true
			}
		}

		route := &RouteInfo{
			Method:      method,
			Path:        fullPath,
			HandlerName: handlerName,
			Group:       groupPrefix,
		}

		p.Routes = append(p.Routes, route)
		return true
	})
}
