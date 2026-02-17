package swaggo

import (
	"go/parser"
	"os"
	"path/filepath"
	"strings"
)

// ParseFromEntry 從指定入口檔案開始，只解析被 import 的 package
func (p *Parser) ParseFromEntry(entryFile string, projectRoot string) error {
	// 解析 go.mod 取得 module name
	moduleName := p.findModuleName(projectRoot)
	if moduleName == "" {
		// 沒有 go.mod，fallback 到掃整個目錄
		return p.ParseDir(projectRoot)
	}

	// 收集需要解析的 package 路徑
	visited := make(map[string]bool)
	toVisit := []string{}

	// 從入口檔案開始
	entryDir := filepath.Dir(entryFile)
	toVisit = append(toVisit, entryDir)

	// BFS 遍歷 import 依賴
	for len(toVisit) > 0 {
		current := toVisit[0]
		toVisit = toVisit[1:]

		if visited[current] {
			continue
		}
		visited[current] = true

		// 解析這個目錄
		pkgs, err := parser.ParseDir(p.fset, current, func(fi os.FileInfo) bool {
			return !strings.HasSuffix(fi.Name(), "_test.go")
		}, parser.ParseComments)
		if err != nil {
			continue
		}

		for name, pkg := range pkgs {
			p.packages[name] = pkg
			for _, file := range pkg.Files {
				p.files = append(p.files, file)

				// 收集這個檔案的 imports
				for _, imp := range file.Imports {
					impPath := strings.Trim(imp.Path.Value, `"`)

					// 只追蹤專案內部的 import
					if strings.HasPrefix(impPath, moduleName) {
						// 轉換成實際路徑
						relPath := strings.TrimPrefix(impPath, moduleName)
						absPath := filepath.Join(projectRoot, relPath)

						if !visited[absPath] && p.dirExists(absPath) && !p.isExcluded(absPath) {
							toVisit = append(toVisit, absPath)
						}
					}
				}
			}
		}
	}

	return nil
}

func (p *Parser) findModuleName(projectRoot string) string {
	goModPath := filepath.Join(projectRoot, "go.mod")
	data, err := os.ReadFile(goModPath)
	if err != nil {
		return ""
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			return strings.TrimPrefix(line, "module ")
		}
	}
	return ""
}

func (p *Parser) dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func (p *Parser) isExcluded(path string) bool {
	name := filepath.Base(path)
	if name == "vendor" && !p.parseVendor {
		return true
	}
	for _, exc := range p.excludeDirs {
		if name == exc || strings.Contains(path, string(filepath.Separator)+exc+string(filepath.Separator)) {
			return true
		}
	}
	return false
}
