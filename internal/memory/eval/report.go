package eval

import (
	"fmt"
	"strings"
)

type Report struct {
	CaseID  string
	Results []AssertionResult
	Err     error
}

type AssertionResult struct {
	Name string
	Type string
	Err  error
}

type AssertionFailure struct {
	CaseID    string
	Assertion string
	Expected  string
	Actual    string
}

func (e AssertionFailure) Error() string {
	return fmt.Sprintf("case_id=%s assertion=%s expected=%s actual=%s", e.CaseID, e.Assertion, e.Expected, e.Actual)
}

func (r Report) Failed() bool {
	if r.Err != nil {
		return true
	}
	for _, result := range r.Results {
		if result.Err != nil {
			return true
		}
	}
	return false
}

func (r Report) Error() string {
	var parts []string
	if r.Err != nil {
		parts = append(parts, fmt.Sprintf("case_id=%s error=%v", r.CaseID, r.Err))
	}
	for _, result := range r.Results {
		if result.Err == nil {
			continue
		}
		name := result.Name
		if name == "" {
			name = result.Type
		}
		parts = append(parts, fmt.Sprintf("case_id=%s assertion=%s error=%v", r.CaseID, name, result.Err))
	}
	return strings.Join(parts, "\n")
}
