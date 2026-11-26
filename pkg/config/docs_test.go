package config

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The config docs embed each struct of types.go by line range, so any change to types.go that
// moves lines must update docs/get-started/configuration.md too.
func TestConfigDocsInSync(t *testing.T) {
	doc, err := os.ReadFile("../../docs/get-started/configuration.md")
	if err != nil {
		t.Fatal(err)
	}

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "types.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	structs := map[string][2]int{} // lowercased name -> first and last line
	ast.Inspect(file, func(n ast.Node) bool {
		if spec, ok := n.(*ast.TypeSpec); ok {
			if _, ok := spec.Type.(*ast.StructType); ok {
				structs[strings.ToLower(spec.Name.Name)] = [2]int{fset.Position(spec.Pos()).Line, fset.Position(spec.End()).Line}
			}
		}
		return true
	})

	const url = `https://github\.com/cedana/cedana/blob/main/pkg/config/types\.go#L(\d+)-L(\d+)`
	sections := regexp.MustCompile(`## \[(\w+)\]\(` + url + `\)\n\n\{% @github-files/github-code-block url="` + url + `" %\}`).
		FindAllStringSubmatch(string(doc), -1)

	documented := map[string]bool{}
	for _, s := range sections {
		name := strings.ToLower(s[1])
		documented[name] = true
		lines, ok := structs[name]
		if !ok {
			t.Errorf("docs have a %s section, but types.go has no such struct", s[1])
			continue
		}
		want := "L" + strconv.Itoa(lines[0]) + "-L" + strconv.Itoa(lines[1])
		if got := "L" + s[2] + "-L" + s[3]; got != want {
			t.Errorf("%s section links %s, want %s", s[1], got, want)
		}
		if got := "L" + s[4] + "-L" + s[5]; got != want {
			t.Errorf("%s section embeds %s, want %s", s[1], got, want)
		}
	}
	for name := range structs {
		if !documented[name] {
			t.Errorf("struct %s in types.go has no section in the docs", name)
		}
	}
}
