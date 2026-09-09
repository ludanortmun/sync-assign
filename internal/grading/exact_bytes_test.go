package grading

import (
	"context"
	"testing"
)

func TestImmutableFullBytes(t *testing.T) {
	for _, content := range []string{"a b\n", "a  b\n", "a\tb\n", "a b\r\n", "a b", "a b\n\x00", "a\u00a0b\n"} {
		t.Run(content, func(t *testing.T) {
			teacher, student := t.TempDir(), t.TempDir()
			writeFile(t, teacher, "test.py", "a b\n")
			writeFile(t, student, "test.py", content)
			result, err := newImmutableFilesUnmodifiedCheck(CheckContext{
				AssignmentDir: student, TeacherAssignmentDir: teacher, ImmutableFiles: []string{"test.py"},
			}).Execute(context.Background())
			if err != nil || result.Passed != (content == "a b\n") {
				t.Fatalf("content %q: %+v %v", content, result, err)
			}
		})
	}
}
