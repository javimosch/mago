package main

import "testing"

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
