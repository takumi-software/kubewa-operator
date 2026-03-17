package actions

import (
	"testing"
)

func TestParseScaleReplicas(t *testing.T) {
	cases := []struct {
		action  string
		want    int32
		wantErr bool
	}{
		{"scale:3", 3, false},
		{"scale:0", 0, false},
		{"scale:10", 10, false},
		{"scale:-1", 0, true},
		{"rollback", 0, true},
		{"scale:", 0, true},
		{"scale:abc", 0, true},
	}
	for _, c := range cases {
		got, err := ParseScaleReplicas(c.action)
		if c.wantErr {
			if err == nil {
				t.Errorf("ParseScaleReplicas(%q): expected error, got nil", c.action)
			}
		} else {
			if err != nil {
				t.Errorf("ParseScaleReplicas(%q): unexpected error: %v", c.action, err)
			}
			if got != c.want {
				t.Errorf("ParseScaleReplicas(%q) = %d, want %d", c.action, got, c.want)
			}
		}
	}
}

func TestLabelsToSelector(t *testing.T) {
	labels := map[string]string{"app": "myapp", "env": "prod"}
	sel := labelsToSelector(labels)
	// Both orderings are valid.
	if sel != "app=myapp,env=prod" && sel != "env=prod,app=myapp" {
		t.Errorf("unexpected selector: %q", sel)
	}
}
