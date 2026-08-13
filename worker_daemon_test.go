package main

import "testing"

func TestStripArg(t *testing.T) {
	cases := []struct {
		name string
		args []string
		flag string
		want []string
	}{
		{"removes flag", []string{"mago", "serve", "--relay"}, "--relay", []string{"mago", "serve"}},
		{"no match", []string{"mago", "serve"}, "--daemon", []string{"mago", "serve"}},
		{"empty", []string{}, "--relay", []string{}},
		{"removes all occurrences", []string{"--relay", "mago", "--relay"}, "--relay", []string{"mago"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := stripArg(c.args, c.flag)
			if len(got) != len(c.want) {
				t.Fatalf("stripArg(%v, %q) = %v, want %v", c.args, c.flag, got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Errorf("stripArg(%v, %q)[%d] = %q, want %q", c.args, c.flag, i, got[i], c.want[i])
				}
			}
		})
	}
}

func TestClassifyServeProc(t *testing.T) {
	dir := "/root/co-am"
	cases := []struct {
		name       string
		args       []string
		match      bool
		supervisor bool
	}{
		{"worker", []string{"/x/mago", "serve", "--relay", "-C", "/root/co-am"}, true, false},
		{"supervisor", []string{"/x/mago", "serve", "--supervise", "--relay", "-C", "/root/co-am"}, true, true},
		{"stop invocation excluded", []string{"/x/mago", "serve", "stop", "-C", "/root/co-am"}, false, false},
		{"prefix dir not matched (co-ampanel)", []string{"/x/mago", "serve", "--relay", "-C", "/root/co-ampanel"}, false, false},
		{"other company", []string{"/x/mago", "serve", "--relay", "-C", "/root/co-mago"}, false, false},
		{"not serve", []string{"/x/mago", "digest", "-C", "/root/co-am"}, false, false},
		{"-C without value", []string{"/x/mago", "serve", "--relay", "-C"}, false, false},
	}
	for _, c := range cases {
		m, s := classifyServeProc(c.args, dir)
		if m != c.match || (m && s != c.supervisor) {
			t.Errorf("%s: got match=%v sup=%v, want match=%v sup=%v", c.name, m, s, c.match, c.supervisor)
		}
	}
}
