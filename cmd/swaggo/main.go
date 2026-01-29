package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/miyago9267/swaggo/pkg/swaggo"
)

var version = "dev"

func main() {
	var (
		dir         string
		output      string
		format      string
		title       string
		description string
		apiVersion  string
		showVersion bool
	)

	flag.StringVar(&dir, "dir", ".", "要解析的目錄")
	flag.StringVar(&output, "output", "docs/openapi", "輸出檔案路徑（不含副檔名）")
	flag.StringVar(&format, "format", "both", "輸出格式：json, yaml, both")
	flag.StringVar(&title, "title", "API Documentation", "API 標題")
	flag.StringVar(&description, "desc", "", "API 描述")
	flag.StringVar(&apiVersion, "version", "1.0.0", "API 版本")
	flag.BoolVar(&showVersion, "v", false, "顯示版本")
	flag.Parse()

	if showVersion {
		fmt.Printf("swaggo %s\n", version)
		os.Exit(0)
	}

	gen := swaggo.New().
		WithTitle(title).
		WithDescription(description).
		WithVersion(apiVersion)

	if err := gen.ParseSource(dir); err != nil {
		fmt.Fprintf(os.Stderr, "解析錯誤：%v\n", err)
		os.Exit(1)
	}

	spec, err := gen.Generate()
	if err != nil {
		fmt.Fprintf(os.Stderr, "產生錯誤：%v\n", err)
		os.Exit(1)
	}

	if format == "json" || format == "both" {
		data, err := spec.ToJSON()
		if err != nil {
			fmt.Fprintf(os.Stderr, "JSON 序列化錯誤：%v\n", err)
			os.Exit(1)
		}
		if err := os.WriteFile(output+".json", data, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "寫入 JSON 錯誤：%v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Generated %s.json\n", output)
	}

	if format == "yaml" || format == "both" {
		data, err := spec.ToYAML()
		if err != nil {
			fmt.Fprintf(os.Stderr, "YAML 序列化錯誤：%v\n", err)
			os.Exit(1)
		}
		if err := os.WriteFile(output+".yaml", data, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "寫入 YAML 錯誤：%v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Generated %s.yaml\n", output)
	}

	fmt.Println("Done!")
}
