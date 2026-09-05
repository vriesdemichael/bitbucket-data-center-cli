package projectcmd

import (
	"testing"
)

func TestProjectCLIValidation(t *testing.T) {
	_, err := executeTestCLI(t, "project", "get")
	if err == nil {
		t.Fatal("expected err missing arg")
	}
	_, err = executeTestCLI(t, "project", "create")
	if err == nil {
		t.Fatal("expected err missing arg")
	}
	_, err = executeTestCLI(t, "project", "update")
	if err == nil {
		t.Fatal("expected err missing arg")
	}
	_, err = executeTestCLI(t, "project", "delete")
	if err == nil {
		t.Fatal("expected err missing arg")
	}
}
