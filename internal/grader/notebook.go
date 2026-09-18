package grader

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type notebook struct {
	Cells []notebookCell `json:"cells"`
}

type notebookCell struct {
	CellType       string             `json:"cell_type"`
	Source         notebookCellSource `json:"source"`
	Outputs        []json.RawMessage  `json:"outputs"`
	ExecutionCount *int               `json:"execution_count"`
}

type notebookCellSource string

func (source *notebookCellSource) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		*source = notebookCellSource(normalizeSource(text))
		return nil
	}

	var lines []string
	if err := json.Unmarshal(data, &lines); err != nil {
		return fmt.Errorf("source must be a string or an array of strings")
	}
	*source = notebookCellSource(normalizeSource(strings.Join(lines, "")))
	return nil
}

func readNotebook(path string) (notebook, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return notebook{}, err
	}

	var value notebook
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&value); err != nil {
		return notebook{}, fmt.Errorf("parse notebook: %w", err)
	}
	return value, nil
}

func normalizeSource(source string) string {
	return strings.ReplaceAll(strings.ReplaceAll(source, "\r\n", "\n"), "\r", "\n")
}

func (cell notebookCell) normalizedType() string {
	return strings.ToLower(strings.TrimSpace(cell.CellType))
}

func sameCell(left, right notebookCell) bool {
	return left.normalizedType() == right.normalizedType() && left.Source == right.Source
}
