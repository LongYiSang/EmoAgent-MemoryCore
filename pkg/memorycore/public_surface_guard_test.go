package memorycore_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"
)

func TestPublicSurfaceDoesNotExposeMirrorProtocol(t *testing.T) {
	exported := exportedRootNames(t)
	blocked := []string{
		"MirrorAdapter",
		"MirrorNamespaceAdapter",
		"MirrorCandidateAdapter",
		"MirrorDedupSearchAdapter",
		"MirrorDeleteCandidatesAdapter",
		"MirrorActivationAdapter",
		"MirrorRerankAdapter",
		"MirrorEvalConfigurator",
		"MirrorHealthChecker",
		"MirrorNodeRef",
		"MirrorEdgeRef",
		"MirrorNodePayload",
		"MirrorEdgePayload",
		"MirrorNodeUpsertResult",
		"MirrorCandidateRequest",
		"MirrorCandidate",
		"MirrorCandidateSourceBreakdown",
		"MirrorCandidateSidecarDiagnostics",
		"MirrorCandidatePerQueryDiagnostic",
		"MirrorCandidateResult",
		"MirrorDedupSearchRequest",
		"MirrorDedupCandidate",
		"MirrorDedupSearchPolicy",
		"MirrorDedupSearchResult",
		"MirrorDedupSearchCandidate",
		"MirrorDeleteCandidatesRequest",
		"MirrorDeleteCandidateIntent",
		"MirrorDeleteCandidateScope",
		"MirrorDeleteCandidatePolicy",
		"MirrorDeleteCandidatesResult",
		"MirrorDeleteCandidate",
		"MirrorActivationRequest",
		"MirrorActivationSeed",
		"MirrorActivationParams",
		"MirrorActivationCandidate",
		"MirrorActivationPath",
		"MirrorActivationResult",
		"MirrorRerankRequest",
		"MirrorRerankCandidate",
		"MirrorRerankResult",
		"MirrorRerankItem",
		"MirrorEvalConfigRequest",
		"MirrorEvalConfigResult",
	}
	for _, name := range blocked {
		if exported[name] {
			t.Fatalf("public root exports mirror protocol type %s", name)
		}
	}
}

func TestPublicSurfaceDoesNotExposeExtractionRuntime(t *testing.T) {
	exported := exportedRootNames(t)
	blocked := []string{
		"ExtractionLLM",
		"ExtractionLLMRequest",
		"ExtractionLLMResponse",
		"ExtractionRunRequest",
		"ExtractionRunAuditRecord",
	}
	for _, name := range blocked {
		if exported[name] {
			t.Fatalf("public root exports old extraction runtime type %s", name)
		}
	}

	entries, err := os.ReadDir("extractionruntime")
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		t.Fatalf("read extractionruntime dir: %v", err)
	}
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) == ".go" && !isTestFile(entry.Name()) {
			t.Fatalf("public extractionruntime production file remains: %s", entry.Name())
		}
	}
}

func TestPublicSentinelsAreNotAppcoreAliases(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "facade.go", nil, 0)
	if err != nil {
		t.Fatalf("parse facade.go: %v", err)
	}
	blocked := map[string]bool{
		"ErrInvalidOptions": true,
		"ErrInvalidRequest": true,
		"ErrNotFound":       true,
	}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.VAR {
			continue
		}
		for _, spec := range gen.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for idx, name := range value.Names {
				if !blocked[name.Name] || idx >= len(value.Values) {
					continue
				}
				selector, ok := value.Values[idx].(*ast.SelectorExpr)
				if !ok {
					continue
				}
				ident, ok := selector.X.(*ast.Ident)
				if ok && ident.Name == "appcore" {
					t.Fatalf("%s aliases appcore.%s", name.Name, selector.Sel.Name)
				}
			}
		}
	}
}

func exportedRootNames(t *testing.T) map[string]bool {
	t.Helper()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(info os.FileInfo) bool {
		return !isTestFile(info.Name()) && filepath.Ext(info.Name()) == ".go"
	}, 0)
	if err != nil {
		t.Fatalf("parse pkg/memorycore: %v", err)
	}
	pkg := pkgs["memorycore"]
	if pkg == nil {
		t.Fatalf("memorycore package not found")
	}
	exported := map[string]bool{}
	for _, file := range pkg.Files {
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, spec := range gen.Specs {
				switch value := spec.(type) {
				case *ast.TypeSpec:
					if value.Name.IsExported() {
						exported[value.Name.Name] = true
					}
				case *ast.ValueSpec:
					for _, name := range value.Names {
						if name.IsExported() {
							exported[name.Name] = true
						}
					}
				}
			}
		}
	}
	return exported
}

func isTestFile(name string) bool {
	return len(name) >= len("_test.go") && name[len(name)-len("_test.go"):] == "_test.go"
}
