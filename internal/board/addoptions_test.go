package board

import (
	"strings"
	"testing"
)

func TestAddOptionsValidate(t *testing.T) {
	ok := []AddOptions{
		{},
		{Value: 1, Effort: 5},
		{Due: "+1d"},
		{Due: "2026-09-01"},
		{Deps: []string{"t-a"}, Checks: []string{"書く"}, Refs: []string{"a.go:1"}},
	}
	for _, o := range ok {
		if err := o.Validate(); err != nil {
			t.Errorf("Validate(%+v) refused a furrow-acceptable add: %v", o, err)
		}
	}

	bad := []struct {
		o    AddOptions
		want string
	}{
		{AddOptions{Value: 6}, "want 1..5"},
		{AddOptions{Effort: -1}, "want 1..5"},
		{AddOptions{Due: "someday"}, "not a date"},
		{AddOptions{Deps: []string{""}}, "needs a task id"},
		// --dep is the same pflag CSV field as --ref (re-review, finding A):
		// a comma'd id would split silently, a bare `"` is pflag's exit 2.
		{AddOptions{Deps: []string{`t-a"b`}}, "CSV"},
		{AddOptions{Deps: []string{"t-a,t-b"}}, "CSV"},
		{AddOptions{Checks: []string{"  "}}, "needs text"},
		{AddOptions{Refs: []string{""}}, "cannot be empty"},
		// The t-pwrp CSV caveat: a comma'd ref would land SPLIT after the
		// reconcile, so it is refused before the flag layer sees it.
		{AddOptions{Refs: []string{"a,b"}}, "CSV"},
	}
	for _, tc := range bad {
		err := tc.o.Validate()
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("Validate(%+v) = %v, want a refusal naming %q", tc.o, err, tc.want)
		}
	}
}
