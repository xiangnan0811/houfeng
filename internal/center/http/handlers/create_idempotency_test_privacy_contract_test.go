package handlers_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

type idempotencyPrivacyScope struct {
	path             string
	functionPrefixes []string
}

func TestCreateIdempotencyTestFailureMessagesStayContentFree(t *testing.T) {
	t.Parallel()

	storePath := filepath.Join("..", "..", "store")
	scopes := []idempotencyPrivacyScope{
		{path: "asset_links_test.go", functionPrefixes: []string{"TestVPSMonitoringInstances"}},
		{path: "asset_services_test.go", functionPrefixes: []string{"TestVPSServices"}},
		{path: "asset_domains_test.go", functionPrefixes: []string{"TestVPSDomains"}},
		{path: "vps_test.go", functionPrefixes: []string{"TestVPSExperienceLogs"}},
		{path: "create_idempotency_sequence_test.go"},
		{path: filepath.Join(storePath, "subscription_create_idempotency_postgres_integration_test.go")},
		{path: filepath.Join(storePath, "vps_create_idempotency_e2e_test.go")},
		{path: filepath.Join(storePath, "vps_create_idempotency_test.go")},
	}

	for _, scope := range scopes {
		scope := scope
		t.Run(filepath.Base(scope.path), func(t *testing.T) {
			assertIdempotencyTestFailureMessagesStayContentFree(t, scope)
		})
	}
}

func assertIdempotencyTestFailureMessagesStayContentFree(t *testing.T, scope idempotencyPrivacyScope) {
	t.Helper()
	source, err := os.ReadFile(scope.path)
	if err != nil {
		t.Fatalf("read source error type = %T", err)
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, scope.path, source, 0)
	if err != nil {
		t.Fatalf("parse source error type = %T", err)
	}

	violations := 0
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Body == nil || !idempotencyPrivacyFunctionInScope(function.Name.Name, scope.functionPrefixes) {
			continue
		}
		ast.Inspect(function.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || !idempotencyPrivacyFailureCall(call) || idempotencyPrivacyFailureCallIsSafe(call) {
				return true
			}
			violations++
			t.Errorf("%s:%d: failure call can emit content-bearing data", filepath.Base(scope.path), fset.Position(call.Pos()).Line)
			return true
		})
	}
	if violations != 0 {
		t.Fatalf("content-bearing failure calls = %d", violations)
	}
}

func idempotencyPrivacyFunctionInScope(name string, prefixes []string) bool {
	if len(prefixes) == 0 {
		return true
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func idempotencyPrivacyFailureCall(call *ast.CallExpr) bool {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	receiver, ok := selector.X.(*ast.Ident)
	if !ok || receiver.Name != "t" {
		return false
	}
	switch selector.Sel.Name {
	case "Fatal", "Fatalf", "Error", "Errorf":
		return true
	default:
		return false
	}
}

func idempotencyPrivacyFailureCallIsSafe(call *ast.CallExpr) bool {
	selector := call.Fun.(*ast.SelectorExpr)
	formatted := strings.HasSuffix(selector.Sel.Name, "f")
	if len(call.Args) == 0 {
		return false
	}
	message, ok := call.Args[0].(*ast.BasicLit)
	if !ok || message.Kind != token.STRING {
		return false
	}
	if !formatted {
		return len(call.Args) == 1
	}
	format, err := strconv.Unquote(message.Value)
	if err != nil {
		return false
	}
	verbs, ok := idempotencyPrivacyFormatVerbs(format)
	if !ok || len(verbs) != len(call.Args)-1 {
		return false
	}
	for index, argument := range call.Args[1:] {
		if idempotencyPrivacyExpressionContainsResponseBody(argument) || !idempotencyPrivacyFailureArgumentIsSafe(argument, verbs[index]) {
			return false
		}
	}
	return true
}

func idempotencyPrivacyFormatVerbs(format string) ([]byte, bool) {
	verbs := make([]byte, 0, strings.Count(format, "%"))
	for index := 0; index < len(format); index++ {
		if format[index] != '%' {
			continue
		}
		index++
		if index >= len(format) {
			return nil, false
		}
		if format[index] == '%' {
			continue
		}
		for index < len(format) && strings.ContainsRune("#+- 0.123456789[]", rune(format[index])) {
			index++
		}
		if index >= len(format) || !strings.ContainsRune("dtqT", rune(format[index])) {
			return nil, false
		}
		verbs = append(verbs, format[index])
	}
	return verbs, true
}

func idempotencyPrivacyExpressionContainsResponseBody(expression ast.Expr) bool {
	contains := false
	ast.Inspect(expression, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if ok && selector.Sel.Name == "Body" {
			contains = true
			return false
		}
		return true
	})
	return contains
}

func idempotencyPrivacyFailureArgumentIsSafe(expression ast.Expr, verb byte) bool {
	if verb == 'T' {
		return idempotencyPrivacyErrorEvidence(expression)
	}
	switch value := expression.(type) {
	case *ast.BasicLit:
		return true
	case *ast.ParenExpr:
		return idempotencyPrivacyFailureArgumentIsSafe(value.X, verb)
	case *ast.UnaryExpr:
		return value.Op == token.NOT
	case *ast.BinaryExpr:
		switch value.Op {
		case token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ, token.LAND, token.LOR:
			return true
		default:
			return false
		}
	case *ast.CallExpr:
		return idempotencyPrivacySafeEvidenceCall(value)
	case *ast.IndexExpr:
		key, ok := value.Index.(*ast.BasicLit)
		return ok && key.Kind == token.STRING && key.Value == `"code"`
	case *ast.SelectorExpr:
		return idempotencyPrivacySafeEvidenceName(value.Sel.Name)
	case *ast.Ident:
		return idempotencyPrivacySafeEvidenceName(value.Name)
	default:
		return false
	}
}

func idempotencyPrivacyErrorEvidence(expression ast.Expr) bool {
	switch value := expression.(type) {
	case *ast.ParenExpr:
		return idempotencyPrivacyErrorEvidence(value.X)
	case *ast.Ident:
		name := strings.ToLower(value.Name)
		return name == "err" || strings.HasSuffix(name, "err") || strings.HasSuffix(name, "error")
	case *ast.SelectorExpr:
		name := strings.ToLower(value.Sel.Name)
		return strings.HasSuffix(name, "err") || strings.HasSuffix(name, "error")
	default:
		return false
	}
}

func idempotencyPrivacySafeEvidenceCall(call *ast.CallExpr) bool {
	if identifier, ok := call.Fun.(*ast.Ident); ok && identifier.Name == "len" {
		return true
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	switch selector.Sel.Name {
	case "Contains", "DeepEqual", "Is", "As":
		return true
	default:
		return false
	}
}

func idempotencyPrivacySafeEvidenceName(name string) bool {
	name = strings.ToLower(name)
	for _, safeFragment := range []string{"status", "code", "count", "call", "match", "replay", "attempt", "materialization", "insert", "rollback", "commit", "begin", "lookup", "remain"} {
		if strings.Contains(name, safeFragment) {
			return true
		}
	}
	return name == "ok" || name == "id" || name == "want" || strings.HasSuffix(name, "id")
}
