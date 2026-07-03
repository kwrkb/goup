package main

import "testing"

// TestElevationDecision exhaustively covers the 4-bit decision space
// (uid==0 collapsed to a single bit). The three outcomes we care about
// are run / elevate / fail — the pure function is where the branching
// gets pinned down because the runtime path can only be smoke-tested.
func TestElevationDecision(t *testing.T) {
	cases := []struct {
		name    string
		uid     int
		write   bool
		tty     bool
		noSudo  bool
		want    decision
	}{
		// root always runs, regardless of everything else.
		{"root+writable+tty", 0, true, true, false, decisionRun},
		{"root+writable+notty", 0, true, false, false, decisionRun},
		{"root+unwritable+tty", 0, false, true, false, decisionRun},
		{"root+nosudo", 0, false, true, true, decisionRun},

		// non-root with write access already: just run.
		{"user+writable+tty", 1000, true, true, false, decisionRun},
		{"user+writable+notty", 1000, true, false, false, decisionRun},
		{"user+writable+nosudo", 1000, true, true, true, decisionRun},

		// non-root, no write, TTY, no --no-sudo → elevate.
		{"user+unwritable+tty", 1000, false, true, false, decisionElevate},

		// non-root, no write, non-TTY → fast-fail (CI / pipe / redirect).
		{"user+unwritable+notty", 1000, false, false, false, decisionFail},

		// non-root, no write, --no-sudo → fast-fail even on TTY.
		{"user+unwritable+tty+nosudo", 1000, false, true, true, decisionFail},
		{"user+unwritable+notty+nosudo", 1000, false, false, true, decisionFail},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := elevationDecision(c.uid, c.write, c.tty, c.noSudo)
			if got != c.want {
				t.Fatalf("elevationDecision(uid=%d, write=%v, tty=%v, noSudo=%v) = %d, want %d",
					c.uid, c.write, c.tty, c.noSudo, got, c.want)
			}
		})
	}
}
