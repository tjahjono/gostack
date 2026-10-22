package architecture_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func violation(file, imported string) string {
	file = filepath.ToSlash(file)
	module := strings.HasPrefix(file, "internal/module/")
	driver := imported == "database/sql" || strings.HasPrefix(imported, "github.com/jackc/") || strings.HasPrefix(imported, "github.com/go-sql-driver/") || strings.HasPrefix(imported, "go.mongodb.org/") || strings.HasPrefix(imported, "github.com/redis/go-redis") || strings.HasPrefix(imported, "gorm.io/") || imported == "github.com/tjahjono/gostack/database"
	concrete := strings.Contains(imported, "/internal/module/") && (strings.Contains(imported, "/repository") || strings.Contains(imported, "/service") || strings.Contains(imported, "/delivery"))
	if module && strings.Contains(file, "/delivery/") && (driver || concrete) {
		return "delivery must depend on service contracts"
	}
	if module && strings.Contains(file, "/service/") && (driver || concrete || imported == "net/http") {
		return "service must depend on repository contracts and ports"
	}
	if module && strings.Contains(file, "/repository/") && (strings.Contains(imported, "/service") || strings.Contains(imported, "/delivery") || imported == "net/http") {
		return "repository cannot depend on service or delivery"
	}
	if module && strings.HasSuffix(file, "/model.go") && (driver || concrete || imported == "net/http" || imported == "github.com/tjahjono/gostack/moduleapi") {
		return "model must contain dependency-free domain values"
	}
	if module && (strings.HasSuffix(file, "/contract/repository.go") || strings.HasSuffix(file, "/contract/service.go")) && (driver || concrete || imported == "net/http") {
		return "business contracts cannot expose infrastructure"
	}
	if strings.HasPrefix(file, "internal/") && strings.HasPrefix(imported, "github.com/tjahjono/gostack/module/") {
		return "internal implementation cannot import its public facade"
	}
	if strings.HasPrefix(file, "internal/bootstrap/") && imported == "github.com/tjahjono/gostack/framework" {
		return "bootstrap cannot import its public factory"
	}
	return ""
}

func TestProductionArchitecture(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if entry.Name() == ".git" || entry.Name() == "vendor" || entry.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		relative, _ := filepath.Rel(root, path)
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		for _, spec := range parsed.Imports {
			imported, _ := strconv.Unquote(spec.Path.Value)
			if reason := violation(relative, imported); reason != "" {
				t.Errorf("%s imports %s: %s", relative, imported, reason)
			}
		}
		for _, declaration := range parsed.Decls {
			switch typed := declaration.(type) {
			case *ast.FuncDecl:
				if typed.Recv == nil && typed.Name.Name == "init" {
					t.Errorf("%s: implicit init registration is forbidden", relative)
				}
			case *ast.GenDecl:
				if typed.Tok == token.VAR {
					for _, spec := range typed.Specs {
						for _, name := range spec.(*ast.ValueSpec).Names {
							if name.Name != "_" {
								t.Errorf("%s: mutable package variable %s is forbidden", relative, name.Name)
							}
						}
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestArchitectureRulesDetectForbiddenFlows(t *testing.T) {
	for _, test := range []struct{ file, imported string }{
		{"internal/module/product/delivery/http/get.go", "github.com/tjahjono/gostack/internal/module/product/repository"},
		{"internal/module/product/service/get.go", "github.com/jackc/pgx/v5"},
		{"internal/module/product/service/get.go", "github.com/tjahjono/gostack/internal/module/user/service"},
		{"internal/module/product/repository/get.go", "github.com/tjahjono/gostack/internal/module/product/service"},
		{"internal/module/product/model.go", "database/sql"},
		{"internal/module/product/contract/repository.go", "net/http"},
		{"internal/bootstrap/bootstrap.go", "github.com/tjahjono/gostack/framework"},
	} {
		if violation(test.file, test.imported) == "" {
			t.Errorf("missed %s -> %s", test.file, test.imported)
		}
	}
	if reason := violation("internal/module/product/delivery/http/get.go", "github.com/tjahjono/gostack/internal/module/product/contract"); reason != "" {
		t.Fatal(reason)
	}
}
