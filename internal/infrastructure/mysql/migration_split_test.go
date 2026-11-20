package mysqlstore

import (
	"strings"
	"testing"
)

func TestSplitSQLStatementsIgnoresSemicolonsInCommentsAndQuotes(t *testing.T) {
	input := `
-- prose comment with a semicolon; must not split here
CREATE TABLE sample (
    value VARCHAR(20) DEFAULT 'a;b'
);
/* block comment; still one statement */
INSERT INTO sample(value) VALUES ('x;y');
`
	statements := splitSQLStatements(input)
	if len(statements) != 2 {
		t.Fatalf("got %d statements, want 2: %#v", len(statements), statements)
	}
	if !strings.Contains(statements[0], "CREATE TABLE sample") || !strings.Contains(statements[0], "'a;b'") {
		t.Fatalf("create statement lost quoted value: %q", statements[0])
	}
	if !strings.Contains(statements[1], "INSERT INTO sample") || !strings.Contains(statements[1], "'x;y'") {
		t.Fatalf("insert statement lost quoted value: %q", statements[1])
	}
}
