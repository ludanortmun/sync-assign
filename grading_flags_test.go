package main

import (
	"testing"
	"time"

	"github.com/alecthomas/kong"
)

func TestGradingFlags(t *testing.T) {
	for _, name := range []string{"grade", "check"} {
		cli := &cliModel{}
		parser, err := kong.New(cli)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := parser.Parse([]string{name, "lab", "--teacher-commit", "abcdef1234", "--timeout", "3m"}); err != nil {
			t.Fatal(err)
		}
		pin, timeout := cli.Check.TeacherCommit, cli.Check.Timeout
		if name == "grade" {
			pin, timeout = cli.Grade.TeacherCommit, cli.Grade.Timeout
		}
		if pin != "abcdef1234" || timeout == nil || *timeout != 3*time.Minute {
			t.Fatalf("%s: pin=%q timeout=%v", name, pin, timeout)
		}
	}
}
