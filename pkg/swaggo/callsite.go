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
		p.collectRegistrarsFromFile(file, registrars)
	}

	return registrars
}

func (p *Parser) collectRegistrarsFromFile(file *ast.File, registrars map[string]*RouteRegistrar) {
	pkgName := file.Name.Name

	ast.Inspect(file, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Type.Params == nil {
			return true
		}

		reg := p.tryBuildRegistrar(fn, pkgName, file)
		if reg != nil {
			registrars[reg.FullName] = reg
		}

		return true
	})
}

func (p *Parser) tryBuildRegistrar(fn *ast.FuncDecl, pkgName string, file *ast.File) *RouteRegistrar {
	for _, param := range fn.Type.Params.List {
		paramType := p.getGinParamType(param.Type)
		if paramType == "" {
			continue
		}

		paramName := ""
		if len(param.Names) > 0 {
			paramName = param.Names[0].Name
		}

		fullName := p.buildFuncFullName(fn, pkgName)

		return &RouteRegistrar{
			Package:   pkgName,
			Name:      fn.Name.Name,
			FullName:  fullName,
			ParamName: paramName,
			ParamType: paramType,
			File:      file,
			FuncDecl:  fn,
		}
	}
	return nil
}

func (p *Parser) buildFuncFullName(fn *ast.FuncDecl, pkgName string) string {
	if fn.Recv != nil && len(fn.Recv.List) > 0 {
		recvType := p.extractReceiverType(fn.Recv.List[0].Type)
		return pkgName + "." + recvType + "." + fn.Name.Name
	}
	return pkgName + "." + fn.Name.Name
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
	case "RouterGroup", "Engine":
		return sel.Sel.Name
	default:
		return ""
	}
}

// findCallSites 找出路由註冊函數的呼叫點，追蹤傳入的 group prefix
func (p *Parser) findCallSites(registrars map[string]*RouteRegistrar) []CallSite {
	var callSites []CallSite

	for _, file := range p.files {
		sites := p.findCallSitesInFile(file, registrars)
		callSites = append(callSites, sites...)
	}

	return callSites
}

func (p *Parser) findCallSitesInFile(file *ast.File, registrars map[string]*RouteRegistrar) []CallSite {
	pkgName := file.Name.Name
	groupPrefixes := p.collectGroupPrefixes(file)

	var callSites []CallSite

	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		site := p.tryBuildCallSite(call, pkgName, groupPrefixes, registrars)
		if site != nil {
			callSites = append(callSites, *site)
		}

		return true
	})

	return callSites
}

func (p *Parser) collectGroupPrefixes(file *ast.File) map[string]string {
	prefixes := make(map[string]string)

	ast.Inspect(file, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}

		for i, rhs := range assign.Rhs {
			info := p.extractGroupCall(rhs)
			if info == nil {
				continue
			}

			parentPrefix := prefixes[info.parentVar]
			varName := p.getAssignTarget(assign.Lhs, i)
			if varName != "" {
				prefixes[varName] = parentPrefix + info.prefix
			}
		}

		return true
	})

	return prefixes
}

type groupCallInfo struct {
	parentVar string
	prefix    string
}

func (p *Parser) extractGroupCall(expr ast.Expr) *groupCallInfo {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return nil
	}

	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Group" || len(call.Args) == 0 {
		return nil
	}

	prefix := p.extractStringArg(call.Args[0])
	if prefix == "" {
		return nil
	}

	parentVar := ""
	if ident, ok := sel.X.(*ast.Ident); ok {
		parentVar = ident.Name
	}

	return &groupCallInfo{parentVar: parentVar, prefix: prefix}
}

func (p *Parser) getAssignTarget(lhs []ast.Expr, index int) string {
	if index >= len(lhs) {
		return ""
	}
	if ident, ok := lhs[index].(*ast.Ident); ok {
		return ident.Name
	}
	return ""
}

func (p *Parser) tryBuildCallSite(call *ast.CallExpr, pkgName string, groupPrefixes map[string]string, registrars map[string]*RouteRegistrar) *CallSite {
	funcName, groupArg := p.extractCallInfo(call, pkgName)
	if funcName == "" {
		return nil
	}

	reg := p.matchRegistrar(funcName, registrars)
	if reg == nil {
		return nil
	}

	prefix := p.resolveGroupPrefix(groupArg, groupPrefixes)

	return &CallSite{
		Registrar:   reg,
		GroupPrefix: prefix,
	}
}

func (p *Parser) extractCallInfo(call *ast.CallExpr, pkgName string) (funcName string, groupArg ast.Expr) {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		funcName = pkgName + "." + fn.Name
	case *ast.SelectorExpr:
		if ident, ok := fn.X.(*ast.Ident); ok {
			funcName = ident.Name + "." + fn.Sel.Name
		}
	}

	if len(call.Args) > 0 {
		groupArg = call.Args[0]
	}

	return funcName, groupArg
}

func (p *Parser) matchRegistrar(funcName string, registrars map[string]*RouteRegistrar) *RouteRegistrar {
	simpleName := p.getSimpleName(funcName)

	for fullName, reg := range registrars {
		if fullName == funcName {
			return reg
		}
		if strings.HasSuffix(fullName, "."+simpleName) {
			return reg
		}
		if reg.Name == simpleName {
			return reg
		}
	}

	return nil
}

func (p *Parser) resolveGroupPrefix(groupArg ast.Expr, prefixes map[string]string) string {
	if groupArg == nil {
		return ""
	}
	if ident, ok := groupArg.(*ast.Ident); ok {
		return prefixes[ident.Name]
	}
	return ""
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

	p.registerReceiverInstance(registrar.FuncDecl, pkgName)

	ast.Inspect(registrar.FuncDecl.Body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.AssignStmt:
			p.updateGroupPrefixes(node, groupPrefixes)
		case *ast.CallExpr:
			p.tryAddRouteFromCall(node, pkgName, groupPrefixes)
		}
		return true
	})
}

func (p *Parser) registerReceiverInstance(fn *ast.FuncDecl, pkgName string) {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return
	}

	recv := fn.Recv.List[0]
	if len(recv.Names) == 0 {
		return
	}

	recvVarName := recv.Names[0].Name
	recvType := p.extractReceiverType(recv.Type)
	if recvType != "" {
		p.controllerInstances[recvVarName] = pkgName + "." + recvType
	}
}

func (p *Parser) updateGroupPrefixes(assign *ast.AssignStmt, prefixes map[string]string) {
	for i, rhs := range assign.Rhs {
		info := p.extractGroupCall(rhs)
		if info == nil {
			continue
		}

		parentPrefix := prefixes[info.parentVar]
		varName := p.getAssignTarget(assign.Lhs, i)
		if varName != "" {
			prefixes[varName] = parentPrefix + info.prefix
		}
	}
}

func (p *Parser) tryAddRouteFromCall(call *ast.CallExpr, pkgName string, groupPrefixes map[string]string) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return
	}

	if !isHTTPMethod(sel.Sel.Name) || len(call.Args) < 2 {
		return
	}

	path := p.extractStringArg(call.Args[0])
	groupPrefix := p.getReceiverPrefix(sel.X, groupPrefixes)
	fullPath := groupPrefix + path

	handlerArg := call.Args[len(call.Args)-1]
	handlerName := p.resolveHandlerName(handlerArg, pkgName)

	if handlerName == "" || shouldSkipHandler(handlerName) {
		return
	}

	if p.routeExists(sel.Sel.Name, fullPath) {
		return
	}

	p.Routes = append(p.Routes, &RouteInfo{
		Method:      sel.Sel.Name,
		Path:        fullPath,
		HandlerName: handlerName,
		Group:       groupPrefix,
	})
}

func (p *Parser) getReceiverPrefix(expr ast.Expr, prefixes map[string]string) string {
	if ident, ok := expr.(*ast.Ident); ok {
		return prefixes[ident.Name]
	}
	return ""
}

func (p *Parser) routeExists(method, path string) bool {
	for _, existing := range p.Routes {
		if existing.Method == method && existing.Path == path {
			return true
		}
	}
	return false
}

func isHTTPMethod(name string) bool {
	switch name {
	case "GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS", "HEAD", "Any", "Handle":
		return true
	default:
		return false
	}
}
